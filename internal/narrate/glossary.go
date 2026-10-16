package narrate

import (
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/report"
)

// Glossary explains the report's words, with the numbers of limits, the
// duplicate-block floor minTokens, the churn window and the human
// estimate's basis. It is the one
// source of these sentences: the module report carries it as
// Module.Glossary and the MCP server's explain_metric answers from it, so
// the page and the tool say the same thing.
func Glossary(language string, limits []Limit, minTokens, window int, basis report.HumanBasis) []Term {
	out := []Term{
		{Term: "Rebuild cost", Text: "How long it would take to rewrite the package from its tests. Shown as one engineer's working time, and as agent sittings: how many times an AI agent would have to start over with a fresh context window. One sitting means it fits in one go; over three means the package has to be split before anyone can rebuild it. " + HumanBasis(basis)},
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
// underscores do not matter ("rebuild_cost", "Rebuild cost"). "Paths" is
// answered by the "Hard to follow" entry, which explains it.
func Lookup(terms []Term, name string) (Term, bool) {
	key := termKey(name)
	if key == "paths" {
		key = "hard_to_follow"
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
