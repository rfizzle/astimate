package metrics

import "context"

// ModuleRowID is the package identifier of the module-level row: the key
// its metrics are stored under in a baseline, and the package path it is
// reported under by check.
const ModuleRowID = "module"

// ModuleMetrics is implemented by an Extractor that measures the module as
// a whole as well as package by package. Callers type-assert an Extractor
// to ModuleMetrics; an extractor without it has no module row.
//
// The module row exists for metrics that one edit can change in two
// packages at once, such as dup_blocks_cross_pkg: copying a function from
// package a into package b raises the count of both, so a per-package
// baseline would blame b for an edit it did not receive. The row carries
// the module-wide value (for dup_blocks_cross_pkg, the number of distinct
// cross-package blocks, not the sum over packages), is baselined under
// ModuleRowID like a package and is evaluated by the same thresholds.
type ModuleMetrics interface {
	// ModuleRow returns the module-level row of the module described by
	// mod: a RawMetrics whose v0 fields are zero and whose v1 fields are
	// null except the module-wide ones the extractor computes.
	ModuleRow(ctx context.Context, mod *ModuleContext) (RawMetrics, error)
}
