package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"io"
	"strings"
)

// placeholder is the cell character that a kitty-protocol terminal replaces
// with a slice of an image. Row and column are appended as combining marks,
// and the image id travels in the cell's foreground colour.
const placeholder = '\U0010EEEE'

// ErrTooLarge means the image needs more cells than the diacritic table can
// address. 297 rows or columns is far beyond any real terminal.
type ErrTooLarge struct{ Rows, Cols int }

func (e ErrTooLarge) Error() string {
	return fmt.Sprintf("image needs %dx%d cells, only %d are addressable", e.Cols, e.Rows, len(diacritics))
}

// transmitPlaceholder uploads the pixels without displaying them (a=t, U=1),
// then writes a rectangle of placeholder cells. Unlike a direct a=T placement,
// the image occupies real cells in the multiplexer's grid, so tmux reserves
// the space, repaints it, and clears it when switching windows.
func transmitPlaceholder(w io.Writer, img image.Image, cols, rows int, escape func(string) string) error {
	if rows > len(diacritics) || cols > len(diacritics) {
		return ErrTooLarge{Rows: rows, Cols: cols}
	}

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

	// a=T with U=1 transmits and creates a *virtual* placement: nothing is
	// drawn at the cursor, but the placeholder cells below now have a
	// placement to reference. Without U=1 this would paint over the screen;
	// without a=T there would be no placement and the cells stay blank.
	keys := map[string]string{
		"a": "T",
		"U": "1",
		"f": "32",
		"o": "z",
		"s": fmt.Sprint(b.Dx()),
		"v": fmt.Sprint(b.Dy()),
		"i": fmt.Sprint(id),
		"c": fmt.Sprint(cols),
		"r": fmt.Sprint(rows),
		"q": "2",
	}
	if err := (cmd{keys: keys, payload: buf.Bytes()}).encode(w, escape); err != nil {
		return err
	}

	_, err := io.WriteString(w, placeholderGrid(id, cols, rows))
	return err
}

// placeholderGrid builds the block of cells. The id goes in the foreground
// colour: ids 1..255 fit the 256-colour form, which every terminal and
// multiplexer passes through untouched.
func placeholderGrid(id, cols, rows int) string {
	var s strings.Builder
	fg := fmt.Sprintf("\x1b[38;5;%dm", id)
	if id > 255 {
		fg = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xff, id>>8&0xff, id&0xff)
	}
	for r := 0; r < rows; r++ {
		s.WriteString(fg)
		for c := 0; c < cols; c++ {
			s.WriteRune(placeholder)
			s.WriteRune(diacritics[r])
			s.WriteRune(diacritics[c])
		}
		s.WriteString("\x1b[39m\n")
	}
	return s.String()
}

// cellRows converts an image's pixel size into a row count for a given column
// width, using the terminal's cell geometry.
func cellRows(img image.Image, cols, cellW, cellH int) int {
	b := img.Bounds()
	rows := (b.Dy() * cols * cellW) / (b.Dx() * cellH)
	return max(rows, 1)
}
