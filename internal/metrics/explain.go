package metrics

// Explanation describes one RawMetrics field for a reader deciding what a
// value means and whether it can fail the gate.
type Explanation struct {
	// Definition says what the metric counts, as in SPEC.md section 6.
	Definition string
	// Evidence summarizes why the metric is reported or gated, from SPEC.md
	// section 4.
	Evidence string
	// Release is "v0" or "v1", the SPEC.md section 6 release column.
	Release string
	// Gated reports whether the embedded default configuration has a
	// threshold on the metric. A config file may gate any metric; this is
	// the shipped default only.
	Gated bool
}

// Explain returns the explanation of the metric named by its JSON field
// name, and false when the name is not a RawMetrics field. Every name in
// MetricNames has an entry.
func Explain(name string) (Explanation, bool) {
	e, ok := explanations()[name]
	return e, ok
}

// Evidence texts shared by more than one metric.
const (
	evidenceDuplication = "Copy-paste instead of extraction: industry reports on AI-assisted " +
		"repositories show rising duplicated blocks. The most specific LLM failure mode found; moderate evidence."
	evidenceBloat = "Package bloat: successful agent trajectories stay under 20 to 30k tokens and " +
		"resolve rates collapse past 64k. Strong evidence."
	evidenceTests = "Untested additions: agents self-correct through a run-and-check loop, and a " +
		"package without tests denies the next agent that loop. Moderate, indirect evidence."
	evidenceHiddenState = "Hidden state: no direct study; plausible and kept at low weight."
	evidenceNesting     = "Deep nesting: classical complexity shows no consistent correlation with " +
		"LLM performance once length is controlled. Gated on regressions only; weak evidence."
	evidenceContext = "Context only: an input to the rebuild estimate or to other metrics, not a " +
		"failure mode the gate targets."
	evidenceCoupling = "Reported, not gated. Martin's package metrics are widely reported but their " +
		"validation as predictors is mixed and none exists for Go, whose consumer-defined interfaces " +
		"invert the abstract-provider assumption: idiomatic Go puts stable concrete leaf packages near " +
		"distance 1 by design. No gate rule until the reference-corpus measurement shows where " +
		"well-regarded Go modules sit."
	evidenceCoverage = "Opt-in and reported only: no gate rule, since baselines do not run tests " +
		"and a check measures head only, so there is no delta to gate."
	evidenceOpacity = "Opacity: code the next agent cannot follow from Go source alone (C, reflection, " +
		"unsafe memory, generator output). Informational: reported, not gated, and no default threshold."
)

// generatedExcluded ends the definition of every size and structure metric
// that counts only the files a person wrote (SPEC.md 6.5).
const generatedExcluded = " Generated files, those with a \"Code generated ... DO NOT EDIT.\" header, " +
	"are left out: a rebuild regenerates them; see tokens_est_generated."

// explanations returns a fresh table of every metric's explanation.
func explanations() map[string]Explanation {
	return map[string]Explanation{
		"files":                 {Definition: "Non-test source files, generated files included.", Evidence: evidenceContext, Release: "v0"},
		"sloc":                  {Definition: "Non-blank, non-comment lines in non-test files." + generatedExcluded, Evidence: evidenceBloat, Release: "v0", Gated: true},
		"largest_file_sloc":     {Definition: "SLOC of the largest non-test file." + generatedExcluded, Evidence: evidenceBloat + " Files past the ceiling rarely fit an edit in one view.", Release: "v0", Gated: true},
		"tokens_est":            {Definition: "Estimated tokens of non-test source: bytes divided by chars_per_token." + generatedExcluded, Evidence: evidenceBloat, Release: "v0", Gated: true},
		"tokens_est_with_tests": {Definition: "Estimated tokens of all source, test files included." + generatedExcluded, Evidence: evidenceContext, Release: "v0"},
		"internal_imports": {
			Definition: "Fan-out: distinct module-internal packages imported by non-test files.",
			Evidence: "Coupling growth: agent failures come from coupled facts absent from context. " +
				"Moderate evidence.",
			Release: "v0", Gated: true,
		},
		"external_imports": {Definition: "Distinct imports that are neither standard library nor in the module.", Evidence: evidenceContext, Release: "v0"},
		"stdlib_imports":   {Definition: "Distinct standard-library imports.", Evidence: evidenceContext, Release: "v0"},
		"fan_in": {
			Definition: "Distinct module-internal packages importing this package from non-test files.",
			Evidence: "Blast radius: the strongest predictor of task difficulty. Rarely changes within one " +
				"change, so it drives the rebuild estimate and ranking rather than the gate.",
			Release: "v0",
		},
		"fan_in_tests": {Definition: "Module packages importing this package from test files only.", Evidence: evidenceContext, Release: "v0"},
		"exported_symbols": {
			Definition: "Exported funcs, methods, types, vars and consts in non-test files." + generatedExcluded,
			Evidence: "Exporting everything: no direct study; a wider API surface raises fan-in cost and " +
				"the facts the next agent must hold. Indirect evidence.",
			Release: "v0", Gated: true,
		},
		"globals":         {Definition: "Names declared by package-level var in non-test files, excluding _." + generatedExcluded, Evidence: evidenceHiddenState, Release: "v0", Gated: true},
		"init_funcs":      {Definition: "Number of init() functions." + generatedExcluded, Evidence: evidenceHiddenState, Release: "v0", Gated: true},
		"max_nesting":     {Definition: "Deepest nesting of if, for, range, switch, select and func literal." + generatedExcluded, Evidence: evidenceNesting, Release: "v0", Gated: true},
		"cognitive_total": {Definition: "Sum of cognitive complexity over all functions (gocognit rules)." + generatedExcluded, Evidence: evidenceContext, Release: "v0"},
		"cognitive_p90":   {Definition: "Nearest-rank 90th percentile of per-function cognitive complexity." + generatedExcluded, Evidence: evidenceNesting, Release: "v0", Gated: true},
		"func_count":      {Definition: "Functions and methods in non-test files." + generatedExcluded, Evidence: evidenceContext, Release: "v0"},
		"dup_blocks":      {Definition: "Distinct maximal duplicate token sequences of at least duplication.min_tokens within the package." + generatedExcluded, Evidence: evidenceDuplication, Release: "v0", Gated: true},
		"duplication_pct": {Definition: "Share of non-test SLOC covered by a duplicate block, as a percentage." + generatedExcluded, Evidence: evidenceDuplication, Release: "v0", Gated: true},
		"test_files":      {Definition: "_test.go files.", Evidence: evidenceContext, Release: "v0"},
		"test_funcs":      {Definition: "Test, Benchmark, Fuzz and Example functions.", Evidence: evidenceContext, Release: "v0"},
		"has_tests":       {Definition: "Whether test_funcs is above 0.", Evidence: evidenceTests, Release: "v0", Gated: true},
		"untested_exports": {
			Definition: "Exported funcs and methods not referenced from any test file in the package." + generatedExcluded,
			Evidence:   evidenceTests,
			Release:    "v0", Gated: true,
		},
		"dup_blocks_cross_pkg": {
			Definition: "Duplicate blocks shared with other packages in the module: exact normalized repeats of at " +
				"least duplication.min_tokens found over one module-wide stream, counted once per package they touch. " +
				"The module-level row (package path \"<module>\") counts the distinct blocks and is baselined like a " +
				"package, because one edit that copies code across packages changes two packages' counts. A rule on " +
				"it is gated on the module row only: package rows report their count but are not gated on it, so one " +
				"cross-package copy is one finding.",
			Evidence: evidenceDuplication + " The default gates the module row: " +
				"max_delta 0, and max 450 (the corpus p90) " +
				"for a module with no baseline.",
			Release: "v1", Gated: true,
		},
		"instability": {
			Definition: "Martin instability Ce / (Ca + Ce) with Ca = fan_in and Ce = internal_imports; " +
				"0 is maximally stable, 1 maximally unstable. Null when both are 0.",
			Evidence: evidenceCoupling,
			Release:  "v1",
		},
		"abstractness": {
			Definition: "Exported interface types over all exported types. Null with no exported types." + generatedExcluded,
			Evidence:   evidenceCoupling,
			Release:    "v1",
		},
		"main_sequence_distance": {
			Definition: "|abstractness + instability - 1|, the distance from Martin's main sequence. " +
				"Null when either input is null.",
			Evidence: evidenceCoupling,
			Release:  "v1",
		},
		"uses_cgo": {Definition: "Whether a non-test file of the package imports \"C\".", Evidence: evidenceOpacity, Release: "v1"},
		"uses_reflect": {
			Definition: "Whether a non-test file of the package imports reflect or unsafe, under any name.",
			Evidence:   evidenceOpacity,
			Release:    "v1",
		},
		"generated_files": {
			Definition: "Non-test files with a \"// Code generated ... DO NOT EDIT.\" line before the package clause, " +
				"Go's generated-file convention. They count in files and the import metrics and in no other " +
				"size or structure metric; tokens_est_generated carries their volume.",
			Evidence: evidenceOpacity,
			Release:  "v1",
		},
		"tokens_est_generated": {
			Definition: "Estimated tokens of the generated non-test files that tokens_est leaves out, by the same " +
				"method. Null for a language with no generated-file convention.",
			Evidence: evidenceOpacity + " The rebuild estimate leaves this volume out, since a rebuild reruns " +
				"the generator.",
			Release: "v1",
		},
		"coverage_pct": {
			Definition: "Statement coverage of the package's own statements from go test -cover -count=1 -run ., " +
				"measured only when opted in (--coverage, or coverage: true on an MCP tool) because it runs the tests. " +
				"Null without the opt-in, for a package without test files, and when its tests fail to build or run. " +
				"When present it scales the rebuild estimate's unspecified term by 1 - coverage_pct/100.",
			Evidence: evidenceTests + " " + evidenceCoverage,
			Release:  "v1",
		},
		"changed_func_cognitive_max": {
			Definition: "Highest cognitive complexity among functions added or modified since baseline, " +
				"matched by receiver and name and compared by a fingerprint of the normalized body, so a " +
				"comment or formatting edit does not count; 0 when none changed. Null without a " +
				"function-level baseline to diff against, as in assess.",
			Evidence: "Closes the gap where one very complex new function leaves the package's p90 low. " +
				evidenceNesting + " Gated with an absolute max only, since the value is already a diff.",
			Release: "v1",
			Gated:   true,
		},
	}
}
