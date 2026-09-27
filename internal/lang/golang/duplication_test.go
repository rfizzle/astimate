package golang

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
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
	sa, reps := dupFind(s.codes, opts.minTokens)
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
