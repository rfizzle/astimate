package duptok

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// found is one repeat as tests compare it: length and sorted occurrences.
type found struct {
	n   int32
	pos []int32
}

func foundOrder(a, b found) int {
	return cmp.Or(cmp.Compare(a.pos[0], b.pos[0]), cmp.Compare(a.n, b.n))
}

func equalFound(a, b []found) bool {
	return slices.EqualFunc(a, b, func(g, w found) bool {
		return g.n == w.n && slices.Equal(g.pos, w.pos)
	})
}

// findSorted runs find and returns the repeats in order of first
// occurrence.
func findSorted(codes []int32, minTokens int) []found {
	sa, reps := find(codes, minTokens)
	out := make([]found, 0, len(reps))
	for _, r := range reps {
		pos := slices.Clone(sa[r.lb : r.rb+1])
		slices.Sort(pos)
		out = append(out, found{r.n, pos})
	}
	slices.SortFunc(out, foundOrder)
	return out
}

func TestFind(t *testing.T) {
	const (
		a, b, c, d, e, x, y, z = 1, 2, 3, 4, 5, 6, 7, 8
		s1, s2, s3             = SeparatorBase, SeparatorBase + 1, SeparatorBase + 2
	)
	lr := []int32{c, d, x, c, d, y}
	cases := []struct {
		name  string
		codes []int32
		min   int
		want  []found
	}{
		{"no repeat", []int32{a, b, c, d, e, s1}, 1, nil},
		{"one repeat of exactly min", []int32{a, b, c, x, a, b, c, y, s1}, 3, []found{{3, []int32{0, 4}}}},
		{"one repeat of min minus one", []int32{a, b, c, x, a, b, c, y, s1}, 4, nil},
		{"three occurrences one block", []int32{a, b, c, x, a, b, c, y, a, b, c, z, s1}, 3, []found{{3, []int32{0, 4, 8}}}},
		{"overlapping tandem run", []int32{x, a, b, a, b, a, b, a, b, y, s1}, 2, []found{{6, []int32{1, 3}}}},
		{"nested repeats merge", slices.Concat(lr, []int32{z}, lr, []int32{e, s1}), 2, []found{{6, []int32{0, 7}}}},
		{
			"partly nested repeat stays",
			[]int32{a, b, c, d, x, a, b, c, d, y, a, b, z, s1},
			2,
			[]found{{2, []int32{0, 5, 10}}, {4, []int32{0, 5}}},
		},
		{"across file separator", []int32{x, a, b, s1, c, d, y, s2, z, a, b, c, d, e, s3}, 4, nil},
		{"same stream without separator", []int32{x, a, b, c, d, y, z, a, b, c, d, e, s3}, 4, []found{{4, []int32{1, 7}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := findSorted(tc.codes, tc.min); !equalFound(got, tc.want) {
				t.Errorf("find = %v, want %v", got, tc.want)
			}
		})
	}
}

// naive applies the SPEC.md 6.3 rule by brute force: every maximal repeat
// of at least minTokens codes, minus those each of whose occurrences lies
// inside an occurrence of a longer one. s must end with a unique code.
func naive(s []int32, minTokens int) []found {
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
	var all []found
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
			all = append(all, found{int32(n), pos})
		}
	}
	var out []found
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
	slices.SortFunc(out, foundOrder)
	return out
}

func TestFindMatchesNaive(t *testing.T) {
	v := uint32(7)
	for trial := range 40 {
		alphabet := int32(2 + trial%3)
		codes := make([]int32, 0, 121)
		for range 120 {
			v = v*1103515245 + 12345
			codes = append(codes, int32(v>>16)%alphabet)
		}
		codes = append(codes, SeparatorBase)
		for _, minTokens := range []int{3, 6} {
			got := findSorted(codes, minTokens)
			if want := naive(codes, minTokens); !equalFound(got, want) {
				t.Fatalf("trial %d, min %d: find = %v, naive %v", trial, minTokens, got, want)
			}
		}
	}
}

// TestFindLongPeriodicRun guards against quadratic behaviour on long
// literal tables: 200,000 tokens of "LIT ," form one block.
func TestFindLongPeriodicRun(t *testing.T) {
	const (
		pairs                    = 100_000
		lbrace, lit, comma       = 1, 2, 3
		rbrace             int32 = 4
	)
	codes := make([]int32, 0, 2*pairs+3)
	codes = append(codes, lbrace)
	for range pairs {
		codes = append(codes, lit, comma)
	}
	codes = append(codes, rbrace, SeparatorBase)
	want := []found{{2*pairs - 2, []int32{1, 3}}}
	if got := findSorted(codes, 40); !equalFound(got, want) {
		t.Errorf("find = %v, want %v", got, want)
	}
}

func TestPercent(t *testing.T) {
	cases := []struct {
		covered, sloc int
		want          float64
	}{
		{54, 67, 80.6},
		{0, 10, 0},
		{5, 0, 0},
		{1, 3, 33.3},
		{2, 3, 66.7},
		{10, 10, 100},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.covered)+"/"+strconv.Itoa(tc.sloc), func(t *testing.T) {
			if got := Percent(tc.covered, tc.sloc); got != tc.want {
				t.Errorf("Percent = %v, want %v", got, tc.want)
			}
		})
	}
}

// tok is one token a test adds to a Stream.
type tok struct {
	code  int32
	class Class
	line  int
}

// streamOf builds a stream of one file per element of files, each token on
// its own line unless it says otherwise, every line a source line.
func streamOf(t *testing.T, files ...[]tok) *Stream {
	t.Helper()
	var s Stream
	for i, toks := range files {
		lines := 1
		for _, tk := range toks {
			lines = max(lines, tk.line)
			if err := s.Add(tk.code, tk.class, tk.line, tk.line); err != nil {
				t.Fatal(err)
			}
		}
		code := make([]bool, lines+1)
		for ln := 1; ln <= lines; ln++ {
			code[ln] = true
		}
		s.EndFile("f"+strconv.Itoa(i), code)
	}
	return &s
}

// seq returns n tokens alternating between two codes of class c, one per
// line from line 1, followed by a closing token of class Code.
func seq(n int, a, b int32, c Class, end int32) []tok {
	out := make([]tok, 0, n+1)
	for i := range n {
		code := a
		if i%2 == 1 {
			code = b
		}
		out = append(out, tok{code, c, i + 1})
	}
	return append(out, tok{end, Code, n + 1})
}

func TestCountRenamedCopiesAcrossFiles(t *testing.T) {
	body := seq(10, 1, 2, Code, 3)
	other := seq(10, 1, 2, Code, 4)
	s := streamOf(t, body, other)
	got, err := s.Count(Options{MinTokens: 5}, 22)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks != 1 {
		t.Fatalf("Blocks = %d, want 1", got.Blocks)
	}
	// The common prefix is the 10 alternating tokens, lines 1-10 of each
	// file: 20 of 22 source lines.
	if got.Pct != Percent(20, 22) {
		t.Errorf("Pct = %v, want %v", got.Pct, Percent(20, 22))
	}
	want := []Location{{"f0", 1, 10}, {"f1", 1, 10}}
	if !slices.Equal(got.Locations, want) {
		t.Errorf("Locations = %v, want %v", got.Locations, want)
	}
}

func TestCountLiteralOnly(t *testing.T) {
	cases := []struct {
		name       string
		class      Class
		opts       Options
		wantBlocks int
	}{
		{"literal table dropped", Literal, Options{MinTokens: 5, IgnoreLiteralOnly: true}, 0},
		{"literal table kept when rule off", Literal, Options{MinTokens: 5}, 1},
		{"punctuation only dropped", Punct, Options{MinTokens: 5, IgnoreLiteralOnly: true}, 0},
		{"code kept", Code, Options{MinTokens: 5, IgnoreLiteralOnly: true}, 1},
		{"signs folded", Sign, Options{MinTokens: 5, IgnoreLiteralOnly: true, FoldSigns: true}, 0},
		{"signs not folded", Sign, Options{MinTokens: 5, IgnoreLiteralOnly: true}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Alternate the class under test with literals, so a sign
			// always precedes a literal.
			var toks []tok
			for i := range 8 {
				if i%2 == 0 {
					toks = append(toks, tok{10, tc.class, i + 1})
				} else {
					toks = append(toks, tok{11, Literal, i + 1})
				}
			}
			a := append(slices.Clone(toks), tok{20, Code, 9})
			b := append(slices.Clone(toks), tok{21, Code, 9})
			got, err := streamOf(t, a, b).Count(tc.opts, 18)
			if err != nil {
				t.Fatal(err)
			}
			if got.Blocks != tc.wantBlocks {
				t.Errorf("Blocks = %d, want %d", got.Blocks, tc.wantBlocks)
			}
			if tc.wantBlocks == 0 && (got.Pct != 0 || len(got.Locations) != 0) {
				t.Errorf("dropped block still covers: %+v", got)
			}
		})
	}
}

// classed returns one token per class, one per line from line 1, with
// codes from base up, so no two tokens of the result repeat.
func classed(base int32, classes ...Class) []tok {
	out := make([]tok, len(classes))
	for i, c := range classes {
		out[i] = tok{base + int32(i), c, i + 1}
	}
	return out
}

// run returns n copies of c.
func run(n int, c Class) []Class {
	return slices.Repeat([]Class{c}, n)
}

func TestCountSplitLiteralRuns(t *testing.T) {
	// A 28-token block: a 3-token header, a 6-token literal run, 6 code
	// tokens, a 4-token literal run, 2 code tokens, a 5-token literal run
	// and 2 code tokens. At MinTokens 5 the split cuts the 6- and 5-token
	// runs and keeps only lines 10-21, the part between them.
	block := slices.Concat(run(3, Code), run(6, Literal), run(6, Code), run(4, Punct), run(2, Code), run(5, Literal), run(2, Code))
	// The same block with a sign in the first run: that run is only
	// literal-only when signs fold.
	signed := slices.Clone(block)
	signed[5] = Sign
	on := Options{MinTokens: 5, IgnoreLiteralOnly: true, FoldSigns: true, SplitLiteralRuns: true}
	cases := []struct {
		name    string
		classes []Class
		opts    Options
		want    []string
	}{
		{"not split", block, Options{MinTokens: 5, IgnoreLiteralOnly: true, FoldSigns: true}, []string{"f0:1-28", "f1:1-28"}},
		{"split", block, on, []string{"f0:10-21", "f1:10-21"}},
		{"split needs the literal-only rule", block, Options{MinTokens: 5, SplitLiteralRuns: true}, []string{"f0:1-28", "f1:1-28"}},
		{"sign folded into the run", signed, on, []string{"f0:10-21", "f1:10-21"}},
		{"sign breaks the run", signed, Options{MinTokens: 5, IgnoreLiteralOnly: true, SplitLiteralRuns: true}, []string{"f0:1-21", "f1:1-21"}},
		{"runs shorter than MinTokens kept", block, Options{MinTokens: 6, IgnoreLiteralOnly: true, SplitLiteralRuns: true}, []string{"f0:10-28", "f1:10-28"}},
		{"parts below MinTokens dropped", block, Options{MinTokens: 13, IgnoreLiteralOnly: true, SplitLiteralRuns: true}, []string{"f0:1-28", "f1:1-28"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := append(classed(100, tc.classes...), tok{90, Code, len(tc.classes) + 1})
			b := append(classed(100, tc.classes...), tok{91, Code, len(tc.classes) + 1})
			s := streamOf(t, a, b)
			got, err := s.Count(tc.opts, 58)
			if err != nil {
				t.Fatal(err)
			}
			locs := make([]string, 0, len(got.Locations))
			for _, loc := range got.Locations {
				locs = append(locs, locationKey(loc))
			}
			if !slices.Equal(locs, tc.want) {
				t.Errorf("Locations = %v, want %v", locs, tc.want)
			}
			if got.Blocks != 1 {
				t.Errorf("Blocks = %d, want 1", got.Blocks)
			}
			blocks, err := s.Blocks(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(blocks) != 1 {
				t.Fatalf("Blocks found %d blocks, want 1", len(blocks))
			}
			bl := make([]string, 0, len(blocks[0].Locations))
			for _, loc := range blocks[0].Locations {
				bl = append(bl, locationKey(loc))
			}
			slices.Sort(bl)
			if !slices.Equal(bl, tc.want) {
				t.Errorf("Blocks locations = %v, want %v", bl, tc.want)
			}
		})
	}
}

func TestCountSplitLiteralRunsDropsEverything(t *testing.T) {
	// A block that is one long literal run with a short header leaves no
	// part of MinTokens: the whole block goes.
	classes := slices.Concat(run(2, Code), run(10, Literal))
	a := append(classed(100, classes...), tok{90, Code, 13})
	b := append(classed(100, classes...), tok{91, Code, 13})
	got, err := streamOf(t, a, b).Count(Options{MinTokens: 5, IgnoreLiteralOnly: true, SplitLiteralRuns: true}, 26)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks != 0 || got.Pct != 0 || len(got.Locations) != 0 {
		t.Errorf("Count = %+v, want nothing", got)
	}
}

func TestCountSplitLiteralRunsSharedPart(t *testing.T) {
	// Two blocks, H L K in files 0 and 1 and K L' H' in files 0 and 2,
	// both cut down to the 6 code tokens K at the same place in file 0.
	// The part counts once, with the occurrences of the first block.
	h := classed(100, Code)
	l := classed(200, run(5, Literal)...)
	k := classed(300, run(6, Code)...)
	l2 := classed(400, run(5, Literal)...)
	h2 := classed(500, Code)
	lines := func(parts ...[]tok) []tok {
		var out []tok
		for _, p := range parts {
			for _, tk := range p {
				tk.line = len(out) + 1
				out = append(out, tk)
			}
		}
		return out
	}
	f0 := lines(h, l, k, l2, h2)
	f1 := lines(h, l, k, classed(600, Code))
	f2 := lines(classed(700, Code), k, l2, h2)
	s := streamOf(t, f0, f1, f2)
	off := Options{MinTokens: 5, IgnoreLiteralOnly: true}
	if got, err := s.Count(off, 42); err != nil || got.Blocks != 2 {
		t.Fatalf("unsplit Count = %+v, %v; want 2 blocks", got, err)
	}
	on := off
	on.SplitLiteralRuns = true
	got, err := s.Count(on, 42)
	if err != nil {
		t.Fatal(err)
	}
	locs := make([]string, 0, len(got.Locations))
	for _, loc := range got.Locations {
		locs = append(locs, locationKey(loc))
	}
	if want := []string{"f0:7-12", "f1:7-12"}; got.Blocks != 1 || !slices.Equal(locs, want) {
		t.Errorf("Count = %d blocks at %v, want 1 at %v", got.Blocks, locs, want)
	}
}

func TestCountSkipsNonSourceLines(t *testing.T) {
	var s Stream
	for f := range 2 {
		for i := range 6 {
			// Each token spans two lines, the second not a source line.
			if err := s.Add(int32(i+1), Code, 2*i+1, 2*i+2); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Add(int32(100+f), Code, 13, 13); err != nil {
			t.Fatal(err)
		}
		code := make([]bool, 14)
		for ln := 1; ln <= 13; ln += 2 {
			code[ln] = true
		}
		s.EndFile("f"+strconv.Itoa(f), code)
	}
	got, err := s.Count(Options{MinTokens: 3}, 14)
	if err != nil {
		t.Fatal(err)
	}
	// Six source lines covered per file.
	if want := Percent(12, 14); got.Pct != want {
		t.Errorf("Pct = %v, want %v", got.Pct, want)
	}
	if got.Locations[0].EndLine != 12 {
		t.Errorf("EndLine = %d, want 12, the last line of the last token", got.Locations[0].EndLine)
	}
}

func TestCountErrors(t *testing.T) {
	var s Stream
	if err := s.Add(-1, Code, 1, 1); err == nil {
		t.Error("Add(-1) returned no error")
	}
	if err := s.Add(SeparatorBase, Code, 1, 1); err == nil {
		t.Error("Add(SeparatorBase) returned no error")
	}
	if _, err := s.Count(Options{MinTokens: 0}, 1); err == nil {
		t.Error("Count with MinTokens 0 returned no error")
	}
}

func TestCountIgnoresUnterminatedFile(t *testing.T) {
	s := streamOf(t, seq(10, 1, 2, Code, 3), seq(10, 1, 2, Code, 4))
	for i := range 10 {
		if err := s.Add(int32(1+i%2), Code, i+1, i+1); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Count(Options{MinTokens: 5}, 22)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Locations) != 2 {
		t.Errorf("Locations = %v, want the two closed files only", got.Locations)
	}
}

func TestEmptyStream(t *testing.T) {
	var s Stream
	got, err := s.Count(DefaultOptions(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocks != 0 || got.Pct != 0 {
		t.Errorf("Count on an empty stream = %+v", got)
	}
}

// blockKeys renders blocks as "tokens:[files]" with files sorted, in
// sorted order, so blocks compare regardless of order.
func blockKeys(bs []Block) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		f := make([]string, 0, len(b.Files))
		for _, i := range b.Files {
			f = append(f, strconv.Itoa(int(i)))
		}
		slices.Sort(f)
		out = append(out, strconv.Itoa(b.Tokens)+":"+strings.Join(f, ","))
	}
	slices.Sort(out)
	return out
}

// locationKey renders loc as "file:start-end".
func locationKey(loc Location) string {
	return loc.File + ":" + strconv.Itoa(loc.StartLine) + "-" + strconv.Itoa(loc.EndLine)
}

func TestBlocks(t *testing.T) {
	// Files 0 and 3 are the same 11 tokens; file 1 shares their first 10;
	// files 2 and 4 share a 10-token literal table.
	body := seq(10, 1, 2, Code, 3)
	s := streamOf(t, body, seq(10, 1, 2, Code, 4), seq(10, 5, 6, Literal, 7), body, seq(10, 5, 6, Literal, 8))
	if got := s.Files(); got != 5 {
		t.Fatalf("Files = %d, want 5", got)
	}
	cases := []struct {
		name string
		opts Options
		want []Block
	}{
		{"literal table dropped", Options{MinTokens: 5, IgnoreLiteralOnly: true}, []Block{
			{Tokens: 10, Files: []int32{0, 1, 3}}, {Tokens: 11, Files: []int32{0, 3}},
		}},
		{"literal table kept", Options{MinTokens: 5}, []Block{
			{Tokens: 10, Files: []int32{0, 1, 3}}, {Tokens: 11, Files: []int32{0, 3}}, {Tokens: 10, Files: []int32{2, 4}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Blocks(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if g, w := blockKeys(got), blockKeys(tc.want); !slices.Equal(g, w) {
				t.Errorf("Blocks = %v, want %v", g, w)
			}
			res, err := s.Count(tc.opts, 55)
			if err != nil {
				t.Fatal(err)
			}
			if res.Blocks != len(got) {
				t.Errorf("Count found %d blocks, Blocks %d", res.Blocks, len(got))
			}
			// Each block's locations parallel its files and, together, are
			// the occurrences Count locates.
			var locs []string
			for _, b := range got {
				if len(b.Locations) != len(b.Files) {
					t.Fatalf("block of %d tokens has %d locations for %d files", b.Tokens, len(b.Locations), len(b.Files))
				}
				for i, loc := range b.Locations {
					if want := "f" + strconv.Itoa(int(b.Files[i])); loc.File != want {
						t.Errorf("location %d of a %d-token block is in %s, want %s", i, b.Tokens, loc.File, want)
					}
					locs = append(locs, locationKey(loc))
				}
			}
			var want []string
			for _, loc := range res.Locations {
				want = append(want, locationKey(loc))
			}
			slices.Sort(locs)
			slices.Sort(want)
			if !slices.Equal(locs, want) {
				t.Errorf("Blocks locations = %v, Count locations %v", locs, want)
			}
		})
	}
	if _, err := s.Blocks(Options{}); err == nil {
		t.Error("Blocks with MinTokens 0 returned no error")
	}
}
