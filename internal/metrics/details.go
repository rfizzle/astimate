package metrics

import "context"

// Details carries the identifiers behind some RawMetrics counts, for
// suggestions that name what to fix. It is a side channel: it never enters
// RawMetrics or the report schema.
type Details struct {
	// UntestedExports names the exported functions and methods counted by
	// untested_exports, sorted. Its length equals untested_exports.
	UntestedExports []string
	// UntestedExcluded names the exported functions and methods an
	// implementation left out of untested_exports on request, sorted.
	UntestedExcluded []string
	// DupLocations lists every occurrence of every duplicate block counted
	// by dup_blocks, block by block in order of first occurrence, each
	// rendered "file:start-end" with file relative to the package directory
	// and start and end the first and last lines of the occurrence.
	DupLocations []string
}

// Detailer is an optional interface an Extractor implements when it can name
// what some of its counts refer to. Callers type-assert an Extractor to it
// and fall back to counts alone when the assertion fails.
type Detailer interface {
	// Details returns the details of pkg. They are valid only after Extract
	// for pkg on the same mod; an implementation may call Extract itself
	// when nothing is recorded for pkg yet. It fails for a package Extract
	// would reject.
	Details(ctx context.Context, mod *ModuleContext, pkg string) (Details, error)
}
