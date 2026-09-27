package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunNoArgs(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	got := run(nil, &stderr)

	if got != exitUsage {
		t.Errorf("run(nil) exit code = %d, want %d", got, exitUsage)
	}
	if !strings.HasPrefix(stderr.String(), "usage: astimate") {
		t.Errorf("run(nil) stderr = %q, want usage text", stderr.String())
	}
}

// TestMainNoArgs runs the real main in a child process so the os.Exit path
// and the process exit code are exercised, not just run.
func TestMainNoArgs(t *testing.T) {
	t.Parallel()

	if os.Getenv("ASTIMATE_TEST_MAIN") == "1" {
		os.Args = []string{"astimate"}
		main()
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMainNoArgs$")
	cmd.Env = append(os.Environ(), "ASTIMATE_TEST_MAIN=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("main with no args: err = %v, want exit error", err)
	}
	if code := exitErr.ExitCode(); code != exitUsage {
		t.Errorf("main with no args exit code = %d, want %d", code, exitUsage)
	}
	if !strings.HasPrefix(stderr.String(), "usage: astimate") {
		t.Errorf("main with no args stderr = %q, want usage text", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("main with no args stdout = %q, want empty", stdout.String())
	}
}

func TestExitCodesMatchSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  int
		want int
	}{
		{name: "ok", got: exitOK, want: 0},
		{name: "usage", got: exitUsage, want: 1},
		{name: "analysis", got: exitAnalysis, want: 2},
		{name: "gate failed", got: exitGateFailed, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("exit code = %d, want %d", tt.got, tt.want)
			}
		})
	}
}
