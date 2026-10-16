package typescript

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/typescript/internal/resolve"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// updateFlag names the flag that makes TestConformance rewrite the goldens
// under testdata/ts/fixture/golden from the extractor's output. Use it only
// after an intentional counting change, then check every changed number by
// hand and record it in COUNTING.md.
const updateFlag = "update"

// TestMain defines the -update flag before the test binary parses flags;
// TestConformance reads it back with flag.Lookup, so no package variable
// holds it.
func TestMain(m *testing.M) {
	flag.Bool(updateFlag, false, "rewrite the TypeScript fixture goldens")
	flag.Parse()
	os.Exit(m.Run())
}

func TestConformance(t *testing.T) {
	root := fixtureRoot(t)
	metricstest.TestExtractor(t, New(), metricstest.Fixture{
		Root:      root,
		Packages:  fixturePackages(),
		GoldenDir: filepath.Join(root, "golden"),
		Unmarked:  unmarkedCopy(t, root),
		Update:    flag.Lookup(updateFlag).Value.String() == "true",
	})
}

// generatedMarker and unmarked are the marker of the fixture's generated
// file and the same number of bytes that no longer match any marker, so a
// file keeps its length, and its token estimate, when unmarkedCopy defaces
// it.
const (
	generatedMarker = "@generated"
	unmarked        = "@generatex"
)

// unmarkedCopy copies the fixture module at root to a temporary directory,
// turns every non-test file with a generated-file marker into a
// hand-written one by defacing the marker in place, and returns the copied
// root. It fails the test when no file was generated, since the check
// would then prove nothing.
func unmarkedCopy(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), filepath.Base(root))
	if err := os.CopyFS(dir, os.DirFS(root)); err != nil {
		t.Fatalf("copying %s: %v", root, err)
	}
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !resolve.IsSourceName(d.Name()) || strings.Contains(d.Name(), ".test.") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(data, []byte(generatedMarker)) {
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
	return dir
}

// fixturePackages lists the packages of testdata/ts/fixture, sorted.
func fixturePackages() []string {
	return []string{"a", "b", "covered", "dupes", "hidden", "hub", "tested", "trivial"}
}

// fixtureRoot returns the absolute path of testdata/ts/fixture, relative to
// this package's directory, where go test runs.
func fixtureRoot(tb testing.TB) string {
	tb.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "ts", "fixture"))
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, resolve.ManifestName)); err != nil {
		tb.Fatalf("TypeScript fixture missing: %v", err)
	}
	return root
}
