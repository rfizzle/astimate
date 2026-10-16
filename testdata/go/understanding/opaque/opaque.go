package opaque

func Score(xs []int, strict bool) int {
	n := 0
	for _, x := range xs {
		if x > 10 && strict || x < 0 {
			n -= 2
		} else if x%2 == 0 {
			for i := 0; i < x; i++ {
				if i%3 == 0 {
					n++
				}
			}
		} else {
			n--
		}
	}
	if n < 0 && !strict {
		return 0
	}
	return n
}

func Fold(words []string, limit int) string {
	out := ""
	for _, w := range words {
		switch {
		case len(w) > limit && limit > 0:
			out += w[:limit]
		case len(w) == 0:
			continue
		default:
			for _, r := range w {
				if r == ' ' || r == '\t' {
					break
				}
				out += string(r)
			}
		}
	}
	return out
}
