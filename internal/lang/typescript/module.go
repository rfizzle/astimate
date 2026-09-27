package typescript

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/rfizzle/astimate/internal/metrics"
)

// manifestName is the file that marks a module root.
const manifestName = "package.json"

// testsDir is the directory name whose contents are test files of the
// package that contains it.
const testsDir = "__tests__"

// module is the parse of one module root: every source and test file's
// facts, grouped by package, and the module-wide import graph. It is built
// once by loadModule and read-only afterwards, apart from the details map.
type module struct {
	root string
	// pkgIDs lists the package identifiers, sorted.
	pkgIDs []string
	pkgs   map[string]*pkg

	detailsMu sync.Mutex
	details   map[string]details
}

// pkg is one package: a directory of the module with at least one
// non-test source file.
type pkg struct {
	id string
	// dir is the absolute directory of the package.
	dir string
	// src and tests are the package's non-test and test files, sorted by
	// path.
	src, tests []*fileFacts
	// internal and external and stdlib are the distinct imports of the
	// non-test files by class: internal package identifiers, external
	// package names and Node built-in names.
	internal, external, stdlib map[string]bool
	// reexports counts the names re-exported from other packages.
	reexports int
	// fanIn and fanInTests are the packages importing this one from
	// non-test files, and those importing it from test files only.
	fanIn, fanInTests map[string]bool
}

// details is the per-package record Details serves: the names behind
// untested_exports, those the untested directive left out, and the
// duplicate block locations; the declarations of the untested names, index
// for index, and of the globals, in the order the count reads them; the
// largest file; and the source files, sorted. Files are relative to the
// package directory in slash form.
type details struct {
	untested, excluded []string
	dupLocations       []string
	untestedPos        []metrics.Position
	globalPos          []metrics.Position
	largestFile        string
	sourceFiles        []string
}

// loadOptions carries the extractor settings a load needs.
type loadOptions struct {
	// o200k makes the load count each file's o200k_base tokens.
	o200k bool
	// logger receives one info record per file with syntax errors; nil
	// discards them.
	logger *slog.Logger
}

// sourceFile is one TypeScript source or test file found under a module
// root.
type sourceFile struct {
	// abs is the absolute path; rel the slash path relative to the root.
	abs, rel string
	// pkg is the identifier of the package the file belongs to.
	pkg  string
	test bool
}

// loadModule finds and parses every TypeScript file of the module at the
// absolute path root, then groups the files into packages and resolves the
// import graph. ctx is checked before each file.
func loadModule(ctx context.Context, root string, opts loadOptions) (*module, error) {
	files, err := findFiles(root)
	if err != nil {
		return nil, fmt.Errorf("listing TypeScript files of %s: %w", root, err)
	}
	m := &module{root: root, pkgs: make(map[string]*pkg)}
	for _, f := range files {
		if f.test {
			continue
		}
		if _, ok := m.pkgs[f.pkg]; !ok {
			m.pkgs[f.pkg] = &pkg{
				id:  f.pkg,
				dir: filepath.Join(root, filepath.FromSlash(f.pkg)),
			}
			m.pkgIDs = append(m.pkgIDs, f.pkg)
		}
	}
	slices.Sort(m.pkgIDs)

	cfg, err := readTSConfig(root)
	if err != nil {
		return nil, err
	}
	res := newResolver(root, m.pkgs, cfg)
	sc := newScanner(opts)
	for _, f := range files {
		p, ok := m.pkgs[f.pkg]
		if !ok {
			// A test file in a directory with no source files belongs to
			// no package.
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("loading %s: %w", root, err)
		}
		ff, err := sc.scanFile(f)
		if err != nil {
			return nil, err
		}
		if f.test {
			p.tests = append(p.tests, ff)
		} else {
			p.src = append(p.src, ff)
		}
	}
	m.link(res)
	return m, nil
}

// findFiles returns the .ts, .tsx, .mts and .cts files under root that
// belong to the module, sorted by relative path: declaration files (.d.ts,
// .d.mts, .d.cts) are left out, and so are directories named node_modules,
// dist or build, directories whose name starts with a dot, and directories
// below root holding their own package.json, which are modules of their
// own.
func findFiles(root string) ([]sourceFile, error) {
	var out []sourceFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			if skipDir(d.Name()) || isFile(filepath.Join(p, manifestName)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !isSourceName(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		out = append(out, sourceFile{abs: p, rel: rel, pkg: packageOf(rel), test: isTestPath(rel)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b sourceFile) int { return strings.Compare(a.rel, b.rel) })
	return out, nil
}

// skipDir reports whether a directory named name is outside every package.
func skipDir(name string) bool {
	switch name {
	case "node_modules", "dist", "build":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// isFile reports whether p is a regular file.
func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// isSourceName reports whether a file named name is TypeScript source: it
// ends in .ts, .tsx, .mts or .cts and is not a declaration file (.d.ts,
// .d.mts or .d.cts).
func isSourceName(name string) bool {
	for _, ext := range []string{".d.ts", ".d.mts", ".d.cts"} {
		if strings.HasSuffix(name, ext) {
			return false
		}
	}
	for _, ext := range []string{".ts", ".tsx", ".mts", ".cts"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// isTestPath reports whether the file at the slash path rel is a test file:
// its name ends in .test or .spec followed by .ts, .tsx, .mts or .cts, or it
// lies under a __tests__ directory.
func isTestPath(rel string) bool {
	name := path.Base(rel)
	for _, suffix := range []string{
		".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx",
		".test.mts", ".spec.mts", ".test.cts", ".spec.cts",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return slices.Contains(strings.Split(path.Dir(rel), "/"), testsDir)
}

// packageOf returns the identifier of the package the file at the slash
// path rel belongs to: its directory, cut before the first __tests__
// segment, or "." at the root.
func packageOf(rel string) string {
	return packageDir(path.Dir(rel))
}

// packageDir maps the slash directory dir, relative to the module root, to
// the package directory it belongs to: dir cut before its first __tests__
// segment, "." for the root.
func packageDir(dir string) string {
	segs := strings.Split(dir, "/")
	if i := slices.Index(segs, testsDir); i >= 0 {
		segs = segs[:i]
	}
	if len(segs) == 0 {
		return "."
	}
	return path.Clean(strings.Join(segs, "/"))
}

// languageFor returns the grammar for the file at rel: TSX for .tsx files,
// TypeScript otherwise, including .mts and .cts.
func languageFor(rel string) *sitter.Language {
	if strings.HasSuffix(rel, ".tsx") {
		return grammars.TsxLanguage()
	}
	return grammars.TypescriptLanguage()
}

// link resolves every file's imports and re-exports into its package's
// import sets and the reverse edges fan_in counts. Imports resolving to the
// importing package itself are intra-package and not counted.
func (m *module) link(res *resolver) {
	for _, id := range m.pkgIDs {
		p := m.pkgs[id]
		p.internal, p.external, p.stdlib = map[string]bool{}, map[string]bool{}, map[string]bool{}
		p.fanIn, p.fanInTests = map[string]bool{}, map[string]bool{}
	}
	testOnly := make(map[string]map[string]bool, len(m.pkgIDs))
	for _, id := range m.pkgIDs {
		p := m.pkgs[id]
		for _, f := range p.src {
			for _, spec := range f.imports {
				imp := res.classify(f.dir, spec)
				switch imp.kind {
				case importInternal:
					if imp.name != id {
						p.internal[imp.name] = true
					}
				case importExternal:
					p.external[imp.name] = true
				case importStdlib:
					p.stdlib[imp.name] = true
				}
			}
			for _, re := range f.reexports {
				if imp := res.classify(f.dir, re.spec); imp.kind != importInternal || imp.name != id {
					p.reexports += re.names
				}
			}
		}
		for _, f := range p.tests {
			for _, spec := range f.imports {
				if imp := res.classify(f.dir, spec); imp.kind == importInternal && imp.name != id {
					if testOnly[imp.name] == nil {
						testOnly[imp.name] = map[string]bool{}
					}
					testOnly[imp.name][id] = true
				}
			}
		}
	}
	for _, id := range m.pkgIDs {
		for target := range m.pkgs[id].internal {
			m.pkgs[target].fanIn[id] = true
		}
	}
	for target, importers := range testOnly {
		t := m.pkgs[target]
		for id := range importers {
			if !t.fanIn[id] {
				t.fanInTests[id] = true
			}
		}
	}
}

// setDetails records d as the most recent details of package id.
func (m *module) setDetails(id string, d details) {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	if m.details == nil {
		m.details = make(map[string]details)
	}
	m.details[id] = d
}

// detailsOf returns the most recent details recorded for package id, and
// whether any were.
func (m *module) detailsOf(id string) (details, bool) {
	m.detailsMu.Lock()
	defer m.detailsMu.Unlock()
	d, ok := m.details[id]
	return d, ok
}
