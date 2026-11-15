package extract

import (
	"github.com/rfizzle/astimate/internal/metrics/inventory"
	"github.com/rfizzle/astimate/internal/metrics/role"
	"golang.org/x/tools/go/packages"
)

// roleOf returns the role of p in m (SPEC.md 6.5), the first that holds:
// main for a package named main, generated-only when p has non-test files
// and every one is generated, test-support when only tests use it (the
// test reference index's rule d of SPEC.md 6.4), else library.
func roleOf(m *Module, p *packages.Package) string {
	switch {
	case p.Name == "main":
		return role.Main
	case len(m.SourceSyntax(p)) > 0 && len(m.AuthoredSyntax(p)) == 0:
		return role.GeneratedOnly
	case m.testRefs.Support(m.Module, p.PkgPath):
		return role.TestSupport
	}
	return role.Library
}

// BlockRoles returns the role of every package an occurrence of blocks
// lies in, by import path, as Assemble gives it; nil when blocks is empty.
func BlockRoles(m *Module, blocks []inventory.CrossBlock) map[string]string {
	if len(blocks) == 0 {
		return nil
	}
	roles := make(map[string]string)
	for i := range blocks {
		for _, o := range blocks[i].Occurrences {
			if _, done := roles[o.Package]; done {
				continue
			}
			if p, ok := m.Pkgs[o.Package]; ok {
				roles[o.Package] = roleOf(m, p)
			}
		}
	}
	return roles
}
