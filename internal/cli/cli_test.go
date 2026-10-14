package cli

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestParseModuleArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantDir  string
		wantOK   bool
		wantFlag bool
		wantErr  string
	}{
		{"no root", nil, ".", true, false, ""},
		{"flag after root", []string{"mod", "--json"}, "mod", true, true, ""},
		{"flag before root", []string{"--json", "mod"}, "mod", true, true, ""},
		{"two roots", []string{"a", "b"}, "", false, false, "want at most one module root, got 2 arguments"},
		{"unknown flag", []string{"--nope"}, "", false, false, "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			fs := NewFlagSet("demo", "usage: astimate demo", &stderr)
			asJSON := fs.Bool("json", false, "json")
			dir, ok := ParseModuleArgs("demo", fs, tt.args, &stderr)
			if dir != tt.wantDir || ok != tt.wantOK || *asJSON != tt.wantFlag {
				t.Errorf("ParseModuleArgs(%q) = %q, %v with --json %v, want %q, %v with %v",
					tt.args, dir, ok, *asJSON, tt.wantDir, tt.wantOK, tt.wantFlag)
			}
			if tt.wantErr != "" && (!strings.Contains(stderr.String(), tt.wantErr) ||
				!strings.Contains(stderr.String(), "usage: astimate demo")) {
				t.Errorf("stderr = %q, want %q and the usage line", stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestParseInterspersed(t *testing.T) {
	t.Parallel()

	fs := NewFlagSet("demo", "usage", io.Discard)
	top := fs.Int("top", 0, "top")
	got, err := ParseInterspersed(fs, []string{"a", "--top", "2", "b"})
	if err != nil || !slices.Equal(got, []string{"a", "b"}) || *top != 2 {
		t.Errorf("ParseInterspersed = %q, %v with --top %d, want [a b] and 2", got, err, *top)
	}
}

func TestModuleRoot(t *testing.T) {
	t.Parallel()

	fs := NewFlagSet("demo", "usage", io.Discard)
	if dir, ok := ModuleRoot("demo", []string{"x"}, fs, io.Discard); dir != "x" || !ok {
		t.Errorf("ModuleRoot([x]) = %q, %v, want x, true", dir, ok)
	}
}

func TestNewLogger(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	l := NewLogger(&stderr)
	l.Debug("hidden")
	l.Info("shown", "k", "v")
	if got := stderr.String(); strings.Contains(got, "hidden") || !strings.Contains(got, "msg=shown k=v") {
		t.Errorf("log = %q, want the info record only", got)
	}
}

func TestWriteBuffered(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := WriteBuffered(&out, func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return errors.New("render failed")
	})
	if err == nil || out.Len() != 0 {
		t.Errorf("WriteBuffered = %v with %q written, want the error and nothing written", err, out.String())
	}
	if err := WriteBuffered(&out, func(w io.Writer) error {
		_, err := io.WriteString(w, "whole")
		return err
	}); err != nil || out.String() != "whole" {
		t.Errorf("WriteBuffered = %v with %q written, want whole", err, out.String())
	}
}
