package golang

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// fixtureRoot returns the absolute path of testdata/go/fixture. It walks up
// from the test's working directory (the package directory under go test)
// to the go.mod that declares this repository's module.
func fixtureRoot(t *testing.T) string {
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

// isRepoRoot reports whether path is a go.mod declaring the astimate module.
func isRepoRoot(t *testing.T, path string) bool {
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

func TestFixtureLoadsSevenPackages(t *testing.T) {
	root := fixtureRoot(t)
	cfg := &packages.Config{
		Mode:    packages.NeedName,
		Dir:     root,
		Context: t.Context(),
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("loading fixture at %s: %v", root, err)
	}
	got := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Errorf("package %s: %v", p.PkgPath, e)
		}
		got = append(got, p.PkgPath)
	}
	slices.Sort(got)
	want := []string{
		"example.com/fixture/a",
		"example.com/fixture/b",
		"example.com/fixture/dupes",
		"example.com/fixture/hidden",
		"example.com/fixture/hub",
		"example.com/fixture/tested",
		"example.com/fixture/trivial",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("fixture packages = %v, want %v", got, want)
	}
}
