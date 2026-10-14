package load

import (
	"path/filepath"
	"testing"
)

// TestSkippedSources checks that the skipped package of testdata/go/skipped
// still names its non-test and test files.
func TestSkippedSources(t *testing.T) {
	offline(t)
	root := skippedRoot(t)
	m := loadRoot(t, root)
	got := m.SkippedSources()
	if len(got) != 1 {
		t.Fatalf("skipped sources = %+v, want remote alone", got)
	}
	s := got[0]
	dir := filepath.Join(root, "remote")
	if s.Package != "example.com/skipped/remote" || s.Dir != dir {
		t.Errorf("skipped source = %s in %s, want example.com/skipped/remote in %s", s.Package, s.Dir, dir)
	}
	if len(s.GoFiles) != 1 || s.GoFiles[0] != filepath.Join(dir, "remote.go") {
		t.Errorf("GoFiles = %v, want remote.go", s.GoFiles)
	}
	if len(s.TestGoFiles) != 1 || s.TestGoFiles[0] != filepath.Join(dir, "remote_test.go") {
		t.Errorf("TestGoFiles = %v, want remote_test.go", s.TestGoFiles)
	}
}
