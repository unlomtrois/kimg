package main

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/spf13/cobra"
)

// Fallback cell geometry for when the terminal will not report pixel
// dimensions, as tmux 3.4 does not. Most monospace setups are near 1:2.
const (
	defaultCellW = 9
	defaultCellH = 18
)

type options struct {
	cols   int
	rows   int
	cell   string
	direct bool
	noTmux bool
}

func main() {
	cmd := newRootCmd()
	// Errors are printed here so that per-file failures, already reported as
	// they happened, can exit non-zero without a second message.
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, "kimg:", msg)
		}
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var opt options

	cmd := &cobra.Command{
		Use:   "kimg [flags] FILE...",
		Short: "Show images in a terminal using the kitty graphics protocol",
		Long: "Show images in a terminal that speaks the kitty graphics protocol, such as\n" +
			"Ghostty or kitty.\n\n" +
			"By default images are drawn as Unicode placeholder cells, so a multiplexer\n" +
			"sees them as ordinary text: tmux reserves the lines, repaints them on redraw\n" +
			"and scrolls them into its history. Pass --direct for a plain placement, which\n" +
			"is simpler but is not tracked by tmux.",
		Version:      buildVersion(),
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.OutOrStdout(), cmd.ErrOrStderr(), args, opt)
		},
	}

	cmd.SetVersionTemplate("kimg {{.Version}}\n")

	f := cmd.Flags()
	f.IntVarP(&opt.cols, "cols", "c", 0, "width in terminal cells (0 fits the terminal, capped at the image's own size)")
	f.IntVarP(&opt.rows, "rows", "r", 0, "height in terminal cells (0 derives it from the aspect ratio)")
	f.StringVar(&opt.cell, "cell", "", "cell size in pixels as WxH, for terminals that will not report it")
	f.BoolVar(&opt.direct, "direct", false, "place directly (a=T) instead of using Unicode placeholders")
	f.BoolVar(&opt.noTmux, "no-tmux", false, "do not use tmux passthrough even when $TMUX is set")

	return cmd
}

func run(stdout, stderr io.Writer, paths []string, opt options) error {
	escape := identity
	if os.Getenv("TMUX") != "" && !opt.noTmux {
		escape = tmuxEscape
	}

	term := termSize()
	if opt.cell != "" {
		w, h, err := parseCell(opt.cell)
		if err != nil {
			return err
		}
		term.cellW, term.cellH = w, h
	}
	if term.cellW == 0 || term.cellH == 0 {
		term.cellW, term.cellH = defaultCellW, defaultCellH
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()

	var failed bool
	for _, path := range paths {
		img, err := decode(path)
		if err != nil {
			// One unreadable file should not abort the rest.
			fmt.Fprintf(stderr, "kimg: %s: %v\n", path, err)
			failed = true
			continue
		}

		c, r := opt.cols, opt.rows
		if c == 0 {
			c = term.cols
			// Never blow a small image up to the full window.
			if natural := img.Bounds().Dx() / term.cellW; natural > 0 && natural < c {
				c = natural
			}
		}

		if opt.direct {
			// The terminal derives the missing axis itself.
			err = transmitRGBA(out, img, c, r, escape)
			fmt.Fprintln(out)
		} else {
			if r == 0 {
				r = cellRows(img, c, term.cellW, term.cellH)
			}
			err = transmitPlaceholder(out, img, c, r, escape)
		}
		if err != nil {
			return err
		}
		out.Flush()
	}
	if failed {
		return errSomeFilesFailed
	}
	return nil
}

// errSomeFilesFailed reports a non-zero exit without printing again; each
// failure was already written to stderr as it happened.
var errSomeFilesFailed = silentErr{}

type silentErr struct{}

func (silentErr) Error() string { return "" }

func parseCell(s string) (int, int, error) {
	var w, h int
	if _, err := fmt.Sscanf(strings.ToLower(s), "%dx%d", &w, &h); err != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("bad --cell %q, want WxH such as 9x18", s)
	}
	return w, h, nil
}

func decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(bufio.NewReader(f))
	return img, err
}

type size struct{ cols, rows, cellW, cellH int }

// termSize reads the window size from the controlling tty. Under tmux the
// pixel fields are often zero, so cellW/cellH may come back unset.
func termSize() size {
	var ws struct{ rows, cols, x, y uint16 }
	tty := os.Stdout
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(),
		syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return size{cols: 80, rows: 24}
	}
	s := size{cols: int(ws.cols), rows: int(ws.rows)}
	if s.cols == 0 {
		s.cols = 80
	}
	if ws.x > 0 && ws.cols > 0 {
		s.cellW = int(ws.x) / int(ws.cols)
	}
	if ws.y > 0 && ws.rows > 0 {
		s.cellH = int(ws.y) / int(ws.rows)
	}
	return s
}
