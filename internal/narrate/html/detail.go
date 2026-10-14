package html

import (
	"html/template"
	"math"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/narrate"
)

// detail is the numbers of a package's opened row: the three widgets and
// the limit meters. Its words are the package's own fields.
type detail struct {
	Rebuild    rebuildWidget
	Copies     copiesWidget
	Complexity complexityWidget
	Meters     []meter
}

// rebuildWidget places the passes on the sittings track and splits the
// rebuild tokens by driver.
type rebuildWidget struct {
	// Tier is the sittings band as a level: fine, watch or split.
	Tier, Pos, Passes string
	Shares            []shareBar
	Rest              string
}

// shareBar is one segment of a share bar and its legend entry.
type shareBar struct {
	Class, Width, Label string
}

// copiesWidget is the waffle: one class per cell, each 1% of the lines.
type copiesWidget struct {
	Cells   []string
	Longest template.HTML
}

// complexityWidget is the function buckets and the hardest functions.
type complexityWidget struct {
	N, Easy, Care, Hard int
	Class               string
	EasyW, CareW, HardW string
	Pos                 string
	Fns                 []fnBar
}

// fnBar is one of the hardest functions with its bar.
type fnBar struct {
	F     narrate.Function
	Width string
}

// meter is one ceiling's meter.
type meter struct {
	Label, Value, Max, Width, Level string
}

// meterLabel names a ceiling on the detail's meters.
func meterLabel(metric, fallback string) string {
	switch metric {
	case "sloc":
		return "Lines"
	case "tokens_est":
		return "Tokens"
	case "largest_file_sloc":
		return "Largest file"
	case "exported_symbols":
		return "Exports"
	case "internal_imports":
		return "Internal imports"
	}
	return fallback
}

// newDetail computes p's widgets.
func newDetail(p *narrate.Package) detail {
	d := detail{Rebuild: newRebuild(&p.Rebuild, p.Tests.TestFuncs), Copies: newCopies(&p.Copies), Complexity: newComplexity(&p.Complexity)}
	for _, c := range p.Ceilings {
		d.Meters = append(d.Meters, meter{Label: meterLabel(c.Metric, c.Label), Value: fmtNum(c.Value), Max: fmtNum(c.Max),
			Width: pct(math.Min(c.Value, c.Max), c.Max), Level: c.Level})
	}
	return d
}

// newRebuild places r on the track: one sitting fills the first sixth,
// a few the next third, and splitting the rest up to nine passes. The
// spec share names the testFuncs it counts.
func newRebuild(r *narrate.Rebuild, testFuncs int) rebuildWidget {
	n := r.AgentPasses
	word := "passes"
	if n == 1 {
		word = "pass"
	}
	w := rebuildWidget{Passes: fmtNum(n) + " agent " + word}
	var pos float64
	switch {
	case n <= 1:
		w.Tier, pos = narrate.BandFine, 16.7*n
	case n <= 3:
		w.Tier, pos = narrate.BandWatch, 16.7+33.3*(n-1)/2
	default:
		w.Tier, pos = narrate.BandSplit, 50+50*math.Min(1, (n-3)/6)
	}
	w.Pos = coord(pos)
	tot := float64(max(1, r.RebuildTokens))
	rest := tot
	for _, term := range [...]struct{ term, class, note string }{{"spec", "spec", " (" + strconv.Itoa(testFuncs) + ")"}, {"volume", "vol", ""}} {
		for _, s := range r.Drivers {
			if s.Term == term.term {
				rest -= float64(s.Tokens)
				w.Shares = append(w.Shares, shareBar{Class: term.class, Width: pct(float64(s.Tokens), tot),
					Label: strconv.Itoa(s.Pct) + "% " + s.Label + term.note})
			}
		}
	}
	w.Rest = pct(math.Max(0, rest), tot)
	return w
}

// newCopies fills one waffle cell per whole percent copied, the share of
// non-code blocks (tables and boilerplate) drawn apart.
func newCopies(c *narrate.Copies) copiesWidget {
	cells := int(math.Round(c.Pct))
	code := c.Blocks
	if c.CodeBlocks != nil {
		code = *c.CodeBlocks
	}
	table := int(math.Round(float64(cells) * (1 - float64(code)/float64(max(1, c.Blocks)))))
	w := copiesWidget{Cells: make([]string, 100), Longest: longest(c)}
	for i := range w.Cells {
		switch {
		case i < cells-table:
			w.Cells[i] = "on"
		case i < cells:
			w.Cells[i] = "table"
		}
	}
	return w
}

// longest is the copies' Longest sentence with each repeat's place in
// bold, as the mockup sets it.
func longest(c *narrate.Copies) template.HTML {
	s := c.Longest
	var b strings.Builder
	for _, ch := range c.Chunks {
		place := ch.File + ":" + strconv.Itoa(ch.Start) + "-" + strconv.Itoa(ch.End)
		before, after, ok := strings.Cut(s, place)
		if !ok {
			continue
		}
		b.WriteString(template.HTMLEscapeString(before) + "<b>" + template.HTMLEscapeString(place) + "</b>")
		s = after
	}
	b.WriteString(template.HTMLEscapeString(s))
	// Every piece of the sentence went through HTMLEscapeString.
	return template.HTML(b.String())
}

// newComplexity splits the functions into the three buckets and scales
// the hardest functions' bars to the hardest of them.
func newComplexity(c *narrate.Complexity) complexityWidget {
	w := complexityWidget{N: max(1, c.Functions), Class: narrate.BandFine}
	if b := c.Buckets; b != nil {
		w.Easy, w.Care, w.Hard = b.Easy, b.Care, b.Hard
	}
	switch {
	case w.Hard > 0:
		w.Class = narrate.BandSplit
	case w.Care > 0:
		w.Class = narrate.BandWatch
	}
	n := float64(w.N)
	w.EasyW, w.CareW, w.HardW = pct(float64(w.Easy), n), pct(float64(w.Care), n), pct(float64(w.Hard), n)
	w.Pos = coord(math.Min(100, 100*float64(c.CognitiveP90)/30))
	top := 1
	for _, f := range c.Hardest {
		top = max(top, f.Cognitive)
	}
	for _, f := range c.Hardest {
		w.Fns = append(w.Fns, fnBar{F: f, Width: pct(float64(f.Cognitive), float64(top))})
	}
	return w
}
