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
// whichever package declares that interface.
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
			if tn, ok := scope.Lookup(recv).(*types.TypeName); ok && implements(tn.Type(), iface) {
				marked[key] = true
			}
		}
	}
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
