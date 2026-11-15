package golang

import (
	"context"
	"fmt"
	"slices"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/dup"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/extract"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/inventory"
)

// Details returns the names behind untested_exports, the duplicate block
// locations and the cross-package blocks touching the package with import
// path pkg, the declarations of its untested exports and globals, the names
// of its globals, its largest file, its functions, its top-level
// declarations and its source files, from the details its most recent
// Extract on mod recorded and the memoized cross-package pass. When nothing
// is recorded it runs Extract first, so it fails exactly when Extract would.
func (e *Extractor) Details(ctx context.Context, mod *metrics.ModuleContext, pkg string) (inventory.Details, error) {
	d, l, dir, err := e.recorded(ctx, mod, pkg)
	if err != nil {
		return inventory.Details{}, err
	}
	var cross []inventory.CrossBlock
	if dup.Applies(l.Module) {
		// Extract has run the cross-package pass for e.dup, so this is the
		// memoized result and reads no file.
		c, err := l.CrossDup.Cross(l.Module, load.OSFiles{}, e.dup)
		if err != nil {
			return inventory.Details{}, fmt.Errorf("details of %s: %w", pkg, err)
		}
		cross = c.BlocksOf(pkg)
	}
	files := relFiles(dir, d.Files)
	slices.Sort(files)
	locs, blocks := duptok.Details(d.DupBlocks, func(f string) string { return load.RelFile(dir, f) })
	return inventory.Details{
		UntestedExports:  slices.Clone(d.Untested.Names),
		UntestedExcluded: slices.Clone(d.Untested.Excluded),
		UntestedCovered:  slices.Clone(d.Covered),

		UntestedRules: inventory.UntestedRules{
			OptionFuncs:      d.Untested.OptionFuncs,
			InterfaceMethods: d.Untested.InterfaceMethods,
			TestSupport:      d.Untested.TestSupport,
		}.Clone(),
		DupLocations:      locs,
		DupBlocks:         blocks,
		CrossBlocks:       cross,
		UntestedPositions: inspect.Positions(l.Fset, dir, d.Untested.Pos),
		GlobalPositions:   inspect.Positions(l.Fset, dir, d.Globals.Pos),
		GlobalNames:       slices.Clone(d.Globals.Names),
		LargestFile:       load.RelFile(dir, d.LargestFile),
		Functions:         inspect.FunctionInfos(l.Fset, dir, d.Functions),
		Exports:           inspect.Symbols(l.Fset, dir, d.Exports.Exported),
		Unexported:        inspect.Symbols(l.Fset, dir, d.Exports.Unexported),
		SourceFiles:       files,
	}, nil
}

// Functions returns the top-level functions and methods of the package with
// import path pkg in declaration order, init functions included, with the
// cognitive complexity and body fingerprint its most recent Extract on mod
// recorded (metrics.FunctionLister). File is relative to the package
// directory. When nothing is recorded it runs Extract first, so it fails
// exactly when Extract would.
func (e *Extractor) Functions(ctx context.Context, mod *metrics.ModuleContext, pkg string) ([]inventory.FunctionInfo, error) {
	d, l, dir, err := e.recorded(ctx, mod, pkg)
	if err != nil {
		return nil, err
	}
	return inspect.FunctionInfos(l.Fset, dir, d.Functions), nil
}

// recorded returns the details the most recent Extract of pkg on mod
// recorded, running Extract first when there are none, the load they came
// from, and pkg's directory, empty when the load has no such package.
func (e *Extractor) recorded(ctx context.Context, mod *metrics.ModuleContext, pkg string) (extract.Details, *extract.Module, string, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return extract.Details{}, nil, "", err
	}
	d, ok := l.DetailsOf(pkg)
	if !ok {
		if _, err := e.Extract(ctx, mod, pkg); err != nil {
			return extract.Details{}, nil, "", err
		}
		d, _ = l.DetailsOf(pkg)
	}
	dir := ""
	if p, ok := l.Pkgs[pkg]; ok {
		dir = p.Dir
	}
	return d, l, dir, nil
}

// relFiles returns each of files relative to dir in slash form (see
// load.RelFile).
func relFiles(dir string, files []string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = load.RelFile(dir, f)
	}
	return out
}
