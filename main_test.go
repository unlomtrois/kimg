package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePNG puts a solid test image on disk and returns its path.
func writePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	path := filepath.Join(t.TempDir(), "test.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// exec runs the root command with args, returning stdout, stderr and the error.
func exec(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestCLIRendersPlaceholdersByDefault(t *testing.T) {
	t.Setenv("TMUX", "")
	path := writePNG(t, 100, 50)

	stdout, stderr, err := exec(t, "-c", "10", "-r", "4", path)
	if err != nil {
		t.Fatalf("err = %v, stderr = %q", err, stderr)
	}
	if !strings.Contains(stdout, "U=1") {
		t.Error("expected a virtual placement (U=1)")
	}
	if n := strings.Count(stdout, string(placeholder)); n != 40 {
		t.Errorf("got %d placeholder cells, want 40", n)
	}
}

func TestCLIDirectSkipsPlaceholders(t *testing.T) {
	t.Setenv("TMUX", "")
	path := writePNG(t, 100, 50)

	stdout, _, err := exec(t, "--direct", "-c", "10", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, string(placeholder)) {
		t.Error("--direct should not emit placeholder cells")
	}
	if strings.Contains(stdout, "U=1") {
		t.Error("--direct should not create a virtual placement")
	}
}

func TestCLITmuxPassthrough(t *testing.T) {
	path := writePNG(t, 40, 20)

	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	stdout, _, err := exec(t, "-c", "8", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "\x1bPtmux;") {
		t.Error("expected tmux passthrough wrapping when $TMUX is set")
	}

	stdout, _, err = exec(t, "--no-tmux", "-c", "8", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "\x1bPtmux;") {
		t.Error("--no-tmux should suppress passthrough wrapping")
	}
}

func TestCLIKeepsGoingAfterABadFile(t *testing.T) {
	t.Setenv("TMUX", "")
	good := writePNG(t, 40, 20)
	bad := filepath.Join(t.TempDir(), "missing.png")

	stdout, stderr, err := exec(t, "-c", "8", bad, good)
	if err == nil {
		t.Error("expected a non-nil error so the exit status is non-zero")
	}
	if err != nil && err.Error() != "" {
		t.Errorf("error should be silent, already reported on stderr; got %q", err)
	}
	if !strings.Contains(stderr, "missing.png") {
		t.Errorf("stderr should name the failing file, got %q", stderr)
	}
	if !strings.Contains(stdout, string(placeholder)) {
		t.Error("the readable file should still have been rendered")
	}
}

func TestCLIRequiresAFile(t *testing.T) {
	if _, _, err := exec(t); err == nil {
		t.Error("expected an error when no file is given")
	}
}

func TestParseCell(t *testing.T) {
	if w, h, err := parseCell("9x18"); err != nil || w != 9 || h != 18 {
		t.Errorf("got %d,%d,%v", w, h, err)
	}
	for _, bad := range []string{"", "9", "9x", "0x18", "-9x18", "axb"} {
		if _, _, err := parseCell(bad); err == nil {
			t.Errorf("parseCell(%q) should have failed", bad)
		}
	}
}

func TestCLIVersion(t *testing.T) {
	stdout, _, err := exec(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "kimg ") {
		t.Errorf("got %q, want it to start with \"kimg \"", stdout)
	}
	if strings.TrimSpace(strings.TrimPrefix(stdout, "kimg ")) == "" {
		t.Error("version is empty")
	}
}

func TestBuildVersionPrefersLinkerStamp(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })

	version = "v9.9.9"
	if got := buildVersion(); got != "v9.9.9" {
		t.Errorf("got %q, want the stamped value", got)
	}

	// Falling back to build metadata must still say something usable.
	version = ""
	if got := buildVersion(); got == "" {
		t.Error("fallback version is empty")
	}
}
