// Package tests measures the tests of one Go package: test_files,
// test_funcs and has_tests from its test files, and untested_exports from
// the references its tests make to the package's exported funcs and methods
// (SPEC.md 6.4).
package tests

import (
	"go/ast"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// Counts holds the test metrics test_files, test_funcs and has_tests of one
// package, with its external test package folded in.
type Counts struct {
	TestFiles, TestFuncs int
	HasTests             bool
}

// Count counts the _test.go files of p's in-package test variant and
// external test package, and the Test, Benchmark, Fuzz and Example functions
// declared in them that go test would run. A file is counted once by path
// even if both variants list it.
//
// Parameter types are checked syntactically: a Test, Benchmark or Fuzz
// function qualifies only when its parameter is written *<name>.T (B, F),
// where <name> is the name the file imports "testing" under, or *T when
// "testing" is dot-imported. This works on packages without type
// information; a local type alias of testing.T is not recognized.
func Count(m *load.Module, p *packages.Package) Counts {
	var c Counts
	seen := make(map[string]bool)
	for _, tp := range m.TestPackages(p) {
		for _, f := range tp.Syntax {
			name := m.Fset.Position(f.Package).Filename
			if !strings.HasSuffix(name, "_test.go") || seen[name] {
				continue
			}
			seen[name] = true
			c.TestFiles++
			c.TestFuncs += countTestFuncs(f)
		}
	}
	c.HasTests = c.TestFuncs > 0
	return c
}

// countTestFuncs counts the top-level functions of f that go test runs.
func countTestFuncs(f *ast.File) int {
	pkgName := testingName(f)
	n := 0
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && isTestFunc(fd, pkgName) {
			n++
		}
	}
	return n
}

// testingName returns the name f imports "testing" under: "testing", an
// alias, or "." for a dot import. It returns "" when f does not import
// "testing" or imports it only blank.
func testingName(f *ast.File) string {
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != "testing" {
			continue
		}
		if spec.Name == nil {
			return "testing"
		}
		if spec.Name.Name != "_" {
			return spec.Name.Name
		}
	}
	return ""
}

// isTestFunc reports whether fd is a Test, Benchmark, Fuzz or Example
// function by go test's rules: no receiver or type parameters, a name that
// is the prefix alone or the prefix followed by a non-lowercase rune, no
// results, and exactly one parameter of type *testing.T, *testing.B or
// *testing.F respectively, or no parameters for Example. pkgName is the name
// "testing" is imported under, "" when it is not imported.
func isTestFunc(fd *ast.FuncDecl, pkgName string) bool {
	if fd.Recv != nil || fd.Type.TypeParams != nil || fd.Type.Results.NumFields() != 0 {
		return false
	}
	name := fd.Name.Name
	if hasTestPrefix(name, "Example") {
		return fd.Type.Params.NumFields() == 0
	}
	for _, k := range [...]struct{ prefix, typ string }{
		{"Test", "T"},
		{"Benchmark", "B"},
		{"Fuzz", "F"},
	} {
		if hasTestPrefix(name, k.prefix) {
			return pkgName != "" && fd.Type.Params.NumFields() == 1 &&
				isTestingPointer(fd.Type.Params.List[0].Type, pkgName, k.typ)
		}
	}
	return false
}

// hasTestPrefix reports whether name is prefix, or prefix followed by a rune
// that is not lower case, the rule go test uses to find test functions.
func hasTestPrefix(name, prefix string) bool {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(r)
}

// isTestingPointer reports whether expr is written *<pkgName>.<typ>, or
// *<typ> when pkgName is ".".
func isTestingPointer(expr ast.Expr, pkgName, typ string) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	if pkgName == "." {
		id, ok := star.X.(*ast.Ident)
		return ok && id.Name == typ
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != typ {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkgName
}
