package golang

import (
	"context"
	"fmt"
	"go/token"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// stdModulePath is the module path the standard library reports.
const stdModulePath = "std"

// stdAllModulePath is the module path of a load of the whole standard
// library by ExtractStdlibAll. No go.mod can declare it, so it never
// collides with a real module. Under it every standard-library package is
// internal, for internal_imports and fan_in alike (see isInternal and
// classifyImport), so the two count the same import edges. The
// single-package load of ExtractStdlib keeps stdModulePath, under which
// standard-library imports stay stdlib_imports.
const stdAllModulePath = "std/..."

// ExtractStdlib computes the v0 metrics of the standard-library package at
// importPath, with its test files, under the extractor options opts. It is
// intended for the acceptance invariants (SPEC.md 7.5), which measure one
// standard-library package that belongs to no module a caller could load
// with Extract. The package is loaded on its own, so fan_in and
// fan_in_tests are 0, since no other standard-library package is in the
// load; ExtractStdlibAll measures those. Like ExtractStdlibAll it parses
// the source files of a package whose syntax is not its source
// (parseSources), so a cgo package such as net, and unsafe, which
// go/packages gives no syntax, are measured from their source files. It
// returns an error when the load fails, which is how a toolchain without
// usable GOROOT sources shows, and one wrapping metrics.ErrUnknownPackage
// when the load yields no package at importPath.
func ExtractStdlib(ctx context.Context, importPath string, opts ...Option) (metrics.RawMetrics, error) {
	e := New(opts...)
	counter, err := e.counter()
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", importPath, err)
	}
	cfg := &packages.Config{Context: ctx, Mode: loadMode, Tests: true, Fset: token.NewFileSet()}
	roots, err := e.load(cfg, importPath)
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("loading %s: %w", importPath, ctxCause(ctx, err))
	}
	// index keeps the packages under its module path argument, so importPath
	// selects the package and its test variants and drops the test main.
	l, err := index(importPath, roots)
	if err != nil {
		return metrics.RawMetrics{}, err
	}
	p, ok := l.pkgs[importPath]
	if !ok {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", importPath, metrics.ErrUnknownPackage)
	}
	l.modulePath = stdModulePath
	l.fset = cfg.Fset
	if err := parseSources(l); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("loading %s: %w", importPath, err)
	}
	return assemble(ctx, l, p, assembleOptions{counter: counter, dup: e.dup})
}

// ExtractStdlibAll computes the v0 metrics of every package of the standard
// library (the go list pattern "std", vendored packages included), with
// its test files, under the extractor options opts. It loads the whole
// library in one go/packages call and shares that load, and so one reverse
// import graph, across every package, which makes fan_in and fan_in_tests
// count the standard-library packages importing each one. Every
// standard-library import is internal to that load, so internal_imports
// counts them and stdlib_imports is 0. Both metrics count the edges
// importedPackages lists, keyed by package path, so their sums over the
// result are equal. It is intended for calibration,
// which pools standard-library rows with module rows.
//
// It returns the metrics by import path and, separately, the packages that
// failed with their errors: a package that fails to load or to extract is
// left out of the metrics. The error is non-nil only when opts are invalid
// or the library could not be loaded at all, which is how a toolchain
// without usable GOROOT sources shows.
func ExtractStdlibAll(ctx context.Context, opts ...Option) (map[string]metrics.RawMetrics, map[string]error, error) {
	got, failed, _, err := extractStdlibAll(ctx, opts...)
	return got, failed, err
}

// ExtractStdlibAllFunctions is ExtractStdlibAll that also returns, by
// import path, the functions of every measured package the way
// Extractor.Functions lists them (metrics.FunctionLister), with File
// relative to the package directory. It is intended for calibration, which
// pools the per-function cognitive complexity behind
// changed_func_cognitive_max; the metrics and failures are exactly
// ExtractStdlibAll's.
func ExtractStdlibAllFunctions(ctx context.Context, opts ...Option,
) (map[string]metrics.RawMetrics, map[string][]metrics.FunctionInfo, map[string]error, error) {
	got, failed, l, err := extractStdlibAll(ctx, opts...)
	if err != nil {
		return nil, nil, nil, err
	}
	fns := make(map[string][]metrics.FunctionInfo, len(got))
	for path := range got {
		// assemble recorded the details of every package it measured.
		d, _ := l.detailsOf(path)
		fns[path] = functionInfos(l.fset, l.pkgs[path].Dir, d.functions)
	}
	return got, fns, failed, nil
}

// extractStdlibAll does the work of ExtractStdlibAll and also returns the
// load, whose recorded details hold each measured package's functions.
func extractStdlibAll(ctx context.Context, opts ...Option) (map[string]metrics.RawMetrics, map[string]error, *loaded, error) {
	e := New(opts...)
	counter, err := e.counter()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("extracting std: %w", err)
	}
	l, failed, err := loadStdlib(ctx, e.load)
	if err != nil {
		return nil, nil, nil, err
	}

	results := make([]metrics.RawMetrics, len(l.paths))
	errs := make([]error, len(l.paths))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(l.paths)) {
		wg.Go(func() {
			for i := range next {
				results[i], errs[i] = assemble(ctx, l, l.pkgs[l.paths[i]],
					assembleOptions{counter: counter, dup: e.dup})
			}
		})
	}
	for i := range l.paths {
		next <- i
	}
	close(next)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("extracting std: %w", err)
	}

	got := make(map[string]metrics.RawMetrics, len(l.paths))
	for i, path := range l.paths {
		if errs[i] != nil {
			failed[path] = errs[i]
			continue
		}
		got[path] = results[i]
	}
	return got, failed, l, nil
}

// loadStdlib loads the whole standard library with load, in one call, and
// indexes it under stdAllModulePath with indexStdlib, returning the load and
// the packages indexStdlib left out.
func loadStdlib(ctx context.Context, load loadFunc) (*loaded, map[string]error, error) {
	cfg := &packages.Config{Context: ctx, Mode: loadMode, Tests: true, Fset: token.NewFileSet()}
	roots, err := load(cfg, "std")
	if err != nil {
		return nil, nil, fmt.Errorf("loading std: %w", ctxCause(ctx, err))
	}
	l, failed := indexStdlib(roots)
	if len(l.paths) == 0 {
		return nil, nil, fmt.Errorf("loading std: %w", ErrNoPackages)
	}
	l.fset = cfg.Fset
	if err := parseSources(l); err != nil {
		return nil, nil, fmt.Errorf("loading std: %w", err)
	}
	return l, failed, nil
}

// ctxCause returns ctx's error when it has one and err otherwise:
// go/packages flattens a cancellation into its message, and callers match
// the context's own error.
func ctxCause(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

// indexStdlib sorts the packages of a load of the pattern "std" into
// loaded's maps under stdAllModulePath, as index does for a module, and
// drops generated test mains. Where index fails the whole load on the first
// package with errors, indexStdlib leaves that package and its test
// variants out, so it is not measured and imports nothing, and returns it
// among the failures keyed by import path; one broken package does not
// cost the rest of the library.
func indexStdlib(roots []*packages.Package) (*loaded, map[string]error) {
	sorted := slices.Clone(roots)
	slices.SortFunc(sorted, func(a, b *packages.Package) int { return strings.Compare(a.ID, b.ID) })
	failed := make(map[string]error)
	for _, p := range sorted {
		under, _ := forTest(p)
		if under == "" {
			under = p.PkgPath
		}
		if _, seen := failed[under]; !seen && len(p.Errors) > 0 {
			failed[under] = packageError(p)
		}
	}

	l := &loaded{
		modulePath: stdAllModulePath,
		pkgs:       make(map[string]*packages.Package, len(sorted)),
		tests:      make(map[string]*packages.Package),
		xtests:     make(map[string]*packages.Package),
	}
	plain := make([]*packages.Package, 0, len(sorted))
	for _, p := range sorted {
		under, external := forTest(p)
		switch {
		case under == "":
			if failed[p.PkgPath] == nil {
				plain = append(plain, p)
			}
		case failed[under] != nil:
			// A test variant of a failed package is dropped with it.
		case external:
			l.xtests[under] = p
		default:
			l.tests[under] = p
		}
	}
	for _, p := range plain {
		// The generated test main of a package with tests is "path.test"; no
		// standard-library package path ends in ".test".
		if _, ok := strings.CutSuffix(p.PkgPath, ".test"); ok {
			continue
		}
		l.pkgs[p.PkgPath] = p
		l.paths = append(l.paths, p.PkgPath)
	}
	slices.Sort(l.paths)
	return l, failed
}
