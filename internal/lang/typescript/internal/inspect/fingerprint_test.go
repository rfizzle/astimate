package inspect

import (
	"testing"
)

// fingerprintOf scans src, which declares exactly one function, and returns
// that function's fingerprint.
func fingerprintOf(t *testing.T, src string) uint64 {
	t.Helper()
	return only(t, scanSource(t, "x.ts", src)).Fingerprint
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
			name: "method self call through this is distinct from another method call",
			a:    "class A { m(n: number): number { return n <= 1 ? 1 : this.m(n - 1); } }\n",
			b:    "class A { m(n: number): number { return n <= 1 ? 1 : this.n(n - 1); } }\n",
		},
		{
			name: "renaming a this call target to the method itself",
			a:    "class A { m(n: number) { return this.k(n); } }\n",
			b:    "class A { m(n: number) { return this.m(n); } }\n",
		},
		{
			name: "renaming a this call target away from the method itself",
			a:    "class A { m(n: number) { return this.m(n); } }\n",
			b:    "class A { m(n: number) { return this.j(n); } }\n",
		},
		{
			name: "calls to two other methods through this are alike",
			a:    "class A { m(n: number) { return this.k(n); } }\n",
			b:    "class A { m(n: number) { return this.j(n); } }\n",
			same: true,
		},
		{
			name: "private method self call through this is distinct",
			a:    "class A { #m(n: number) { return this.#m(n); } }\n",
			b:    "class A { #m(n: number) { return this.#k(n); } }\n",
		},
		{
			name: "function-valued field self call through this is distinct",
			a:    "class A { m = (n: number) => this.m(n); }\n",
			b:    "class A { m = (n: number) => this.k(n); }\n",
		},
		{
			name: "super call is not a self call",
			a:    "class A extends B { m(n: number) { return super.m(n); } }\n",
			b:    "class A extends B { m(n: number) { return super.k(n); } }\n",
			same: true,
		},
		{
			name: "call on another object is not a self call",
			a:    "class A { m(o: A) { return o.m(); } }\n",
			b:    "class A { m(o: A) { return o.k(); } }\n",
			same: true,
		},
		{
			name: "this call from a nested arrow function is a self call",
			a:    "class A { m(xs: number[]) { return xs.map((x) => this.m([x])); } }\n",
			b:    "class A { m(xs: number[]) { return xs.map((x) => this.k([x])); } }\n",
		},
		{
			name: "this call from a nested function expression is not a self call",
			a:    "class A { m(xs: number[]) { return xs.map(function (x) { return this.m([x]); }); } }\n",
			b:    "class A { m(xs: number[]) { return xs.map(function (x) { return this.k([x]); }); } }\n",
			same: true,
		},
		{
			name: "this call from a nested class is not a self call",
			a:    "class A { m() { return class { f = this.m(); }; } }\n",
			b:    "class A { m() { return class { f = this.k(); }; } }\n",
			same: true,
		},
		{
			name: "this call in a plain function is not a self call",
			a:    "function m(this: any) { return this.m(); }\n",
			b:    "function m(this: any) { return this.k(); }\n",
			same: true,
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
