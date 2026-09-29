package metrics

import "strings"

// numFields is the number of RawMetrics fields, the rows of fieldTable.
const numFields = 33

// field is one row of SPEC.md section 6's metric table: what MetricNames,
// Value, Delta, Validate, ModuleWide and Explain know about a RawMetrics
// field. Its columns are literals, so the table is data, not code.
type field struct {
	// name is the JSON field name.
	name string
	// release is "v0" for a field every extractor computes and "v1" for
	// one that is null until computed; a v1 field is a pointer.
	release string
	// upper is the inclusive bound Validate holds a float field to, 100
	// for a percentage and 1 for a ratio; 0 for a count or a flag, which
	// Validate holds to non-negative and not at all.
	upper float64
	// flags holds "gated" when the embedded default configuration has a
	// threshold on the metric and "module-wide" when the module row
	// carries it (ModuleWide), space-separated.
	flags string
	// definition and evidence are the Explain texts, before the
	// placeholders explainText expands.
	definition, evidence string
}

// has reports whether f's flags hold flag.
func (f field) has(flag string) bool {
	for w := range strings.FieldsSeq(f.flags) {
		if w == flag {
			return true
		}
	}
	return false
}

// fieldTable returns the metric table in SPEC.md section 6 order, the
// order of the RawMetrics fields and of slots.
func fieldTable() [numFields]field {
	return [numFields]field{
		{"files", "v0", 0, "", "Non-test source files, generated files included.", "{context}"},
		{"sloc", "v0", 0, "gated", "Non-blank, non-comment lines in non-test files.{generated}", "{bloat}"},
		{
			"largest_file_sloc", "v0", 0, "gated", "SLOC of the largest non-test file.{generated}",
			"{bloat} Files past the ceiling rarely fit an edit in one view.",
		},
		{
			"tokens_est", "v0", 0, "gated",
			"Estimated tokens of non-test source: bytes divided by chars_per_token.{generated}", "{bloat}",
		},
		{"tokens_est_with_tests", "v0", 0, "", "Estimated tokens of all source, test files included.{generated}", "{context}"},
		{
			"internal_imports", "v0", 0, "gated", "Fan-out: distinct module-internal packages imported by non-test files.",
			"Coupling growth: agent failures come from coupled facts absent from context. Moderate evidence.",
		},
		{"external_imports", "v0", 0, "", "Distinct imports that are neither standard library nor in the module.", "{context}"},
		{"stdlib_imports", "v0", 0, "", "Distinct standard-library imports.", "{context}"},
		{
			"fan_in", "v0", 0, "", "Distinct module-internal packages importing this package from non-test files.",
			"Blast radius: no direct study; the reasoning is that a change to a package with many importers can " +
				"break each of them. Unproven; reported for ranking. Recorded in the rebuild estimate's contract " +
				"detail, not in its formula, and not gated.",
		},
		{"fan_in_tests", "v0", 0, "", "Module packages importing this package from test files only.", "{context}"},
		{
			"exported_symbols", "v0", 0, "gated",
			"Exported funcs, methods, types, vars and consts in non-test files.{generated}",
			"Exporting everything: no direct study; a wider API surface raises fan-in cost and the facts the " +
				"next agent must hold. Indirect evidence.",
		},
		{
			"globals", "v0", 0, "gated",
			"Names declared by package-level var in non-test files that hold mutable state, excluding _. " +
				"In Go, sentinel errors (errors.New or fmt.Errorf of constants), //go:embed variables and " +
				"boolean, numeric or string variables with a constant initializer or none that the package " +
				"never assigns, increments or takes the address of (build information set by -ldflags) " +
				"are left out.{generated}",
			"{hidden}",
		},
		{"init_funcs", "v0", 0, "gated", "Number of init() functions.{generated}", "{hidden}"},
		{
			"max_nesting", "v0", 0, "gated",
			"Deepest nesting of if, for, range, switch, select and func literal.{generated}", "{nesting}",
		},
		{
			"cognitive_total", "v0", 0, "",
			"Sum of cognitive complexity over all functions (gocognit rules).{generated}", "{context}",
		},
		{
			"cognitive_p90", "v0", 0, "gated",
			"Nearest-rank 90th percentile of per-function cognitive complexity.{generated}", "{nesting}",
		},
		{"func_count", "v0", 0, "", "Functions and methods in non-test files.{generated}", "{context}"},
		{
			"dup_blocks", "v0", 0, "gated",
			"Distinct maximal duplicate token sequences of at least duplication.min_tokens within the package.{generated}",
			"{duplication}",
		},
		{
			"duplication_pct", "v0", 100, "gated",
			"Share of non-test SLOC covered by a duplicate block, as a percentage.{generated}", "{duplication}",
		},
		{"test_files", "v0", 0, "", "_test.go files.", "{context}"},
		{"test_funcs", "v0", 0, "", "Test, Benchmark, Fuzz and Example functions.", "{context}"},
		{"has_tests", "v0", 0, "gated", "Whether test_funcs is above 0.", "{tests}"},
		{
			"untested_exports", "v0", 0, "gated",
			"Exported funcs and methods not referenced from any test file in the package.{generated}", "{tests}",
		},
		{
			"dup_blocks_cross_pkg", "v1", 0, "gated module-wide",
			"Duplicate blocks shared with other packages in the module: exact normalized repeats of at least " +
				"duplication.min_tokens found over one module-wide stream, counted once per package they touch. " +
				"The module-level row (package path \"<module>\") counts the distinct blocks and is baselined like a " +
				"package, because one edit that copies code across packages changes two packages' counts. A rule on " +
				"it is gated on the module row only: package rows report their count but are not gated on it, so one " +
				"cross-package copy is one finding.",
			"{duplication} The default gates the module row: max_delta 0, and max 450 (the corpus p90) for a " +
				"module with no baseline.",
		},
		{
			"instability", "v1", 1, "",
			"Martin instability Ce / (Ca + Ce) with Ca = fan_in and Ce = internal_imports; 0 is maximally stable, " +
				"1 maximally unstable. Null when both are 0.",
			"{coupling}",
		},
		{
			"abstractness", "v1", 1, "",
			"Exported interface types over all exported types. Null with no exported types.{generated}", "{coupling}",
		},
		{
			"main_sequence_distance", "v1", 1, "",
			"|abstractness + instability - 1|, the distance from Martin's main sequence. Null when either input is null.",
			"{coupling}",
		},
		{"uses_cgo", "v1", 0, "", "Whether a non-test file of the package imports \"C\".", "{opacity}"},
		{
			"uses_reflect", "v1", 0, "",
			"Whether a non-test file of the package imports reflect or unsafe, under any name.", "{opacity}",
		},
		{
			"generated_files", "v1", 0, "",
			"Non-test files with a \"// Code generated ... DO NOT EDIT.\" line before the package clause, Go's " +
				"generated-file convention. They count in files and the import metrics and in no other size or " +
				"structure metric; tokens_est_generated carries their volume.",
			"{opacity}",
		},
		{
			"tokens_est_generated", "v1", 0, "",
			"Estimated tokens of the generated non-test files that tokens_est leaves out, by the same method. " +
				"Null for a language with no generated-file convention.",
			"{opacity} The rebuild estimate leaves this volume out, since a rebuild reruns the generator.",
		},
		{
			"coverage_pct", "v1", 100, "",
			"Statement coverage of the package's own statements from go test -cover -count=1 -run ., measured " +
				"only when opted in (--coverage, or coverage: true on an MCP tool) because it runs the tests. " +
				"Null without the opt-in, for a package without test files, and when its tests fail to build or " +
				"run. When present it scales the rebuild estimate's unspecified term by 1 - coverage_pct/100.",
			"{tests} {coverage}",
		},
		{
			"changed_func_cognitive_max", "v1", 0, "gated",
			"Highest cognitive complexity among functions added or modified since baseline, matched by receiver " +
				"and name and compared by a fingerprint of the normalized body, so a comment or formatting edit " +
				"does not count; 0 when none changed. Null without a function-level baseline to diff against, " +
				"as in assess.",
			"Closes the gap where one very complex new function leaves the package's p90 low. {nesting} Gated " +
				"with an absolute max only, since the value is already a diff.",
		},
	}
}

// fieldIndex returns the row of fieldTable named name, or -1.
func fieldIndex(name string) int {
	t := fieldTable()
	for i := range t {
		if t[i].name == name {
			return i
		}
	}
	return -1
}

// slots returns a pointer to every field of m in fieldTable order: an
// *int, *float64 or *bool for a v0 field, and a pointer to the pointer
// field for a v1 one.
func (m *RawMetrics) slots() [numFields]any {
	return [numFields]any{
		&m.Files, &m.SLOC, &m.LargestFileSLOC, &m.TokensEst, &m.TokensEstWithTests, &m.InternalImports,
		&m.ExternalImports, &m.StdlibImports, &m.FanIn, &m.FanInTests, &m.ExportedSymbols, &m.Globals,
		&m.InitFuncs, &m.MaxNesting, &m.CognitiveTotal, &m.CognitiveP90, &m.FuncCount, &m.DupBlocks,
		&m.DuplicationPct, &m.TestFiles, &m.TestFuncs, &m.HasTests, &m.UntestedExports,
		&m.DupBlocksCrossPkg, &m.Instability, &m.Abstractness, &m.MainSequenceDistance, &m.UsesCgo,
		&m.UsesReflect, &m.GeneratedFiles, &m.TokensEstGenerated, &m.CoveragePct, &m.ChangedFuncCognitiveMax,
	}
}

// valueKind is how a field's value is stored.
type valueKind uint8

const (
	kindInt valueKind = iota
	kindFloat
	kindBool
)

// read returns the value a slot points at as a float64, bools as 0 or 1,
// with its kind; ok is false for a nil v1 field.
func read(slot any) (v float64, ok bool, k valueKind) {
	switch p := slot.(type) {
	case *int:
		return float64(*p), true, kindInt
	case *float64:
		return *p, true, kindFloat
	case *bool:
		return boolValue(*p), true, kindBool
	case **int:
		n, ok := deref(*p)
		return float64(n), ok, kindInt
	case **float64:
		f, ok := deref(*p)
		return f, ok, kindFloat
	case **bool:
		b, ok := deref(*p)
		return boolValue(b), ok, kindBool
	}
	return 0, false, kindInt
}

// deref returns *p, and false with the zero value when p is nil.
func deref[T any](p *T) (T, bool) {
	if p == nil {
		var zero T
		return zero, false
	}
	return *p, true
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
