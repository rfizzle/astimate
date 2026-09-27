package typescript

import (
	"errors"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// The extractor lists functions for the changed-function rule.
var _ metrics.FunctionLister = (*Extractor)(nil)

// fingerprintOf scans src, which declares exactly one function, and returns
// that function's fingerprint.
func fingerprintOf(t *testing.T, src string) uint64 {
	t.Helper()
	return only(t, scanSource(t, "x.ts", src)).fingerprint
}

func TestFingerprint(t *testing.T) {
	const base = `// sum adds the positive values of xs.
function sum(xs: number[]): number {
  let total = 0;
  for (const v of xs) {
    if (v > 0 && v < 100) {
      total += v; // accumulate
    }
  }
  return total;
}
`
	tests := []struct {
		name string
		src  string
		same bool
	}{
		{name: "identical", src: base, same: true},
		{
			name: "comments only",
			src: `/** sum adds up what is positive. */
function sum(xs: number[]): number {
  /* running sum */
  let total = 0;
  for (const v of xs) {
    // skip negatives
    if (v > 0 && v < 100) {
      total += v;
    }
  }
  return total; // done
}
`,
			same: true,
		},
		{
			name: "layout and automatic semicolons",
			src:  "function sum(xs: number[]): number { let total = 0\n for (const v of xs) { if (v > 0 && v < 100) { total += v } }\n\n\n return total }\n",
			same: true,
		},
		{
			name: "renamed variables and changed literals",
			src: `function sum(values: number[]): number {
  let acc = 0;
  for (const x of values) {
    if (x > 1 && x < 50) {
      acc += x;
    }
  }
  return acc;
}
`,
			same: true,
		},
		{
			name: "signature only",
			src: `function sum(xs: number[], _strict?: boolean): number {
  let total = 0;
  for (const v of xs) {
    if (v > 0 && v < 100) {
      total += v;
    }
  }
  return total;
}
`,
			same: true,
		},
		{
			name: "operator changed",
			src: `function sum(xs: number[]): number {
  let total = 0;
  for (const v of xs) {
    if (v > 0 || v < 100) {
      total += v;
    }
  }
  return total;
}
`,
		},
		{
			name: "assignment operator changed",
			src: `function sum(xs: number[]): number {
  let total = 0;
  for (const v of xs) {
    if (v > 0 && v < 100) {
      total -= v;
    }
  }
  return total;
}
`,
		},
		{
			name: "branch added",
			src: `function sum(xs: number[]): number {
  let total = 0;
  for (const v of xs) {
    if (v > 0 && v < 100) {
      total += v;
    } else {
      total -= 1;
    }
  }
  return total;
}
`,
		},
		{
			name: "loop kind changed",
			src: `function sum(xs: number[]): number {
  let total = 0;
  for (const v in xs) {
    if (v > 0 && v < 100) {
      total += v;
    }
  }
  return total;
}
`,
		},
	}
	want := fingerprintOf(t, base)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fingerprintOf(t, tt.src)
			if (got == want) != tt.same {
				t.Errorf("fingerprint equal to base = %v, want %v", got == want, tt.same)
			}
		})
	}
}

func TestFingerprintDetails(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		same bool
	}{
		{
			name: "self call is distinct from another call",
			a:    "function f(n: number): number { return n <= 1 ? 1 : f(n - 1); }\n",
			b:    "function f(n: number): number { return n <= 1 ? 1 : g(n - 1); }\n",
		},
		{
			name: "calls to two other functions are alike",
			a:    "function f(n: number): number { return h(n - 1); }\n",
			b:    "function f(n: number): number { return g(n - 1); }\n",
			same: true,
		},
		{
			name: "control flow inside a template substitution counts",
			a:    "const f = (a: boolean) => `x${a ? 1 : 2}y`;\n",
			b:    "const f = (a: boolean) => `x${a}y`;\n",
		},
		{
			name: "template text does not count",
			a:    "const f = (a: boolean) => `x${a ? 1 : 2}y`;\n",
			b:    "const f = (a: boolean) => `hello\\n${a ? 1 : 2} there`;\n",
			same: true,
		},
		{
			name: "arrow expression body and block body differ",
			a:    "const f = (a: number) => a + 1;\n",
			b:    "const f = (a: number) => { return a + 1; };\n",
		},
		{
			name: "method body alike in two classes",
			a:    "class A { m(x: number) { if (x) { return 1; } return 2; } }\n",
			b:    "class B { m(y: number) { if (y) { return 3; } return 4; } }\n",
			same: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := fingerprintOf(t, tt.a), fingerprintOf(t, tt.b)
			if (a == b) != tt.same {
				t.Errorf("fingerprints equal = %v, want %v", a == b, tt.same)
			}
		})
	}
}

// TestFingerprintStableAcrossLoads checks that a function's fingerprint
// does not depend on the other files of its module, which change the order
// token kinds are interned in.
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
	}
	got := make([]row, 0, len(fns))
	for _, f := range fns {
		got = append(got, row{f.Receiver, f.Name, f.File, f.Line, f.Cognitive})
	}
	want := []row{
		{"", "arrow", "a.ts", 1, 1},
		{"K", "m", "a.ts", 3, 0},
		{"K", "f", "a.ts", 4, 0},
		{"", "over", "b.ts", 2, 1},
		{"", "default", "b.ts", 5, 0},
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
