package judge

import (
	"path"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics/inventory"
	"github.com/rfizzle/astimate/internal/report"
)

// locateReshaped locates r's reshaped-exports heads-up (gate.IsReshaped)
// on at, the declaration of the first symbol it names, relative to r's
// package directory; with at unknown it keeps the location
// locateFindings gave it.
func locateReshaped(r *report.Report, at inventory.Position) {
	if at.File == "" {
		return
	}
	for _, f := range r.AllFindings() {
		if f.Metric == gate.SurfaceMetric && gate.IsReshaped(f.Limit) {
			f.Location = &report.Location{File: path.Join(r.PackagePath, at.File), Line: at.Line}
		}
	}
}
