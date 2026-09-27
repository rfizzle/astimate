package metrics

import "context"

// Coverage is the outcome of measuring one package's statement coverage.
type Coverage struct {
	// Pct is the statement coverage as a percentage in [0, 100], nil when
	// it could not be measured: the package has no test files or no
	// statements, or its tests failed to build or run.
	Pct *float64
	// Reason says why Pct is nil when a failure caused it, for example
	// "test build failed: x_test.go:3:1: undefined: y" or "tests failed:
	// TestParse"; empty when Pct is set or nothing failed (no test files,
	// no statements).
	Reason string
}

// CoverageMeasurer is implemented by an Extractor that can measure
// coverage_pct by running the package tests (SPEC.md 6). Measuring runs
// code, so it is not part of Extract: callers opt in, type-assert the
// Extractor to CoverageMeasurer, and set RawMetrics.CoveragePct from the
// result after extraction. An extractor without it leaves coverage_pct
// null.
type CoverageMeasurer interface {
	// Coverage measures the packages pkgs, identifiers as Packages lists
	// them, of the module described by mod, in one run where it can. The
	// result has an entry for every package of pkgs. A package whose tests
	// fail to build or run gets a nil Pct and a Reason and does not fail
	// the call. The error reports a run that could not start at all, such
	// as a missing toolchain; a run cut short by ctx fills Reason for the
	// packages it did not reach instead.
	Coverage(ctx context.Context, mod *ModuleContext, pkgs []string) (map[string]Coverage, error)
}
