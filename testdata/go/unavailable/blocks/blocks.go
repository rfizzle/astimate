// Package blocks holds one duplicate block, between First and Second, in
// otherwise distinct code. The engine tests commit it beside broken.go, a
// file that does not type-check, and then without it.
package blocks

// First mixes its arguments.
func First(a, b, c int) (int, int, int) {
	x := a*b + c - a/b + c*c - a
	y := b*c + a - b/c + a*a - b
	z := c*a + b - c/a + b*b - c
	x, y, z = x+y*z, y+z*x, z+x*y
	return x - y + z, y - z + x, z - x + y
}

// Second mixes its arguments the same way.
func Second(a, b, c int) (int, int, int) {
	x := a*b + c - a/b + c*c - a
	y := b*c + a - b/c + a*a - b
	z := c*a + b - c/a + b*b - c
	x, y, z = x+y*z, y+z*x, z+x*y
	return x - y + z, y - z + x, z - x + y
}

// Sum adds xs.
func Sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

// Max returns the largest of xs, or 0 for none.
func Max(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	best := xs[0]
	for _, x := range xs[1:] {
		if x > best {
			best = x
		}
	}
	return best
}

// Join concatenates the words with sep.
func Join(words []string, sep string) string {
	out := ""
	for i, w := range words {
		if i > 0 {
			out += sep
		}
		out += w
	}
	return out
}

// Clamp limits v to lo and hi.
func Clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// Count tallies the words by their first letter.
func Count(words []string) map[byte]int {
	counts := make(map[byte]int)
	for _, w := range words {
		if w != "" {
			counts[w[0]]++
		}
	}
	return counts
}
