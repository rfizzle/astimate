package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// skippedDir is a module whose package remote imports a module go.mod
// does not require, so it does not load; base and app load.
const skippedDir = "../../testdata/go/skipped"

// skippedRepo copies skippedDir into a new git repository committed on
// master, appends a comment to file (a module-relative path) in the
// working tree, a change that selects its package and moves no metric,
// and returns the module root.
func skippedRepo(t *testing.T, file string) string {
	t.Helper()
	root := t.TempDir()
	copyTree(t, skippedDir, root)
	gitIn(t, root, "init", "-q", "-b", "master")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "--no-verify", "-m", "fixture")
	path := filepath.Join(root, file)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(src, "\n// A comment the change adds.\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestCheckSkippedPackage checks the exit rule for a package that does not
// load: a check that changed another package gates it and exits 0, listing
// the skipped package with its reason in JSON, text and on stderr; a check
// that changed the skipped package exits 2 and names it.
func TestCheckSkippedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages and runs git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	for _, tc := range []struct {
		name     string
		file     string
		format   string
		wantCode int
		// wantOut and wantErr must each appear in stdout and stderr.
		wantOut, wantErr []string
	}{
		{
			name: "another package changed, json", file: "app/app.go", format: formatJSON, wantCode: exitOK,
			wantOut: []string{`"package_path": "app"`, `"package": "remote"`, `"reason": "missing module"`},
			wantErr: []string{"skipped package", "package=example.com/skipped/remote"},
		},
		{
			name: "another package changed, text", file: "app/app.go", format: formatText, wantCode: exitOK,
			wantOut: []string{"app: ", "remote skipped, it does not load (missing module)"},
		},
		{
			name: "skipped package changed", file: "remote/remote.go", format: formatText, wantCode: exitAnalysis,
			wantOut: []string{"remote skipped", "verdict is incomplete"},
			wantErr: []string{"checking package failed", "path=remote", "skipped, it does not load"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := skippedRepo(t, tc.file)
			var stdout, stderr bytes.Buffer
			got := runCheck([]string{root, "--base", "master", "--format", tc.format}, &stdout, &stderr)
			if got != tc.wantCode {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, tc.wantCode, stdout.String(), stderr.String())
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
				}
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr.String())
				}
			}
			if tc.format == formatJSON && !json.Valid(stdout.Bytes()) {
				t.Errorf("stdout is not JSON:\n%s", stdout.String())
			}
		})
	}
}

// TestRankSkippedPackage checks that rank ranks the packages that load,
// exits 0 and names the skipped one on stderr.
func TestRankSkippedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if got := runRank([]string{skippedDir}, &stdout, &stderr); got != exitOK {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", got, exitOK, stderr.String())
	}
	if out := stdout.String(); !strings.Contains(out, "base") || !strings.Contains(out, "app") || strings.Contains(out, "remote") {
		t.Errorf("rank output does not list base and app alone:\n%s", out)
	}
	if !strings.Contains(stderr.String(), "package=example.com/skipped/remote") {
		t.Errorf("stderr does not name the skipped package:\n%s", stderr.String())
	}
}
