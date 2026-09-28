package inspect

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/typescript/internal/resolve"
	"github.com/rfizzle/astimate/internal/lang/typescript/internal/walk"
)

// scanSource scans src as the file name, a test file when name says so,
// and returns its facts.
func scanSource(t *testing.T, name, src string) *Facts {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	ff, err := NewScanner(Options{}).Scan(p, name, resolve.IsTestPath(name))
	if err != nil {
		t.Fatalf("scanning %s: %v", name, err)
	}
	return ff
}

// only returns the single function score of ff.
func only(t *testing.T, ff *Facts) walk.Func {
	t.Helper()
	if len(ff.Funcs) != 1 {
		t.Fatalf("funcs = %+v, want exactly one", ff.Funcs)
	}
	return ff.Funcs[0]
}

func TestCognitive(t *testing.T) {
	cases := []struct {
		name, body         string
		cognitive, nesting int
	}{
		{"empty", "", 0, 0},
		{"if", "if (a) { x(); }", 1, 1},
		{"if else", "if (a) { x(); } else { y(); }", 2, 1},
		{"else without block", "if (a) x(); else y();", 2, 1},
		{"else if chain", "if (a) {} else if (b) {} else if (c) {} else {}", 4, 3},
		{"nested if", "if (a) { if (b) { if (c) {} } }", 6, 3},
		{"if inside else is nested at the if's level", "if (a) {} else { if (b) {} }", 3, 2},
		{"loops", "for (;;) {} for (const k of v) {} for (const k in o) {} while (a) {} do {} while (a);", 5, 1},
		{"switch", "switch (a) { case 1: if (b) {} break; }", 3, 2},
		{"try catch", "try { a(); } catch (e) { if (b) {} } finally { c(); }", 3, 2},
		{"ternary", "const v = a ? (b ? 1 : 2) : 3;", 3, 0},
		{"one logical sequence", "if (a && b && c) {}", 2, 1},
		{"mixed logical sequences", "if (a && b || c && d) {}", 4, 1},
		{"parentheses start a new sequence", "if (a && (b && c)) {}", 3, 1},
		{"nullish", "const v = a ?? b ?? c;", 1, 0},
		{"comparisons are not logical", "const v = a < b;", 0, 0},
		{"labeled continue", "outer: for (;;) { for (;;) { continue outer; } }", 4, 2},
		{"unlabeled break", "for (;;) { break; }", 1, 1},
		{"nested arrow nests", "const f = () => { if (a) {} };", 2, 2},
		{"nested function expression", "run(function () { for (;;) {} });", 2, 2},
		{"recursion is not counted", "return f(n - 1);", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := scanSource(t, "x.ts", "function f(n: number) {\n"+tc.body+"\n}\n")
			got := only(t, ff)
			if got.Cognitive != tc.cognitive || got.Nesting != tc.nesting {
				t.Errorf("cognitive, nesting = %d, %d, want %d, %d", got.Cognitive, got.Nesting, tc.cognitive, tc.nesting)
			}
		})
	}
}

func TestFunctions(t *testing.T) {
	src := `function a() {}
function* gen() {}
const b = () => 1;
let c = function () {};
const notFn = 1;
export default function () { if (x) {} }
export class K {
  constructor() {}
  m() {}
  get g() { return 1; }
  f = () => 2;
  field = 3;
  abstractLike(): void;
}
if (top) { const inner = () => 1; }
function overload(a: string): void;
function overload(a: any) {}
`
	ff := scanSource(t, "x.ts", src)
	var names []string
	for _, f := range ff.Funcs {
		name := f.Ident
		if f.Receiver != "" {
			name = f.Receiver + "." + name
		}
		names = append(names, name)
	}
	want := []string{"a", "gen", "b", "c", "default", "K.constructor", "K.m", "K.g", "K.f", "overload"}
	if !slices.Equal(names, want) {
		t.Errorf("functions = %q, want %q", names, want)
	}
}

func TestExportsAndUntestedCandidates(t *testing.T) {
	src := `function local() {}
const arrow = () => 1;
class Hidden { run() {} }
export function f() {}
export async function g() {}
export const h = () => 1, value = 2;
export let { p, q: [r, s] } = obj;
export class C { pub() {} private priv() {} protected prot() {} #hash() {} static st() {} get acc() { return 1; } constructor() {} }
export abstract class A {}
export interface I {}
export type T = string;
export enum E { X }
export namespace N {}
export { local, arrow as renamed, Hidden };
export { fromElsewhere } from "./other";
export * from "./star";
export * as ns from "./ns";
export default local;
export function over(a: string): void;
`
	ff := scanSource(t, "x.ts", src)
	// f g h value p r s C A I T E N local renamed Hidden default; the
	// overload signature and the re-exports are not counted here.
	if ff.Exports != 17 {
		t.Errorf("exports = %d, want 17", ff.Exports)
	}
	if ff.ExportedTypes != 5 || ff.ExportedInterfaces != 1 {
		t.Errorf("exported types, interfaces = %d, %d, want 5, 1", ff.ExportedTypes, ff.ExportedInterfaces)
	}
	var got []string
	for _, e := range ff.ExportedFuncs {
		got = append(got, e.Display+"="+e.Match+"@"+strconv.Itoa(e.Line))
	}
	// Each candidate carries the line of its declaration, not of the
	// export list naming it.
	want := []string{
		"f=f@4", "g=g@5", "h=h@6", "C.pub=pub@8", "C.st=st@8",
		"local=local@1", "renamed=renamed@2", "Hidden.run=run@3",
	}
	if !slices.Equal(got, want) {
		t.Errorf("exported funcs = %q, want %q", got, want)
	}
	wantRe := []Reexport{{"./other", 1}, {"./star", 0}, {"./ns", 1}}
	if !slices.Equal(ff.Reexports, wantRe) {
		t.Errorf("reexports = %+v, want %+v", ff.Reexports, wantRe)
	}
}

func TestGlobalsAndInit(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		globals []int
		init    bool
	}{
		{"let and var count", "let a = 1;\nvar b, c;\n", []int{1, 2, 2}, false},
		{"const does not", "const a = 1;\n", nil, false},
		{"underscore excluded", "let _ = 1, d = 2;\n", []int{1}, false},
		{"destructuring binds each name", "let { a, b: [c, d = 1], ...e } = o;\n", []int{1, 1, 1, 1}, false},
		{"binding line", "let {\n  a,\n  b,\n} = o;\n", []int{2, 3}, false},
		{"exported let", "\nexport let a = 1;\n", []int{2}, false},
		{"let inside a function is local", "function f() { let a = 1; }\n", nil, false},
		{"top-level call", "setup();\n", nil, true},
		{"awaited call", "await load();\n", nil, true},
		{"iife", "(function () {})();\n", nil, true},
		{"assignment is not a call", "x = f();\n", nil, false},
		{"call inside a function", "function f() { g(); }\n", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := scanSource(t, "x.ts", tc.src)
			if !slices.Equal(ff.Globals, tc.globals) || ff.HasInit != tc.init {
				t.Errorf("global lines, init = %v, %v, want %v, %v", ff.Globals, ff.HasInit, tc.globals, tc.init)
			}
			if len(ff.GlobalNames) != len(ff.Globals) {
				t.Errorf("global names %q for %d globals, want one per global", ff.GlobalNames, len(ff.Globals))
			}
		})
	}
}

func TestGlobalNames(t *testing.T) {
	ff := scanSource(t, "x.ts", "let a = 1, _ = 2;\nvar { b, c: [d, e = 1], ...f } = o;\nexport let g = 3;\nconst h = 4;\n")
	if want := []string{"a", "b", "d", "e", "f", "g"}; !slices.Equal(ff.GlobalNames, want) {
		t.Errorf("global names = %q, want %q", ff.GlobalNames, want)
	}
}

func TestTestCases(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"it test bench", `it("a", () => {}); test("b", () => {}); bench("c", () => {});`, 3},
		{"template name", "it(`a`, () => {});", 1},
		{"non-string name", "it(name, () => {});", 0},
		{"each counts once", `it.each([1, 2])("a %s", () => {}); test.each` + "`x`" + `("b", () => {});`, 2},
		{"only and skip", `it.only("a", () => {}); test.skip("b", () => {});`, 2},
		{"describe nesting", `describe("s", () => { it("a", () => {}); describe("t", function () { test("b", () => {}); }); });`, 2},
		{"describe each", `describe.each([1])("s %s", () => { it("a", () => {}); });`, 1},
		{"it inside it is not counted", `it("a", () => { it("b", () => {}); });`, 1},
		{"other calls", `expect(1).toBe(1); beforeEach(() => {});`, 0},
		{"it inside a function", `function helper() { it("a", () => {}); }`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := scanSource(t, "x.test.ts", tc.src+"\n")
			if ff.TestFuncs != tc.want {
				t.Errorf("test cases = %d, want %d", ff.TestFuncs, tc.want)
			}
			if len(ff.Funcs) != 0 || len(ff.Tokens.Codes) != 0 {
				t.Errorf("test file recorded functions %v or %d duplication tokens", ff.Funcs, len(ff.Tokens.Codes))
			}
		})
	}
}

func TestTestIdentifiers(t *testing.T) {
	ff := scanSource(t, "x.spec.ts", "import { a } from \"./a\";\nconst s = `${b.c}`;\nnew D().e(\"f\");\n")
	for _, id := range []string{"a", "b", "c", "D", "e", "s"} {
		if !ff.Idents[id] {
			t.Errorf("identifier %q not recorded; got %v", id, ff.Idents)
		}
	}
	if ff.Idents["f"] {
		t.Error("string contents recorded as an identifier")
	}
}

func TestImportsCollected(t *testing.T) {
	src := `import a from "./a";
import type { B } from "b";
import "side-effect";
import c = require("c");
export { d } from "./d";
export * from "e";
const f = require("f");
const g = await import("g");
const h = require(name);
function lazy() { return import("./lazy"); }
`
	ff := scanSource(t, "x.ts", src)
	want := []string{"./a", "b", "side-effect", "c", "./d", "e", "f", "g", "./lazy"}
	if !slices.Equal(ff.Imports, want) {
		t.Errorf("imports = %q, want %q", ff.Imports, want)
	}
}

func TestSLOC(t *testing.T) {
	src := "// comment\n\nconst a = 1; // trailing\n/* block\n   still */ const b = `x\n\n  y`;\n  /** doc */\n"
	ff := scanSource(t, "x.ts", src)
	// Line 3 (code with a trailing comment), 5 (code after a block
	// comment) and 7 (template text) count; line 6 is blank inside the
	// template and line 8 a comment.
	if ff.SLOC != 3 {
		t.Errorf("sloc = %d, want 3", ff.SLOC)
	}
	wantLines := []int{3, 5, 7}
	var got []int
	for ln, ok := range ff.CodeLines {
		if ok {
			got = append(got, ln)
		}
	}
	if !slices.Equal(got, wantLines) {
		t.Errorf("code lines = %v, want %v", got, wantLines)
	}
}

func TestDuplicationTokens(t *testing.T) {
	ff := scanSource(t, "x.ts", "f(foo, -1, `a${b}`, \"s\", /r/g, true, null, undefined);\n")
	// f ( ID , - LIT , LIT , LIT , LIT , LIT , LIT , ID ) ;
	lit, id := walk.LiteralCode, walk.IdentCode
	codes := ff.Tokens.Codes
	if len(codes) != 20 {
		t.Fatalf("tokens = %v, want 20", codes)
	}
	for i, want := range map[int]int32{0: id, 2: id, 5: lit, 7: lit, 9: lit, 11: lit, 13: lit, 15: lit, 17: id} {
		if codes[i] != want {
			t.Errorf("token %d = %d, want %d", i, codes[i], want)
		}
	}
	signs := 0
	for _, c := range ff.Tokens.Class {
		if c == 3 { // duptok.Sign
			signs++
		}
	}
	if signs != 1 {
		t.Errorf("sign tokens = %d, want 1", signs)
	}
}

func TestUntestedDirective(t *testing.T) {
	cases := []struct {
		name, src string
		// want lists each candidate as display=directed.
		want []string
	}{
		{"alone", "//astimate:untested\nexport function f() {}\n", []string{"f=true"}},
		{"with a reason", "//astimate:untested wraps g\nexport function f() {}\n", []string{"f=true"}},
		{"second line of the doc comment", "// f wraps g.\n//astimate:untested\nexport function f() {}\n", []string{"f=true"}},
		{"first line of the doc comment", "//astimate:untested\n// f wraps g.\nexport function f() {}\n", []string{"f=true"}},
		{"space after the slashes", "// astimate:untested\nexport function f() {}\n", []string{"f=false"}},
		{"longer word", "//astimate:untestedness\nexport function f() {}\n", []string{"f=false"}},
		{"inside a line", "// see astimate:untested\nexport function f() {}\n", []string{"f=false"}},
		{"block comment", "/* //astimate:untested */\nexport function f() {}\n", []string{"f=false"}},
		{"jsdoc", "/**\n * astimate:untested\n */\nexport function f() {}\n", []string{"f=false"}},
		{"blank line before the declaration", "//astimate:untested\n\nexport function f() {}\n", []string{"f=false"}},
		{"blank line inside the comment run", "//astimate:untested\n\n// f.\nexport function f() {}\n", []string{"f=false"}},
		{"trailing comment of the line above", "const x = 1; //astimate:untested\nexport function f() {}\n", []string{"f=false"}},
		{"crlf line ending", "//astimate:untested\r\nexport function f() {}\r\n", []string{"f=true"}},
		{"arrow constant", "//astimate:untested\nexport const f = () => 1, g = () => 2;\n", []string{"f=true", "g=true"}},
		{"default function", "//astimate:untested\nexport default function f() {}\n", []string{"f=true"}},
		{"export list", "//astimate:untested\nfunction f() {}\nfunction g() {}\nexport { f, g as h };\n", []string{"f=true", "h=false"}},
		{"export list comment does not apply", "function f() {}\n//astimate:untested\nexport { f };\n", []string{"f=false"}},
		{"public method", "export class C {\n  //astimate:untested\n  m() {}\n  n() {}\n}\n", []string{"C.m=true", "C.n=false"}},
		{"class comment does not apply to methods", "//astimate:untested\nexport class C {\n  m() {}\n}\n", []string{"C.m=false"}},
		{"method of a class exported by list", "class C {\n  //astimate:untested\n  m() {}\n}\nexport { C };\n", []string{"C.m=true"}},
		{"first overload", "//astimate:untested\nexport function f(a: string): string;\nexport function f(a: number): number;\nexport function f(a: any): any { return a; }\n", []string{"f=true"}},
		{"middle overload", "export function f(a: string): string;\n//astimate:untested\nexport function f(a: number): number;\nexport function f(a: any): any { return a; }\n", []string{"f=true"}},
		{"implementation after overloads", "export function f(a: string): string;\nexport function f(a: number): number;\n//astimate:untested\nexport function f(a: any): any { return a; }\n", []string{"f=true"}},
		{"undirected overloads", "// f converts a.\nexport function f(a: string): string;\nexport function f(a: any): any { return a; }\n", []string{"f=false"}},
		{"overload of another function", "//astimate:untested\nexport function g(): void;\nexport function g() {}\nexport function f(): void;\nexport function f() {}\n", []string{"g=true", "f=false"}},
		{"overloads exported by list", "//astimate:untested\nfunction f(a: string): string;\nfunction f(a: any): any { return a; }\nexport { f };\n", []string{"f=true"}},
		{"default overload", "//astimate:untested\nexport default function f(a: string): string;\nexport default function f(a: any): any { return a; }\n", []string{"f=true"}},
		{"method overload", "export class C {\n  //astimate:untested\n  m(a: string): string;\n  m(a: number): number;\n  m(a: any): any { return a; }\n  n(): void;\n  n() {}\n}\n", []string{"C.m=true", "C.n=false"}},
		{"middle method overload", "export class C {\n  m(a: string): string;\n  //astimate:untested\n  m(a: number): number;\n  m(a: any): any { return a; }\n}\n", []string{"C.m=true"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := scanSource(t, "x.ts", tc.src)
			var got []string
			for _, e := range ff.ExportedFuncs {
				got = append(got, e.Display+"="+strconv.FormatBool(e.Directed))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("candidates = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModuleExtensions(t *testing.T) {
	cases := []struct{ name, src string }{
		{"x.mts", "import { a } from \"./a.mjs\";\nexport function f(): number { return a; }\n"},
		{"x.cts", "import a = require(\"./a.cjs\");\nexport function f(): number { return a; }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := scanSource(t, tc.name, tc.src)
			if len(ff.Funcs) != 1 || len(ff.Imports) != 1 || ff.SLOC != 2 {
				t.Errorf("funcs, imports, sloc = %d, %q, %d, want 1, one import, 2", len(ff.Funcs), ff.Imports, ff.SLOC)
			}
		})
	}
}

// BenchmarkScanFile measures the per-file parse and walk on the fixture's
// largest file.
func BenchmarkScanFile(b *testing.B) {
	rel := "dupes/dupes.ts"
	abs := filepath.Join(fixtureRoot(b), filepath.FromSlash(rel))
	s := NewScanner(Options{})
	for b.Loop() {
		if _, err := s.Scan(abs, rel, false); err != nil {
			b.Fatal(err)
		}
	}
}

// fixtureRoot returns the absolute path of testdata/ts/fixture, relative to
// this package's directory, where go test runs.
func fixtureRoot(tb testing.TB) string {
	tb.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "testdata", "ts", "fixture"))
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, resolve.ManifestName)); err != nil {
		tb.Fatalf("TypeScript fixture missing: %v", err)
	}
	return root
}
