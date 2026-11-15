package typescript

import (
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// TestDescendantImports checks internal_imports_descendants for
// TypeScript (SPEC.md 13.1): the internal imports below the package's own
// directory, every other package for the root package, and
// internal_imports_private null, since TypeScript has no directory a
// parent alone may import.
func TestDescendantImports(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":          `{}`,
		"index.ts":              "import { b } from \"./a/b/b\";\nimport { l } from \"./lib/lib\";\nexport const r = b + l;\n",
		"a/a.ts":                "import { b } from \"./b/b\";\nimport { c } from \"./internal/c/c\";\nimport { l } from \"../lib/lib\";\nimport { ab } from \"../ab/ab\";\nexport const a = b + c + l + ab;\n",
		"a/b/b.ts":              "export const b = 1;\n",
		"a/internal/c/c.ts":     "export const c = 2;\n",
		"ab/ab.ts":              "export const ab = 3;\n",
		"lib/lib.ts":            "export const l = 4;\n",
		"lib/deep/inner/in.ts":  "import { l } from \"../../lib\";\nexport const i = l;\n",
		"lib/deep/inner/use.ts": "import { b } from \"../../../a/b/b\";\nexport const u = b;\n",
	})
	e := New()
	mod := &metrics.ModuleContext{Root: root}
	for _, tc := range []struct {
		pkg                   string
		internal, descendants int
	}{
		{".", 2, 2},
		{"a", 4, 2},
		{"lib", 0, 0},
		{"lib/deep/inner", 2, 0},
	} {
		t.Run(tc.pkg, func(t *testing.T) {
			m, err := e.Extract(t.Context(), mod, tc.pkg)
			if err != nil {
				t.Fatalf("Extract(%s): %v", tc.pkg, err)
			}
			if m.InternalImportsDescendants == nil {
				t.Fatal("internal_imports_descendants is null, want a count")
			}
			if m.InternalImports != tc.internal || *m.InternalImportsDescendants != tc.descendants {
				t.Errorf("internal_imports/descendants = %d/%d, want %d/%d",
					m.InternalImports, *m.InternalImportsDescendants, tc.internal, tc.descendants)
			}
			if m.InternalImportsPrivate != nil {
				t.Errorf("internal_imports_private = %d, want null", *m.InternalImportsPrivate)
			}
		})
	}
}
