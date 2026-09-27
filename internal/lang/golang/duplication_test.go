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
)

// dupRender renders the stream codes of one scanned source up to its
// separator: ID, LIT, #n for the n-th interned text, else the token.
func dupRender(t *testing.T, src string, opts dupOptions) string {
	t.Helper()
	s := &dupStream{intern: make(map[string]int32)}
	if err := s.scan(token.NewFileSet(), "src.go", []byte(src), opts); err != nil {
		t.Fatalf("scan: %v", err)
	}
	out := make([]string, 0, len(s.codes))
	for _, c := range s.codes {
		switch {
		case c >= dupSeparatorBase:
			return strings.Join(out, " ")
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
	t.Fatal("stream has no separator")
	return ""
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

// dupFound is a repeat as its length and sorted start positions.
type dupFound struct {
	n   int32
	pos []int32
}

// dupFoundOrder orders repeats by first occurrence, then length.
func dupFoundOrder(a, b dupFound) int {
	return cmp.Or(cmp.Compare(a.pos[0], b.pos[0]), cmp.Compare(a.n, b.n))
}

// dupFindSorted runs dupFind and returns the repeats in order of first
// occurrence.
func dupFindSorted(codes []int32, minTokens int) []dupFound {
	sa, reps := dupFind(codes, minTokens)
	out := make([]dupFound, 0, len(reps))
	for _, r := range reps {
		pos := slices.Clone(sa[r.lb : r.rb+1])
		slices.Sort(pos)
		out = append(out, dupFound{r.n, pos})
	}
	slices.SortFunc(out, dupFoundOrder)
	return out
}

func TestDupFind(t *testing.T) {
	const (
		a, b, c, d, e, x, y, z = 1, 2, 3, 4, 5, 6, 7, 8
		s1, s2, s3             = dupSeparatorBase, dupSeparatorBase + 1, dupSeparatorBase + 2
	)
	lr := []int32{c, d, x, c, d, y}
	cases := []struct {
		name  string
		codes []int32
		min   int
		want  []dupFound
	}{
		{"no repeat", []int32{a, b, c, d, e, s1}, 1, nil},
		{"one repeat of exactly min", []int32{a, b, c, x, a, b, c, y, s1}, 3, []dupFound{{3, []int32{0, 4}}}},
		{"one repeat of min minus one", []int32{a, b, c, x, a, b, c, y, s1}, 4, nil},
		{"three occurrences one block", []int32{a, b, c, x, a, b, c, y, a, b, c, z, s1}, 3, []dupFound{{3, []int32{0, 4, 8}}}},
		{"overlapping tandem run", []int32{x, a, b, a, b, a, b, a, b, y, s1}, 2, []dupFound{{6, []int32{1, 3}}}},
		{
			"nested repeats merge",
			slices.Concat(lr, []int32{z}, lr, []int32{e, s1}),
			2,
			[]dupFound{{6, []int32{0, 7}}},
		},
		{
			"partly nested repeat stays",
			[]int32{a, b, c, d, x, a, b, c, d, y, a, b, z, s1},
			2,
			[]dupFound{{2, []int32{0, 5, 10}}, {4, []int32{0, 5}}},
		},
		{"across file separator", []int32{x, a, b, s1, c, d, y, s2, z, a, b, c, d, e, s3}, 4, nil},
		{"same stream without separator", []int32{x, a, b, c, d, y, z, a, b, c, d, e, s3}, 4, []dupFound{{4, []int32{1, 7}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dupFindSorted(tc.codes, tc.min)
			if !slices.EqualFunc(got, tc.want, func(g, w dupFound) bool {
				return g.n == w.n && slices.Equal(g.pos, w.pos)
			}) {
				t.Errorf("dupFind = %v, want %v", got, tc.want)
			}
		})
	}
}

// dupNaive is the SPEC.md 6.3 rule applied by brute force: every maximal
// repeat of at least minTokens codes (at least two occurrences, not all with
// the same code before, not all with the same code after), minus those each
// of whose occurrences lies inside an occurrence of a longer one. s must end
// with a unique code.
func dupNaive(s []int32, minTokens int) []dupFound {
	occurs := func(w []int32) []int32 {
		var out []int32
		for q := 0; q+len(w) <= len(s); q++ {
			if slices.Equal(s[q:q+len(w)], w) {
				out = append(out, int32(q))
			}
		}
		return out
	}
	at := func(i int) int32 {
		if i < 0 || i >= len(s) {
			return -1
		}
		return s[i]
	}
	diverse := func(pos []int32, off int) bool {
		for _, p := range pos[1:] {
			if at(int(p)+off) != at(int(pos[0])+off) || at(int(p)+off) == -1 {
				return true
			}
		}
		return at(int(pos[0])+off) == -1
	}
	seen := make(map[string]bool)
	var all []dupFound
	for i := range s {
		for n := minTokens; i+n <= len(s); n++ {
			pos := occurs(s[i : i+n])
			if len(pos) < 2 {
				break
			}
			k := strconv.Itoa(int(pos[0])) + "/" + strconv.Itoa(n)
			if seen[k] || !diverse(pos, -1) || !diverse(pos, n) {
				continue
			}
			seen[k] = true
			all = append(all, dupFound{int32(n), pos})
		}
	}
	var out []dupFound
	for _, r := range all {
		inside := true
		for _, p := range r.pos {
			contained := false
			for _, o := range all {
				for _, q := range o.pos {
					if o.n > r.n && q <= p && p+r.n <= q+o.n {
						contained = true
					}
				}
			}
			inside = inside && contained
		}
		if !inside {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, dupFoundOrder)
	return out
}

func TestDupFindMatchesNaive(t *testing.T) {
	v := uint32(7)
	for trial := range 40 {
		alphabet := int32(2 + trial%3)
		codes := make([]int32, 0, 121)
		for range 120 {
			v = v*1103515245 + 12345
			codes = append(codes, int32(v>>16)%alphabet)
		}
		codes = append(codes, dupSeparatorBase)
		for _, minTokens := range []int{3, 6} {
			got := dupFindSorted(codes, minTokens)
			want := dupNaive(codes, minTokens)
			if !slices.EqualFunc(got, want, func(g, w dupFound) bool {
				return g.n == w.n && slices.Equal(g.pos, w.pos)
			}) {
				t.Fatalf("trial %d, min %d: dupFind = %v, naive %v", trial, minTokens, got, want)
			}
		}
	}
}

// TestDupLongPeriodicRun guards against quadratic behaviour on long literal
// tables: 200,000 tokens of "LIT ," form one block covering the run.
func TestDupLongPeriodicRun(t *testing.T) {
	const pairs = 100_000
	codes := make([]int32, 0, 2*pairs+3)
	codes = append(codes, int32(token.LBRACE))
	for range pairs {
		codes = append(codes, dupLitCode, int32(token.COMMA))
	}
	codes = append(codes, int32(token.RBRACE), dupSeparatorBase)
	got := dupFindSorted(codes, 40)
	want := []dupFound{{2*pairs - 2, []int32{1, 3}}}
	if !slices.EqualFunc(got, want, func(g, w dupFound) bool {
		return g.n == w.n && slices.Equal(g.pos, w.pos)
	}) {
		t.Errorf("dupFind = %v, want %v", got, want)
	}
}

func TestDupPercent(t *testing.T) {
	cases := []struct {
		covered, sloc int
		want          float64
	}{
		{54, 67, 80.6},
		{0, 10, 0},
		{5, 0, 0},
		{1, 3, 33.3},
		{2, 3, 66.7},
		{1, 8, 12.5},
		{10, 10, 100},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.covered)+"/"+strconv.Itoa(tc.sloc), func(t *testing.T) {
			if got := dupPercent(tc.covered, tc.sloc); got != tc.want {
				t.Errorf("dupPercent = %v, want %v", got, tc.want)
			}
		})
	}
}

// dupOfSources runs the duplication stream over inline sources, one file
// each, skipping generated ones as duplication does, with the SLOC of all
// of them (generated included) as the denominator.
func dupOfSources(t *testing.T, opts dupOptions, srcs ...string) dupCounts {
	t.Helper()
	s := &dupStream{intern: make(map[string]int32)}
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
		if err := s.scan(fs, name, []byte(src), opts); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	sa, reps := s.find(opts)
	return s.count(sa, reps, sloc)
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
	if same.blocks != 1 {
		t.Fatalf("identical copies: blocks = %d, want 1", same.blocks)
	}
	if renamed.blocks < same.blocks {
		t.Errorf("renamed copy: blocks = %d, identical copies %d", renamed.blocks, same.blocks)
	}
	if renamed.pct != same.pct {
		t.Errorf("renamed copy: pct = %v, identical copies %v", renamed.pct, same.pct)
	}
	// SLOC 1 + 13 + 1 + 13 = 28, covered 26.
	if same.pct != 92.9 {
		t.Errorf("pct = %v, want 92.9", same.pct)
	}
	raw := def
	raw.normalizeIdents, raw.normalizeLiterals = false, false
	if got := dupOfSources(t, raw, "package p\n\n"+dupCopy+"\nvar sep = 1\n\n"+dupRenamed); got.blocks != 0 {
		t.Errorf("renamed copy without normalization: blocks = %d, want 0", got.blocks)
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
	if got := dupOfSources(t, defaultDupOptions(), src); got.blocks != 0 {
		t.Errorf("default threshold: blocks = %d, want 0", got.blocks)
	}
	opts := defaultDupOptions()
	opts.minTokens = 20
	if got := dupOfSources(t, opts, src); got.blocks != 1 {
		t.Errorf("minTokens 20: blocks = %d, want 1", got.blocks)
	}
	opts.minTokens = 21
	if got := dupOfSources(t, opts, src); got.blocks != 0 {
		t.Errorf("minTokens 21: blocks = %d, want 0", got.blocks)
	}
}

func TestDupGeneratedExcluded(t *testing.T) {
	def := defaultDupOptions()
	a := "package p\n\n" + dupCopy
	b := "package p\n\n" + dupRenamed
	gen := "// Code generated by tool. DO NOT EDIT.\n\n" + b
	if got := dupOfSources(t, def, a, b); got.blocks != 1 {
		t.Fatalf("plain files: blocks = %d, want 1", got.blocks)
	}
	got := dupOfSources(t, def, a, gen)
	if got.blocks != 0 || got.pct != 0 {
		t.Errorf("with generated copy: blocks = %d, pct = %v, want 0, 0", got.blocks, got.pct)
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
	if got.blocks != 1 {
		t.Fatalf("blocks = %d, want 1", got.blocks)
	}
	// Each copy spans 18 lines of which 15 are SLOC. SLOC = 1 + 15 + 1 + 15,
	// covered 30.
	if want := 93.8; got.pct != want {
		t.Errorf("pct = %v, want %v", got.pct, want)
	}
	if len(got.locations) != 2 {
		t.Fatalf("locations = %v, want 2", got.locations)
	}
	for _, loc := range got.locations {
		if loc.endLine-loc.startLine != 17 {
			t.Errorf("location %v spans %d lines, want 18", loc, loc.endLine-loc.startLine+1)
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
	want := []dupLocation{{file, 9, 26}, {file, 31, 48}, {file, 53, 70}}
	if !slices.Equal(got.locations, want) {
		t.Errorf("locations = %v, want %v", got.locations, want)
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
		len(p.Syntax), loadTime, elapsed, sz.sloc, got.blocks, got.pct)
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
		rows = append(rows, dupMeasureRow{p.PkgPath, sz.sloc, a.blocks, b.blocks, a.pct, b.pct})
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
			if got.blocks != tc.want {
				t.Errorf("blocks = %d, want %d (locations %v)", got.blocks, tc.want, got.locations)
			}
			if tc.want == 0 && got.pct != 0 {
				t.Errorf("pct = %v, want 0", got.pct)
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
			s := &dupStream{intern: make(map[string]int32)}
			opts := defaultDupOptions()
			opts.normalizeLiterals = false
			if err := s.scan(token.NewFileSet(), "src.go", []byte(src), opts); err != nil {
				t.Fatal(err)
			}
			texts := make([]string, len(s.intern))
			for k, c := range s.intern {
				texts[c-dupInternBase] = k[1:]
			}
			got := make([]string, 0, len(s.signs))
			for _, p := range s.signs {
				got = append(got, token.Token(s.codes[p]).String()+texts[s.codes[p+1]-dupInternBase])
			}
			if g := strings.Join(got, " "); g != tc.want {
				t.Errorf("signs = %q, want %q", g, tc.want)
			}
		})
	}
}

// TestDupMeasureStdlibRefinements measures two candidate refinements of the
// duplicate-block rules against the shipped defaults over the standard
// library, for calibration/notes/duplication-refinements.md. A1 folds each
// unary sign before a numeric literal into the literal in the stream; A2
// (dup_fold_signs) leaves the stream alone and lets the literal-only rule
// count such a sign as literal; B drops a block whose only non-literal
// tokens are calls to one or two distinct functions (strictly, every
// identifier is a callee; loosely, identifiers may also be whole
// arguments). It loads every
// std package, so it runs only with ASTIMATE_MEASURE_STDLIB=1.
func TestDupMeasureStdlibRefinements(t *testing.T) {
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
	base := defaultDupOptions()
	base.foldSigns = false // the defaults before dup_fold_signs
	signs := base
	signs.foldSigns = true
	raw := base
	raw.normalizeIdents = false
	variants := []string{"A1 stream fold", "A2 sign-aware literal-only", "B strict", "B loose"}
	rows := make([][]dupMeasureRow, len(variants))
	changes := make([][]string, len(variants))
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
		s, err := dupStreamOf(l, p, osFiles{}, base)
		if err != nil {
			t.Fatal(err)
		}
		sa, reps := s.find(base)
		b := s.count(sa, reps, sz.sloc)
		f := dupFoldSigns(s)
		fsa, freps := f.find(base)
		a1 := f.count(fsa, freps, sz.sloc)
		a2, err := duplication(l, p, osFiles{}, sz, signs)
		if err != nil {
			t.Fatal(err)
		}
		for v, a := range []dupCounts{a1, a2} {
			rows[v] = append(rows[v], dupMeasureRow{p.PkgPath, sz.sloc, b.blocks, a.blocks, b.pct, a.pct})
			changes[v] = append(changes[v], dupLocDiff(p.PkgPath, "-", b.locations, a.locations)...)
			changes[v] = append(changes[v], dupLocDiff(p.PkgPath, "+", a.locations, b.locations)...)
		}
		names, err := dupStreamOf(l, p, osFiles{}, raw)
		if err != nil {
			t.Fatal(err)
		}
		for i, loose := range []bool{false, true} {
			v := i + 2
			kept := slices.DeleteFunc(slices.Clone(reps), func(r dupRepeat) bool {
				at := sa[r.lb]
				if !dupCallChain(s, names.codes, at, r.n, loose) {
					return false
				}
				changes[v] = append(changes[v], "- "+p.PkgPath+" "+s.files[s.file[at]].name+":"+
					strconv.Itoa(int(s.line[at]))+"-"+strconv.Itoa(int(s.last[at+r.n-1]))+
					" x"+strconv.Itoa(int(r.rb-r.lb+1))+" n"+strconv.Itoa(int(r.n)))
				return true
			})
			c := s.count(sa, kept, sz.sloc)
			rows[v] = append(rows[v], dupMeasureRow{p.PkgPath, sz.sloc, b.blocks, c.blocks, b.pct, c.pct})
		}
	}
	t.Logf("packages %d, wall %v", len(rows[0]), time.Since(start))
	for v, name := range variants {
		t.Logf("=== %s (off = shipped defaults, on = variant)", name)
		dupLogMeasure(t, rows[v])
		t.Logf("changed block occurrences (%d):", len(changes[v]))
		for _, c := range changes[v] {
			t.Log(c)
		}
	}
}

// dupLogMeasure logs the quantiles, totals, named packages, largest drops
// and rises and top ten of one variant's rows.
func dupLogMeasure(t *testing.T, rows []dupMeasureRow) {
	t.Helper()
	offPct := func(r dupMeasureRow) float64 { return r.offPct }
	onPct := func(r dupMeasureRow) float64 { return r.onPct }
	offB := func(r dupMeasureRow) float64 { return float64(r.offB) }
	onB := func(r dupMeasureRow) float64 { return float64(r.onB) }
	for _, q := range []float64{0.5, 0.9, 0.99} {
		t.Logf("p%v: duplication_pct off %v on %v; dup_blocks off %v on %v", q*100,
			dupQuantile(rows, offPct, q), dupQuantile(rows, onPct, q),
			dupQuantile(rows, offB, q), dupQuantile(rows, onB, q))
	}
	totalOff, totalOn, changed := 0, 0, 0
	for _, r := range rows {
		totalOff += r.offB
		totalOn += r.onB
		if r.offB != r.onB || r.offPct != r.onPct {
			changed++
		}
	}
	t.Logf("total dup_blocks off %d on %d; packages changed %d", totalOff, totalOn, changed)
	row := func(r dupMeasureRow) {
		t.Logf("| %s | %d | %d | %d | %v | %v |", r.path, r.sloc, r.offB, r.onB, r.offPct, r.onPct)
	}
	t.Log("named:")
	for _, r := range rows {
		switch r.path {
		case "crypto/internal/fips140/nistec", "math/big", "net/http":
			row(r)
		}
	}
	top := func(title string, get func(dupMeasureRow) float64) {
		t.Log(title)
		s := slices.Clone(rows)
		slices.SortStableFunc(s, func(a, b dupMeasureRow) int { return cmp.Compare(get(b), get(a)) })
		for _, r := range s[:min(10, len(s))] {
			row(r)
		}
	}
	top("largest drop:", func(r dupMeasureRow) float64 { return r.offPct - r.onPct })
	top("largest rise:", func(r dupMeasureRow) float64 { return r.onPct - r.offPct })
	top("top ten on:", onPct)
}

// dupFoldSigns returns a copy of s with every unary sign before a numeric
// literal removed from the stream, so the literal's code stands for the
// signed number: the fold considered in the normalizer and not shipped. The
// literal takes the sign's line.
func dupFoldSigns(s *dupStream) *dupStream {
	n := len(s.codes) - len(s.signs)
	f := &dupStream{
		codes: make([]int32, 0, n), file: make([]int32, 0, n),
		line: make([]int32, 0, n), last: make([]int32, 0, n),
		files: s.files, intern: s.intern, internLit: s.internLit,
	}
	k := 0
	for i := range s.codes {
		if k < len(s.signs) && int(s.signs[k]) == i {
			k++
			continue
		}
		line := s.line[i]
		if k > 0 && int(s.signs[k-1]) == i-1 {
			line = s.line[i-1]
		}
		f.codes = append(f.codes, s.codes[i])
		f.file = append(f.file, s.file[i])
		f.line = append(f.line, line)
		f.last = append(f.last, s.last[i])
	}
	return f
}

// dupLocDiff returns, prefixed by mark and pkg, the locations in a that are
// not in b.
func dupLocDiff(pkg, mark string, a, b []dupLocation) []string {
	var out []string
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, mark+" "+pkg+" "+x.file+":"+strconv.Itoa(x.startLine)+"-"+strconv.Itoa(x.endLine))
		}
	}
	return out
}

// dupCallChain reports whether the n codes of s at p are a call chain: every
// code is a literal, table punctuation or an identifier; the identifiers
// followed by "(" (the callees) have one or two distinct texts in names, the
// parallel stream with identifiers interned; and every other identifier is,
// when loose, a whole argument between "(" or "," and "," or ")", and when
// not loose, absent.
func dupCallChain(s *dupStream, names []int32, p, n int32, loose bool) bool {
	callees := make([]int32, 0, 2)
	codes := s.codes[p : p+n]
	for i, c := range codes {
		if s.literalOrPunct(c) {
			continue
		}
		if c != dupIdentCode {
			return false
		}
		next, prev := token.ILLEGAL, token.ILLEGAL
		if i+1 < len(codes) {
			next = token.Token(codes[i+1])
		}
		if i > 0 {
			prev = token.Token(codes[i-1])
		}
		if next == token.LPAREN {
			if name := names[int(p)+i]; !slices.Contains(callees, name) {
				callees = append(callees, name)
			}
			continue
		}
		if !loose || (prev != token.LPAREN && prev != token.COMMA) || (next != token.COMMA && next != token.RPAREN) {
			return false
		}
	}
	return len(callees) > 0 && len(callees) <= 2
}

// TestDupMeasureStdlibLiteralRuns measures two ways of reaching literal
// tables inside mixed duplicate blocks against the shipped defaults over the
// standard library, for calibration/notes/duplication-literal-runs.md. T
// trims literal-only prefixes and suffixes off each block and drops what is
// left below duplication.min_tokens; S splits each block at every
// literal-only run of at least duplication.min_tokens tokens and keeps the
// parts of at least that length; S10 is S with runs of at least 10 tokens,
// short enough to reach the coefficient tables in math. Literal-only is the
// sign-aware rule of dropLiteralOnly. It loads every std package, so it
// runs only with ASTIMATE_MEASURE_STDLIB=1.
func TestDupMeasureStdlibLiteralRuns(t *testing.T) {
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
	opts := defaultDupOptions()
	variants := []string{"T trim", "S split", "S10 split at runs of 10"}
	rows := make([][]dupMeasureRow, len(variants))
	changes := make([][]string, len(variants))
	mathFiles := []string{"j0.go", "j1.go", "erf.go", "lgamma.go"}
	mathLines := make([]map[string]int, len(variants)+1)
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
		s, err := dupStreamOf(l, p, osFiles{}, opts)
		if err != nil {
			t.Fatal(err)
		}
		sa, reps := s.find(opts)
		bb := dupBlocksOf(sa, reps)
		b, bl := dupCountOccs(s, bb, sz.sloc)
		if p.PkgPath == "math" {
			mathLines[0] = bl
		}
		for v, blocks := range [][]dupOcc{
			dupTrimRuns(s, bb, opts.minTokens),
			dupSplitRuns(s, bb, opts.minTokens, opts.minTokens),
			dupSplitRuns(s, bb, 10, opts.minTokens),
		} {
			a, al := dupCountOccs(s, blocks, sz.sloc)
			if p.PkgPath == "math" {
				mathLines[v+1] = al
			}
			rows[v] = append(rows[v], dupMeasureRow{p.PkgPath, sz.sloc, b.blocks, a.blocks, b.pct, a.pct})
			changes[v] = append(changes[v], dupOccDiff(s, p.PkgPath, "-", bb, blocks)...)
			changes[v] = append(changes[v], dupOccDiff(s, p.PkgPath, "+", blocks, bb)...)
		}
	}
	t.Logf("packages %d, wall %v", len(rows[0]), time.Since(start))
	t.Log("math covered lines per file: base, T, S, S10")
	for _, f := range mathFiles {
		var got []string
		for _, m := range mathLines {
			for name, n := range m {
				if filepath.Base(name) == f {
					got = append(got, strconv.Itoa(n))
				}
			}
		}
		t.Logf("| %s | %s |", f, strings.Join(got, " | "))
	}
	for v, name := range variants {
		t.Logf("=== %s (off = shipped defaults, on = variant)", name)
		dupLogMeasure(t, rows[v])
		named := []string{"math", "math/big", "math/rand", "math/cmplx"}
		for _, r := range rows[v] {
			if slices.Contains(named, r.path) {
				t.Logf("| %s | %d | %d | %d | %v | %v |", r.path, r.sloc, r.offB, r.onB, r.offPct, r.onPct)
			}
		}
		t.Logf("changed blocks (%d):", len(changes[v]))
		for _, c := range changes[v] {
			t.Log(c)
		}
	}
}

// dupOcc is a duplicate block given by its occurrences, in stream order, and
// its length n.
type dupOcc struct {
	pos []int32
	n   int32
}

// dupBlocksOf returns the blocks reps, intervals of the suffix array sa, as
// occurrence lists.
func dupBlocksOf(sa []int32, reps []dupRepeat) []dupOcc {
	out := make([]dupOcc, 0, len(reps))
	for _, r := range reps {
		pos := slices.Clone(sa[r.lb : r.rb+1])
		slices.Sort(pos)
		out = append(out, dupOcc{pos: pos, n: r.n})
	}
	return out
}

// dupLiteralMask reports, per token of the block b's first occurrence,
// whether it is literal-only under the sign-aware rule.
func dupLiteralMask(s *dupStream, b dupOcc) []bool {
	p := b.pos[0]
	mask := make([]bool, b.n)
	for i, c := range s.codes[p : p+b.n] {
		switch {
		case s.literalOrPunct(c):
			mask[i] = true
		case c == int32(token.SUB) || c == int32(token.ADD):
			_, mask[i] = slices.BinarySearch(s.signs, p+int32(i))
		}
	}
	return mask
}

// dupShift returns b's occurrences moved k tokens right with length n.
func dupShift(b dupOcc, k, n int32) dupOcc {
	pos := make([]int32, len(b.pos))
	for i, p := range b.pos {
		pos[i] = p + k
	}
	return dupOcc{pos: pos, n: n}
}

// dupUniq drops blocks with the same first occurrence and length as an
// earlier one.
func dupUniq(blocks []dupOcc) []dupOcc {
	type key struct{ p, n int32 }
	seen := make(map[key]bool, len(blocks))
	return slices.DeleteFunc(blocks, func(b dupOcc) bool {
		k := key{b.pos[0], b.n}
		if seen[k] {
			return true
		}
		seen[k] = true
		return false
	})
}

// dupTrimRuns is variant T: trim literal-only prefixes and suffixes off
// every block, dropping it when fewer than minTokens tokens remain.
func dupTrimRuns(s *dupStream, blocks []dupOcc, minTokens int) []dupOcc {
	out := make([]dupOcc, 0, len(blocks))
	for _, b := range blocks {
		mask := dupLiteralMask(s, b)
		lo, hi := int32(0), b.n
		for lo < hi && mask[lo] {
			lo++
		}
		for hi > lo && mask[hi-1] {
			hi--
		}
		if int(hi-lo) >= minTokens {
			out = append(out, dupShift(b, lo, hi-lo))
		}
	}
	return dupUniq(out)
}

// dupSplitRuns is variant S: split every block at each literal-only run of
// at least runTokens tokens and keep the parts of at least minTokens.
func dupSplitRuns(s *dupStream, blocks []dupOcc, runTokens, minTokens int) []dupOcc {
	out := make([]dupOcc, 0, len(blocks))
	for _, b := range blocks {
		mask := dupLiteralMask(s, b)
		from := int32(0)
		emit := func(to int32) {
			if int(to-from) >= minTokens {
				out = append(out, dupShift(b, from, to-from))
			}
		}
		for i := int32(0); i < b.n; {
			if !mask[i] {
				i++
				continue
			}
			j := i
			for j < b.n && mask[j] {
				j++
			}
			if int(j-i) >= runTokens {
				emit(i)
				from = j
			}
			i = j
		}
		emit(b.n)
	}
	return dupUniq(out)
}

// dupCountOccs is count over occurrence lists: it also returns the covered
// source lines per file name.
func dupCountOccs(s *dupStream, blocks []dupOcc, sloc int) (dupCounts, map[string]int) {
	c := dupCounts{blocks: len(blocks)}
	diff := make([]int32, len(s.codes)+1)
	for _, b := range blocks {
		for _, p := range b.pos {
			diff[p]++
			diff[p+b.n]--
			c.locations = append(c.locations, dupLocation{
				file:      s.files[s.file[p]].name,
				startLine: int(s.line[p]),
				endLine:   int(s.last[p+b.n-1]),
			})
		}
	}
	covered := make([][]bool, len(s.files))
	for i, f := range s.files {
		covered[i] = make([]bool, len(f.code))
	}
	depth := int32(0)
	for t := range s.codes {
		depth += diff[t]
		if depth == 0 || s.file[t] < 0 {
			continue
		}
		for ln := s.line[t]; ln <= s.last[t]; ln++ {
			covered[s.file[t]][ln] = true
		}
	}
	lines, per := 0, make(map[string]int, len(s.files))
	for i, f := range s.files {
		for ln, ok := range covered[i] {
			if ok && f.code[ln] {
				lines++
				per[f.name]++
			}
		}
	}
	c.pct = dupPercent(lines, sloc)
	return c, per
}

// dupOccDiff returns, prefixed by mark and pkg, the blocks of a whose first
// occurrence and length are not a block of b, with their first two
// occurrences, occurrence count and length.
func dupOccDiff(s *dupStream, pkg, mark string, a, b []dupOcc) []string {
	var out []string
	for _, x := range a {
		if slices.ContainsFunc(b, func(y dupOcc) bool { return y.pos[0] == x.pos[0] && y.n == x.n }) {
			continue
		}
		line := mark + " " + pkg
		for _, p := range x.pos[:min(2, len(x.pos))] {
			line += " " + filepath.Base(s.files[s.file[p]].name) + ":" +
				strconv.Itoa(int(s.line[p])) + "-" + strconv.Itoa(int(s.last[p+x.n-1]))
		}
		out = append(out, line+" x"+strconv.Itoa(len(x.pos))+" n"+strconv.Itoa(int(x.n)))
	}
	return out
}
