package cli

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// Colour modes the --color flag accepts.
const (
	// ColorAuto colours output on a terminal unless NO_COLOR is set.
	ColorAuto = "auto"
	// ColorAlways colours output wherever it goes.
	ColorAlways = "always"
	// ColorNever never colours output.
	ColorNever = "never"
)

// DefaultWidth is the width text reports are laid out at when stdout is
// not a terminal.
const DefaultWidth = 100

// Terminal returns the width and colour a text report written to w is
// laid out with: the terminal's width when w is a terminal, else
// DefaultWidth, and colour as mode says, under ColorAuto only on a
// terminal and when NO_COLOR is unset or empty (https://no-color.org).
// An unknown mode is an error naming the flag.
func Terminal(w io.Writer, mode string) (width int, color bool, err error) {
	tty, cols := false, 0
	if f, ok := w.(*os.File); ok {
		fd := int(f.Fd())
		if tty = term.IsTerminal(fd); tty {
			cols, _, _ = term.GetSize(fd)
		}
	}
	return layout(tty, cols, mode, os.Getenv("NO_COLOR"))
}

// layout is Terminal for a writer that is a terminal of cols columns when
// tty is set (0 when its size is unknown), with noColor the value of
// NO_COLOR.
func layout(tty bool, cols int, mode, noColor string) (width int, color bool, err error) {
	width = DefaultWidth
	if tty && cols > 0 {
		width = cols
	}
	switch mode {
	case ColorAuto, "":
		return width, tty && noColor == "", nil
	case ColorAlways:
		return width, true, nil
	case ColorNever:
		return width, false, nil
	}
	return 0, false, fmt.Errorf("--color must be %s, %s or %s, got %q", ColorAuto, ColorAlways, ColorNever, mode)
}
