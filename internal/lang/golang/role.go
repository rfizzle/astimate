package golang

import (
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/role"
	"golang.org/x/tools/go/packages"
)

// roleOf returns the role of p in l (SPEC.md 6.5), the first that holds:
// main for a package named main, generated-only when p has non-test files
// and every one is generated, test-support when only tests use it (the
// test reference index's rule d of SPEC.md 6.4), else library.
func roleOf(l *loaded, p *packages.Package) string {
	switch {
	case p.Name == "main":
		return role.Main
	case len(l.SourceSyntax(p)) > 0 && len(l.AuthoredSyntax(p)) == 0:
		return role.GeneratedOnly
	case l.testRefs.Support(l.Module, p.PkgPath):
		return role.TestSupport
	}
	return role.Library
}

// blockRoles returns the role of every package an occurrence of blocks
// lies in, by import path; nil when blocks is empty.
func blockRoles(l *loaded, blocks []metrics.CrossBlock) map[string]string {
	if len(blocks) == 0 {
		return nil
	}
	roles := make(map[string]string)
	for i := range blocks {
		for _, o := range blocks[i].Occurrences {
			if _, done := roles[o.Package]; done {
				continue
			}
			if p, ok := l.Pkgs[o.Package]; ok {
				roles[o.Package] = roleOf(l, p)
			}
		}
	}
	return roles
}
