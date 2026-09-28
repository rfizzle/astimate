// Package report renders the Markdown report calibration/fit writes
// alongside a candidate configuration or a language override block,
// describing the pooled distribution behind every fitted limit.
package report

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/calibration/fit/internal/pool"
	"github.com/rfizzle/astimate/calibration/fit/internal/stats"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// writer returns a printf-style function that writes to dst, an
// io.Writer's Write through fmt.Fprintf without its error, since a
// strings.Builder never fails to write.
func writer(dst *strings.Builder) func(format string, args ...any) {
	return func(format string, args ...any) { fmt.Fprintf(dst, format, args...) }
}

// numRow formats vs as Num, joined by " | " for a Markdown table row.
func numRow(vs ...float64) string {
	cells := make([]string, len(vs))
	for i, v := range vs {
		cells[i] = Num(v)
	}
	return strings.Join(cells, " | ")
}

// Input is what the Markdown report describes.
type Input struct {
	// Version is the candidate's config_version.
	Version string
	// BaseVersion is the base configuration's config_version.
	BaseVersion string
	// Data and Candidate are the input rows and output config paths.
	Data, Candidate string
	// Rows is the number of pooled rows; Modules the distinct modules.
	Rows    int
	Modules []string
	// Provisional says the data holds only the standard library.
	Provisional bool
	// Choices are the fitted rules in config order.
	Choices []pool.Choice
	// CrossPkg is the distribution of dup_blocks_cross_pkg over the
	// data's module rows; its N is 0 when the data has none.
	CrossPkg stats.Stats
	// ModuleRows is the number of module rows pooled, and ModulesData the
	// modules.jsonl they were read from, empty when none was given.
	ModuleRows  int
	ModulesData string
	// Previous is an earlier configuration to compare the candidate's
	// limits with, and PreviousPath its file; nil for no comparison.
	Previous     *config.Config
	PreviousPath string
	// Language is the language of a per-language override fit, empty for
	// a whole configuration; the base's top-level rules are then Go's.
	Language string
	// BaseData is the rows the base was fitted from, and BaseStats each
	// choice's distribution over them, index for index; nil when not
	// given.
	BaseData  string
	BaseStats []stats.Stats
	// Run is the collector's run.json beside the data, read for a
	// per-language fit to describe the corpus; nil when absent.
	Run *RunInfo
}

// RunInfo is the part of the collector's run.json the report reads.
type RunInfo struct {
	// Corpus is the corpus file the collection read.
	Corpus string `json:"corpus"`
	// AstimateCommit is the astimate commit the collector ran at.
	AstimateCommit string `json:"astimate_commit"`
	// Modules are the collected corpus entries.
	Modules []struct {
		Module     string   `json:"module"`
		Commit     string   `json:"commit"`
		CommitDate string   `json:"commit_date"`
		Packages   int      `json:"packages"`
		Roots      []string `json:"roots"`
		Excluded   int      `json:"excluded_packages"`
		Error      string   `json:"error"`
	} `json:"modules"`
}

// writeCorpus writes the corpus the data was collected from, one row per
// repository, from the collector's run.json.
func writeCorpus(b *strings.Builder, in *Input) {
	w := writer(b)
	r := in.Run
	w("## Corpus\n\n")
	w("`%s`, %d repositories collected at astimate `%s`. Module roots are the workspace packages collected, each ranked on its own; ", r.Corpus, len(r.Modules), shortCommit(r.AstimateCommit))
	w("excluded packages are test, fixture, example, benchmark and documentation directories left out of the pool.\n\n")
	w("| Repository | Commit | Commit date | Module roots | Packages | Excluded |\n| --- | --- | --- | ---: | ---: | ---: |\n")
	for _, m := range r.Modules {
		w("| `%s` | `%s` | %s | %d | %d | %d |\n", m.Module, shortCommit(m.Commit), m.CommitDate, len(m.Roots), m.Packages, m.Excluded)
	}
	w("\n")
}

// shortCommit abbreviates a commit hash to 12 characters.
func shortCommit(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// MethodText states the fitting rules; the report header and the
// candidate's leading comment both carry it.
func MethodText() []string {
	return []string{
		"Percentiles are nearest-rank: the p-th percentile of n values is the value at rank ceil(p/100 * n), so it is always an observed value. IQR is p75 minus p25.",
		"A capacity rule's max, and a density rule's max where the base rule has one, is the 90th percentile rounded to two significant figures and then to the nearest readable step: 500 above 1000, 50 above 100, 5 above 10, otherwise 1 (0.5 for a percentage). A capacity max is at least one step.",
		"A density rule's max_delta is a quarter of the IQR rounded up to a whole step, at least 1 for a count and 0.5 for a percentage, except that a rule whose base max_delta is 0 keeps it: zero tolerance on new duplicate blocks, untested exports, globals, init functions and nesting is a policy, not a statistic.",
		"internal_imports is pooled from cloned-module rows only, since the standard library is loaded as one module and counts every standard-library import as internal; a module-wide metric (dup_blocks_cross_pkg) is pooled from the module rows only, one per module, since the gate evaluates it there alone; every other metric is pooled from all package rows.",
		"changed_func_cognitive_max is a diff against a baseline, so it is fitted per function with every function of every row counted as new, as in a package new at head: its max is the 99th percentile of per-function cognitive complexity, rounded the same way, since a single function past the corpus's own worst percentile is the signal.",
		"Kinds, warn_at, ratchet_from_zero, when guards, requirement rules, the rebuild parameters and every other key are copied from the base unchanged; a density rule with no max in the base gets none.",
	}
}

// RenderReport writes the Markdown report.
func RenderReport(in *Input) string {
	var b strings.Builder
	w := writer(&b)

	if in.Language != "" {
		w("# Threshold override %s\n\n", in.Version)
		w("The `languages.%s` override block of the configuration (SPEC.md 9 and 13), fitted from %s rows alone. ", in.Language, in.Language)
		w("The base's top-level rules, fitted to Go, are what every other language is judged by; ")
		w("\"Base\" below means them, and \"Candidate\" the %s override. ", in.Language)
		w("Each rule the data fitted a statistic for replaces the top-level rule on its metric for %s; every other rule is inherited. ", in.Language)
		w("The rebuild parameters are not overridden: they cannot be calibrated from a corpus and wait for the rebuild experiments of SPEC.md 11.2.\n\n")
	} else {
		w("# Threshold candidate %s\n\n", in.Version)
	}
	if in.Provisional {
		w("**Provisional.** Every pooled row is from the Go standard library; no external module has been collected yet. ")
		w("This candidate is not the shipped default and must not replace it; refit once the corpus in `calibration/corpus.yaml` is collected.\n\n")
	}
	w("- Data: `%s`, %d packages from %d module(s): %s\n", in.Data, in.Rows, len(in.Modules), strings.Join(quoted(in.Modules), ", "))
	if in.ModulesData != "" {
		w("- Module rows: `%s`, %d `%s` rows\n", in.ModulesData, in.ModuleRows, metrics.ModuleRowID)
	}
	w("- Base configuration: `%s`\n", in.BaseVersion)
	w("- Candidate: `%s`\n", in.Candidate)
	w("- Generated by `go run ./calibration/fit`\n\n")

	w("## Method\n\n")
	for _, line := range MethodText() {
		w("- %s\n", line)
	}
	w("\n\"Fail as new\" counts pooled packages that would violate the rule as a package new at head, with no baseline: ")
	w("above max, above max_delta from zero where ratchet_from_zero is set, or not meeting a requirement whose guard holds. ")
	w("For `%s` it counts functions above max, and its section also counts the packages holding one.\n\n", pool.FuncMetric)

	w("## Summary\n\n")
	w("Base is `%s`, candidate `%s`. Rows counts the rows the metric was measured on and names which rows fed it.\n\n",
		in.BaseVersion, in.Version)
	w("| Metric | Kind | Rows | p90 | IQR | Base max | Candidate max | Base max_delta | Candidate max_delta | Fail as new, base | Fail as new, candidate |\n| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for i := range in.Choices {
		c := &in.Choices[i]
		p90, iqr := "none", "none"
		if c.Stats.N > 0 {
			p90, iqr = Num(c.Stats.P90), Num(c.Stats.IQR)
		}
		w("| `%s` | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", c.Rule.Metric, c.Rule.Kind, rowsUsed(c),
			p90, iqr, Opt(c.Rule.Max), Opt(c.Max), Opt(c.Rule.MaxDelta), Opt(c.MaxDelta),
			share(c.OverBase, c.Stats.N), share(c.OverCandidate, c.Stats.N))
	}
	w("\n")

	if in.Language != "" {
		writeOverride(&b, in)
	}

	if in.Run != nil {
		writeCorpus(&b, in)
	}

	if in.BaseStats != nil {
		writeBaseData(&b, in)
	}

	if in.Previous != nil {
		writePrevious(&b, in)
	}

	w("## What changed most\n\n")
	writeChanges(&b, in)

	writeFunctions(&b, in)

	writeCrossPkg(&b, in)

	w("## Not fitted\n\n")
	writeUnfitted(&b, in)

	w("## Per metric\n\n")
	for i := range in.Choices {
		writeMetric(&b, &in.Choices[i])
	}
	return b.String()
}

// writeChanges writes the moves of the fitted limits, largest first, and
// what the data behind them implies.
func writeChanges(b *strings.Builder, in *Input) {
	w := writer(b)
	moved := make([]*pool.Choice, 0, len(in.Choices))
	for i := range in.Choices {
		c := &in.Choices[i]
		if c.Max != nil && c.Rule.Max != nil && *c.Max != *c.Rule.Max {
			moved = append(moved, c)
		}
	}
	slices.SortStableFunc(moved, func(x, y *pool.Choice) int {
		return cmp.Compare(logRatio(*y.Max, *y.Rule.Max), logRatio(*x.Max, *x.Rule.Max))
	})
	for _, c := range moved {
		dir := "loosens"
		if *c.Max < *c.Rule.Max {
			dir = "tightens"
		}
		move := Num(*c.Rule.Max) + " to " + Num(*c.Max)
		overBase, overCand := share(c.OverBase, c.Stats.N), share(c.OverCandidate, c.Stats.N)
		if c.Pool == pool.PoolFunctions {
			w("- `%s` max %s: %s. The per-function median is %s, p90 %s and p99 %s; %s of the pooled functions are above the base max and %s above the candidate.\n",
				c.Rule.Metric, dir, move, Num(c.Stats.P50), Num(c.Stats.P90), Num(c.Stats.P99), overBase, overCand)
			continue
		}
		unit := "packages"
		if c.Pool == pool.PoolModule {
			unit = "modules"
		}
		w("- `%s` max %s: %s. The pooled median is %s and p90 is %s; as new %s, %s of the pool failed the base max and %s fail the candidate.\n",
			c.Rule.Metric, dir, move, Num(c.Stats.P50), Num(c.Stats.P90), unit, overBase, overCand)
	}
	var moves, pinned []string
	for i := range in.Choices {
		c := &in.Choices[i]
		switch {
		case c.DeltaPinned:
			pinned = append(pinned, "`"+c.Rule.Metric+"`")
		case c.MaxDelta != nil && c.Rule.MaxDelta != nil && *c.MaxDelta != *c.Rule.MaxDelta:
			moves = append(moves, fmt.Sprintf("`%s` %s to %s", c.Rule.Metric, Num(*c.Rule.MaxDelta), Num(*c.MaxDelta)))
		}
	}
	if len(moves) > 0 {
		w("- max_delta moves on %s: a quarter of each IQR.\n", strings.Join(moves, ", "))
	}
	if len(pinned) > 0 {
		w("- max_delta stays 0 on %s whatever the IQR: zero tolerance on these is a policy, not a statistic (SPEC.md 11.1).\n",
			strings.Join(pinned, ", "))
	}
	w("\n")
	if in.Provisional {
		w("The standard library is the reference for idiomatic Go but not a typical module: it is low level, ")
		w("carries platform variants and literal data tables, and, measured as one module, counts every standard-library import as `internal_imports` (see `calibration/corpus.md`). ")
		w("Limits fitted from it alone describe the standard library, not the ecosystem. ")
		w("The data has no external modules yet; treat every number here as provisional until the corpus run.\n\n")
	}
}

// writeUnfitted names the gated metrics no row measures, which keep their
// base values, and describes the module-wide dup_blocks_cross_pkg, which
// no base rule gates, from the data's module rows or their absence.
func writeUnfitted(b *strings.Builder, in *Input) {
	written := false
	inner := writer(b)
	w := func(format string, args ...any) { written = true; inner(format, args...) }
	for i := range in.Choices {
		c := &in.Choices[i]
		switch {
		case c.Stats.N > 0:
		case in.Language != "":
			w("- `%s`: no %s row measures it, so the override sets no rule and the top-level rule applies (max %s, max_delta %s). ",
				c.Rule.Metric, in.Language, Opt(c.Rule.Max), Opt(c.Rule.MaxDelta))
			w("The gate skips a rule whose metric is null at head, so it never fires on a %s package whose extractor leaves the metric null.\n", in.Language)
		case c.Pool == pool.PoolModule:
			w("- `%s`: the data has no `%s` rows, so the base values are kept (max %s, max_delta %s). ",
				c.Rule.Metric, metrics.ModuleRowID, Opt(c.Max), Opt(c.MaxDelta))
			w("Collect them with `calibration/collect --modules-only` and fit with `--modules`.\n")
		case c.Pool == pool.PoolFunctions:
			w("- `%s`: no row counts its functions by cognitive complexity (`func_cognitive`), so the base values are kept (max %s, max_delta %s). ",
				c.Rule.Metric, Opt(c.Max), Opt(c.MaxDelta))
			w("Recollect the data with the current `calibration/collect` to fit it.\n")
		default:
			w("- `%s`: no row measures it, so the base values are kept (max %s, max_delta %s).\n",
				c.Rule.Metric, Opt(c.Max), Opt(c.MaxDelta))
		}
	}
	if in.CrossPkg.N == 0 && crossRule(in) == nil && in.Language == "" {
		w("- `dup_blocks_cross_pkg`: the data has no `%s` rows, so the module-wide metric has no distribution here and no default rule gates it.\n",
			metrics.ModuleRowID)
	}
	if !written {
		w("Every gated metric was measured.\n")
	}
	w("\n")
}

// crossRule returns the choice fitted on the module rows, nil when the base
// has no rule on a module-wide metric.
func crossRule(in *Input) *pool.Choice {
	for i := range in.Choices {
		if in.Choices[i].Pool == pool.PoolModule {
			return &in.Choices[i]
		}
	}
	return nil
}

// writeCrossPkg writes the distribution of dup_blocks_cross_pkg over the
// module rows, when the data has any, and the rule it feeds, if the base
// has one.
func writeCrossPkg(b *strings.Builder, in *Input) {
	w := writer(b)
	s := &in.CrossPkg
	if s.N == 0 {
		return
	}
	w("## Cross-package duplication\n\n")
	w("`dup_blocks_cross_pkg` on the module rows (`%s`), one per cloned module; the standard library has none (see `calibration/notes/cross-package-duplication-2026-09-28.md`):\n\n",
		metrics.ModuleRowID)
	w("| Modules | p25 | p50 | p75 | p90 | max |\n| ---: | ---: | ---: | ---: | ---: | ---: |\n")
	w("| %d | %s |\n\n", s.N, numRow(s.P25, s.P50, s.P75, s.P90, s.Max))
	c := crossRule(in)
	if c == nil {
		w("No base rule gates it. Per SPEC.md 11.1 a density rule's max would be p90 %s rounded to %s.\n\n",
			Num(s.P90), Num(stats.RoundReadable(s.P90, false)))
		return
	}
	w("The rule is evaluated on the module row only. Its max_delta %s is kept by policy; its max, which judges a module with no baseline, is p90 %s rounded to %s. ",
		Opt(c.MaxDelta), Num(s.P90), Opt(c.Max))
	w("As new modules, %s of the pooled modules fail the base rule and %s the candidate.\n\n",
		share(c.OverBase, c.Stats.N), share(c.OverCandidate, c.Stats.N))
}

// rowsUsed describes the rows a metric was measured on: the count and the
// pool.
func rowsUsed(c *pool.Choice) string {
	return strconv.Itoa(c.Stats.N) + ", " + c.Pool
}

// writeMetric writes one metric's section: the rows it was measured on,
// its percentiles, histogram and limits.
func writeMetric(b *strings.Builder, c *pool.Choice) {
	w := writer(b)
	s := &c.Stats
	w("### `%s` (%s)\n\n", c.Rule.Metric, c.Rule.Kind)
	w("Rows: %s.\n\n", rowsUsed(c))
	w("| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |\n| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	w("| %d | %s |\n\n", s.N, numRow(s.Min, s.P25, s.P50, s.P75, s.P90, s.P95, s.Max, s.IQR))
	if len(s.Hist) > 0 {
		unit := "Packages"
		switch c.Pool {
		case pool.PoolFunctions:
			unit = "Functions"
		case pool.PoolModule:
			unit = "Modules"
		}
		w("| Range | %s | Share |\n| --- | ---: | ---: |\n", unit)
		closed := len(s.Hist) - 1
		for i, bin := range s.Hist {
			var rng string
			switch {
			case bin.Open:
				rng = "> " + edge(bin.Lo) + " (to " + edge(bin.Hi) + ")"
			case i == closed-1:
				rng = "[" + edge(bin.Lo) + ", " + edge(bin.Hi) + "]"
			default:
				rng = "[" + edge(bin.Lo) + ", " + edge(bin.Hi) + ")"
			}
			w("| %s | %d | %s |\n", rng, bin.Count, pct(bin.Count, s.N))
		}
		w("\n")
	}
	switch c.Rule.Kind {
	case gate.Requirement:
		guard := ""
		if c.Rule.When != nil {
			guard = " when " + c.Rule.When.Metric + " > " + Num(c.Rule.When.Value)
		}
		w("Requirement kept: require %t%s. Fail as new: %s.\n\n", c.Rule.Require != nil && *c.Rule.Require, guard,
			share(c.OverCandidate, s.N))
	default:
		w("| | max | max_delta | Fail as new |\n| --- | ---: | ---: | ---: |\n")
		w("| Base | %s | %s | %s |\n", Opt(c.Rule.Max), Opt(c.Rule.MaxDelta), share(c.OverBase, s.N))
		w("| Candidate | %s | %s | %s |\n\n", Opt(c.Max), Opt(c.MaxDelta), share(c.OverCandidate, s.N))
	}
}

// writePrevious compares every rule's limits in the candidate with those
// of the earlier configuration in.Previous, rule by rule in the
// candidate's order, and names the rules that moved.
func writePrevious(b *strings.Builder, in *Input) {
	w := writer(b)
	prev := make(map[string]*gate.Threshold, len(in.Previous.Thresholds))
	for i := range in.Previous.Thresholds {
		prev[in.Previous.Thresholds[i].Metric] = &in.Previous.Thresholds[i]
	}
	w("## Against %s\n\n", in.Previous.Version)
	w("Limits of `%s` (`%s`) against the candidate.\n\n", in.Previous.Version, in.PreviousPath)
	w("| Metric | Previous max | Candidate max | Previous max_delta | Candidate max_delta |\n| --- | ---: | ---: | ---: | ---: |\n")
	var moved []string
	for i := range in.Choices {
		c := &in.Choices[i]
		var pMax, pDelta *float64
		if p, ok := prev[c.Rule.Metric]; ok {
			pMax, pDelta = p.Max, p.MaxDelta
		}
		if Opt(pMax) != Opt(c.Max) || Opt(pDelta) != Opt(c.MaxDelta) {
			moved = append(moved, "`"+c.Rule.Metric+"`")
		}
		w("| `%s` | %s | %s | %s | %s |\n", c.Rule.Metric, Opt(pMax), Opt(c.Max), Opt(pDelta), Opt(c.MaxDelta))
	}
	w("\n")
	if len(moved) == 0 {
		w("No limit moved.\n\n")
		return
	}
	w("Only %s moved; every other limit is unchanged.\n\n", strings.Join(moved, ", "))
}

// writeOverride lists which rules the language override replaces and
// which it leaves to the top level, and why.
func writeOverride(b *strings.Builder, in *Input) {
	w := writer(b)
	var set, pinned, requirement, unmeasured []string
	for i := range in.Choices {
		c := &in.Choices[i]
		name := "`" + c.Rule.Metric + "`"
		switch {
		case Overridden(c):
			set = append(set, name)
		case c.Stats.N == 0:
			unmeasured = append(unmeasured, name)
		case c.Rule.Kind == gate.Requirement:
			requirement = append(requirement, name)
		default:
			pinned = append(pinned, name)
		}
	}
	w("## Override\n\n")
	w("- Replaced for %s: %s.\n", in.Language, joinOrNone(set))
	w("- Inherited, zero-tolerance ratchets with no max (max_delta 0 is a policy, not a statistic): %s.\n", joinOrNone(pinned))
	w("- Inherited, requirements (not fitted): %s.\n", joinOrNone(requirement))
	w("- Inherited, measured by no %s row: %s.\n\n", in.Language, joinOrNone(unmeasured))
}

// Overridden reports whether a language override carries c's rule: the
// data measured the metric and fitted it a statistic, a max or a max_delta
// from the IQR. A requirement rule, a rule whose only limit is a
// zero-tolerance max_delta, and a rule on a metric no row measured (one
// the language's extractor leaves null, which the gate skips) are left to
// the top level. Both the report and the emitted override block (package
// emit) judge a choice by this rule.
func Overridden(c *pool.Choice) bool {
	if c.Stats.N == 0 || c.Rule.Kind == gate.Requirement {
		return false
	}
	return c.Max != nil || (c.MaxDelta != nil && !c.DeltaPinned)
}

// joinOrNone joins names with commas, or says none.
func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// writeBaseData sets each gated metric's distribution over the rows the
// base was fitted from beside the fitted data's, with both rules' limits.
func writeBaseData(b *strings.Builder, in *Input) {
	w := writer(b)
	lang := in.Language
	w("## Go against %s\n\n", lang)
	w("Go is `%s`, the rows the top-level rules were fitted from, pooled by the same rules; %s is this data. ", in.BaseData, lang)
	w("Percentiles are per package, except `%s`, per function (p99 in the p90 column, since its max is fitted there). ", pool.FuncMetric)
	w("Max and max_delta are the top-level rule's and the rule %s is judged by.\n\n", lang)
	w("| Metric | Go rows | Go p50 | Go p90 | %s rows | %s p50 | %s p90 | Go max | %s max | Go max_delta | %s max_delta |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n",
		lang, lang, lang, lang, lang)
	for i := range in.Choices {
		c, g := &in.Choices[i], &in.BaseStats[i]
		tMax, tDelta := c.Rule.Max, c.Rule.MaxDelta
		if Overridden(c) {
			tMax, tDelta = c.Max, c.MaxDelta
		}
		w("| `%s` | %d | %s | %s | %d | %s | %s | %s | %s | %s | %s |\n", c.Rule.Metric,
			g.N, statOrNone(g, g.P50), statOrNone(g, upper(c, g)), c.Stats.N, statOrNone(&c.Stats, c.Stats.P50),
			statOrNone(&c.Stats, upper(c, &c.Stats)), Opt(c.Rule.Max), Opt(tMax), Opt(c.Rule.MaxDelta), Opt(tDelta))
	}
	w("\n")
}

// upper is the percentile c's max is fitted at over s: p99 for the
// per-function metric, p90 otherwise.
func upper(c *pool.Choice, s *stats.Stats) float64 {
	if c.Pool == pool.PoolFunctions {
		return s.P99
	}
	return s.P90
}

// statOrNone formats v, or "none" when s has no values.
func statOrNone(s *stats.Stats, v float64) string {
	if s.N == 0 {
		return "none"
	}
	return Num(v)
}

// writeFunctions writes the per-function cognitive distribution behind
// pool.FuncMetric: its percentiles over every pooled function, the fitted
// max and how many functions and packages fall above it.
func writeFunctions(b *strings.Builder, in *Input) {
	w := writer(b)
	var c *pool.Choice
	for i := range in.Choices {
		if in.Choices[i].Pool == pool.PoolFunctions {
			c = &in.Choices[i]
		}
	}
	if c == nil || c.Stats.N == 0 {
		return
	}
	s := &c.Stats
	w("## Per-function cognitive complexity\n\n")
	w("Every function of every pooled row, counted as new (`%s`), %d functions in %d packages:\n\n", c.Rule.Metric, s.N, c.Packages)
	w("| Functions | p50 | p90 | p99 | max |\n| ---: | ---: | ---: | ---: | ---: |\n")
	w("| %d | %s |\n\n", s.N, numRow(s.P50, s.P90, s.P99, s.Max))
	w("The candidate max is p99 %s rounded to %s. Above the base max %s: %s of functions, in %s of packages. ",
		Num(s.P99), Opt(c.Max), Opt(c.Rule.Max), share(c.OverBase, s.N), share(c.PkgOverBase, c.Packages))
	w("Above the candidate: %s of functions, in %s of packages.\n\n",
		share(c.OverCandidate, s.N), share(c.PkgOverCandidate, c.Packages))
}

// logRatio is the size of the move from base to v on a log scale, so a
// halving and a doubling rank alike; a zero on either side ranks first.
func logRatio(v, base float64) float64 {
	if v <= 0 || base <= 0 {
		return math.Inf(1)
	}
	return math.Abs(math.Log(v / base))
}

// Num formats v with the shortest exact representation.
func Num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// edge formats a histogram edge to at most two decimals.
func edge(v float64) string {
	return Num(math.Round(v*100) / 100)
}

// Opt formats an optional limit, "none" when absent.
func Opt(v *float64) string {
	if v == nil {
		return "none"
	}
	return Num(*v)
}

// pct formats k of n as a percentage with one decimal.
func pct(k, n int) string {
	if n == 0 {
		return "0%"
	}
	return strconv.FormatFloat(100*float64(k)/float64(n), 'f', 1, 64) + "%"
}

// share formats k of n as "k (p%)".
func share(k, n int) string {
	return strconv.Itoa(k) + " (" + pct(k, n) + ")"
}

// quoted wraps each string in backticks.
func quoted(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "`" + s + "`"
	}
	return out
}

// Wrap breaks s into lines of at most width bytes at spaces.
func Wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) > width:
			lines = append(lines, line)
			line = word
		default:
			line += " " + word
		}
	}
	return append(lines, line)
}
