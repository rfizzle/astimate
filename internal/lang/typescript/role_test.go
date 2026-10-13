package typescript

import (
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/role"
)

// TestRoles checks SPEC.md 6.5 for TypeScript: a package a bin entry of
// package.json names, or one that exports nothing, is main; any other is
// a library.
func TestRoles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":     `{"bin": {"tool": "./cli/index.ts"}}`,
		"cli/index.ts":     "import { run } from \"../lib/lib\";\nexport const version = 1;\nrun();\n",
		"scripts/build.ts": "import { run } from \"../lib/lib\";\nrun();\n",
		"lib/lib.ts":       "export function run(): number { return 1; }\n",
	})
	e := New()
	mod := &metrics.ModuleContext{Root: root}
	for pkg, want := range map[string]string{"cli": role.Main, "scripts": role.Main, "lib": role.Library} {
		m, err := e.Extract(t.Context(), mod, pkg)
		if err != nil {
			t.Fatalf("Extract(%s): %v", pkg, err)
		}
		if m.Role != want {
			t.Errorf("%s role = %q, want %q", pkg, m.Role, want)
		}
	}
}
