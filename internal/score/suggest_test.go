package score

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
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
			if got := MetricSuggestion("changed_func_cognitive_max", 41, metrics.RawMetrics{}, tt.names); got != tt.want {
				t.Errorf("MetricSuggestion = %q, want %q", got, tt.want)
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
		"globals":                    {head: 3, want: "3 package-level variables"},
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
			got := MetricSuggestion(metric, tt.head, m, Names{})
			if !strings.Contains(got, tt.want) {
				t.Errorf("MetricSuggestion(%s) = %q, want it to contain %q", metric, got, tt.want)
			}
		})
	}
}

func TestMetricSuggestionUnknown(t *testing.T) {
	t.Parallel()

	for _, metric := range []string{"files", "fan_in", "coverage_pct", "nope"} {
		if got := MetricSuggestion(metric, 1, metrics.RawMetrics{}, Names{}); got != "" {
			t.Errorf("MetricSuggestion(%s) = %q, want empty", metric, got)
		}
	}
}

func TestMetricSuggestionSingular(t *testing.T) {
	t.Parallel()

	got := MetricSuggestion("untested_exports", 1, metrics.RawMetrics{}, Names{UntestedExports: []string{"Parse"}})
	want := "1 exported function has no test (Parse); a rebuild would have to reverse-engineer its behavior."
	if got != want {
		t.Errorf("MetricSuggestion = %q, want %q", got, want)
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
			got := MetricSuggestion("untested_exports", tt.head, metrics.RawMetrics{}, Names{UntestedExports: tt.names})
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestDuplicationNamesFirstLocation(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{DupBlocks: 4, DuplicationPct: 9.23}
	n := Names{DupLocations: []string{"parse.go:40", "encode.go:12"}}
	got := MetricSuggestion("dup_blocks", 4, m, n)
	want := "4 duplicate blocks cover 9.2% of lines; extract shared helpers, starting with parse.go:40."
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
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
		{TermVolume, 8000, []string{"10000 tokens", "20% of it duplicated", "8000 tokens of the rebuild"}},
		{TermSpec, 6000, []string{"Tests are 6000 tokens of the rebuild context"}},
		{TermContract, 1200, []string{"30 exported symbols", "4 internal packages", "1200 tokens"}},
		{TermUnspecified, 5600, []string{"7 exported functions have no test (Parse, Encode, Decode, Flush, Close and 2 more)"}},
		{TermHidden, 1200, []string{"2 package-level variables", "1 init function", "1200 tokens"}},
	}
	for _, tt := range tests {
		t.Run(tt.term, func(t *testing.T) {
			t.Parallel()
			got := driverSuggestion(Driver{Term: tt.term, Tokens: tt.tokens}, &m, Names{UntestedExports: sevenNames()})
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

	got := driverSuggestion(Driver{Term: TermVolume, Tokens: 10000}, &metrics.RawMetrics{TokensEst: 10000}, Names{})
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
			got := DriverSuggestions(rebuildOf(900, 0, 0, 0, 8100), m, Names{DupLocations: tt.locs})
			for _, s := range got {
				if strings.HasPrefix(s, "The package is") {
					if s != tt.want {
						t.Errorf("got  %q\nwant %q", s, tt.want)
					}
					return
				}
			}
			t.Fatalf("DriverSuggestions = %q, want a volume suggestion", got)
		})
	}
}

func TestDriverSuggestionsCutoff(t *testing.T) {
	t.Parallel()

	m := metrics.RawMetrics{TokensEst: 900, UntestedExports: 1, Globals: 1}
	tests := []struct {
		name string
		r    Rebuild
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
			if got := DriverSuggestions(tt.r, m, Names{}); len(got) != tt.want {
				t.Errorf("DriverSuggestions() = %q, want %d suggestions", got, tt.want)
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
	got := DriverSuggestions(Estimate(m, validParams()), m, Names{UntestedExports: sevenNames()})
	if len(got) != 2 {
		t.Fatalf("DriverSuggestions() = %q, want 2", got)
	}
	want := "15 exported functions have no test (Parse, Encode, Decode, Flush, Close and 10 more)"
	if !strings.HasPrefix(got[0], want) {
		t.Errorf("first suggestion = %q, want prefix %q", got[0], want)
	}
	if !strings.Contains(got[1], "8000 tokens of the rebuild") {
		t.Errorf("second suggestion = %q, want the volume driver", got[1])
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
	}
	for _, tt := range tests {
		got := MetricSuggestion(tt.metric, tt.head, metrics.RawMetrics{}, Names{})
		if !strings.Contains(got, tt.want) {
			t.Errorf("MetricSuggestion(%s, %v) = %q, want it to contain %q", tt.metric, tt.head, got, tt.want)
		}
	}
}

// TestMetricSuggestionCrossBlocks checks that the dup_blocks_cross_pkg
// template names both sides of the first shared block when the names carry
// it: the first occurrence and the first in another package.
func TestMetricSuggestionCrossBlocks(t *testing.T) {
	t.Parallel()

	occ := func(pkg, file string, start, end int) metrics.Occurrence {
		return metrics.Occurrence{Package: pkg, File: file, StartLine: start, EndLine: end}
	}
	pair := []metrics.CrossBlock{
		{Occurrences: []metrics.Occurrence{occ("m/a", "a/a.go", 12, 40), occ("m/b", "b/b.go", 8, 36)}},
		{Occurrences: []metrics.Occurrence{occ("m/c", "c/c.go", 1, 9), occ("m/d", "d/d.go", 2, 10)}},
	}
	tests := []struct {
		name   string
		head   float64
		blocks []metrics.CrossBlock
		want   string
	}{
		{"one block", 1, pair[:1],
			"1 duplicate block is shared with other packages; extract the shared block in a/a.go:12-40 and b/b.go:8-36 into one package."},
		{"several blocks cite the first", 2, pair,
			"2 duplicate blocks are shared with other packages; extract each into one package, starting with the block in a/a.go:12-40 and b/b.go:8-36."},
		{"second copy in the same package is skipped", 1, []metrics.CrossBlock{{Occurrences: []metrics.Occurrence{
			occ("m/a", "a/a.go", 12, 40), occ("m/a", "a/z.go", 3, 31), occ("m/b", "b/b.go", 8, 36),
		}}},
			"1 duplicate block is shared with other packages; extract the shared block in a/a.go:12-40 and b/b.go:8-36 (and 1 more) into one package."},
		{"no names", 1, nil, "1 duplicate block is shared with other packages; extract it into one place."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MetricSuggestion("dup_blocks_cross_pkg", tt.head, metrics.RawMetrics{}, Names{CrossBlocks: tt.blocks})
			if got != tt.want {
				t.Errorf("MetricSuggestion = %q, want %q", got, tt.want)
			}
		})
	}
}
