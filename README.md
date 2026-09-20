# kimg

Show images in the terminal, using the [kitty graphics protocol](https://sw.kovidgoyal.net/kitty/graphics-protocol/). Works in Ghostty and kitty, and unlike most of the alternatives it behaves properly inside tmux.

```console
$ kimg photo.jpg
$ kimg -c 40 diagram.png
$ kimg *.png
```

## Why

Most terminal image viewers place an image at the cursor with `a=T` and are done. Inside a multiplexer that falls apart: tmux does not understand the protocol, so it reserves no cells for the image. The picture lands wherever the *outer* terminal's cursor happens to be, overlaps your prompt, survives a window switch, and does not scroll.

`kimg` draws images as **Unicode placeholder cells** instead. The pixels are uploaded with a virtual placement (`a=T,U=1`), which paints nothing, and then a rectangle of `U+10EEEE` characters is printed, each carrying its row and column as combining marks and the image id in its foreground colour. The terminal substitutes an image tile for each cell.

To tmux those are ordinary characters. It reserves the lines, repaints them on redraw, clears them on a window switch, and scrolls them into its history like any other text.

## Install

```console
$ go install github.com/unlomtrois/kimg@latest
```

This puts `kimg` in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure that is on your `PATH`.

Or build from a clone:

```console
$ git clone https://github.com/unlomtrois/kimg
$ cd kimg
$ go build -o kimg .
```

Requires Go 1.26. The only dependency is cobra.

### Without Go installed

The repository builds itself in a container, so nothing but Docker or Podman is needed:

```console
$ docker build --output type=local,dest=. .
$ install -m 755 kimg ~/.local/bin/
```

The build context carries no `.git`, so pass `--build-arg VERSION=v0.1.0` if you want `kimg --version` to report something other than `devel`.

The final stage is empty, so no image is added to your image store; `--output type=local` just drops the binary next to you. Podman finds the `Dockerfile` without `-f` as well.

Cross building needs no extra toolchain, since CGO is off:

```console
$ docker build --platform linux/arm64 --output type=local,dest=. .
```

To run the tests in the container instead:

```console
$ docker build --target test .
```

## Usage

```
kimg [flags] FILE...

  -c, --cols int      width in terminal cells (0 fits the terminal, capped at the image's own size)
  -r, --rows int      height in terminal cells (0 derives it from the aspect ratio)
      --cell string   cell size in pixels as WxH, for terminals that will not report it
      --direct        place directly (a=T) instead of using Unicode placeholders
      --no-tmux       do not use tmux passthrough even when $TMUX is set
```

PNG, JPEG and GIF are decoded through Go's standard `image` package. Everything is re-encoded to zlib-compressed RGBA (`f=32,o=z`), so every format takes the same path.

With no `-c`, the image is fitted to the window width but never enlarged past its own pixel size. Give `-c` alone and the height follows the aspect ratio.

### tmux

tmux must be told to forward the escape sequences:

```tmux
set -g allow-passthrough on
set -as terminal-features ',xterm-ghostty:RGB'
```

`kimg` detects `$TMUX` and wraps its sequences in the passthrough DCS automatically.

**tmux 3.4 reports no pixel dimensions at all**, so there is no way to learn the cell size and row counts fall back to an assumed 9×18 cell. If images look squashed or stretched, pass your real cell size:

```console
$ kimg --cell 10x20 photo.jpg
```

Newer tmux handles the protocol better; 3.7 is worth the upgrade.

## Notes and limits

Image ids are an FNV-1a hash of the decoded pixels and the display size, masked to 24 bits so they fit the truecolour foreground that placeholder cells carry. Showing the same file at the same size twice reuses the terminal's existing copy; different images always get different ids, so a new image cannot overwrite one still on your screen. Collisions are possible in principle, after a few thousand distinct images in one session.

The pixels live in the terminal, not in the scrollback. Scroll back far enough, past enough newer images, and the terminal evicts the old ones, leaving those cells blank.

A grid is limited to 297 rows or columns, the size of the diacritic table, which no real terminal reaches.

Placeholder cells copy out of the terminal as `U+10EEEE` and combining marks, not as anything meaningful.

`--direct` is the simple `a=T` path, for use outside a multiplexer or when placeholders misbehave. It is not tracked by tmux.

To drop every image the terminal is holding, including ones in your scrollback:

```console
$ printf '\033_Ga=d,d=A\033\\'                                 # without tmux
$ printf '\033Ptmux;\033\033_Ga=d,d=A\033\033\\\033\\'         # inside tmux
```

## Layout

| file | |
| --- | --- |
| `main.go` | cobra CLI, terminal geometry, the per-file loop |
| `kitty.go` | protocol encoding: APC framing, chunking, image ids, tmux passthrough |
| `placeholder.go` | virtual placements and the placeholder grid |
| `diacritics.go` | generated from kitty's `rowcolumn-diacritics.txt`; do not edit |
| `Dockerfile` | builds the binary without Go on the host |

## Acknowledgements

The protocol and the row/column diacritic table are Kovid Goyal's work, from [kitty](https://github.com/kovidgoyal/kitty).
