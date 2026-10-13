package load

import (
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// EmbedWarning is a //go:embed pattern of a module package that matches no
// file. A build directory that is gitignored exists in the working tree and
// not in the checkout of a baseline, where go list reports the pattern as a
// package error. The metrics never read embedded files, so the package
// loads, and is extracted, as usual.
type EmbedWarning struct {
	// Package is the import path of the package holding the pattern.
	Package string
	// Pattern is the embed pattern as written, such as "all:build".
	Pattern string
}

// embedPattern returns the pattern of e when e is go list's report of an
// embed pattern that matches no file: "pattern all:build: no matching files
// found".
func embedPattern(e packages.Error) (string, bool) {
	if e.Kind != packages.ListError {
		return "", false
	}
	rest, ok := strings.CutPrefix(e.Msg, "pattern ")
	if !ok {
		return "", false
	}
	pattern, ok := strings.CutSuffix(rest, ": no matching files found")
	if !ok || pattern == "" {
		return "", false
	}
	return pattern, true
}

// tolerateEmbeds appends to warnings one entry for each embed-pattern error
// of p, and reports whether p has any other error, which fails the load.
// The test variants of a package repeat its errors, so duplicates are left
// for sortWarnings to drop.
func tolerateEmbeds(warnings []EmbedWarning, p *packages.Package) ([]EmbedWarning, bool) {
	fatal := false
	for _, e := range p.Errors {
		pattern, ok := embedPattern(e)
		if !ok {
			fatal = true
			continue
		}
		under, _ := forTest(p)
		if under == "" {
			under = p.PkgPath
		}
		warnings = append(warnings, EmbedWarning{Package: under, Pattern: pattern})
	}
	return warnings, fatal
}

// withoutEmbedErrors returns a copy of p without its embed-pattern errors.
func withoutEmbedErrors(p *packages.Package) *packages.Package {
	q := *p
	q.Errors = slices.DeleteFunc(slices.Clone(p.Errors), func(e packages.Error) bool {
		_, ok := embedPattern(e)
		return ok
	})
	return &q
}

// sortWarnings sorts warnings by package then pattern and drops repeats.
func sortWarnings(warnings []EmbedWarning) []EmbedWarning {
	slices.SortFunc(warnings, func(a, b EmbedWarning) int {
		return strings.Compare(a.Package+"\x00"+a.Pattern, b.Package+"\x00"+b.Pattern)
	})
	return slices.Compact(warnings)
}
