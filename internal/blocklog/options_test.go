package blocklog

import (
	"path/filepath"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

func TestOptionsPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "blocks.jsonl")
	with := func(p string) *config.Config {
		c := &config.Config{}
		c.Hook.BlockLog = p
		return c
	}
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{name: "no configuration", cfg: nil, want: ""},
		{name: "log off", cfg: with(""), want: ""},
		{name: "relative to the module root", cfg: with(".astimate/blocks.jsonl"), want: filepath.Join(root, ".astimate", "blocks.jsonl")},
		{name: "absolute path kept", cfg: with(filepath.ToSlash(abs)), want: abs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := &Options{Root: root, Config: tt.cfg}
			if got := o.Path(); got != tt.want {
				t.Errorf("Path() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineKey(t *testing.T) {
	t.Parallel()

	l := &Line{Event: EventBlock, Module: "example.com/m", Package: "internal/x", Metric: "sloc"}
	want := Key{Module: "example.com/m", Package: "internal/x", Metric: "sloc"}
	if got := l.Key(); got != want {
		t.Errorf("Key() = %+v, want %+v", got, want)
	}
}
