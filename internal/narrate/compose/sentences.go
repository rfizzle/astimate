package compose

import (
	"math"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/narrate"
)

// This file holds every sentence the module report says, so the glossary
// and explain_metric can serve the same words. A sentence states only
// numbers its row carries; a clause whose number is missing is dropped.

// metricLabel names a metric for people.
func metricLabel(metric string) string {
	switch metric {
	case "sloc":
		return "size"
	case "tokens_est":
		return "tokens"
	case "largest_file_sloc":
		return "largest file"
	case "exported_symbols":
		return "exports"
	case "internal_imports":
		return "imports"
	}
	return strings.ReplaceAll(metric, "_", " ")
}

// driverLabel says what a rebuild driver is; empty for the terms the
// rebuild view lumps into the rest.
func driverLabel(term string) string {
	switch term {
	case "spec":
		return "the tests it must satisfy"
	case "volume":
		return "the amount of code"
	}
	return ""
}

// why says why the gate cares about a violation of metric; empty for a
// rule without a sentence.
func why(metric string) string {
	switch metric {
	case "dup_blocks":
		return "A new copy is a second place every future fix has to land."
	case "sloc":
		return "The package is past what one agent pass can hold, and this change took it there or grew it more than one change is allowed to."
	case "tokens_est":
		return "The package is past what one agent pass can read, and this change took it there or grew it more than one change is allowed to."
	case "largest_file_sloc":
		return "One file is carrying the growth, and a rebuild has to read all of it."
	case "globals":
		return "Shared mutable state is a dependency no signature shows, so callers and tests rely on it without saying so."
	case "untested_exports":
		return "Nothing pins what an untested export does, so a rebuild would have to guess it."
	case "internal_imports":
		return "Each internal import is another package to understand before this one can be rebuilt."
	case "exported_symbols":
		return "A wider surface is more to reverse-engineer and more callers to keep working."
	case "changed_func_cognitive_max":
		return "A function this complex is the one a rebuild gets wrong."
	}
	return ""
}

// warnWhy says what a warning on metric means, the limits' numbers taken
// from limits; empty for a metric without a sentence.
func warnWhy(metric string, limits []narrate.Limit) string {
	ceiling := func() string {
		for _, l := range limits {
			if l.Metric == metric {
				return narrate.Num(l.Max)
			}
		}
		return ""
	}
	switch metric {
	case "sloc":
		return "close to or over the size limit"
	case "tokens_est":
		return "close to or over the token limit for one agent pass"
	case "untested_exports":
		return "new exports without a test"
	case "globals":
		return "new package-level state"
	case "cognitive_p90":
		return "complexity drifting up"
	}
	c := ceiling()
	if c == "" {
		return ""
	}
	switch metric {
	case "largest_file_sloc":
		return "a file close to or over " + c + " lines"
	case "exported_symbols":
		return "close to or over " + c + " exports"
	case "internal_imports":
		return "close to or over " + c + " internal imports"
	}
	return ""
}

// story says what is wrong with a package.
func story(p *narrate.Package) string {
	s := make([]string, 0, 4)
	switch len(p.Over) {
	case 0:
		s = append(s, "It is inside every limit")
	case 1:
		s = append(s, "It is over its "+metricLabel(p.Over[0])+" limit")
	default:
		labels := make([]string, len(p.Over))
		for i, m := range p.Over {
			labels[i] = metricLabel(m)
		}
		over := strconv.Itoa(len(p.Over)) + " of its " + countWord(len(p.Ceilings)) + " limits"
		if len(p.Over) == len(p.Ceilings) {
			over = "all " + countWord(len(p.Ceilings)) + " of its limits"
		}
		s = append(s, "It is over "+over+" ("+strings.Join(labels, ", ")+")")
	}
	if p.Copies.Pct >= 20 {
		s = append(s, narrate.Num(p.Copies.Pct)+"% of its lines are copies of each other")
	}
	if p.Tests.Untested >= 5 {
		s = append(s, strconv.Itoa(p.Tests.Untested)+" of its "+strconv.Itoa(p.Tests.Exports)+" exports have no test")
	}
	if !p.Tests.HasTests {
		s = append(s, "it has no tests at all")
	}
	return strings.Join(s, ", and ") + "."
}

// todo says what to do about a package.
func todo(p *narrate.Package) string {
	switch p.Band {
	case narrate.BandSplit:
		if p.Copies.Pct >= 20 {
			return "Extract the copied code first, then split the package along its responsibilities."
		}
		return "Split it into smaller packages before the next feature lands here."
	case narrate.BandWatch:
		if len(p.Over) > 0 {
			return "Plan a split now, before it grows further past its limit."
		}
		if p.Copies.Pct >= watchDupPct {
			return "Extract the copied code next time you work here; nothing needs a split yet."
		}
		return "Nothing yet. Keep an eye on it."
	}
	return "Nothing. Leave it alone."
}

// changes says how often a package changed; empty without churn.
func changes(p *narrate.Package, window int) string {
	if p.Churn == nil || window <= 0 {
		return ""
	}
	if p.Churn.Commits == 0 {
		return "It did not change in the last " + strconv.Itoa(window) + " commits."
	}
	return "It changed " + plural(p.Churn.Commits, "time", "times") + " in the last " + strconv.Itoa(window) +
		" commits, so every one of those problems is in the way of day-to-day work."
}

// facts returns the labelled facts of a package's detail.
func facts(p *narrate.Package, window int) []narrate.Fact {
	out := []narrate.Fact{
		{Label: "Rebuild", Text: narrate.Num(p.Rebuild.AgentPasses) + " agent passes · " +
			narrate.Num(float64(p.Rebuild.RebuildTokens)) + " rebuild tokens"},
		{Label: "Copies", Text: plural(p.Copies.Blocks, "duplicate block covers ", "duplicate blocks cover ") + narrate.Num(p.Copies.Pct) + "% of lines"},
		{Label: "Complexity", Text: "cognitive p90 " + strconv.Itoa(p.Complexity.CognitiveP90) + " · cyclomatic p90 " +
			strconv.Itoa(p.Complexity.CyclomaticP90) + " · nesting " + strconv.Itoa(p.Complexity.Nesting)},
		{Label: "Tests", Text: plural(p.Tests.TestFuncs, "test function", "test functions") + " · " + strconv.Itoa(p.Tests.Untested) + " of " +
			strconv.Itoa(p.Tests.Exports) + " exports untested"},
	}
	if p.Churn != nil && window > 0 {
		out = append(out, narrate.Fact{Label: "Changes", Text: strconv.Itoa(p.Churn.Commits) + " commits by " +
			plural(p.Churn.Authors, "author", "authors") + " in the last " + strconv.Itoa(window)})
	}
	return append(out, narrate.Fact{Label: "Used by", Text: plural(p.FanIn, "internal package", "internal packages")},
		narrate.Fact{Label: "Engineer time", Text: humanDays(p.Rebuild.HumanDaysLow, p.Rebuild.HumanDaysHigh) +
			" for one engineer, from two textbook models, not measured"})
}

// sittingsBands are the labels of the sittings bands, by sittings.
func sittingsBand(tier int) string {
	return [...]string{"one sitting", "a few sittings", "split it first"}[tier]
}

// sittingsText is passes as the sittings an agent needs.
func sittingsText(passes float64) string {
	if passes <= oneSitting {
		return "one sitting"
	}
	return strconv.Itoa(int(math.Ceil(passes))) + " sittings"
}

// rebuildSay is the rebuild view's sentence, with the measured evidence
// for its sittings band.
func rebuildSay(r *narrate.Rebuild) string {
	switch sittings(r.AgentPasses) {
	case 0:
		return "Small enough that an AI agent could rewrite it from its tests in one sitting; a package of one sitting was rebuilt in " +
			strconv.Itoa(narrate.OneSittingRebuilt) + "% of measured runs."
	case 1:
		return "An agent would need about " + r.SittingsText + ", carrying notes between them; a package of a few sittings was rebuilt in " +
			strconv.Itoa(narrate.FewSittingsRebuilt) + "% of measured runs."
	}
	return "Too big to rebuild as it stands: an agent would need about " + r.SittingsText +
		", so the package has to be split first. No measured package needed more than three sittings."
}

// copiesSay is the copies view's sentence.
func copiesSay(pct float64) string {
	switch {
	case pct >= 50:
		return "Half or more of this code exists at least twice. A bug fixed in one copy stays alive in the others, and every copy is more for a rebuild to read."
	case pct >= 40:
		return "Nearly half of this code exists at least twice. A bug fixed in one copy stays alive in the others, and every copy is more for a rebuild to read."
	case pct >= 20:
		return "A large share of this code exists at least twice. A bug fixed in one copy stays alive in the others."
	case pct >= 5:
		return "Some of this code is repeated. Worth extracting when you are next in here, not urgent."
	}
	return "Very little repetition. Nothing to do."
}

// longest names the chunks, two occurrences of the longest duplicate
// block; empty without any. Two that overlap are said to.
func longest(cs []narrate.Chunk) string {
	if len(cs) == 0 {
		return ""
	}
	where := func(c narrate.Chunk) string { return c.File + ":" + strconv.Itoa(c.Start) + "-" + strconv.Itoa(c.End) }
	s := "Longest repeat: " + where(cs[0]) + " (" + plural(cs[0].Lines, "line", "lines") + ")"
	if len(cs) > 1 {
		s += ", again at " + where(cs[1])
		if a, b := cs[0], cs[1]; a.File == b.File && a.Start <= b.End && b.Start <= a.End {
			s += ", overlapping it: the same code back to back"
		}
	}
	return s + "."
}

// complexitySay is the complexity view's sentence; empty without
// functions.
func complexitySay(c *narrate.Complexity) string {
	if c.Functions == 0 {
		return ""
	}
	var s string
	switch b := c.Buckets; {
	case b == nil:
	case b.Hard == 0 && b.Care == 0:
		s = "Every function here is easy to follow. "
	case b.Hard == 0:
		s = "Most functions are easy to follow; " + strconv.Itoa(b.Care) + pick(b.Care, " needs", " need") + " some care. "
	case b.Hard == 1:
		s = "1 function is hard to follow: it branches and nests enough that a rewrite would likely get it wrong. Start with the ones below. "
	default:
		s = strconv.Itoa(b.Hard) + " functions are hard to follow: they branch and nest enough that a rewrite would likely get them wrong. Start with the ones below. "
	}
	return s + "The hardest tenth of functions score " + strconv.Itoa(c.CognitiveP90) + " or more; code nests up to " +
		strconv.Itoa(c.Nesting) + " levels deep."
}

// headline is the repository's one-sentence verdict; first is the first
// hotspot, nil when there is none, named when it is a split package.
func headline(m *narrate.Module, first *narrate.Package) string {
	if m.Counts.Split == 0 {
		return m.Name + " has no package that needs a split."
	}
	s := m.Name + " has " + plural(m.Counts.Split, "package that needs", "packages that need") + " a split."
	if first != nil && first.Band == narrate.BandSplit {
		s += " Start with " + first.Path + "."
	}
	return s
}

// lead is the paragraph under the headline; hottest is the split package
// with the highest hotspot score, nil when none has one, and churned
// reports whether churn was measured for any package.
func lead(m *narrate.Module, hottest *narrate.Package, churned bool) string {
	c := m.Counts
	s := plural(c.Packages, "package", "packages") + "; " + strconv.Itoa(c.MultiSitting) + pick(c.MultiSitting, " needs", " need") +
		" more than one agent sitting to rebuild from their tests. In measured agent rebuilds, a package of one sitting was rebuilt in " +
		strconv.Itoa(narrate.OneSittingRebuilt) + "% of runs and one of a few sittings in " + strconv.Itoa(narrate.FewSittingsRebuilt) + "%. " +
		strconv.Itoa(c.Fine) + pick(c.Fine, " is", " are") + " fine, " +
		strconv.Itoa(c.Watch) + pick(c.Watch, " needs", " need") + " watching, and " + strconv.Itoa(c.Split) +
		pick(c.Split, " needs", " need") + " a split."
	if hottest != nil && hottest.Churn != nil && m.ChurnWindow > 0 {
		s += " The hottest of those changed " + plural(hottest.Churn.Commits, "time", "times") + " in the last " +
			strconv.Itoa(m.ChurnWindow) + " commits and " + narrate.Num(hottest.Copies.Pct) + "% of it is copied code."
	}
	if !churned && len(m.Hotspots) > 0 {
		s += " There is no change history to read, so the packages to start with are ranked by rebuild time alone."
	}
	return s
}

// changeHeadline is the change ribbon's sentence.
func changeHeadline(c *narrate.Change) string {
	if c.Failed == 0 {
		return "Nothing needs attention before this merges."
	}
	return plural(c.Failed, "package", "packages") + pick(c.Failed, " needs", " need") + " attention before this merges."
}

// changeLead is the paragraph under the ribbon; untested reports whether
// any finding is on untested_exports or has_tests.
func changeLead(c *narrate.Change, untested bool) string {
	s := plural(c.Changed, "package", "packages") + " changed. " + strconv.Itoa(c.Failed) + " fail" +
		pick(c.Failed, "s", "") + " the gate with " + plural(c.Violations, "finding", "findings") + ". " +
		strconv.Itoa(c.Passed) + " pass" + pick(c.Passed, "es", "") + " clean"
	if !untested {
		s += ", and nothing new was left untested"
	}
	return s + "."
}

// passedSummary says how many changed packages passed and how many did
// not change.
func passedSummary(c *narrate.Change) string {
	return strconv.Itoa(c.Passed) + " changed " + pick(c.Passed, "package passes", "packages pass") + " clean, " +
		strconv.Itoa(c.Unchanged) + " did not change"
}

// failingSummary counts a failing package's findings with the sittings an
// agent needs to rebuild it and its churn, each when known.
func failingSummary(n int, p *narrate.Package) string {
	s := plural(n, "finding", "findings")
	if p == nil {
		return s
	}
	s += " · " + [...]string{"rebuilds in one sitting", p.Rebuild.SittingsText + " to rebuild", "split before a rebuild"}[sittings(p.Rebuild.AgentPasses)]
	if p.Churn != nil {
		s += " · " + plural(p.Churn.Commits, "change lately", "changes lately")
	}
	return s
}

// moved says how far a finding moved against what is allowed, for a page
// and for a terminal line; both empty when it cannot say.
func moved(f *narrate.Finding) (page, term string) {
	if f.Base == nil {
		return "", ""
	}
	grew := f.Head - *f.Base
	if f.Allowed != nil {
		return "grew " + narrate.Num(grew) + ", allowed " + narrate.Num(*f.Allowed) + " while over the limit",
			"grew " + narrate.Num(grew) + ", allowed " + narrate.Num(*f.Allowed)
	}
	if rule, _, _ := strings.Cut(f.Limit, ";"); rule == "max_delta +0" {
		n := int(grew)
		switch f.Metric {
		case "dup_blocks":
			return plural(n, "new duplicate block", "new duplicate blocks") + "; the rule allows none",
				plural(n, "new copy", "new copies") + ", none allowed"
		case "dup_blocks_cross_pkg":
			return plural(n, "new duplicate block", "new duplicate blocks") + " shared with other packages; the rule allows none",
				plural(n, "new copy", "new copies") + " across packages, none allowed"
		}
	}
	return "", ""
}

// firstClause is the first clause of a gate suggestion, ending in a
// period, its first letter upper-cased: it follows the why sentence.
func firstClause(s string) string {
	i := strings.Index(s, ". ")
	if j := strings.Index(s, "; "); j >= 0 && (i < 0 || j < i) {
		i = j
	}
	if i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return ""
	}
	return capitalize(s) + "."
}

// timeUnit is a unit of one engineer's working time: its length in
// working days and its names, singular, plural and in a table cell.
type timeUnit struct {
	days             float64
	one, many, short string
}

// unitOf is the unit days are written in: days under a week, weeks under
// a month, months under a year, else engineer-years.
func unitOf(days float64) timeUnit {
	units := [...]timeUnit{
		{daysPerYear, "engineer-year", "engineer-years", "yr"},
		{daysPerMonth, "month", "months", "mo"},
		{daysPerWeek, "week", "weeks", "wk"},
	}
	for _, u := range units {
		if days >= u.days {
			return u
		}
	}
	return timeUnit{1, "day", "days", "d"}
}

// count is days in whole units, at least 1.
func (u timeUnit) count(days float64) int {
	return max(1, int(math.Round(days/u.days)))
}

// human is the range low to high days as one engineer's working time, in
// the unit of the high end, each end a whole number of it and at least 1:
// "3 to 5 weeks", or "4 weeks" when both ends round the same.
func human(low, high float64) string {
	u := unitOf(high)
	lo, hi := u.count(low), u.count(high)
	if lo == hi {
		return plural(hi, u.one, u.many)
	}
	return strconv.Itoa(lo) + " to " + plural(hi, u.one, u.many)
}

// humanShort is human in a table cell's units, "3–5 wk", the years to one
// decimal.
func humanShort(low, high float64) string {
	u := unitOf(high)
	cell := func(d float64) string { return strconv.Itoa(u.count(d)) }
	if u.days == daysPerYear {
		cell = func(d float64) string { return narrate.Num(round1(d / daysPerYear)) }
	}
	lo, hi := cell(low), cell(high)
	if lo == hi {
		return hi + " " + u.short
	}
	return lo + "–" + hi + " " + u.short
}

// plural is n followed by one or many.
func plural(n int, one, many string) string {
	return strconv.Itoa(n) + " " + pick(n, one, many)
}

// humanDays writes the range of human days, never below 1 as human does,
// and "day" when it writes a single 1.
func humanDays(low, high float64) string {
	r := narrate.NumRange(max(low, 1), max(high, 1))
	if r == "1" {
		return r + " human day"
	}
	return r + " human days"
}

// pick returns one when n is 1, else many.
func pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// countWord spells a count of limits up to five.
func countWord(n int) string {
	if n >= 0 && n <= 5 {
		return [...]string{"zero", "one", "two", "three", "four", "five"}[n]
	}
	return strconv.Itoa(n)
}
