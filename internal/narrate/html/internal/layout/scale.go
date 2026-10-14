package layout

import "math"

// Scale maps a domain [0, Max] onto a range, linearly or by square root,
// as d3.scaleLinear and d3.scaleSqrt do.
type Scale struct {
	max, from, to float64
	sqrt          bool
}

// Linear maps [0, domainMax] onto [from, to] linearly.
func Linear(domainMax, from, to float64) Scale {
	return Scale{max: domainMax, from: from, to: to}
}

// Sqrt maps [0, domainMax] onto [from, to] by square root, so equal steps
// of the range cover growing steps of the domain.
func Sqrt(domainMax, from, to float64) Scale {
	return Scale{max: domainMax, from: from, to: to, sqrt: true}
}

// At returns v's place in the range. A domain of zero width maps every
// value to the middle of the range, as d3 does.
func (s Scale) At(v float64) float64 {
	hi := s.max
	if s.sqrt {
		v, hi = math.Sqrt(math.Max(0, v)), math.Sqrt(math.Max(0, hi))
	}
	t := 0.5
	if hi != 0 {
		t = v / hi
	}
	return s.from + t*(s.to-s.from)
}

// Ticks returns about count round values from start to stop inclusive, at
// a step of 1, 2 or 5 times a power of ten, as d3.ticks chooses them.
func Ticks(start, stop float64, count int) []float64 {
	if count <= 0 || stop < start {
		return nil
	}
	if start == stop {
		return []float64{start}
	}
	i1, i2, inc := tickSpec(start, stop, float64(count))
	if i2 < i1 {
		return nil
	}
	out := make([]float64, 0, int(i2-i1)+1)
	for i := i1; i <= i2; i++ {
		if inc < 0 {
			out = append(out, i/-inc)
		} else {
			out = append(out, i*inc)
		}
	}
	return out
}

// tickSpec returns the first and last tick as multiples of the step, and
// the step, negative for its reciprocal when it is below 1.
func tickSpec(start, stop, count float64) (i1, i2, inc float64) {
	step := (stop - start) / count
	power := math.Floor(math.Log10(step))
	e := step / math.Pow(10, power)
	factor := 1.0
	switch {
	case e >= math.Sqrt(50):
		factor = 10
	case e >= math.Sqrt(10):
		factor = 5
	case e >= math.Sqrt(2):
		factor = 2
	}
	if power < 0 {
		inc = math.Pow(10, -power) / factor
		i1, i2 = jsRound(start*inc), jsRound(stop*inc)
		if i1/inc < start {
			i1++
		}
		if i2/inc > stop {
			i2--
		}
		inc = -inc
	} else {
		inc = math.Pow(10, power) * factor
		i1, i2 = jsRound(start/inc), jsRound(stop/inc)
		if i1*inc < start {
			i1++
		}
		if i2*inc > stop {
			i2--
		}
	}
	if i2 < i1 && count >= 0.5 && count < 2 {
		return tickSpec(start, stop, count*2)
	}
	return i1, i2, inc
}

// jsRound rounds half up, as JavaScript's Math.round does.
func jsRound(v float64) float64 { return math.Floor(v + 0.5) }

// Arc is one band of a ring drawn as a dashed circle stroke: the dash,
// the gap after it and the offset where it starts.
type Arc struct {
	Dash, Gap, Offset float64
}

// Ring splits a circle of radius r into one arc per count, each as long
// as its share of the total, in order, with gap trimmed off each arc's
// end. A zero total gives empty arcs.
func Ring(counts []int, r, gap float64) []Arc {
	total := 0
	for _, n := range counts {
		total += n
	}
	arcs := make([]Arc, len(counts))
	if total == 0 {
		return arcs
	}
	circ := 2 * math.Pi * r
	off := 0.0
	for i, n := range counts {
		l := circ * float64(n) / float64(total)
		arcs[i] = Arc{Dash: math.Max(0, l-gap), Gap: circ - l + gap, Offset: -off}
		off += l
	}
	return arcs
}
