package typescript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

func TestConformance(t *testing.T) {
	root := fixtureRoot(t)
	metricstest.TestExtractor(t, New(), metricstest.Fixture{
		Root:      root,
		Packages:  fixturePackages(),
		GoldenDir: filepath.Join(root, "golden"),
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
	if _, err := os.Stat(filepath.Join(root, manifestName)); err != nil {
		tb.Fatalf("TypeScript fixture missing: %v", err)
	}
	return root
}
