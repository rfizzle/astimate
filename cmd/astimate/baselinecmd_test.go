package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
)

// writeModule creates a two-package Go module in a new temporary directory
// and returns its root.
func writeModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":  "module example.com/m\n\ngo 1.27\n",
		"a/a.go":  "package a\n\nvar A = 1\n",
		"b/b.go":  "package b\n\nfunc B() int { return 2 }\n",
		"b/b2.go": "package b\n\nfunc C() int { return 3 }\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return root
}

func TestBaselineWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tests := []struct {
		name string
		args func(root string) []string
		want func(root string) string
	}{
		{
			name: "default out under module root",
			args: func(root string) []string { return []string{filepath.Join(root, "a")} },
			want: func(root string) string { return filepath.Join(root, ".astimate", "baseline.json") },
		},
		{
			name: "out flag after the module root",
			args: func(root string) []string {
				return []string{root, "--out", filepath.Join(root, "custom", "base.json")}
			},
			want: func(root string) string { return filepath.Join(root, "custom", "base.json") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := writeModule(t)
			var stdout, stderr bytes.Buffer
			args := append([]string{"baseline", "write"}, tt.args(root)...)
			if got := run(args, &stdout, &stderr); got != exitOK {
				t.Fatalf("exit code = %d, want %d; stderr = %q", got, exitOK, stderr.String())
			}
			path := tt.want(root)
			if want := "wrote " + path + " (2 packages)\n"; stdout.String() != want {
				t.Errorf("stdout = %q, want %q", stdout.String(), want)
			}

			b, err := baseline.FromFile(path)
			if err != nil {
				t.Fatalf("FromFile: %v", err)
			}
			a, ok := b.Metrics("example.com/m/a")
			if !ok || a.Globals != 1 {
				t.Errorf("example.com/m/a = %+v (found %v), want globals 1", a, ok)
			}
			if bm, ok := b.Metrics("example.com/m/b"); !ok || bm.Files != 2 {
				t.Errorf("example.com/m/b = %+v (found %v), want 2 files", bm, ok)
			}
			if b.Tokenizer() != tokenizerEst {
				t.Errorf("Tokenizer() = %q, want the default %q", b.Tokenizer(), tokenizerEst)
			}
		})
	}
}

func TestBaselineWriteErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     func(t *testing.T) []string
		wantCode int
		want     string
	}{
		{
			name:     "two positional arguments",
			args:     func(*testing.T) []string { return []string{"one", "two"} },
			wantCode: exitUsage,
			want:     "want at most one module root, got 2 arguments",
		},
		{
			name:     "unknown tokenizer",
			args:     func(*testing.T) []string { return []string{"--tokenizer", "bogus"} },
			wantCode: exitUsage,
			want:     `unknown tokenizer "bogus"`,
		},
		{
			name:     "unknown flag",
			args:     func(*testing.T) []string { return []string{"--nope"} },
			wantCode: exitUsage,
			want:     "flag provided but not defined",
		},
		{
			name:     "not a module",
			args:     func(t *testing.T) []string { return []string{t.TempDir()} },
			wantCode: exitAnalysis,
			want:     "no go.mod found",
		},
		{
			name: "missing config",
			args: func(t *testing.T) []string {
				return []string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}
			},
			wantCode: exitAnalysis,
			want:     "reading config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			args := append([]string{"baseline", "write"}, tt.args(t)...)
			if got := run(args, &stdout, &stderr); got != tt.wantCode {
				t.Errorf("exit code = %d, want %d", got, tt.wantCode)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}
