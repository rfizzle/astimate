package load

import (
	"context"
	"fmt"
	"go/token"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// StdModulePath is the module path the standard library reports, and the
// Path of a single standard-library package loaded by StdPackage.
const StdModulePath = "std"

// StdAllModulePath is the Path of a load of the whole standard library by
// Stdlib. No go.mod can declare it, so it never collides with a real module.
// Under it every standard-library package is internal, for internal_imports
// and fan_in alike, so the two count the same import edges. The
// single-package load of StdPackage keeps StdModulePath, under which
// standard-library imports stay stdlib_imports.
const StdAllModulePath = "std/..."

// StdPackage loads the standard-library package at importPath, with its test
// variants, and returns it as a Module whose Path is StdModulePath. Like
// Stdlib it parses the source files of a package whose syntax is not its
// source, so a cgo package such as net, and unsafe, which go/packages gives
// no syntax, are measured from their source files. The Module has no
// package at importPath when the load yields none there; the caller checks.
func StdPackage(ctx context.Context, load Func, importPath string) (*Module, error) {
	roots, fset, err := loadPattern(ctx, load, importPath)
	if err != nil {
		return nil, err
	}
	// index keeps the packages under its module path argument, so importPath
	// selects the package and its test variants and drops the test main.
	m, err := index(importPath, roots)
	if err != nil {
		return nil, err
	}
	m.Path = StdModulePath
	m.Fset = fset
	if err := m.parseSources(); err != nil {
		return nil, fmt.Errorf("loading %s: %w", importPath, err)
	}
	return m, nil
}

// Stdlib loads the whole standard library with load, in one call, and
// indexes it under StdAllModulePath, returning the load and the packages it
// left out with their errors: a package that fails to load is not measured
// and imports nothing, and one broken package does not cost the rest of the
// library. A load that yields no package returns a Module with no Paths and
// a nil error; the caller names it.
func Stdlib(ctx context.Context, load Func) (*Module, map[string]error, error) {
	roots, fset, err := loadPattern(ctx, load, "std")
	if err != nil {
		return nil, nil, err
	}
	m, failed := indexStdlib(roots)
	if len(m.Paths) == 0 {
		return m, failed, nil
	}
	m.Fset = fset
	if err := m.parseSources(); err != nil {
		return nil, nil, fmt.Errorf("loading std: %w", err)
	}
	return m, failed, nil
}

// loadPattern loads the standard-library packages pattern matches, with
// their test variants, into a new file set, and returns them with it.
func loadPattern(ctx context.Context, load Func, pattern string) ([]*packages.Package, *token.FileSet, error) {
	cfg := &packages.Config{Context: ctx, Mode: mode, Tests: true, Fset: token.NewFileSet()}
	roots, err := load(cfg, pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("loading %s: %w", pattern, ctxCause(ctx, err))
	}
	return roots, cfg.Fset, nil
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

// indexStdlib sorts the packages of a load of the pattern "std" into a
// Module's maps under StdAllModulePath, as index does for a module, and
// drops generated test mains. Where index fails the whole load on the first
// package with errors, indexStdlib leaves that package and its test
// variants out and returns it among the failures keyed by import path.
func indexStdlib(roots []*packages.Package) (*Module, map[string]error) {
	sorted := slices.Clone(roots)
	slices.SortFunc(sorted, byID)
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
	m := newModule(StdAllModulePath, len(sorted))
	// The generated test main of a package with tests is "path.test"; no
	// standard-library package path ends in ".test".
	m.fill(sorted, func(path string) bool { return failed[path] != nil }, func(path string) bool {
		return strings.HasSuffix(path, ".test")
	})
	return m, failed
}
