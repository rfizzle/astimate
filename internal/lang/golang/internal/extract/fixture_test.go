package extract

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/dup"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
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

// loadFixture loads the fixture module, bypassing the Extractor, for tests
// that call Assemble directly.
func loadFixture(tb testing.TB) *Module {
	tb.Helper()
	return loadRoot(tb, fixtureRoot(tb))
}

// loadRoot loads the module at root, bypassing the Extractor.
func loadRoot(tb testing.TB, root string) *Module {
	tb.Helper()
	m, err := load.Load(&packages.Config{Dir: root}, packages.Load)
	if err != nil {
		tb.Fatalf("loading %s: %v", root, err)
	}
	return New(m, root)
}

// defaultOptions returns the options of an Extractor built with no option:
// the ratio token counter at the default ratio and the default duplication
// options, reading from the disk.
func defaultOptions() Options {
	return Options{Counter: inspect.NewRatioCounter(inspect.DefaultCharsPerToken), Dup: dup.DefaultOptions()}
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

// writeFile writes content to the file name, failing the test on error.
func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// newO200kForTest returns an o200k counter with proxies pointed at an
// unroutable address and an empty tiktoken cache directory, so a download of
// the BPE file would fail instead of succeeding.
func newO200kForTest(t *testing.T) inspect.Counter {
	t.Helper()
	t.Setenv("TIKTOKEN_CACHE_DIR", t.TempDir())
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(k, "http://127.0.0.1:9")
	}
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	c, err := inspect.NewO200kCounter()
	if err != nil {
		t.Fatalf("NewO200kCounter: %v", err)
	}
	return c
}
