package markdown

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/narrate"
)

// Marker is the hidden comment that leads a pull request comment, so the
// next run finds it and updates it in place.
const Marker = "<!-- astimate-report -->"

// Options shape the Markdown.
type Options struct {
	// Artifact names the HTML report written beside the run; when set,
	// the change block ends with a pointer to it.
	Artifact string
}

// rows is the number of packages the repository table lists.
const rows = 10

// Write writes m as Markdown: the change block first when m has one, then
// the repository report: the headline, the lead, the three packages to
// start with and the ten largest packages.
func Write(w io.Writer, m *narrate.Module, opts Options) error {
	var b strings.Builder
	if m.Change != nil {
		change(&b, m.Change, opts)
		b.WriteString("\n")
	}
	repository(&b, m)
	return write(w, &b)
}

// WriteChange writes the change block of m alone, which is what a pull
// request comment and a step summary carry. A report without a change
// section is an error.
func WriteChange(w io.Writer, m *narrate.Module, opts Options) error {
	if m.Change == nil {
		return errors.New("writing markdown change report: the report has no change section")
	}
	var b strings.Builder
	change(&b, m.Change, opts)
	return write(w, &b)
}

func write(w io.Writer, b *strings.Builder) error {
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing markdown report: %w", err)
	}
	return nil
}

// change lays out the change block: the verdict heading, one table row per
// finding, the heads-ups by rule, the passed count, the cross-package
// copies and the pointer to the HTML report.
func change(b *strings.Builder, c *narrate.Change, opts Options) {
	b.WriteString("## astimate: " + c.Verdict + " — ")
	switch {
	case c.Failed > 0:
		verb := " need attention\n"
		if c.Failed == 1 {
			verb = " needs attention\n"
		}
		b.WriteString(strconv.Itoa(c.Failed) + " of " + count(c.Changed, "changed package", "changed packages") + verb)
	case c.Changed == 0:
		b.WriteString("no package changed\n")
	default:
		b.WriteString(count(c.Changed, "changed package", "changed packages") + ", nothing needs attention\n")
	}
	if c.Violations > 0 {
		b.WriteString("\n| Package | What moved | Change | Open |\n|---|---|---|---|\n")
		for i := range c.Failing {
			f := &c.Failing[i]
			for j := range f.Findings {
				v := &f.Findings[j]
				open := ""
				if loc := v.Location; loc != nil && loc.File != "" {
					open = loc.File
					if loc.Line > 0 {
						open += ":" + strconv.Itoa(loc.Line)
					}
					open = code(open)
				}
				b.WriteString("| " + code(f.Path) + " | " + cell(v.Label) + " | " + number(v.Base, "new") + " → " +
					number(&v.Head, "") + " | " + open + " |\n")
			}
		}
	}
	var tail []string
	if c.Warnings > 0 {
		s := count(c.Warnings, "heads-up", "heads-ups")
		if len(c.WarningGroups) > 0 {
			labels := make([]string, len(c.WarningGroups))
			for i, g := range c.WarningGroups {
				labels[i] = g.Label
			}
			s += " (" + strings.Join(labels, ", ") + ")"
		}
		tail = append(tail, s)
	}
	if c.Passed > 0 {
		tail = append(tail, count(c.Passed, "package passes", "packages pass")+" clean")
	}
	var line []string
	if len(tail) > 0 {
		line = append(line, strings.Join(tail, ", ")+".")
	}
	if x := c.Cross; x != nil {
		if x.Base != nil {
			line = append(line, "Copies between packages "+number(x.Base, "")+" → "+number(&x.Head, "")+".")
		} else {
			line = append(line, "Copies between packages "+number(&x.Head, "")+".")
		}
	}
	if len(line) > 0 {
		b.WriteString("\n" + strings.Join(line, " ") + "\n")
	}
	if opts.Artifact != "" {
		b.WriteString("Full report: " + code(opts.Artifact) + " (CI artifact).\n")
	}
}

// repository lays out the repository report.
func repository(b *strings.Builder, m *narrate.Module) {
	b.WriteString("## " + m.Headline + "\n\n" + m.Lead + "\n\n_" + m.Counts.HumanBasisShort + "_\n")
	var start []*narrate.Package
	for _, path := range m.Hotspots[:min(3, len(m.Hotspots))] {
		if i := slices.IndexFunc(m.Packages, func(p narrate.Package) bool { return p.Path == path }); i >= 0 {
			start = append(start, &m.Packages[i])
		}
	}
	if len(start) > 0 {
		b.WriteString("\n### Start here\n")
		for i, p := range start {
			b.WriteString("\n" + strconv.Itoa(i+1) + ". " + code(p.Path) + " (" + p.Band + "): " + p.Rebuild.SittingsText + ", " + p.Rebuild.HumanShort + " for one engineer")
			if p.Churn != nil {
				b.WriteString(", " + count(p.Churn.Commits, "change", "changes"))
			}
			b.WriteString("\n   " + p.Story + "\n   Do this: " + p.Todo + "\n")
		}
	}
	all := slices.Clone(m.Packages)
	slices.SortStableFunc(all, func(a, c narrate.Package) int { return narrate.Hotter(&a, &c) })
	order := "Costliest to rebuild first"
	if slices.ContainsFunc(m.Packages, func(p narrate.Package) bool { return p.Hotspot > 0 }) {
		order = "By changes × sittings"
	}
	b.WriteString("\n### Every package\n\n" + order + "; * marks a package over its size limit.\n\n")
	b.WriteString("| Package | Band | Sittings | Time | Lines | Copied | Untested | Changes |\n|---|---|---|---|---|---|---|---|\n")
	for i := range all[:min(rows, len(all))] {
		p := &all[i]
		star := ""
		if slices.Contains(p.Over, "sloc") {
			star = "*"
		}
		changes := "n/a"
		if p.Churn != nil {
			changes = strconv.Itoa(p.Churn.Commits)
		}
		b.WriteString("| " + code(p.Path) + " | " + p.Band + " | " + p.Rebuild.SittingsText + " | " + p.Rebuild.HumanShort + " | " +
			number(ptr(float64(p.Copies.Lines)), "") + star + " | " + number(ptr(p.Copies.Pct), "") + "% | " +
			strconv.Itoa(p.Tests.Untested) + " | " + changes + " |\n")
	}
	if len(all) > rows {
		b.WriteString("\n… " + strconv.Itoa(len(all)-rows) + " more packages; `astimate report --format json` has every row.\n")
	}
}

func ptr(v float64) *float64 { return &v }

// code wraps s in a code span, safe inside a table cell.
func code(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return "`" + strings.ReplaceAll(s, "`", "'") + "`"
}

// cell escapes s for a table cell.
func cell(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
}

// count formats n with the singular or plural noun.
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// number formats v with thousands separators and at most one decimal; nil
// is none.
func number(v *float64, none string) string {
	if v == nil {
		return none
	}
	s := strings.TrimSuffix(strconv.FormatFloat(*v, 'f', 1, 64), ".0")
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	var out strings.Builder
	for i := range len(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteByte(whole[i])
	}
	if hasFrac {
		out.WriteString("." + frac)
	}
	return sign + out.String()
}
