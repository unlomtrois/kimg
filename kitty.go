package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"image"
	"io"
	"sort"
	"strings"
)

// The kitty graphics protocol wraps every command in APC ... ST:
//
//	ESC _ G <key=value,...> ; <base64 payload> ESC \
//
// Payloads larger than 4096 base64 bytes are split into chunks; every chunk
// but the last carries m=1, and the terminal concatenates them.
const chunkSize = 4096

type cmd struct {
	keys    map[string]string
	payload []byte
}

// encode renders one command, chunking the payload as needed. escape lets the
// caller wrap each APC sequence (used for tmux passthrough).
func (c cmd) encode(w io.Writer, escape func(string) string) error {
	b64 := base64.StdEncoding.EncodeToString(c.payload)

	// Deterministic key order keeps the output diffable and testable.
	names := make([]string, 0, len(c.keys))
	for k := range c.keys {
		names = append(names, k)
	}
	sort.Strings(names)
	var kv strings.Builder
	for i, k := range names {
		if i > 0 {
			kv.WriteByte(',')
		}
		fmt.Fprintf(&kv, "%s=%s", k, c.keys[k])
	}

	first := true
	for {
		n := min(len(b64), chunkSize)
		chunk := b64[:n]
		b64 = b64[n:]

		var head strings.Builder
		if first {
			head.WriteString(kv.String())
		}
		if len(b64) > 0 {
			if head.Len() > 0 {
				head.WriteByte(',')
			}
			head.WriteString("m=1")
		} else if !first {
			head.WriteString("m=0")
		}

		seq := "\x1b_G" + head.String() + ";" + chunk + "\x1b\\"
		if _, err := io.WriteString(w, escape(seq)); err != nil {
			return err
		}
		first = false
		if len(b64) == 0 {
			return nil
		}
	}
}

// tmuxEscape wraps a sequence in tmux's passthrough DCS. tmux forwards the
// inner bytes verbatim to the outer terminal, but it does not understand the
// graphics protocol itself, so it will not repaint the image on redraw.
// Requires: tmux set -g allow-passthrough on
func tmuxEscape(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

func identity(seq string) string { return seq }

// imageID derives a stable id from what will be displayed. Ids must be unique
// across invocations: transmitting an id the terminal already holds replaces
// that image, and any placeholder cells still on screen referencing it repaint
// with the new pixels. Hashing rather than counting also means re-showing the
// same file at the same size reuses one entry in the terminal's store.
//
// The id is kept to 24 bits so it fits the truecolour foreground encoding that
// placeholder cells use, and never 0, which the protocol reserves.
func imageID(pix []byte, cols, rows int) int {
	h := fnv.New32a()
	h.Write(pix)
	fmt.Fprintf(h, "|%dx%d", cols, rows)
	if id := int(h.Sum32() & 0xffffff); id != 0 {
		return id
	}
	return 1
}

// transmitRGBA sends the decoded pixels as zlib-compressed 32-bit RGBA (f=32,
// o=z) and displays them (a=T). Going through pixels rather than the original
// file means JPEG, GIF and WebP all take the same path as PNG.
func transmitRGBA(w io.Writer, img image.Image, cols, rows int, escape func(string) string) error {
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	drawOver(rgba, img)
	id := imageID(rgba.Pix, cols, rows)

	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(rgba.Pix); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}

	keys := map[string]string{
		"a": "T",
		"f": "32",
		"o": "z",
		"s": fmt.Sprint(b.Dx()),
		"v": fmt.Sprint(b.Dy()),
		"i": fmt.Sprint(id),
		"q": "2", // suppress the terminal's OK/error replies
	}
	// Given only c (or only r), kitty scales the other axis to preserve the
	// aspect ratio.
	if cols > 0 {
		keys["c"] = fmt.Sprint(cols)
	}
	if rows > 0 {
		keys["r"] = fmt.Sprint(rows)
	}
	return cmd{keys: keys, payload: buf.Bytes()}.encode(w, escape)
}

func drawOver(dst *image.NRGBA, src image.Image) {
	b := src.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := dst.PixOffset(x, y)
			if a == 0 {
				dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = 0, 0, 0, 0
				continue
			}
			// RGBA() is alpha-premultiplied and 16-bit; undo both, rounding
			// rather than truncating so opaque pixels survive exactly.
			un := func(v uint32) uint8 { return uint8((v*255 + a/2) / a) }
			dst.Pix[i] = un(r)
			dst.Pix[i+1] = un(g)
			dst.Pix[i+2] = un(bl)
			dst.Pix[i+3] = uint8((a*255 + 0x7fff) / 0xffff)
		}
	}
}
