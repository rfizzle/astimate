package mcpserver

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWithin(t *testing.T) {
	t.Parallel()

	dir := filepath.FromSlash("/w/d")
	tests := []struct {
		path string
		want bool
	}{
		{"/w/d", true},
		{"/w/d/p", true},
		{"/w/d/..p", true},
		{"/w", false},
		{"/w/dd", false},
		{"/w/e/p", false},
		{"/", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := within(dir, filepath.FromSlash(tt.path)); got != tt.want {
				t.Errorf("within(%q, %q) = %v, want %v", dir, tt.path, got, tt.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	t.Parallel()

	wd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(wd, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wd, "out")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		anyPath  bool
		want     string
		wantErr  error
		wantFail bool
	}{
		{name: "relative", path: "p", want: filepath.Join(wd, "p")},
		{name: "work dir", path: "", want: wd},
		{name: "absolute inside", path: filepath.Join(wd, "p"), want: filepath.Join(wd, "p")},
		{name: "parent", path: "..", wantErr: errOutsideWorkDir},
		{name: "absolute outside", path: outside, wantErr: errOutsideWorkDir},
		{name: "symlink out", path: "out", wantErr: errOutsideWorkDir},
		{name: "missing", path: "nope", wantFail: true},
		{name: "outside allowed", path: "out", anyPath: true, want: outside},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := Options{WorkDir: wd, AllowAnyPath: tt.anyPath}
			got, err := o.resolvePath(tt.path)
			switch {
			case tt.wantErr != nil || tt.wantFail:
				if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
					t.Errorf("resolvePath(%q) = (%q, %v), want error %v", tt.path, got, err, tt.wantErr)
				}
			case err != nil || got != tt.want:
				t.Errorf("resolvePath(%q) = (%q, %v), want %q", tt.path, got, err, tt.want)
			}
		})
	}
}
