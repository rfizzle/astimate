// Package inventory holds the per-package records an extractor hands over
// beside its metrics: the identifiers and positions behind some counts
// (Details), the functions with their complexity and fingerprints
// (FunctionInfo) and the function-level diff over them. It imports the
// standard library only, so the metrics contract can return its types.
package inventory

import "slices"

// Details carries the identifiers behind some metrics.RawMetrics counts,
// for suggestions that name what to fix and for the report's details
// block. It is a side channel: it never enters metrics.RawMetrics.
type Details struct {
	// UntestedExports names the exported functions and methods counted by
	// untested_exports, sorted. Its length equals untested_exports.
	UntestedExports []string
	// UntestedExcluded names the exported functions and methods an
	// implementation left out of untested_exports on request, sorted.
	UntestedExcluded []string
	// UntestedCovered names the exports of UntestedExports whose body a
	// coverage run (metrics.CoverageMeasurer) executed, sorted; nil
	// without one or when it executed none of them.
	UntestedCovered []string
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
	// package they lie. For a package (metrics.Detailer) it holds the
	// blocks that touch the package, so its length equals the package's
	// dup_blocks_cross_pkg; for the module row (metrics.ModuleDetailer) it
	// holds every cross-package block of the module, so its length equals
	// the row's dup_blocks_cross_pkg. Blocks are in order of first
	// occurrence. Nil when there are none or the implementation does not
	// compute them.
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
	// Functions are the package's functions with their cognitive and
	// cyclomatic complexity, as metrics.FunctionLister lists them, for the
	// report's details. Nil when there are none or the implementation does
	// not list them.
	Functions []FunctionInfo
	// Exports are the package-level declarations exported_symbols counts,
	// in declaration order, so its length equals exported_symbols, and
	// Unexported the package's other package-level funcs, methods, types,
	// vars and consts, _ and init left out: what a check reads to tell an
	// export made unexported (SPEC.md 8.1, Reshaped exports). Nil when
	// there are none or the implementation does not record them.
	Exports, Unexported []Symbol
	// SourceFiles are the package's non-test source files relative to the
	// package directory in slash form, sorted. Nil when unknown.
	SourceFiles []string
	// Roles maps the identifier of each package an occurrence of
	// CrossBlocks lies in to its role (metrics.RawMetrics.Role), for the
	// module row (metrics.ModuleDetailer), whose rule leaves out the
	// declarative blocks shared only by composition roots (SPEC.md 8.1).
	// Nil for a package's details and when the implementation records no
	// roles.
	Roles map[string]string
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
// commits, its kind, and its occurrences as in DupLocations.
type DupBlock struct {
	Anchors [2]uint64
	// Kind is what the block is made of (SPEC.md 6.3): "code",
	// "declarative", "idiom" or "self-overlap"; empty when the
	// implementation does not classify blocks.
	Kind      string
	Locations []string
}

// Symbol is one package-level declaration: a func or method, a type, a
// var or a const name.
type Symbol struct {
	// Kind is "func" (a method too), "type", "var" or "const".
	Kind string
	// Receiver is a method's receiver base type name, as in FunctionInfo;
	// empty for anything else.
	Receiver string
	// Name is the declared name.
	Name string
	// Position is where it is declared; zero when unknown, as in a
	// baseline.
	Position
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
	// Kind is what the block is made of, as DupBlock.Kind.
	Kind string
	// Tokens is the block's length in normalized tokens; 0 when unknown.
	Tokens int
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
		out[i] = CrossBlock{Kind: blocks[i].Kind, Tokens: blocks[i].Tokens, Occurrences: occ}
	}
	return out
}

// Occurrence is one occurrence of a duplicate block.
type Occurrence struct {
	// Package is the native identifier of the package holding the
	// occurrence, as metrics.Extractor.Packages lists it.
	Package string
	// File is the file holding the occurrence, relative to the module root
	// in slash form.
	File string
	// StartLine and EndLine are the first and last 1-based lines holding
	// the occurrence's tokens.
	StartLine, EndLine int
}
