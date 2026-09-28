package load

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// fixtureRoot returns the absolute path of testdata/go/fixture. It walks up
// from the test's working directory (the package directory under go test)
// to the go.mod that declares this repository's module.
func fixtureRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	for {
		if isRepoRoot(t, filepath.Join(dir, "go.mod")) {
			return filepath.Join(dir, "testdata", "go", "fixture")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod for module github.com/rfizzle/astimate above the test directory")
		}
		dir = parent
	}
}

// loadFixture loads the fixture module.
func loadFixture(tb testing.TB) *Module {
	tb.Helper()
	return loadRoot(tb, fixtureRoot(tb))
}

// loadRoot loads the module at root.
func loadRoot(tb testing.TB, root string) *Module {
	tb.Helper()
	m, err := Load(&packages.Config{Dir: root}, packages.Load)
	if err != nil {
		tb.Fatalf("loading %s: %v", root, err)
	}
	return m
}

// isRepoRoot reports whether path is a go.mod declaring the astimate module.
func isRepoRoot(t testing.TB, path string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("closing %s: %v", path, cerr)
		}
	}()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "module github.com/rfizzle/astimate" {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
