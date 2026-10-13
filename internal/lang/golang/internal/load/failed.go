package load

import (
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// SkippedPackage is a module package left out of a load because it, or one
// of its test variants, fails to load: a module it imports is not
// available, a file does not parse or type-check, or cgo cannot build it.
// Every other package of the module loads and is measured as usual, so one
// broken package does not cost the rest of the module.
type SkippedPackage struct {
	// Package is the import path of the package.
	Package string
	// Reason says what kind of failure it was: "missing module", "syntax
	// error", "type error" or "build error", followed by " in test files"
	// when only a test variant failed.
	Reason string
	// Err is the package's first load error, as a load that failed on it
	// would have reported it.
	Err error
}

// skippedLoad holds the packages go list returned for one skipped module
// package: its non-test package, nil when go list returned none, and its
// test variants. The import graph reads their edges, so a skipped package
// still counts in its neighbours' fan_in (GraphPackages).
type skippedLoad struct {
	pkg   *packages.Package
	tests []*packages.Package
}

// GraphPackage is one module package whose imports are edges of the
// module's import graph, with its test variants.
type GraphPackage struct {
	// Pkg is the non-test package.
	Pkg *packages.Package
	// Tests are its in-package test variant and its external test
	// package, in that order, omitting whichever does not exist.
	Tests []*packages.Package
}

// GraphPackages returns every module package whose imports are edges of
// the import graph, keyed by import path: the loaded packages of Pkgs and
// the skipped packages go list returned a non-test package for. A skipped
// package is not measured, but its imports still count in the fan_in and
// fan_in_tests of the packages it imports, so skipping it leaves its
// neighbours' metrics unchanged.
func (m *Module) GraphPackages() map[string]GraphPackage {
	out := make(map[string]GraphPackage, len(m.Pkgs)+len(m.skipped))
	for path, p := range m.Pkgs {
		out[path] = GraphPackage{Pkg: p, Tests: m.TestPackages(p)}
	}
	for path, s := range m.skipped {
		if s.pkg != nil {
			out[path] = GraphPackage{Pkg: s.pkg, Tests: s.tests}
		}
	}
	return out
}

// classifyFailures returns the import paths of the packages of mod, sorted
// by ID, that have a load error other than an embed pattern matching no
// file (tolerateEmbeds), each with its first error, and appends the embed
// warnings of every package to warnings. A package fails when its non-test
// package or any test variant does; the error recorded is the first in ID
// order, so the non-test package's when it fails. It returns a non-nil
// error, failing the whole load, when a failure traces to a build cache go
// list could not use, since that fails every package alike. isTestMain
// reports a generated test main, which is never a package of its own.
func classifyFailures(mod []*packages.Package, warnings []EmbedWarning, isTestMain func(string) bool,
) (map[string]SkippedPackage, []EmbedWarning, error) {
	failed := make(map[string]SkippedPackage)
	for _, p := range mod {
		var fatal bool
		if warnings, fatal = tolerateEmbeds(warnings, p); !fatal {
			continue
		}
		under, variant := forTest(p)
		if under == "" {
			under = p.PkgPath
			if isTestMain(under) {
				continue
			}
		} else {
			variant = true
		}
		q := withoutEmbedErrors(p)
		if cacheFailure(q) {
			return nil, nil, packageError(q)
		}
		s, seen := failed[under]
		if !seen {
			s = SkippedPackage{Package: under, Reason: failureReason(q.Errors), Err: packageError(q)}
		}
		if !variant {
			// The non-test package's own failure is the reason, whatever its
			// test variants report.
			s.Reason, s.Err = failureReason(q.Errors), packageError(q)
		} else if !seen {
			s.Reason += " in test files"
		}
		failed[under] = s
	}
	return failed, warnings, nil
}

// cacheFailure reports whether p failed because go list could not use the
// build cache.
func cacheFailure(p *packages.Package) bool {
	return slices.ContainsFunc(p.Errors, func(e packages.Error) bool {
		return e.Kind == packages.ListError && fromCache(e.Msg)
	})
}

// failureReason classifies a package's load errors, errs, which must not be
// empty: "missing module" when one names a module that is not available,
// else by the kind of the first error (firstError).
func failureReason(errs []packages.Error) string {
	// Fragments of the go command's and the type checker's messages about
	// a module that cannot be found, downloaded or verified.
	missingModule := []string{
		"no required module provides package",
		"cannot find module providing package",
		"missing go.sum entry",
		"invalid version",
		"unknown revision",
		"module lookup disabled",
		"invalid package name: \"\"",
	}
	for _, e := range errs {
		for _, frag := range missingModule {
			if strings.Contains(e.Msg, frag) {
				return "missing module"
			}
		}
	}
	switch firstError(errs).Kind {
	case packages.ParseError:
		return "syntax error"
	case packages.TypeError:
		return "type error"
	default:
		return "build error"
	}
}

// sortSkipped returns the values of failed sorted by import path.
func sortSkipped(failed map[string]SkippedPackage) []SkippedPackage {
	out := make([]SkippedPackage, 0, len(failed))
	for _, s := range failed {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b SkippedPackage) int { return strings.Compare(a.Package, b.Package) })
	return out
}
