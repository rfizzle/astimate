package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDebugFlagShowsSkippedPackages checks that a module with a package its
// build constraints exclude reports quietly by default, and that --debug
// adds the skipped-package line.
func TestDebugFlagShowsSkippedPackages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{
		"go.mod":         "module example.com/tagged\n\ngo 1.27\n",
		"pure/pure.go":   "// Package pure is plain Go.\npackage pure\n\n// Double returns 2x.\nfunc Double(x int) int { return 2 * x }\n",
		"gated/gated.go": "//go:build ignore\n\npackage main\n",
	}
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"default", []string{"report", root, "--format", formatJSON}, ""},
		{"debug", []string{"report", root, "--format", formatJSON, "--debug"}, "package=example.com/tagged/gated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != exitOK {
				t.Fatalf("run(%q) = %d, want %d\nstderr:\n%s", tt.args, got, exitOK, stderr.String())
			}
			if tt.want == "" && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want it empty", stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want it to hold %q", stderr.String(), tt.want)
			}
		})
	}
}
