package html

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/narrate/html/internal/layout"
)

// The map's box and the scatter's margins, as the mockup draws them.
const (
	mapW, mapH                         = 1000.0, 560.0
	marginL, marginR, marginT, marginB = 70.0, 30.0, 30.0, 60.0
	// minBubble is the smallest value a bubble is sized by, so a tiny
	// package stays visible.
	minBubble = 400
)

// bubble is one package's circle on the bubble map.
type bubble struct {
	I                int
	Band             string
	X, Y, R          string
	Label, Font, Sub string
	Tip              string
}

// bubbles packs one circle per row, sized by its tokens, in row order.
func bubbles(rows []row) []bubble {
	values := make([]float64, len(rows))
	for i := range rows {
		values[i] = math.Max(rows[i].Tokens, minBubble)
	}
	out := make([]bubble, len(rows))
	for i, c := range layout.Pack(values, mapW, mapH, 5) {
		r := &rows[i]
		b := bubble{I: r.I, Band: r.P.Band, X: coord(c.X), Y: coord(c.Y), R: coord(c.R), Tip: tip(r)}
		if c.R > 26 {
			parts := strings.Split(shortPath(r.P.Path), "/")
			b.Label = truncate(parts[len(parts)-1], max(3, int(c.R/4.2)))
			b.Font = coord(math.Min(16, c.R/3.2))
		}
		if c.R > 40 {
			b.Sub = r.P.Rebuild.SittingsText + " · " + r.P.Rebuild.HumanShort
		}
		out[i] = b
	}
	return out
}

// scatter is the "hot vs costly" chart: churn across, rebuild days up.
type scatter struct {
	XTicks, YTicks []tick
	Points         []point
	Labels         []point
	XTitle         string
	// MidX and MidY are the plot's middle, for the axis titles.
	MidX, MidY string
	// Left, Right, Top and Bottom are the plot's edges; Below and Beside
	// where the tick labels sit.
	Left, Right, Top, Bottom, Below, Beside string
}

// tick is one gridline and its label.
type tick struct {
	At, Label string
}

// point is one package's dot, or the label beside a hotspot's dot.
type point struct {
	I       int
	Band    string
	X, Y, R string
	Text    string
	Tip     string
}

// newScatter places every row by churn on a linear x and rebuild days on
// a square-root y, sized by the square root of its tokens, and labels the
// hottest rows. window is the churn window for the x title.
func newScatter(rows []row, hot []*row, window int) scatter {
	maxChurn, maxDays, maxTokens := 0.0, 0.0, 0.0
	for i := range rows {
		maxChurn = math.Max(maxChurn, float64(max(0, rows[i].Churn)))
		maxDays = math.Max(maxDays, rows[i].Days)
		maxTokens = math.Max(maxTokens, rows[i].Tokens)
	}
	x := layout.Linear(maxChurn*1.05, marginL, mapW-marginR)
	y := layout.Sqrt(maxDays*1.05, mapH-marginB, marginT)
	r := layout.Sqrt(maxTokens, 4, 34)
	s := scatter{MidX: coord((marginL + mapW - marginR) / 2), MidY: coord((marginT + mapH - marginB) / 2),
		Left: coord(marginL), Right: coord(mapW - marginR), Top: coord(marginT), Bottom: coord(mapH - marginB),
		Below: coord(mapH - marginB + 3), Beside: coord(marginL - 3), XTitle: "commits touching the package"}
	if window > 0 {
		s.XTitle += " in the last " + strconv.Itoa(window)
	}
	for _, v := range layout.Ticks(0, maxChurn*1.05, 6) {
		s.XTicks = append(s.XTicks, tick{At: coord(x.At(v)), Label: fmtNum(v)})
	}
	for _, v := range dayTicks(maxDays * 1.05) {
		s.YTicks = append(s.YTicks, tick{At: coord(y.At(v)), Label: fmtNum(v)})
	}
	place := func(rw *row) (px, py, pr float64) {
		return x.At(float64(max(0, rw.Churn))), y.At(rw.Days), r.At(rw.Tokens)
	}
	order := make([]*row, len(rows))
	for i := range rows {
		order[i] = &rows[i]
	}
	slices.SortStableFunc(order, func(a, b *row) int { return cmp.Compare(b.Tokens, a.Tokens) })
	for _, rw := range order {
		px, py, pr := place(rw)
		s.Points = append(s.Points, point{I: rw.I, Band: rw.P.Band, X: coord(px), Y: coord(py), R: coord(pr), Tip: tip(rw)})
	}
	for _, rw := range hot {
		px, py, pr := place(rw)
		s.Labels = append(s.Labels, point{X: coord(px - pr - 6), Y: coord(py + 4), Text: shortPath(rw.P.Path)})
	}
	return s
}

// dayTicks returns the y gridlines up to top: the mockup's fixed
// rebuild-day values, or d3's ticks when fewer than four of those fit.
func dayTicks(top float64) []float64 {
	var out []float64
	for _, v := range [...]float64{0, 10, 50, 100, 200, 300} {
		if v <= top {
			out = append(out, v)
		}
	}
	if len(out) >= 4 {
		return out
	}
	return layout.Ticks(0, top, 5)
}
