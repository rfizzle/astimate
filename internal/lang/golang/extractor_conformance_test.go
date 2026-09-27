package golang

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

func TestConformance(t *testing.T) {
	root := fixtureRoot(t)
	metricstest.TestExtractor(t, New(), metricstest.Fixture{
		Root:       root,
		ModulePath: "example.com/fixture",
		Packages:   fixturePackages(),
		GoldenDir:  filepath.Join(root, "golden"),
		Unmarked:   unmarkedCopy(t, root),
	})
}

// generatedMarker and unmarked are the start of Go's generated-file line
// and the same number of bytes that no longer match it, so a file keeps its
// length, and its token estimate, when unmarkedCopy defaces the line.
const (
	generatedMarker = "// Code generated "
	unmarked        = "// Code-generated "
)

// unmarkedCopy copies the fixture module at root and the extmod module its
// go.mod replaces to a temporary directory, keeping their layout, turns
// every non-test file with a generated-file header into a hand-written one
// by defacing the header line in place, and returns the copied root. It
// fails the test when no file was generated, since the check would then
// prove nothing.
func unmarkedCopy(t *testing.T, root string) string {
	t.Helper()
	dir := t.TempDir()
	for _, mod := range []string{filepath.Base(root), "extmod"} {
		if err := os.CopyFS(filepath.Join(dir, mod), os.DirFS(filepath.Join(filepath.Dir(root), mod))); err != nil {
			t.Fatalf("copying %s: %v", mod, err)
		}
	}
	copied := filepath.Join(dir, filepath.Base(root))
	n := 0
	err := filepath.WalkDir(copied, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ParseComments|parser.PackageClauseOnly)
		if err != nil || !ast.IsGenerated(f) {
			return err
		}
		// The copied files keep the fixture's permissions, which may be
		// read-only in a read-only checkout.
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
		n++
		return os.WriteFile(path, bytes.Replace(data, []byte(generatedMarker), []byte(unmarked), 1), 0o600)
	})
	if err != nil {
		t.Fatalf("unmarking generated files: %v", err)
	}
	if n == 0 {
		t.Fatalf("no generated file under %s; the generated-file check needs one", root)
	}
	return copied
}

// TestConformanceCgo runs the contract on the cgo module, whose goldens are
// counted from the source files rather than the files cgo generates. The
// module loads only when cgo and its C compiler are available.
func TestConformanceCgo(t *testing.T) {
	if !haveCgo(t) {
		t.Skip("cgo or its C compiler is unavailable")
	}
	root := cgoRoot(t)
	metricstest.TestExtractor(t, New(), metricstest.Fixture{
		Root:       root,
		ModulePath: "example.com/cgo",
		Packages:   []string{"example.com/cgo/native", "example.com/cgo/user"},
		GoldenDir:  filepath.Join(root, "golden"),
	})
}
