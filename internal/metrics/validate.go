package metrics

import (
	"errors"
	"fmt"
	"math"
)

// ErrInvalidMetrics is wrapped by every error returned from
// RawMetrics.Validate.
var ErrInvalidMetrics = errors.New("invalid metrics")

// Validate reports every field that is out of range: negative counts,
// percentages outside [0, 100], ratios outside [0, 1], largest_file_sloc
// above sloc, and tokens_est_with_tests below tokens_est. The returned error
// joins one error per problem, each wrapping ErrInvalidMetrics; it is nil when
// the record is consistent. Nil v1 fields are not checked.
func (m *RawMetrics) Validate() error {
	var errs []error
	count := func(name string, v int) {
		if v < 0 {
			errs = append(errs, fmt.Errorf("%w: %s is negative (%d)", ErrInvalidMetrics, name, v))
		}
	}
	bounded := func(name string, v, hi float64) {
		if math.IsNaN(v) || v < 0 || v > hi {
			errs = append(errs, fmt.Errorf("%w: %s %v outside [0, %v]", ErrInvalidMetrics, name, v, hi))
		}
	}

	count("files", m.Files)
	count("sloc", m.SLOC)
	count("largest_file_sloc", m.LargestFileSLOC)
	count("tokens_est", m.TokensEst)
	count("tokens_est_with_tests", m.TokensEstWithTests)
	count("internal_imports", m.InternalImports)
	count("external_imports", m.ExternalImports)
	count("stdlib_imports", m.StdlibImports)
	count("fan_in", m.FanIn)
	count("fan_in_tests", m.FanInTests)
	count("exported_symbols", m.ExportedSymbols)
	count("globals", m.Globals)
	count("init_funcs", m.InitFuncs)
	count("max_nesting", m.MaxNesting)
	count("cognitive_total", m.CognitiveTotal)
	count("cognitive_p90", m.CognitiveP90)
	count("func_count", m.FuncCount)
	count("dup_blocks", m.DupBlocks)
	bounded("duplication_pct", m.DuplicationPct, 100)
	count("test_files", m.TestFiles)
	count("test_funcs", m.TestFuncs)
	count("untested_exports", m.UntestedExports)
	if m.ConcreteParamRatio != nil {
		bounded("concrete_param_ratio", *m.ConcreteParamRatio, 1)
	}
	if m.DupBlocksCrossPkg != nil {
		count("dup_blocks_cross_pkg", *m.DupBlocksCrossPkg)
	}
	if m.GeneratedFiles != nil {
		count("generated_files", *m.GeneratedFiles)
	}
	if m.CoveragePct != nil {
		bounded("coverage_pct", *m.CoveragePct, 100)
	}
	if m.ChangedFuncCognitiveMax != nil {
		count("changed_func_cognitive_max", *m.ChangedFuncCognitiveMax)
	}

	if m.LargestFileSLOC > m.SLOC {
		errs = append(errs, fmt.Errorf("%w: largest_file_sloc %d exceeds sloc %d",
			ErrInvalidMetrics, m.LargestFileSLOC, m.SLOC))
	}
	if m.TokensEstWithTests < m.TokensEst {
		errs = append(errs, fmt.Errorf("%w: tokens_est_with_tests %d is below tokens_est %d",
			ErrInvalidMetrics, m.TokensEstWithTests, m.TokensEst))
	}
	return errors.Join(errs...)
}
