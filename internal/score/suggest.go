package score

import (
	"math"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

const (
	// driverShareMin is the share of rebuild_tokens a driver must reach to
	// get a suggestion.
	driverShareMin = 0.10
	// maxNamed is how many function names a suggestion lists before
	// summarizing the rest as "and N more".
	maxNamed = 5
)

// Names carries the identifiers behind some counts in RawMetrics, for
// suggestions that name what to fix. A caller fills what its extractor
// exposes; the zero value is valid and yields suggestions with counts only.
type Names struct {
	// UntestedExports names the exported functions and methods no test
	// references.
	UntestedExports []string
	// DupLocations lists duplicate block locations, for example
	// "parse.go:40-58", first occurrence first.
	DupLocations []string
	// ChangedFunction names the most complex function added or modified
	// since the baseline, the one changed_func_cognitive_max reports, with
	// its location when known, for example "Parser.next (parse.go:40)".
	ChangedFunction string
	// CrossBlocks lists the cross-package duplicate blocks behind
	// dup_blocks_cross_pkg, first occurrence first, with files relative to
	// the module root.
	CrossBlocks []metrics.CrossBlock
}

// DriverSuggestions returns one suggestion per driver of r, in driver order,
// for each driver whose tokens are at least 10% of r.RebuildTokens. m supplies
// the metric values quoted in the text and n the names, when known.
func DriverSuggestions(r Rebuild, m metrics.RawMetrics, n Names) []string {
	ds := Drivers(r)
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		if d.Tokens < driverShareMin*r.RebuildTokens {
			continue
		}
		if s := driverSuggestion(d, &m, n); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// driverSuggestion renders the template for one driver's term.
func driverSuggestion(d Driver, m *metrics.RawMetrics, n Names) string {
	tokens := formatNum(d.Tokens)
	switch d.Term {
	case TermVolume:
		s := "The package is " + strconv.Itoa(m.TokensEst) + " tokens of non-test source"
		if m.DuplicationPct > 0 {
			s += ", " + formatPct(m.DuplicationPct) + "% of it duplicated"
			if len(n.DupLocations) > 0 {
				s += ", starting with " + n.DupLocations[0]
			}
		}
		return s + "; its volume is " + tokens + " tokens of the rebuild; split the package to shrink it."
	case TermSpec:
		return "Tests are " + tokens + " tokens of the rebuild context; they are the spec a rebuild is checked against."
	case TermContract:
		return count(m.ExportedSymbols, "exported symbol", "exported symbols") + " used by " +
			count(m.FanIn, "internal package", "internal packages") + " cost " + tokens +
			" tokens of the rebuild; narrow the exported surface."
	case TermUnspecified:
		return untestedSentence(m.UntestedExports, n.UntestedExports)
	case TermHidden:
		return count(m.Globals, "package-level variable", "package-level variables") + " and " +
			count(m.InitFuncs, "init function", "init functions") + " are hidden state worth " + tokens +
			" tokens of the rebuild; pass state explicitly instead."
	default:
		return ""
	}
}

// MetricSuggestion returns the fix suggestion for a gate violation or warning
// on metric, whose value at head is head. m supplies related values quoted in
// the text and n the names, when known. It returns "" for a metric without a
// template; every metric with a default threshold in SPEC.md 8.2 has one, and
// so do dup_blocks_cross_pkg and the informational flags uses_cgo,
// uses_reflect and generated_files, which a config may gate.
func MetricSuggestion(metric string, head float64, m metrics.RawMetrics, n Names) string {
	v := formatNum(head)
	switch metric {
	case "dup_blocks":
		return dupSentence(int(head), formatPct(m.DuplicationPct), n.DupLocations)
	case "duplication_pct":
		return dupSentence(m.DupBlocks, formatPct(head), n.DupLocations)
	case "untested_exports":
		return untestedSentence(int(head), n.UntestedExports)
	case "globals":
		return count(int(head), "package-level variable holds", "package-level variables hold") + " state no signature reveals; pass it explicitly or move it into a struct."
	case "init_funcs":
		return count(int(head), "init function runs", "init functions run") + " hidden setup on import; replace them with explicit constructors."
	case "max_nesting":
		return "Nesting reaches depth " + v + "; flatten with early returns or extract the inner blocks."
	case "cognitive_p90":
		return "The 90th-percentile function has cognitive complexity " + v + "; split the most complex functions."
	case "changed_func_cognitive_max":
		fn := "A changed function"
		if n.ChangedFunction != "" {
			fn = "Changed function " + n.ChangedFunction
		}
		return fn + " has cognitive complexity " + v + "; split it into smaller functions or flatten its branching."
	case "tokens_est":
		return "The package is " + v + " tokens of non-test source; split it so a rebuild fits one agent pass."
	case "largest_file_sloc":
		return "The largest file has " + v + " source lines; split it by responsibility."
	case "exported_symbols":
		return "The package exports " + count(int(head), "symbol", "symbols") + "; narrow the surface or split the package."
	case "internal_imports":
		return "The package imports " + count(int(head), "internal package", "internal packages") + "; cut dependencies or split the package."
	case "sloc":
		return "The package has " + v + " source lines; split it before the next feature."
	case "has_tests":
		return "The package has " + strconv.Itoa(m.SLOC) + " source lines and no tests; add tests that pin its behavior."
	case "dup_blocks_cross_pkg":
		return crossSentence(int(head), n.CrossBlocks)
	case "uses_cgo":
		return "The package imports \"C\"; cgo code is opaque to Go analysis and needs a C toolchain to build, so keep it behind a narrow Go API."
	case "uses_reflect":
		return "The package imports reflect or unsafe; what they do is invisible in signatures, so keep their use small and tested."
	case "generated_files":
		return count(int(head), "file is", "files are") + " generated; change the generator or its input, not the output."
	default:
		return ""
	}
}

// dupSentence renders the duplication template, citing the first location
// when one is known.
func dupSentence(blocks int, pct string, locations []string) string {
	s := count(blocks, "duplicate block covers", "duplicate blocks cover") + " " + pct + "% of lines; extract shared helpers"
	if len(locations) > 0 {
		s += ", starting with " + locations[0]
	}
	return s + "."
}

// crossSentence renders the cross-package duplication template for n
// blocks. When blocks names any, it cites the first block's first
// occurrence and its first occurrence in another package, so the sentence
// names both sides of the copy.
func crossSentence(n int, blocks []metrics.CrossBlock) string {
	s := count(n, "duplicate block is", "duplicate blocks are") + " shared with other packages; "
	where := crossPair(blocks)
	switch {
	case where == "" && n == 1:
		return s + "extract it into one place."
	case where == "":
		return s + "extract them into one place."
	case n == 1:
		return s + "extract the shared block in " + where + " into one package."
	default:
		return s + "extract each into one package, starting with the block in " + where + "."
	}
}

// crossPair renders the first occurrence of the first of blocks and its
// first occurrence in another package as "a/a.go:12-40 and b/b.go:8-36",
// followed by "(and N more)" for further occurrences, or "" when blocks
// names no pair.
func crossPair(blocks []metrics.CrossBlock) string {
	if len(blocks) == 0 || len(blocks[0].Occurrences) < 2 {
		return ""
	}
	occ := blocks[0].Occurrences
	first, second := occ[0], occ[1]
	for _, o := range occ[1:] {
		if o.Package != first.Package {
			second = o
			break
		}
	}
	s := occurrenceText(first) + " and " + occurrenceText(second)
	if more := len(occ) - 2; more > 0 {
		s += " (and " + strconv.Itoa(more) + " more)"
	}
	return s
}

// occurrenceText renders o as "file:start-end".
func occurrenceText(o metrics.Occurrence) string {
	return o.File + ":" + strconv.Itoa(o.StartLine) + "-" + strconv.Itoa(o.EndLine)
}

// untestedSentence renders the untested-exports template for n exports,
// naming up to five of names, in the singular when n is 1.
func untestedSentence(n int, names []string) string {
	s := count(n, "exported function has", "exported functions have") + " no test"
	if len(names) > 0 {
		s += " (" + nameList(names, n) + ")"
	}
	if n == 1 {
		return s + "; a rebuild would have to reverse-engineer its behavior."
	}
	return s + "; a rebuild would have to reverse-engineer their behavior."
}

// nameList joins up to five names and summarizes the rest as "and N more",
// where the total is the larger of total and len(names).
func nameList(names []string, total int) string {
	shown := names[:min(len(names), maxNamed)]
	s := strings.Join(shown, ", ")
	if more := max(total, len(names)) - len(shown); more > 0 {
		s += " and " + strconv.Itoa(more) + " more"
	}
	return s
}

// count renders n followed by the singular or plural phrase.
func count(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

// formatNum renders a metric or token value rounded to whole units, since
// every value it quotes is a count or a token total.
func formatNum(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// formatPct renders a percentage to at most one decimal.
func formatPct(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
}
