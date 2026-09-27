package golang

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
	packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
	packages.NeedModule

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
	// sources maps the import path of each non-test module package whose
	// Syntax is not its source files, a cgo package, to its source files
	// parsed into fset, one per GoFiles entry. See sourceSyntax.
	sources map[string][]*ast.File
	// skipped lists the module directories holding Go files that the load
	// returned no package for, sorted by directory: build constraints
	// exclude all their files, so "./..." dropped them without a word.
	skipped []skippedDir
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
		return nil, fmt.Errorf("loading %s: %w", root, cacheCause(err))
	}
	l, err := index(modPath, roots)
	if err != nil {
		return nil, err
	}
	if len(l.paths) == 0 {
		// go list reports some failures, among them a build cache it cannot
		// write to, on a pseudo-package named after the pattern instead of
		// exiting non-zero; without this they would read as an empty module.
		for _, p := range roots {
			if len(p.Errors) > 0 {
				return nil, fmt.Errorf("loading %s: %w", root, cacheCause(firstError(p.Errors)))
			}
		}
		return nil, fmt.Errorf("loading %s: %w", root, ErrNoPackages)
	}
	l.fset = cfg.Fset
	if err := parseSources(l); err != nil {
		return nil, fmt.Errorf("loading %s: %w", root, err)
	}
	if l.skipped, err = findSkipped(root, modPath, l.pkgs); err != nil {
		return nil, fmt.Errorf("loading %s: %w", root, err)
	}
	return l, nil
}

// usesDisabledCgo completes a message about a package that build constraints
// left without Go files because they import "C".
const usesDisabledCgo = "uses cgo, which is disabled (CGO_ENABLED=0, the default when no C compiler is on PATH)"

// skippedDir is a module directory with Go files that a load returned no
// package for.
type skippedDir struct {
	// dir is the directory relative to the module root, in slash form.
	dir string
	// importPath is the import path the directory's package would have.
	importPath string
	// reason says why the load left it out.
	reason string
}

// findSkipped walks the module at root, whose module path is modulePath, for
// directories holding Go files that have no package in pkgs, and returns
// them sorted by directory. It skips what "./..." skips: testdata and vendor
// directories, directories whose names start with "_" or ".", and nested
// modules; it ignores files whose names start with "_" or ".", as the go
// command does. go list drops a directory from "./..." without a word when
// build constraints exclude all of its Go files, as they do a package made
// only of cgo files when cgo is disabled.
func findSkipped(root, modulePath string, pkgs map[string]*packages.Package) ([]skippedDir, error) {
	var skipped []skippedDir
	var files []string // Go files of the directory being walked
	dir := ""          // slash path of that directory
	flush := func() {
		if len(files) == 0 {
			return
		}
		path := modulePath
		if dir != "." {
			path += "/" + dir
		}
		if _, ok := pkgs[path]; !ok {
			skipped = append(skipped, skippedDir{dir: dir, importPath: path, reason: skipReason(files)})
		}
		files = files[:0]
	}
	err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := d.Name()
		if !d.IsDir() {
			if strings.HasSuffix(base, ".go") && !ignoredName(base) && d.Type().IsRegular() {
				files = append(files, name)
			}
			return nil
		}
		if name != root {
			if base == "testdata" || base == "vendor" || ignoredName(base) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(name, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		flush()
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		dir = filepath.ToSlash(rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("finding skipped packages: %w", err)
	}
	flush()
	slices.SortFunc(skipped, func(a, b skippedDir) int { return strings.Compare(a.dir, b.dir) })
	return skipped, nil
}

// ignoredName reports whether the go command ignores a file or directory
// named base: its name starts with "_" or ".".
func ignoredName(base string) bool {
	return strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".")
}

// skipReason says why the directory holding the Go files named by files was
// left out of a load: build constraints exclude them all, and, when one
// imports "C", that cgo is the likely cause. It parses only the import
// clauses.
func skipReason(files []string) string {
	const reason = "build constraints exclude all Go files"
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, spec := range f.Imports {
			if spec.Path.Value == `"C"` {
				return reason + "; it " + usesDisabledCgo
			}
		}
	}
	return reason
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
// It also drops a package whose non-test files build constraints exclude
// entirely, with its test variants: go list keeps such a package in "./..."
// only for its test files, and it fails to build, while the same package
// without tests drops out of "./..." silently. findSkipped reports both.
// It returns the first error reported on a module package, in package ID
// order, so the failing package is named deterministically.
func index(modulePath string, roots []*packages.Package) (*loaded, error) {
	excluded := make(map[string]bool)
	for _, p := range roots {
		if under, _ := forTest(p); under == "" && len(p.GoFiles) == 0 && len(p.IgnoredFiles) > 0 {
			excluded[p.PkgPath] = true
		}
	}
	mod := make([]*packages.Package, 0, len(roots))
	for _, p := range roots {
		// A test variant belongs to the package it tests; the external test
		// package of the module root package is "<module>_test".
		under, _ := forTest(p)
		if under == "" {
			under = p.PkgPath
		}
		if excluded[under] || excluded[strings.TrimSuffix(under, ".test")] {
			continue
		}
		if inModule(modulePath, under) {
			mod = append(mod, p)
		}
	}
	slices.SortFunc(mod, func(a, b *packages.Package) int { return strings.Compare(a.ID, b.ID) })
	for _, p := range mod {
		if len(p.Errors) > 0 {
			return nil, packageError(p)
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

// packageError returns the load error of the module package p, which has
// errors. When the failure traces to a build cache go list could not use, it
// returns that go list error, naming the cause, even if a type error follows
// it. When it traces to cgo that could not run, it names that cause ahead of
// the error it produced, which on its own ("could not import C",
// "undefined: dep.F") does not.
func packageError(p *packages.Package) error {
	for _, e := range p.Errors {
		if e.Kind == packages.ListError && fromCache(e.Msg) {
			return fmt.Errorf("loading %s: %w", p.PkgPath, cacheCause(e))
		}
	}
	err := firstError(p.Errors)
	if cause := cgoCause(p); cause != "" {
		return fmt.Errorf("loading %s: %s: %w", p.PkgPath, cause, err)
	}
	return fmt.Errorf("loading %s: %w", p.PkgPath, err)
}

// cgoCause describes why p failed to load when the cause is cgo, and
// returns "" otherwise. It looks at p, then its direct imports in import
// path order: a package whose import "C" failed could not be preprocessed
// because cgo's C compiler did not run, and a dependency whose only files
// import "C" was excluded because cgo is disabled. A dependency whose export
// data cannot be built does not fail the load by itself, because go/packages
// then type-checks it from source; only a module package that uses what cgo
// left out fails, so only direct imports need a look.
func cgoCause(p *packages.Package) string {
	if importsCFailed(p) {
		return "cgo package " + p.PkgPath + " needs a C compiler (" + ccSetting() + ")"
	}
	imports := make([]string, 0, len(p.Imports))
	for path := range p.Imports {
		imports = append(imports, path)
	}
	slices.Sort(imports)
	for _, path := range imports {
		imp := p.Imports[path]
		switch {
		case importsCFailed(imp):
			return "cgo dependency " + path + " needs a C compiler (" + ccSetting() + ")"
		case cgoExcluded(imp):
			return "dependency " + path + " " + usesDisabledCgo
		}
	}
	return ""
}

// importsCFailed reports whether type-checking p failed to import "C", which
// happens when go list could not run cgo on p's files.
func importsCFailed(p *packages.Package) bool {
	return slices.ContainsFunc(p.Errors, func(e packages.Error) bool {
		return strings.Contains(e.Msg, "could not import C (")
	})
}

// cgoExcluded reports whether build constraints left p with no Go files and
// one of the files they excluded imports "C". It parses only the import
// clauses of the excluded files, and runs only on the error path.
func cgoExcluded(p *packages.Package) bool {
	if len(p.GoFiles) > 0 {
		return false
	}
	fset := token.NewFileSet()
	for _, name := range p.IgnoredFiles {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, spec := range f.Imports {
			if spec.Path.Value == `"C"` {
				return true
			}
		}
	}
	return false
}

// ccSetting describes the C compiler cgo runs: the CC environment variable,
// or the go command's default when it is unset.
func ccSetting() string {
	if cc := os.Getenv("CC"); cc != "" {
		return "CC=" + cc
	}
	return "CC unset, go env CC names the default"
}

// cacheCause wraps err, a failure of go list itself, with its cause when it
// came from the build cache, and returns err unchanged otherwise. Loading
// without NeedDeps runs go list -export, which writes export data to
// GOCACHE, so a read-only cache (or GOCACHE=off) fails every load.
func cacheCause(err error) error {
	if !fromCache(err.Error()) {
		return err
	}
	dir := goCacheDir()
	if dir == "" {
		dir = "unknown"
	}
	return fmt.Errorf("the Go build cache must be writable (GOCACHE=%s): %w", dir, err)
}

// fromCache reports whether msg, a go list error, is about the build cache:
// it mentions the build cache or a path inside it.
func fromCache(msg string) bool {
	if strings.Contains(msg, "build cache") {
		return true
	}
	dir := goCacheDir()
	return dir != "" && strings.Contains(msg, dir)
}

// goCacheDir returns the build cache directory: GOCACHE, or the go
// command's default under the user cache directory when it is unset. It
// returns "" when neither is known.
func goCacheDir() string {
	if dir := os.Getenv("GOCACHE"); dir != "" {
		return dir
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "go-build")
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
