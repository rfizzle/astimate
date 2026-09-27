package golang

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/rfizzle/astimate/internal/lang/duptok"
)

// dupToken is one token scan emitted.
type dupToken struct {
	code  int32
	class duptok.Class
}

// dupRecorder is a tokenSink that records the tokens of each file.
type dupRecorder struct {
	toks  []dupToken
	files int
}

func (r *dupRecorder) Add(code int32, class duptok.Class, _, _ int) error {
	r.toks = append(r.toks, dupToken{code, class})
	return nil
}

func (r *dupRecorder) EndFile(string, []bool) { r.files++ }

// dupScan scans one source under opts and returns what it emitted, with
// the tokenizer that interned its text.
func dupScan(t *testing.T, src string, opts dupOptions) ([]dupToken, *dupTokenizer) {
	t.Helper()
	var r dupRecorder
	z := newDupTokenizer(opts)
	if err := z.scan(token.NewFileSet(), "src.go", []byte(src), &r); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if r.files != 1 {
		t.Fatalf("scan closed %d files, want 1", r.files)
	}
	return r.toks, z
}

// dupRender renders the stream codes of one scanned source: ID, LIT, #n
// for the n-th interned text, else the token.
func dupRender(t *testing.T, src string, opts dupOptions) string {
	t.Helper()
	toks, _ := dupScan(t, src, opts)
	out := make([]string, 0, len(toks))
	for _, tk := range toks {
		switch c := tk.code; {
		case c >= dupInternBase:
			out = append(out, "#"+strconv.Itoa(int(c-dupInternBase)))
		case c == dupIdentCode:
			out = append(out, "ID")
		case c == dupLitCode:
			out = append(out, "LIT")
		default:
			out = append(out, token.Token(c).String())
		}
	}
	return strings.Join(out, " ")
}

func TestDupNormalization(t *testing.T) {
	def := defaultDupOptions()
	rawIdents := def
	rawIdents.normalizeIdents = false
	rawLits := def
	rawLits.normalizeLiterals = false
	cases := []struct {
		name string
		src  string
		opts dupOptions
		want string
	}{
		{"identifiers", "a := b.c", def, "ID := ID . ID"},
		{"predeclared", "true nil int", def, "ID ID ID"},
		{"literals", "1 1.5 2i 'c' \"s\" `r`", def, "LIT LIT LIT LIT LIT LIT"},
		{"keywords and operators", "if a && !b { return }", def, "if ID && ! ID { return }"},
		{"comments dropped", "a /* c */ + b // d\n", def, "ID + ID"},
		{"auto semicolons dropped", "a\nb\n", def, "ID ID"},
		{"explicit semicolon kept", "a; b", def, "ID ; ID"},
		{"identifiers interned", "a b a 1", rawIdents, "#0 #1 #0 LIT"},
		{"literals interned", "1 2 1 x \"1\"", rawLits, "#0 #1 #0 ID #2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dupRender(t, tc.src, tc.opts); got != tc.want {
				t.Errorf("stream = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDupClasses(t *testing.T) {
	raw := defaultDupOptions()
	raw.normalizeIdents, raw.normalizeLiterals = false, false
	cases := []struct {
		name string
		src  string
		opts dupOptions
		want string // C code, L literal, P punctuation, S sign
	}{
		{"identifiers and keywords are code", "if a { return b }", defaultDupOptions(), "C C P C C P"},
		{"literals", "1 1.5 2i 'c' \"s\" `r`", defaultDupOptions(), "L L L L L L"},
		{"table punctuation", "( ) [ ] { } , : ;", defaultDupOptions(), "P P P P P P P P P"},
		{"other operators are code", "a + b . c := d", defaultDupOptions(), "C C C C C C C"},
		{"interned identifiers are code", "a b", raw, "C C"},
		{"interned literals are literals", "1 \"s\"", raw, "L L"},
		{"unary sign before a number", "f(-1, +2.5, -'a')", defaultDupOptions(), "C P S L P S L P S L P"},
		{"binary minus", "a - 1", defaultDupOptions(), "C C L"},
		{"sign before an identifier", "-a", defaultDupOptions(), "C C"},
		{"sign at end of file", "a = -", defaultDupOptions(), "C C C"},
	}
	letter := map[duptok.Class]string{duptok.Code: "C", duptok.Literal: "L", duptok.Punct: "P", duptok.Sign: "S"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toks, _ := dupScan(t, tc.src, tc.opts)
			got := make([]string, 0, len(toks))
			for _, tk := range toks {
				got = append(got, letter[tk.class])
			}
			if g := strings.Join(got, " "); g != tc.want {
				t.Errorf("classes = %q, want %q", g, tc.want)
			}
		})
	}
}

// dupOfSources runs the duplication stream over inline sources, one file
// each, skipping generated ones as duplication does, with the SLOC of all
// of them (generated included) as the denominator.
func dupOfSources(t *testing.T, opts dupOptions, srcs ...string) duptok.Result {
	t.Helper()
	var s duptok.Stream
	z := newDupTokenizer(opts)
	fs := token.NewFileSet()
	sloc := 0
	for i, src := range srcs {
		name := "f" + strconv.Itoa(i) + ".go"
		pf := token.NewFileSet()
		f, err := parser.ParseFile(pf, name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		sloc += fileSLOC(pf.File(f.FileStart), f, []byte(src))
		if ast.IsGenerated(f) {
			continue
		}
		if err := z.scan(fs, name, []byte(src), &s); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	res, err := s.Count(opts.finder(), sloc)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	return res
}

// dupCopy is a 45-token function body used by the source tests, before
// identifier and literal renaming.
const dupCopy = `func Sum(xs []int, floor int) int {
	total := 0
	for _, x := range xs {
		if x < floor {
			continue
		}
		total += x * 3
	}
	if total > 100 {
		return 100
	}
	return total
}
`

// dupRenamed is dupCopy with every identifier renamed and every literal
// changed.
const dupRenamed = `func Tally(vals []int, least int) int {
	acc := 7
	for _, v := range vals {
		if v < least {
			continue
		}
		acc += v * 9
	}
	if acc > 250 {
		return 250
	}
	return acc
}
`

func TestDupRenamedCopies(t *testing.T) {
	def := defaultDupOptions()
	same := dupOfSources(t, def, "package p\n\n"+dupCopy+"\nvar sep = 1\n\n"+strings.Replace(dupCopy, "Sum", "Sum2", 1))
	renamed := dupOfSources(t, def, "package p\n\n"+dupCopy+"\nvar sep = 1\n\n"+dupRenamed)
	if same.Blocks != 1 {
		t.Fatalf("identical copies: blocks = %d, want 1", same.Blocks)
	}
	if renamed.Blocks < same.Blocks {
		t.Errorf("renamed copy: blocks = %d, identical copies %d", renamed.Blocks, same.Blocks)
	}
	if renamed.Pct != same.Pct {
		t.Errorf("renamed copy: pct = %v, identical copies %v", renamed.Pct, same.Pct)
	}
	// SLOC 1 + 13 + 1 + 13 = 28, covered 26.
	if same.Pct != 92.9 {
		t.Errorf("pct = %v, want 92.9", same.Pct)
	}
	raw := def
	raw.normalizeIdents, raw.normalizeLiterals = false, false
	if got := dupOfSources(t, raw, "package p\n\n"+dupCopy+"\nvar sep = 1\n\n"+dupRenamed); got.Blocks != 0 {
		t.Errorf("renamed copy without normalization: blocks = %d, want 0", got.Blocks)
	}
}

func TestDupSharedPrefixBelowThreshold(t *testing.T) {
	// The two functions share their first 20 normalized tokens, from func to
	// the second multiplication operand, then diverge.
	src := `package p

func A(a, b int) int {
	c := a + b
	d := a * b
	for i := 0; i < a; i++ {
		d += i
	}
	if c > d {
		return c
	}
	return d
}

func B(x, y int) int {
	u := x + y
	v := x * y
	switch {
	case u > v:
		return u - v
	case u < v:
		return v - u
	}
	return 0
}
`
	if got := dupOfSources(t, defaultDupOptions(), src); got.Blocks != 0 {
		t.Errorf("default threshold: blocks = %d, want 0", got.Blocks)
	}
	opts := defaultDupOptions()
	opts.minTokens = 20
	if got := dupOfSources(t, opts, src); got.Blocks != 1 {
		t.Errorf("minTokens 20: blocks = %d, want 1", got.Blocks)
	}
	opts.minTokens = 21
	if got := dupOfSources(t, opts, src); got.Blocks != 0 {
		t.Errorf("minTokens 21: blocks = %d, want 0", got.Blocks)
	}
}

func TestDupGeneratedExcluded(t *testing.T) {
	def := defaultDupOptions()
	a := "package p\n\n" + dupCopy
	b := "package p\n\n" + dupRenamed
	gen := "// Code generated by tool. DO NOT EDIT.\n\n" + b
	if got := dupOfSources(t, def, a, b); got.Blocks != 1 {
		t.Fatalf("plain files: blocks = %d, want 1", got.Blocks)
	}
	got := dupOfSources(t, def, a, gen)
	if got.Blocks != 0 || got.Pct != 0 {
		t.Errorf("with generated copy: blocks = %d, pct = %v, want 0, 0", got.Blocks, got.Pct)
	}
}

func TestDupCoverageSkipsNonCodeLines(t *testing.T) {
	// A comment line and a blank line inside each copy, and a raw string
	// with a blank interior line, are covered by position but are not SLOC.
	body := `func F(xs []int) string {
	total := 0
	// not code

	for _, x := range xs {
		if x < 3 {
			continue
		}
		total += x * 3
	}
	s := ` + "`" + `a

b` + "`" + `
	if total > 100 {
		return s
	}
	return s + s + s
}
`
	src := "package p\n\n" + body + "\nvar sep = 1\n\n" + strings.Replace(body, "func F", "func G", 1)
	got := dupOfSources(t, defaultDupOptions(), src)
	if got.Blocks != 1 {
		t.Fatalf("blocks = %d, want 1", got.Blocks)
	}
	// Each copy spans 18 lines of which 15 are SLOC. SLOC = 1 + 15 + 1 + 15,
	// covered 30.
	if want := 93.8; got.Pct != want {
		t.Errorf("pct = %v, want %v", got.Pct, want)
	}
	if len(got.Locations) != 2 {
		t.Fatalf("locations = %v, want 2", got.Locations)
	}
	for _, loc := range got.Locations {
		if loc.EndLine-loc.StartLine != 17 {
			t.Errorf("location %v spans %d lines, want 18", loc, loc.EndLine-loc.StartLine+1)
		}
	}
}

func TestDupRejectsNonPositiveMinimum(t *testing.T) {
	l := loadFixture(t)
	p := l.pkgs[l.paths[0]]
	sz, err := size(l, p, osFiles{})
	if err != nil {
		t.Fatal(err)
	}
	opts := defaultDupOptions()
	opts.minTokens = 0
	if _, err := duplication(l, p, osFiles{}, sz, opts); err == nil {
		t.Error("duplication with minTokens 0: no error")
	}
}

func TestDuplicationDupesLocations(t *testing.T) {
	l := loadFixture(t)
	p := l.pkgs["example.com/fixture/dupes"]
	sz, err := size(l, p, osFiles{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := duplication(l, p, osFiles{}, sz, defaultDupOptions())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(fixtureRoot(t), "dupes", "dupes.go")
	want := []duptok.Location{{File: file, StartLine: 9, EndLine: 26}, {File: file, StartLine: 31, EndLine: 48}, {File: file, StartLine: 53, EndLine: 70}}
	if !slices.Equal(got.Locations, want) {
		t.Errorf("locations = %v, want %v", got.Locations, want)
	}
}

func TestDuplicationNetHTTPUnderThreeSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("loads net/http")
	}
	start := time.Now()
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedName,
		Fset: fset,
	}, "net/http")
	if err != nil || len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Skipf("loading net/http: %v", err)
	}
	loadTime := time.Since(start)
	l := &loaded{fset: fset}
	p := pkgs[0]

	begin := time.Now()
	sz, err := size(l, p, osFiles{})
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	got, err := duplication(l, p, osFiles{}, sz, defaultDupOptions())
	if err != nil {
		t.Fatalf("duplication: %v", err)
	}
	elapsed := time.Since(begin)
	t.Logf("net/http: %d files, load %v, size+duplication %v, sloc %d, dup_blocks %d, duplication_pct %v",
		len(p.Syntax), loadTime, elapsed, sz.sloc, got.Blocks, got.Pct)
	if elapsed >= 3*time.Second {
		t.Errorf("net/http duplication took %v, want under 3s", elapsed)
	}
}

func BenchmarkDuplication(b *testing.B) {
	fixture := fixtureRoot(b)
	roots := map[string]string{
		"fixture": fixture,
		"module":  filepath.Dir(filepath.Dir(filepath.Dir(fixture))),
	}
	for _, name := range []string{"fixture", "module"} {
		b.Run(name, func(b *testing.B) {
			l, err := loadModule(&packages.Config{Dir: roots[name]}, packages.Load)
			if err != nil {
				b.Fatalf("loading %s: %v", name, err)
			}
			sizes := make(map[string]sizeCounts, len(l.paths))
			for _, path := range l.paths {
				if sizes[path], err = size(l, l.pkgs[path], osFiles{}); err != nil {
					b.Fatal(err)
				}
			}
			for b.Loop() {
				for _, path := range l.paths {
					if _, err := duplication(l, l.pkgs[path], osFiles{}, sizes[path], defaultDupOptions()); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// TestDupMeasureStdlibLiteralOnly measures dup_blocks and duplication_pct
// over the standard library with dup_ignore_literal_only off and on, for
// calibration/notes/duplication-literal-only.md. It loads every std package,
// so it runs only with ASTIMATE_MEASURE_STDLIB=1.
func TestDupMeasureStdlibLiteralOnly(t *testing.T) {
	if os.Getenv("ASTIMATE_MEASURE_STDLIB") != "1" {
		t.Skip("set ASTIMATE_MEASURE_STDLIB=1 to measure the standard library")
	}
	start := time.Now()
	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedName,
		Fset: fset,
	}, "std")
	if err != nil {
		t.Fatalf("loading std: %v", err)
	}
	l := &loaded{fset: fset}
	var rows []dupMeasureRow
	on := defaultDupOptions()
	on.ignoreLiteralOnly = true
	on.foldSigns = false // as measured, before dup_fold_signs existed
	off := on
	off.ignoreLiteralOnly = false
	for _, p := range pkgs {
		if len(p.Errors) > 0 {
			continue
		}
		sz, err := size(l, p, osFiles{})
		if err != nil {
			t.Fatalf("size %s: %v", p.PkgPath, err)
		}
		if sz.sloc < 200 {
			continue
		}
		a, err := duplication(l, p, osFiles{}, sz, off)
		if err != nil {
			t.Fatal(err)
		}
		b, err := duplication(l, p, osFiles{}, sz, on)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, dupMeasureRow{p.PkgPath, sz.sloc, a.Blocks, b.Blocks, a.Pct, b.Pct})
	}
	offPct := func(r dupMeasureRow) float64 { return r.offPct }
	onPct := func(r dupMeasureRow) float64 { return r.onPct }
	offB := func(r dupMeasureRow) float64 { return float64(r.offB) }
	onB := func(r dupMeasureRow) float64 { return float64(r.onB) }
	t.Logf("packages %d, wall %v", len(rows), time.Since(start))
	for _, q := range []float64{0.5, 0.9, 0.99} {
		t.Logf("p%v: duplication_pct off %v on %v; dup_blocks off %v on %v", q*100,
			dupQuantile(rows, offPct, q), dupQuantile(rows, onPct, q),
			dupQuantile(rows, offB, q), dupQuantile(rows, onB, q))
	}
	totalOff, totalOn, changed := 0, 0, 0
	for _, r := range rows {
		totalOff += r.offB
		totalOn += r.onB
		if r.offB != r.onB {
			changed++
		}
	}
	t.Logf("total dup_blocks off %d on %d; packages changed %d", totalOff, totalOn, changed)
	top := func(title string, get func(dupMeasureRow) float64) {
		t.Log(title)
		s := slices.Clone(rows)
		slices.SortStableFunc(s, func(a, b dupMeasureRow) int { return cmp.Compare(get(b), get(a)) })
		for _, r := range s[:min(10, len(s))] {
			t.Logf("| %s | %d | %d | %d | %v | %v |", r.path, r.sloc, r.offB, r.onB, r.offPct, r.onPct)
		}
	}
	t.Log("named table packages:")
	for _, r := range rows {
		if r.path == "crypto/internal/fips140/nistec" || r.path == "math/big" {
			t.Logf("| %s | %d | %d | %d | %v | %v |", r.path, r.sloc, r.offB, r.onB, r.offPct, r.onPct)
		}
	}
	top("largest drop:", func(r dupMeasureRow) float64 { return r.offPct - r.onPct })
	top("top ten off:", offPct)
	top("top ten on:", onPct)
}

// dupMeasureRow is one standard library package in the literal-only
// measurement.
type dupMeasureRow struct {
	path          string
	sloc          int
	offB, onB     int
	offPct, onPct float64
}

// dupQuantile returns the nearest-rank q quantile of get over rows.
func dupQuantile(rows []dupMeasureRow, get func(dupMeasureRow) float64, q float64) float64 {
	v := make([]float64, len(rows))
	for i, r := range rows {
		v[i] = get(r)
	}
	slices.Sort(v)
	return v[max(0, int(math.Ceil(q*float64(len(v))))-1)]
}

// dupTable returns n copies of elem, each followed by a comma and a space.
func dupTable(elem string, n int) string {
	return strings.Repeat(elem+", ", n)
}

func TestDupLiteralOnly(t *testing.T) {
	on := defaultDupOptions()
	off := on
	off.ignoreLiteralOnly = false
	rawLitsOn := on
	rawLitsOn.normalizeLiterals = false
	rawLitsOff := rawLitsOn
	rawLitsOff.ignoreLiteralOnly = false
	noFold := on
	noFold.foldSigns = false
	rawNoFold := rawLitsOn
	rawNoFold.foldSigns = false
	signed := "package p\n\nvar t = []float64{" + dupTable("-1", 20) + dupTable("+2.5", 10) + dupTable("-'a'", 10) + "}\n"
	rawSigned := "package p\n\nvar t = []int{" + dupTable("-7", 40) + "}\n"
	nums := make([]string, 60)
	for i := range nums {
		nums[i] = strconv.Itoa(i*7919%1000) + ","
	}
	numTable := "package p\n\nvar t = []int{\n" + strings.Join(nums, "\n") + "\n}\n"
	zeros := "package p\n\nvar t = []int{" + dupTable("0", 40) + "}\n"
	mixed := "func F() []int {\n\tx := []int{" + dupTable("1", 30) + "}\n\treturn x\n}\n"
	cases := []struct {
		name string
		src  string
		opts dupOptions
		want int
	}{
		{"numeric table on", numTable, on, 0},
		{"numeric table off", numTable, off, 1},
		{"keyed table on", "package p\n\nvar m = map[string]int{\n" + dupTable(`"k": 1`, 30) + "}\n", on, 0},
		{"nested table on", "package p\n\nvar m = [][2]int{\n" + dupTable("{1, 2}", 30) + "}\n", on, 0},
		{"interned literals on", zeros, rawLitsOn, 0},
		{"interned literals off", zeros, rawLitsOff, 1},
		{"identifier table kept", "package p\n\nvar t = []int{" + dupTable("a", 40) + "}\n", on, 1},
		{"signed table on", signed, on, 0},
		{"signed table without fold signs", signed, noFold, 1},
		{"interned signed literals on", rawSigned, rawLitsOn, 0},
		{"interned signed literals without fold signs", rawSigned, rawNoFold, 1},
		{"binary operator table kept", "package p\n\nvar t = []int{" + dupTable("1-1", 30) + "}\n", on, 1},
		{"sign after bracket is binary", "package p\n\nvar t = []int{" + dupTable("(1)-1", 20) + "}\n", on, 1},
		{"negated identifier table kept", "package p\n\nvar t = []int{" + dupTable("-a", 30) + "}\n", on, 1},
		{"other unary operator kept", "package p\n\nvar t = []int{" + dupTable("^1", 30) + "}\n", on, 1},
		{"mixed block kept", "package p\n\n" + mixed + "\n" + strings.Replace(mixed, "F", "G", 1), on, 1},
		{"code copies kept", "package p\n\n" + dupCopy + "\nvar sep = 1\n\n" + dupRenamed, on, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dupOfSources(t, tc.opts, tc.src)
			if got.Blocks != tc.want {
				t.Errorf("blocks = %d, want %d (locations %v)", got.Blocks, tc.want, got.Locations)
			}
			if tc.want == 0 && got.Pct != 0 {
				t.Errorf("pct = %v, want 0", got.Pct)
			}
		})
	}
}

func TestDupSigns(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string // the source text of each recorded sign and its literal
	}{
		{"leading", "-1", "-1"},
		{"plus and kinds", "f(+2, -3.5, -4i, -'a')", "+2 -3.5 -4i -'a'"},
		{"after operators", "a * -1 + (-2) - -3", "-1 -2 -3"},
		{"binary after operand", "a - 1 + b[0] - 2 + f() - 3", ""},
		{"binary after literal", "1 - 2", ""},
		{"not a numeric literal", `-a + -"s"[0] + -(1)`, ""},
		{"in a table", "[]int{-1, 2, -3}[0]", "-1 -3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\n\nvar x = " + tc.expr + "\n"
			opts := defaultDupOptions()
			opts.normalizeLiterals = false
			toks, z := dupScan(t, src, opts)
			texts := make([]string, len(z.intern))
			for k, c := range z.intern {
				texts[c-dupInternBase] = k[1:]
			}
			var got []string
			for i, tk := range toks {
				if tk.class == duptok.Sign {
					got = append(got, token.Token(tk.code).String()+texts[toks[i+1].code-dupInternBase])
				}
			}
			if g := strings.Join(got, " "); g != tc.want {
				t.Errorf("signs = %q, want %q", g, tc.want)
			}
		})
	}
}
