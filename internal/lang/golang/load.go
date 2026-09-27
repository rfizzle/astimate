package golang

import (
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

// ErrNoPackages is returned when a module root contains no Go packages.
var ErrNoPackages = errors.New("no Go packages in module")

// loadMode is the go/packages mode every module is loaded with: names, files,
// syntax and full type information for the module packages and their
// dependencies, plus module metadata.
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
	packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
	packages.NeedDeps | packages.NeedModule

// loadFunc has the signature of packages.Load, injectable so tests can count
// loads.
type loadFunc func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error)

// loaded is one module loaded with type information. It is built once per
// module root and shared, read-only, by every metric for every package in
// the module.
type loaded struct {
	// fset is the file set every syntax tree in the load was parsed into.
	fset *token.FileSet
	// modulePath is the module path declared in the root go.mod.
	modulePath string
	// paths holds the import paths of the module's non-test packages, sorted.
	paths []string
	// pkgs maps the import path of each non-test module package to the
	// package, loaded from its non-test files only.
	pkgs map[string]*packages.Package
	// tests maps the import path of each module package that has in-package
	// _test.go files to its test variant: the same package type-checked with
	// its non-test and in-package test files together.
	tests map[string]*packages.Package
	// xtests maps the import path of each module package that has an
	// external test package (package foo_test in the same directory) to that
	// external test package.
	xtests map[string]*packages.Package
	// reverse maps an import path to the sorted import paths of the module
	// packages that import it. It is nil until the fan-in metrics (fan_in,
	// fan_in_tests) build it once per load.
	reverse map[string][]string
	// fanIn guards the one-time build of reverse and holds the test-only
	// importer graph behind fan_in_tests.
	fanIn reverseGraph

	// detailsMu guards details, which maps an import path to the debug
	// details of its most recent Extract. Besides the fan-in graphs it is
	// the only state on loaded written after the load.
	detailsMu sync.Mutex
	details   map[string]details
}

// loadModule loads every package under cfg.Dir, which must be an absolute
// module root, including test variants, and indexes the module's packages.
// It sets the mode, tests flag and file set on cfg.
func loadModule(cfg *packages.Config, load loadFunc) (*loaded, error) {
	root := cfg.Dir
	modPath, err := readModulePath(root)
	if err != nil {
		return nil, err
	}
	cfg.Mode = loadMode
	cfg.Tests = true
	cfg.Fset = token.NewFileSet()
	roots, err := load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", root, err)
	}
	l, err := index(modPath, roots)
	if err != nil {
		return nil, err
	}
	if len(l.paths) == 0 {
		return nil, fmt.Errorf("loading %s: %w", root, ErrNoPackages)
	}
	l.fset = cfg.Fset
	return l, nil
}

// readModulePath returns the module path declared in root/go.mod.
func readModulePath(root string) (string, error) {
	name := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("reading module path: %w", err)
	}
	p := modfile.ModulePath(data)
	if p == "" {
		return "", fmt.Errorf("reading module path: no module directive in %s", name)
	}
	return p, nil
}

// index sorts the packages returned by packages.Load into loaded's maps,
// keeping only packages inside modulePath and dropping generated test mains.
// It returns the first error reported on a module package, in package ID
// order, so the failing package is named deterministically.
func index(modulePath string, roots []*packages.Package) (*loaded, error) {
	mod := make([]*packages.Package, 0, len(roots))
	for _, p := range roots {
		// A test variant belongs to the package it tests; the external test
		// package of the module root package is "<module>_test".
		under, _ := forTest(p)
		if under == "" {
			under = p.PkgPath
		}
		if inModule(modulePath, under) {
			mod = append(mod, p)
		}
	}
	slices.SortFunc(mod, func(a, b *packages.Package) int { return strings.Compare(a.ID, b.ID) })
	for _, p := range mod {
		if len(p.Errors) > 0 {
			return nil, fmt.Errorf("loading %s: %w", p.PkgPath, p.Errors[0])
		}
	}

	l := &loaded{
		modulePath: modulePath,
		pkgs:       make(map[string]*packages.Package, len(mod)),
		tests:      make(map[string]*packages.Package),
		xtests:     make(map[string]*packages.Package),
	}
	plain := make([]*packages.Package, 0, len(mod))
	for _, p := range mod {
		switch under, external := forTest(p); {
		case under == "":
			plain = append(plain, p)
		case external:
			l.xtests[under] = p
		default:
			l.tests[under] = p
		}
	}
	for _, p := range plain {
		// The generated test main of a package with tests is "path.test".
		if under, ok := strings.CutSuffix(p.PkgPath, ".test"); ok && l.hasTests(under) {
			continue
		}
		l.pkgs[p.PkgPath] = p
		l.paths = append(l.paths, p.PkgPath)
	}
	slices.Sort(l.paths)
	return l, nil
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

// hasTests reports whether the package at importPath has an internal or
// external test package.
func (l *loaded) hasTests(importPath string) bool {
	_, internal := l.tests[importPath]
	_, external := l.xtests[importPath]
	return internal || external
}

// inModule reports whether importPath belongs to the module modulePath.
func inModule(modulePath, importPath string) bool {
	rest, ok := strings.CutPrefix(importPath, modulePath)
	return ok && (rest == "" || rest[0] == '/')
}
