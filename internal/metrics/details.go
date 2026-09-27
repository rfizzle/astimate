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
	// CrossBlocks lists the cross-package duplicate blocks behind
	// dup_blocks_cross_pkg, each with all its occurrences, in whichever
	// package they lie. For a package (Detailer) it holds the blocks that
	// touch the package, so its length equals the package's
	// dup_blocks_cross_pkg; for the module row (ModuleDetailer) it holds
	// every cross-package block of the module, so its length equals the
	// row's dup_blocks_cross_pkg. Blocks are in order of first occurrence.
	// Nil when there are none or the implementation does not compute them.
	CrossBlocks []CrossBlock
	// UntestedPositions are the declarations of UntestedExports, index for
	// index. Nil when the implementation does not record them.
	UntestedPositions []Position
	// GlobalPositions are the declarations of the names counted by globals,
	// in declaration order. Nil when there are none or the implementation
	// does not record them.
	GlobalPositions []Position
	// LargestFile is the non-test file with the most source lines, the one
	// largest_file_sloc measures, relative to the package directory in
	// slash form; the first such file on a tie. Empty when unknown.
	LargestFile string
	// SourceFiles are the package's non-test source files relative to the
	// package directory in slash form, sorted. Nil when unknown.
	SourceFiles []string
}

// Position is a place in a package's source, for renderers that annotate
// the line a finding refers to.
type Position struct {
	// File is relative to the package directory, in slash form.
	File string
	// Line is 1-based; 0 when unknown.
	Line int
}

// CrossBlock is one duplicate block whose occurrences lie in two or more
// packages of a module.
type CrossBlock struct {
	// Occurrences are the block's occurrences, at least two, in order of
	// package identifier, then file, then line.
	Occurrences []Occurrence
}

// Occurrence is one occurrence of a duplicate block.
type Occurrence struct {
	// Package is the native identifier of the package holding the
	// occurrence, as Extractor.Packages lists it.
	Package string
	// File is the file holding the occurrence, relative to the module root
	// in slash form.
	File string
	// StartLine and EndLine are the first and last 1-based lines holding
	// the occurrence's tokens.
	StartLine, EndLine int
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

// ModuleDetailer is an optional interface an Extractor that implements
// ModuleMetrics implements when it can name what the module row's counts
// refer to. Callers type-assert an Extractor to it and fall back to counts
// alone when the assertion fails.
type ModuleDetailer interface {
	// ModuleDetails returns the details of the module row of the module
	// described by mod: today only CrossBlocks, every cross-package block
	// behind the row's dup_blocks_cross_pkg. It fails when ModuleRow would.
	ModuleDetails(ctx context.Context, mod *ModuleContext) (Details, error)
}
