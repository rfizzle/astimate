package tests

import (
	"go/token"
	"go/types"
	"strings"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"golang.org/x/tools/go/packages"
)

// Refs is the module-wide test reference index: what the _test.go files of
// every package of one module refer to among the funcs and methods of the
// module's packages (SPEC.md 6.4). It is the Go extractor's answer to
// "referenced from a test anywhere in the module": untested_exports reads
// it, and a later metric that needs the same answer asks it rather than
// walking the test files again.
//
// It is built once per load, on the first Untested, in one pass over the
// types.Info.Uses and Selections of every test package, and shared, like
// the load it belongs to, by every package measured in it. The zero value
// is ready to use; it is safe for concurrent use.
type Refs struct {
	once sync.Once
	// direct maps a package path to the keys (see funcKey) of its funcs and
	// methods that a test file refers to: through types.Info.Uses, or by a
	// selection on a concrete type in a generic test helper.
	direct map[string]map[string]origin
	// dispatch maps a method name to the interfaces test files call a
	// method of that name through, with the test package holding the call.
	dispatch map[string][]dispatchRef
	// testFiles maps each test package in the index to its _test.go files.
	testFiles map[*packages.Package]map[*token.File]bool
	// std holds the standard-library interfaces and errors conventions of
	// the closed list, built with the index.
	std stdInterfaces
	// builds counts completed builds; it is 1 after the first use.
	builds int

	// mu guards insts and reach, filled per test package on first need.
	mu sync.Mutex
	// insts holds each test package's instantiations of generic types (see
	// instantiations).
	insts map[*packages.Package]map[*types.TypeName][]types.Type
	// reach maps each test package to the module packages its type-check
	// sees, by path, for receiver lookups in dispatch.
	reach map[*packages.Package]map[string]*types.Package
}

// origin says whose test files refer to a func or method: the package's
// own, another package's, or both.
type origin uint8

const (
	// fromOwn is a reference from the package's own test files.
	fromOwn origin = 1 << iota
	// fromOther is a reference from another package's test files.
	fromOther
)

// dispatchRef is one interface a test file calls a method through.
type dispatchRef struct {
	// tp is the test package holding the call and under the import path of
	// the package it tests.
	tp    *packages.Package
	under string
	iface *types.Interface
}

// Referenced reports whether a _test.go file of any package of m refers,
// through types.Info.Uses, to the func or method key of the package at
// pkgPath, where key is Func or Type.Method with the receiver's pointer
// and type arguments dropped. Interface dispatch is not included; Untested
// applies it. It builds the index on first use.
func (r *Refs) Referenced(m *load.Module, pkgPath, key string) bool {
	r.build(m)
	return r.direct[pkgPath][key] != 0
}

// build fills the index from every test package of m on its first call and
// does nothing afterwards. Test packages are visited in import-path order,
// so the dispatch lists are deterministic.
func (r *Refs) build(m *load.Module) {
	r.once.Do(func() {
		r.direct = make(map[string]map[string]origin, len(m.Pkgs))
		r.dispatch = make(map[string][]dispatchRef)
		r.testFiles = make(map[*packages.Package]map[*token.File]bool, len(m.Tests)+len(m.XTests))
		r.std = newStdInterfaces()
		for _, path := range m.Paths {
			for _, tp := range m.TestPackages(m.Pkgs[path]) {
				r.index(m, path, tp)
			}
		}
		r.builds++
	})
}

// index adds the references of tp's _test.go files to the index. under is
// the import path of the package tp tests.
func (r *Refs) index(m *load.Module, under string, tp *packages.Package) {
	files := make(map[*token.File]bool, len(tp.Syntax))
	for _, f := range tp.Syntax {
		if tf := m.Fset.File(f.Pos()); tf != nil && strings.HasSuffix(tf.Name(), "_test.go") {
			files[tf] = true
		}
	}
	if len(files) == 0 || tp.TypesInfo == nil {
		return
	}
	r.testFiles[tp] = files
	inTest := func(pos token.Pos) bool { return files[m.Fset.File(pos)] }

	for id, obj := range tp.TypesInfo.Uses {
		if fn, ok := obj.(*types.Func); ok && inTest(id.Pos()) {
			r.addDirect(m, under, fn)
		}
	}

	// seen dedupes tp's dispatch refs by method name and interface.
	type ifaceUse struct {
		name  string
		iface *types.Interface
	}
	seen := make(map[ifaceUse]bool)
	addDispatch := func(name string, iface *types.Interface) {
		if k := (ifaceUse{name, iface}); !seen[k] {
			seen[k] = true
			r.dispatch[name] = append(r.dispatch[name], dispatchRef{tp: tp, under: under, iface: iface})
		}
	}
	// helpers holds tp's generic test funcs, built on the first selection
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
			addDispatch(name, iface)
			continue
		}
		if helpers == nil {
			helpers = genericHelpers(tp, inTest)
		}
		for _, recv := range helperReceivers(helpers, sel.Pos(), s.Recv()) {
			if ri, ok := recv.Underlying().(*types.Interface); ok {
				addDispatch(name, ri)
				continue
			}
			obj, _, _ := types.LookupFieldOrMethod(recv, true, nil, name)
			if fn, ok := obj.(*types.Func); ok {
				r.addDirect(m, under, fn)
			}
		}
	}
}

// addDirect records a reference to fn from a test file of the package at
// under, when fn belongs to a package of m.
func (r *Refs) addDirect(m *load.Module, under string, fn *types.Func) {
	if fn.Pkg() == nil {
		return
	}
	path := fn.Pkg().Path()
	if _, ok := m.Pkgs[path]; !ok {
		return
	}
	key, _, ok := funcKey(fn)
	if !ok {
		return
	}
	o := fromOther
	if path == under {
		o = fromOwn
	}
	keys := r.direct[path]
	if keys == nil {
		keys = make(map[string]origin)
		r.direct[path] = keys
	}
	keys[key] |= o
}

// scopeIn returns the scope of the package at pkgPath as the test package
// tp sees it: tp's own when tp is the in-package test variant, otherwise
// the one reached through tp's imports of packages of m, directly or not.
// It returns nil when tp's type-check does not see that package, so no
// value of its types can reach a call in tp.
func (r *Refs) scopeIn(m *load.Module, tp *packages.Package, pkgPath string) *types.Scope {
	if tp.Types != nil && tp.Types.Path() == pkgPath {
		return tp.Types.Scope()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seen, ok := r.reach[tp]
	if !ok {
		seen = reachable(m, tp)
		if r.reach == nil {
			r.reach = make(map[*packages.Package]map[string]*types.Package)
		}
		r.reach[tp] = seen
	}
	if pkg := seen[pkgPath]; pkg != nil {
		return pkg.Scope()
	}
	return nil
}

// instantiationsOf returns tp's instantiations of generic types in its
// _test.go files, computed on first need.
func (r *Refs) instantiationsOf(m *load.Module, tp *packages.Package) map[*types.TypeName][]types.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	if insts, ok := r.insts[tp]; ok {
		return insts
	}
	files := r.testFiles[tp]
	insts := instantiations(tp.TypesInfo, func(pos token.Pos) bool { return files[m.Fset.File(pos)] })
	if r.insts == nil {
		r.insts = make(map[*packages.Package]map[*types.TypeName][]types.Type)
	}
	r.insts[tp] = insts
	return insts
}

// reachable maps the path of every package of m that tp's type-check
// imports, directly or through other packages of m, to that package as tp
// sees it. It walks the go/packages import graph, so a package recompiled
// for tp's test is the variant tp sees. Packages outside m are not
// followed: they are loaded from export data without their imports, and a
// test binary reaches a package of m through them only when that package
// is imported by one outside the module, which a module's own layout does
// not produce.
func reachable(m *load.Module, tp *packages.Package) map[string]*types.Package {
	seen := make(map[string]*types.Package)
	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		for path, ip := range p.Imports {
			if _, ok := seen[path]; ok || ip == nil {
				continue
			}
			if _, ok := m.Pkgs[path]; !ok {
				continue
			}
			seen[path] = ip.Types
			walk(ip)
		}
	}
	walk(tp)
	return seen
}
