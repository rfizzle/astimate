package gate

import "github.com/rfizzle/astimate/internal/metrics"

// ChangedFuncMetric is the metric whose rule judges each changed function
// against its own baseline complexity when Evaluate is given the changed
// functions (WithChangedFunctions), and the one density metric that may
// carry OverMaxDelta.
const ChangedFuncMetric = "changed_func_cognitive_max"

// Option is an optional input to Evaluate.
type Option func(*options)

// options holds Evaluate's optional inputs.
type options struct {
	// changed are the row's changed functions; hasChanged is set when the
	// caller gave them, an empty list included.
	changed    []metrics.ChangedFunction
	hasChanged bool
}

// WithChangedFunctions gives Evaluate the row's functions added or
// modified since the baseline, each with the complexity of the baseline
// function it was matched to (metrics.ChangedFunctionsWithBase), so the max
// of changed_func_cognitive_max judges each function against its own
// baseline value (changedFunctionsOverMax) rather than the row's one
// value. fns may leave out functions at or below a floor under the rule's
// max, as replay rows do, since such a function is never judged. Without
// this option the rule judges the row's value as any density max does.
func WithChangedFunctions(fns []metrics.ChangedFunction) Option {
	return func(o *options) {
		o.changed, o.hasChanged = fns, true
	}
}

// changedFunctionsOverMax judges the max of r, a rule on
// changed_func_cognitive_max, per changed function (SPEC.md 8.1); it is
// the one place that verdict and its over_max_delta live. A function over
// the max fails when it has no baseline value (it is new or renamed),
// when its baseline value was below the max (it crossed it), or when it
// rose by more than r's OverMaxDelta (nil means 0) from a baseline value
// already at or over the max; a legacy function over the max that held,
// fell or rose by at most OverMaxDelta passes. One finding is recorded,
// on the most complex failing function (the first on ties), with its
// baseline and head values and Function set, so a legacy function that
// stays over the max does not hide a new one that fails. Its limit is
// "max <max>", or for growth of a function already over the max under a
// positive OverMaxDelta "over max by N, allowed M" as on a capacity rule.
func (e *evaluator) changedFunctionsOverMax(r Threshold) {
	limit, allowed := *r.Max, 0.0
	if r.OverMaxDelta != nil {
		allowed = *r.OverMaxDelta
	}
	var worst *metrics.ChangedFunction
	for i := range e.opts.changed {
		f := &e.opts.changed[i]
		h, b := float64(f.Cognitive), float64(f.BaseCognitive)
		fails := h > limit+epsilon && (!f.HasBase || b < limit-epsilon || h-b > allowed+epsilon)
		if fails && (worst == nil || f.Cognitive > worst.Cognitive) {
			worst = f
		}
	}
	if worst == nil {
		return
	}
	h, b := float64(worst.Cognitive), float64(worst.BaseCognitive)
	label := "max " + num(limit)
	if worst.HasBase && b >= limit-epsilon && allowed > 0 {
		label = "over max by " + num(h-limit) + ", allowed " + num(allowed)
	}
	v := e.finding(r.Metric, b, h, worst.HasBase, label)
	fn := worst.FunctionInfo
	v.Function = &fn
	e.breach(v)
}
