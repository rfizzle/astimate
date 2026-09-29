package gate

import (
	"cmp"
	"math"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/internal/metrics"
)

// defaultWarnAt is the capacity warn band used when a rule arrives with
// WarnAt zero. The config loader always applies 0.75 (SPEC.md section 8.1)
// before validation, so zero only reaches Evaluate from a hand-built rule;
// treating it as the default keeps the band instead of warning on every value.
const defaultWarnAt = 0.75

// epsilon absorbs float noise in comparisons such as 2.1 - 1.6 > 0.5.
const epsilon = 1e-9

// Violation is one failed rule: the metric, its baseline and head values,
// the limit it broke and a fix suggestion.
type Violation struct {
	// Metric is the RawMetrics JSON field name.
	Metric string
	// Base is the baseline value; meaningful only when HasBase is true.
	Base float64
	// Head is the value at head.
	Head float64
	// HasBase reports whether a baseline value exists for the metric.
	HasBase bool
	// Limit is the rule that was broken, for example "max_delta +0",
	// "max 5" or "require true".
	Limit string
	// Suggestion is the fix sentence; empty when no Suggester was given.
	Suggestion string
	// Severity is SeverityWarn on the breach of a warn rule
	// (Threshold.Warns), which Evaluate reports as a warning, and empty on
	// every other finding.
	Severity Severity
}

// Warning is a non-failing finding with the same shape as a Violation: a
// capacity rule's warning, whose Suggestion leads with the remaining
// headroom or the growth over the ceiling, or the breach of a warn rule,
// which is the violation it would have been with Severity SeverityWarn.
type Warning = Violation

// Note records a rule that was not evaluated and why.
type Note struct {
	// Metric is the RawMetrics JSON field name.
	Metric string
	// Text explains why the rule was skipped.
	Text string
}

// Result is the outcome of evaluating one package.
type Result struct {
	// Passed is true when there are no violations; warnings and exempted
	// violations never fail.
	Passed bool
	// Violations are sorted by metric name.
	Violations []Violation
	// Warnings are sorted by metric name.
	Warnings []Warning
	// Notes are sorted by metric name.
	Notes []Note
	// Exempted are the violations an exemption silenced
	// (Exemptions.Apply), in the order Violations had, followed by the
	// warn rules' breaches it silenced, in the order Warnings had;
	// Evaluate leaves it nil.
	Exempted []Exempted
}

// Suggester returns the fix sentence for a metric at its head value. The gate
// package cannot import score, so callers inject score.MetricSuggestion
// through a closure that supplies its Names argument.
type Suggester func(metric string, head float64, m metrics.RawMetrics) string

// Row is the kind of row a set of rules is evaluated on (SPEC.md 8.1).
type Row int

// Row kinds.
const (
	// PackageRow is one package's metrics.
	PackageRow Row = iota
	// ModuleRow is the module-level row, which carries the module-wide
	// metrics (metrics.ModuleWide) and zero or null everywhere else.
	ModuleRow
)

// ForRow returns the rules of rules that apply to a row of kind row: on
// the module row, the rules whose metric is module-wide (metrics.ModuleWide);
// on a package row, every other rule. A package row still reports its value
// of a module-wide metric, but one cross-package copy is gated once, on the
// module row, not once per package it touches; and the module row, whose
// package metrics are all zero, is not judged by package rules. The result
// is a new slice; rules is not modified.
func ForRow(rules []Threshold, row Row) []Threshold {
	out := make([]Threshold, 0, len(rules))
	for _, r := range rules {
		if metrics.ModuleWide(r.Metric) == (row == ModuleRow) {
			out = append(out, r)
		}
	}
	return out
}

// Evaluate checks head, and base when it is non-nil, against rules per
// SPEC.md section 8.1. A nil base means the package is new at head: every
// density rule still applies its Max, but MaxDelta is taken against zero only
// for rules with RatchetFromZero set. Density rules fail when head minus base
// exceeds MaxDelta, or when head exceeds Max and either there is no baseline
// value or head rose above it, so an unchanged or improved legacy value over
// Max passes. Capacity rules fail above Max under the same condition, except
// that a value already at or over Max at baseline may rise by up to
// OverMaxDelta with a warning to split; they warn when an unchanged or
// improved legacy value is still above Max, and warn at or above WarnAt of
// Max. Requirement rules fail when the metric's boolean disagrees with
// Require while When holds and the package is new or its sloc grew. A rule
// whose metric is unknown or not computed at head is skipped, which also
// covers rebuild outputs the config loader already rejects; a v1 metric
// computed at head but null at base skips only its delta rule and adds a
// Note. A warn rule (Threshold.Warns) is evaluated the same way, and each
// violation it would have is reported as a warning instead, with the same
// fields and Severity SeverityWarn, so it never fails the result.
// Suggestions come from s; a nil s leaves them empty. Evaluate does no
// I/O. Evaluate applies every rule it is given, whatever the row; callers
// select a row's rules with ForRow.
func Evaluate(head metrics.RawMetrics, base *metrics.RawMetrics, rules []Threshold, s Suggester) Result {
	e := evaluator{head: head, base: base, suggest: s}
	for _, r := range rules {
		e.rule(r)
	}
	byMetric := func(a, b Violation) int { return cmp.Compare(a.Metric, b.Metric) }
	slices.SortStableFunc(e.res.Violations, byMetric)
	slices.SortStableFunc(e.res.Warnings, byMetric)
	slices.SortStableFunc(e.res.Notes, func(a, b Note) int { return cmp.Compare(a.Metric, b.Metric) })
	e.res.Passed = len(e.res.Violations) == 0
	return e.res
}

type evaluator struct {
	head    metrics.RawMetrics
	base    *metrics.RawMetrics
	suggest Suggester
	res     Result
}

func (e *evaluator) rule(r Threshold) {
	h, ok := e.head.Value(r.Metric)
	if !ok {
		return
	}
	n := len(e.res.Violations)
	switch r.Kind {
	case Density:
		e.density(r, h)
	case Capacity:
		e.capacity(r, h)
	case Requirement:
		e.requirement(r, h)
	}
	if r.Warns() {
		e.demote(n)
	}
}

// demote reports the violations recorded from index n on, a warn rule's,
// as warnings through warn, each unchanged but for Severity SeverityWarn,
// and removes them from the violations, leaving them nil when none
// remain, as they are for a result that never had one.
func (e *evaluator) demote(n int) {
	for _, v := range e.res.Violations[n:] {
		v.Severity = SeverityWarn
		e.warn(v, "")
	}
	if n == 0 {
		e.res.Violations = nil
		return
	}
	e.res.Violations = e.res.Violations[:n]
}

func (e *evaluator) density(r Threshold, h float64) {
	b, hasBase, ok := e.baseValue(r.Metric)
	// Intensive metrics are not ratcheted from zero: every real package has
	// some nesting, so a new package faces only their absolute max.
	if r.MaxDelta != nil && (e.base != nil || r.RatchetFromZero) {
		switch {
		case !ok:
			e.res.Notes = append(e.res.Notes, Note{
				Metric: r.Metric,
				Text:   "not computed at baseline; max_delta skipped",
			})
		case h-b > *r.MaxDelta+epsilon:
			e.res.Violations = append(e.res.Violations, e.finding(r.Metric, b, h, hasBase, "max_delta "+signed(*r.MaxDelta)))
		}
	}
	// The max is a ceiling on what a change may introduce, not a judgment
	// of history: an unchanged or improved legacy value above it passes.
	introduced := !hasBase || h > b+epsilon
	if r.Max != nil && introduced && h > *r.Max+epsilon {
		e.res.Violations = append(e.res.Violations, e.finding(r.Metric, b, h, hasBase, "max "+num(*r.Max)))
	}
}

func (e *evaluator) capacity(r Threshold, h float64) {
	// Validate rejects a missing or non-positive max; skipping here keeps the
	// headroom percentage from dividing by zero on a hand-built rule.
	if r.Max == nil || *r.Max <= 0 {
		return
	}
	limit := *r.Max
	b, hasBase, _ := e.baseValue(r.Metric)
	if h > limit+epsilon {
		e.overMax(r, b, h, hasBase)
		return
	}
	warnAt := r.WarnAt
	if warnAt == 0 {
		warnAt = defaultWarnAt
	}
	if h < warnAt*limit-epsilon {
		return
	}
	pct := strconv.Itoa(int(math.Floor(h / limit * 100)))
	e.warn(e.finding(r.Metric, b, h, hasBase, "max "+num(limit)), "at "+pct+"% of the "+num(limit)+" ceiling; plan a split before the next feature.")
}

// overMax judges a capacity value h above the rule's max (SPEC.md 8.1); it
// is the one place the over-ceiling ratchet lives. As with a density max,
// the ceiling fails only what a change introduced: crossing the max, or a
// new package over it, is a violation. A package already at or over the
// max at baseline gets a warning when its value did not rise, so the split
// still gets planned. When it rose by at most OverMaxDelta it gets a
// warning that names the excess and says to split, and when it rose by
// more it is a violation that says the same. Under the default
// OverMaxDelta of 0 any rise is a violation with the plain max limit.
func (e *evaluator) overMax(r Threshold, b, h float64, hasBase bool) {
	limit := *r.Max
	allowed := 0.0
	if r.OverMaxDelta != nil {
		allowed = *r.OverMaxDelta
	}
	switch {
	case !hasBase || b < limit-epsilon:
		e.res.Violations = append(e.res.Violations, e.finding(r.Metric, b, h, hasBase, "max "+num(limit)))
	case h <= b+epsilon:
		e.warn(e.finding(r.Metric, b, h, hasBase, "max "+num(limit)), "over the "+num(limit)+" ceiling (unchanged since baseline); plan a split.")
	case allowed <= 0:
		e.res.Violations = append(e.res.Violations, e.finding(r.Metric, b, h, hasBase, "max "+num(limit)))
	case h-b <= allowed+epsilon:
		e.warn(e.overMaxFinding(r.Metric, b, h, limit, allowed), "")
	default:
		e.res.Violations = append(e.res.Violations, e.overMaxFinding(r.Metric, b, h, limit, allowed))
	}
}

// overMaxFinding is the finding for growth of a value already over the
// ceiling limit, with allowed the growth one change may make there: its
// limit reads "over max by N, allowed M", N being how far h is over the
// ceiling, and its suggestion says how much it grew against the allowance
// and to split the package, followed by the Suggester's sentence. A warning
// and a violation share it.
func (e *evaluator) overMaxFinding(metric string, b, h, limit, allowed float64) Violation {
	f := e.finding(metric, b, h, true, "over max by "+num(h-limit)+", allowed "+num(allowed))
	text := "grew " + num(h-b) + " while over the " + num(limit) + " ceiling, "
	if h-b <= allowed+epsilon {
		text += "within the " + num(allowed) + " one change may add there; split the package before it grows further."
	} else {
		text += "more than the " + num(allowed) + " one change may add there; split the package."
	}
	f.Suggestion = joinSentences(text, f.Suggestion)
	return f
}

// warn records w as a warning, its suggestion led by text when text is
// not empty.
func (e *evaluator) warn(w Warning, text string) {
	w.Suggestion = joinSentences(text, w.Suggestion)
	e.res.Warnings = append(e.res.Warnings, w)
}

// joinSentences joins two fix sentences with a space, leaving out an
// empty one.
func joinSentences(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}

func (e *evaluator) requirement(r Threshold, h float64) {
	if r.Require == nil {
		return
	}
	if r.When != nil {
		g, ok := e.head.Value(r.When.Metric)
		if !ok || g <= r.When.Value {
			return
		}
	}
	if (h != 0) == *r.Require || !e.grew() {
		return
	}
	b, hasBase, _ := e.baseValue(r.Metric)
	e.res.Violations = append(e.res.Violations, e.finding(r.Metric, b, h, hasBase, "require "+strconv.FormatBool(*r.Require)))
}

// grew reports whether requirement rules apply: the package is new at head,
// or its sloc rose from the baseline. An unchanged legacy package is not
// failed for a requirement it already missed.
func (e *evaluator) grew() bool {
	return e.base == nil || e.head.SLOC > e.base.SLOC
}

// baseValue returns the metric's baseline value and whether a baseline value
// exists. Without a baseline the value is zero and ok is true, so
// ratchet-from-zero deltas run against zero; ok is false only for a metric
// the baseline did not compute.
func (e *evaluator) baseValue(metric string) (v float64, hasBase, ok bool) {
	if e.base == nil {
		return 0, false, true
	}
	v, ok = e.base.Value(metric)
	return v, ok, ok
}

func (e *evaluator) finding(metric string, b, h float64, hasBase bool, limit string) Violation {
	v := Violation{Metric: metric, Base: b, Head: h, HasBase: hasBase, Limit: limit}
	if e.suggest != nil {
		v.Suggestion = e.suggest(metric, h, e.head)
	}
	return v
}

// num renders a limit value without trailing zeros.
func num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// signed renders a delta limit with an explicit sign, such as +0 or -1.
func signed(v float64) string {
	if v < 0 {
		return num(v)
	}
	return "+" + num(v)
}
