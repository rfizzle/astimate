// Package imports classifies the imports of a Go module's packages and
// builds the module's reverse import graph: the fan-out metrics
// internal_imports, external_imports and stdlib_imports, and the fan-in
// metrics fan_in and fan_in_tests (SPEC.md section 6). Both sides count the
// edges Imported lists, so the module-wide sums of fan_in and
// internal_imports agree.
package imports

import (
	"go/ast"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// Counts holds the distinct imported packages of a package's non-test files
// by category, the fan-out metrics internal_imports, external_imports and
// stdlib_imports. Blank and Dot record the paths of blank (_) and dot (.)
// imports, which count like any other import; they are kept for debugging.
type Counts struct {
	Internal, External, Stdlib int
	Blank, Dot                 []string
}

// Class is the category of one imported package.
type Class int

// The categories Classify returns.
const (
	Stdlib Class = iota
	Internal
	External
)

// Count classifies the imports of p's non-test files and counts distinct
// imported packages per category, as Imported lists them: blank and dot
// imports count, a package imported by several files counts once, and an
// import with no loaded package, such as cgo's "C", is skipped. Blank and
// dot imports are also recorded by path.
func Count(m *load.Module, p *packages.Package) Counts {
	var c Counts
	files := m.SourceSyntax(p)
	for _, f := range files {
		for _, spec := range f.Imports {
			if spec.Name == nil || (spec.Name.Name != "_" && spec.Name.Name != ".") {
				continue
			}
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if spec.Name.Name == "_" {
				c.Blank = append(c.Blank, path)
			} else {
				c.Dot = append(c.Dot, path)
			}
		}
	}
	for _, imp := range Imported(p, files) {
		switch Classify(m, imp) {
		case Stdlib:
			c.Stdlib++
		case Internal:
			c.Internal++
		case External:
			c.External++
		}
	}
	return c
}

// Imported returns the distinct packages the import specs of files resolve
// to through p.Imports, in order of first appearance, deduplicated by
// package path. It is the one edge list behind both fan-out
// (internal_imports) and fan-in, so the two count the same edges. Every
// import spec counts whatever its name, blank (_) and dot (.) included,
// since each is a real dependency. An import with no loaded package, such
// as cgo's "C", is skipped, and the imports cgo adds to the files it
// generates, such as runtime/cgo, are never seen when files are the source
// files (see load.Module.SourceSyntax). Keying by package path makes a
// vendored package, imported as golang.org/x/... but with package path
// vendor/golang.org/x/..., one node on both sides.
func Imported(p *packages.Package, files []*ast.File) []*packages.Package {
	out := make([]*packages.Package, 0, len(p.Imports))
	seen := make(map[string]bool, len(p.Imports))
	for _, f := range files {
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			imp, ok := p.Imports[path]
			if !ok || seen[imp.PkgPath] {
				continue
			}
			seen[imp.PkgPath] = true
			out = append(out, imp)
		}
	}
	return out
}

// Classify returns the category of the imported package imp. It is standard
// library when it belongs to no module and the first element of its path
// has no dot, internal when its path is the module path or lies under it,
// and external otherwise. A replaced local module has a Module and so is
// external. The standard-library test comes first, so a standard-library
// package is never internal, except in a load of the whole standard library
// (load.StdAllModulePath), where it is the module and so internal.
func Classify(m *load.Module, imp *packages.Package) Class {
	switch {
	case imp.Module == nil && stdlibPath(imp.PkgPath):
		if m.Path == load.StdAllModulePath {
			return Internal
		}
		return Stdlib
	case isInternal(m, imp.PkgPath):
		return Internal
	default:
		return External
	}
}

// isInternal reports whether importPath is the module path of m or lies
// under it. It is the internal test Classify applies after ruling out the
// standard library. In a load of the whole standard library every
// standard-library path is internal, as Classify has it, so fan_in and
// internal_imports both count the edges between standard-library packages.
func isInternal(m *load.Module, importPath string) bool {
	if m.Path == load.StdAllModulePath {
		return stdlibPath(importPath)
	}
	return load.InModule(m.Path, importPath)
}

// stdlibPath reports whether the first element of importPath has no dot,
// the path shape of a standard-library package.
func stdlibPath(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
