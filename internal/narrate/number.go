package narrate

import (
	"math"
	"strconv"
	"strings"
)

// Num formats v as the report writes every number: en-US with thousands
// separators and at most one decimal (SPEC.md 10.3), so the sentences and
// every renderer's cells agree.
func Num(v float64) string {
	v = math.Round(v*10) / 10
	neg := v < 0
	v = math.Abs(v)
	whole := math.Floor(v)
	digits := strconv.FormatFloat(whole, 'f', 0, 64)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if frac := int(math.Round((v - whole) * 10)); frac > 0 {
		b.WriteString("." + strconv.Itoa(frac))
	}
	return b.String()
}

// NumRange writes the range low to high as Num writes each end, "3.2 to
// 4.1", and once when both ends write the same.
func NumRange(low, high float64) string {
	lo, hi := Num(low), Num(high)
	if lo == hi {
		return hi
	}
	return lo + " to " + hi
}
