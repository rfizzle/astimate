package judge

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// changedFunction is the most complex function added or modified since the
// baseline: its cognitive complexity, its display name and its location,
// with the files every changed function lies in.
type changedFunction struct {
	// cognitive is the function's cognitive complexity; 0 when no function
	// changed.
	cognitive int
	// name is the qualified name with "(file:line)" when the location is
	// known; empty when no function changed.
	name string
	// file and line locate the function's declaration, file relative to
	// the package directory; empty when unknown or no function changed.
	file string
	line int
	// files holds the file, relative to the package directory, of each
	// changed function whose file is known; nil when none is.
	files map[string]bool
}

// worstChanged diffs a package's functions at head against those before it
// (metrics.ChangedFunctions) and returns the most complex changed one, or
// a zero changedFunction when none changed.
func worstChanged(before, head []metrics.FunctionInfo) *changedFunction {
	changed := metrics.ChangedFunctions(before, head)
	i := metrics.MostComplex(changed)
	if i < 0 {
		return &changedFunction{}
	}
	f := &changed[i]
	name := f.QualifiedName()
	if f.File != "" {
		name += " (" + f.File + ":" + strconv.Itoa(f.Line) + ")"
	}
	w := &changedFunction{cognitive: f.Cognitive, name: name, file: f.File, line: f.Line}
	for j := range changed {
		if file := changed[j].File; file != "" {
			if w.files == nil {
				w.files = make(map[string]bool)
			}
			w.files[file] = true
		}
	}
	return w
}

// locateFindings locates each of r's findings, exempted ones included, on
// the file and line that
// caused it, as far as d, the package's details, and worst, its most
// complex changed function (nil when unknown), can say:
//
//   - dup_blocks and duplication_pct on a duplicate block's occurrence,
//   - untested_exports on an untested export's declaration,
//   - globals on a global's declaration,
//   - sloc, largest_file_sloc, tokens_est and tokens_est_with_tests on the
//     largest file,
//   - changed_func_cognitive_max on the function it measures,
//   - any other metric, or one of the above with nothing recorded, on the
//     package's doc.go, else its first source file.
//
// Among several candidates it takes the first in a file holding a changed
// function, so the annotation lands on the diff, else the first. Files are
// made module-relative by joining r's package path. A finding with no
// candidate keeps no location, and renderers fall back to the package
// directory.
func locateFindings(r *report.Report, d *metrics.Details, worst *changedFunction) {
	for _, f := range r.AllFindings() {
		if pos := findingPosition(f.Metric, d, worst); pos.File != "" {
			f.Location = &report.Location{File: path.Join(r.PackagePath, pos.File), Line: pos.Line}
		}
	}
}

// locateCross locates r's dup_blocks_cross_pkg findings, exempted ones
// included, on the first occurrence of the first of blocks, the one their
// suggestion names first, so a renderer can annotate that file and line.
func locateCross(r *report.Report, blocks []metrics.CrossBlock) {
	if len(blocks) == 0 || len(blocks[0].Occurrences) == 0 {
		return
	}
	o := blocks[0].Occurrences[0]
	for _, f := range r.AllFindings() {
		if f.Metric == "dup_blocks_cross_pkg" {
			f.Location = &report.Location{File: o.File, Line: o.StartLine}
		}
	}
}

// findingPosition returns the package-relative position locateFindings
// puts a finding on metric at; its File is empty when there is none.
func findingPosition(metric string, d *metrics.Details, worst *changedFunction) metrics.Position {
	var changed map[string]bool
	if worst != nil {
		changed = worst.files
	}
	var pos metrics.Position
	switch metric {
	case "dup_blocks", "duplication_pct":
		pos = pickPosition(dupPositions(d.DupLocations), changed)
	case "untested_exports":
		pos = pickPosition(d.UntestedPositions, changed)
	case "globals":
		pos = pickPosition(d.GlobalPositions, changed)
	case "sloc", "largest_file_sloc", "tokens_est", "tokens_est_with_tests":
		pos = metrics.Position{File: d.LargestFile, Line: 1}
	case "changed_func_cognitive_max":
		if worst != nil {
			pos = metrics.Position{File: worst.file, Line: worst.line}
		}
	}
	if pos.File != "" {
		return pos
	}
	if slices.Contains(d.SourceFiles, "doc.go") {
		return metrics.Position{File: "doc.go", Line: 1}
	}
	if len(d.SourceFiles) > 0 {
		return metrics.Position{File: d.SourceFiles[0], Line: 1}
	}
	return metrics.Position{}
}

// pickPosition returns the first of ps in a file of changed, else the first
// with a file, else the zero Position.
func pickPosition(ps []metrics.Position, changed map[string]bool) metrics.Position {
	first := metrics.Position{}
	for _, p := range ps {
		if p.File == "" {
			continue
		}
		if changed[p.File] {
			return p
		}
		if first.File == "" {
			first = p
		}
	}
	return first
}

// dupPositions parses metrics.Details.DupLocations, each "file:start-end",
// into the position of each occurrence's first line, skipping any that
// does not parse.
func dupPositions(locs []string) []metrics.Position {
	ps := make([]metrics.Position, 0, len(locs))
	for _, loc := range locs {
		i := strings.LastIndexByte(loc, ':')
		if i <= 0 {
			continue
		}
		start, _, _ := strings.Cut(loc[i+1:], "-")
		line, err := strconv.Atoi(start)
		if err != nil {
			continue
		}
		ps = append(ps, metrics.Position{File: loc[:i], Line: line})
	}
	return ps
}
