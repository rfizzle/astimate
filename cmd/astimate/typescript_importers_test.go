package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestTypeScriptCheckSelectsImporters runs check without --all after a new
// declaration file in hub turns a's import of "hub/types", resolved under
// baseUrl, from an external import into an internal one: no file of a
// changed, but its internal_imports moved, so a is checked beside hub and
// the log says why, while b, which imports nothing, is not.
func TestTypeScriptCheckSelectsImporters(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	repo := t.TempDir()
	for name, text := range map[string]string{
		"package.json":  "{}\n",
		"tsconfig.json": "{\"compilerOptions\": {\"baseUrl\": \".\"}}\n",
		"hub/hub.ts":    "export const h = 1;\n",
		"a/a.ts":        "import type { T } from \"hub/types\";\n\nexport const a: T = 1;\n",
		"b/b.ts":        "export const b = 1;\n",
	} {
		writeRepoFile(t, repo, name, text)
	}
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "base")
	writeRepoFile(t, repo, "hub/types.d.ts", "export type T = number;\n")

	var stdout, stderr bytes.Buffer
	args := []string{"check", repo, "--format", formatJSON}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
			args, got, exitOK, stdout.String(), stderr.String())
	}
	reports := decodeReports(t, stdout.Bytes())
	paths := make([]string, 0, len(reports))
	for i := range reports {
		paths = append(paths, reports[i].PackagePath)
	}
	if slices.Sort(paths); !slices.Equal(paths, []string{"a", "hub"}) {
		t.Fatalf("checked packages = %q, want a and hub", paths)
	}
	for i := range reports {
		r := &reports[i]
		if r.PackagePath != "a" {
			continue
		}
		if r.Baseline == nil {
			t.Fatal("a has no baseline block")
		}
		if got, was := r.Metrics.InternalImports, r.Baseline.Metrics.InternalImports; got != 1 || was != 0 {
			t.Errorf("a internal_imports = %d, baseline %d; want 1 and 0", got, was)
		}
		if got, was := r.Metrics.ExternalImports, r.Baseline.Metrics.ExternalImports; got != 0 || was != 1 {
			t.Errorf("a external_imports = %d, baseline %d; want 0 and 1", got, was)
		}
	}
	if want := "package=a importer_of=hub"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want a line saying a was selected as an importer of hub (%q)", stderr.String(), want)
	}
}

// writeRepoFile writes text to the slash path name under repo, creating
// its directory.
func writeRepoFile(t *testing.T, repo, name, text string) {
	t.Helper()
	p := filepath.Join(repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
