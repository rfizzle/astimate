package main

import (
	"bytes"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
)

// TestTypeScriptCheckGitHubPositions runs check --format github on a
// TypeScript repository whose tested package gained a global and an
// untested export: each annotation lands on the declaration's file and
// line, not on the package directory.
func TestTypeScriptCheckGitHubPositions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	repo := newTSFixtureRepo(t)
	// tested.ts has 20 lines, so the appended global is on line 22 and the
	// function on line 24.
	appendFile(t, filepath.Join(repo, "tested", "tested.ts"),
		"\nexport let hits = 0;\n\nexport function neverTested(n: number): number {\n  return n + hits;\n}\n")

	var stdout, stderr bytes.Buffer
	args := []string{"check", repo, "--format", formatGitHub}
	if got := run(args, &stdout, &stderr); got != exitGateFailed {
		t.Fatalf("run(%q) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
			args, got, exitGateFailed, stdout.String(), stderr.String())
	}
	out := stdout.String()
	abs, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	file := path.Join(baseline.RepoDir(t.Context(), abs), "tested", "tested.ts")
	for m, line := range map[string]string{"globals": "22", "untested_exports": "24"} {
		want := "::error file=" + file + ",line=" + line + "::" + m + ": "
		if !strings.Contains(out, want) {
			t.Errorf("github output lacks %q:\n%s", want, out)
		}
	}
}
