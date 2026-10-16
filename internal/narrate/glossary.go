package narrate

import (
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/report"
)

// The measured evidence for the agent estimate (SPEC.md 7.4): the
// percentage of measured rebuilds that succeeded, for a package of one
// sitting and for one of a few.
const (
	// OneSittingRebuilt is the ONE_PASS tier's measured success rate.
	OneSittingRebuilt = 97
	// FewSittingsRebuilt is the FEW_PASSES tier's measured success rate.
	FewSittingsRebuilt = 78
)

// Glossary explains the report's words, with the numbers of limits, the
// duplicate-block floor minTokens, the churn window and the human
// estimate's basis. It is the one
// source of these sentences: the module report carries it as
// Module.Glossary and the MCP server's explain_metric answers from it, so
// the page and the tool say the same thing.
func Glossary(language string, limits []Limit, minTokens, window int, basis report.HumanBasis) []Term {
	out := []Term{
		{Term: "Sittings", Text: "How many times an AI agent would have to start over with a fresh context window to rewrite the package from its tests. " +
			"A sitting is a unit of the estimate, not literally one context window: one estimate token is about 7.6 tokens of an agent session's footprint. " +
			"The estimate is fitted on 85 measured agent rebuilds of 31 packages: a package of one sitting was rebuilt in " +
			strconv.Itoa(OneSittingRebuilt) + "% of measured runs (65 of 67), one of two or three sittings in " + strconv.Itoa(FewSittingsRebuilt) +
			"% (14 of 18). No measured package needed more than three; over three means the package has to be split before anyone can rebuild it. " +
			"Sittings lead the report: packages are ranked by how often they changed times their agent passes, then by the low end of the engineer time."},
		{Term: "Engineer time", Text: "How long one engineer would take to rewrite the package, as a range of working time: a secondary figure, " +
			"shown once beside the module's total and in each package's detail. rebuild.cocomo.unfm, how unfamiliar the engineer is with the code, " +
			"moves it most, inflating the lines by up to double. " + HumanBasis(basis)},
		{Term: "Split", Text: "The package is past what one agent pass can hold, or over two of its limits. Carve it into smaller packages before adding to it."},
		{Term: "Watch", Text: "Over one limit, large enough to need a few passes, or a quarter or more of it copied. Plan a split or a clean-up before it grows."},
	}
	if minTokens > 0 {
		out = append(out, Term{Term: "Copied", Text: "The share of lines inside duplicate blocks of " + strconv.Itoa(minTokens) +
			" or more tokens. Every copy is a second place a fix has to land."})
	}
	out = append(out, Term{Term: "Untested", Text: "Exported functions no test touches. A rebuild would have to guess their behaviour."})
	if window > 0 {
		out = append(out, Term{Term: "Changes", Text: "Commits touching the package in the last " + strconv.Itoa(window) +
			". Hot code is where a split pays back soonest."})
	}
	if l := limitsText(limits); l != "" {
		t := "Per package: " + l + "."
		if language == "go" {
			t += " Fitted from well-regarded Go modules."
		}
		out = append(out, Term{Term: "Limits", Text: t})
	}
	return append(out, Term{Term: "Hard to follow", Text: "Each function gets a cognitive complexity score: how much branching and nesting a reader has to hold in their head. Up to 10 is easy, 11 to 20 needs care, over 20 is hard and the usual linter limit. \"Paths\" is the cyclomatic count, the number of distinct routes through the function a test would have to cover."})
}

// Lookup finds the glossary term named name among terms. name is a term as
// the page prints it or as a tool argument: case, spaces, hyphens and
// underscores do not matter ("engineer_time", "Engineer time"). "Paths" is
// answered by the "Hard to follow" entry, which explains it, and
// "rebuild_cost", the entry the two rebuild terms were split from, by
// "Sittings".
func Lookup(terms []Term, name string) (Term, bool) {
	key := termKey(name)
	switch key {
	case "paths":
		key = "hard_to_follow"
	case "rebuild_cost":
		key = "sittings"
	}
	for _, t := range terms {
		if termKey(t.Term) == key {
			return t, true
		}
	}
	return Term{}, false
}

// termKey lower-cases s and joins its words with underscores.
func termKey(s string) string {
	sep := func(r rune) bool { return r == ' ' || r == '_' || r == '-' }
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), sep), "_")
}

// limitsText lists the limits as the glossary says them.
func limitsText(limits []Limit) string {
	parts := make([]string, 0, len(limits))
	for _, l := range limits {
		v := Num(l.Max)
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
