package golang

import (
	"path/filepath"
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
	})
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
