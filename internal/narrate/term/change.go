package term

import (
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/narrate"
)

// Column caps of a finding line.
const (
	// labelCap and valueCap bound the label and base → head columns a
	// finding line is aligned to.
	labelCap = 27
	valueCap = 18
)

// change writes m's change report: the verdict, the counts, each failing
// package with its findings, the heads-ups, with WriteOptions.ChangeOnly
// the packages not analysed (which the repository report lists
// otherwise), the exempted findings, the deleted packages, the notices and
// the exit code.
func (p *printer) change(m *narrate.Module) {
	c := m.Change
	code := sgrGreen
	if c.Verdict == "FAIL" {
		code = sgrRed
	}
	lead := "      "
	lines := wrap(c.Headline, p.width-runes(lead))
	if len(lines) == 0 {
		lines = []string{""}
	}
	first := p.style(pad(c.Verdict, runes(lead)-2, false), sgrBold, code) + "  " + p.style(lines[0], sgrBold)
	ref := c.Base
	if ref == "" {
		ref = short(c.BaseCommit)
	}
	against := ""
	if ref != "" {
		against = "against " + ref
	}
	if against != "" && len(lines) == 1 && runes(lead)+runes(lines[0])+2+runes(against) <= p.width {
		first += "  " + p.style(against, sgrDim)
		against = ""
	}
	p.line(first)
	for _, l := range lines[1:] {
		p.line(lead + p.style(l, sgrBold))
	}
	parts := make([]string, 0, 6)
	if against != "" {
		parts = append(parts, against)
	}
	parts = append(parts, strconv.Itoa(c.Changed)+" changed", strconv.Itoa(c.Failed)+" fail",
		strconv.Itoa(c.Warnings)+" heads up", strconv.Itoa(c.Passed)+" pass clean")
	if x := c.Cross; x != nil {
		v := narrate.Num(x.Head)
		if x.Base != nil {
			v = narrate.Num(*x.Base) + " → " + v
		}
		parts = append(parts, "copies between packages "+v)
	}
	if c.NotAnalysed > 0 {
		parts = append(parts, strconv.Itoa(c.NotAnalysed)+" not analysed")
	}
	p.fit(parts, lead, lead, func(i int) string { return parts[i] })
	lw, vw := 0, 0
	for i := range c.Failing {
		for j := range c.Failing[i].Findings {
			f := &c.Failing[i].Findings[j]
			lw = max(lw, min(labelCap, runes(f.Label)))
			vw = max(vw, min(valueCap, runes(values(f))))
		}
	}
	for i := range c.Failing {
		p.failing(&c.Failing[i], lw, vw)
	}
	p.headsUp(c)
	if p.opts.ChangeOnly && len(m.Skipped) > 0 {
		p.line("")
		p.para(m.NotAnalysed, " ")
		p.notAnalysed(m.Skipped)
	}
	p.tail(c)
}

// failing writes one failing package: its path and summary, then each
// finding on three lines (what moved, why and what to do, where to look),
// or at narrowWidth or less on one line with one place to look for the
// package.
func (p *printer) failing(f *narrate.Failing, lw, vw int) {
	narrow := p.width <= narrowWidth
	summary := f.Summary
	if narrow {
		summary, _, _ = strings.Cut(summary, " · ")
	}
	p.line("")
	mark := " " + p.style("✗", sgrBold, sgrRed) + " "
	if pathW := p.width - 5 - runes(summary); pathW >= 12 {
		p.line(mark + p.style(truncLeft(f.Path, pathW), sgrCyan) + "  " + p.style(summary, sgrDim))
	} else {
		p.line(mark + p.style(truncLeft(f.Path, max(12, p.width-3)), sgrCyan))
		p.para(summary, "   ", sgrDim)
	}
	opened := false
	for i := range f.Findings {
		x := &f.Findings[i]
		moved := x.MovedShort
		if moved == "" {
			moved = x.Limit
		}
		bar := ""
		if x.Allowed != nil && x.Base != nil {
			bar = p.bar(*x.Allowed, max(x.Head-*x.Base, 1), 10)
		}
		tail, tailW := moved, runes(moved)
		if bar != "" {
			tail, tailW = p.style(bar, sgrYellow)+" "+moved, tailW+1+runes(bar)
		}
		lead := "   " + pad(x.Label, lw, false) + "  " + pad(values(x), vw, false)
		if runes(lead)+2+tailW <= p.width {
			p.line(lead + "  " + tail)
		} else {
			p.line(strings.TrimRight(lead, " "))
			p.para(moved, "     ")
		}
		if why := strings.TrimSpace(x.Why + " " + x.Do); !narrow && why != "" {
			p.para(why, "   ", sgrDim)
		}
		if l := x.Location; l != nil && (!narrow || !opened) {
			at := l.File
			if l.Line > 0 {
				at += ":" + strconv.Itoa(l.Line)
			}
			p.line("   " + p.style("open", sgrDim) + " " + p.style(truncLeft(at, p.width-8), sgrCyan))
			opened = true
		}
	}
}

// headsUp writes the warnings: one line counting them by rule, or with
// WriteOptions.Warnings every group with what it means and its packages.
func (p *printer) headsUp(c *narrate.Change) {
	if c.Warnings == 0 {
		return
	}
	p.line("")
	bang := " " + p.style("!", sgrBold, sgrYellow) + " "
	if !p.opts.Warnings {
		groups := make([]string, 0, len(c.WarningGroups))
		for _, g := range c.WarningGroups {
			groups = append(groups, g.Label+" "+strconv.Itoa(len(g.Packages)))
		}
		for i, l := range wrap(strconv.Itoa(c.Warnings)+" heads up: "+strings.Join(groups, ", "), p.width-3) {
			if i == 0 {
				p.line(bang + l)
			} else {
				p.line("   " + l)
			}
		}
		p.line("   " + p.style("astimate check --warnings lists them; they never block", sgrDim))
		return
	}
	p.line(bang + strconv.Itoa(c.Warnings) + " heads up  " + p.style("they never block", sgrDim))
	for _, g := range c.WarningGroups {
		p.line("")
		p.line("   " + p.style(g.Label, sgrBold))
		p.para(g.Why, "   ", sgrDim)
		for _, w := range g.Packages {
			f := narrate.Finding{Base: w.Base, Head: w.Head}
			v := values(&f)
			p.line("     " + p.style(truncLeft(w.Path, max(12, p.width-9-runes(v))), sgrCyan) + "  " + v)
			note := w.Limit
			if note != "" && w.Do != "" {
				note += ". "
			}
			p.para(note+w.Do, "       ", sgrDim)
		}
	}
}

// tail writes the exempted findings, each with its reason, the deleted
// packages, the notices and the exit code.
func (p *printer) tail(c *narrate.Change) {
	for _, e := range c.Exempted {
		p.line("")
		p.line(" exempted  " + p.style(e.Path, sgrCyan) + "  " + e.Label)
		p.para(e.Reason, "   ", sgrDim)
	}
	if len(c.Deleted) > 0 {
		p.line("")
		p.para("deleted since baseline: "+strings.Join(c.Deleted, ", "), " ")
	}
	if len(c.Notices) > 0 {
		p.line("")
		for _, n := range c.Notices {
			p.para(n, " ")
		}
	}
	if p.opts.Exit != nil {
		p.line("")
		p.line(" exit " + strconv.Itoa(*p.opts.Exit))
	}
}

// values is a finding's base → head, or its head alone without a base.
func values(f *narrate.Finding) string {
	if f.Base == nil {
		return narrate.Num(f.Head)
	}
	return narrate.Num(*f.Base) + " → " + narrate.Num(f.Head)
}
