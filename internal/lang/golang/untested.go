package golang

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// untestedDirective marks an exported function or method as intentionally
// untested when it is a line of the declaration's doc comment, alone or
// followed by a space and a reason.
const untestedDirective = "//astimate:untested"

// untestedCounts is the untested_exports metric of one package.
type untestedCounts struct {
	// untested is the number of exported funcs and methods of the package
	// that no test file references.
	untested int
	// names are the untested funcs and methods, written Func or
	// Type.Method, sorted.
	names []string
	// excluded are the exported funcs and methods carrying the
	// //astimate:untested directive, in the same form, sorted. They are a
	// debug record of what the directive removed from the count.
	excluded []string
}

// untestedExports counts the exported funcs and methods declared in p's
// non-test files that no identifier in any _test.go file of p's in-package
// test variant or external test package refers to (SPEC.md 6.4).
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
func untestedExports(l *loaded, p *packages.Package) untestedCounts {
	var c untestedCounts
	// marked holds every counted key, true once a test refers to it.
	marked := make(map[string]bool)
	// recvs maps a method name to the receiver type names declaring it, for
	// interface dispatch.
	recvs := make(map[string][]string)
	for _, f := range p.Syntax {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || !fd.Name.IsExported() {
				continue
			}
			fn, ok := p.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			key, recv, ok := funcKey(fn)
			if !ok {
				continue
			}
			if hasUntestedDirective(fd.Doc) {
				c.excluded = append(c.excluded, key)
				continue
			}
			marked[key] = false
			if recv != "" {
				recvs[fn.Name()] = append(recvs[fn.Name()], recv)
			}
		}
	}
	for _, tp := range testPackagesFor(l, p) {
		markTestRefs(l, p.PkgPath, tp, marked, recvs)
	}
	for key, ok := range marked {
		if !ok {
			c.names = append(c.names, key)
		}
	}
	slices.Sort(c.names)
	slices.Sort(c.excluded)
	c.untested = len(c.names)
	return c
}

// markTestRefs sets marked[key] for every key of the package at pkgPath
// that an identifier in one of tp's _test.go files refers to, in one pass
// over tp's Uses and one over its Selections. Keys absent from marked are
// ignored.
func markTestRefs(l *loaded, pkgPath string, tp *packages.Package, marked map[string]bool, recvs map[string][]string) {
	files := make(map[*token.File]bool, len(tp.Syntax))
	for _, f := range tp.Syntax {
		if tf := l.fset.File(f.Pos()); tf != nil && strings.HasSuffix(tf.Name(), "_test.go") {
			files[tf] = true
		}
	}
	if len(files) == 0 || tp.TypesInfo == nil {
		return
	}
	inTest := func(pos token.Pos) bool { return files[l.fset.File(pos)] }

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
	for sel, s := range tp.TypesInfo.Selections {
		if s.Kind() == types.FieldVal || !inTest(sel.Sel.Pos()) {
			continue
		}
		iface, ok := s.Recv().Underlying().(*types.Interface)
		if !ok {
			continue
		}
		name := s.Obj().Name()
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
		t = types.Unalias(t)
		if ptr, ok := t.(*types.Pointer); ok {
			t = types.Unalias(ptr.Elem())
		}
		n, ok := t.(*types.Named)
		if !ok || n.TypeArgs().Len() == 0 || !inTest(pos) {
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
	t := types.Unalias(sig.Recv().Type())
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		t = types.Unalias(ptr.Elem())
	}
	named, isNamed := t.(*types.Named)
	if !isNamed {
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
