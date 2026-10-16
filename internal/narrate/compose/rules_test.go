package compose

import (
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/report/details"
)

func TestBand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tier string
		over int
		dup  float64
		want string
	}{
		{"partition", tierPartition, 0, 0, narrate.BandSplit},
		{"over two", "ONE_PASS", 2, 0, narrate.BandSplit},
		{"over one", "ONE_PASS", 1, 0, narrate.BandWatch},
		{"few passes", tierFewPasses, 0, 0, narrate.BandWatch},
		{"a quarter copied", "ONE_PASS", 0, 25, narrate.BandWatch},
		{"just under a quarter", "ONE_PASS", 0, 24.9, narrate.BandFine},
		{"fine", "ONE_PASS", 0, 0, narrate.BandFine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := band(tt.tier, tt.over, tt.dup); got != tt.want {
				t.Errorf("band(%s, %d, %v) = %s, want %s", tt.tier, tt.over, tt.dup, got, tt.want)
			}
		})
	}
}

func TestCeilings(t *testing.T) {
	t.Parallel()

	m := &metrics.RawMetrics{SLOC: 1001, TokensEst: 12800, LargestFileSLOC: 100, ExportedSymbols: 61, InternalImports: 11}
	cs, over := ceilings(m, "main", defaultLimits())
	if want := []string{"sloc"}; !slices.Equal(over, want) {
		t.Errorf("over = %q, want %q: main is not judged on exports or imports", over, want)
	}
	levels := make([]string, 0, len(cs))
	for _, c := range cs {
		levels = append(levels, c.Level)
	}
	// tokens_est is at 80% of its ceiling; exports and imports are over
	// but not judged, so their meters only warn.
	if want := []string{narrate.BandSplit, narrate.BandWatch, narrate.BandFine, narrate.BandWatch, narrate.BandWatch}; !slices.Equal(levels, want) {
		t.Errorf("levels = %q, want %q", levels, want)
	}
	if _, over := ceilings(m, "", defaultLimits()); len(over) != 3 {
		t.Errorf("over = %q, want three: no role is a library", over)
	}
}

func TestHotOrder(t *testing.T) {
	t.Parallel()

	// By churn times human days, "wide" (5 x 10.0) would outrank "deep"
	// (3 x 8.0); by churn times agent passes, "deep" (3 x 4.0) outranks
	// "wide" (5 x 1.0). Without churn, human days alone order the rest.
	mk := func(path string, churn *int, passes, days float64) narrate.Package {
		r := &report.Report{PackagePath: path, Rebuild: report.Rebuild{AgentPasses: passes, HumanDays: days}}
		r.Metrics.ChurnCommits = churn
		return pkg(r, nil, 0)
	}
	five, three := 5, 3
	ps := []narrate.Package{
		mk("wide", &five, 1, 10), mk("deep", &three, 4, 8),
		mk("idle-big", nil, 0, 12), mk("idle-small", nil, 0, 2), mk("idle-small-a", nil, 0, 2),
	}
	var got []string
	for _, p := range hotOrder(ps) {
		got = append(got, p.Path)
	}
	want := []string{"deep", "wide", "idle-big", "idle-small", "idle-small-a"}
	if !slices.Equal(got, want) {
		t.Errorf("hot order = %q, want %q", got, want)
	}
	if ps[0].Hotspot != 5 || ps[1].Hotspot != 12 {
		t.Errorf("hotspots = %v and %v, want 5 and 12 (churn times agent passes)", ps[0].Hotspot, ps[1].Hotspot)
	}
}

func TestBuckets(t *testing.T) {
	t.Parallel()

	fns := []details.Function{{Name: "a", Cognitive: 10}, {Name: "b", Cognitive: 11}, {Name: "c", Cognitive: 20},
		{Name: "d", Cognitive: 21}, {Name: "e", Cognitive: 0}}
	if got := buckets(fns); *got != (narrate.Buckets{Easy: 2, Care: 2, Hard: 1}) {
		t.Errorf("buckets = %+v, want easy 2, care 2, hard 1", *got)
	}
	if got := buckets(nil); got != nil {
		t.Errorf("buckets(nil) = %+v, want nil", got)
	}
	got := hardest(fns)
	names := make([]string, 0, len(got))
	for _, f := range got {
		names = append(names, f.Name+":"+f.Level)
	}
	if want := []string{"d:split", "c:watch", "b:watch"}; !slices.Equal(names, want) {
		t.Errorf("hardest = %q, want %q", names, want)
	}
}

func TestChunks(t *testing.T) {
	t.Parallel()

	// The longest block's two occurrences come first, even when a later
	// block's occurrence spans more lines: pairing the longest spans by
	// line count once named two different blocks as one repeat.
	spans := []details.Span{
		{File: "handlers.go", StartLine: 10, EndLine: 20},
		{File: "state.go", StartLine: 5, EndLine: 14},
		{File: "a.go", StartLine: 12, EndLine: 60},
		{File: "b.go", StartLine: 15, EndLine: 63},
	}
	want := []narrate.Chunk{{File: "handlers.go", Start: 10, End: 20, Lines: 11}, {File: "state.go", Start: 5, End: 14, Lines: 10}}
	if got := chunks(spans); !slices.Equal(got, want) {
		t.Errorf("chunks = %+v, want %+v", got, want)
	}
	if got := chunks(spans[:1]); len(got) != 1 {
		t.Errorf("chunks of one span = %+v, want one", got)
	}
	if got := chunks(nil); len(got) != 0 {
		t.Errorf("chunks of none = %+v, want none", got)
	}
}

func TestHuman(t *testing.T) {
	t.Parallel()

	// One figure, both ends equal, at each unit boundary.
	tests := []struct {
		days        float64
		long, short string
	}{
		{0, "1 day", "1 d"},
		{1.4, "1 day", "1 d"},
		{4.9, "5 days", "5 d"},
		{5, "1 week", "1 wk"},
		{20.9, "4 weeks", "4 wk"},
		{21, "1 month", "1 mo"},
		{219, "10 months", "10 mo"},
		{220, "1 engineer-year", "1 yr"},
		{328.2, "1 engineer-year", "1.5 yr"},
		{550, "3 engineer-years", "2.5 yr"},
	}
	for _, tt := range tests {
		if got := human(tt.days, tt.days); got != tt.long {
			t.Errorf("human(%v, %v) = %q, want %q", tt.days, tt.days, got, tt.long)
		}
		if got := humanShort(tt.days, tt.days); got != tt.short {
			t.Errorf("humanShort(%v, %v) = %q, want %q", tt.days, tt.days, got, tt.short)
		}
	}
}

// TestHumanRange checks the range phrase: one unit, the high end's, with
// the low end in it at least 1, and one figure when both ends round the
// same.
func TestHumanRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		low, high   float64
		long, short string
	}{
		{"days", 3, 4.9, "3 to 5 days", "3–5 d"},
		{"days collapse", 4.6, 4.9, "5 days", "5 d"},
		{"low end under a day", 0.2, 2, "1 to 2 days", "1–2 d"},
		{"weeks from days", 3, 5, "1 week", "1 wk"},
		{"weeks", 6, 20.9, "1 to 4 weeks", "1–4 wk"},
		{"months from weeks", 12, 21, "1 month", "1 mo"},
		{"months", 30, 219, "1 to 10 months", "1–10 mo"},
		{"years from months", 150, 220, "1 engineer-year", "0.7–1 yr"},
		{"years", 1760, 2420, "8 to 11 engineer-years", "8–11 yr"},
		{"years collapse", 2410, 2420, "11 engineer-years", "11 yr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := human(tt.low, tt.high); got != tt.long {
				t.Errorf("human(%v, %v) = %q, want %q", tt.low, tt.high, got, tt.long)
			}
			if got := humanShort(tt.low, tt.high); got != tt.short {
				t.Errorf("humanShort(%v, %v) = %q, want %q", tt.low, tt.high, got, tt.short)
			}
		})
	}
}

func TestSittings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		passes     float64
		band, text string
	}{
		{0, "one sitting", "one sitting"},
		{1, "one sitting", "one sitting"},
		{1.1, "a few sittings", "2 sittings"},
		{3, "a few sittings", "3 sittings"},
		{3.1, "split it first", "4 sittings"},
	}
	for _, tt := range tests {
		if got := sittingsBand(sittings(tt.passes)); got != tt.band {
			t.Errorf("sittings band of %v = %q, want %q", tt.passes, got, tt.band)
		}
		if got := sittingsText(tt.passes); got != tt.text {
			t.Errorf("sittingsText(%v) = %q, want %q", tt.passes, got, tt.text)
		}
	}
}

func TestOverMax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		limit       string
		by, allowed float64
		ok          bool
	}{
		{"over max by 9980, allowed 150; tokens_est 148530 -> 154081, over max by 138081, allowed 2000", 9980, 150, true},
		{"over max by 1,143, allowed 5", 1143, 5, true},
		{"max_delta +0", 0, 0, false},
		{"max 10", 0, 0, false},
		{"over max by x, allowed 5", 0, 0, false},
	}
	for _, tt := range tests {
		by, allowed, ok := overMax(tt.limit)
		if by != tt.by || allowed != tt.allowed || ok != tt.ok {
			t.Errorf("overMax(%q) = %v, %v, %v, want %v, %v, %v", tt.limit, by, allowed, ok, tt.by, tt.allowed, tt.ok)
		}
	}
}

func TestModuleName(t *testing.T) {
	t.Parallel()

	tests := []struct{ path, root, want string }{
		{"example.com/sixtyone", "x", "Sixtyone"},
		{"example.com/app/v2", "x", "App"},
		{"", "web", "Web"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := moduleName(tt.path, tt.root); got != tt.want {
			t.Errorf("moduleName(%q, %q) = %q, want %q", tt.path, tt.root, got, tt.want)
		}
	}
}
