package metrics

import (
	"context"
	"slices"
)

// Details carries the identifiers behind some RawMetrics counts, for
// suggestions that name what to fix and for the report's details block.
// It is a side channel: it never enters RawMetrics.
type Details struct {
	// UntestedExports names the exported functions and methods counted by
	// untested_exports, sorted. Its length equals untested_exports.
	UntestedExports []string
	// UntestedExcluded names the exported functions and methods an
	// implementation left out of untested_exports on request, sorted.
	UntestedExcluded []string
	// UntestedRules lists what the refinement rules of untested_exports
	// (SPEC.md 6.4) took out of the count or counted as tested.
	UntestedRules UntestedRules
	// DupLocations lists every occurrence of every duplicate block counted
	// by dup_blocks, ordered by the length in tokens of the block, longest
	// first, ties in file order, each rendered "file:start-end" with file
	// relative to the package directory and start and end the first and
	// last lines of the occurrence.
	DupLocations []string
	// DupBlocks groups DupLocations by block, in the same order. Nil when
	// the implementation does not group them.
	DupBlocks []DupBlock
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
	// GlobalNames names the globals counted by globals, index for index
	// with GlobalPositions, in the same order. Nil when there are none or
	// the implementation does not record them; when set, its length equals
	// globals.
	GlobalNames []string
	// LargestFile is the non-test file with the most source lines, the one
	// largest_file_sloc measures, relative to the package directory in
	// slash form; the first such file on a tie. Empty when unknown.
	LargestFile string
	// SourceFiles are the package's non-test source files relative to the
	// package directory in slash form, sorted. Nil when unknown.
	SourceFiles []string
}

// UntestedRules lists the exports that rules a through d of SPEC.md 6.4
// settled, each sorted, nil when there are none or the implementation has
// no such rule.
type UntestedRules struct {
	// OptionFuncs are the functional-option constructors left out of
	// untested_exports because their body only sets fields of the options
	// value.
	OptionFuncs []string
	// InterfaceMethods are the exported methods of unexported types left
	// out because the type implements an interface of the module that a
	// test holds.
	InterfaceMethods []string
	// TestSupport are the exports of a test-support package counted as
	// tested only because the package's own non-test code refers to them.
	TestSupport []string
}

// Clone returns a deep copy of r.
func (r UntestedRules) Clone() UntestedRules {
	return UntestedRules{
		OptionFuncs:      slices.Clone(r.OptionFuncs),
		InterfaceMethods: slices.Clone(r.InterfaceMethods),
		TestSupport:      slices.Clone(r.TestSupport),
	}
}

// DupBlock is one duplicate block: hashes of the first and of the last
// duplication.min_tokens tokens of its normalized sequence, equal across
// commits, and its occurrences as in DupLocations.
type DupBlock struct {
	Anchors   [2]uint64
	Locations []string
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

// MapPackages returns copies of blocks with each occurrence's Package
// replaced by rename(Package), as when blocks move between package
// identifiers and module-relative directories; blocks is left unchanged
// and nil stays nil.
func MapPackages(blocks []CrossBlock, rename func(string) string) []CrossBlock {
	if blocks == nil {
		return nil
	}
	out := make([]CrossBlock, len(blocks))
	for i := range blocks {
		occ := make([]Occurrence, len(blocks[i].Occurrences))
		for j, o := range blocks[i].Occurrences {
			o.Package = rename(o.Package)
			occ[j] = o
		}
		out[i] = CrossBlock{Occurrences: occ}
	}
	return out
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
