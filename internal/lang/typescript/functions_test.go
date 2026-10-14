package typescript

import (
	"errors"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// The extractor lists functions for the changed-function rule.
var _ metrics.FunctionLister = (*Extractor)(nil)

func TestFingerprintStableAcrossLoads(t *testing.T) {
	const fn = "export function f(a: number) { while (a > 0) { a--; } return a; }\n"
	lone, crowded := t.TempDir(), t.TempDir()
	writeTree(t, lone, map[string]string{"package.json": "{}", "p/p.ts": fn})
	writeTree(t, crowded, map[string]string{
		"package.json": "{}",
		"a/a.ts":       "export class C { x = [1, 2]; y?: string; }\nexport function g() { try { throw new Error(); } catch { return; } }\n",
		"p/p.ts":       fn,
	})
	var got []uint64
	for _, root := range []string{lone, crowded} {
		fns, err := New().Functions(t.Context(), &metrics.ModuleContext{Root: root}, "p")
		if err != nil {
			t.Fatal(err)
		}
		if len(fns) != 1 {
			t.Fatalf("functions = %+v, want one", fns)
		}
		got = append(got, fns[0].Fingerprint)
	}
	if got[0] != got[1] {
		t.Errorf("fingerprints = %d and %d, want equal", got[0], got[1])
	}
}

// TestDupBlockAnchorsStableAcrossLoads checks that a duplicate block
// has the same anchors whatever order the load interned its token kinds
// in, so a check can match it against the baseline's blocks.
func TestDupBlockAnchorsStableAcrossLoads(t *testing.T) {
	const fold = "(xs: number[]): number {\n  let total = 0;\n  for (const x of xs) {\n    if (x > 10) {\n" +
		"      total += x * 2;\n    } else {\n      total -= x;\n    }\n  }\n  return total;\n}\n"
	const src = "export function f1" + fold + "const gap = 1;\nexport function f2" + fold
	lone, crowded := t.TempDir(), t.TempDir()
	writeTree(t, lone, map[string]string{"package.json": "{}", "p/p.ts": src})
	writeTree(t, crowded, map[string]string{
		"package.json": "{}",
		"a/a.ts":       "export class C { x = [1, 2]; y?: string; }\nexport function g() { try { throw new Error(); } catch { return; } }\n",
		"p/p.ts":       src,
	})
	var got []uint64
	for _, root := range []string{lone, crowded} {
		d, err := New().Details(t.Context(), &metrics.ModuleContext{Root: root}, "p")
		if err != nil {
			t.Fatal(err)
		}
		if len(d.DupBlocks) != 1 || len(d.DupBlocks[0].Locations) != 2 {
			t.Fatalf("DupBlocks = %+v, want one block occurring twice", d.DupBlocks)
		}
		got = append(got, d.DupBlocks[0].Anchors[0], d.DupBlocks[0].Anchors[1])
	}
	if got[0] != got[2] || got[1] != got[3] {
		t.Errorf("anchors = %x and %x, want equal", got[:2], got[2:])
	}
}

func TestFunctionsList(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json": "{}",
		"p/b.ts": `export function over(a: string): void;
export function over(a: any) {
  if (a) { return; }
}
export default function () {}
`,
		"p/a.ts": `const arrow = (x: number) => (x ? 1 : 2);
export class K {
  m() {}
  f = () => 1;
  abstractLike(): void;
}
`,
		"p/p.test.ts": "function helper() { if (x) {} }\n",
	})
	fns, err := New().Functions(t.Context(), &metrics.ModuleContext{Root: root}, "p")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		receiver, name, file string
		line, cognitive      int
		cyclomatic           int
	}
	got := make([]row, 0, len(fns))
	for _, f := range fns {
		got = append(got, row{f.Receiver, f.Name, f.File, f.Line, f.Cognitive, f.Cyclomatic})
	}
	want := []row{
		{"", "arrow", "a.ts", 1, 1, 2},
		{"K", "m", "a.ts", 3, 0, 1},
		{"K", "f", "a.ts", 4, 0, 1},
		{"", "over", "b.ts", 2, 1, 2},
		{"", "default", "b.ts", 5, 0, 1},
	}
	if !slices.Equal(got, want) {
		t.Errorf("functions =\n%+v\nwant\n%+v", got, want)
	}

	_, err = New().Functions(t.Context(), &metrics.ModuleContext{Root: root}, "nope")
	if !errors.Is(err, metrics.ErrUnknownPackage) {
		t.Errorf("Functions of an unknown package: err = %v, want ErrUnknownPackage", err)
	}
}

// TestFunctionsChanged checks the function-level diff over two versions of
// a package: a comment-only edit changes nothing, a changed body and a new
// function are changed, and a method moved to another class is new.
func TestFunctionsChanged(t *testing.T) {
	list := func(src string) []metrics.FunctionInfo {
		t.Helper()
		root := t.TempDir()
		writeTree(t, root, map[string]string{"package.json": "{}", "p/p.ts": src})
		e := New()
		fns, err := e.Functions(t.Context(), &metrics.ModuleContext{Root: root}, "p")
		if err != nil {
			t.Fatal(err)
		}
		e.Forget(root)
		return fns
	}
	base := list(`export function keep(a: number) { return a > 0 ? a : -a; }
export function edit(a: number) { return a + 1; }
export class A { move(x: number) { if (x) { return 1; } return 2; } }
export class B {}
`)
	head := list(`// keep returns the absolute value.
export function keep(a: number) {
  // unchanged logic, new layout
  return a > 0 ? a : -a;
}
export function edit(a: number) { return a - 1; }
export function added() {}
export class A {}
export class B { move(x: number) { if (x) { return 1; } return 2; } }
`)
	var got []string
	for _, f := range metrics.ChangedFunctions(base, head) {
		got = append(got, f.QualifiedName())
	}
	want := []string{"edit", "added", "B.move"}
	if !slices.Equal(got, want) {
		t.Errorf("changed = %q, want %q", got, want)
	}
}
