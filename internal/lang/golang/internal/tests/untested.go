package tests

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// untestedDirective marks an exported function or method as intentionally
// untested when it is a line of the declaration's doc comment, alone or
// followed by a space and a reason.
const untestedDirective = "//astimate:untested"

// UntestedCounts is the untested_exports metric of one package.
type UntestedCounts struct {
	// Untested is the number of exported funcs and methods of the package
	// that no test file references.
	Untested int
	// Names are the untested funcs and methods, written Func or
	// Type.Method, sorted.
	Names []string
	// Pos holds the declaring identifier of each of Names, index for
	// index, for annotations.
	Pos []token.Pos
	// Excluded are the exported funcs and methods carrying the
	// //astimate:untested directive, in the same form, sorted. They are a
	// debug record of what the directive removed from the count.
	Excluded []string
}

// Untested counts the exported funcs and methods declared in p's
// authored non-test files that no identifier in any _test.go file of p's
// in-package test variant or external test package refers to (SPEC.md
// 6.4). A generated file's exports are left out (see
// load.Module.AuthoredSyntax): a rebuild regenerates them, so they need no
// test.
//
// The non-test package and its test variants are separate type-checks, so
// their objects are not pointer-equal. Declarations are identified by a key
// instead: the name for a func, and <receiver type>.<name> for a method,
// where the receiver type is the named type after dereferencing a pointer.
// A method on an unexported type counts when its own name is exported.
//
// A test reference marks a key when it resolves through types.Info.Uses to
// a func or method of p, which covers direct calls, method values, method
// expressions and methods promoted through embedding (Uses resolves those
// to the embedded type's method). A call or method value through an
// interface, or through a type parameter, resolves to the interface's
// method instead; it marks every method of p with that name whose receiver
// type T, or *T, implements the interface the selection is made on,
// whichever package declares that interface. When T is generic, the check
// runs on each instantiation of T in the test files instead of on T itself.
// When the interface mentions the type parameters of a generic function
// declared in a test file, such as Getter[T] inside a helper
// get[T any](g Getter[T]), the check runs once per instantiation of that
// function in the test files, on the receiver type with the type arguments
// substituted; a receiver that substitutes to a concrete type marks the
// method it selects directly. A method of a generic type declared in a test
// file is treated the same way, once per instantiation of that type in the
// test files. A helper instantiated inside another with the outer one's
// type parameters, such as get[T] called from outer[T], takes the outer
// one's instantiations with their arguments substituted.
//
// Unlike the syntactic metrics, it reads p.Syntax rather than SourceSyntax,
// because p.TypesInfo.Defs is keyed by the identifiers of those trees. For a
// cgo package they are the files cgo generated: the rewritten sources keep
// every declaration and doc comment of the original, and the helper files
// declare nothing exported, so the count matches the source files. Because
// cgo marks every file it writes as generated, a tree of p.Syntax is
// matched to a generated source file by name, its own or the one its
// package clause's //line directive names, never by its own header.
func Untested(m *load.Module, p *packages.Package) UntestedCounts {
	var c UntestedCounts
	// marked holds every counted key, true once a test refers to it.
	marked := make(map[string]bool)
	// recvs maps a method name to the receiver type names declaring it, for
	// interface dispatch.
	recvs := make(map[string][]string)
	// declared holds the position of each counted key's identifier.
	declared := make(map[string]token.Pos)
	generated := m.GeneratedNames(p)
	for _, f := range p.Syntax {
		if isGeneratedTree(m, p, f, generated) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			switch key, recv, kind := exportKey(p.TypesInfo, fd); kind {
			case exportExcluded:
				c.Excluded = append(c.Excluded, key)
			case exportCounted:
				marked[key] = false
				declared[key] = fd.Name.Pos()
				if recv != "" {
					recvs[fd.Name.Name] = append(recvs[fd.Name.Name], recv)
				}
			}
		}
	}
	for _, tp := range m.TestPackages(p) {
		markTestRefs(m, p.PkgPath, tp, marked, recvs)
	}
	for key, ok := range marked {
		if !ok {
			c.Names = append(c.Names, key)
		}
	}
	slices.Sort(c.Names)
	slices.Sort(c.Excluded)
	c.Pos = make([]token.Pos, len(c.Names))
	for i, key := range c.Names {
		c.Pos[i] = declared[key]
	}
	c.Untested = len(c.Names)
	return c
}

// exportKind says how untested_exports treats one declaration.
type exportKind int

const (
	// exportIgnored is not an exported func or method untested_exports
	// counts.
	exportIgnored exportKind = iota
	// exportExcluded carries the //astimate:untested directive.
	exportExcluded
	// exportCounted is counted until a test refers to it.
	exportCounted
)

// exportKey returns the key and receiver type name (see funcKey) of fd, a
// declaration of the package whose type information is info, and how
// untested_exports treats it.
func exportKey(info *types.Info, fd *ast.FuncDecl) (key, recv string, kind exportKind) {
	if !fd.Name.IsExported() {
		return "", "", exportIgnored
	}
	fn, ok := info.Defs[fd.Name].(*types.Func)
	if !ok {
		return "", "", exportIgnored
	}
	key, recv, ok = funcKey(fn)
	switch {
	case !ok:
		return "", "", exportIgnored
	case hasUntestedDirective(fd.Doc):
		return key, recv, exportExcluded
	}
	return key, recv, exportCounted
}

// isGeneratedTree reports whether f, a tree of p.Syntax, is one of the
// generated source files named in generated: by its file name, which is the
// source name unless p uses cgo, or by the file its package clause is
// attributed to through a //line directive, which for a cgo-rewritten tree
// is the source file cgo read. A nil m resolves positions in p.Fset.
func isGeneratedTree(m *load.Module, p *packages.Package, f *ast.File, generated map[string]bool) bool {
	if len(generated) == 0 {
		return false
	}
	fset := p.Fset
	if m != nil && m.Fset != nil {
		fset = m.Fset
	}
	if fset == nil {
		return false
	}
	if tf := fset.File(f.FileStart); tf != nil && generated[tf.Name()] {
		return true
	}
	return generated[fset.Position(f.Package).Filename]
}

// markTestRefs sets marked[key] for every key of the package at pkgPath
// that an identifier in one of tp's _test.go files refers to, in one pass
// over tp's Uses and one over its Selections. Keys absent from marked are
// ignored.
func markTestRefs(m *load.Module, pkgPath string, tp *packages.Package, marked map[string]bool, recvs map[string][]string) {
	files := make(map[*token.File]bool, len(tp.Syntax))
	for _, f := range tp.Syntax {
		if tf := m.Fset.File(f.Pos()); tf != nil && strings.HasSuffix(tf.Name(), "_test.go") {
			files[tf] = true
		}
	}
	if len(files) == 0 || tp.TypesInfo == nil {
		return
	}
	inTest := func(pos token.Pos) bool { return files[m.Fset.File(pos)] }

	for id, obj := range tp.TypesInfo.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath || !inTest(id.Pos()) {
			continue
		}
		if key, _, ok := funcKey(fn); ok {
			if _, counted := marked[key]; counted {
				marked[key] = true
			}
		}
	}

	scope := scopeOf(tp, pkgPath)
	if scope == nil {
		return
	}
	// insts holds tp's instantiations of generic types, built on the first
	// dispatch that needs one.
	var insts map[*types.TypeName][]types.Type
	// dispatch marks every method name of p whose receiver type, or one of
	// its instantiations in the test files, implements iface.
	dispatch := func(name string, iface *types.Interface) {
		for _, recv := range recvs[name] {
			key := recv + "." + name
			if marked[key] {
				continue
			}
			tn, ok := scope.Lookup(recv).(*types.TypeName)
			if !ok {
				continue
			}
			if !isGeneric(tn) {
				marked[key] = implements(tn.Type(), iface)
				continue
			}
			if insts == nil {
				insts = instantiations(tp.TypesInfo, inTest)
			}
			marked[key] = slices.ContainsFunc(insts[tn], func(t types.Type) bool { return implements(t, iface) })
		}
	}
	// helpers holds tp's generic test funcs, built on the first dispatch
	// whose interface mentions a type parameter.
	var helpers []genericHelper
	for sel, s := range tp.TypesInfo.Selections {
		if s.Kind() == types.FieldVal || !inTest(sel.Sel.Pos()) {
			continue
		}
		iface, ok := s.Recv().Underlying().(*types.Interface)
		if !ok {
			continue
		}
		name := s.Obj().Name()
		if !mentionsTypeParam(iface) {
			dispatch(name, iface)
			continue
		}
		if helpers == nil {
			helpers = genericHelpers(tp, inTest)
		}
		for _, r := range helperReceivers(helpers, sel.Pos(), s.Recv()) {
			if ri, ok := r.Underlying().(*types.Interface); ok {
				dispatch(name, ri)
				continue
			}
			markMethod(pkgPath, r, name, marked)
		}
	}
}

// markMethod sets marked for the method name of the concrete type t, or of
// *t, when the package at pkgPath declares it and marked counts it.
func markMethod(pkgPath string, t types.Type, name string, marked map[string]bool) {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, name)
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return
	}
	if key, _, ok := funcKey(fn); ok {
		if _, counted := marked[key]; counted {
			marked[key] = true
		}
	}
}

// isGeneric reports whether tn names a generic type, one with type
// parameters.
func isGeneric(tn *types.TypeName) bool {
	n, ok := tn.Type().(*types.Named)
	return ok && n.TypeParams().Len() > 0
}

// instantiations maps each generic type's name to its distinct
// instantiations at positions for which inTest holds, from info's Instances
// and from the types of expressions, which also catch a value of an
// instantiated type returned by a call. A generic type's methods mention
// its type parameters, so only an instantiation can implement an interface
// over concrete types. Non-test files are skipped because a method
// declaration's receiver, such as Box[T] in func (Box[T]) Get(), is itself
// recorded as an instantiation. An instantiation that no test file names or
// holds as an expression's type, such as one hidden behind an interface
// returned by non-test code, is missed.
func instantiations(info *types.Info, inTest func(token.Pos) bool) map[*types.TypeName][]types.Type {
	m := make(map[*types.TypeName][]types.Type)
	add := func(pos token.Pos, t types.Type) {
		n := namedOf(t)
		if n == nil || n.TypeArgs().Len() == 0 || !inTest(pos) {
			return
		}
		tn := n.Origin().Obj()
		if !slices.ContainsFunc(m[tn], func(u types.Type) bool { return types.Identical(u, n) }) {
			m[tn] = append(m[tn], n)
		}
	}
	for id, inst := range info.Instances {
		add(id.Pos(), inst.Type)
	}
	for e, tv := range info.Types {
		add(e.Pos(), tv.Type)
	}
	return m
}

// scopeOf returns the scope of the package at pkgPath as tp sees it: tp's
// own when tp is the in-package test variant, otherwise the scope of the
// variant tp imports. It returns nil when tp does not import it.
func scopeOf(tp *packages.Package, pkgPath string) *types.Scope {
	if tp.Types != nil && tp.Types.Path() == pkgPath {
		return tp.Types.Scope()
	}
	if ip, ok := tp.Imports[pkgPath]; ok && ip.Types != nil {
		return ip.Types.Scope()
	}
	return nil
}

// implements reports whether t or *t implements iface.
func implements(t types.Type, iface *types.Interface) bool {
	return types.Implements(t, iface) || types.Implements(types.NewPointer(t), iface)
}

// funcKey returns the key of fn, its name for a func and
// <receiver type>.<name> for a method, and the receiver type name, "" for a
// func. It reports false for a method whose receiver is not a named type
// after dereferencing, such as a method of an interface literal.
func funcKey(fn *types.Func) (key, recv string, ok bool) {
	sig, isSig := fn.Type().(*types.Signature)
	if !isSig || sig.Recv() == nil {
		return fn.Name(), "", true
	}
	named := namedOf(sig.Recv().Type())
	if named == nil {
		return "", "", false
	}
	recv = named.Obj().Name()
	return recv + "." + fn.Name(), recv, true
}

// hasUntestedDirective reports whether doc holds the //astimate:untested
// directive as one of its lines.
func hasUntestedDirective(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		rest, ok := strings.CutPrefix(c.Text, untestedDirective)
		if ok && (rest == "" || rest[0] == ' ') {
			return true
		}
	}
	return false
}
