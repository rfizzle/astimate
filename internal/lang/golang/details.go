package golang

import (
	"context"
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Details returns the names behind untested_exports, the duplicate block
// locations and the cross-package blocks touching the package with import
// path pkg, the declarations of its untested exports and globals, the names
// of its globals, its largest file and its source files, from the details
// its most recent Extract on mod recorded and the memoized cross-package
// pass. When nothing is recorded it runs Extract first, so it fails exactly
// when Extract would.
func (e *Extractor) Details(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.Details, error) {
	d, l, dir, err := e.recorded(ctx, mod, pkg)
	if err != nil {
		return metrics.Details{}, err
	}
	locs := make([]string, 0, len(d.dupLocations))
	for _, loc := range d.dupLocations {
		locs = append(locs, relLocation(dir, loc))
	}
	var cross []metrics.CrossBlock
	if crossApplies(l) {
		// Extract has run the cross-package pass for e.dup, so this is the
		// memoized result and reads no file.
		c, err := crossDuplication(l, osFiles{}, e.dup)
		if err != nil {
			return metrics.Details{}, fmt.Errorf("details of %s: %w", pkg, err)
		}
		cross = crossBlocksOf(c, pkg)
	}
	files := make([]string, 0, len(d.files))
	for _, f := range d.files {
		files = append(files, relFile(dir, f))
	}
	slices.Sort(files)
	return metrics.Details{
		UntestedExports:   slices.Clone(d.untestedNames),
		UntestedExcluded:  slices.Clone(d.untestedExcluded),
		DupLocations:      locs,
		CrossBlocks:       cross,
		UntestedPositions: positions(l.fset, dir, d.untestedPos),
		GlobalPositions:   positions(l.fset, dir, d.globalPos),
		GlobalNames:       slices.Clone(d.globalNames),
		LargestFile:       relFile(dir, d.largestFile),
		SourceFiles:       files,
	}, nil
}

// positions resolves each of ps in fset to a file relative to dir and a
// line, honoring //line directives, so a cgo package's rewritten sources
// map back to the files cgo read. It returns nil for no positions, and an
// invalid position, or a nil fset, yields an empty Position.
func positions(fset *token.FileSet, dir string, ps []token.Pos) []metrics.Position {
	if len(ps) == 0 {
		return nil
	}
	out := make([]metrics.Position, len(ps))
	for i, p := range ps {
		if fset == nil || !p.IsValid() {
			continue
		}
		pos := fset.Position(p)
		out[i] = metrics.Position{File: relFile(dir, pos.Filename), Line: pos.Line}
	}
	return out
}

// Functions returns the top-level functions and methods of the package with
// import path pkg in declaration order, init functions included, with the
// cognitive complexity and body fingerprint its most recent Extract on mod
// recorded (metrics.FunctionLister). File is relative to the package
// directory. When nothing is recorded it runs Extract first, so it fails
// exactly when Extract would.
func (e *Extractor) Functions(ctx context.Context, mod *metrics.ModuleContext, pkg string) ([]metrics.FunctionInfo, error) {
	d, l, dir, err := e.recorded(ctx, mod, pkg)
	if err != nil {
		return nil, err
	}
	return functionInfos(l.fset, dir, d.functions), nil
}

// functionInfos converts the per-function records of a package in dir to
// metrics.FunctionInfo, resolving each position in fset to a file relative
// to dir and a line. A nil fset leaves File and Line empty.
func functionInfos(fset *token.FileSet, dir string, fcs []funcComplexity) []metrics.FunctionInfo {
	fns := make([]metrics.FunctionInfo, len(fcs))
	for i, fc := range fcs {
		fns[i] = metrics.FunctionInfo{
			Receiver:    fc.receiver,
			Name:        fc.ident,
			Fingerprint: fc.fingerprint,
			Cognitive:   fc.cognitive,
		}
		if fset != nil && fc.pos.IsValid() {
			pos := fset.Position(fc.pos)
			fns[i].File, fns[i].Line = relFile(dir, pos.Filename), pos.Line
		}
	}
	return fns
}

// recorded returns the details the most recent Extract of pkg on mod
// recorded, running Extract first when there are none, the load they came
// from, and pkg's directory, empty when the load has no such package.
func (e *Extractor) recorded(ctx context.Context, mod *metrics.ModuleContext, pkg string) (details, *loaded, string, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return details{}, nil, "", err
	}
	d, ok := l.detailsOf(pkg)
	if !ok {
		if _, err := e.Extract(ctx, mod, pkg); err != nil {
			return details{}, nil, "", err
		}
		d, _ = l.detailsOf(pkg)
	}
	dir := ""
	if p, ok := l.pkgs[pkg]; ok {
		dir = p.Dir
	}
	return d, l, dir, nil
}

// relFile returns file relative to dir in slash form, falling back to the
// base name when dir is empty or the file cannot be related to it. An empty
// file stays empty.
func relFile(dir, file string) string {
	if file == "" {
		return ""
	}
	rel := filepath.Base(file)
	if dir != "" {
		if r, err := filepath.Rel(dir, file); err == nil {
			rel = r
		}
	}
	return filepath.ToSlash(rel)
}

// relLocation renders loc as "file:start-end" with file relative to dir, in
// slash form. It falls back to the base name when dir is empty or the file
// cannot be related to it.
func relLocation(dir string, loc duptok.Location) string {
	return relFile(dir, loc.File) + ":" + strconv.Itoa(loc.StartLine) + "-" + strconv.Itoa(loc.EndLine)
}
