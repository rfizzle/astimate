package main

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMakeBuildInjectsMetadata builds the binary with the same -ldflags the
// Makefile build target computes, from the same git and date commands, and
// checks that `version` reports the injected values rather than a fallback.
func TestMakeBuildInjectsMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped under -short")
	}
	for _, tool := range []string{"make", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}

	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), name, args...).Output()
		if err != nil {
			t.Skipf("%s %s: %v (not a git checkout?)", name, strings.Join(args, " "), err)
		}
		return strings.TrimSpace(string(out))
	}
	version := run("git", "describe", "--tags", "--always", "--dirty")
	commit := run("git", "rev-parse", "HEAD")
	date := run("date", "-u", "+%Y-%m-%dT%H:%M:%SZ")

	bin := filepath.Join(t.TempDir(), "astimate")
	ldflags := "-X main.buildVersion=" + version +
		" -X main.buildCommit=" + commit +
		" -X main.buildDate=" + date
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", ldflags, "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	out, err := exec.CommandContext(t.Context(), bin, "version").Output()
	if err != nil {
		t.Fatalf("astimate version: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("malformed version line %q", line)
		}
		got[key] = value
	}

	for _, key := range []string{"version", "commit", "date"} {
		if v := got[key]; v == "" || v == unknownBuildValue || v == "(devel)" {
			t.Errorf("%s = %q, want an injected value", key, v)
		}
	}
	if got["version"] != version {
		t.Errorf("version = %q, want %q", got["version"], version)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(got["commit"]) {
		t.Errorf("commit = %q, want 40 hex characters", got["commit"])
	}
	if got["date"] != date {
		t.Errorf("date = %q, want %q", got["date"], date)
	}
}
