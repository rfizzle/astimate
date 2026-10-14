package details

import (
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// Details is the details block of a report: what some of the metrics
// count, named and located, so a consumer of the JSON can see where a
// duplicate block lies or which export has no test. Every field is omitted
// when empty. Files are relative to the package directory in slash form,
// as metrics.Details gives them, except the occurrences of CrossBlocks,
// which are relative to the module root.
type Details struct {
	// Duplicates are the occurrences of the blocks counted by dup_blocks,
	// block by block, longest block first; in a check, the blocks the
	// change added come first (SPEC.md 10.2).
	Duplicates []Span `json:"duplicates,omitempty"`
	// UntestedExports are the exports counted by untested_exports, sorted
	// by name; in a check, those the change added come first.
	UntestedExports []Declaration `json:"untested_exports,omitempty"`
	// ExcludedUntested names the exports left out of untested_exports by
	// an //astimate:untested directive, sorted.
	ExcludedUntested []string `json:"excluded_untested,omitempty"`
	// Globals are the declarations counted by globals, in declaration
	// order, each named and located as far as the extractor records it.
	Globals []Global `json:"globals,omitempty"`
	// LargestFile is the file largest_file_sloc measures.
	LargestFile string `json:"largest_file,omitempty"`
	// CrossBlocks are the cross-package duplicate blocks behind
	// dup_blocks_cross_pkg: for a package, those touching it; for the
	// module row, every one in the module.
	CrossBlocks []CrossBlock `json:"cross_blocks,omitempty"`
	// Functions are the package's functions in declaration order with the
	// per-function values behind cognitive_total, cognitive_p90,
	// cyclomatic_total and cyclomatic_p90.
	Functions []Function `json:"functions,omitempty"`
}

// Function is one function or method with its complexity.
type Function struct {
	// Name is the function name, prefixed with the receiver and a dot for
	// a method.
	Name string `json:"name"`
	// File is relative to the package directory, in slash form; empty
	// (omitted) when unknown.
	File string `json:"file,omitempty"`
	// Line is 1-based; 0 (omitted) when unknown.
	Line int `json:"line,omitempty"`
	// Cognitive and Cyclomatic are the function's cognitive and cyclomatic
	// complexity.
	Cognitive  int `json:"cognitive"`
	Cyclomatic int `json:"cyclomatic"`
}

// Span is a range of lines in one file.
type Span struct {
	// File is relative to the package directory, in slash form.
	File string `json:"file"`
	// StartLine and EndLine are the first and last 1-based lines.
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// Declaration is a named declaration and, when the extractor records it,
// where it is.
type Declaration struct {
	// Name is the declaration's name as the extractor gives it.
	Name string `json:"name"`
	// File is relative to the package directory, in slash form; empty
	// (omitted) when unknown.
	File string `json:"file,omitempty"`
	// Line is 1-based; 0 (omitted) when unknown.
	Line int `json:"line,omitempty"`
}

// Global is one package-level variable counted by globals, named and
// located as far as the extractor records it.
type Global struct {
	// Name is the variable's name; empty (omitted) when unknown.
	Name string `json:"name,omitempty"`
	// File is relative to the package directory, in slash form; empty
	// (omitted) when unknown.
	File string `json:"file,omitempty"`
	// Line is 1-based; 0 (omitted) when unknown.
	Line int `json:"line,omitempty"`
}

// CrossBlock is one duplicate block shared between packages.
type CrossBlock struct {
	// Occurrences are the block's occurrences, at least two.
	Occurrences []CrossOccurrence `json:"occurrences"`
}

// CrossOccurrence is one occurrence of a cross-package block.
type CrossOccurrence struct {
	// Package is the directory of the package holding the occurrence,
	// relative to the module root as package_path is.
	Package string `json:"package"`
	// File is relative to the module root, in slash form.
	File string `json:"file"`
	// StartLine and EndLine are the first and last 1-based lines.
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// NewDetails converts d to a report details block, with pkgPath mapping a
// native package identifier, as metrics.Occurrence.Package holds it, to
// the package path the report uses. A duplicate location that is not
// "file:start-end" is skipped. It returns nil when d has nothing to show,
// so the report omits the block.
func NewDetails(d *metrics.Details, pkgPath func(string) string) *Details {
	out := Details{
		ExcludedUntested: d.UntestedExcluded,
		LargestFile:      d.LargestFile,
	}
	if len(d.DupLocations) > 0 {
		out.Duplicates = make([]Span, 0, len(d.DupLocations))
		for _, loc := range d.DupLocations {
			if s, ok := parseSpan(loc); ok {
				out.Duplicates = append(out.Duplicates, s)
			}
		}
	}
	if len(d.UntestedExports) > 0 {
		out.UntestedExports = make([]Declaration, len(d.UntestedExports))
		for i, name := range d.UntestedExports {
			out.UntestedExports[i].Name = name
			if i < len(d.UntestedPositions) {
				out.UntestedExports[i].File = d.UntestedPositions[i].File
				out.UntestedExports[i].Line = d.UntestedPositions[i].Line
			}
		}
	}
	if n := max(len(d.GlobalNames), len(d.GlobalPositions)); n > 0 {
		out.Globals = make([]Global, n)
		for i := range out.Globals {
			if i < len(d.GlobalNames) {
				out.Globals[i].Name = d.GlobalNames[i]
			}
			if i < len(d.GlobalPositions) {
				out.Globals[i].File = d.GlobalPositions[i].File
				out.Globals[i].Line = d.GlobalPositions[i].Line
			}
		}
	}
	if len(d.CrossBlocks) > 0 {
		out.CrossBlocks = make([]CrossBlock, len(d.CrossBlocks))
		for i, b := range d.CrossBlocks {
			occ := make([]CrossOccurrence, len(b.Occurrences))
			for j, o := range b.Occurrences {
				occ[j] = CrossOccurrence{Package: pkgPath(o.Package), File: o.File, StartLine: o.StartLine, EndLine: o.EndLine}
			}
			out.CrossBlocks[i].Occurrences = occ
		}
	}
	out.Functions = newFunctions(d.Functions)
	if len(out.Duplicates) == 0 && len(out.UntestedExports) == 0 && len(out.ExcludedUntested) == 0 &&
		len(out.Globals) == 0 && out.LargestFile == "" && len(out.CrossBlocks) == 0 && len(out.Functions) == 0 {
		return nil
	}
	return &out
}

// newFunctions converts the function records of a package's details to
// the report's, in the same order; nil for none.
func newFunctions(fns []metrics.FunctionInfo) []Function {
	if len(fns) == 0 {
		return nil
	}
	out := make([]Function, len(fns))
	for i := range fns {
		f := &fns[i]
		out[i] = Function{Name: f.QualifiedName(), File: f.File, Line: f.Line, Cognitive: f.Cognitive, Cyclomatic: f.Cyclomatic}
	}
	return out
}

// parseSpan parses a metrics.Details.DupLocations entry, "file:start-end".
func parseSpan(loc string) (Span, bool) {
	i := strings.LastIndexByte(loc, ':')
	if i <= 0 {
		return Span{}, false
	}
	start, end, ok := strings.Cut(loc[i+1:], "-")
	if !ok {
		return Span{}, false
	}
	s, err := strconv.Atoi(start)
	if err != nil {
		return Span{}, false
	}
	e, err := strconv.Atoi(end)
	if err != nil {
		return Span{}, false
	}
	return Span{File: loc[:i], StartLine: s, EndLine: e}, true
}
