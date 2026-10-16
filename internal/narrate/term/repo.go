package term

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/narrate"
)

// Counts of the repository report.
const (
	// startHere is how many hotspots "Start here" lists.
	startHere = 3
	// tableRows is how many packages the table lists without
	// WriteOptions.All.
	tableRows = 10
)

// repo writes the repository report: the header, the verdict, the counts,
// "Start here" and "Every package".
func (p *printer) repo(m *narrate.Module) {
	meta := make([]string, 0, 3)
	for _, s := range []string{m.ModulePath, short(m.Head), m.ConfigVersion} {
		if s != "" {
			meta = append(meta, s)
		}
	}
	head := p.style(m.Name, sgrBold)
	if len(meta) > 0 {
		head += "  " + p.style(strings.Join(meta, " · "), sgrDim)
	}
	p.line(head)
	p.line("")
	p.para(m.Headline, "", sgrBold)
	c := &m.Counts
	parts := []string{count(c.Packages, "package", "packages"), c.HumanTime + " to rebuild",
		strconv.Itoa(c.Fine) + " fine", strconv.Itoa(c.Watch) + " watch", strconv.Itoa(c.Split) + " split",
		strconv.Itoa(c.WithoutTests) + " without tests"}
	codes := []string{"", "", sgrGreen, sgrYellow, sgrRed, ""}
	p.fit(parts, "", "", func(i int) string { return p.style(parts[i], codes[i]) })
	p.para(c.HumanBasisShort, "", sgrDim)
	if bar := p.bar(float64(c.Fine), float64(c.Packages), 20); bar != "" {
		p.line(p.style(bar, sgrGreen) + "  " + p.style("share of packages that are fine", sgrDim))
	}
	byPath := make(map[string]*narrate.Package, len(m.Packages))
	for i := range m.Packages {
		byPath[m.Packages[i].Path] = &m.Packages[i]
	}
	p.hotspots(m, byPath)
	p.table(m.Packages)
}

// hotspots writes "Start here": the first hotspots, each with its band,
// time, changes and bar, its story and what to do.
func (p *printer) hotspots(m *narrate.Module, byPath map[string]*narrate.Package) {
	n := min(startHere, len(m.Hotspots))
	if n == 0 {
		return
	}
	maxHot := 0.0
	for _, path := range m.Hotspots[:n] {
		if pk := byPath[path]; pk != nil {
			maxHot = max(maxHot, pk.Hotspot)
		}
	}
	sub := "by days to rebuild"
	if maxHot > 0 {
		sub = "changes in the last " + strconv.Itoa(m.ChurnWindow) + " commits × days to rebuild"
	}
	p.line("")
	p.heading("Start here", sub)
	for i, path := range m.Hotspots[:n] {
		pk := byPath[path]
		if pk == nil {
			continue
		}
		cells := []string{pk.Rebuild.HumanTime}
		if pk.Churn != nil {
			cells = append(cells, count(pk.Churn.Commits, "change", "changes"))
		}
		bar := p.bar(pk.Hotspot, maxHot, 12)
		if bar != "" {
			cells = append(cells, bar)
		}
		num := " " + strconv.Itoa(i+1) + "  "
		band := bandWord(pk.Band)
		rest := "  " + band + "  " + strings.Join(cells, "  ")
		pathW := max(12, p.width-runes(num)-runes(rest))
		p.line("")
		p.line(p.style(num, sgrBold) + p.style(truncLeft(path, pathW), sgrCyan) + "  " +
			p.style(band, sgrBold, bandColor(pk.Band)) + "  " + strings.Join(cells, "  "))
		p.para(pk.Story, "    ")
		p.para("Do this → "+pk.Todo, "    ", sgrDim)
	}
}

// column is one column of the package table.
type column struct {
	head  string
	right bool
	cell  func(pk *narrate.Package) string
}

// columns returns the table's columns after the path: every column from
// wideWidth, else without the copied and untested ones.
func (p *printer) columns() []column {
	cols := []column{
		{"BAND", false, func(pk *narrate.Package) string { return bandWord(pk.Band) }},
		{"TIME", true, func(pk *narrate.Package) string { return pk.Rebuild.HumanShort }},
		{"LINES ", true, func(pk *narrate.Package) string {
			if slices.Contains(pk.Over, "sloc") {
				return narrate.Num(float64(pk.Copies.Lines)) + "*"
			}
			return narrate.Num(float64(pk.Copies.Lines)) + " "
		}},
		{"COPIED", true, func(pk *narrate.Package) string { return narrate.Num(pk.Copies.Pct) + "%" }},
		{"UNTESTED", true, func(pk *narrate.Package) string { return strconv.Itoa(pk.Tests.Untested) }},
		{"CHANGES", true, func(pk *narrate.Package) string {
			if pk.Churn == nil {
				return "-"
			}
			return strconv.Itoa(pk.Churn.Commits)
		}},
	}
	if p.width < wideWidth {
		cols = slices.Delete(cols, 3, 5)
	}
	return cols
}

// table writes "Every package": the costliest packages by rebuild days,
// every one with WriteOptions.All, and the trailer naming --all and the
// other formats (WriteOptions.Formats).
func (p *printer) table(ps []narrate.Package) {
	if len(ps) == 0 {
		return
	}
	rows := make([]*narrate.Package, len(ps))
	for i := range ps {
		rows[i] = &ps[i]
	}
	slices.SortStableFunc(rows, func(a, b *narrate.Package) int {
		return cmp.Or(cmp.Compare(b.Rebuild.HumanDays, a.Rebuild.HumanDays), strings.Compare(a.Path, b.Path))
	})
	hidden := 0
	if !p.opts.All && len(rows) > tableRows {
		rows, hidden = rows[:tableRows], len(rows)-tableRows
	}
	cols := p.columns()
	cells := make([][]string, len(rows))
	widths := make([]int, len(cols))
	pathW := runes("PACKAGE")
	for i, pk := range rows {
		pathW = max(pathW, runes(pk.Path))
		cells[i] = make([]string, len(cols))
		for j, col := range cols {
			cells[i][j] = col.cell(pk)
			widths[j] = max(widths[j], runes(col.head), runes(cells[i][j]))
		}
	}
	rest := 1
	for _, w := range widths {
		rest += 2 + w
	}
	pathW = max(12, min(pathW, p.width-rest))
	p.line("")
	p.heading("Every package", "by days to rebuild · * is over its size limit")
	p.line("")
	head := " " + pad("PACKAGE", pathW, false)
	for j, col := range cols {
		head += "  " + pad(col.head, widths[j], col.right)
	}
	p.line(p.style(strings.TrimRight(head, " "), sgrDim))
	for i, pk := range rows {
		row := " " + p.style(pad(truncLeft(pk.Path, pathW), pathW, false), sgrCyan)
		for j, col := range cols {
			cell := pad(cells[i][j], widths[j], col.right)
			if j == 0 {
				cell = p.style(cell, bandColor(pk.Band))
			}
			row += "  " + cell
		}
		p.line(strings.TrimRight(row, " "))
	}
	trailer := make([]string, 0, 4)
	if hidden > 0 {
		trailer = append(trailer, "… "+strconv.Itoa(hidden)+" more", "--all for every row")
	}
	for _, f := range p.opts.Formats {
		if f != "text" {
			trailer = append(trailer, "--format "+f)
		}
	}
	if len(trailer) == 0 {
		return
	}
	p.fit(trailer, " ", " ", func(i int) string { return p.style(trailer[i], sgrDim) })
}

// short is a full commit hash cut to eight characters; any other text is
// kept.
func short(s string) string {
	if len(s) == 40 && strings.Trim(s, "0123456789abcdef") == "" {
		return s[:8]
	}
	return s
}
