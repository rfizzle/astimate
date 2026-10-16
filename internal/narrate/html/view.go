package html

import (
	"html/template"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/html/internal/layout"
)

// page is everything the template lays out: the module report and the
// numbers computed from it for the charts and widgets.
type page struct {
	M        *narrate.Module
	Fonts    template.CSS
	Eyebrow  string
	Stickers []sticker
	// Basis is the tooltip of the engineer-time sticker: where the
	// engineer time comes from.
	Basis   string
	Ring    []ringArc
	Steps   []step
	Bubbles []bubble
	Scatter scatter
	Rows    []row
	// Window is the churn window, 0 when churn is off.
	Window int
	// Churned reports whether any package has churn, so "Start here"
	// says what it is ranked by.
	Churned bool
	Change  *changeView
}

// sticker is one of the hero's tilted counts.
type sticker struct {
	Class, Value, Label string
}

// ringArc is one band's arc of the hero's ring.
type ringArc struct {
	Band, Dash, Offset string
}

// step is one "Start here" card.
type step struct {
	N     int
	P     *narrate.Package
	Meter string
}

// row is one package's row of the table, with its detail.
type row struct {
	I int
	P *narrate.Package
	// Sloc and Days are the sort keys; Churn is -1 without churn.
	Sloc, Days float64
	Churn      int
	Tokens     float64
	// Mini is the size bar's width; Over marks a package over its sloc
	// limit.
	Mini string
	Over bool
	// Hot is the row's place in narrate.Hotter order, counted down from
	// the number of rows, so a higher value is hotter and none ties.
	Hot    int
	Detail detail
}

// build computes the page for m.
func build(m *narrate.Module, fonts template.CSS) *page {
	p := &page{M: m, Fonts: fonts, Eyebrow: eyebrow(m), Window: m.ChurnWindow, Basis: m.Counts.HumanBasisShort}
	c := m.Counts
	p.Stickers = []sticker{
		{narrate.BandSplit, strconv.Itoa(c.Split), "need a split"},
		{narrate.BandWatch, strconv.Itoa(c.Watch), "to watch"},
		{narrate.BandFine, strconv.Itoa(c.Fine), "fine"},
		{"ink", strconv.Itoa(c.MultiSitting), "need more than one sitting"},
		{"ink", c.HumanTime, "of one engineer's time"},
		{"ink", strconv.Itoa(c.WithoutTests), "without tests"},
	}
	// An empty module draws no arcs: an all-zero dash would draw each
	// arc as a full circle.
	for i, a := range layout.Ring([]int{c.Fine, c.Watch, c.Split}, 40, 2) {
		band := [...]string{narrate.BandFine, narrate.BandWatch, narrate.BandSplit}[i]
		if a.Dash+a.Gap > 0 {
			p.Ring = append(p.Ring, ringArc{Band: band, Dash: coord(a.Dash) + " " + coord(a.Gap), Offset: coord(a.Offset)})
		}
	}
	p.Rows = make([]row, len(m.Packages))
	byPath := make(map[string]int, len(m.Packages))
	for i := range m.Packages {
		pk := &m.Packages[i]
		byPath[pk.Path] = i
		r := row{I: i, P: pk, Days: pk.Rebuild.HumanDays, Churn: -1, Detail: newDetail(pk)}
		if pk.Churn != nil {
			r.Churn = pk.Churn.Commits
			p.Churned = true
		}
		if v, lim, ok := ceiling(pk, "sloc"); ok {
			r.Sloc, r.Over, r.Mini = v.Value, v.Over, pct(min(v.Value, lim), lim)
		} else {
			r.Sloc = float64(pk.Copies.Lines)
		}
		if v, _, ok := ceiling(pk, "tokens_est"); ok {
			r.Tokens = v.Value
		}
		p.Rows[i] = r
	}
	p.Steps = steps(m, byPath)
	p.Bubbles = bubbles(p.Rows)
	p.Scatter = newScatter(p.Rows, hotRows(m, p.Rows, 4), m.ChurnWindow)
	slices.SortStableFunc(p.Rows, func(a, b row) int { return narrate.Hotter(a.P, b.P) })
	for i := range p.Rows {
		p.Rows[i].Hot = len(p.Rows) - i
	}
	if m.Change != nil {
		p.Change = newChange(m)
	}
	return p
}

// eyebrow names the module and the commit the report describes.
func eyebrow(m *narrate.Module) string {
	s := m.ModulePath
	if s == "" {
		s = m.Name
	}
	if m.Head != "" {
		s += " · " + shortCommit(m.Head)
	}
	return s
}

// shortCommit is the first eight characters of a commit hash.
func shortCommit(c string) string {
	if len(c) > 8 {
		return c[:8]
	}
	return c
}

// ceiling returns p's ceiling on metric and its max.
func ceiling(p *narrate.Package, metric string) (c narrate.Ceiling, limit float64, ok bool) {
	for _, c := range p.Ceilings {
		if c.Metric == metric {
			return c, c.Max, true
		}
	}
	return narrate.Ceiling{}, 0, false
}

// steps are the first three hotspots as "Start here" cards, each meter
// against the first's score.
func steps(m *narrate.Module, byPath map[string]int) []step {
	var out []step
	top := 0.0
	for _, path := range m.Hotspots {
		i, ok := byPath[path]
		if !ok || len(out) == 3 {
			continue
		}
		pk := &m.Packages[i]
		if len(out) == 0 {
			top = pk.Hotspot
		}
		out = append(out, step{N: len(out) + 1, P: pk, Meter: pct(pk.Hotspot, top)})
	}
	return out
}

// hotRows returns the rows of the first n hotspots, in order.
func hotRows(m *narrate.Module, rows []row, n int) []*row {
	out := make([]*row, 0, n)
	for _, path := range m.Hotspots {
		for i := range rows {
			if rows[i].P.Path == path && len(out) < n {
				out = append(out, &rows[i])
			}
		}
	}
	return out
}

// tip is a package's tooltip: its path, its story and its numbers, one
// per line.
func tip(r *row) string {
	s := r.P.Path + "\n" + r.P.Story + "\n" + fmtNum(r.Days) + " days to rebuild"
	if r.Churn >= 0 {
		s += " · " + fmtInt(r.Churn) + " changes"
	}
	return s + " · " + fmtNum(r.Tokens) + " tokens"
}
