package html

import (
	"math"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/narrate"
)

// shownWarned is how many packages a heads-up card names before "and N
// more".
const shownWarned = 6

// changeView is the "This change" tab.
type changeView struct {
	C        *narrate.Change
	Fail     bool
	Eyebrow  string
	Stickers []sticker
	Failing  []failCard
	Warns    []warnCard
	// Passed lists the changed packages that pass clean.
	Passed string
}

// failCard is one failing package.
type failCard struct {
	F        *narrate.Failing
	Findings []findingView
}

// findingView is one violation with its growth meter.
type findingView struct {
	F        *narrate.Finding
	Change   string
	Grew     string
	Allowed  string
	HasMeter bool
	Open     string
}

// warnCard is one heads-up group.
type warnCard struct {
	G     *narrate.WarningGroup
	Count string
	Who   string
}

// newChange computes the change tab of m, which has a change.
func newChange(m *narrate.Module) *changeView {
	c := m.Change
	v := &changeView{C: c, Fail: c.Verdict == "FAIL", Eyebrow: eyebrow(m), Passed: strings.Join(c.PassedPaths, ", ")}
	if c.BaseCommit != "" {
		v.Eyebrow += " against " + shortCommit(c.BaseCommit)
	} else if c.Base != "" {
		v.Eyebrow += " against " + c.Base
	}
	v.Stickers = []sticker{
		{narrate.BandSplit, strconv.Itoa(c.Failed), "fail"},
		{narrate.BandWatch, strconv.Itoa(c.Warnings), "heads up"},
		{narrate.BandFine, strconv.Itoa(c.Passed), "pass clean"},
	}
	if c.Cross != nil {
		v.Stickers = append(v.Stickers, sticker{"ink", fmtPtr(c.Cross.Base) + "→" + fmtNum(c.Cross.Head), "copies between packages"})
	}
	for i := range c.Failing {
		f := &c.Failing[i]
		card := failCard{F: f}
		for j := range f.Findings {
			card.Findings = append(card.Findings, newFinding(&f.Findings[j]))
		}
		v.Failing = append(v.Failing, card)
	}
	for i := range c.WarningGroups {
		v.Warns = append(v.Warns, newWarn(&c.WarningGroups[i]))
	}
	return v
}

// newFinding scales an over-max finding's growth against what one change
// may add.
func newFinding(f *narrate.Finding) findingView {
	v := findingView{F: f, Change: fmtPtr(f.Base) + " → " + fmtNum(f.Head)}
	if f.Location != nil {
		v.Open = f.Location.File + ":" + strconv.Itoa(f.Location.Line)
	}
	if f.Base != nil && f.Allowed != nil && f.Moved != "" {
		grew := f.Head - *f.Base
		top := math.Max(math.Max(grew, *f.Allowed), 1)
		v.HasMeter, v.Grew, v.Allowed = true, pct(math.Max(0, grew), top), pct(*f.Allowed, top)
	}
	return v
}

// newWarn names the first packages of a heads-up group with their values.
func newWarn(g *narrate.WarningGroup) warnCard {
	n := len(g.Packages)
	w := warnCard{G: g, Count: strconv.Itoa(n) + " " + plural(n, "package", "packages")}
	names := make([]string, 0, min(n, shownWarned)+1)
	for _, p := range g.Packages[:min(n, shownWarned)] {
		names = append(names, p.Path+" "+fmtPtr(p.Base)+"→"+fmtNum(p.Head))
	}
	if n > shownWarned {
		names = append(names, "and "+strconv.Itoa(n-shownWarned)+" more")
	}
	w.Who = strings.Join(names, " · ")
	return w
}
