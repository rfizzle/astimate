package judge

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// TestNameJudged checks a changed_func_cognitive_max finding the gate
// judged on one changed function is named and located on that function,
// an exempted one included, rather than on the most complex changed one.
func TestNameJudged(t *testing.T) {
	t.Parallel()

	worst := &changedFunction{cognitive: 369, name: "UI.Update (ui.go:20)", file: "ui.go", line: 20,
		files: map[string]bool{"ui.go": true, "f.go": true}}
	fresh := metrics.FunctionInfo{Name: "fresh", Cognitive: 60, File: "f.go", Line: 4}
	finding := report.Finding{Metric: gate.ChangedFuncMetric, Head: 60, Suggestion: "Changed function UI.Update (ui.go:20) has"}
	for _, exempted := range []bool{false, true} {
		v := gate.Violation{Metric: gate.ChangedFuncMetric, Head: 60, Function: &fresh}
		var res gate.Result
		r := report.Report{PackagePath: "old"}
		if exempted {
			res.Exempted = []gate.Exempted{{Violation: v}}
			r.Exemptions = []report.Exempted{{Finding: finding}}
		} else {
			res.Violations = []gate.Violation{v}
			r.Violations = []report.Finding{finding}
		}
		got := nameJudged(&r, &res, worst, metrics.RawMetrics{}, score.Names{ChangedFunction: worst.name})
		if got.file != "f.go" || got.line != 4 || !got.files["ui.go"] {
			t.Errorf("exempted %v: nameJudged = %+v, want fresh at f.go:4 with worst's files", exempted, got)
		}
		locateFindings(&r, &metrics.Details{}, got)
		f := r.AllFindings()[0]
		if want := "Changed function fresh (f.go:4) has cognitive complexity 60"; !strings.HasPrefix(f.Suggestion, want) {
			t.Errorf("exempted %v: suggestion = %q, want it to start %q", exempted, f.Suggestion, want)
		}
		if f.Location == nil || f.Location.File != "old/f.go" || f.Location.Line != 4 {
			t.Errorf("exempted %v: location = %+v, want old/f.go:4", exempted, f.Location)
		}
	}
	if got := nameJudged(&report.Report{}, &gate.Result{}, worst, metrics.RawMetrics{}, score.Names{}); got != worst {
		t.Errorf("without a judged function nameJudged = %+v, want worst", got)
	}
}
