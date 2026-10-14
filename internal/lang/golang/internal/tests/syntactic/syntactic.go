// Package syntactic reads the test files of the packages a Go module load
// skipped because they fail to load, by name, for the test reference index
// behind untested_exports and has_tests (SPEC.md 6.4).
//
// A skipped package has no type information, so its test files cannot be
// resolved through types.Info.Uses. They are parsed instead and matched by
// name, as the TypeScript extractor matches: in a _test.go file of a
// skipped package, a selector pkg.Name whose pkg is the name an import of
// the file gives a loaded package of the module, or a bare identifier Name
// in a file that dot-imports one (the file's own declarations aside),
// refers to the exported func Name of that package; and any other selector
// x.Name, x not the name of an import, refers to every exported method
// called Name, on any receiver, of each loaded package the file imports.
// Shadowing and receiver types are not checked, so the match may
// over-count.
package syntactic

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
)

// names is what the test files of one skipped package name of one loaded
// package: exported func names and exported method names.
type names struct {
	funcs, methods map[string]bool
}

// Funcs returns the funcs and methods of the loaded packages of m that the
// test files of the skipped packages of m refer to by name. A file that
// does not parse is read as far as the parser got.
func Funcs(m *load.Module) []*types.Func {
	var out []*types.Func
	for _, s := range m.SkippedSources() {
		if len(s.TestGoFiles) == 0 {
			continue
		}
		byPath := make(map[string]*names)
		fset := token.NewFileSet()
		for _, name := range s.TestGoFiles {
			if f, _ := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution); f != nil {
				refs(m, f, byPath)
			}
		}
		for path, n := range byPath {
			out = resolve(out, m, path, n)
		}
	}
	return out
}

// fileImports is the import block of one file as the name rules read it.
type fileImports struct {
	// byName maps the name an import gives a loaded package to its path.
	byName map[string]string
	// pkgNames holds the name every import gives its package, the last
	// element of the path standing in for a package outside the module, so
	// a selector on one is not read as a method call.
	pkgNames map[string]bool
	// dot and loaded hold the paths of the dot-imported loaded packages and
	// of every imported loaded package.
	dot, loaded []string
}

// importsOf reads the import block of f against the loaded packages of m.
func importsOf(m *load.Module, f *ast.File) fileImports {
	im := fileImports{byName: make(map[string]string), pkgNames: make(map[string]bool)}
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		p, ok := m.Pkgs[path]
		var name string
		switch {
		case spec.Name != nil:
			name = spec.Name.Name
		case ok:
			name = p.Name
		default:
			for _, guess := range guessNames(path) {
				im.pkgNames[guess] = true
			}
			continue
		}
		if ok {
			im.loaded = append(im.loaded, path)
		}
		switch {
		case name == "." && ok:
			im.dot = append(im.dot, path)
		case name == "." || name == "_":
		case ok:
			im.byName[name] = path
			im.pkgNames[name] = true
		default:
			im.pkgNames[name] = true
		}
	}
	return im
}

// guessNames returns the names an unnamed import of the package at path,
// outside the module, may give it without type information: its last path
// element, the one before a major version element (math/rand/v2 is rand),
// the element less a .vN suffix (gopkg.in/yaml.v3 is yaml), and less a go-
// prefix or a -go suffix (go-foo and foo-go are often foo). A wrong guess
// only lets a selector on that name count as a method call, an over-count.
func guessNames(path string) []string {
	elems := strings.Split(path, "/")
	last := elems[len(elems)-1]
	if isMajor(last) && len(elems) > 1 {
		last = elems[len(elems)-2]
	}
	if i := strings.LastIndex(last, ".v"); i > 0 && isMajor(last[i+1:]) {
		last = last[:i]
	}
	out := []string{last}
	if s, ok := strings.CutPrefix(last, "go-"); ok {
		out = append(out, s)
	}
	if s, ok := strings.CutSuffix(last, "-go"); ok {
		out = append(out, s)
	}
	return out
}

// isMajor reports whether elem is a major version element such as v2.
func isMajor(elem string) bool {
	if len(elem) < 2 || elem[0] != 'v' {
		return false
	}
	for _, c := range elem[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// refs adds to byPath, keyed by import path, what f names of the loaded
// packages of m that its import block imports.
func refs(m *load.Module, f *ast.File, byPath map[string]*names) {
	v := visitor{im: importsOf(m, f), byPath: byPath, skip: make(map[*ast.Ident]bool)}
	if len(v.im.loaded) > 0 {
		ast.Inspect(f, v.visit)
	}
}

// visitor applies the name rules to the nodes of one file.
type visitor struct {
	im     fileImports
	byPath map[string]*names
	// skip holds the identifiers the dot-import rule leaves alone: the Sel
	// of a selector, which the selector rule reads, and the names of the
	// file's own declarations.
	skip map[*ast.Ident]bool
}

// visit is the ast.Inspect callback.
func (v visitor) visit(node ast.Node) bool {
	switch x := node.(type) {
	case *ast.ImportSpec:
		return false
	case *ast.FuncDecl:
		v.skip[x.Name] = true
	case *ast.TypeSpec:
		v.skip[x.Name] = true
	case *ast.ValueSpec:
		for _, id := range x.Names {
			v.skip[id] = true
		}
	case *ast.SelectorExpr:
		v.selector(x)
	case *ast.Ident:
		if !v.skip[x] && x.IsExported() {
			for _, path := range v.im.dot {
				v.of(path).funcs[x.Name] = true
			}
		}
	}
	return true
}

// selector records what an exported selector x names: a func of the
// package an import name selects, nothing for a package outside the
// module, and otherwise a method of every imported loaded package.
func (v visitor) selector(x *ast.SelectorExpr) {
	v.skip[x.Sel] = true
	if !x.Sel.IsExported() {
		return
	}
	if id, ok := x.X.(*ast.Ident); ok && v.im.pkgNames[id.Name] {
		if path, ok := v.im.byName[id.Name]; ok {
			v.of(path).funcs[x.Sel.Name] = true
		}
		return
	}
	for _, path := range v.im.loaded {
		v.of(path).methods[x.Sel.Name] = true
	}
}

// of returns the names recorded for the package at path, adding them on
// first use.
func (v visitor) of(path string) *names {
	n := v.byPath[path]
	if n == nil {
		n = &names{funcs: make(map[string]bool), methods: make(map[string]bool)}
		v.byPath[path] = n
	}
	return n
}

// resolve appends to out the funcs and methods of the loaded package at
// path that n names.
func resolve(out []*types.Func, m *load.Module, path string, n *names) []*types.Func {
	p := m.Pkgs[path]
	if p == nil || p.Types == nil {
		return out
	}
	scope := p.Types.Scope()
	for _, name := range scope.Names() {
		switch obj := scope.Lookup(name).(type) {
		case *types.Func:
			if n.funcs[name] {
				out = append(out, obj)
			}
		case *types.TypeName:
			named, ok := obj.Type().(*types.Named)
			if !ok || obj.IsAlias() {
				continue
			}
			for fn := range named.Methods() {
				if n.methods[fn.Name()] {
					out = append(out, fn)
				}
			}
		}
	}
	return out
}
