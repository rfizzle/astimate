// Package exemptions holds the configured exemptions of one check (SPEC.md
// 8.6): it applies them to each row's result as package judge evaluates
// the rows, and, once every row is judged, reports those the check
// ignored because they expired or did not need because they matched no
// violation. Package engine composes it; it judges no row.
package exemptions

import (
	"log/slog"
	"slices"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/gate/exempt"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// Set is the configured exemptions of one check as of the time it runs.
type Set struct {
	ex *exempt.List
	// rules are the configured thresholds, which tell a stale exemption
	// from one on a metric the language gates no rule on.
	rules []gate.Threshold
	// logger receives the notices without a row.
	logger *slog.Logger
}

// New returns the exemptions of cfg as of now (exempt.New), the time an
// exemption's expiry is judged at, whose notices without a row go to
// logger.
func New(cfg config.Effective, now time.Time, logger *slog.Logger) *Set {
	return &Set{ex: exempt.New(cfg.Exemptions, now), rules: cfg.Thresholds, logger: logger}
}

// Apply applies the exemptions to res, the result of the row with package
// path pkg (exempt.List.Apply): a violation an unexpired exemption matches
// moves to res's exempted findings.
func (s *Set) Apply(pkg string, res *gate.Result) {
	s.ex.Apply(pkg, res)
}

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
//   - each exemption expired at the time New was given is ignored, so the
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
//
// A notice about an exemption from a file other than the configuration
// (exempt.Entry.Source) names that file; one about the configuration's
// own reads as it always has.
func (s *Set) Notices(ch *report.Check, scope Scope) {
	logger := s.logger
	for _, e := range s.ex.Expired() {
		text := "The exemption for " + e.Metric + " on " + e.Package + inFile(e) + " expired on " + e.Expires +
			" and no longer applies; fix the finding or renew the exemption with a new date. Its reason: " + e.Reason
		if r := row(ch, e.Package); r != nil {
			r.Warnings = append(r.Warnings, notice(r, e.Metric, "exemption expired "+e.Expires, text))
			continue
		}
		logger.Warn("exemption expired; ignored", logArgs(e, "package", e.Package, "metric", e.Metric,
			"expires", e.Expires, "reason", e.Reason)...)
	}
	if !scope.All {
		return
	}
	unknown := func(e exempt.Entry) bool {
		if slices.Contains(scope.Failed, e.Package) ||
			!slices.ContainsFunc(s.rules, func(r gate.Threshold) bool { return r.Metric == e.Metric }) {
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
	for _, e := range s.ex.Stale(unknown) {
		text := "The exemption for " + e.Metric + " on " + e.Package + inFile(e) + " matched no violation; remove it from " + home(e) + ". Its reason: " + e.Reason
		r := row(ch, e.Package)
		if r == nil {
			text = "The exemption for " + e.Metric + " on " + e.Package + inFile(e) + " matched no violation: the module has no such package. " +
				"Remove it from " + home(e) + ". Its reason: " + e.Reason
			if ch.Module != nil {
				r = &ch.Module.Report
			}
		}
		if r == nil {
			logger.Warn("exemption matched no violation; remove it", logArgs(e, "package", e.Package, "metric", e.Metric, "reason", e.Reason)...)
			continue
		}
		r.Warnings = append(r.Warnings, notice(r, e.Metric, "stale exemption", text))
	}
}

// inFile names the file e came from, " in <file>", when it is not the
// configuration; empty for the configuration's own, whose notices read as
// they always have.
func inFile(e exempt.Entry) string {
	if e.Source == "" {
		return ""
	}
	return " in " + e.Source
}

// home names the file a person edits to remove e.
func home(e exempt.Entry) string {
	if e.Source == "" {
		return "the config"
	}
	return e.Source
}

// logArgs returns args, the attributes of a logged notice about e, with
// the file e came from added when it is not the configuration.
func logArgs(e exempt.Entry, args ...any) []any {
	if e.Source != "" {
		args = append(args, "file", e.Source)
	}
	return args
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
