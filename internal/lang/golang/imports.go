package golang

import (
	"go/ast"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// importCounts holds the distinct imported packages of a package's non-test
// files by category, the fan-out metrics internal_imports, external_imports
// and stdlib_imports. blank and dot record the paths of blank (_) and dot
// (.) imports, which count like any other import; they are kept for
// debugging.
type importCounts struct {
	internal, external, stdlib int
	blank, dot                 []string
}

// importClass is the category of one imported package.
type importClass int

const (
	importStdlib importClass = iota
	importInternal
	importExternal
)

// imports classifies the imports of p's non-test files and counts distinct
// imported packages per category, as importedPackages lists them: blank and
// dot imports count, a package imported by several files counts once, and
// an import with no loaded package, such as cgo's "C", is skipped. Blank
// and dot imports are also recorded by path.
func imports(l *loaded, p *packages.Package) importCounts {
	var c importCounts
	files := sourceSyntax(l, p)
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
				c.blank = append(c.blank, path)
			} else {
				c.dot = append(c.dot, path)
			}
		}
	}
	for _, imp := range importedPackages(p, files) {
		switch classifyImport(l, imp) {
		case importStdlib:
			c.stdlib++
		case importInternal:
			c.internal++
		case importExternal:
			c.external++
		}
	}
	return c
}

// importedPackages returns the distinct packages the import specs of files
// resolve to through p.Imports, in order of first appearance, deduplicated
// by package path. It is the one edge list behind both fan-out
// (internal_imports) and fan-in, so the two count the same edges. Every
// import spec counts whatever its name, blank (_) and dot (.) included,
// since each is a real dependency. An import with no loaded package, such
// as cgo's "C", is skipped, and the imports cgo adds to the files it
// generates, such as runtime/cgo, are never seen when files are the source
// files (see sourceSyntax). Keying by package path makes a vendored
// package, imported as golang.org/x/... but with package path
// vendor/golang.org/x/..., one node on both sides.
func importedPackages(p *packages.Package, files []*ast.File) []*packages.Package {
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

// classifyImport returns the category of the imported package imp. It is
// standard library when it belongs to no module and the first element of its
// path has no dot, internal when its path is the module path or lies under
// it, and external otherwise. A replaced local module has a Module and so is
// external. The standard-library test comes first, so a standard-library
// package is never internal, except in a load of the whole standard library
// (stdAllModulePath), where it is the module and so internal.
func classifyImport(l *loaded, imp *packages.Package) importClass {
	switch {
	case imp.Module == nil && stdlibPath(imp.PkgPath):
		if l.modulePath == stdAllModulePath {
			return importInternal
		}
		return importStdlib
	case isInternal(l, imp.PkgPath):
		return importInternal
	default:
		return importExternal
	}
}

// isInternal reports whether importPath is the module path of l or lies
// under it. It is the internal test classifyImport applies after ruling out
// the standard library. In a load of the whole standard library every
// standard-library path is internal, as classifyImport has it, so fan_in
// and internal_imports both count the edges between standard-library
// packages.
func isInternal(l *loaded, importPath string) bool {
	if l.modulePath == stdAllModulePath {
		return stdlibPath(importPath)
	}
	return inModule(l.modulePath, importPath)
}

// stdlibPath reports whether the first element of importPath has no dot,
// the path shape of a standard-library package.
func stdlibPath(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
