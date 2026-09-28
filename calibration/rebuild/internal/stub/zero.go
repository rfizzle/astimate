package stub

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// typeCheckEnv is the build configuration the initializers are
// type-checked under, fixed so the stub does not depend on the host. A
// file this configuration excludes keeps its initializers.
func typeCheckEnv() []string {
	return []string{"GOOS=linux", "GOARCH=amd64"}
}

// zeroEdits returns, by file name, the edits that remove the package
// initialization code that calls a function or method the package defines
// (a call inside a function literal counts only when the literal is called
// there). With the stub, such a call panics before any test runs. A
// package-level var spec whose initializer does so keeps its names and
// gets its declared type, or the type go/types infers, and no value, so
// the variable starts at its zero value; an import the type needs is
// added, and a spec whose type cannot be written in the file (a foreign
// unexported type) is left as it is. A top-level statement of an init body
// that does so is removed; the others stay. The package is type-checked
// only when some initialization code syntactically may call it, so a
// package without such code loads nothing.
func zeroEdits(ctx context.Context, dir string, env []string, srcs []*srcFile) (map[string][]edit, error) {
	if !mayCallAtInit(srcs) {
		return nil, nil
	}
	pkg, err := loadTypes(ctx, dir, env)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*srcFile, len(srcs))
	for _, s := range srcs {
		byName[s.name] = s
	}
	out := map[string][]edit{}
	for _, f := range pkg.Syntax {
		s := byName[filepath.Base(pkg.Fset.File(f.Pos()).Name())]
		if s == nil {
			continue
		}
		// Offsets come from the loaded syntax, which parsed the same bytes.
		loaded := &srcFile{name: s.name, path: s.path, src: s.src, fset: pkg.Fset, file: f}
		if edits := loaded.zeroEdits(pkg.Types, pkg.TypesInfo); len(edits) > 0 {
			out[s.name] = edits
		}
	}
	return out, nil
}

// loadTypes type-checks the package in dir under typeCheckEnv.
func loadTypes(ctx context.Context, dir string, env []string) (*packages.Package, error) {
	cfg := &packages.Config{
		Context: ctx,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Dir: dir,
		Env: slices.Concat(os.Environ(), env, typeCheckEnv()),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("stubbing %s: type-checking initializers: %w", dir, err)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("stubbing %s: type-checking initializers: loaded %d packages", dir, len(pkgs))
	}
	if errs := pkgs[0].Errors; len(errs) > 0 {
		return nil, fmt.Errorf("stubbing %s: type-checking initializers: %w", dir, errs[0])
	}
	return pkgs[0], nil
}

// callsAtInit reports whether evaluating n calls a function for which
// local(fun) holds, fun being the callee expression with parentheses and
// type arguments removed. A function literal's body counts only where the
// literal itself is called.
func callsAtInit(n ast.Node, local func(fun ast.Expr) bool) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			found = found || callHere(n, local)
		}
		return !found
	})
	return found
}

// callHere reports whether call is of a function for which local holds,
// or of a function literal whose body calls one at once.
func callHere(call *ast.CallExpr, local func(fun ast.Expr) bool) bool {
	fun := callee(call.Fun)
	if lit, ok := fun.(*ast.FuncLit); ok {
		return callsAtInit(lit.Body, local)
	}
	return local(fun)
}

// callee strips parentheses and type arguments from a call's function
// expression.
func callee(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		default:
			return e
		}
	}
}

// varSpecs calls fn for every package-level var spec of f with a value.
func varSpecs(f *ast.File, fn func(gd *ast.GenDecl, vs *ast.ValueSpec)) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, vs := range valueSpecs(gd) {
			fn(gd, vs)
		}
	}
}

// valueSpecs returns the specs of gd that have values.
func valueSpecs(gd *ast.GenDecl) []*ast.ValueSpec {
	out := make([]*ast.ValueSpec, len(gd.Specs))
	for i, spec := range gd.Specs {
		out[i], _ = spec.(*ast.ValueSpec)
	}
	return slices.DeleteFunc(out, func(vs *ast.ValueSpec) bool { return vs == nil || len(vs.Values) == 0 })
}

// mayCallAtInit reports, from syntax alone, whether initialization code of the
// package may call its own code: a call of a name the package declares as
// a function, or of a selector naming one of its methods that is not
// qualified by an import. It holds whenever zeroEdits would edit, so a
// false answer skips type-checking.
func mayCallAtInit(srcs []*srcFile) bool {
	funcs, methods := map[string]bool{}, map[string]bool{}
	for _, s := range srcs {
		for _, decl := range s.file.Decls {
			declare(decl, funcs, methods)
		}
	}
	return slices.ContainsFunc(srcs, func(s *srcFile) bool { return s.mayCall(funcs, methods) })
}

// mayCall reports whether the file's initialization code may call one of
// funcs, or of methods through a selector not qualified by an import.
func (s *srcFile) mayCall(funcs, methods map[string]bool) bool {
	qualified := importNames(s.file)
	local := func(fun ast.Expr) bool {
		switch f := fun.(type) {
		case *ast.Ident:
			return funcs[f.Name]
		case *ast.SelectorExpr:
			x, ok := f.X.(*ast.Ident)
			return methods[f.Sel.Name] && (!ok || !qualified[x.Name])
		}
		return false
	}
	found := slices.ContainsFunc(initStmts(s.file), func(st ast.Stmt) bool { return callsAtInit(st, local) })
	varSpecs(s.file, func(_ *ast.GenDecl, vs *ast.ValueSpec) {
		found = found || slices.ContainsFunc(vs.Values, func(v ast.Expr) bool { return callsAtInit(v, local) })
	})
	return found
}

// declare adds the names decl declares as functions to funcs, and as
// methods, of a type or of an interface type, to methods.
func declare(decl ast.Decl, funcs, methods map[string]bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil {
			funcs[d.Name.Name] = true
			return
		}
		methods[d.Name.Name] = true
	case *ast.GenDecl:
		for _, it := range interfaceTypes(d) {
			addNames(it.Methods, methods)
		}
	}
}

// interfaceTypes returns the interface types d declares.
func interfaceTypes(d *ast.GenDecl) []*ast.InterfaceType {
	var out []*ast.InterfaceType
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if it, ok := ts.Type.(*ast.InterfaceType); ok {
			out = append(out, it)
		}
	}
	return out
}

// addNames adds the names of the fields in l to names.
func addNames(l *ast.FieldList, names map[string]bool) {
	for _, f := range l.List {
		for _, n := range f.Names {
			names[n.Name] = true
		}
	}
}

// importNames returns the names f may qualify an identifier with: each
// import's explicit name, and the last element of each import path, which
// is the package name of most packages.
func importNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range f.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		names[path[strings.LastIndexByte(path, '/')+1:]] = true
		if spec.Name != nil {
			names[spec.Name.Name] = true
		}
	}
	return names
}

// calleeObj returns the object a call's callee expression fun, stripped by
// callee, refers to, or nil.
func calleeObj(fun ast.Expr, info *types.Info) types.Object {
	switch f := fun.(type) {
	case *ast.Ident:
		return info.Uses[f]
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[f]; ok {
			return sel.Obj()
		}
		return info.Uses[f.Sel]
	}
	return nil
}

// zeroEdits returns the edits of the file's var specs whose initializers
// call pkg's own functions or methods at initialization, with one more
// edit adding the imports their types need.
func (s *srcFile) zeroEdits(pkg *types.Package, info *types.Info) []edit {
	local := func(fun ast.Expr) bool {
		fn, ok := calleeObj(fun, info).(*types.Func)
		return ok && fn.Pkg() == pkg
	}
	q := newQualifier(s.file, pkg, info)
	var edits []edit
	varSpecs(s.file, func(gd *ast.GenDecl, vs *ast.ValueSpec) {
		calls := slices.ContainsFunc(vs.Values, func(v ast.Expr) bool { return callsAtInit(v, local) })
		if !calls {
			return
		}
		if text, ok := s.zeroSpec(gd, vs, q, info); ok {
			edits = append(edits, edit{start: s.offset(vs.Pos()), end: s.offset(vs.End()), text: text, kind: editVar})
		}
	})
	for _, st := range initStmts(s.file) {
		if callsAtInit(st, local) {
			start, end := s.lines(s.offset(st.Pos()), s.offset(st.End()))
			edits = append(edits, edit{start: start, end: end, kind: editInit})
		}
	}
	if imp := q.importDecl(); imp != "" {
		at := s.offset(s.file.Name.End())
		edits = append(edits, edit{start: at, end: at, text: imp, kind: editImport})
	}
	return edits
}

// lines widens src[start:end] to the whole lines it spans, newline
// included, when nothing but white space shares them, so removing it
// leaves no blank line; otherwise it returns start and end.
func (s *srcFile) lines(start, end int) (int, int) {
	first := bytes.LastIndexByte(s.src[:start], '\n') + 1
	next := bytes.IndexByte(s.src[end:], '\n')
	if next < 0 || len(bytes.TrimSpace(s.src[first:start])) > 0 || len(bytes.TrimSpace(s.src[end:end+next])) > 0 {
		return start, end
	}
	return first, end + next + 1
}

// initStmts returns the top-level statements of every init function of f.
func initStmts(f *ast.File) []ast.Stmt {
	var out []ast.Stmt
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && isInit(fn) {
			out = append(out, fn.Body.List...)
		}
	}
	return out
}

// zeroSpec returns the text of vs without its values: its names and its
// declared type, or, without one, each name's type as go/types inferred
// it. Names of different types become one spec each. It reports false
// when a type cannot be written in the file.
func (s *srcFile) zeroSpec(gd *ast.GenDecl, vs *ast.ValueSpec, q *qualifier, info *types.Info) (string, bool) {
	names := make([]string, len(vs.Names))
	for i, n := range vs.Names {
		names[i] = n.Name
	}
	if vs.Type != nil {
		return strings.Join(names, ", ") + " " + string(s.src[s.offset(vs.Type.Pos()):s.offset(vs.Type.End())]), true
	}
	typs := make([]string, len(vs.Names))
	for i, n := range vs.Names {
		obj := info.Defs[n]
		if obj == nil || !q.writable(obj.Type()) {
			return "", false
		}
		typs[i] = types.TypeString(obj.Type(), q.qualify)
	}
	if !slices.ContainsFunc(typs, func(t string) bool { return t != typs[0] }) {
		return strings.Join(names, ", ") + " " + typs[0], true
	}
	sep := "\nvar "
	if gd.Lparen.IsValid() {
		sep = "\n"
	}
	parts := make([]string, len(names))
	for i := range names {
		parts[i] = names[i] + " " + typs[i]
	}
	return strings.Join(parts, sep), true
}
