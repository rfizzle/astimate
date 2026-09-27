package baseline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoDir(t *testing.T) {
	r := newRepo(t, "master")
	sub := filepath.Join(r.dir, "services", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		root string
		want string
	}{
		{name: "top level", root: r.dir, want: ""},
		{name: "below the top level", root: sub, want: "services/api"},
		{name: "outside git", root: t.TempDir(), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepoDir(t.Context(), tt.root); got != tt.want {
				t.Errorf("RepoDir(%s) = %q, want %q", tt.root, got, tt.want)
			}
		})
	}
}
