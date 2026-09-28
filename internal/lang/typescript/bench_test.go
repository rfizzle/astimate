package typescript

import (
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// BenchmarkExtractModule measures one whole extraction of the fixture
// module: discovery, the tsconfig chain, the per-file pass over every file,
// import resolution and the metrics of every package, on a fresh extractor
// each time so nothing is cached across iterations.
func BenchmarkExtractModule(b *testing.B) {
	root := fixtureRoot(b)
	for b.Loop() {
		e := New()
		mod := &metrics.ModuleContext{Root: root}
		pkgs, err := e.Packages(root)
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range pkgs {
			if _, err := e.Extract(b.Context(), mod, p); err != nil {
				b.Fatal(err)
			}
		}
	}
}
