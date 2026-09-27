package typescript

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// scanSource scans src as the file name, a test file when name says so,
// and returns its facts.
func scanSource(t *testing.T, name, src string) *fileFacts {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	ff, err := newScanner(loadOptions{}).scanFile(sourceFile{abs: p, rel: name, pkg: packageOf(name), test: isTestPath(name)})
	if err != nil {
		t.Fatalf("scanning %s: %v", name, err)
	}
	return ff
}

// only returns the single function score of ff.
func only(t *testing.T, ff *fileFacts) funcScore {
	t.Helper()
	if len(ff.funcs) != 1 {
		t.Fatalf("funcs = %+v, want exactly one", ff.funcs)
	}
	return ff.funcs[0]
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
			if got.cognitive != tc.cognitive || got.nesting != tc.nesting {
				t.Errorf("cognitive, nesting = %d, %d, want %d, %d", got.cognitive, got.nesting, tc.cognitive, tc.nesting)
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
	for _, f := range ff.funcs {
		names = append(names, f.name)
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
	if ff.exports != 17 {
		t.Errorf("exports = %d, want 17", ff.exports)
	}
	if ff.exportedTypes != 5 || ff.exportedInterfaces != 1 {
		t.Errorf("exported types, interfaces = %d, %d, want 5, 1", ff.exportedTypes, ff.exportedInterfaces)
	}
	var got []string
	for _, e := range ff.exportedFuncs {
		got = append(got, e.display+"="+e.match)
	}
	want := []string{
		"f=f", "g=g", "h=h", "C.pub=pub", "C.st=st",
		"local=local", "renamed=renamed", "Hidden.run=run",
	}
	if !slices.Equal(got, want) {
		t.Errorf("exported funcs = %q, want %q", got, want)
	}
	wantRe := []reexport{{"./other", 1}, {"./star", 0}, {"./ns", 1}}
	if !slices.Equal(ff.reexports, wantRe) {
		t.Errorf("reexports = %+v, want %+v", ff.reexports, wantRe)
	}
}

func TestGlobalsAndInit(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		globals int
		init    bool
	}{
		{"let and var count", "let a = 1;\nvar b, c;\n", 3, false},
		{"const does not", "const a = 1;\n", 0, false},
		{"underscore excluded", "let _ = 1, d = 2;\n", 1, false},
		{"destructuring binds each name", "let { a, b: [c, d = 1], ...e } = o;\n", 4, false},
		{"exported let", "export let a = 1;\n", 1, false},
		{"let inside a function is local", "function f() { let a = 1; }\n", 0, false},
		{"top-level call", "setup();\n", 0, true},
		{"awaited call", "await load();\n", 0, true},
		{"iife", "(function () {})();\n", 0, true},
		{"assignment is not a call", "x = f();\n", 0, false},
		{"call inside a function", "function f() { g(); }\n", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := scanSource(t, "x.ts", tc.src)
			if ff.globals != tc.globals || ff.hasInit != tc.init {
				t.Errorf("globals, init = %d, %v, want %d, %v", ff.globals, ff.hasInit, tc.globals, tc.init)
			}
		})
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
			if ff.testFuncs != tc.want {
				t.Errorf("test cases = %d, want %d", ff.testFuncs, tc.want)
			}
			if len(ff.funcs) != 0 || len(ff.toks.codes) != 0 {
				t.Errorf("test file recorded functions %v or %d duplication tokens", ff.funcs, len(ff.toks.codes))
			}
		})
	}
}

func TestTestIdentifiers(t *testing.T) {
	ff := scanSource(t, "x.spec.ts", "import { a } from \"./a\";\nconst s = `${b.c}`;\nnew D().e(\"f\");\n")
	for _, id := range []string{"a", "b", "c", "D", "e", "s"} {
		if !ff.idents[id] {
			t.Errorf("identifier %q not recorded; got %v", id, ff.idents)
		}
	}
	if ff.idents["f"] {
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
	if !slices.Equal(ff.imports, want) {
		t.Errorf("imports = %q, want %q", ff.imports, want)
	}
}

func TestSLOC(t *testing.T) {
	src := "// comment\n\nconst a = 1; // trailing\n/* block\n   still */ const b = `x\n\n  y`;\n  /** doc */\n"
	ff := scanSource(t, "x.ts", src)
	// Line 3 (code with a trailing comment), 5 (code after a block
	// comment) and 7 (template text) count; line 6 is blank inside the
	// template and line 8 a comment.
	if ff.sloc != 3 {
		t.Errorf("sloc = %d, want 3", ff.sloc)
	}
	wantLines := []int{3, 5, 7}
	var got []int
	for ln, ok := range ff.codeLines {
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
	lit, id := literalCode, identCode
	codes := ff.toks.codes
	if len(codes) != 20 {
		t.Fatalf("tokens = %v, want 20", codes)
	}
	for i, want := range map[int]int32{0: id, 2: id, 5: lit, 7: lit, 9: lit, 11: lit, 13: lit, 15: lit, 17: id} {
		if codes[i] != want {
			t.Errorf("token %d = %d, want %d", i, codes[i], want)
		}
	}
	signs := 0
	for _, c := range ff.toks.class {
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ff := scanSource(t, "x.ts", tc.src)
			var got []string
			for _, e := range ff.exportedFuncs {
				got = append(got, e.display+"="+strconv.FormatBool(e.directed))
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
			if len(ff.funcs) != 1 || len(ff.imports) != 1 || ff.sloc != 2 {
				t.Errorf("funcs, imports, sloc = %d, %q, %d, want 1, one import, 2", len(ff.funcs), ff.imports, ff.sloc)
			}
		})
	}
}

// BenchmarkScanFile measures the per-file parse and walk on the fixture's
// largest file.
func BenchmarkScanFile(b *testing.B) {
	rel := "dupes/dupes.ts"
	f := sourceFile{abs: filepath.Join(fixtureRoot(b), filepath.FromSlash(rel)), rel: rel, pkg: "dupes"}
	s := newScanner(loadOptions{})
	for b.Loop() {
		if _, err := s.scanFile(f); err != nil {
			b.Fatal(err)
		}
	}
}
