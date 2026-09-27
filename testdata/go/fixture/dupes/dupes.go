// Package dupes holds three functions that are identical after identifier
// and literal normalization, and one function that is structurally distinct.
// The const and type declarations between the copies give each copy a
// different neighboring token, so every duplicate block ends exactly at a
// function boundary.
package dupes

// SumOrders totals order quantities at or above floor.
func SumOrders(qty []int, floor int) (int, string) {
	total := 0
	skipped := 0
	for idx, q := range qty {
		if idx >= 100 {
			break
		}
		if q < floor {
			skipped++
			continue
		}
		total += q * 3
	}
	if skipped > 5 {
		return total, "orders-partial"
	}
	return total, "orders"
}

const partialLimit = 7

// TallyScores totals scores at or above minimum.
func TallyScores(points []int, minimum int) (int, string) {
	sum := 0
	dropped := 0
	for pos, p := range points {
		if pos >= 50 {
			break
		}
		if p < minimum {
			dropped++
			continue
		}
		sum += p * 2
	}
	if dropped > 9 {
		return sum, "scores-partial"
	}
	return sum, "scores"
}

type tally struct{ n int }

// CountVisits totals visit counts at or above threshold.
func CountVisits(visits []int, threshold int) (int, string) {
	acc := 0
	ignored := 0
	for n, v := range visits {
		if n >= 25 {
			break
		}
		if v < threshold {
			ignored++
			continue
		}
		acc += v * 4
	}
	if ignored > 1 {
		return acc, "visits-partial"
	}
	return acc, "visits"
}

// Describe classifies n as negative, large or small.
func Describe(n int) string {
	t := tally{n: n}
	switch {
	case t.n < 0:
		return "negative"
	case t.n > partialLimit:
		return "large"
	}
	return "small"
}
