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
