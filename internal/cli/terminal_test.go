package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestLayout checks the width and colour of every mode on a terminal and
// off one, NO_COLOR included: colour under auto only on a terminal and
// without NO_COLOR, always and never as they say, and the terminal's
// width only on a terminal whose size is known.
func TestLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tty       bool
		cols      int
		mode      string
		noColor   string
		wantWidth int
		wantColor bool
	}{
		{"terminal", true, 132, ColorAuto, "", 132, true},
		{"terminal, default mode", true, 132, "", "", 132, true},
		{"terminal under NO_COLOR", true, 132, ColorAuto, "1", 132, false},
		{"terminal of unknown size", true, 0, ColorAuto, "", DefaultWidth, true},
		{"pipe", false, 0, ColorAuto, "", DefaultWidth, false},
		{"pipe, always", false, 0, ColorAlways, "", DefaultWidth, true},
		{"always under NO_COLOR", true, 80, ColorAlways, "1", 80, true},
		{"terminal, never", true, 80, ColorNever, "", 80, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			width, color, err := layout(tt.tty, tt.cols, tt.mode, tt.noColor)
			if err != nil || width != tt.wantWidth || color != tt.wantColor {
				t.Errorf("layout = %d, %v, %v; want %d, %v", width, color, err, tt.wantWidth, tt.wantColor)
			}
		})
	}
	if _, _, err := layout(true, 80, "sometimes", ""); err == nil || !strings.Contains(err.Error(), `"sometimes"`) {
		t.Errorf("layout(sometimes) error = %v, want one naming the mode", err)
	}
}

// TestTerminal checks that a writer that is no file lays out at the
// default width without colour.
func TestTerminal(t *testing.T) {
	t.Parallel()

	width, color, err := Terminal(&bytes.Buffer{}, ColorAuto)
	if err != nil || width != DefaultWidth || color {
		t.Errorf("Terminal(buffer) = %d, %v, %v; want %d, false", width, color, err, DefaultWidth)
	}
}
