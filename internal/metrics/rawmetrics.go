package metrics

// RawMetrics is the per-package metric record every extractor produces. Field
// names and JSON tags match SPEC.md section 6 exactly. v0 fields are plain
// values; v1 fields are pointers so that "not computed" serializes as null and
// stays distinguishable from zero.
type RawMetrics struct {
	// Files is the number of non-test source files, generated files
	// included. Every other size and structure field counts only the files
	// a person wrote: a file with a generated-code header is left out of
	// them and reported in GeneratedFiles and TokensEstGenerated instead
	// (SPEC.md 6.5).
	Files int `json:"files"`
	// SLOC is the number of non-blank, non-comment lines in non-test files.
	SLOC int `json:"sloc"`
	// LargestFileSLOC is the SLOC of the largest non-test file.
	LargestFileSLOC int `json:"largest_file_sloc"`
	// TokensEst is the estimated tokens of non-test source; see SPEC.md 6.1.
	TokensEst int `json:"tokens_est"`
	// TokensEstWithTests is the same estimate including test files.
	TokensEstWithTests int `json:"tokens_est_with_tests"`
	// InternalImports is fan-out: distinct internal packages imported.
	InternalImports int `json:"internal_imports"`
	// ExternalImports is the number of distinct non-stdlib, non-module imports.
	ExternalImports int `json:"external_imports"`
	// StdlibImports is the number of distinct standard-library imports.
	StdlibImports int `json:"stdlib_imports"`
	// FanIn is the number of distinct internal packages importing this package.
	FanIn int `json:"fan_in"`
	// FanInTests is the number of packages importing this one from test files
	// only.
	FanInTests int `json:"fan_in_tests"`
	// ExportedSymbols counts exported funcs, methods, types, vars and consts.
	ExportedSymbols int `json:"exported_symbols"`
	// Globals counts package-level var specs in non-test files, excluding _.
	Globals int `json:"globals"`
	// InitFuncs is the number of init() functions.
	InitFuncs int `json:"init_funcs"`
	// MaxNesting is the deepest nesting of if/for/range/switch/select/func
	// literal.
	MaxNesting int `json:"max_nesting"`
	// CognitiveTotal is the sum of cognitive complexity (gocognit rules).
	CognitiveTotal int `json:"cognitive_total"`
	// CognitiveP90 is the 90th percentile cognitive complexity per function.
	CognitiveP90 int `json:"cognitive_p90"`
	// FuncCount is the number of functions and methods in non-test files.
	FuncCount int `json:"func_count"`
	// DupBlocks counts duplicate token sequences of at least
	// duplication.min_tokens (default 40) within the package; see SPEC.md
	// 6.3.
	DupBlocks int `json:"dup_blocks"`
	// DuplicationPct is the share of non-test SLOC covered by a duplicate
	// block, as a percentage in [0, 100].
	DuplicationPct float64 `json:"duplication_pct"`
	// TestFiles is the number of _test.go files.
	TestFiles int `json:"test_files"`
	// TestFuncs counts Test*, Benchmark*, Fuzz* and Example* functions.
	TestFuncs int `json:"test_funcs"`
	// HasTests reports test_funcs > 0.
	HasTests bool `json:"has_tests"`
	// UntestedExports counts exported funcs and methods not referenced from
	// any test file in the package; see SPEC.md 6.4.
	UntestedExports int `json:"untested_exports"`

	// DupBlocksCrossPkg counts duplicate blocks shared with other packages in
	// the module. Nil when not computed (v1).
	DupBlocksCrossPkg *int `json:"dup_blocks_cross_pkg"`
	// Instability is Martin's instability Ce / (Ca + Ce) with Ca = fan_in and
	// Ce = internal_imports, in [0, 1]. Nil when both are 0 or when not
	// computed (v1). Reported, not gated.
	Instability *float64 `json:"instability"`
	// Abstractness is exported interface types over all exported types, in
	// [0, 1]. Nil with no exported types or when not computed (v1). Reported,
	// not gated.
	Abstractness *float64 `json:"abstractness"`
	// MainSequenceDistance is |abstractness + instability - 1|, in [0, 1].
	// Nil when either input is nil (v1). Reported, not gated: idiomatic Go
	// leaf packages sit near 1 by design.
	MainSequenceDistance *float64 `json:"main_sequence_distance"`
	// UsesCgo reports whether the package imports "C". Nil when not computed
	// (v1).
	UsesCgo *bool `json:"uses_cgo"`
	// UsesReflect reports whether the package imports reflect or unsafe. Nil
	// when not computed (v1).
	UsesReflect *bool `json:"uses_reflect"`
	// GeneratedFiles counts files with a "Code generated ... DO NOT EDIT"
	// header. Nil when not computed (v1).
	GeneratedFiles *int `json:"generated_files"`
	// TokensEstGenerated is the estimated tokens of the generated non-test
	// source that tokens_est leaves out, by the same method as tokens_est.
	// Nil when not computed, as for a language with no generated-file
	// convention (v1).
	TokensEstGenerated *int `json:"tokens_est_generated"`
	// CoveragePct is statement coverage from go test -cover, only with
	// --coverage, as a percentage in [0, 100]. Nil when not computed (v1).
	CoveragePct *float64 `json:"coverage_pct"`
	// ChangedFuncCognitiveMax is the highest cognitive complexity among
	// functions added or modified since baseline; nil without a baseline diff
	// (v1).
	ChangedFuncCognitiveMax *int `json:"changed_func_cognitive_max"`
}

// MetricNames returns every RawMetrics JSON field name in SPEC.md section 6
// order. The slice is freshly allocated on each call.
func MetricNames() []string {
	t := fieldTable()
	names := make([]string, len(t))
	for i := range t {
		names[i] = t[i].name
	}
	return names
}

// Value returns the metric named by its JSON field name as a float64. Bools
// are reported as 0 or 1. The second result is false when the name is unknown
// or names a v1 field that was not computed.
func (m *RawMetrics) Value(name string) (float64, bool) {
	i := fieldIndex(name)
	if i < 0 {
		return 0, false
	}
	v, ok, _ := read(m.slots()[i])
	return v, ok
}
