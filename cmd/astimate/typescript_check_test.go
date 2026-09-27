package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// newTSFixtureRepo commits a copy of the TypeScript fixture on master in a
// new repository and returns the repository's directory.
func newTSFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	copyTree(t, tsFixtureDir, repo)
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")
	return repo
}

// appendFile appends text to the file at name.
func appendFile(t *testing.T, name, text string) {
	t.Helper()
	f, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestTypeScriptCheckChanged runs check without --all on a TypeScript
// repository against its default ref, master: only the packages a change
// touches are checked, so a degraded package fails the gate and untouched
// ones are not reported, while a tsconfig change checks every package.
func TestTypeScriptCheckChanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	tests := []struct {
		name     string
		change   func(t *testing.T, repo string)
		wantExit int
		want     []string
	}{
		{
			name: "degraded package",
			change: func(t *testing.T, repo string) {
				appendFile(t, filepath.Join(repo, "tested", "tested.ts"),
					"\nexport function neverTested(n: number): number {\n  return n + 1;\n}\n")
			},
			wantExit: exitGateFailed,
			want:     []string{"tested"},
		},
		{
			name: "test-only change",
			change: func(t *testing.T, repo string) {
				appendFile(t, filepath.Join(repo, "tested", "__tests__", "count.test.ts"), "\n// edited\n")
			},
			wantExit: exitOK,
			want:     []string{"tested"},
		},
		{
			name: "root tsconfig",
			change: func(t *testing.T, repo string) {
				appendFile(t, filepath.Join(repo, "tsconfig.json"), "\n// edited\n")
			},
			wantExit: exitOK,
			want:     tsFixturePackages(),
		},
		{
			name:     "no change",
			change:   func(*testing.T, string) {},
			wantExit: exitOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := newTSFixtureRepo(t)
			tt.change(t, repo)

			var stdout, stderr bytes.Buffer
			args := []string{"check", repo, "--format", formatJSON}
			if got := run(args, &stdout, &stderr); got != tt.wantExit {
				t.Fatalf("run(%q) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					args, got, tt.wantExit, stdout.String(), stderr.String())
			}
			reports := decodeReports(t, stdout.Bytes())
			paths := make([]string, 0, len(reports))
			for i := range reports {
				paths = append(paths, reports[i].PackagePath)
			}
			if slices.Sort(paths); !slices.Equal(paths, tt.want) {
				t.Errorf("checked packages = %q, want %q", paths, tt.want)
			}
			if tt.wantExit == exitGateFailed {
				if got := violationMetrics(reports, "tested"); !slices.Contains(got, "untested_exports") {
					t.Errorf("tested violations = %q, want untested_exports", got)
				}
			}
		})
	}
}
