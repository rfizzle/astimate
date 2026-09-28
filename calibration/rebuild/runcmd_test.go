package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
)

// TestCallsClaude checks that callsClaude recognizes every word of a
// template whose base name is claude, and only that.
func TestCallsClaude(t *testing.T) {
	for tmpl, want := range map[string]bool{
		agent.DefaultTemplate:                 true,
		"/usr/local/bin/claude -p x":          true,
		`sh -c "claude -p x"`:                 true,
		"sh /repo/dryrun-agent.sh {dir}":      false,
		"echo claude-code {dir}":              false,
		"sh ./calibration/rebuild/claude.txt": false,
	} {
		if got := callsClaude(tmpl); got != want {
			t.Errorf("callsClaude(%q) = %v, want %v", tmpl, got, want)
		}
	}
}

// TestRunNeedsLive checks that the run command never starts Claude Code
// without --live: a claude on PATH that records its calls is never
// called, and nothing is written.
func TestRunNeedsLive(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	fake := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"default agent", nil, exitUsage},
		{"one experiment", []string{"--only", "google.golang.org/grpc/resolver/manual"}, exitUsage},
		{"template calling claude", []string{"--agent", "claude -p x"}, exitUsage},
		{"plan", []string{"--plan"}, exitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			var stdout, stderr bytes.Buffer
			args := append([]string{"run", "--definition", "rebuild.yaml", "--out", out}, tt.args...)
			if code := run(context.Background(), args, &stdout, &stderr); code != tt.code {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tt.code, stderr.String())
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("claude was started")
			}
			if _, err := os.Stat(out); err == nil {
				t.Fatal("the output directory was created")
			}
			if tt.code == exitUsage && !strings.Contains(stderr.String(), "--live") {
				t.Errorf("refusal does not say how to run live:\n%s", stderr.String())
			}
			if want := fmt.Sprintf("%d runs pending", 3*shippedExperiments(t)); tt.code == exitOK && !strings.Contains(stdout.String(), want) {
				t.Errorf("plan output:\n%s", stdout.String())
			}
		})
	}
}

// shippedExperiments returns the number of experiments in rebuild.yaml.
func shippedExperiments(t *testing.T) int {
	t.Helper()
	d, err := definition.LoadDefinition("rebuild.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return len(d.Experiments)
}
