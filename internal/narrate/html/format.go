package html

import (
	"math"
	"strconv"
	"strings"
)

// fmtNum writes v the way the page shows a number: en-US, thousands
// grouped, at most one decimal.
func fmtNum(v float64) string {
	s := strconv.FormatFloat(math.Abs(math.Round(v*10)/10), 'f', -1, 64)
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	if v <= -0.05 {
		b.WriteByte('-')
	}
	for i := range len(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(whole[i])
	}
	if frac != "" {
		b.WriteString("." + frac)
	}
	return b.String()
}

// fmtInt is fmtNum of an int.
func fmtInt(n int) string { return fmtNum(float64(n)) }

// fmtPtr is fmtNum of *v, or n/a when v is nil.
func fmtPtr(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmtNum(*v)
}

// coord writes a coordinate or length for SVG and CSS, to two decimals
// with trailing zeros dropped, so the page is the same on every run.
func coord(v float64) string {
	s := strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

// pct is part of whole as a percentage for a CSS width, 0 when whole is
// not positive.
func pct(part, whole float64) string {
	if whole <= 0 {
		return "0"
	}
	return coord(100 * part / whole)
}

// plural returns one when n is 1, else many.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// shortPath drops the internal/ prefix and abbreviates modules/, as the
// map labels its bubbles.
func shortPath(p string) string {
	p = strings.TrimPrefix(p, "internal/")
	if rest, ok := strings.CutPrefix(p, "modules/"); ok {
		return "m/" + rest
	}
	return p
}

// truncate keeps the first n runes of s, marking a cut with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
