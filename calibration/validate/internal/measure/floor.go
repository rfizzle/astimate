package measure

import "github.com/rfizzle/astimate/internal/gate"

// Floor is a density rule's floor: the value at or below which the rule
// never fails (SPEC.md 8.1). A sweep varies it only on a rule that ships
// one.
const Floor Param = "floor"

// floorOf returns the floor setting s, a Floor setting, puts in place:
// nil when s removes it.
func floorOf(s Setting) *float64 {
	if s.None {
		return nil
	}
	v := s.Value
	return &v
}

// floorGrid returns the floors a sweep of t tries, before Grid sorts
// them: the shipped value cur, and each of 0, 1, 2, 3, 4, 5, 8, 10, 15,
// 20 and 30 below the rule's max when it has one. Removing the floor is
// the setting Grid adds last.
func floorGrid(t gate.Threshold, cur float64) []float64 {
	vs := []float64{cur}
	for _, v := range []float64{0, 1, 2, 3, 4, 5, 8, 10, 15, 20, 30} {
		if t.Max == nil || v < *t.Max {
			vs = append(vs, v)
		}
	}
	return vs
}
