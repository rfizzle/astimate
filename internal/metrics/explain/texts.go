package explain

import (
	"slices"
	"strings"
)

// entry is the explanation of one RawMetrics field before explainText
// expands its placeholders, keyed by the field's JSON name.
type entry struct {
	// name is the JSON field name, as in metrics.MetricNames.
	name string
	// flags holds "gated" when the embedded default configuration has a
	// threshold on the metric and "capacity" when that threshold is a
	// capacity rule, whose over-ceiling behavior Explain appends to the
	// evidence, space-separated.
	flags string
	// definition and evidence are the Explain texts.
	definition, evidence string
}

// has reports whether e's flags hold flag.
func (e entry) has(flag string) bool {
	return slices.Contains(strings.Fields(e.flags), flag)
}

// lookup returns the entry of texts named name.
func lookup(name string) (entry, bool) {
	for _, e := range texts() {
		if e.name == name {
			return e, true
		}
	}
	return entry{}, false
}

// texts returns an entry per RawMetrics field in SPEC.md section 6 order.
// Each text is one string literal, never joined with +, so the table stays
// literals and punctuation only, which duplication counts as data
// (duplication.ignore_literal_only) rather than as repeated code.
func texts() []entry {
	return []entry{
		{
			"files", "",
			"Non-test source files, generated files included.",
			"{context}",
		},
		{
			"sloc", "gated capacity",
			"Non-blank, non-comment lines in non-test files.{generated}",
			"{bloat}",
		},
		{
			"largest_file_sloc", "gated capacity",
			"SLOC of the largest non-test file.{generated}",
			"{bloat} Files past the ceiling rarely fit an edit in one view.",
		},
		{
			"tokens_est", "gated capacity",
			"Estimated tokens of non-test source: bytes divided by chars_per_token.{generated}",
			"{bloat}",
		},
		{
			"tokens_est_with_tests", "",
			"Estimated tokens of all source, test files included.{generated}",
			"{context}",
		},
		{
			"internal_imports", "gated capacity",
			"Fan-out: distinct module-internal packages imported by non-test files.",
			"Coupling growth: agent failures come from coupled facts absent from context. Moderate evidence.",
		},
		{
			"external_imports", "",
			"Distinct imports that are neither standard library nor in the module.",
			"{context}",
		},
		{
			"stdlib_imports", "",
			"Distinct standard-library imports.",
			"{context}",
		},
		{
			"fan_in", "",
			"Distinct module-internal packages importing this package from non-test files.",
			"Blast radius: no direct study; the reasoning is that a change to a package with many importers can break each of them. Unproven; reported for ranking. Recorded in the rebuild estimate's contract detail, not in its formula, and not gated.",
		},
		{
			"fan_in_tests", "",
			"Module packages importing this package from test files only.",
			"{context}",
		},
		{
			"exported_symbols", "gated capacity",
			"Exported funcs, methods, types, vars and consts in non-test files.{generated}",
			"Exporting everything: no direct study; a wider API surface raises fan-in cost and the facts the next agent must hold. Indirect evidence.",
		},
		{
			"globals", "gated",
			"Names declared by package-level var in non-test files that hold mutable state, excluding _. In Go, sentinel errors (errors.New or fmt.Errorf of constants), //go:embed variables and boolean, numeric or string variables with a constant initializer or none that the package never assigns, increments or takes the address of (build information set by -ldflags), compiled regexps of constants, and read-only literal tables of constants that nothing writes an element of or passes to a call that could mutate them are left out.{generated}",
			"{hidden}",
		},
		{
			"init_funcs", "gated",
			"Number of init() functions.{generated}",
			"{hidden}",
		},
		{
			"max_nesting", "gated",
			"Deepest nesting of if, for, range, switch, select and func literal.{generated}",
			"{nesting}",
		},
		{
			"cognitive_total", "",
			"Sum of cognitive complexity over all functions (gocognit rules).{generated}",
			"{context}",
		},
		{
			"cognitive_p90", "gated",
			"Nearest-rank 90th percentile of per-function cognitive complexity.{generated}",
			"{nesting}",
		},
		{
			"cyclomatic_total", "",
			"Sum of McCabe cyclomatic complexity over all functions (gocyclo rules: 1 per function plus 1 for each if, for, range, case other than default, && and ||).{generated}",
			"{cyclomatic}",
		},
		{
			"cyclomatic_p90", "",
			"Nearest-rank 90th percentile of per-function cyclomatic complexity (gocyclo rules).{generated}",
			"{cyclomatic}",
		},
		{
			"func_count", "",
			"Functions and methods in non-test files.{generated}",
			"{context}",
		},
		{
			"dup_blocks", "gated",
			"Distinct maximal duplicate token sequences of at least duplication.min_tokens within the package, blocks of every kind: declarative, idiom, self-overlap and code (dup_blocks_code counts the code ones).{generated}",
			"{duplication} The default gate judges blocks of every kind. Judging the code blocks only, a dup_blocks_code rule in its place, was measured on the 2026-10-14 hand labels and not taken: the rule's J fell from +60.8 to +50.4 points and the gate's from +76.4 to +72.0, its recall from 86.7% to 80.0%.",
		},
		{
			"dup_blocks_code", "",
			"The dup_blocks blocks that are code: not only declarations and literals, not only error returns, call statements and call assignments, and not overlapping their own previous copy.{generated}",
			"{duplication} Not gated by default: the default judges dup_blocks, blocks of every kind. A configuration may gate dup_blocks_code in its place, with the same limits, to judge code blocks only; its finding cites the first code block, an added one first. On the 2026-10-14 hand labels that rule failed 53.3% of block and 2.9% of allow commits against dup_blocks' 66.7% and 5.8%, a lower J.",
		},
		{
			"duplication_pct", "gated",
			"Share of non-test SLOC covered by a duplicate block, as a percentage.{generated}",
			"{duplication}",
		},
		{
			"duplicated_lines", "",
			"Non-test SLOC covered by a duplicate block, duplication_pct's numerator.{generated}",
			"{duplication}",
		},
		{
			"test_files", "",
			"_test.go files.",
			"{context}",
		},
		{
			"test_funcs", "",
			"Test, Benchmark, Fuzz and Example functions.",
			"{context}",
		},
		{
			"has_tests", "gated",
			"Whether test_funcs is above 0 or, a test file anywhere in the module refers to an exported func or method of the package.",
			"{tests}",
		},
		{
			"untested_exports_covered", "",
			"untested_exports less the exports whose body has a statement the --coverage run executed, from any test of the run (coverage: true on an MCP tool), so an export a test reaches only through an HTTP handler, a pipeline or an interface counts as tested. Measured only when opted in, because it runs the tests; null without the opt-in and for a package the run reported nothing for, such as one without test files. Reported; a threshold on untested_exports with source: coverage judges it when the run has it and falls back to untested_exports otherwise.",
			"{tests} Opt-in and judged only through such a rule. Baselines do not run tests, so a check counts the baseline's untested exports less those head's run executed, and the rule judges what the change added; against a baseline that recorded no export names it judges untested_exports on both sides.",
		},
		{
			"untested_exports", "gated",
			"Exported funcs and methods not referenced from any test file in the package.{generated}",
			"{tests}",
		},
		{
			"documented_exports", "",
			"Exported symbols, as exported_symbols counts them, with a doc comment: on the declaration, on its parenthesised group, or as the line comment of a var or const. Null where not measured, as for TypeScript.{generated}",
			"{context} The human estimate reads documented_exports over exported_symbols as the self-descriptiveness of COCOMO II's software understanding, so undocumented exports raise the engineer time.",
		},
		{
			"dup_blocks_cross_pkg", "gated",
			"Duplicate blocks shared with other packages in the module: exact normalized repeats of at least duplication.min_tokens found over one module-wide stream, counted once per package they touch. The module-level row (package path \"<module>\") counts the distinct blocks and is baselined like a package, because one edit that copies code across packages changes two packages' counts. A rule on it is gated on the module row only: package rows report their count but are not gated on it, so one cross-package copy is one finding.",
			"{duplication} The default gates the module row: max_delta 0, and max 450 (the corpus p90) for a module with no baseline. Against a git baseline, which records the blocks, the rule judges the baseline's count plus the blocks the change added: new blocks of at least duplication.min_tokens_cross tokens (default 80, near where golangci-lint's dupl starts reporting copies), not counting a block every package of which came out of one rename, the code a split leaves shared between its halves; the finding names only those, and the row keeps the full count. Against a baseline file, which records only the count, the full count is judged.",
		},
		{
			"instability", "",
			"Martin instability Ce / (Ca + Ce) with Ca = fan_in and Ce = internal_imports; 0 is maximally stable, 1 maximally unstable. Null when both are 0.",
			"{coupling}",
		},
		{
			"abstractness", "",
			"Exported interface types over all exported types. Null with no exported types.{generated}",
			"{coupling}",
		},
		{
			"main_sequence_distance", "",
			"|abstractness + instability - 1|, the distance from Martin's main sequence. Null when either input is null.",
			"{coupling}",
		},
		{
			"uses_cgo", "",
			"Whether a non-test file of the package imports \"C\".",
			"{opacity}",
		},
		{
			"uses_reflect", "",
			"Whether a non-test file of the package imports reflect or unsafe, under any name.",
			"{opacity}",
		},
		{
			"generated_files", "",
			"Non-test files with a \"// Code generated ... DO NOT EDIT.\" line before the package clause, Go's generated-file convention. They count in files and the import metrics and in no other size or structure metric; tokens_est_generated carries their volume.",
			"{opacity}",
		},
		{
			"tokens_est_generated", "",
			"Estimated tokens of the generated non-test files that tokens_est leaves out, by the same method. Null for a language with no generated-file convention.",
			"{opacity} The rebuild estimate leaves this volume out, since a rebuild reruns the generator.",
		},
		{
			"coverage_pct", "",
			"Statement coverage of the package's own statements by its own tests, from the profile of go test -cover -coverpkg=./... -count=1 -run ., measured only when opted in (--coverage, or coverage: true on an MCP tool) because it runs the tests. Null without the opt-in, for a package without test files, and when its tests fail to build or run. When present it scales the rebuild estimate's unspecified term by 1 - coverage_pct/100.",
			"{tests} {coverage}",
		},
		{
			"changed_func_cognitive_max", "gated",
			"Highest cognitive complexity among functions added or modified since baseline, matched by receiver and name and compared by a fingerprint of the normalized body, so a comment or formatting edit does not count; 0 when none changed. Null without a function-level baseline to diff against, as in assess.",
			"Closes the gap where one very complex new function leaves the package's p90 low. {nesting} Gated per function against its own baseline: over the max, a new function or a crossing fails, and one already over it fails only on growth past over_max_delta.",
		},
		{
			"churn_commits", "",
			"How many commits of the churn window touched a non-test, non-generated file of the package. The window is the last churn.commits first-parent commits from HEAD (default 200; 0 turns churn off), read with one git log per run; a commit that touched a file under an old name counts for the old package. Null outside a git repository, in a shallow clone, with the window off, on the module row and in a git baseline.",
			"{churn}",
		},
		{
			"churn_authors", "",
			"Distinct author emails among the commits churn_commits counts, over the same window. Null whenever churn_commits is.",
			"{churn}",
		},
	}
}
