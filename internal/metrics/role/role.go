// Package role names what a package is for in its module (SPEC.md 6.5):
// the role an extractor records on each package row
// (metrics.RawMetrics.Role) and a threshold's roles list names. It
// imports the standard library only, so the extractors, the gate and the
// rebuild estimate can all share the names.
package role

import "slices"

// The roles, in the order SPEC.md 6.5 lists them.
const (
	// Main is a composition root: a Go package main, or a TypeScript
	// package a bin entry of package.json names or that exports nothing.
	// Nothing can import it.
	Main = "main"
	// Library is every package no other role describes.
	Library = "library"
	// TestSupport is a package only tests use: some other package's test
	// file refers to it and no package of the module imports it from a
	// non-test file (SPEC.md 6.4, rule d).
	TestSupport = "test-support"
	// GeneratedOnly is a package all of whose non-test files are
	// generated (SPEC.md 6.5).
	GeneratedOnly = "generated-only"
)

// Names returns every role in the order SPEC.md 6.5 lists them. The slice
// is freshly allocated on each call.
func Names() []string {
	return []string{Main, Library, TestSupport, GeneratedOnly}
}

// Known reports whether s names a role.
func Known(s string) bool {
	return slices.Contains(Names(), s)
}

// Of returns the role a row carrying r is judged as: r itself, or Library
// for the empty role of a row recorded before roles existed or by an
// extractor that records none.
func Of(r string) string {
	if r == "" {
		return Library
	}
	return r
}
