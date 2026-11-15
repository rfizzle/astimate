package gate

import (
	"strconv"
	"strings"
)

// SurfaceMetric is the capacity metric whose rule also reports, as a
// heads-up, a change that reshaped the exported surface near its ceiling
// without adding an export (SPEC.md 8.1, Reshaped exports).
const SurfaceMetric = "exported_symbols"

// reshapedLimit leads the limit of a reshaped-exports heads-up, which
// ends in the rule's max.
const reshapedLimit = "exports reshaped, max "

// reshapedShown is how many names a reshaped-exports heads-up lists before
// "and N more".
const reshapedShown = 5

// IsReshaped reports whether limit is the limit of a reshaped-exports
// heads-up, so a caller can locate the finding on the first symbol it
// names.
func IsReshaped(limit string) bool { return strings.HasPrefix(limit, reshapedLimit) }

// WithReshaped gives Evaluate what the change did to the row's exported
// surface that exported_symbols does not count: names describes each
// export whose signature changed, then each export made unexported, in the
// order the heads-up lists them. A capacity rule on SurfaceMetric that
// judges the row adds a heads-up naming them (reshaped) when the row has a
// baseline value and its head value is in the rule's warn band or over its
// max. Without this option, or with no names, there is none.
func WithReshaped(names []string) Option {
	return func(o *options) { o.reshaped = names }
}

// reshaped records the reshaped-exports heads-up of a capacity rule on
// SurfaceMetric with the given max, the row's value b at the baseline and
// h at head. It is a warning whatever the rule's severity, never merged and
// never exempted, so it never changes the verdict.
func (e *evaluator) reshaped(limit, b, h float64) {
	names := e.opts.reshaped
	shown := min(len(names), reshapedShown)
	more := len(names) - shown
	var list strings.Builder
	list.WriteString("Reshaped without adding an export: ")
	for i, n := range names[:shown] {
		switch {
		case i == 0:
		case i == shown-1 && more == 0:
			list.WriteString(" and ")
		default:
			list.WriteString(", ")
		}
		list.WriteString(n)
	}
	if more > 0 {
		list.WriteString(" and " + strconv.Itoa(more) + " more")
	}
	v := Violation{Metric: SurfaceMetric, Base: b, Head: h, HasBase: true, Limit: reshapedLimit + num(limit),
		Suggestion: list.String() + ". Changes like these can hold the export count down without a split; " +
			"a reviewer should confirm each is wanted for its own sake, and if the surface has outgrown the " +
			"package, split it."}
	e.found = append(e.found, found{v: v, rule: e.cur, warning: true})
}
