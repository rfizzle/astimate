package typescript

import (
	"flag"
	"os"
	"path/filepath"
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
		Update:    flag.Lookup(updateFlag).Value.String() == "true",
	})
}

// fixturePackages lists the packages of testdata/ts/fixture, sorted.
func fixturePackages() []string {
	return []string{"a", "b", "dupes", "hidden", "hub", "tested", "trivial"}
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
