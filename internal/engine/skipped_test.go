package engine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skippedFixture is a module whose package remote imports a module go.mod
// does not require, so it does not load.
const skippedFixture = "../../testdata/go/skipped"

// skippedRepo copies skippedFixture into a new git repository, commits it
// on master, then applies edits (path to content) to the working tree, and
// returns the module root.
func skippedRepo(t *testing.T, edits map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files := make(map[string]string)
	err := filepath.WalkDir(skippedFixture, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skippedFixture, path)
		files[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, files)
	gitIn(t, root, "init", "-q", "-b", "master")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "fixture")
	writeFiles(t, root, edits)
	return root
}

// TestCheckSkipped checks the exit rule's input: a package that does not
// load is listed in every check, and fails one only when the check selects
// it; the other packages are gated either way.
func TestCheckSkipped(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	for _, tc := range []struct {
		name         string
		edit         string
		all          bool
		wantPackages []string
		wantSelected bool
	}{
		{name: "another package changed", edit: "app/extra.go", wantPackages: []string{"app"}},
		{name: "skipped package changed", edit: "remote/extra.go", wantSelected: true},
		{name: "all packages", all: true, wantPackages: []string{"app", "base"}},
		{name: "all packages, another changed", edit: "app/extra.go", all: true, wantPackages: []string{"app", "base"}},
		{name: "all packages, skipped changed", edit: "remote/extra.go", all: true, wantPackages: []string{"app", "base"}, wantSelected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			edits := map[string]string{}
			if tc.edit != "" {
				pkg := filepath.Base(filepath.Dir(tc.edit))
				edits[tc.edit] = "package " + pkg + "\n\n// Extra returns two.\nfunc Extra() int { return 2 }\n"
			}
			tg, err := LoadTarget(skippedRepo(t, edits), TargetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			c, failed, err := Check(t.Context(), tg, CheckOptions{Base: "master", All: tc.all})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			var got []string
			for i := range c.Packages {
				got = append(got, c.Packages[i].Report.PackagePath)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantPackages, ",") {
				t.Errorf("checked %v, want %v", got, tc.wantPackages)
			}
			if len(c.Skipped) != 1 {
				t.Fatalf("skipped = %+v, want remote alone", c.Skipped)
			}
			s := c.Skipped[0]
			if s.Package != "remote" || s.Reason != "missing module" || s.Selected != tc.wantSelected ||
				!strings.Contains(s.Error, "example.com/absent/client") {
				t.Errorf("skipped = %+v, want remote, missing module, selected %v", s, tc.wantSelected)
			}
			if strings.Contains(s.Error, tg.Mod.Root) {
				t.Errorf("skipped error names the module root: %s", s.Error)
			}
			assertSkippedFailure(t, failed, tc.wantSelected)
		})
	}
}

// assertSkippedFailure checks that failed holds one error, IsSkipped and
// naming remote, when want is set, and none otherwise.
func assertSkippedFailure(t *testing.T, failed []error, want bool) {
	t.Helper()
	if !want {
		if len(failed) != 0 {
			t.Errorf("failed = %v, want none", failed)
		}
		return
	}
	var pe *PackageError
	if len(failed) != 1 || !IsSkipped(failed[0]) || !errors.As(failed[0], &pe) || pe.Path != "remote" {
		t.Errorf("failed = %v, want remote, skipped", failed)
	}
}

// TestSkippedKeepsFanIn checks that a package importing a skipped one
// keeps the edge: base, imported by app and by the skipped remote, has a
// fan_in of 2.
func TestSkippedKeepsFanIn(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tg, err := LoadTarget(filepath.Join(skippedRepo(t, nil), "base"), TargetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Assess(t.Context(), tg, AssessOptions{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if r.Metrics.FanIn != 2 {
		t.Errorf("base fan_in = %d, want 2 (app and the skipped remote)", r.Metrics.FanIn)
	}
}
