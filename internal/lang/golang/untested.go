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
// When the interface mentions the type parameters of a generic function
// declared in a test file, such as Getter[T] inside a helper
// get[T any](g Getter[T]), the check runs once per instantiation of that
// function in the test files, on the receiver type with the type arguments
// substituted; a receiver that substitutes to a concrete type marks the
// method it selects directly.
//
// Unlike the syntactic metrics, it reads p.Syntax rather than sourceSyntax,
// because p.TypesInfo.Defs is keyed by the identifiers of those trees. For a
// cgo package they are the files cgo generated: the rewritten sources keep
// every declaration and doc comment of the original, and the helper files
// declare nothing exported, so the count matches the source files.
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

// genericHelper is a generic func declared in a test file, with the
// distinct type-argument lists the test files instantiate it with.
type genericHelper struct {
	// body is the func's body, where a selection may mention its type
	// parameters.
	body *ast.BlockStmt
	// tparams are the func's type parameters.
	tparams *types.TypeParamList
	// args holds one concrete type-argument list per distinct
	// instantiation.
	args [][]types.Type
}

// genericHelpers returns the generic funcs declared in tp's test files with
// their instantiations there, from tp's Instances. An instantiation whose
// type arguments still mention a type parameter, such as a call from
// another generic helper, is skipped because it names no concrete type. It
// returns a non-nil slice so that callers can cache an empty result.
func genericHelpers(tp *packages.Package, inTest func(token.Pos) bool) []genericHelper {
	hs := []genericHelper{}
	index := make(map[*types.Func]int)
	for _, f := range tp.Syntax {
		if !inTest(f.Pos()) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Type.TypeParams == nil || fd.Body == nil {
				continue
			}
			fn, ok := tp.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			index[fn] = len(hs)
			hs = append(hs, genericHelper{body: fd.Body, tparams: fn.Signature().TypeParams()})
		}
	}
	if len(hs) == 0 {
		return hs
	}
	for id, inst := range tp.TypesInfo.Instances {
		fn, ok := tp.TypesInfo.Uses[id].(*types.Func)
		if !ok || !inTest(id.Pos()) {
			continue
		}
		i, ok := index[fn]
		if !ok {
			continue
		}
		args := make([]types.Type, inst.TypeArgs.Len())
		for j := range args {
			args[j] = inst.TypeArgs.At(j)
		}
		if slices.ContainsFunc(args, mentionsTypeParam) {
			continue
		}
		if !slices.ContainsFunc(hs[i].args, func(a []types.Type) bool { return slices.EqualFunc(a, args, types.Identical) }) {
			hs[i].args = append(hs[i].args, args)
		}
	}
	return hs
}

// helperReceivers returns recv with the type arguments of each
// instantiation of the helper whose body holds pos substituted for the
// helper's type parameters. It returns nil when no helper holds pos, and
// skips an instantiation for which substitute cannot rebuild recv.
func helperReceivers(hs []genericHelper, pos token.Pos, recv types.Type) []types.Type {
	for _, h := range hs {
		if pos < h.body.Pos() || pos >= h.body.End() {
			continue
		}
		var rs []types.Type
		for _, args := range h.args {
			m := make(map[*types.TypeParam]types.Type, len(args))
			for i, a := range args {
				m[h.tparams.At(i)] = a
			}
			if r := substitute(recv, m); r != nil {
				rs = append(rs, r)
			}
		}
		return rs
	}
	return nil
}

// substitute returns t with each type parameter in m replaced by its
// mapped type. go/types keeps its own substitution unexported, so this one
// rebuilds type parameters, instantiated named types, pointers, slices,
// arrays, maps, channels, signatures and interfaces, and returns nil when
// t mentions a type parameter anywhere else, such as in a struct field, or
// one absent from m.
func substitute(t types.Type, m map[*types.TypeParam]types.Type) types.Type {
	if !mentionsTypeParam(t) {
		return t
	}
	switch t := types.Unalias(t).(type) {
	case *types.TypeParam:
		return m[t]
	case *types.Named:
		args := make([]types.Type, t.TypeArgs().Len())
		for i := range args {
			if args[i] = substitute(t.TypeArgs().At(i), m); args[i] == nil {
				return nil
			}
		}
		inst, err := types.Instantiate(nil, t.Origin(), args, false)
		if err != nil {
			return nil
		}
		return inst
	case *types.Pointer:
		return rewrap(t.Elem(), m, func(e types.Type) types.Type { return types.NewPointer(e) })
	case *types.Slice:
		return rewrap(t.Elem(), m, func(e types.Type) types.Type { return types.NewSlice(e) })
	case *types.Array:
		return rewrap(t.Elem(), m, func(e types.Type) types.Type { return types.NewArray(e, t.Len()) })
	case *types.Chan:
		return rewrap(t.Elem(), m, func(e types.Type) types.Type { return types.NewChan(t.Dir(), e) })
	case *types.Map:
		k := substitute(t.Key(), m)
		if k == nil {
			return nil
		}
		return rewrap(t.Elem(), m, func(e types.Type) types.Type { return types.NewMap(k, e) })
	case *types.Signature:
		if sig := substituteSig(t, m); sig != nil {
			return sig
		}
	case *types.Interface:
		methods := make([]*types.Func, t.NumMethods())
		for i := range methods {
			f := t.Method(i)
			sig := substituteSig(f.Signature(), m)
			if sig == nil {
				return nil
			}
			methods[i] = types.NewFunc(f.Pos(), f.Pkg(), f.Name(), sig)
		}
		return types.NewInterfaceType(methods, nil).Complete()
	}
	return nil
}

// rewrap substitutes elem and rebuilds its container with mk, or returns
// nil when elem cannot be substituted.
func rewrap(elem types.Type, m map[*types.TypeParam]types.Type, mk func(types.Type) types.Type) types.Type {
	if e := substitute(elem, m); e != nil {
		return mk(e)
	}
	return nil
}

// substituteSig returns sig, without receiver or type parameters, with its
// parameter and result types substituted, or nil when one cannot be.
func substituteSig(sig *types.Signature, m map[*types.TypeParam]types.Type) *types.Signature {
	params := substituteTuple(sig.Params(), m)
	results := substituteTuple(sig.Results(), m)
	if params == nil || results == nil {
		return nil
	}
	return types.NewSignatureType(nil, nil, nil, params, results, sig.Variadic())
}

// substituteTuple returns tup with its variables' types substituted, or nil
// when one cannot be.
func substituteTuple(tup *types.Tuple, m map[*types.TypeParam]types.Type) *types.Tuple {
	vars := make([]*types.Var, tup.Len())
	for i := range vars {
		v := tup.At(i)
		t := substitute(v.Type(), m)
		if t == nil {
			return nil
		}
		vars[i] = types.NewParam(v.Pos(), v.Pkg(), v.Name(), t)
	}
	return types.NewTuple(vars...)
}

// mentionsTypeParam reports whether t refers to a type parameter, looking
// through composite types, signatures, interface methods and embeddeds,
// and the type arguments of named types but not their underlying types.
func mentionsTypeParam(t types.Type) bool {
	switch t := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		return slices.ContainsFunc(slices.Collect(t.TypeArgs().Types()), mentionsTypeParam)
	case *types.Map:
		return mentionsTypeParam(t.Key()) || mentionsTypeParam(t.Elem())
	case interface{ Elem() types.Type }: // pointer, slice, array, chan
		return mentionsTypeParam(t.Elem())
	case *types.Signature:
		return tupleMentions(t.Params()) || tupleMentions(t.Results())
	case *types.Interface:
		for m := range t.Methods() {
			if mentionsTypeParam(m.Type()) {
				return true
			}
		}
		return slices.ContainsFunc(slices.Collect(t.EmbeddedTypes()), mentionsTypeParam)
	case *types.Struct:
		for f := range t.Fields() {
			if mentionsTypeParam(f.Type()) {
				return true
			}
		}
	case *types.Union:
		for term := range t.Terms() {
			if mentionsTypeParam(term.Type()) {
				return true
			}
		}
	}
	return false
}

// tupleMentions reports whether a variable of tup has a type that mentions
// a type parameter.
func tupleMentions(tup *types.Tuple) bool {
	for v := range tup.Variables() {
		if mentionsTypeParam(v.Type()) {
			return true
		}
	}
	return false
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
