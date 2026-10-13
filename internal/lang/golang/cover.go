package golang

import (
	"context"
	"go/token"
	"maps"
	"path/filepath"
	"slices"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/cover"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// Coverage measures coverage_pct and untested_exports_covered for the
// packages pkgs, import paths of the module at mod.Root
// (metrics.CoverageMeasurer), with one `go test -cover -coverpkg=./...
// -coverprofile -count=1 -run .` run per package in the module root
// (cover.Measure). coverage_pct counts a package's own statements run by
// its own tests, so only the packages of pkgs with test files run for it.
// For a package of pkgs with untested exports, the tests of every module
// package whose test binary links it run as well, a directory holding only
// test files included, so the exports a test elsewhere in the module
// reaches count; their own coverage is not reported.
// untested_exports_covered is untested_exports less the counted exports
// with a statement of their body that any passing run executed, so an
// export a test reaches through an HTTP handler, a pipeline or an
// interface counts as tested; it is nil for a package no passing run
// linked. The covered exports are recorded with the package's details
// (Details' UntestedCovered). The environment, GOFLAGS and CGO_ENABLED
// included, is passed through unchanged. When ctx has a deadline, go
// test's -timeout is set to the time left, so a hung test binary exits
// with it. A package without test files or statements gets a nil Pct and
// no Reason; one whose tests fail to build or run a nil Pct and a Reason.
// When ctx ends a run, its package gets a Reason saying so. The error
// reports a go command that could not start, or a module or package that
// cannot be loaded or extracted.
func (e *Extractor) Coverage(ctx context.Context, mod *metrics.ModuleContext, pkgs []string) (map[string]metrics.Coverage, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return nil, err
	}
	own := make([]string, 0, len(pkgs))
	settle := make(map[string]bool, len(pkgs))
	for _, pkg := range pkgs {
		if hasTests(l.Module, pkg) {
			own = append(own, pkg)
		}
		d, _, _, err := e.recorded(ctx, mod, pkg)
		if err != nil {
			return nil, err
		}
		if d.untested.Untested > 0 {
			settle[pkg] = true
		}
	}
	also := testersOf(l.Module, settle, own)
	res, profile, err := cover.Measure(ctx, mod.Root, own, also)
	if err != nil {
		return nil, err
	}
	for _, pkg := range pkgs {
		c := res[pkg]
		if profile.Has(pkg) {
			d, l, _, err := e.recorded(ctx, mod, pkg)
			if err != nil {
				return nil, err
			}
			covered := coveredExports(l.Fset, pkg, d.untested.Names, d.untested.Body, profile)
			l.setCovered(pkg, covered)
			n := d.untested.Untested - len(covered)
			c.UntestedExports = &n
		}
		res[pkg] = c
	}
	return res, nil
}

// hasTests reports whether the module package at path has test files: an
// in-package test variant or an external test package.
func hasTests(m *load.Module, path string) bool {
	return m.Tests[path] != nil || m.XTests[path] != nil
}

// testersOf returns, sorted, the module packages with test files, other
// than those of skip, whose test binary links a package of targets: one of
// their test variants imports it, directly or through other packages of
// the module. A directory with only test files is such a package when the
// load returned its test variant. It returns nil when targets is empty.
func testersOf(m *load.Module, targets map[string]bool, skip []string) []string {
	if len(targets) == 0 {
		return nil
	}
	links := make(map[string]bool)
	var reaches func(p *packages.Package) bool
	reaches = func(p *packages.Package) bool {
		for path := range p.Imports {
			if targets[path] {
				return true
			}
			done, seen := links[path]
			if !seen {
				links[path] = false
				if dep := m.Pkgs[path]; dep != nil {
					done = reaches(dep)
				}
				links[path] = done
			}
			if done {
				return true
			}
		}
		return false
	}
	var out []string
	for _, path := range slices.Sorted(maps.Keys(withTests(m))) {
		if slices.Contains(skip, path) {
			continue
		}
		for _, tp := range []*packages.Package{m.Tests[path], m.XTests[path]} {
			if tp != nil && reaches(tp) {
				out = append(out, path)
				break
			}
		}
	}
	return out
}

// withTests returns the import paths of the module packages with test
// files as a set.
func withTests(m *load.Module) map[string]bool {
	out := make(map[string]bool, len(m.Tests)+len(m.XTests))
	for path := range m.Tests {
		out[path] = true
	}
	for path := range m.XTests {
		out[path] = true
	}
	return out
}

// coveredExports returns the names of names, the untested exports of the
// package with import path pkg, whose body, bodies[i], has a statement
// that ran in profile, in the order of names. A body's braces are resolved
// in fset, honoring //line directives as the cover tool does, and its file
// is named as the profile names it, pkg joined with the file's base name.
func coveredExports(fset *token.FileSet, pkg string, names []string, bodies [][2]token.Pos, profile cover.Profile) []string {
	var covered []string
	for i, name := range names {
		if i >= len(bodies) || !bodies[i][0].IsValid() || !bodies[i][1].IsValid() {
			continue
		}
		from, to := fset.Position(bodies[i][0]), fset.Position(bodies[i][1])
		file := pkg + "/" + filepath.Base(from.Filename)
		if profile.Ran(file, cover.Pos{Line: from.Line, Col: from.Column}, cover.Pos{Line: to.Line, Col: to.Column}) {
			covered = append(covered, name)
		}
	}
	return covered
}
