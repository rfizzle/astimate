package stub

import (
	"go/ast"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// qualifier writes the types of zeroed variables in one file: a type of
// the package itself unqualified, a type of an imported package by the
// file's name for it, and a type of any other package by a name it adds
// an import for.
type qualifier struct {
	pkg *types.Package
	// names maps an import path to its name in the file ("" for a dot
	// import); taken holds the names in use.
	names map[string]string
	taken map[string]bool
	// added are the packages that need an import, by path.
	added map[string]*types.Package
}

// newQualifier returns the qualifier of file f of pkg.
func newQualifier(f *ast.File, pkg *types.Package, info *types.Info) *qualifier {
	q := &qualifier{pkg: pkg, names: map[string]string{}, taken: map[string]bool{}, added: map[string]*types.Package{}}
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		name, ok := importName(spec, info)
		if err != nil || !ok {
			continue
		}
		q.names[path], q.taken[name] = name, true
	}
	return q
}

// importName returns the name an import gives its package in the file,
// "" for a dot import, and false for a blank import or one go/types did
// not resolve.
func importName(spec *ast.ImportSpec, info *types.Info) (string, bool) {
	switch {
	case spec.Name != nil && spec.Name.Name == "_":
		return "", false
	case spec.Name != nil && spec.Name.Name == ".":
		return "", true
	case spec.Name != nil:
		return spec.Name.Name, true
	}
	pn := info.PkgNameOf(spec)
	if pn == nil {
		return "", false
	}
	return pn.Imported().Name(), true
}

// qualify is a types.Qualifier: the name to write before a type of p.
func (q *qualifier) qualify(p *types.Package) string {
	if p == q.pkg {
		return ""
	}
	if n, ok := q.names[p.Path()]; ok {
		return n
	}
	n := p.Name()
	for i := 2; q.taken[n] || q.pkg.Scope().Lookup(n) != nil; i++ {
		n = p.Name() + strconv.Itoa(i)
	}
	q.names[p.Path()], q.taken[n], q.added[p.Path()] = n, true, p
	return n
}

// importDecl returns the import declaration for the added packages,
// preceded by a blank line, or "" when none was added.
func (q *qualifier) importDecl() string {
	if len(q.added) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nimport (\n")
	for _, p := range slices.Sorted(maps.Keys(q.added)) {
		b.WriteString("\t")
		if n := q.names[p]; n != q.added[p].Name() {
			b.WriteString(n + " ")
		}
		b.WriteString(strconv.Quote(p) + "\n")
	}
	b.WriteString(")")
	return b.String()
}

// writable reports whether t can be written in a file of the package: it
// is valid and typed, and names no unexported or function-local type of
// another package.
func (q *qualifier) writable(t types.Type) bool {
	switch t := t.(type) {
	case *types.Basic:
		return t.Kind() != types.Invalid && t.Info()&types.IsUntyped == 0
	case interface { // *types.Named and *types.Alias
		Obj() *types.TypeName
		TypeArgs() *types.TypeList
	}:
		return q.writableObj(t.Obj()) && q.writableList(t.TypeArgs())
	case *types.Map:
		return q.writable(t.Key()) && q.writable(t.Elem())
	case interface{ Elem() types.Type }: // pointer, slice, array, channel
		return q.writable(t.Elem())
	case *types.Signature:
		return t.TypeParams() == nil && q.writableTuple(t.Params()) && q.writableTuple(t.Results())
	case *types.Struct:
		return !slices.ContainsFunc(slices.Collect(t.Fields()), func(f *types.Var) bool {
			return !q.writable(f.Type()) || (!f.Exported() && f.Pkg() != q.pkg)
		})
	case *types.Interface:
		return t.NumEmbeddeds() == 0 && t.NumExplicitMethods() == 0
	}
	return false
}

// writableObj reports whether the type name obj can be written in the
// package: a predeclared type, a package-level type of the package, or an
// exported package-level type of another.
func (q *qualifier) writableObj(obj *types.TypeName) bool {
	switch p := obj.Pkg(); {
	case p == nil:
		return true
	case obj.Parent() != p.Scope():
		return false
	default:
		return p == q.pkg || obj.Exported()
	}
}

// writableList reports whether every type argument in l is writable.
func (q *qualifier) writableList(l *types.TypeList) bool {
	return !slices.ContainsFunc(slices.Collect(l.Types()), func(t types.Type) bool { return !q.writable(t) })
}

// writableTuple reports whether every variable's type in t is writable.
func (q *qualifier) writableTuple(t *types.Tuple) bool {
	return !slices.ContainsFunc(slices.Collect(t.Variables()), func(v *types.Var) bool { return !q.writable(v.Type()) })
}
