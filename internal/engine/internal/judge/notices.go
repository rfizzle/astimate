package judge

import (
	"slices"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// Scope is what Notices needs to know about a check to tell a stale
// exemption from one whose row it could not judge.
type Scope struct {
	// All reports that the check judged every row of the module: --all,
	// with no named packages.
	All bool
	// Failed are the package paths of the rows that failed to extract.
	Failed []string
	// ModuleJudged reports that the module row was evaluated with its
	// module-wide rules; false when the check has no module row or skipped
	// those rules.
	ModuleJudged bool
}

// Notices reports the configured exemptions that ch ignored or did not
// need, as warnings a reader sees in every format (SPEC.md 8.6):
//
//   - each exemption expired at Options.Now is ignored, so the
//     violation it would have silenced fails the gate; it is reported on
//     its row when ch has that row, and otherwise, since a check of other
//     packages has nowhere to show it, in a warning log;
//   - with scope.All, each unexpired exemption that matched no violation
//     is stale and reported on its row, or, when the module has no such
//     package, on the module row, the row about the module as a whole;
//     without a module row it goes to a warning log.
//
// An exemption is not called stale when its row could not be judged: the
// row failed to extract, the language gates no rule on its metric (one
// config may serve modules of several languages), the metric is not
// computed on the row, or it is on the module row and the check did not
// evaluate that row's rules (scope.ModuleJudged).
func (c *Checker) Notices(ch *report.Check, scope Scope) {
	logger := c.o.Logger
	for _, e := range c.ex.Expired() {
		text := "The exemption for " + e.Metric + " on " + e.Package + " expired on " + e.Expires +
			" and no longer applies; fix the finding or renew the exemption with a new date. Its reason: " + e.Reason
		if r := row(ch, e.Package); r != nil {
			r.Warnings = append(r.Warnings, notice(r, e.Metric, "exemption expired "+e.Expires, text))
			continue
		}
		logger.Warn("exemption expired; ignored", "package", e.Package, "metric", e.Metric,
			"expires", e.Expires, "reason", e.Reason)
	}
	if !scope.All {
		return
	}
	unknown := func(e gate.Exemption) bool {
		if slices.Contains(scope.Failed, e.Package) ||
			!slices.ContainsFunc(c.o.Config.Thresholds, func(r gate.Threshold) bool { return r.Metric == e.Metric }) {
			return true
		}
		if e.Package == metrics.ModuleRowID && !scope.ModuleJudged {
			return true
		}
		if r := row(ch, e.Package); r != nil {
			_, ok := r.Metrics.Value(e.Metric)
			return !ok
		}
		return false
	}
	for _, e := range c.ex.Stale(unknown) {
		text := "The exemption for " + e.Metric + " on " + e.Package + " matched no violation; remove it from the config. Its reason: " + e.Reason
		r := row(ch, e.Package)
		if r == nil {
			text = "The exemption for " + e.Metric + " on " + e.Package + " matched no violation: the module has no such package. " +
				"Remove it from the config. Its reason: " + e.Reason
			if ch.Module != nil {
				r = &ch.Module.Report
			}
		}
		if r == nil {
			logger.Warn("exemption matched no violation; remove it", "package", e.Package, "metric", e.Metric, "reason", e.Reason)
			continue
		}
		r.Warnings = append(r.Warnings, notice(r, e.Metric, "stale exemption", text))
	}
}

// row returns the report of c's row whose package path is pkg, the module
// row for metrics.ModuleRowID, or nil when c has no such row.
func row(c *report.Check, pkg string) *report.Report {
	if pkg == metrics.ModuleRowID {
		if c.Module == nil {
			return nil
		}
		return &c.Module.Report
	}
	for i := range c.Packages {
		if c.Packages[i].Report.PackagePath == pkg {
			return &c.Packages[i].Report
		}
	}
	return nil
}

// notice returns the warning an exemption notice is reported as on r:
// metric with r's own head and baseline values of it, limit, and text as
// its suggestion.
func notice(r *report.Report, metric, limit, text string) report.Finding {
	f := report.Finding{Metric: metric, Limit: limit, Suggestion: text}
	if v, ok := r.Metrics.Value(metric); ok {
		f.Head = v
	}
	if r.Baseline != nil {
		if v, ok := r.Baseline.Metrics.Value(metric); ok {
			f.Base = &v
		}
	}
	return f
}
