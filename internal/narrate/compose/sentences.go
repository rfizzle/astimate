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
		return "The package was already past what one agent pass can hold, and this change grew it more than one change is allowed to."
	case "largest_file_sloc":
		return "One file is carrying the growth, and a rebuild has to read all of it."
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
		return "close to the token limit for one agent pass"
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
		s = append(s, "It is over "+strconv.Itoa(len(p.Over))+" of its "+countWord(len(p.Ceilings))+" limits ("+
			strings.Join(labels, ", ")+")")
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
			return "Plan a split now; the gate fails the next change that grows it."
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
	return "It changed " + strconv.Itoa(p.Churn.Commits) + " times in the last " + strconv.Itoa(window) +
		" commits, so every one of those problems is in the way of day-to-day work."
}

// facts returns the labelled facts of a package's detail.
func facts(p *narrate.Package, window int) []narrate.Fact {
	out := []narrate.Fact{
		{Label: "Rebuild", Text: narrate.Num(p.Rebuild.AgentPasses) + " agent passes · " + narrate.Num(p.Rebuild.HumanDays) + " human days"},
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
	return append(out, narrate.Fact{Label: "Used by", Text: plural(p.FanIn, "internal package", "internal packages")})
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

// rebuildSay is the rebuild view's sentence.
func rebuildSay(r *narrate.Rebuild) string {
	h := r.HumanTime
	switch sittings(r.AgentPasses) {
	case 0:
		return "Small enough that an AI agent could rewrite it from its tests in one sitting. For one engineer, about " + h + "."
	case 1:
		return "An agent would need about " + r.SittingsText + ", carrying notes between them. For one engineer, about " + h + "."
	}
	return "Too big to rebuild as it stands: an agent would need about " + r.SittingsText +
		", so the package has to be split first. For one engineer, about " + h + "."
}

// copiesSay is the copies view's sentence.
func copiesSay(pct float64) string {
	switch {
	case pct >= 40:
		return "Nearly half of this code exists at least twice. A bug fixed in one copy stays alive in the others, and every copy is more for a rebuild to read."
	case pct >= 20:
		return "A large share of this code exists at least twice. A bug fixed in one copy stays alive in the others."
	case pct >= 5:
		return "Some of this code is repeated. Worth extracting when you are next in here, not urgent."
	}
	return "Very little repetition. Nothing to do."
}

// longest names the chunks; empty without any.
func longest(cs []narrate.Chunk) string {
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range cs {
		where := c.File + ":" + strconv.Itoa(c.Start) + "-" + strconv.Itoa(c.End)
		if i == 0 {
			b.WriteString("Longest repeat: " + where + " (" + strconv.Itoa(c.Lines) + " lines)")
			continue
		}
		b.WriteString(", again at " + where)
	}
	if len(cs) > 1 {
		b.WriteString(".")
	}
	return b.String()
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

// headline is the repository's one-sentence verdict.
func headline(m *narrate.Module) string {
	if m.Counts.Split == 0 {
		return m.Name + " has no package too big to rebuild in one go."
	}
	s := m.Name + " has " + plural(m.Counts.Split, "package", "packages") + " too big to rebuild in one go."
	if len(m.Hotspots) > 0 {
		s += " Start with " + m.Hotspots[0] + "."
	}
	return s
}

// lead is the paragraph under the headline; hottest is the split package
// with the highest hotspot score, nil when none has one.
func lead(m *narrate.Module, hottest *narrate.Package) string {
	c := m.Counts
	s := plural(c.Packages, "package", "packages") + ", about " + c.HumanTime +
		" of one engineer's time to rebuild from their tests. " + strconv.Itoa(c.Fine) + pick(c.Fine, " is", " are") + " fine, " +
		strconv.Itoa(c.Watch) + pick(c.Watch, " is", " are") + " close to a limit, and " + strconv.Itoa(c.Split) +
		pick(c.Split, " needs", " need") + " a split."
	if hottest != nil && hottest.Churn != nil && m.ChurnWindow > 0 {
		s += " The hottest of those changed " + strconv.Itoa(hottest.Churn.Commits) + " times in the last " +
			strconv.Itoa(m.ChurnWindow) + " commits and " + narrate.Num(hottest.Copies.Pct) + "% of it is copied code."
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
// any finding is on untested_exports.
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

// failingSummary counts a failing package's findings with its rebuild
// days and churn, each when known.
func failingSummary(n int, p *narrate.Package) string {
	s := plural(n, "finding", "findings")
	if p == nil {
		return s
	}
	s += " · " + narrate.Num(p.Rebuild.HumanDays) + " days to rebuild"
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
	if rule, _, _ := strings.Cut(f.Limit, ";"); f.Metric == "dup_blocks" && rule == "max_delta +0" {
		n := int(grew)
		return plural(n, "new duplicate block", "new duplicate blocks") + "; the rule allows none",
			plural(n, "new copy", "new copies") + ", none allowed"
	}
	return "", ""
}

// firstClause is the first clause of a gate suggestion, ending in a
// period.
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
	return s + "."
}

// Glossary explains the report's words, with the numbers of limits, the
// duplicate-block floor minTokens and the churn window.
func Glossary(language string, limits []narrate.Limit, minTokens, window int) []narrate.Term {
	out := []narrate.Term{
		{Term: "Rebuild cost", Text: "How long it would take to rewrite the package from its tests. Shown as one engineer's working time, and as agent sittings: how many times an AI agent would have to start over with a fresh context window. One sitting means it fits in one go; over three means the package has to be split before anyone can rebuild it."},
		{Term: "Split", Text: "The package is past what one agent pass can hold, or over two of its limits. Carve it into smaller packages before adding to it."},
		{Term: "Watch", Text: "Over one limit, or large enough to need a few passes. The next growth here will start failing the gate."},
	}
	if minTokens > 0 {
		out = append(out, narrate.Term{Term: "Copied", Text: "The share of lines inside duplicate blocks of " + strconv.Itoa(minTokens) +
			" or more tokens. Every copy is a second place a fix has to land."})
	}
	out = append(out, narrate.Term{Term: "Untested", Text: "Exported functions no test touches. A rebuild would have to guess their behaviour."})
	if window > 0 {
		out = append(out, narrate.Term{Term: "Changes", Text: "Commits touching the package in the last " + strconv.Itoa(window) +
			". Hot code is where a split pays back soonest."})
	}
	if l := limitsText(limits); l != "" {
		t := "Per package: " + l + "."
		if language == "go" {
			t += " Fitted from well-regarded Go modules."
		}
		out = append(out, narrate.Term{Term: "Limits", Text: t})
	}
	return append(out, narrate.Term{Term: "Hard to follow", Text: "Each function gets a cognitive complexity score: how much branching and nesting a reader has to hold in their head. Up to 10 is easy, 11 to 20 needs care, over 20 is hard and the usual linter limit. \"Paths\" is the cyclomatic count, the number of distinct routes through the function a test would have to cover."})
}

// limitsText lists the limits as the glossary says them.
func limitsText(limits []narrate.Limit) string {
	parts := make([]string, 0, len(limits))
	for _, l := range limits {
		v := narrate.Num(l.Max)
		switch l.Metric {
		case "sloc":
			parts = append(parts, v+" lines")
		case "tokens_est":
			parts = append(parts, v+" tokens")
		case "largest_file_sloc":
			parts = append(parts, "a "+v+"-line largest file")
		case "exported_symbols":
			parts = append(parts, v+" exports")
		case "internal_imports":
			parts = append(parts, v+" internal imports")
		}
	}
	return strings.Join(parts, ", ")
}

// human is days as one engineer's working time: days under a week, weeks
// under a month, months under a year, else engineer-years.
func human(d float64) string {
	switch {
	case d >= daysPerYear:
		return plural(int(math.Round(d/daysPerYear)), "engineer-year", "engineer-years")
	case d >= daysPerMonth:
		return plural(int(math.Round(d/daysPerMonth)), "month", "months")
	case d >= daysPerWeek:
		return plural(int(math.Round(d/daysPerWeek)), "week", "weeks")
	}
	return plural(max(1, int(math.Round(d))), "day", "days")
}

// humanShort is human in a table cell's units.
func humanShort(d float64) string {
	switch {
	case d >= daysPerYear:
		return narrate.Num(round1(d/daysPerYear)) + " yr"
	case d >= daysPerMonth:
		return strconv.Itoa(int(math.Round(d/daysPerMonth))) + " mo"
	case d >= daysPerWeek:
		return strconv.Itoa(int(math.Round(d/daysPerWeek))) + " wk"
	}
	return strconv.Itoa(max(1, int(math.Round(d)))) + " d"
}

// plural is n followed by one or many.
func plural(n int, one, many string) string {
	return strconv.Itoa(n) + " " + pick(n, one, many)
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
