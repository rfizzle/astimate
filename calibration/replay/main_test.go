package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gocache"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		ok   bool
	}{
		{name: "defaults", ok: true},
		{name: "extra argument", args: []string{"x"}},
		{name: "zero parallel", args: []string{"--parallel", "0"}},
		{name: "option as range", args: []string{"--range", "--all"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFlags(tt.args, &bytes.Buffer{})
			if (err == nil) != tt.ok {
				t.Errorf("err %v, want ok %v", err, tt.ok)
			}
		})
	}
}

// TestReplayIsolatesGoCache checks that a replay's go commands build into a
// temporary directory of the replay's own, not the shared GOCACHE, and that
// cleanup removes it and restores the environment.
func TestReplayIsolatesGoCache(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("GOCACHE", shared)
	t.Setenv("GOTMPDIR", "")
	cleanup, err := gocache.Isolate("astimate-replay-gocache-")
	if err != nil {
		t.Fatal(err)
	}
	cache := os.Getenv("GOCACHE")
	dir := filepath.Dir(cache)
	if cache == shared || filepath.Base(cache) != "gocache" || !strings.HasPrefix(filepath.Base(dir), "astimate-replay-gocache-") {
		t.Errorf("GOCACHE = %q, want gocache under a new astimate-replay-gocache-* directory", cache)
	}
	if got := os.Getenv("GOTMPDIR"); got != filepath.Join(dir, "gotmp") {
		t.Errorf("GOTMPDIR = %q, want it beside the cache", got)
	}
	cleanup()
	if got := os.Getenv("GOCACHE"); got != shared {
		t.Errorf("GOCACHE after cleanup = %q, want %q", got, shared)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("replay's build cache %s left behind: %v", dir, err)
	}
}

// TestRunWritesData checks the command end to end on a one-commit module:
// it replays into --out and writes the three files of the data format
// calibration/replay/label and calibration/validate read.
func TestRunWritesData(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	repo := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/fix\n\ngo 1.22\n")
	write("a/a.go", "package a\n\nfunc one() int { return 1 }\n")
	for _, args := range [][]string{
		{"init", "-q", "-b", "master"},
		{"add", "-A"},
		{"-c", "user.name=Ann", "-c", "user.email=ann@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "feat: add a"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", args[0], err, out)
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"--repo", repo, "--out", out}, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, name := range []string{"commits.jsonl", "packages.jsonl", "run.json"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || len(data) == 0 {
			t.Errorf("%s: %v, %d bytes", name, err, len(data))
		}
	}
}
