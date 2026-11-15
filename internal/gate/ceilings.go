package gate

import "github.com/rfizzle/astimate/internal/metrics"

// Ceiling is one capacity rule's ceiling on a row and the row's value
// under it, the input of rank's headroom (SPEC.md 8.1, Headroom).
type Ceiling struct {
	// Metric is the rule's metric.
	Metric string
	// Value is the row's value of what the rule reads: its metric, or the
	// coverage count its Source selects.
	Value float64
	// Max is the rule's ceiling, always greater than zero.
	Max float64
}

// Ceiling returns the ceiling of t and whether t has one: a capacity
// rule with a positive Max. A density rule's Max is a limit on what a
// change may introduce, not a ceiling. It is the one definition rank's
// headroom (Ceilings) and the module report's limits share.
func (t Threshold) Ceiling() (float64, bool) {
	if t.Kind != Capacity || t.Max == nil || *t.Max <= 0 {
		return 0, false
	}
	return *t.Max, true
}

// Ceilings returns the ceilings of the capacity rules among rules that
// would judge head in Evaluate, in rule order: a rule whose roles leave out
// head's role (Threshold.Judges), whose metric head does not compute, or
// without a positive Max is left out. A rule at SeverityWarn is kept,
// since its ceiling is still one to plan a split around. Like Evaluate,
// Ceilings applies every rule it is given; callers select a package row's
// rules with ForRow. It does no I/O and judges nothing.
func Ceilings(head metrics.RawMetrics, rules []Threshold) []Ceiling {
	e := evaluator{head: head}
	var out []Ceiling
	for _, r := range rules {
		limit, ok := r.Ceiling()
		if !ok || !r.Judges(head.Role) {
			continue
		}
		v, ok := head.Value(e.source(r))
		if !ok {
			continue
		}
		out = append(out, Ceiling{Metric: r.Metric, Value: v, Max: limit})
	}
	return out
}
