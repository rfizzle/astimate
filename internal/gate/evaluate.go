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
}

// Warning is a non-failing capacity finding with the same shape as a
// Violation; its Suggestion leads with the remaining headroom.
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
	// Passed is true when there are no violations; warnings never fail.
	Passed bool
	// Violations are sorted by metric name.
	Violations []Violation
	// Warnings are sorted by metric name.
	Warnings []Warning
	// Notes are sorted by metric name.
	Notes []Note
}

// Suggester returns the fix sentence for a metric at its head value. The gate
// package cannot import score, so callers inject score.MetricSuggestion
// through a closure that supplies its Names argument.
type Suggester func(metric string, head float64, m metrics.RawMetrics) string

// Evaluate checks head, and base when it is non-nil, against rules per
// SPEC.md section 8.1. A nil base means the package is new at head: every
// density rule still applies its Max, but MaxDelta is taken against zero only
// for rules with RatchetFromZero set. Density rules fail when head minus base
// exceeds MaxDelta, or when head exceeds Max and either there is no baseline
// value or head rose above it, so an unchanged or improved legacy value over
// Max passes. Capacity rules fail above Max under the same condition, warn
// when an unchanged or improved legacy value is still above Max, and warn at
// or above WarnAt of Max. Requirement rules fail when the metric's boolean
// disagrees with Require while When holds and the package is new or its sloc
// grew. A rule
// whose metric is unknown or not computed at head is skipped, which also
// covers rebuild outputs the config loader already rejects; a v1 metric
// computed at head but null at base skips only its delta rule and adds a
// Note. Suggestions come from s; a nil s leaves them empty. Evaluate does no
// I/O.
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
	switch r.Kind {
	case Density:
		e.density(r, h)
	case Capacity:
		e.capacity(r, h)
	case Requirement:
		e.requirement(r, h)
	}
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
		// As with a density max, the ceiling fails only what a change
		// introduced; a legacy value already over it that did not rise is
		// reported so the split still gets planned.
		if !hasBase || h > b+epsilon {
			e.res.Violations = append(e.res.Violations, e.finding(r.Metric, b, h, hasBase, "max "+num(limit)))
			return
		}
		e.warn(r.Metric, b, h, hasBase, limit, "over the "+num(limit)+" ceiling (unchanged since baseline); plan a split.")
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
	e.warn(r.Metric, b, h, hasBase, limit, "at "+pct+"% of the "+num(limit)+" ceiling; plan a split before the next feature.")
}

// warn records a capacity warning whose suggestion is text followed by the
// Suggester's sentence, if any.
func (e *evaluator) warn(metric string, b, h float64, hasBase bool, limit float64, text string) {
	w := e.finding(metric, b, h, hasBase, "max "+num(limit))
	if w.Suggestion != "" {
		text += " " + w.Suggestion
	}
	w.Suggestion = text
	e.res.Warnings = append(e.res.Warnings, w)
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
