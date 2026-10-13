package gate_test

import (
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// TestEvaluateMerge checks each pair of the merge table under the default
// rules and the cases that keep two findings apart: one finding per cause,
// attributed to the first rule of its pair, whose limit names both and
// whose Also lists the merged rules.
func TestEvaluateMerge(t *testing.T) {
	t.Parallel()

	rules := defaultRules(t)
	with := func(f func(*metrics.RawMetrics)) metrics.RawMetrics {
		m := healthy()
		f(&m)
		return m
	}
	tests := []struct {
		name       string
		head, base metrics.RawMetrics
		want       []gate.Violation
	}{
		{
			name: "duplication_pct crossing max 40 by a rise of 7 is one finding",
			base: with(func(m *metrics.RawMetrics) { m.DuplicationPct = 38 }),
			head: with(func(m *metrics.RawMetrics) { m.DuplicationPct = 45 }),
			want: []gate.Violation{
				{Metric: "duplication_pct", Base: 38, Head: 45, HasBase: true, Limit: "max_delta +6 and max 40"},
			},
		},
		{
			name: "dup_blocks carries duplication_pct",
			base: with(func(m *metrics.RawMetrics) { m.DupBlocks, m.DuplicationPct = 1, 2.5 }),
			head: with(func(m *metrics.RawMetrics) { m.DupBlocks, m.DuplicationPct = 3, 12.25 }),
			want: []gate.Violation{
				{Metric: "dup_blocks", Base: 1, Head: 3, HasBase: true,
					Limit: "max_delta +0; duplication_pct 2.5 -> 12.25, max_delta +6", Also: []string{"duplication_pct"}},
			},
		},
		{
			name: "dup_blocks carries duplication_pct already merged across max_delta and max",
			base: with(func(m *metrics.RawMetrics) { m.DupBlocks, m.DuplicationPct = 1, 38 }),
			head: with(func(m *metrics.RawMetrics) { m.DupBlocks, m.DuplicationPct = 2, 45 }),
			want: []gate.Violation{
				{Metric: "dup_blocks", Base: 1, Head: 2, HasBase: true,
					Limit: "max_delta +0; duplication_pct 38 -> 45, max_delta +6 and max 40", Also: []string{"duplication_pct"}},
			},
		},
		{
			name: "sloc carries tokens_est on the same growth",
			base: with(func(m *metrics.RawMetrics) { m.SLOC, m.TokensEst = 900, 15000 }),
			head: with(func(m *metrics.RawMetrics) { m.SLOC, m.TokensEst = 1100, 17500 }),
			want: []gate.Violation{
				{Metric: "sloc", Base: 900, Head: 1100, HasBase: true,
					Limit: "max 1000; tokens_est 15000 -> 17500, max 16000", Also: []string{"tokens_est"}},
			},
		},
		{
			name: "tokens_est alone is reported when sloc did not fire",
			base: with(func(m *metrics.RawMetrics) { m.TokensEst = 15000 }),
			head: with(func(m *metrics.RawMetrics) { m.TokensEst = 17500 }),
			want: []gate.Violation{
				{Metric: "tokens_est", Base: 15000, Head: 17500, HasBase: true, Limit: "max 16000"},
			},
		},
		{
			name: "duplication_pct alone is reported when dup_blocks did not fire",
			base: with(func(m *metrics.RawMetrics) { m.DuplicationPct = 2 }),
			head: with(func(m *metrics.RawMetrics) { m.DuplicationPct = 9 }),
			want: []gate.Violation{
				{Metric: "duplication_pct", Base: 2, Head: 9, HasBase: true, Limit: "max_delta +6"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := gate.Evaluate(tt.head, &tt.base, rules, nil)
			if !reflect.DeepEqual(got.Violations, tt.want) || got.Passed {
				t.Errorf("Evaluate().Violations =\n%+v\nwant\n%+v", got.Violations, tt.want)
			}
		})
	}
}

// TestEvaluateMergeKeepsApart checks the findings a merge leaves apart:
// two rules whose values moved in different directions, and a violation
// and a warn rule's breach, which would change what fails if merged.
func TestEvaluateMergeKeepsApart(t *testing.T) {
	t.Parallel()

	sloc := density("sloc", -1, nil) // demands a shrink, so unchanged sloc fires
	tokens := capacity("tokens_est", 16000, 0.75)
	head := metrics.RawMetrics{SLOC: 900, TokensEst: 17000}
	base := metrics.RawMetrics{SLOC: 900, TokensEst: 15000}

	t.Run("different directions", func(t *testing.T) {
		t.Parallel()

		got := gate.Evaluate(head, &base, []gate.Threshold{sloc, tokens}, nil)
		want := []gate.Violation{
			{Metric: "sloc", Base: 900, Head: 900, HasBase: true, Limit: "max_delta -1"},
			{Metric: "tokens_est", Base: 15000, Head: 17000, HasBase: true, Limit: "max 16000"},
		}
		if !reflect.DeepEqual(got.Violations, want) {
			t.Errorf("Evaluate().Violations =\n%+v\nwant\n%+v", got.Violations, want)
		}
	})

	grow := density("sloc", 0, nil)
	grown := head
	grown.SLOC = 1000

	t.Run("violation and warn rule breach", func(t *testing.T) {
		t.Parallel()

		warn := tokens
		warn.Severity = gate.SeverityWarn
		got := gate.Evaluate(grown, &base, []gate.Threshold{grow, warn}, nil)
		want := gate.Result{
			Violations: []gate.Violation{{Metric: "sloc", Base: 900, Head: 1000, HasBase: true, Limit: "max_delta +0"}},
			Warnings: []gate.Warning{{Metric: "tokens_est", Base: 15000, Head: 17000, HasBase: true, Limit: "max 16000",
				Severity: gate.SeverityWarn}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})

	t.Run("two warn rule breaches merge as a warning", func(t *testing.T) {
		t.Parallel()

		warnSLOC, warnTokens := grow, tokens
		warnSLOC.Severity, warnTokens.Severity = gate.SeverityWarn, gate.SeverityWarn
		got := gate.Evaluate(grown, &base, []gate.Threshold{warnTokens, warnSLOC}, nil)
		want := gate.Result{Passed: true, Warnings: []gate.Warning{{Metric: "sloc", Base: 900, Head: 1000, HasBase: true,
			Limit: "max_delta +0; tokens_est 15000 -> 17000, max 16000", Severity: gate.SeverityWarn, Also: []string{"tokens_est"}}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
		}
	})
}

// TestEvaluateMergeLeavesCapacityWarnings checks that a capacity rule's
// ordinary warnings, which each name their own headroom, never merge.
func TestEvaluateMergeLeavesCapacityWarnings(t *testing.T) {
	t.Parallel()

	head := metrics.RawMetrics{SLOC: 800, TokensEst: 13000}
	got := gate.Evaluate(head, nil, []gate.Threshold{capacity("sloc", 1000, 0.75), capacity("tokens_est", 16000, 0.75)}, nil)
	if len(got.Warnings) != 2 || got.Warnings[0].Also != nil || got.Warnings[1].Also != nil {
		t.Errorf("Evaluate().Warnings = %+v, want sloc and tokens_est apart", got.Warnings)
	}
}

func TestMergedInto(t *testing.T) {
	t.Parallel()

	for metric, want := range map[string]string{"tokens_est": "sloc", "duplication_pct": "dup_blocks", "sloc": "", "globals": ""} {
		got, ok := gate.MergedInto(metric)
		if got != want || ok != (want != "") {
			t.Errorf("MergedInto(%q) = %q, %t, want %q", metric, got, ok, want)
		}
	}
}
