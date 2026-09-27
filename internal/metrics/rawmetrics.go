package metrics

// RawMetrics is the per-package metric record every extractor produces. Field
// names and JSON tags match SPEC.md section 6 exactly. v0 fields are plain
// values; v1 fields are pointers so that "not computed" serializes as null and
// stays distinguishable from zero.
type RawMetrics struct {
	// Files is the number of non-test source files.
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
	// DupBlocks counts duplicate token sequences of at least dup_min_tokens
	// (default 40) within the package; see SPEC.md 6.3.
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

	// ConcreteParamRatio is the share of exported-function params typed as
	// struct or pointer-to-struct from another package versus interface, in
	// [0, 1]. Nil when not computed (v1).
	ConcreteParamRatio *float64 `json:"concrete_param_ratio"`
	// DupBlocksCrossPkg counts duplicate blocks shared with other packages in
	// the module. Nil when not computed (v1).
	DupBlocksCrossPkg *int `json:"dup_blocks_cross_pkg"`
	// UsesCgo reports whether the package imports "C". Nil when not computed
	// (v1).
	UsesCgo *bool `json:"uses_cgo"`
	// UsesReflect reports whether the package imports reflect or unsafe. Nil
	// when not computed (v1).
	UsesReflect *bool `json:"uses_reflect"`
	// GeneratedFiles counts files with a "Code generated ... DO NOT EDIT"
	// header. Nil when not computed (v1).
	GeneratedFiles *int `json:"generated_files"`
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
	return []string{
		"files",
		"sloc",
		"largest_file_sloc",
		"tokens_est",
		"tokens_est_with_tests",
		"internal_imports",
		"external_imports",
		"stdlib_imports",
		"fan_in",
		"fan_in_tests",
		"exported_symbols",
		"globals",
		"init_funcs",
		"max_nesting",
		"cognitive_total",
		"cognitive_p90",
		"func_count",
		"dup_blocks",
		"duplication_pct",
		"test_files",
		"test_funcs",
		"has_tests",
		"untested_exports",
		"concrete_param_ratio",
		"dup_blocks_cross_pkg",
		"uses_cgo",
		"uses_reflect",
		"generated_files",
		"coverage_pct",
		"changed_func_cognitive_max",
	}
}

// Value returns the metric named by its JSON field name as a float64. Bools
// are reported as 0 or 1. The second result is false when the name is unknown
// or names a v1 field that was not computed.
func (m *RawMetrics) Value(name string) (float64, bool) {
	switch name {
	case "files":
		return float64(m.Files), true
	case "sloc":
		return float64(m.SLOC), true
	case "largest_file_sloc":
		return float64(m.LargestFileSLOC), true
	case "tokens_est":
		return float64(m.TokensEst), true
	case "tokens_est_with_tests":
		return float64(m.TokensEstWithTests), true
	case "internal_imports":
		return float64(m.InternalImports), true
	case "external_imports":
		return float64(m.ExternalImports), true
	case "stdlib_imports":
		return float64(m.StdlibImports), true
	case "fan_in":
		return float64(m.FanIn), true
	case "fan_in_tests":
		return float64(m.FanInTests), true
	case "exported_symbols":
		return float64(m.ExportedSymbols), true
	case "globals":
		return float64(m.Globals), true
	case "init_funcs":
		return float64(m.InitFuncs), true
	case "max_nesting":
		return float64(m.MaxNesting), true
	case "cognitive_total":
		return float64(m.CognitiveTotal), true
	case "cognitive_p90":
		return float64(m.CognitiveP90), true
	case "func_count":
		return float64(m.FuncCount), true
	case "dup_blocks":
		return float64(m.DupBlocks), true
	case "duplication_pct":
		return m.DuplicationPct, true
	case "test_files":
		return float64(m.TestFiles), true
	case "test_funcs":
		return float64(m.TestFuncs), true
	case "has_tests":
		return boolValue(m.HasTests), true
	case "untested_exports":
		return float64(m.UntestedExports), true
	case "concrete_param_ratio":
		return optFloat(m.ConcreteParamRatio)
	case "dup_blocks_cross_pkg":
		return optInt(m.DupBlocksCrossPkg)
	case "uses_cgo":
		return optBool(m.UsesCgo)
	case "uses_reflect":
		return optBool(m.UsesReflect)
	case "generated_files":
		return optInt(m.GeneratedFiles)
	case "coverage_pct":
		return optFloat(m.CoveragePct)
	case "changed_func_cognitive_max":
		return optInt(m.ChangedFuncCognitiveMax)
	default:
		return 0, false
	}
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func optInt(p *int) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return float64(*p), true
}

func optFloat(p *float64) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return *p, true
}

func optBool(p *bool) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return boolValue(*p), true
}
