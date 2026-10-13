package gate

// rose reports whether the change raised what a ratio or percentile
// metric measures, so that its rule may fail a rise (SPEC.md 8.1); it is
// the one place that direction test lives. A share or a percentile also
// rises when code is only deleted: duplication_pct when SLOC falls under
// the same duplicate blocks, cognitive_p90 when simple functions go. So
// against a baseline, duplication_pct is judged only when
// duplicated_lines rose, and cognitive_p90 only when a function changed
// since the baseline is more complex than the baseline's p90, which is
// when changed_func_cognitive_max at head is above it. Without a
// baseline, for any other metric, and when either value is not computed
// (a baseline file written before duplicated_lines existed, a check
// without a function-level diff), it reports true and the rule is judged
// as before.
func (e *evaluator) rose(metric string) bool {
	if e.base == nil {
		return true
	}
	switch metric {
	case "duplication_pct":
		h, b := e.head.DuplicatedLines, e.base.DuplicatedLines
		return h == nil || b == nil || *h > *b
	case "cognitive_p90":
		c := e.head.ChangedFuncCognitiveMax
		return c == nil || *c > e.base.CognitiveP90
	}
	return true
}

// RiseCondition returns, for a metric whose density rule fails a rise
// against a baseline only when the change raised what it measures (rose),
// the condition that must hold, as a clause; false for every other metric.
// It lets a caller explain the direction test without restating it.
func RiseCondition(metric string) (string, bool) {
	switch metric {
	case "duplication_pct":
		return "duplicated_lines rose", true
	case "cognitive_p90":
		return "a function changed since the baseline is more complex than the baseline's p90", true
	}
	return "", false
}
