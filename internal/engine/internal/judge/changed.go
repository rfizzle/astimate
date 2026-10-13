package judge

import (
	"slices"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// nameJudged returns the changed function r's changed_func_cognitive_max
// finding is to be located on. When the gate judged the finding on one
// changed function (gate.Violation.Function), which need not be the most
// complex one, worst, it is that function, with worst's files, and the
// suggestion of each such finding of r, exempted ones included, is
// rewritten to name it; otherwise it is worst. m and names are the row's
// metrics and the names its suggestions were made with. res is the result
// r was built from.
func nameJudged(r *report.Report, res *gate.Result, worst *changedFunction, m metrics.RawMetrics,
	names score.Names,
) *changedFunction {
	fn := judgedFunction(res)
	if fn == nil || worst == nil {
		return worst
	}
	j := describeChanged(fn)
	j.files = worst.files
	names.ChangedFunction = j.name
	for _, f := range r.AllFindings() {
		if f.Metric == gate.ChangedFuncMetric {
			f.Suggestion = score.MetricSuggestion(f.Metric, f.Head, m, names)
		}
	}
	return j
}

// judgedFunction returns the Function of res's changed_func_cognitive_max
// finding, among its violations, warnings and exempted findings, and nil
// when it has none.
func judgedFunction(res *gate.Result) *metrics.FunctionInfo {
	vs := slices.Concat(res.Violations, res.Warnings)
	for _, e := range res.Exempted {
		vs = append(vs, e.Violation)
	}
	i := slices.IndexFunc(vs, func(v gate.Violation) bool { return v.Metric == gate.ChangedFuncMetric && v.Function != nil })
	if i < 0 {
		return nil
	}
	return vs[i].Function
}
