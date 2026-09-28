package tests

import (
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"slices"

	"golang.org/x/tools/go/packages"
)

// maxHelperDepth bounds the rounds that carry instantiations from one
// generic helper into the helpers it instantiates, so that a chain of that
// many nested helpers resolves and a recursive one stops.
const maxHelperDepth = 8

// genericHelper is a generic func declared in a test file, or a method of a
// generic type declared in one, with the distinct concrete type-argument
// lists the test files instantiate it with.
type genericHelper struct {
	// body is the func's body, where a selection may mention its type
	// parameters.
	body *ast.BlockStmt
	// tparams are the func's type parameters, or the method's receiver type
	// parameters.
	tparams *types.TypeParamList
	// args holds one concrete type-argument list per distinct
	// instantiation.
	args [][]types.Type
}

// addArgs appends args to h's instantiations unless an identical list is
// already there, and reports whether it did.
func (h *genericHelper) addArgs(args []types.Type) bool {
	if slices.ContainsFunc(h.args, func(a []types.Type) bool { return slices.EqualFunc(a, args, types.Identical) }) {
		return false
	}
	h.args = append(h.args, args)
	return true
}

// helperUse is one instantiation, at pos in a test file, of the helpers at
// the given indexes: a generic func, or every method of a generic type.
// Its type arguments may mention the type parameters of the helper whose
// body holds pos.
type helperUse struct {
	pos     token.Pos
	helpers []int
	args    []types.Type
	// from is the index of the helper whose body holds pos, set only for a
	// use whose type arguments mention a type parameter.
	from int
}

// genericHelpers returns the generic funcs declared in tp's test files and
// the methods of the generic types declared there, with their
// instantiations in the test files: a func's from tp's Instances, and a
// method's from the instantiations of its receiver type, in Instances and
// in the types of expressions, mapped onto its receiver type parameters by
// position. An instantiation made inside another helper with that helper's
// type parameters, such as get[T](g) inside outer[T], takes each of the
// enclosing helper's instantiations with its arguments substituted, through
// chains of up to maxHelperDepth helpers. It returns a non-nil slice so
// that callers can cache an empty result.
func genericHelpers(tp *packages.Package, inTest func(token.Pos) bool) []genericHelper {
	hs := []genericHelper{}
	funcs := make(map[*types.Func]int)
	methods := make(map[*types.TypeName][]int)
	for _, f := range tp.Syntax {
		if !inTest(f.Pos()) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn, ok := tp.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			sig := fn.Signature()
			switch {
			case fd.Recv == nil && sig.TypeParams().Len() > 0:
				funcs[fn] = len(hs)
				hs = append(hs, genericHelper{body: fd.Body, tparams: sig.TypeParams()})
			case sig.RecvTypeParams().Len() > 0:
				if n := namedOf(sig.Recv().Type()); n != nil {
					tn := n.Origin().Obj()
					methods[tn] = append(methods[tn], len(hs))
					hs = append(hs, genericHelper{body: fd.Body, tparams: sig.RecvTypeParams()})
				}
			}
		}
	}
	if len(hs) == 0 {
		return hs
	}
	var uses []helperUse
	addType := func(pos token.Pos, t types.Type) {
		if n := namedOf(t); n != nil && n.TypeArgs().Len() > 0 {
			if is, ok := methods[n.Origin().Obj()]; ok {
				uses = append(uses, helperUse{pos: pos, helpers: is, args: slices.Collect(n.TypeArgs().Types())})
			}
		}
	}
	for id, inst := range tp.TypesInfo.Instances {
		if !inTest(id.Pos()) {
			continue
		}
		if fn, ok := tp.TypesInfo.Uses[id].(*types.Func); ok {
			if i, ok := funcs[fn]; ok {
				uses = append(uses, helperUse{pos: id.Pos(), helpers: []int{i}, args: slices.Collect(inst.TypeArgs.Types())})
			}
			continue
		}
		addType(id.Pos(), inst.Type)
	}
	if len(methods) > 0 {
		for e, tv := range tp.TypesInfo.Types {
			if inTest(e.Pos()) {
				addType(e.Pos(), tv.Type)
			}
		}
	}
	resolveHelperUses(hs, uses)
	return hs
}

// resolveHelperUses adds each use's type arguments to its helpers. A use
// whose arguments mention a type parameter is resolved through the helper
// whose body holds it, substituting each of that helper's instantiations,
// repeated until no helper gains one or for maxHelperDepth rounds. A use
// held by no helper, such as the receiver in a generic method's own
// declaration, names no concrete type and is dropped.
func resolveHelperUses(hs []genericHelper, uses []helperUse) {
	var nested []helperUse
	for _, u := range uses {
		if !slices.ContainsFunc(u.args, mentionsTypeParam) {
			for _, i := range u.helpers {
				hs[i].addArgs(u.args)
			}
			continue
		}
		if u.from = enclosingHelper(hs, u.pos); u.from >= 0 {
			nested = append(nested, u)
		}
	}
	for range maxHelperDepth {
		grew := false
		for _, u := range nested {
			for _, outer := range hs[u.from].args {
				args, ok := substituteArgs(u.args, typeParamMap(hs[u.from].tparams, outer))
				if !ok {
					continue
				}
				for _, i := range u.helpers {
					if hs[i].addArgs(args) {
						grew = true
					}
				}
			}
		}
		if !grew {
			return
		}
	}
}

// substituteArgs returns args with m substituted into each, and false when
// one cannot be substituted or still mentions a type parameter.
func substituteArgs(args []types.Type, m map[*types.TypeParam]types.Type) ([]types.Type, bool) {
	out := make([]types.Type, len(args))
	for i, a := range args {
		if out[i] = substitute(a, m); out[i] == nil || mentionsTypeParam(out[i]) {
			return nil, false
		}
	}
	return out, true
}

// enclosingHelper returns the index of the helper whose body holds pos, or
// -1 when none does.
func enclosingHelper(hs []genericHelper, pos token.Pos) int {
	return slices.IndexFunc(hs, func(h genericHelper) bool { return pos >= h.body.Pos() && pos < h.body.End() })
}

// typeParamMap maps each of tparams to the type argument at its position.
func typeParamMap(tparams *types.TypeParamList, args []types.Type) map[*types.TypeParam]types.Type {
	m := make(map[*types.TypeParam]types.Type, len(args))
	for i, a := range args {
		m[tparams.At(i)] = a
	}
	return m
}

// namedOf returns t, or the element of t when t is a pointer, as a named
// type, or nil when it is not one.
func namedOf(t types.Type) *types.Named {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	n, _ := t.(*types.Named)
	return n
}

// helperReceivers returns recv with the type arguments of each
// instantiation of the helper whose body holds pos substituted for the
// helper's type parameters. It returns nil when no helper holds pos, and
// skips an instantiation for which substitute cannot rebuild recv.
func helperReceivers(hs []genericHelper, pos token.Pos, recv types.Type) []types.Type {
	i := enclosingHelper(hs, pos)
	if i < 0 {
		return nil
	}
	var rs []types.Type
	for _, args := range hs[i].args {
		if r := substitute(recv, typeParamMap(hs[i].tparams, args)); r != nil {
			rs = append(rs, r)
		}
	}
	return rs
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
		return rewrap(t.Elem(), m, types.NewPointer)
	case *types.Slice:
		return rewrap(t.Elem(), m, types.NewSlice)
	case *types.Array:
		return rewrap(t.Elem(), m, func(e types.Type) *types.Array { return types.NewArray(e, t.Len()) })
	case *types.Chan:
		return rewrap(t.Elem(), m, func(e types.Type) *types.Chan { return types.NewChan(t.Dir(), e) })
	case *types.Map:
		k := substitute(t.Key(), m)
		if k == nil {
			return nil
		}
		return rewrap(t.Elem(), m, func(e types.Type) *types.Map { return types.NewMap(k, e) })
	case *types.Signature:
		if sig := substituteSig(t, m); sig != nil {
			return sig
		}
	case *types.Interface:
		methods := make([]*types.Func, 0, t.NumMethods())
		for f := range t.Methods() {
			sig := substituteSig(f.Signature(), m)
			if sig == nil {
				return nil
			}
			methods = append(methods, types.NewFunc(f.Pos(), f.Pkg(), f.Name(), sig))
		}
		return types.NewInterfaceType(methods, nil).Complete()
	}
	return nil
}

// rewrap substitutes elem and rebuilds its container with mk, or returns
// nil when elem cannot be substituted.
func rewrap[T types.Type](elem types.Type, m map[*types.TypeParam]types.Type, mk func(types.Type) T) types.Type {
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
		return anyMentions(t.Params().Variables(), (*types.Var).Type) ||
			anyMentions(t.Results().Variables(), (*types.Var).Type)
	case *types.Interface:
		return anyMentions(t.Methods(), (*types.Func).Type) ||
			slices.ContainsFunc(slices.Collect(t.EmbeddedTypes()), mentionsTypeParam)
	case *types.Struct:
		return anyMentions(t.Fields(), (*types.Var).Type)
	case *types.Union:
		return anyMentions(t.Terms(), (*types.Term).Type)
	}
	return false
}

// anyMentions reports whether the type typeOf gives some element of seq
// mentions a type parameter.
func anyMentions[E any](seq iter.Seq[E], typeOf func(E) types.Type) bool {
	for e := range seq {
		if mentionsTypeParam(typeOf(e)) {
			return true
		}
	}
	return false
}
