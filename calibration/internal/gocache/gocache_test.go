package gocache

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEnv(t *testing.T) {
	dir := t.TempDir()
	env, err := Env(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"GOCACHE=" + filepath.Join(dir, "gocache"), "GOTMPDIR=" + filepath.Join(dir, "gotmp")}
	if !slices.Equal(env, want) {
		t.Fatalf("Env = %v, want %v", env, want)
	}
	for _, d := range []string{"gocache", "gotmp"} {
		if fi, err := os.Stat(filepath.Join(dir, d)); err != nil || !fi.IsDir() {
			t.Errorf("%s not created: %v", d, err)
		}
	}
}

// TestEnvOverridesEarlierEntries checks that the go command sees the
// directories Env returns even when an earlier entry set GOCACHE.
func TestEnvOverridesEarlierEntries(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not found")
	}
	dir := t.TempDir()
	env, err := Env(dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "env", "GOCACHE", "GOTMPDIR")
	cmd.Env = append(append(os.Environ(), "GOCACHE="+t.TempDir()), env...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(out))
	want := []string{filepath.Join(dir, "gocache"), filepath.Join(dir, "gotmp")}
	if !slices.Equal(got, want) {
		t.Errorf("go env = %v, want %v", got, want)
	}
}

func TestSetenv(t *testing.T) {
	tests := []struct {
		name string
		prev map[string]string
	}{
		{"unset before", nil},
		{"set before", map[string]string{"GOCACHE": "/prev/cache", "GOTMPDIR": "/prev/tmp"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.TempDir honours GOTMPDIR, so take it before changing that.
			dir := t.TempDir()
			for _, k := range []string{"GOCACHE", "GOTMPDIR"} {
				t.Setenv(k, "") // registers the original value for cleanup
				if v, ok := tt.prev[k]; ok {
					t.Setenv(k, v)
				} else if err := os.Unsetenv(k); err != nil {
					t.Fatal(err)
				}
			}
			restore, err := Setenv(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := os.Getenv("GOCACHE"); got != filepath.Join(dir, "gocache") {
				t.Errorf("GOCACHE = %q", got)
			}
			if got := os.Getenv("GOTMPDIR"); got != filepath.Join(dir, "gotmp") {
				t.Errorf("GOTMPDIR = %q", got)
			}
			restore()
			for _, k := range []string{"GOCACHE", "GOTMPDIR"} {
				v, ok := os.LookupEnv(k)
				if want, wantOK := tt.prev[k]; v != want || ok != wantOK {
					t.Errorf("after restore %s = %q (set %v), want %q (set %v)", k, v, ok, want, wantOK)
				}
			}
		})
	}
}

func TestSetenvFailsOnUncreatableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Setenv(file); err == nil {
		t.Error("Setenv under a regular file succeeded")
	}
	if _, err := Env(file); err == nil {
		t.Error("Env under a regular file succeeded")
	}
}
