package load

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/exclude"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/filter"
	"golang.org/x/tools/go/packages"
)

// Exclude drops from m every Go file of its module packages, test files
// included, that list matches by its slash path relative to root, the
// absolute module root (package filter), and logs each to logger, when
// set, at debug level. A package all of whose non-test files are excluded
// leaves Paths, Pkgs, Tests and XTests, or SkippedPackages when it failed
// to load, and is listed in Skipped with the reason exclude.Reason gives
// the first pattern, in list order, that matched one of them. A test
// variant left with no test file is dropped. It runs once per load,
// before m is shared; a nil list does nothing.
func (m *Module) Exclude(root string, list *exclude.List, logger *slog.Logger) {
	if list == nil {
		return
	}
	f := filter.New(root, list, m.Fset)
	kept := make([]string, 0, len(m.Paths))
	for _, path := range m.Paths {
		p, gone := f.Package(m.Pkgs[path])
		if gone != "" {
			for _, tp := range m.TestPackages(m.Pkgs[path]) {
				f.Package(tp) // only to record the files
			}
			delete(m.Pkgs, path)
			delete(m.Tests, path)
			delete(m.XTests, path)
			delete(m.sources, path)
			m.addExcluded(path, gone)
			continue
		}
		m.Pkgs[path] = p
		if src, ok := m.sources[path]; ok {
			m.sources[path] = f.Trees(src, false)
		}
		for _, tests := range []map[string]*packages.Package{m.Tests, m.XTests} {
			if tp, ok := tests[path]; ok {
				if tp = testsLeft(f, tp); tp == nil {
					delete(tests, path)
				} else {
					tests[path] = tp
				}
			}
		}
		kept = append(kept, path)
	}
	m.Paths = kept
	m.excludeSkipped(f)
	slices.SortFunc(m.Skipped, func(a, b SkippedDir) int { return strings.Compare(a.Dir, b.Dir) })
	if logger != nil {
		for _, file := range f.Files() {
			logger.Debug("excluded file", "file", file.Rel, "pattern", file.Pattern)
		}
	}
}

// testsLeft returns tp, a test variant, without its excluded files, or nil
// when no test file is left.
func testsLeft(f *filter.Filter, tp *packages.Package) *packages.Package {
	tp, _ = f.Package(tp)
	if !slices.ContainsFunc(tp.GoFiles, filter.IsTest) {
		return nil
	}
	return tp
}

// addExcluded lists the package at importPath in Skipped as excluded by
// pattern.
func (m *Module) addExcluded(importPath, pattern string) {
	dir := strings.TrimPrefix(strings.TrimPrefix(importPath, m.Path), "/")
	if dir == "" {
		dir = "."
	}
	m.Skipped = append(m.Skipped, SkippedDir{Dir: dir, ImportPath: importPath, Reason: exclude.Reason(pattern)})
}

// excludeSkipped drops the excluded files of every package skipped because
// it fails to load, and moves one all of whose non-test files are excluded
// to Skipped.
func (m *Module) excludeSkipped(f *filter.Filter) {
	kept := m.SkippedPackages[:0]
	for _, sp := range m.SkippedPackages {
		s, ok := m.skipped[sp.Package]
		if !ok {
			kept = append(kept, sp)
			continue
		}
		var gone string
		if s.pkg != nil {
			s.pkg, gone = f.Package(s.pkg)
		}
		if gone != "" {
			delete(m.skipped, sp.Package)
			m.addExcluded(sp.Package, gone)
			continue
		}
		tests := s.tests[:0:0]
		for _, tp := range s.tests {
			if tp = testsLeft(f, tp); tp != nil {
				tests = append(tests, tp)
			}
		}
		s.tests = tests
		m.skipped[sp.Package] = s
		kept = append(kept, sp)
	}
	m.SkippedPackages = kept
}
