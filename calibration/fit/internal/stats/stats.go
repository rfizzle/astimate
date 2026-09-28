// Package stats computes the pooled distribution of one metric: its
// percentiles, IQR and histogram, and the readable rounding rules
// calibration/fit derives candidate gate limits with (SPEC.md 11.1).
package stats

import (
	"math"
	"slices"
	"strings"
)

// histBins is the number of histogram bins per metric: nine equal-width
// bins from the minimum to the 95th percentile and one open bin above it,
// so a heavy tail does not squeeze every package into the first bin.
const histBins = 10

// Stats is the pooled distribution of one metric.
type Stats struct {
	// N is the number of rows the metric was computed on.
	N int
	// Min and Max are the extremes.
	Min, Max float64
	// P25, P50, P75, P90, P95 and P99 are nearest-rank percentiles.
	P25, P50, P75, P90, P95, P99 float64
	// IQR is P75 minus P25.
	IQR float64
	// Hist is the histogram, histBins bins in ascending order.
	Hist []Bin
}

// Bin is one histogram bin. A closed bin counts values v with
// Lo <= v < Hi, the last closed bin Lo <= v <= Hi; the open bin, the
// last, counts v > Lo.
type Bin struct {
	// Lo and Hi bound the bin; Hi is the maximum for the open bin.
	Lo, Hi float64
	// Open marks the final bin, above the 95th percentile.
	Open bool
	// Count is the number of values in the bin.
	Count int
}

// percentile returns the nearest-rank p-th percentile of sorted: the value
// at rank ceil(p/100 * n), 1-based, so it is always an observed value. It
// returns 0 for an empty slice.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(n)))
	return sorted[min(max(rank-1, 0), n-1)]
}

// ComputeStats summarizes values, which it sorts in place.
func ComputeStats(values []float64) Stats {
	slices.Sort(values)
	s := Stats{N: len(values)}
	if s.N == 0 {
		return s
	}
	s.Min, s.Max = values[0], values[s.N-1]
	ranks := [...]float64{25, 50, 75, 90, 95, 99}
	dst := [...]*float64{&s.P25, &s.P50, &s.P75, &s.P90, &s.P95, &s.P99}
	for i, p := range ranks {
		*dst[i] = percentile(values, p)
	}
	s.IQR = s.P75 - s.P25
	s.Hist = histogram(values, s.Min, s.P95)
	return s
}

// histogram bins sorted, a non-empty slice, into histBins-1 equal-width
// bins over [lo, hi] and one open bin above hi. When lo equals hi every
// value up to hi falls in the first bin.
func histogram(sorted []float64, lo, hi float64) []Bin {
	closed := histBins - 1
	width := (hi - lo) / float64(closed)
	bins := make([]Bin, histBins)
	for i := range closed {
		bins[i] = Bin{Lo: lo + float64(i)*width, Hi: lo + float64(i+1)*width}
	}
	bins[closed-1].Hi = hi
	bins[closed] = Bin{Lo: hi, Hi: sorted[len(sorted)-1], Open: true}
	for _, v := range sorted {
		switch {
		case v > hi:
			bins[closed].Count++
		case width == 0:
			bins[0].Count++
		default:
			bins[min(int((v-lo)/width), closed-1)].Count++
		}
	}
	return bins
}

// IsPercent reports whether metric is measured in percent, which rounds
// and ratchets in steps of 0.5 instead of 1.
func IsPercent(metric string) bool {
	return strings.HasSuffix(metric, "_pct")
}

// RoundReadable rounds v to two significant figures, then to the nearest
// readable step: 500 above 1000, 50 above 100, 5 above 10, otherwise 1
// (0.5 for a percentage). Halves round away from zero.
func RoundReadable(v float64, percent bool) float64 {
	if v == 0 {
		return 0
	}
	mag := math.Pow(10, math.Floor(math.Log10(math.Abs(v)))-1)
	s := math.Round(v/mag) * mag
	step := MinStep(percent)
	switch a := math.Abs(s); {
	case a > 1000:
		step = 500
	case a > 100:
		step = 50
	case a > 10:
		step = 5
	}
	return math.Round(s/step) * step
}

// MinStep is the smallest readable step: 0.5 for a percentage, else 1.
func MinStep(percent bool) float64 {
	if percent {
		return 0.5
	}
	return 1
}

// DeltaFromIQR is the max_delta a density rule gets: a quarter of the
// interquartile range rounded up to a whole step (1, or 0.5 for a
// percentage), and never less than one step.
func DeltaFromIQR(iqr float64, percent bool) float64 {
	step := MinStep(percent)
	// The epsilon keeps an exact multiple, such as 8/4, from rounding up
	// on float noise.
	d := math.Ceil(iqr/4/step-1e-9) * step
	return max(d, step)
}
