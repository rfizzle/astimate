package suggestion

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/inventory"
	"github.com/rfizzle/astimate/internal/score"
)

func sevenNames() []string {
	return []string{"Parse", "Encode", "Decode", "Flush", "Close", "Open", "Reset"}
}

// gatedMetrics lists every metric with a default threshold in SPEC.md 8.2.
func gatedMetrics() []string {
	return []string{
		"dup_blocks", "duplication_pct", "untested_exports", "globals", "init_funcs",
		"max_nesting", "cognitive_p90", "changed_func_cognitive_max", "tokens_est", "largest_file_sloc",
		"exported_symbols", "internal_imports", "sloc", "has_tests",
	}
}

func TestMetricSuggestionChangedFunction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names Names
		want  string
	}{
		{
			name:  "named",
			names: Names{ChangedFunction: "Parser.next (parse.go:40)"},
			want:  "Changed function Parser.next (parse.go:40) has cognitive complexity 41; split it into smaller functions or flatten its branching.",
		},
		{
			name: "unnamed",
			want: "A changed function has cognitive complexity 41; split it into smaller functions or flatten its branching.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Metric("changed_func_cognitive_max", 41, metrics.RawMetrics{}, tt.names); got != tt.want {
				t.Errorf("Metric = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMetricSuggestionEveryTemplate(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{SLOC: 4321, DupBlocks: 4, DuplicationPct: 9.2}
	known := metrics.MetricNames()
	tests := map[string]struct {
		head float64
		want string
	}{
		"dup_blocks":                 {head: 4, want: "4 duplicate blocks cover 9.2% of lines"},
		"duplication_pct":            {head: 9.2, want: "4 duplicate blocks cover 9.2% of lines"},
		"untested_exports":           {head: 7, want: "7 exported functions have no test"},
		"globals":                    {head: 3, want: "3 mutable package-level variables"},
		"init_funcs":                 {head: 2, want: "2 init functions"},
		"max_nesting":                {head: 6, want: "depth 6"},
		"cognitive_p90":              {head: 17, want: "complexity 17"},
		"changed_func_cognitive_max": {head: 40, want: "complexity 40"},
		"tokens_est":                 {head: 31000, want: "31000 tokens"},
		"largest_file_sloc":          {head: 912, want: "912 source lines"},
		"exported_symbols":           {head: 64, want: "64 symbols"},
		"internal_imports":           {head: 13, want: "13 internal packages"},
		"sloc":                       {head: 6500, want: "6500 source lines"},
		"has_tests":                  {head: 0, want: "4321 source lines and no tests"},
	}
	for _, metric := range gatedMetrics() {
		t.Run(metric, func(t *testing.T) {
			t.Parallel()
			if !slices.Contains(known, metric) {
				t.Fatalf("%s is not in metrics.MetricNames()", metric)
			}
			tt, ok := tests[metric]
			if !ok {
				t.Fatalf("no sample values for %s", metric)
			}
			got := Metric(metric, tt.head, m, Names{})
			if !strings.Contains(got, tt.want) {
				t.Errorf("Metric(%s) = %q, want it to contain %q", metric, got, tt.want)
			}
		})
	}
}

// TestMetricSuggestionCapacitySurface pins the exported_symbols and
// internal_imports templates: the split to make, in the first clause so a
// format that keeps only that clause still carries it, then the edits
// that lower the count without one, which are not fixes.
func TestMetricSuggestionCapacitySurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		metric string
		head   float64
		want   string
	}{
		{"exported_symbols", 64, "The package exports 64 symbols: split it along one of its responsibilities. " +
			"Not a fix: unexporting what callers need, folding a new function into an existing one's parameters " +
			"(a variadic or option argument), or changing what an existing export does."},
		{"internal_imports", 13, "The package imports 13 internal packages: move the code that needs them into a package of its own, or split it. " +
			"Not a fix: moving code into a package it does not belong in, or injecting a dependency only to drop the import."},
		{"internal_imports", 1, "The package imports 1 internal package: move the code that needs it into a package of its own, or split it. " +
			"Not a fix: moving code into a package it does not belong in, or injecting a dependency only to drop the import."},
	}
	for _, tt := range tests {
		t.Run(tt.metric+"/"+formatNum(tt.head), func(t *testing.T) {
			t.Parallel()
			if got := Metric(tt.metric, tt.head, metrics.RawMetrics{}, Names{}); got != tt.want {
				t.Errorf("Metric = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMetricSuggestionUnknown(t *testing.T) {
	t.Parallel()

	for _, metric := range []string{"files", "fan_in", "coverage_pct", "nope"} {
		if got := Metric(metric, 1, metrics.RawMetrics{}, Names{}); got != "" {
			t.Errorf("Metric(%s) = %q, want empty", metric, got)
		}
	}
}

func TestMetricSuggestionSingular(t *testing.T) {
	t.Parallel()

	got := Metric("untested_exports", 1, metrics.RawMetrics{}, Names{UntestedExports: []string{"Parse"}})
	want := "1 exported function has no test (Parse); a rebuild would have to reverse-engineer its behavior."
	if got != want {
		t.Errorf("Metric = %q, want %q", got, want)
	}
}

func TestUntestedExportsNamesTruncated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		head  float64
		names []string
		want  string
	}{
		{
			name:  "seven names",
			head:  7,
			names: sevenNames(),
			want:  "7 exported functions have no test (Parse, Encode, Decode, Flush, Close and 2 more); a rebuild would have to reverse-engineer their behavior.",
		},
		{
			name:  "fewer names than count",
			head:  9,
			names: sevenNames()[:3],
			want:  "9 exported functions have no test (Parse, Encode, Decode and 6 more); a rebuild would have to reverse-engineer their behavior.",
		},
		{
			name:  "exactly five",
			head:  5,
			names: sevenNames()[:5],
			want:  "5 exported functions have no test (Parse, Encode, Decode, Flush, Close); a rebuild would have to reverse-engineer their behavior.",
		},
		{
			name: "no names",
			head: 7,
			want: "7 exported functions have no test; a rebuild would have to reverse-engineer their behavior.",
		},
		{
			name: "one without names",
			head: 1,
			want: "1 exported function has no test; a rebuild would have to reverse-engineer its behavior.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Metric("untested_exports", tt.head, metrics.RawMetrics{}, Names{UntestedExports: tt.names})
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestMetricSuggestionGlobalsNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		head  float64
		names []string
		want  string
	}{
		{
			name: "no names",
			head: 3,
			want: "3 mutable package-level variables hold state no signature reveals; pass it explicitly or move it into a struct.",
		},
		{
			name:  "one name",
			head:  1,
			names: []string{"joins"},
			want:  "1 mutable package-level variable holds state no signature reveals (joins); pass it explicitly or move it into a struct.",
		},
		{
			name:  "seven names",
			head:  7,
			names: []string{"a", "b", "c", "d", "e", "f", "g"},
			want:  "7 mutable package-level variables hold state no signature reveals (a, b, c, d, e and 2 more); pass it explicitly or move it into a struct.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Metric("globals", tt.head, metrics.RawMetrics{}, Names{Globals: tt.names}); got != tt.want {
				t.Errorf("Metric = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDuplicationNamesFirstLocation(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{DupBlocks: 4, DuplicationPct: 9.23}
	n := Names{DupLocations: []string{"parse.go:40", "encode.go:12"}}
	got := Metric("dup_blocks", 4, m, n)
	want := "4 duplicate blocks cover 9.2% of lines; extract shared helpers, starting with parse.go:40."
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	n.DupAdded = true
	got = Metric("duplication_pct", 9.23, m, n)
	want = "4 duplicate blocks cover 9.2% of lines; extract shared helpers, starting with parse.go:40, added in this change."
	if got != want {
		t.Errorf("added: got  %q\nwant %q", got, want)
	}
	if got := Metric("dup_blocks", 4, m, Names{DupAdded: true}); got != "4 duplicate blocks cover 9.2% of lines; extract shared helpers." {
		t.Errorf("added without locations: got %q, want no citation", got)
	}
}

// TestDuplicationKinds checks that the dup_blocks suggestion says it
// counts blocks of every kind, and how many are code, when
// dup_blocks_code is known, and that the dup_blocks_code suggestion cites
// the first code block, not the first block of any kind.
func TestDuplicationKinds(t *testing.T) {
	t.Parallel()

	code := 1
	m := metrics.RawMetrics{DupBlocks: 3, DupBlocksCode: &code, DuplicationPct: 12.5}
	n := Names{DupLocations: []string{"keys.go:4-12", "keys.go:20-28", "parse.go:40-52", "lex.go:7-19"},
		DupCodeLocations: []string{"parse.go:40-52", "lex.go:7-19"}, DupCodeAdded: true}
	tests := []struct {
		metric string
		head   float64
		m      metrics.RawMetrics
		want   string
	}{
		{"dup_blocks", 3, m, "3 duplicate blocks of any kind (1 code) cover 12.5% of lines; extract shared helpers, starting with keys.go:4-12."},
		{"dup_blocks", 1, metrics.RawMetrics{DupBlocks: 1, DupBlocksCode: new(int), DuplicationPct: 4},
			"1 duplicate block of any kind (0 code) covers 4% of lines; extract shared helpers, starting with keys.go:4-12."},
		{"dup_blocks_code", 1, m, "1 duplicate code block (of 3 of any kind; not declarative, idiom or self-overlap); extract shared helpers, starting with parse.go:40-52, added in this change."},
		{"dup_blocks_code", 2, metrics.RawMetrics{DupBlocks: 2}, "2 duplicate code blocks (of 2 of any kind; not declarative, idiom or self-overlap); extract shared helpers, starting with parse.go:40-52, added in this change."},
	}
	for _, tt := range tests {
		if got := Metric(tt.metric, tt.head, tt.m, n); got != tt.want {
			t.Errorf("%s at %v:\ngot  %q\nwant %q", tt.metric, tt.head, got, tt.want)
		}
	}
	if got := Metric("dup_blocks_code", 0, m, Names{}); got != "0 duplicate code blocks (of 3 of any kind; not declarative, idiom or self-overlap); extract shared helpers." {
		t.Errorf("no code locations: got %q", got)
	}
}

func TestDriverSuggestionEveryTerm(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{
		TokensEst: 10000, TokensEstWithTests: 16000, DuplicationPct: 20,
		ExportedSymbols: 30, FanIn: 4, UntestedExports: 7, Globals: 2, InitFuncs: 1,
	}
	tests := []struct {
		term   string
		tokens float64
		want   []string
	}{
		{score.TermVolume, 8000, []string{"10000 tokens", "20% of it duplicated", "8000 tokens of the rebuild"}},
		{score.TermSpec, 6000, []string{"Tests are 6000 tokens of the rebuild context"}},
		{score.TermContract, 1200, []string{"30 exported symbols", "4 internal packages", "1200 tokens"}},
		{score.TermUnspecified, 5600, []string{"7 exported functions have no test (Parse, Encode, Decode, Flush, Close and 2 more)"}},
		{score.TermHidden, 1200, []string{"2 package-level variables", "1 init function", "1200 tokens"}},
	}
	for _, tt := range tests {
		t.Run(tt.term, func(t *testing.T) {
			t.Parallel()
			got := driverSuggestion(score.Driver{Term: tt.term, Tokens: tt.tokens}, &m, Names{UntestedExports: sevenNames()})
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("driverSuggestion(%s) = %q, want it to contain %q", tt.term, got, w)
				}
			}
		})
	}
}

func TestDriverSuggestionVolumeWithoutDuplication(t *testing.T) {
	t.Parallel()

	got := driverSuggestion(score.Driver{Term: score.TermVolume, Tokens: 10000}, &metrics.RawMetrics{TokensEst: 10000}, Names{})
	if strings.Contains(got, "duplicated") {
		t.Errorf("driverSuggestion(volume) = %q, want no duplication clause", got)
	}
}

func TestDriverSuggestionVolumeDupLocation(t *testing.T) {
	t.Parallel()

	locs := []string{"parse.go:40-58", "encode.go:12-30"}
	tests := []struct {
		name string
		pct  float64
		locs []string
		want string
	}{
		{
			name: "duplication with locations",
			pct:  12.5, locs: locs,
			want: "The package is 9000 tokens of non-test source, 12.5% of it duplicated, starting with parse.go:40-58; its volume is 900 tokens of the rebuild; split the package to shrink it.",
		},
		{
			name: "duplication without locations",
			pct:  12.5,
			want: "The package is 9000 tokens of non-test source, 12.5% of it duplicated; its volume is 900 tokens of the rebuild; split the package to shrink it.",
		},
		{
			name: "locations without duplication",
			locs: locs,
			want: "The package is 9000 tokens of non-test source; its volume is 900 tokens of the rebuild; split the package to shrink it.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := metrics.RawMetrics{TokensEst: 9000, DuplicationPct: tt.pct, Globals: 1}
			// Volume is exactly 10% of the rebuild, the smallest share that
			// still earns a suggestion.
			got := Drivers(rebuildOf(900, 0, 0, 0, 8100), m, Names{DupLocations: tt.locs})
			for _, s := range got {
				if strings.HasPrefix(s, "The package is") {
					if s != tt.want {
						t.Errorf("got  %q\nwant %q", s, tt.want)
					}
					return
				}
			}
			t.Fatalf("Drivers = %q, want a volume suggestion", got)
		})
	}
}

func TestDriverSuggestionsCutoff(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{TokensEst: 900, UntestedExports: 1, Globals: 1}
	tests := []struct {
		name string
		r    score.Rebuild
		want int
	}{
		{name: "second driver at exactly 10%", r: rebuildOf(900, 0, 0, 100, 0), want: 2},
		{name: "second driver below 10%", r: rebuildOf(950, 0, 0, 0, 49), want: 1},
		{name: "only two drivers even when a third is large", r: rebuildOf(400, 300, 300, 0, 0), want: 2},
		{name: "empty", r: rebuildOf(0, 0, 0, 0, 0), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Drivers(tt.r, m, Names{}); len(got) != tt.want {
				t.Errorf("Drivers() = %q, want %d suggestions", got, tt.want)
			}
		})
	}
}

func TestDriverSuggestionsOrderAndUnspecified(t *testing.T) {
	t.Parallel()

	// SPEC.md 7.2 worked example; seven of the fifteen untested names are known.
	m := metrics.RawMetrics{
		SLOC: 1200, TokensEst: 10000, TokensEstWithTests: 16000, DuplicationPct: 20,
		ExportedSymbols: 30, UntestedExports: 15, Globals: 2, InitFuncs: 1,
	}
	got := Drivers(score.Estimate(m, validParams()), m, Names{UntestedExports: sevenNames()})
	if len(got) != 2 {
		t.Fatalf("Drivers() = %q, want 2", got)
	}
	want := "15 exported functions have no test (Parse, Encode, Decode, Flush, Close and 10 more)"
	if !strings.HasPrefix(got[0], want) {
		t.Errorf("first suggestion = %q, want prefix %q", got[0], want)
	}
	if !strings.Contains(got[1], "8000 tokens of the rebuild") {
		t.Errorf("second suggestion = %q, want the volume driver", got[1])
	}
}

func TestDriverSuggestionUnspecifiedCoverage(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{
		SLOC: 1200, TokensEst: 10000, TokensEstWithTests: 16000, DuplicationPct: 20,
		ExportedSymbols: 30, UntestedExports: 15, Globals: 2, InitFuncs: 1,
	}
	tests := []struct {
		name     string
		coverage *float64
		want     string
	}{
		{name: "without coverage", want: "15 exported functions have no test; a rebuild would have to reverse-engineer their behavior."},
		{
			name: "with coverage", coverage: new(25.0),
			want: "15 exported functions have no test; a rebuild would have to reverse-engineer their behavior. " +
				"Tests cover 25% of statements, which scales this term to 9000 tokens.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mm := m
			mm.CoveragePct = tt.coverage
			r := score.Estimate(mm, validParams())
			ds := score.Drivers(r)
			if len(ds) == 0 || ds[0].Term != score.TermUnspecified {
				t.Fatalf("Drivers = %+v, want unspecified first", ds)
			}
			if tt.coverage != nil && !strings.Contains(ds[0].Detail, "coverage_pct=25") {
				t.Errorf("driver detail = %q, want coverage_pct named", ds[0].Detail)
			}
			if got := Drivers(r, mm, Names{})[0]; got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestMetricSuggestionV1Templates covers the templates of the v1 metrics a
// config may gate although the default configuration does not.
func TestMetricSuggestionV1Templates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		metric string
		head   float64
		want   string
	}{
		{"dup_blocks_cross_pkg", 3, "3 duplicate blocks are shared with other packages; extract them into one place."},
		{"dup_blocks_cross_pkg", 1, "1 duplicate block is shared with other packages; extract it into one place."},
		{"uses_cgo", 1, `imports "C"`},
		{"uses_reflect", 1, "imports reflect or unsafe"},
		{"generated_files", 2, "2 files are generated; change the generator or its input, not the output."},
		{"generated_files", 1, "1 file is generated"},
		{"tokens_est_generated", 240240, "240240 tokens of generated source, outside the rebuild estimate"},
		{"duplicated_lines", 157, "157 source lines are duplicated. 0 duplicate blocks cover 0% of lines; extract shared helpers."},
		{"duplicated_lines", 1, "1 source line is duplicated."},
	}
	for _, tt := range tests {
		got := Metric(tt.metric, tt.head, metrics.RawMetrics{}, Names{})
		if !strings.Contains(got, tt.want) {
			t.Errorf("Metric(%s, %v) = %q, want it to contain %q", tt.metric, tt.head, got, tt.want)
		}
	}
}

// TestMetricSuggestionCrossBlocks checks that the dup_blocks_cross_pkg
// template names both sides of the first shared block when the names carry
// it: the first occurrence and the first in another package.
func TestMetricSuggestionCrossBlocks(t *testing.T) {
	t.Parallel()

	occ := func(pkg, file string, start, end int) inventory.Occurrence {
		return inventory.Occurrence{Package: pkg, File: file, StartLine: start, EndLine: end}
	}
	pair := []inventory.CrossBlock{
		{Occurrences: []inventory.Occurrence{occ("m/a", "a/a.go", 12, 40), occ("m/b", "b/b.go", 8, 36)}},
		{Occurrences: []inventory.Occurrence{occ("m/c", "c/c.go", 1, 9), occ("m/d", "d/d.go", 2, 10)}},
	}
	tests := []struct {
		name   string
		head   float64
		blocks []inventory.CrossBlock
		added  bool
		want   string
	}{
		{"one block", 1, pair[:1], false,
			"1 duplicate block is shared with other packages; extract the shared block in a/a.go:12-40 and b/b.go:8-36 into one package."},
		{"several blocks cite the first", 2, pair, false,
			"2 duplicate blocks are shared with other packages; extract each into one package, starting with the block in a/a.go:12-40 and b/b.go:8-36."},
		{"one added block", 1, pair[:1], true,
			"1 duplicate block is shared with other packages; extract the shared block in a/a.go:12-40 and b/b.go:8-36, added in this change, into one package."},
		{"several blocks cite the added one", 2, pair, true,
			"2 duplicate blocks are shared with other packages; extract each into one package, starting with the block in a/a.go:12-40 and b/b.go:8-36, added in this change."},
		{"second copy in the same package is skipped", 1, []inventory.CrossBlock{{Occurrences: []inventory.Occurrence{
			occ("m/a", "a/a.go", 12, 40), occ("m/a", "a/z.go", 3, 31), occ("m/b", "b/b.go", 8, 36),
		}}}, false,
			"1 duplicate block is shared with other packages; extract the shared block in a/a.go:12-40 and b/b.go:8-36 (and 1 more) into one package."},
		{"no names", 1, nil, true, "1 duplicate block is shared with other packages; extract it into one place."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Metric("dup_blocks_cross_pkg", tt.head, metrics.RawMetrics{}, Names{CrossBlocks: tt.blocks, CrossAdded: tt.added})
			if got != tt.want {
				t.Errorf("Metric = %q, want %q", got, tt.want)
			}
		})
	}
}

// validParams returns the rebuild parameters package score's own tests
// use, which pass Validate.
func validParams() score.RebuildParams {
	p := score.RebuildParams{
		ContextBudget:           25000,
		TokensPerExport:         40,
		TokensPerUntestedExport: 800,
		TokensPerHiddenState:    400,
		SuperlinearExponent:     1.3,
		CocomoA:                 2.4,
		CocomoB:                 1.05,
		DaysPerMonth:            19,
		Cocomo:                  score.Cocomo{Model: score.ModelBasic81, UNFM: "completely_unfamiliar"},
	}
	p.Tiers.OnePassMax = 1
	p.Tiers.FewPassesMax = 3
	return p
}

// rebuildOf returns a rebuild whose terms carry the given tokens, in the
// fixed term order, and whose total is their sum.
func rebuildOf(volume, spec, contract, unspecified, hidden float64) score.Rebuild {
	names := []string{score.TermVolume, score.TermSpec, score.TermContract, score.TermUnspecified, score.TermHidden}
	tokens := []float64{volume, spec, contract, unspecified, hidden}
	r := score.Rebuild{Terms: make([]score.Term, 0, len(names))}
	for i, n := range names {
		r.Terms = append(r.Terms, score.Term{Name: n, Tokens: tokens[i], Detail: n + "-detail"})
		r.RebuildTokens += tokens[i]
	}
	return r
}

// crossOf returns a block of tokens normalized tokens with one occurrence
// in each of pkgs, in order.
func crossOf(tokens int, pkgs ...string) inventory.CrossBlock {
	b := inventory.CrossBlock{Tokens: tokens}
	for i, p := range pkgs {
		b.Occurrences = append(b.Occurrences, inventory.Occurrence{Package: p, File: p + "/x.go", StartLine: 10*i + 1, EndLine: 10*i + 9})
	}
	return b
}

func TestAddedCrossSuggestion(t *testing.T) {
	t.Parallel()

	one := []inventory.CrossBlock{crossOf(100, "a", "b")}
	tests := []struct {
		name  string
		added []inventory.CrossBlock
		floor int
		want  string
	}{
		{name: "none", want: ""},
		{name: "one", added: one, floor: 80,
			want: "The change added 1 duplicate block of at least 80 tokens shared with other packages; " +
				"extract the shared block in a/x.go:1-9 and b/x.go:11-19 into one package."},
		{name: "two without a floor", added: append(slices.Clone(one), crossOf(90, "c", "d")),
			want: "The change added 2 duplicate blocks shared with other packages; " +
				"extract each into one package, starting with the block in a/x.go:1-9 and b/x.go:11-19."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := AddedCross(tt.added, tt.floor); got != tt.want {
				t.Errorf("AddedCross = %q, want %q", got, tt.want)
			}
		})
	}
}
