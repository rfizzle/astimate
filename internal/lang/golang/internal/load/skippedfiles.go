package load

import (
	"slices"
	"strings"
)

// SkippedFiles names the source files of one package skipped because it
// fails to load (SkippedPackage). It has no type information, but its
// files usually still parse: the test reference index reads its test files
// syntactically, and the cross-package duplication stream scans its
// non-test files, so its neighbours keep the references and blocks it
// holds (SPEC.md 6.4, 6.5).
type SkippedFiles struct {
	// Package is the import path of the package.
	Package string
	// Dir is the package directory.
	Dir string
	// GoFiles are the absolute names of its non-test Go files, as go list
	// reported them; empty when go list returned no non-test package.
	GoFiles []string
	// TestGoFiles are the absolute names of its _test.go files, in-package
	// and external, sorted.
	TestGoFiles []string
}

// SkippedSources returns the files of every skipped package go list
// returned packages for, in the order of SkippedPackages, by import path.
func (m *Module) SkippedSources() []SkippedFiles {
	out := make([]SkippedFiles, 0, len(m.skipped))
	for _, sp := range m.SkippedPackages {
		path := sp.Package
		s, ok := m.skipped[path]
		if !ok {
			continue
		}
		f := SkippedFiles{Package: path}
		if s.pkg != nil {
			f.Dir = s.pkg.Dir
			f.GoFiles = slices.Clone(s.pkg.GoFiles)
		}
		for _, tp := range s.tests {
			if f.Dir == "" {
				f.Dir = tp.Dir
			}
			for _, name := range tp.GoFiles {
				if strings.HasSuffix(name, "_test.go") && !slices.Contains(f.TestGoFiles, name) {
					f.TestGoFiles = append(f.TestGoFiles, name)
				}
			}
		}
		slices.Sort(f.TestGoFiles)
		out = append(out, f)
	}
	return out
}
