package golang

import (
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// importCounts holds the distinct import paths of a package's non-test files
// by category, the fan-out metrics internal_imports, external_imports and
// stdlib_imports. blank and dot record the paths of blank (_) and dot (.)
// imports, which are not counted in any category; they are kept for
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
// paths per category. A path imported by several files counts once. Blank
// and dot imports are recorded instead of counted, and an import with no
// loaded package, such as cgo's "C", is skipped.
func imports(l *loaded, p *packages.Package) importCounts {
	var c importCounts
	seen := make(map[string]bool, len(p.Imports))
	for _, f := range sourceSyntax(l, p) {
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if spec.Name != nil {
				switch spec.Name.Name {
				case "_":
					c.blank = append(c.blank, path)
					continue
				case ".":
					c.dot = append(c.dot, path)
					continue
				}
			}
			imp, ok := p.Imports[path]
			if !ok || seen[path] {
				continue
			}
			seen[path] = true
			switch classifyImport(l, imp) {
			case importStdlib:
				c.stdlib++
			case importInternal:
				c.internal++
			case importExternal:
				c.external++
			}
		}
	}
	return c
}

// classifyImport returns the category of the imported package imp. It is
// standard library when it belongs to no module and the first element of its
// path has no dot, internal when its path is the module path or lies under
// it, and external otherwise. A replaced local module has a Module and so is
// external. The standard-library test comes first, so a standard-library
// package is never internal.
func classifyImport(l *loaded, imp *packages.Package) importClass {
	first, _, _ := strings.Cut(imp.PkgPath, "/")
	switch {
	case imp.Module == nil && !strings.Contains(first, "."):
		return importStdlib
	case isInternal(l, imp.PkgPath):
		return importInternal
	default:
		return importExternal
	}
}

// isInternal reports whether importPath is the module path of l or lies
// under it. It is the internal test classifyImport applies after ruling out
// the standard library.
func isInternal(l *loaded, importPath string) bool {
	return inModule(l.modulePath, importPath)
}
