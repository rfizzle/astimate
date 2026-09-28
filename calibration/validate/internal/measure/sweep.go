package measure

import (
	"math"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/internal/gate"
)

// Param is a limit of a rule that a sweep varies.
type Param string

// Params a sweep varies.
const (
	// Max is the absolute ceiling of a capacity or density rule.
	Max Param = "max"
	// MaxDelta is a density rule's largest permitted increase.
	MaxDelta Param = "max_delta"
	// When is the value of a requirement's "<metric> > <value>" guard.
	When Param = "when"
)

// Params returns the limits of t a sweep can vary, in the order above.
func Params(t gate.Threshold) []Param {
	var out []Param
	if t.Max != nil {
		out = append(out, Max)
	}
	if t.MaxDelta != nil {
		out = append(out, MaxDelta)
	}
	if t.When != nil {
		out = append(out, When)
	}
	return out
}

// Setting is one value of a Param, or the Param removed.
type Setting struct {
	Param Param
	Value float64
	// None removes the limit: a density rule keeps its other limit, a
	// requirement loses its guard.
	None bool
}

// String is the setting as the report shows it: "max 1000",
// "max_delta 0", "max none", "when > 100" for a guard on the rule's
// metric, or "no guard".
func (s Setting) String() string {
	v := strconv.FormatFloat(s.Value, 'f', -1, 64)
	if s.None {
		v = "none"
	}
	switch {
	case s.Param == When && s.None:
		return "no guard"
	case s.Param == When:
		return "when > " + v
	}
	return string(s.Param) + " " + v
}

// shipped returns t's current setting of p.
func shipped(t gate.Threshold, p Param) Setting {
	switch p {
	case Max:
		return Setting{Param: p, Value: *t.Max}
	case MaxDelta:
		return Setting{Param: p, Value: *t.MaxDelta}
	default:
		return Setting{Param: p, Value: t.When.Value}
	}
}

// Apply returns a copy of t with s in place of its limit.
func Apply(t gate.Threshold, s Setting) gate.Threshold {
	v := s.Value
	switch {
	case s.Param == Max && s.None:
		t.Max = nil
	case s.Param == Max:
		t.Max = &v
	case s.Param == MaxDelta && s.None:
		t.MaxDelta = nil
	case s.Param == MaxDelta:
		t.MaxDelta = &v
	case s.None:
		t.When = nil
	default:
		t.When = &gate.Condition{Metric: t.When.Metric, Value: v}
	}
	return t
}

// Grid returns the settings a sweep of t's p tries, ascending, the shipped
// value included: for a max, the shipped value times 0.5, 0.75, 1, 1.25,
// 1.5, 2, 3 and 5, rounded to a whole number; for a max_delta, 0, 1, 2, 3, 5,
// 10 and 20 with the shipped value and twice it; for a guard, 0, 50, 100,
// 200, 500 and 1000 with the shipped value. A density rule's max or
// max_delta can also be removed when the rule keeps the other, and a guard
// always can; that setting comes last.
func Grid(t gate.Threshold, p Param) []Setting {
	cur := shipped(t, p).Value
	var vs []float64
	switch p {
	case Max:
		for _, f := range []float64{0.5, 0.75, 1, 1.25, 1.5, 2, 3, 5} {
			vs = append(vs, math.Round(cur*f))
		}
		vs = append(vs, cur)
	case MaxDelta:
		vs = []float64{0, 1, 2, 3, 5, 10, 20, cur, 2 * cur}
	default:
		vs = []float64{0, 50, 100, 200, 500, 1000, cur}
	}
	slices.Sort(vs)
	vs = slices.Compact(vs)
	out := make([]Setting, 0, len(vs)+1)
	for _, v := range vs {
		out = append(out, Setting{Param: p, Value: v})
	}
	removable := (p == Max && t.Kind == gate.Density && t.MaxDelta != nil) ||
		(p == MaxDelta && t.Max != nil) || p == When
	if removable {
		out = append(out, Setting{Param: p, None: true})
	}
	return out
}

// Point is one setting of a sweep with how the rule and the gate did.
type Point struct {
	Setting Setting
	// Shipped marks the configuration's own setting.
	Shipped bool
	// Rule is the rule's rates at the setting; Gate the gate's, the other
	// rules as recorded.
	Rule, Gate Rates
}

// Curve is a sweep of one limit of one rule.
type Curve struct {
	// Rule is the rule's index in Rules.List.
	Rule   int
	Param  Param
	Points []Point
}

// Sweep re-evaluates rule i at every setting of Grid for each of its
// limits and returns one curve per limit.
func (s *Scored) Sweep(i int) []Curve {
	t := s.Rules.List[i].Threshold
	var out []Curve
	for _, p := range Params(t) {
		cur := shipped(t, p)
		c := Curve{Rule: i, Param: p}
		for _, set := range Grid(t, p) {
			applied := Apply(t, set)
			rule, all := s.Swap(i, &applied)
			c.Points = append(c.Points, Point{Setting: set, Shipped: set == cur, Rule: rule, Gate: all})
		}
		out = append(out, c)
	}
	return out
}
