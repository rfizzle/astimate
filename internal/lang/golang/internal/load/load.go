// Package load loads a Go module, or the standard library, once with
// go/packages and indexes its packages for the Go extractor's metrics.
//
// A Module is built once per module root and shared, read-only, by every
// metric for every package in the module. It holds the file set, the module
// path, every non-test module package by import path with its in-package and
// external test packages, and, for a cgo package, its source files parsed
// once more (see Module.SourceSyntax). The package also holds the file
// source every byte-reading metric goes through, so one extraction reads
// each file at most once (FileCache).
package load

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// mode is the go/packages mode every module is loaded with: names, files,
// syntax and full type information for the module packages, plus module
// metadata. It leaves out NeedDeps, so only the packages matched by the load
// pattern, the module's packages and their test variants, are parsed and
// type-checked from source; a dependency outside the module gets its types
// from compiler export data and keeps its name, path and module but no
// syntax. The metrics need no more than that of a dependency: import paths
// for fan-in, the Module for import classification, and types for the
// interface checks of untested_exports. A dependency whose export data go
// list cannot build, such as a cgo package with no C compiler to run, is
// type-checked from source instead by go/packages itself, so it fails the
// load only when a module package uses something the build left out.
const mode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
	packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
	packages.NeedModule

// Func has the signature of packages.Load, injectable so tests can count
// loads.
type Func func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error)

// Module is one module loaded with type information. It is built once per
// module root and shared, read-only, by every metric for every package in
// the module.
type Module struct {
	// Fset is the file set every syntax tree in the load was parsed into.
	Fset *token.FileSet
	// Path is the module path declared in the root go.mod, or StdModulePath
	// or StdAllModulePath for the standard-library loads.
	Path string
	// Paths holds the import paths of the module's non-test packages, sorted.
	Paths []string
	// Pkgs maps the import path of each non-test module package to the
	// package, loaded from its non-test files only.
	Pkgs map[string]*packages.Package
	// Tests maps the import path of each module package that has in-package
	// _test.go files to its test variant: the same package type-checked with
	// its non-test and in-package test files together.
	Tests map[string]*packages.Package
	// XTests maps the import path of each module package that has an
	// external test package (package foo_test in the same directory) to that
	// external test package.
	XTests map[string]*packages.Package
	// Skipped lists the module directories holding Go files that the load
	// returned no package for, sorted by directory: build constraints
	// exclude all their files, so "./..." dropped them without a word.
	Skipped []SkippedDir
	// Warnings lists the module packages that loaded with an embed pattern
	// matching no file, sorted by package then pattern, one per pair (see
	// EmbedWarning). The package is extracted normally.
	Warnings []EmbedWarning
	// SkippedPackages lists the module packages that fail to load, sorted
	// by import path (see SkippedPackage). They are not in Paths, Pkgs,
	// Tests or XTests; GraphPackages still holds their imports.
	SkippedPackages []SkippedPackage

	// sources maps the import path of each non-test module package whose
	// Syntax is not its source files, a cgo package, to its source files
	// parsed into Fset, one per GoFiles entry. See SourceSyntax.
	sources map[string][]*ast.File
	// testOnly holds the import paths of the module packages whose non-test
	// files build constraints exclude entirely but whose test files the
	// load kept: go list lists them in "./..." for their tests alone. index
	// drops them and findSkipped names them test-only.
	testOnly map[string]bool
	// skipped maps the import path of each skipped package to the packages
	// go list returned for it (see GraphPackages).
	skipped map[string]skippedLoad
}

// Load loads every package under cfg.Dir, which must be an absolute module
// root, including test variants, and indexes the module's packages. It sets
// the mode, tests flag and file set on cfg. A load that matched no package
// of the module and reported no error returns a Module with no Paths and a
// nil error, so the caller names the empty module in its own terms. A
// package that fails to load is skipped (Module.SkippedPackages) and the
// rest are loaded; when every package fails, Load returns the first
// failure.
func Load(cfg *packages.Config, load Func) (*Module, error) {
	root := cfg.Dir
	modPath, err := ReadModulePath(root)
	if err != nil {
		return nil, err
	}
	cfg.Mode = mode
	cfg.Tests = true
	cfg.Fset = token.NewFileSet()
	roots, err := load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", root, cacheCause(err))
	}
	m, err := index(modPath, roots)
	if err != nil {
		return nil, err
	}
	if len(m.Paths) == 0 {
		// A module none of whose packages loads has nothing to measure; its
		// first failure is the error.
		if len(m.SkippedPackages) > 0 {
			return nil, m.SkippedPackages[0].Err
		}
		// go list reports some failures, among them a build cache it cannot
		// write to, on a pseudo-package named after the pattern instead of
		// exiting non-zero; without this they would read as an empty module.
		for _, p := range roots {
			if len(p.Errors) > 0 {
				return nil, fmt.Errorf("loading %s: %w", root, cacheCause(firstError(p.Errors)))
			}
		}
		return m, nil
	}
	m.Fset = cfg.Fset
	if err := m.parseSources(); err != nil {
		return nil, fmt.Errorf("loading %s: %w", root, err)
	}
	if m.Skipped, err = findSkipped(root, modPath, m.listed, m.testOnly); err != nil {
		return nil, fmt.Errorf("loading %s: %w", root, err)
	}
	return m, nil
}

// index sorts the packages returned by packages.Load into a Module's maps,
// keeping only packages inside modulePath and dropping generated test mains.
// It also drops a package whose non-test files build constraints exclude
// entirely, with its test variants: go list keeps such a package in "./..."
// only for its test files, and it fails to build, while the same package
// without tests drops out of "./..." silently. findSkipped reports both,
// and index records the first kind in testOnly so the report can say so.
// A module package that fails to load, in its non-test files or a test
// variant, is left out with its test variants and listed in
// SkippedPackages (classifyFailures); only a build cache go list could not
// use fails the whole index.
func index(modulePath string, roots []*packages.Package) (*Module, error) {
	excluded := make(map[string]bool)
	for _, p := range roots {
		if under, _ := forTest(p); under == "" && len(p.GoFiles) == 0 && len(p.IgnoredFiles) > 0 {
			excluded[p.PkgPath] = true
		}
	}
	mod := make([]*packages.Package, 0, len(roots))
	var testOnly map[string]bool
	for _, p := range roots {
		// A test variant belongs to the package it tests; the external test
		// package of the module root package is "<module>_test".
		under, _ := forTest(p)
		variant := under != ""
		if !variant {
			under = p.PkgPath
		}
		if excluded[under] || excluded[strings.TrimSuffix(under, ".test")] {
			if variant {
				if testOnly == nil {
					testOnly = make(map[string]bool)
				}
				testOnly[under] = true
			}
			continue
		}
		if InModule(modulePath, under) {
			mod = append(mod, p)
		}
	}
	slices.SortFunc(mod, byID)
	tested := make(map[string]bool)
	for _, p := range mod {
		if under, _ := forTest(p); under != "" {
			tested[under] = true
		}
	}
	testMain := func(path string) bool {
		under, ok := strings.CutSuffix(path, ".test")
		return ok && tested[under]
	}
	failed, warnings, err := classifyFailures(mod, nil, testMain)
	if err != nil {
		return nil, err
	}
	m := newModule(modulePath, len(mod))
	m.testOnly = testOnly
	m.Warnings = sortWarnings(warnings)
	m.SkippedPackages = sortSkipped(failed)
	drop := func(path string) bool {
		_, ok := failed[path]
		return ok
	}
	m.fill(mod, drop, testMain)
	m.keepSkipped(mod, failed, testMain)
	return m, nil
}

// keepSkipped records, for each package of failed, the packages of mod
// that belong to it, so GraphPackages can still read their imports.
func (m *Module) keepSkipped(mod []*packages.Package, failed map[string]SkippedPackage, testMain func(string) bool) {
	if len(failed) == 0 {
		return
	}
	m.skipped = make(map[string]skippedLoad, len(failed))
	for _, p := range mod {
		under, _ := forTest(p)
		variant := under != ""
		if !variant {
			if testMain(p.PkgPath) {
				continue
			}
			under = p.PkgPath
		}
		if _, ok := failed[under]; !ok {
			continue
		}
		s := m.skipped[under]
		if variant {
			s.tests = append(s.tests, p)
		} else {
			s.pkg = p
		}
		m.skipped[under] = s
	}
}

// newModule returns an empty Module for modulePath with room for n
// packages.
func newModule(modulePath string, n int) *Module {
	return &Module{
		Path:   modulePath,
		Pkgs:   make(map[string]*packages.Package, n),
		Tests:  make(map[string]*packages.Package),
		XTests: make(map[string]*packages.Package),
	}
}

// fill sorts pkgs, in package ID order, into m's maps: each test variant
// under the package it tests, and every other package as a non-test package
// unless testMain reports its import path as a generated test main. drop
// reports whether the package with the given import path is left out, with
// its test variants. Paths is sorted afterwards.
func (m *Module) fill(pkgs []*packages.Package, drop, testMain func(path string) bool) {
	plain := make([]*packages.Package, 0, len(pkgs))
	for _, p := range pkgs {
		switch under, external := forTest(p); {
		case under == "":
			if !drop(p.PkgPath) {
				plain = append(plain, p)
			}
		case drop(under):
			// A test variant of a dropped package is dropped with it.
		case external:
			m.XTests[under] = p
		default:
			m.Tests[under] = p
		}
	}
	for _, p := range plain {
		if testMain(p.PkgPath) {
			continue
		}
		m.Pkgs[p.PkgPath] = p
		m.Paths = append(m.Paths, p.PkgPath)
	}
	slices.Sort(m.Paths)
}

// listed reports whether the load returned the module package at path,
// loaded or skipped because it fails to load, so findSkipped reports
// neither as a directory the load left out.
func (m *Module) listed(path string) bool {
	if _, ok := m.Pkgs[path]; ok {
		return true
	}
	return slices.ContainsFunc(m.SkippedPackages, func(s SkippedPackage) bool { return s.Package == path })
}

// byID orders packages by ID.
func byID(a, b *packages.Package) int { return strings.Compare(a.ID, b.ID) }

// firstError returns the first of errs, which must not be empty, that did
// not come from go list, or errs[0] when all did. Loading without NeedDeps
// makes go list compile the module for export data, so a package that fails
// to type-check also carries the compiler's output as a list error ahead of
// the type checker's own, more precise, error.
func firstError(errs []packages.Error) packages.Error {
	for _, e := range errs {
		if e.Kind != packages.ListError {
			return e
		}
	}
	return errs[0]
}

// forTest returns the import path of the package p is a test variant of, and
// whether p is its external test package. It returns "" when p is not a test
// variant. go/packages gives test variants the ID "path [forTest.test]".
func forTest(p *packages.Package) (under string, external bool) {
	i := strings.IndexByte(p.ID, ' ')
	if i < 0 {
		return "", false
	}
	under = strings.TrimSuffix(strings.TrimPrefix(p.ID[i+1:], "["), ".test]")
	return under, p.PkgPath != under
}

// TestPackages returns the in-package test variant and the external test
// package of p, in that order, omitting whichever does not exist.
func (m *Module) TestPackages(p *packages.Package) []*packages.Package {
	out := make([]*packages.Package, 0, 2)
	if tp, ok := m.Tests[p.PkgPath]; ok {
		out = append(out, tp)
	}
	if xp, ok := m.XTests[p.PkgPath]; ok {
		out = append(out, xp)
	}
	return out
}

// InModule reports whether importPath belongs to the module modulePath.
func InModule(modulePath, importPath string) bool {
	rest, ok := strings.CutPrefix(importPath, modulePath)
	return ok && (rest == "" || rest[0] == '/')
}
