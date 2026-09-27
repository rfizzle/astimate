package gate_test

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

func ptr[T any](v T) *T { return &v }

func density(metric string, maxDelta float64, limit *float64) gate.Threshold {
	return gate.Threshold{Metric: metric, Kind: gate.Density, MaxDelta: ptr(maxDelta), Max: limit}
}

func capacity(metric string, limit, warnAt float64) gate.Threshold {
	return gate.Threshold{Metric: metric, Kind: gate.Capacity, Max: ptr(limit), WarnAt: warnAt}
}

func require(metric string, want bool, when *gate.Condition) gate.Threshold {
	return gate.Threshold{Metric: metric, Kind: gate.Requirement, Require: ptr(want), When: when}
}

func TestEvaluateRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		head  metrics.RawMetrics
		base  *metrics.RawMetrics
		rules []gate.Threshold
		want  gate.Result
	}{
		{
			name:  "density unchanged at +0",
			head:  metrics.RawMetrics{DupBlocks: 2},
			base:  &metrics.RawMetrics{DupBlocks: 2},
			rules: []gate.Threshold{density("dup_blocks", 0, nil)},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "density decreased at +0",
			head:  metrics.RawMetrics{DupBlocks: 1},
			base:  &metrics.RawMetrics{DupBlocks: 2},
			rules: []gate.Threshold{density("dup_blocks", 0, nil)},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "density up by one at +0",
			head:  metrics.RawMetrics{DupBlocks: 3},
			base:  &metrics.RawMetrics{DupBlocks: 2},
			rules: []gate.Threshold{density("dup_blocks", 0, nil)},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "dup_blocks", Base: 2, Head: 3, HasBase: true, Limit: "max_delta +0"},
			}},
		},
		{
			name:  "negative max_delta met",
			head:  metrics.RawMetrics{Globals: 2},
			base:  &metrics.RawMetrics{Globals: 3},
			rules: []gate.Threshold{density("globals", -1, nil)},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "negative max_delta unmet",
			head:  metrics.RawMetrics{Globals: 3},
			base:  &metrics.RawMetrics{Globals: 3},
			rules: []gate.Threshold{density("globals", -1, nil)},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "globals", Base: 3, Head: 3, HasBase: true, Limit: "max_delta -1"},
			}},
		},
		{
			name:  "fractional delta exactly at limit despite float noise",
			head:  metrics.RawMetrics{DuplicationPct: 2.1},
			base:  &metrics.RawMetrics{DuplicationPct: 1.6},
			rules: []gate.Threshold{density("duplication_pct", 0.5, ptr(5.0))},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "fractional delta over limit",
			head:  metrics.RawMetrics{DuplicationPct: 2.2},
			base:  &metrics.RawMetrics{DuplicationPct: 1.6},
			rules: []gate.Threshold{density("duplication_pct", 0.5, ptr(5.0))},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "duplication_pct", Base: 1.6, Head: 2.2, HasBase: true, Limit: "max_delta +0.5"},
			}},
		},
		{
			name:  "density max at edge",
			head:  metrics.RawMetrics{MaxNesting: 5},
			base:  &metrics.RawMetrics{MaxNesting: 5},
			rules: []gate.Threshold{density("max_nesting", 0, ptr(5.0))},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "density max exceeded while unchanged passes",
			head:  metrics.RawMetrics{MaxNesting: 6},
			base:  &metrics.RawMetrics{MaxNesting: 6},
			rules: []gate.Threshold{density("max_nesting", 0, ptr(5.0))},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "density max exceeded while improving passes",
			head:  metrics.RawMetrics{DuplicationPct: 70},
			base:  &metrics.RawMetrics{DuplicationPct: 80},
			rules: []gate.Threshold{density("duplication_pct", 0.5, ptr(5.0))},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "legacy value rising within delta above max fails on max",
			head:  metrics.RawMetrics{DuplicationPct: 80.4},
			base:  &metrics.RawMetrics{DuplicationPct: 80},
			rules: []gate.Threshold{density("duplication_pct", 0.5, ptr(5.0))},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "duplication_pct", Base: 80, Head: 80.4, HasBase: true, Limit: "max 5"},
			}},
		},
		{
			name:  "density delta and max both broken",
			head:  metrics.RawMetrics{MaxNesting: 7},
			base:  &metrics.RawMetrics{MaxNesting: 6},
			rules: []gate.Threshold{density("max_nesting", 0, ptr(5.0))},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "max_nesting", Base: 6, Head: 7, HasBase: true, Limit: "max_delta +0"},
				{Metric: "max_nesting", Base: 6, Head: 7, HasBase: true, Limit: "max 5"},
			}},
		},
		{
			name:  "capacity below warn band",
			head:  metrics.RawMetrics{SLOC: 74},
			rules: []gate.Threshold{capacity("sloc", 100, 0.75)},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "capacity at warn band lower edge",
			head:  metrics.RawMetrics{SLOC: 75},
			rules: []gate.Threshold{capacity("sloc", 100, 0.75)},
			want: gate.Result{Passed: true, Warnings: []gate.Warning{
				{Metric: "sloc", Head: 75, Limit: "max 100", Suggestion: "at 75% of the 100 ceiling; plan a split before the next feature."},
			}},
		},
		{
			name:  "capacity at max is a warning",
			head:  metrics.RawMetrics{SLOC: 100},
			rules: []gate.Threshold{capacity("sloc", 100, 0.75)},
			want: gate.Result{Passed: true, Warnings: []gate.Warning{
				{Metric: "sloc", Head: 100, Limit: "max 100", Suggestion: "at 100% of the 100 ceiling; plan a split before the next feature."},
			}},
		},
		{
			name:  "capacity over max",
			head:  metrics.RawMetrics{SLOC: 101},
			base:  &metrics.RawMetrics{SLOC: 90},
			rules: []gate.Threshold{capacity("sloc", 100, 0.75)},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "sloc", Base: 90, Head: 101, HasBase: true, Limit: "max 100"},
			}},
		},
		{
			name:  "capacity legacy over max unchanged warns",
			head:  metrics.RawMetrics{TokensEst: 81000},
			base:  &metrics.RawMetrics{TokensEst: 81000},
			rules: []gate.Threshold{capacity("tokens_est", 80000, 0.75)},
			want: gate.Result{Passed: true, Warnings: []gate.Warning{
				{Metric: "tokens_est", Base: 81000, Head: 81000, HasBase: true, Limit: "max 80000", Suggestion: "over the 80000 ceiling (unchanged since baseline); plan a split."},
			}},
		},
		{
			name:  "capacity legacy over max shrinking but still over warns",
			head:  metrics.RawMetrics{TokensEst: 80500},
			base:  &metrics.RawMetrics{TokensEst: 81000},
			rules: []gate.Threshold{capacity("tokens_est", 80000, 0.75)},
			want: gate.Result{Passed: true, Warnings: []gate.Warning{
				{Metric: "tokens_est", Base: 81000, Head: 80500, HasBase: true, Limit: "max 80000", Suggestion: "over the 80000 ceiling (unchanged since baseline); plan a split."},
			}},
		},
		{
			name:  "capacity legacy over max shrinking to max passes with a warning",
			head:  metrics.RawMetrics{TokensEst: 80000},
			base:  &metrics.RawMetrics{TokensEst: 81000},
			rules: []gate.Threshold{capacity("tokens_est", 80000, 0.75)},
			want: gate.Result{Passed: true, Warnings: []gate.Warning{
				{Metric: "tokens_est", Base: 81000, Head: 80000, HasBase: true, Limit: "max 80000", Suggestion: "at 100% of the 80000 ceiling; plan a split before the next feature."},
			}},
		},
		{
			name:  "capacity legacy over max growing fails",
			head:  metrics.RawMetrics{TokensEst: 82000},
			base:  &metrics.RawMetrics{TokensEst: 81000},
			rules: []gate.Threshold{capacity("tokens_est", 80000, 0.75)},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "tokens_est", Base: 81000, Head: 82000, HasBase: true, Limit: "max 80000"},
			}},
		},
		{
			name:  "capacity new package over max fails",
			head:  metrics.RawMetrics{TokensEst: 81000},
			rules: []gate.Threshold{capacity("tokens_est", 80000, 0.75)},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "tokens_est", Head: 81000, Limit: "max 80000"},
			}},
		},
		{
			name:  "capacity doubled under max",
			head:  metrics.RawMetrics{TokensEst: 20000},
			base:  &metrics.RawMetrics{TokensEst: 10000},
			rules: []gate.Threshold{capacity("tokens_est", 30000, 0.75)},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "zero warn_at treated as default",
			head:  metrics.RawMetrics{SLOC: 75},
			rules: []gate.Threshold{capacity("sloc", 100, 0)},
			want: gate.Result{Passed: true, Warnings: []gate.Warning{
				{Metric: "sloc", Head: 75, Limit: "max 100", Suggestion: "at 75% of the 100 ceiling; plan a split before the next feature."},
			}},
		},
		{
			name:  "requirement guard not met",
			head:  metrics.RawMetrics{SLOC: 100},
			rules: []gate.Threshold{require("has_tests", true, &gate.Condition{Metric: "sloc", Value: 100})},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "requirement guard met without tests",
			head:  metrics.RawMetrics{SLOC: 101},
			base:  &metrics.RawMetrics{SLOC: 50},
			rules: []gate.Threshold{require("has_tests", true, &gate.Condition{Metric: "sloc", Value: 100})},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "has_tests", Base: 0, Head: 0, HasBase: true, Limit: "require true"},
			}},
		},
		{
			name:  "requirement unmet by unchanged legacy package passes",
			head:  metrics.RawMetrics{SLOC: 300},
			base:  &metrics.RawMetrics{SLOC: 300},
			rules: []gate.Threshold{require("has_tests", true, &gate.Condition{Metric: "sloc", Value: 100})},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "requirement unmet by shrinking legacy package passes",
			head:  metrics.RawMetrics{SLOC: 250},
			base:  &metrics.RawMetrics{SLOC: 300},
			rules: []gate.Threshold{require("has_tests", true, &gate.Condition{Metric: "sloc", Value: 100})},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "requirement unmet by growing legacy package fails",
			head:  metrics.RawMetrics{SLOC: 301},
			base:  &metrics.RawMetrics{SLOC: 300},
			rules: []gate.Threshold{require("has_tests", true, &gate.Condition{Metric: "sloc", Value: 100})},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "has_tests", Base: 0, Head: 0, HasBase: true, Limit: "require true"},
			}},
		},
		{
			name:  "requirement guard met with tests",
			head:  metrics.RawMetrics{SLOC: 101, HasTests: true},
			rules: []gate.Threshold{require("has_tests", true, &gate.Condition{Metric: "sloc", Value: 100})},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "requirement without guard",
			head:  metrics.RawMetrics{UsesCgo: ptr(true)},
			rules: []gate.Threshold{require("uses_cgo", false, nil)},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "uses_cgo", Head: 1, Limit: "require false"},
			}},
		},
		{
			name:  "requirement guard on uncomputed metric is skipped",
			head:  metrics.RawMetrics{},
			rules: []gate.Threshold{require("has_tests", true, &gate.Condition{Metric: "coverage_pct", Value: 0})},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "v1 metric null at head is skipped",
			head:  metrics.RawMetrics{},
			base:  &metrics.RawMetrics{DupBlocksCrossPkg: ptr(4)},
			rules: []gate.Threshold{density("dup_blocks_cross_pkg", 0, ptr(1.0))},
			want:  gate.Result{Passed: true},
		},
		{
			name:  "v1 metric null at base skips delta with a note but keeps max",
			head:  metrics.RawMetrics{DupBlocksCrossPkg: ptr(4)},
			base:  &metrics.RawMetrics{},
			rules: []gate.Threshold{density("dup_blocks_cross_pkg", 0, ptr(1.0))},
			want: gate.Result{
				Violations: []gate.Violation{{Metric: "dup_blocks_cross_pkg", Head: 4, Limit: "max 1"}},
				Notes:      []gate.Note{{Metric: "dup_blocks_cross_pkg", Text: "not computed at baseline; max_delta skipped"}},
			},
		},
		{
			name:  "rebuild output and unknown names are never evaluated",
			head:  metrics.RawMetrics{},
			rules: []gate.Threshold{capacity("agent_passes", 1, 0.75), density("rebuild_tokens", -1, nil)},
			want:  gate.Result{Passed: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := gate.Evaluate(tt.head, tt.base, tt.rules, nil)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestEvaluateNoBaseline(t *testing.T) {
	t.Parallel()

	untested := density("untested_exports", 0, nil)
	untested.RatchetFromZero = true
	rules := []gate.Threshold{
		untested,
		density("duplication_pct", 0.5, ptr(5.0)),
		density("max_nesting", 0, ptr(5.0)),
		capacity("tokens_est", 30000, 0.75),
	}
	tests := []struct {
		name string
		head metrics.RawMetrics
		want gate.Result
	}{
		{
			name: "ratchet from zero fails on any increase",
			head: metrics.RawMetrics{UntestedExports: 1, TokensEst: 1000},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "untested_exports", Head: 1, Limit: "max_delta +0"},
			}},
		},
		{
			name: "intensive density skips delta without a baseline",
			head: metrics.RawMetrics{DuplicationPct: 4.0, MaxNesting: 4},
			want: gate.Result{Passed: true},
		},
		{
			name: "intensive density keeps its max without a baseline",
			head: metrics.RawMetrics{DuplicationPct: 5.5, MaxNesting: 6},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "duplication_pct", Head: 5.5, Limit: "max 5"},
				{Metric: "max_nesting", Head: 6, Limit: "max 5"},
			}},
		},
		{
			name: "capacity is absolute without a baseline",
			head: metrics.RawMetrics{TokensEst: 30001},
			want: gate.Result{Violations: []gate.Violation{
				{Metric: "tokens_est", Head: 30001, Limit: "max 30000"},
			}},
		},
		{
			name: "capacity warns without a baseline",
			head: metrics.RawMetrics{TokensEst: 24000},
			want: gate.Result{Passed: true, Warnings: []gate.Warning{
				{Metric: "tokens_est", Head: 24000, Limit: "max 30000", Suggestion: "at 80% of the 30000 ceiling; plan a split before the next feature."},
			}},
		},
		{
			name: "new v1 metric has no note without a baseline",
			head: metrics.RawMetrics{DupBlocksCrossPkg: ptr(0)},
			want: gate.Result{Passed: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := gate.Evaluate(tt.head, nil, append(rules, density("dup_blocks_cross_pkg", 0, nil)), nil)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestEvaluateOrderingAndSuggestions(t *testing.T) {
	t.Parallel()

	rules := []gate.Threshold{
		capacity("tokens_est", 30000, 0.75),
		density("untested_exports", 0, nil),
		capacity("exported_symbols", 60, 0.75),
		density("dup_blocks", 0, nil),
		density("cognitive_p90", 3, nil),
		density("dup_blocks_cross_pkg", 0, nil),
		density("coverage_pct", 0, nil),
	}
	head := metrics.RawMetrics{
		TokensEst: 24000, UntestedExports: 2, ExportedSymbols: 50, DupBlocks: 1, CognitiveP90: 20,
		DupBlocksCrossPkg: ptr(1), CoveragePct: ptr(50.0),
	}
	base := &metrics.RawMetrics{CognitiveP90: 16}
	var calls []string
	suggest := func(metric string, h float64, m metrics.RawMetrics) string {
		if m.TokensEst != head.TokensEst {
			t.Errorf("suggester got metrics %+v, want head", m)
		}
		calls = append(calls, metric)
		return "fix " + metric + " at " + strconv.FormatFloat(h, 'f', -1, 64) + "."
	}

	got := gate.Evaluate(head, base, rules, suggest)
	want := gate.Result{
		Violations: []gate.Violation{
			{Metric: "cognitive_p90", Base: 16, Head: 20, HasBase: true, Limit: "max_delta +3", Suggestion: "fix cognitive_p90 at 20."},
			{Metric: "dup_blocks", Base: 0, Head: 1, HasBase: true, Limit: "max_delta +0", Suggestion: "fix dup_blocks at 1."},
			{Metric: "untested_exports", Base: 0, Head: 2, HasBase: true, Limit: "max_delta +0", Suggestion: "fix untested_exports at 2."},
		},
		Warnings: []gate.Warning{
			{Metric: "exported_symbols", Head: 50, HasBase: true, Limit: "max 60", Suggestion: "at 83% of the 60 ceiling; plan a split before the next feature. fix exported_symbols at 50."},
			{Metric: "tokens_est", Head: 24000, HasBase: true, Limit: "max 30000", Suggestion: "at 80% of the 30000 ceiling; plan a split before the next feature. fix tokens_est at 24000."},
		},
		Notes: []gate.Note{
			{Metric: "coverage_pct", Text: "not computed at baseline; max_delta skipped"},
			{Metric: "dup_blocks_cross_pkg", Text: "not computed at baseline; max_delta skipped"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
	}
	if len(calls) != 5 {
		t.Errorf("suggester called for %v, want one call per finding", calls)
	}
}

func TestEvaluateUnchangedOverCeilingSuggestion(t *testing.T) {
	t.Parallel()

	head := metrics.RawMetrics{SLOC: 7000}
	base := head
	suggest := func(metric string, h float64, _ metrics.RawMetrics) string {
		return "split " + metric + " at " + strconv.FormatFloat(h, 'f', -1, 64) + "."
	}
	got := gate.Evaluate(head, &base, []gate.Threshold{capacity("sloc", 6000, 0.75)}, suggest)
	want := gate.Result{Passed: true, Warnings: []gate.Warning{
		{Metric: "sloc", Base: 7000, Head: 7000, HasBase: true, Limit: "max 6000", Suggestion: "over the 6000 ceiling (unchanged since baseline); plan a split. split sloc at 7000."},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
	}
}

func defaultRules(t *testing.T) []gate.Threshold {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatalf("config.Parse(config.Default()) error = %v", err)
	}
	return cfg.Thresholds
}

// healthy is a mid-sized package inside every default threshold and below
// every warn band.
func healthy() metrics.RawMetrics {
	return metrics.RawMetrics{
		Files: 6, SLOC: 1200, LargestFileSLOC: 300, TokensEst: 9000,
		InternalImports: 3, ExportedSymbols: 15, MaxNesting: 3,
		CognitiveP90: 6, DupBlocks: 1, DuplicationPct: 1.0,
		TestFiles: 3, TestFuncs: 12, HasTests: true,
	}
}

// fresh is a realistic package new at head: forty tested exports, ordinary
// nesting and complexity, no duplication, and under every ceiling. Without a
// baseline only ratchet_from_zero rules take max_delta against zero; nesting
// and cognitive_p90 face only their absolute max (SPEC.md section 8.1).
func fresh() metrics.RawMetrics {
	return metrics.RawMetrics{
		Files: 8, SLOC: 2500, LargestFileSLOC: 450, TokensEst: 20000,
		InternalImports: 3, ExportedSymbols: 40, MaxNesting: 3, CognitiveP90: 8,
		TestFiles: 8, TestFuncs: 60, HasTests: true,
	}
}

func TestEvaluateDefaultConfig(t *testing.T) {
	t.Parallel()

	rules := defaultRules(t)

	t.Run("head within all limits", func(t *testing.T) {
		t.Parallel()

		base := healthy()
		got := gate.Evaluate(healthy(), &base, rules, nil)
		if !reflect.DeepEqual(got, gate.Result{Passed: true}) {
			t.Errorf("Evaluate() = %+v, want passed with no findings", got)
		}
	})

	// legacy is a package already past the density ceilings and without
	// tests before the change.
	legacy := func() metrics.RawMetrics {
		return metrics.RawMetrics{
			SLOC: 3000, LargestFileSLOC: 500, TokensEst: 20000, InternalImports: 5,
			ExportedSymbols: 30, Globals: 9, InitFuncs: 2, MaxNesting: 8,
			CognitiveP90: 25, DupBlocks: 14, DuplicationPct: 11.5,
			UntestedExports: 20,
		}
	}

	t.Run("legacy package unchanged passes", func(t *testing.T) {
		t.Parallel()

		base := legacy()
		got := gate.Evaluate(legacy(), &base, rules, nil)
		if !reflect.DeepEqual(got, gate.Result{Passed: true}) {
			t.Errorf("Evaluate() = %+v, want passed with no findings", got)
		}
	})

	t.Run("legacy package rising above max fails", func(t *testing.T) {
		t.Parallel()

		base := legacy()
		head := legacy()
		head.DuplicationPct = 11.8
		head.SLOC = 3010
		got := gate.Evaluate(head, &base, rules, nil)
		want := gate.Result{Violations: []gate.Violation{
			{Metric: "duplication_pct", Base: 11.5, Head: 11.8, HasBase: true, Limit: "max 5"},
			{Metric: "has_tests", Base: 0, Head: 0, HasBase: true, Limit: "require true"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("new package with untested exports fails", func(t *testing.T) {
		t.Parallel()

		head := fresh()
		head.UntestedExports = 3
		got := gate.Evaluate(head, nil, rules, nil)
		want := gate.Result{Violations: []gate.Violation{
			{Metric: "untested_exports", Head: 3, Limit: "max_delta +0"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("new package with realistic nesting and complexity passes", func(t *testing.T) {
		t.Parallel()

		head := fresh()
		got := gate.Evaluate(head, nil, rules, nil)
		if !reflect.DeepEqual(got, gate.Result{Passed: true}) {
			t.Errorf("Evaluate() = %+v, want passed with no findings", got)
		}
	})

	t.Run("new package with one global fails", func(t *testing.T) {
		t.Parallel()

		head := fresh()
		head.Globals = 1
		got := gate.Evaluate(head, nil, rules, nil)
		want := gate.Result{Violations: []gate.Violation{
			{Metric: "globals", Head: 1, Limit: "max_delta +0"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("new package nested past the ceiling fails on max", func(t *testing.T) {
		t.Parallel()

		head := fresh()
		head.MaxNesting = 6
		got := gate.Evaluate(head, nil, rules, nil)
		want := gate.Result{Violations: []gate.Violation{
			{Metric: "max_nesting", Head: 6, Limit: "max 5"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("new package with complexity past the ceiling fails on max", func(t *testing.T) {
		t.Parallel()

		head := fresh()
		head.CognitiveP90 = 26
		got := gate.Evaluate(head, nil, rules, nil)
		want := gate.Result{Violations: []gate.Violation{
			{Metric: "cognitive_p90", Head: 26, Limit: "max 25"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("has_tests required only above 100 sloc", func(t *testing.T) {
		t.Parallel()

		small := metrics.RawMetrics{SLOC: 100}
		if got := gate.Evaluate(small, nil, rules, nil); !got.Passed {
			t.Errorf("sloc 100 without tests: Evaluate() = %+v, want passed", got)
		}
		big := metrics.RawMetrics{SLOC: 101}
		got := gate.Evaluate(big, nil, rules, nil)
		want := gate.Result{Violations: []gate.Violation{
			{Metric: "has_tests", Head: 0, Limit: "require true"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sloc 101 without tests: Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("big feature grows capacity 3x and passes with warnings", func(t *testing.T) {
		t.Parallel()

		base := healthy()
		base.SLOC = 1500
		base.LargestFileSLOC = 150
		base.TokensEst = 9000
		base.InternalImports = 2
		base.ExportedSymbols = 15
		head := base
		head.SLOC *= 3            // 4500: exactly the 75% edge of 6000
		head.LargestFileSLOC *= 3 // 450: below 600
		head.TokensEst *= 3       // 27000: 90% of 30000
		head.InternalImports *= 3 // 6: below 9
		head.ExportedSymbols *= 3 // 45: exactly the 75% edge of 60
		head.Files *= 3
		head.FuncCount *= 3
		head.TestFuncs *= 3

		got := gate.Evaluate(head, &base, rules, nil)
		want := gate.Result{Passed: true, Warnings: []gate.Warning{
			{Metric: "exported_symbols", Base: 15, Head: 45, HasBase: true, Limit: "max 60", Suggestion: "at 75% of the 60 ceiling; plan a split before the next feature."},
			{Metric: "sloc", Base: 1500, Head: 4500, HasBase: true, Limit: "max 6000", Suggestion: "at 75% of the 6000 ceiling; plan a split before the next feature."},
			{Metric: "tokens_est", Base: 9000, Head: 27000, HasBase: true, Limit: "max 30000", Suggestion: "at 90% of the 30000 ceiling; plan a split before the next feature."},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})
}
