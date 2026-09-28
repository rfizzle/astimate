package golang

import (
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

var _ metrics.CoverageMeasurer = (*Extractor)(nil)

func TestCoverageFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	root := fixtureRoot(t)
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/fixture"}
	pkgs := []string{"example.com/fixture/tested", "example.com/fixture/trivial"}
	got, err := New().Coverage(t.Context(), mod, pkgs)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	tested := got["example.com/fixture/tested"]
	if tested.Pct == nil || *tested.Pct < 0 || *tested.Pct > 100 {
		t.Errorf("tested = %+v, want a percentage in [0, 100]", tested)
	}
	if trivial := got["example.com/fixture/trivial"]; trivial.Pct != nil || trivial.Reason != "" {
		t.Errorf("trivial, which has no test files, = %+v, want nil with no reason", trivial)
	}
}
