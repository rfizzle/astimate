package tests

import (
	"go/types"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
)

// indexSupport adds, for each test-support package of m (SPEC.md 6.4, rule
// d), the references its own non-test files make to its own funcs and
// methods. A test-support package is one that some other package's test
// file refers to and that no other package of the module imports from a
// non-test file: its code runs only under tests, so what it calls is
// exercised by them. Those references carry fromSupport, and its
// interface calls are dispatch refs flagged support, so a rule set can
// leave them out.
func (r *Refs) indexSupport(m *load.Module) {
	// imported holds the paths a non-test package of the module imports, a
	// skipped one's imports included (load.Module.GraphPackages).
	imported := make(map[string]bool)
	for _, gp := range m.GraphPackages() {
		for path, ip := range gp.Pkg.Imports {
			if ip != nil && path != gp.Pkg.PkgPath {
				imported[path] = true
			}
		}
	}
	r.supportPkgs = make(map[string]bool)
	for _, path := range m.Paths {
		p := m.Pkgs[path]
		if imported[path] || !r.referencedByOthers(path) {
			continue
		}
		r.supportPkgs[path] = true
		if p.TypesInfo == nil {
			continue
		}
		files := syntaxFiles(m, p, func(string) bool { return true })
		r.indexFiles(m, path, p, files, true)
	}
}

// Support reports whether the package at pkgPath is a test-support
// package of m (SPEC.md 6.4, rule d), the test-support role of SPEC.md
// 6.5. It builds the index on first use.
func (r *Refs) Support(m *load.Module, pkgPath string) bool {
	r.build(m)
	return r.supportPkgs[pkgPath]
}

// referencedByOthers reports whether a test file of a package other than
// the one at path refers to a func or method of it.
func (r *Refs) referencedByOthers(path string) bool {
	for _, o := range r.direct[path] {
		if o&fromOther != 0 {
			return true
		}
	}
	return false
}

// heldBy returns the interfaces that declare a method called name, are
// declared in a package of m, and whose values some test file, or the code
// of a test-support package, holds: a named interface type that is the
// type of an expression there. The index is built on first use.
func (r *Refs) heldBy(m *load.Module, name string) []dispatchRef {
	r.heldOnce.Do(func() {
		r.held = make(map[string][]dispatchRef)
		for tp, files := range r.testFiles {
			seen := make(map[*types.Named]bool)
			for e, tv := range tp.TypesInfo.Types {
				n, ok := tv.Type.(*types.Named)
				if !ok || seen[n] || !files[m.Fset.File(e.Pos())] {
					continue
				}
				iface, ok := n.Underlying().(*types.Interface)
				if !ok || n.Obj().Pkg() == nil {
					continue
				}
				if _, in := m.Pkgs[n.Obj().Pkg().Path()]; !in {
					continue
				}
				seen[n] = true
				for fn := range iface.Methods() {
					r.held[fn.Name()] = append(r.held[fn.Name()], dispatchRef{tp: tp, under: r.under[tp], iface: iface, support: r.support[tp]})
				}
			}
		}
	})
	return r.held[name]
}
