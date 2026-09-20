package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/color"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

// parse pulls the key/value header and the concatenated payload back out of a
// stream of APC sequences, so tests can check what the terminal would receive.
func parse(t *testing.T, s string) (map[string]string, []byte) {
	t.Helper()
	keys := map[string]string{}
	var b64 strings.Builder
	for _, seq := range strings.Split(s, "\x1b_G")[1:] {
		body, ok := strings.CutSuffix(seq, "\x1b\\")
		if !ok {
			t.Fatalf("sequence not terminated by ST: %q", seq)
		}
		head, payload, ok := strings.Cut(body, ";")
		if !ok {
			t.Fatalf("sequence has no payload separator: %q", body)
		}
		if len(payload) > chunkSize {
			t.Fatalf("chunk of %d bytes exceeds %d", len(payload), chunkSize)
		}
		for _, kv := range strings.Split(head, ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				if _, seen := keys[k]; !seen || k == "m" {
					keys[k] = v
				}
			}
		}
		b64.WriteString(payload)
	}
	raw, err := base64.StdEncoding.DecodeString(b64.String())
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	return keys, raw
}

func TestTransmitRoundTrip(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 60, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 60; x++ {
			src.SetNRGBA(x, y, color.NRGBA{uint8(x * 4), uint8(y * 6), 128, 255})
		}
	}

	var out bytes.Buffer
	if err := transmitRGBA(&out, src, 30, 0, identity); err != nil {
		t.Fatal(err)
	}

	keys, payload := parse(t, out.String())
	for k, want := range map[string]string{
		"a": "T", "f": "32", "o": "z", "s": "60", "v": "40", "c": "30", "m": "0",
	} {
		if keys[k] != want {
			t.Errorf("key %s = %q, want %q", k, keys[k], want)
		}
	}
	if _, ok := keys["r"]; ok {
		t.Errorf("r should be omitted so the terminal preserves aspect ratio, got %q", keys["r"])
	}

	zr, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	pix, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if len(pix) != 60*40*4 {
		t.Fatalf("got %d pixel bytes, want %d", len(pix), 60*40*4)
	}
	if !bytes.Equal(pix, src.Pix) {
		t.Error("decompressed pixels differ from the source image")
	}
}

func TestChunking(t *testing.T) {
	// Incompressible noise, so the payload is certain to exceed one chunk.
	src := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range src.Pix {
		src.Pix[i] = uint8(rng.Uint32())
	}
	var out bytes.Buffer
	if err := transmitRGBA(&out, src, 40, 0, identity); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "\x1b_G"); n < 2 {
		t.Fatalf("expected multiple chunks, got %d", n)
	}
	if !strings.Contains(out.String(), "m=1;") {
		t.Error("non-final chunks must carry m=1")
	}
	parse(t, out.String()) // enforces the per-chunk size limit
}

func TestAlphaIsUnpremultiplied(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{64, 32, 16, 128}) // premultiplied storage
	dst := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	drawOver(dst, src)
	if got, want := dst.Pix[:4], []uint8{128, 64, 32, 128}; !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTmuxEscape(t *testing.T) {
	got := tmuxEscape("\x1b_Ga=T;AAA\x1b\\")
	want := "\x1bPtmux;\x1b\x1b_Ga=T;AAA\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPlaceholderGrid(t *testing.T) {
	got := placeholderGrid(3, 2, 2)
	want := "\x1b[38;5;3m" +
		string([]rune{placeholder, diacritics[0], diacritics[0]}) +
		string([]rune{placeholder, diacritics[0], diacritics[1]}) +
		"\x1b[39m\n" +
		"\x1b[38;5;3m" +
		string([]rune{placeholder, diacritics[1], diacritics[0]}) +
		string([]rune{placeholder, diacritics[1], diacritics[1]}) +
		"\x1b[39m\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPlaceholderCreatesVirtualPlacement(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	var out bytes.Buffer
	if err := transmitPlaceholder(&out, src, 10, 3, identity); err != nil {
		t.Fatal(err)
	}
	apc, _, _ := strings.Cut(out.String(), "\x1b[38;")
	keys, _ := parse(t, apc)
	for k, want := range map[string]string{"a": "T", "U": "1", "c": "10", "r": "3"} {
		if keys[k] != want {
			t.Errorf("key %s = %q, want %q", k, keys[k], want)
		}
	}
	if n := strings.Count(out.String(), string(placeholder)); n != 30 {
		t.Errorf("got %d placeholder cells, want 30", n)
	}
}

func TestPlaceholderRejectsOversizedGrid(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	err := transmitPlaceholder(io.Discard, src, 10, len(diacritics)+1, identity)
	if _, ok := err.(ErrTooLarge); !ok {
		t.Errorf("got %v, want ErrTooLarge", err)
	}
}

func TestCellRows(t *testing.T) {
	// A 400x400 square across 20 cells of 9x18 covers half as many rows.
	src := image.NewNRGBA(image.Rect(0, 0, 400, 400))
	if got := cellRows(src, 20, 9, 18); got != 10 {
		t.Errorf("got %d rows, want 10", got)
	}
	// Never degenerate to zero.
	wide := image.NewNRGBA(image.Rect(0, 0, 1000, 1))
	if got := cellRows(wide, 10, 9, 18); got != 1 {
		t.Errorf("got %d rows, want 1", got)
	}
}

func TestDiacriticTableIsComplete(t *testing.T) {
	if len(diacritics) != 297 {
		t.Errorf("got %d diacritics, want 297", len(diacritics))
	}
	seen := map[rune]bool{}
	for i, d := range diacritics {
		if seen[d] {
			t.Fatalf("duplicate diacritic at index %d: %U", i, d)
		}
		seen[d] = true
	}
}

// The bug this guards: ids used to be a per-invocation counter, so a second
// kimg run reused id 1 and overwrote the image still shown on screen.
func TestImageIDDistinguishesContentAndSize(t *testing.T) {
	red := bytes.Repeat([]byte{255, 0, 0, 255}, 64)
	blue := bytes.Repeat([]byte{0, 0, 255, 255}, 64)

	if imageID(red, 20, 3) == imageID(blue, 20, 3) {
		t.Error("different images share an id, so one would overwrite the other")
	}
	if imageID(red, 20, 3) == imageID(red, 40, 6) {
		t.Error("same image at a different size shares an id, so the first would be resized")
	}
	if imageID(red, 20, 3) != imageID(red, 20, 3) {
		t.Error("id is not stable, so re-showing a file wastes a terminal image slot")
	}
	for _, pix := range [][]byte{red, blue, nil} {
		if id := imageID(pix, 1, 1); id == 0 || id > 0xffffff {
			t.Errorf("id %d is outside the usable 24-bit non-zero range", id)
		}
	}
}
