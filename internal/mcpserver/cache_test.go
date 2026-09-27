package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/report"
)

func TestStampTree(t *testing.T) {
	t.Parallel()

	later := time.Now().Add(time.Hour)
	tests := []struct {
		name        string
		edit        func(t *testing.T, root string)
		wantChanged bool
	}{
		{name: "unchanged", edit: func(*testing.T, string) {}},
		{name: "source touched", wantChanged: true, edit: func(t *testing.T, root string) {
			chtimes(t, filepath.Join(root, "p", "p.go"), later)
		}},
		{name: "go.mod touched", wantChanged: true, edit: func(t *testing.T, root string) {
			chtimes(t, filepath.Join(root, "go.mod"), later)
		}},
		{name: "file added", wantChanged: true, edit: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "p", "q.go"), "package p\n")
		}},
		{name: "file removed", wantChanged: true, edit: func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "p", "p.go")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "non-source file touched", edit: func(t *testing.T, root string) {
			chtimes(t, filepath.Join(root, "p", "notes.txt"), later)
		}},
		{name: "testdata touched", edit: func(t *testing.T, root string) {
			chtimes(t, filepath.Join(root, "p", "testdata", "x.go"), later)
		}},
		{name: "hidden dir touched", edit: func(t *testing.T, root string) {
			chtimes(t, filepath.Join(root, ".git", "x.go"), later)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for _, name := range []string{"go.mod", "p/p.go", "p/notes.txt", "p/testdata/x.go", ".git/x.go"} {
				writeFile(t, filepath.Join(root, filepath.FromSlash(name)), "package p\n")
			}
			// Start every mtime in the past so a same-tick write shows.
			earlier := time.Now().Add(-time.Hour)
			err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				return os.Chtimes(path, earlier, earlier)
			})
			if err != nil {
				t.Fatal(err)
			}
			before, err := stampTree(root)
			if err != nil {
				t.Fatal(err)
			}
			tt.edit(t, root)
			after, err := stampTree(root)
			if err != nil {
				t.Fatal(err)
			}
			if changed := !after.equal(before); changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v (before %+v, after %+v)", changed, tt.wantChanged, before, after)
			}
		})
	}
}

func TestSessionCacheInvalidation(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	ws := workspace(t, fixtureDir)
	s := newSession(Options{Config: defaultConfig(t), WorkDir: ws})
	in := CheckInput{Path: "mod/tested", BaselineFile: "baseline.json"}
	call := func() int {
		t.Helper()
		res, out, err := s.checkPackage(t.Context(), nil, in)
		if err != nil || res.IsError {
			t.Fatalf("checkPackage = (%v, %v)", resultText(res), err)
		}
		r, ok := out.(*report.Report)
		if !ok {
			t.Fatalf("structured output is %T, want a report", out)
		}
		return r.Metrics.TokensEst
	}
	rc := s.root(filepath.Join(ws, "mod"))

	first := call()
	firstTarget := rc.target
	if call(); rc.builds != 1 || rc.target != firstTarget {
		t.Errorf("unchanged tree: builds = %d, same target = %v, want 1 and the cached target",
			rc.builds, rc.target == firstTarget)
	}

	// Grow the package and move its mtime forward so the edit shows even
	// on a file system with coarse timestamps.
	src := filepath.Join(ws, "mod", "tested", "tested.go")
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, "\nfunc extraForCacheTest() int { return 42 }\n"...)
	writeFile(t, src, string(body))
	chtimes(t, src, time.Now().Add(time.Hour))

	edited := call()
	if rc.builds != 2 || rc.target == firstTarget {
		t.Errorf("after an edit: builds = %d, want 2 with a new target", rc.builds)
	}
	if edited <= first {
		t.Errorf("tokens_est = %d after growing the package, want more than %d: head was not re-extracted", edited, first)
	}
	if n := rc.baselines.Loads(); n != 1 {
		t.Errorf("baseline loads = %d over three calls, want 1", n)
	}
}

// writeFile writes body to path, creating its directory.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// chtimes sets both times of path to when.
func chtimes(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}
