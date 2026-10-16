// Package term writes the module report (SPEC.md 10.3) for a terminal:
// the repository report and the change report of the report mockup's
// terminal tab. Every sentence it prints is a field of the report; it adds
// only labels, numbers and layout. A severity is a word first (FAIL, PASS,
// SPLIT, WATCH, FINE, ✗, !) and a colour second, so the output reads the
// same without colour; colour and bars appear only when asked for. It
// imports narrate and the standard library only.
package term

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rfizzle/astimate/internal/narrate"
)

// Layout widths.
const (
	// defaultWidth is the width when the options give none.
	defaultWidth = 100
	// wideWidth is the width from which the package table shows the
	// copied and untested columns.
	wideWidth = 100
	// narrowWidth is the width at which, and under which, a finding is
	// stacked on fewer lines, as the mockup's 80-column block shows.
	narrowWidth = 80
)

// ANSI SGR codes of the styles.
const (
	sgrBold   = "1"
	sgrDim    = "2"
	sgrRed    = "31"
	sgrGreen  = "32"
	sgrYellow = "33"
	sgrCyan   = "36"
)

// Write writes m to w for a terminal: the repository report, then, when m
// has a change, the change report; with opts.ChangeOnly the change report
// alone, as check prints it.
func Write(w io.Writer, m *narrate.Module, opts narrate.WriteOptions) error {
	p := &printer{opts: opts, width: opts.Width}
	if p.width <= 0 {
		p.width = defaultWidth
	}
	if !opts.ChangeOnly || m.Change == nil {
		p.repo(m)
	}
	if m.Change != nil {
		if !opts.ChangeOnly {
			p.line("")
		}
		p.change(m)
	}
	if _, err := io.WriteString(w, p.b.String()); err != nil {
		return fmt.Errorf("writing terminal report: %w", err)
	}
	return nil
}

// printer lays the report out into b.
type printer struct {
	b     strings.Builder
	opts  narrate.WriteOptions
	width int
}

// line writes s and a line break.
func (p *printer) line(s string) {
	p.b.WriteString(s)
	p.b.WriteByte('\n')
}

// style wraps s in the SGR codes, empty ones skipped, when colour is on.
func (p *printer) style(s string, codes ...string) string {
	if !p.opts.Color || s == "" {
		return s
	}
	set := make([]string, 0, len(codes))
	for _, c := range codes {
		if c != "" {
			set = append(set, c)
		}
	}
	if len(set) == 0 {
		return s
	}
	return "\x1b[" + strings.Join(set, ";") + "m" + s + "\x1b[0m"
}

// bar is a bar of n cells filled in proportion v/max, empty without
// colour, where bars drop away with it.
func (p *printer) bar(v, maxV float64, n int) string {
	if !p.opts.Color || maxV <= 0 {
		return ""
	}
	full := int(max(0, min(1, v/maxV))*float64(n) + 0.5)
	return strings.Repeat("█", full) + strings.Repeat("░", n-full)
}

// para writes text wrapped to the width, each line behind indent, in
// codes.
func (p *printer) para(text, indent string, codes ...string) {
	for _, l := range wrap(text, p.width-runes(indent)) {
		p.line(indent + p.style(l, codes...))
	}
}

// heading writes a section's title and, beside it when it fits, else
// under it, its dimmed subtitle.
func (p *printer) heading(title, sub string) {
	if runes(title)+2+runes(sub) <= p.width {
		p.line(p.style(title, sgrBold) + "  " + p.style(sub, sgrDim))
		return
	}
	p.line(p.style(title, sgrBold))
	p.para(sub, "", sgrDim)
}

// fit writes parts joined by " · ", breaking between parts where a line
// would pass the width; the first line behind first, the rest behind
// indent. styled returns a part as printed.
func (p *printer) fit(parts []string, first, indent string, styled func(i int) string) {
	var b strings.Builder
	b.WriteString(first)
	n := runes(first)
	for i, part := range parts {
		if i > 0 {
			if n+3+runes(part) > p.width {
				p.line(b.String())
				b.Reset()
				b.WriteString(indent)
				n = runes(indent)
			} else {
				b.WriteString(" · ")
				n += 3
			}
		}
		b.WriteString(styled(i))
		n += runes(part)
	}
	p.line(b.String())
}

// bandWord is a band as its word.
func bandWord(band string) string { return strings.ToUpper(band) }

// bandColor is a band's colour.
func bandColor(band string) string {
	switch band {
	case narrate.BandSplit:
		return sgrRed
	case narrate.BandWatch:
		return sgrYellow
	}
	return sgrGreen
}

// count is n followed by one or many.
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// runes is the printed width of s: one column a rune.
func runes(s string) int { return utf8.RuneCountInString(s) }

// pad pads s with spaces to n columns, on the left when right is set.
func pad(s string, n int, right bool) string {
	gap := n - runes(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

// truncLeft shortens s to n columns from the left, marking the cut with
// "…".
func truncLeft(s string, n int) string {
	r := []rune(s)
	if len(r) <= n || n < 1 {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

// wrap breaks text into lines of at most width columns at spaces; a word
// longer than width gets a line of its own.
func wrap(text string, width int) []string {
	var lines []string
	var cur strings.Builder
	n := 0
	for _, word := range strings.Fields(text) {
		wn := runes(word)
		if n > 0 && n+1+wn > width {
			lines = append(lines, cur.String())
			cur.Reset()
			n = 0
		}
		if n > 0 {
			cur.WriteByte(' ')
			n++
		}
		cur.WriteString(word)
		n += wn
	}
	if n > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}
