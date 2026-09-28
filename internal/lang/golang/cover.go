package golang

import (
	"context"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/cover"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Coverage measures coverage_pct for the packages pkgs, import paths of the
// module at mod.Root, with one `go test -cover -count=1 -run .` run over
// all of them in the module root (metrics.CoverageMeasurer). The
// environment, GOFLAGS and CGO_ENABLED included, is passed through
// unchanged; -coverpkg is not used, so each package's figure counts only its
// own statements. When ctx has a deadline, go test's -timeout is set to the
// time left, so a hung test binary exits with it. A package without test
// files or statements gets a nil Pct and no Reason; one whose tests fail to
// build or run a nil Pct and a Reason. When ctx ends the run, the packages
// it had not reported get a Reason saying so. The error reports a go
// command that could not start.
func (e *Extractor) Coverage(ctx context.Context, mod *metrics.ModuleContext, pkgs []string) (map[string]metrics.Coverage, error) {
	return cover.Measure(ctx, mod.Root, pkgs)
}
