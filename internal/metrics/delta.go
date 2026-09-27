package metrics

// MetricDeltas holds the per-field difference head minus base for every
// RawMetrics field, using the same JSON names. Bool fields are reported as -1
// (true to false), 0 (unchanged) or +1 (false to true). A v1 field is nil when
// head did not compute it; when head computed it and base did not, the base is
// treated as zero so a new package against a zero baseline reports the head
// value.
type MetricDeltas struct {
	// Files is the change in files.
	Files int `json:"files"`
	// SLOC is the change in sloc.
	SLOC int `json:"sloc"`
	// LargestFileSLOC is the change in largest_file_sloc.
	LargestFileSLOC int `json:"largest_file_sloc"`
	// TokensEst is the change in tokens_est.
	TokensEst int `json:"tokens_est"`
	// TokensEstWithTests is the change in tokens_est_with_tests.
	TokensEstWithTests int `json:"tokens_est_with_tests"`
	// InternalImports is the change in internal_imports.
	InternalImports int `json:"internal_imports"`
	// ExternalImports is the change in external_imports.
	ExternalImports int `json:"external_imports"`
	// StdlibImports is the change in stdlib_imports.
	StdlibImports int `json:"stdlib_imports"`
	// FanIn is the change in fan_in.
	FanIn int `json:"fan_in"`
	// FanInTests is the change in fan_in_tests.
	FanInTests int `json:"fan_in_tests"`
	// ExportedSymbols is the change in exported_symbols.
	ExportedSymbols int `json:"exported_symbols"`
	// Globals is the change in globals.
	Globals int `json:"globals"`
	// InitFuncs is the change in init_funcs.
	InitFuncs int `json:"init_funcs"`
	// MaxNesting is the change in max_nesting.
	MaxNesting int `json:"max_nesting"`
	// CognitiveTotal is the change in cognitive_total.
	CognitiveTotal int `json:"cognitive_total"`
	// CognitiveP90 is the change in cognitive_p90.
	CognitiveP90 int `json:"cognitive_p90"`
	// FuncCount is the change in func_count.
	FuncCount int `json:"func_count"`
	// DupBlocks is the change in dup_blocks.
	DupBlocks int `json:"dup_blocks"`
	// DuplicationPct is the change in duplication_pct, in percentage points.
	DuplicationPct float64 `json:"duplication_pct"`
	// TestFiles is the change in test_files.
	TestFiles int `json:"test_files"`
	// TestFuncs is the change in test_funcs.
	TestFuncs int `json:"test_funcs"`
	// HasTests is the change in has_tests as -1, 0 or +1.
	HasTests int `json:"has_tests"`
	// UntestedExports is the change in untested_exports.
	UntestedExports int `json:"untested_exports"`

	// ConcreteParamRatio is the change in concrete_param_ratio, nil when head
	// did not compute it.
	ConcreteParamRatio *float64 `json:"concrete_param_ratio"`
	// DupBlocksCrossPkg is the change in dup_blocks_cross_pkg, nil when head
	// did not compute it.
	DupBlocksCrossPkg *int `json:"dup_blocks_cross_pkg"`
	// UsesCgo is the change in uses_cgo as -1, 0 or +1, nil when head did not
	// compute it.
	UsesCgo *int `json:"uses_cgo"`
	// UsesReflect is the change in uses_reflect as -1, 0 or +1, nil when head
	// did not compute it.
	UsesReflect *int `json:"uses_reflect"`
	// GeneratedFiles is the change in generated_files, nil when head did not
	// compute it.
	GeneratedFiles *int `json:"generated_files"`
	// CoveragePct is the change in coverage_pct, in percentage points, nil
	// when head did not compute it.
	CoveragePct *float64 `json:"coverage_pct"`
	// ChangedFuncCognitiveMax is the change in changed_func_cognitive_max, nil
	// when head did not compute it.
	ChangedFuncCognitiveMax *int `json:"changed_func_cognitive_max"`
}

// Delta returns head minus base for every field, where the receiver is head.
// Pass a zero RawMetrics as base for a package that is new at head.
func (m *RawMetrics) Delta(base RawMetrics) MetricDeltas {
	return MetricDeltas{
		Files:                   m.Files - base.Files,
		SLOC:                    m.SLOC - base.SLOC,
		LargestFileSLOC:         m.LargestFileSLOC - base.LargestFileSLOC,
		TokensEst:               m.TokensEst - base.TokensEst,
		TokensEstWithTests:      m.TokensEstWithTests - base.TokensEstWithTests,
		InternalImports:         m.InternalImports - base.InternalImports,
		ExternalImports:         m.ExternalImports - base.ExternalImports,
		StdlibImports:           m.StdlibImports - base.StdlibImports,
		FanIn:                   m.FanIn - base.FanIn,
		FanInTests:              m.FanInTests - base.FanInTests,
		ExportedSymbols:         m.ExportedSymbols - base.ExportedSymbols,
		Globals:                 m.Globals - base.Globals,
		InitFuncs:               m.InitFuncs - base.InitFuncs,
		MaxNesting:              m.MaxNesting - base.MaxNesting,
		CognitiveTotal:          m.CognitiveTotal - base.CognitiveTotal,
		CognitiveP90:            m.CognitiveP90 - base.CognitiveP90,
		FuncCount:               m.FuncCount - base.FuncCount,
		DupBlocks:               m.DupBlocks - base.DupBlocks,
		DuplicationPct:          m.DuplicationPct - base.DuplicationPct,
		TestFiles:               m.TestFiles - base.TestFiles,
		TestFuncs:               m.TestFuncs - base.TestFuncs,
		HasTests:                boolInt(m.HasTests) - boolInt(base.HasTests),
		UntestedExports:         m.UntestedExports - base.UntestedExports,
		ConcreteParamRatio:      deltaOpt(m.ConcreteParamRatio, base.ConcreteParamRatio),
		DupBlocksCrossPkg:       deltaOpt(m.DupBlocksCrossPkg, base.DupBlocksCrossPkg),
		UsesCgo:                 deltaOptBool(m.UsesCgo, base.UsesCgo),
		UsesReflect:             deltaOptBool(m.UsesReflect, base.UsesReflect),
		GeneratedFiles:          deltaOpt(m.GeneratedFiles, base.GeneratedFiles),
		CoveragePct:             deltaOpt(m.CoveragePct, base.CoveragePct),
		ChangedFuncCognitiveMax: deltaOpt(m.ChangedFuncCognitiveMax, base.ChangedFuncCognitiveMax),
	}
}

// Value returns the delta named by its JSON field name as a float64. The
// second result is false when the name is unknown or names a v1 field whose
// delta is nil.
func (d *MetricDeltas) Value(name string) (float64, bool) {
	switch name {
	case "files":
		return float64(d.Files), true
	case "sloc":
		return float64(d.SLOC), true
	case "largest_file_sloc":
		return float64(d.LargestFileSLOC), true
	case "tokens_est":
		return float64(d.TokensEst), true
	case "tokens_est_with_tests":
		return float64(d.TokensEstWithTests), true
	case "internal_imports":
		return float64(d.InternalImports), true
	case "external_imports":
		return float64(d.ExternalImports), true
	case "stdlib_imports":
		return float64(d.StdlibImports), true
	case "fan_in":
		return float64(d.FanIn), true
	case "fan_in_tests":
		return float64(d.FanInTests), true
	case "exported_symbols":
		return float64(d.ExportedSymbols), true
	case "globals":
		return float64(d.Globals), true
	case "init_funcs":
		return float64(d.InitFuncs), true
	case "max_nesting":
		return float64(d.MaxNesting), true
	case "cognitive_total":
		return float64(d.CognitiveTotal), true
	case "cognitive_p90":
		return float64(d.CognitiveP90), true
	case "func_count":
		return float64(d.FuncCount), true
	case "dup_blocks":
		return float64(d.DupBlocks), true
	case "duplication_pct":
		return d.DuplicationPct, true
	case "test_files":
		return float64(d.TestFiles), true
	case "test_funcs":
		return float64(d.TestFuncs), true
	case "has_tests":
		return float64(d.HasTests), true
	case "untested_exports":
		return float64(d.UntestedExports), true
	case "concrete_param_ratio":
		return optFloat(d.ConcreteParamRatio)
	case "dup_blocks_cross_pkg":
		return optInt(d.DupBlocksCrossPkg)
	case "uses_cgo":
		return optInt(d.UsesCgo)
	case "uses_reflect":
		return optInt(d.UsesReflect)
	case "generated_files":
		return optInt(d.GeneratedFiles)
	case "coverage_pct":
		return optFloat(d.CoveragePct)
	case "changed_func_cognitive_max":
		return optInt(d.ChangedFuncCognitiveMax)
	default:
		return 0, false
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// deltaOpt returns head minus base, nil when head is nil and treating a nil
// base as zero.
func deltaOpt[T int | float64](head, base *T) *T {
	if head == nil {
		return nil
	}
	d := *head
	if base != nil {
		d -= *base
	}
	return &d
}

// deltaOptBool is deltaOpt for bools, reporting -1, 0 or +1.
func deltaOptBool(head, base *bool) *int {
	if head == nil {
		return nil
	}
	d := boolInt(*head)
	if base != nil {
		d -= boolInt(*base)
	}
	return &d
}
