package gate_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// TestEvaluateSeverity checks each kind's breach under both severities: a
// fail rule's is a violation, and the same rule marked warn reports the
// identical finding as a warning with severity warn and passes.
func TestEvaluateSeverity(t *testing.T) {
	t.Parallel()

	suggest := func(metric string, _ float64, _ metrics.RawMetrics) string { return "Fix " + metric + "." }
	overMax := capacity("sloc", 1000, 0.75)
	overMax.OverMaxDelta = ptr(100.0)
	tests := []struct {
		name string
		rule gate.Threshold
		head metrics.RawMetrics
		base *metrics.RawMetrics
		want gate.Violation
	}{
		{
			name: "density max_delta",
			rule: density("dup_blocks", 0, nil),
			head: metrics.RawMetrics{DupBlocks: 3}, base: &metrics.RawMetrics{DupBlocks: 2},
			want: gate.Violation{Metric: "dup_blocks", Base: 2, Head: 3, HasBase: true, Limit: "max_delta +0", Suggestion: "Fix dup_blocks."},
		},
		{
			name: "density max on a new package",
			rule: density("max_nesting", 0, ptr(5.0)),
			head: metrics.RawMetrics{MaxNesting: 6},
			want: gate.Violation{Metric: "max_nesting", Head: 6, Limit: "max 5", Suggestion: "Fix max_nesting."},
		},
		{
			name: "capacity crossing max",
			rule: capacity("sloc", 1000, 0.75),
			head: metrics.RawMetrics{SLOC: 1010}, base: &metrics.RawMetrics{SLOC: 950},
			want: gate.Violation{Metric: "sloc", Base: 950, Head: 1010, HasBase: true, Limit: "max 1000", Suggestion: "Fix sloc."},
		},
		{
			name: "capacity growth past over_max_delta",
			rule: overMax,
			head: metrics.RawMetrics{SLOC: 1200}, base: &metrics.RawMetrics{SLOC: 1050},
			want: gate.Violation{Metric: "sloc", Base: 1050, Head: 1200, HasBase: true, Limit: "over max by 200, allowed 100",
				Suggestion: "grew 150 while over the 1000 ceiling, more than the 100 one change may add there; split the package. Fix sloc."},
		},
		{
			name: "requirement",
			rule: require("has_tests", true, &gate.Condition{Metric: "sloc", Value: 100}),
			head: metrics.RawMetrics{SLOC: 200}, base: &metrics.RawMetrics{SLOC: 150},
			want: gate.Violation{Metric: "has_tests", HasBase: true, Limit: "require true", Suggestion: "Fix has_tests."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, sev := range []gate.Severity{"", gate.SeverityFail} {
				rule := tt.rule
				rule.Severity = sev
				got := gate.Evaluate(tt.head, tt.base, []gate.Threshold{rule}, suggest)
				want := gate.Result{Violations: []gate.Violation{tt.want}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("severity %q: Evaluate() =\n%+v\nwant\n%+v", sev, got, want)
				}
			}
			rule := tt.rule
			rule.Severity = gate.SeverityWarn
			got := gate.Evaluate(tt.head, tt.base, []gate.Threshold{rule}, suggest)
			w := tt.want
			w.Severity = gate.SeverityWarn
			want := gate.Result{Passed: true, Warnings: []gate.Warning{w}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("severity warn: Evaluate() =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// TestEvaluateSeverityMixed checks that a warn rule's own warnings, which
// were never violations, carry no severity, that its breach sorts among
// the other warnings by metric, and that a fail rule beside it still
// fails.
func TestEvaluateSeverityMixed(t *testing.T) {
	t.Parallel()

	band := capacity("tokens_est", 16000, 0.75)
	band.Severity = gate.SeverityWarn
	dup := density("dup_blocks", 0, nil)
	dup.Severity = gate.SeverityWarn
	got := gate.Evaluate(metrics.RawMetrics{TokensEst: 13000, DupBlocks: 1, Globals: 1},
		&metrics.RawMetrics{TokensEst: 12000}, []gate.Threshold{band, dup, density("globals", 0, nil)}, nil)
	want := gate.Result{
		Violations: []gate.Violation{{Metric: "globals", Head: 1, HasBase: true, Limit: "max_delta +0"}},
		Warnings: []gate.Warning{
			{Metric: "dup_blocks", Head: 1, HasBase: true, Limit: "max_delta +0", Severity: gate.SeverityWarn},
			{Metric: "tokens_est", Base: 12000, Head: 13000, HasBase: true, Limit: "max 16000",
				Suggestion: "at 81% of the 16000 ceiling; plan a split before the next feature."},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Evaluate() =\n%+v\nwant\n%+v", got, want)
	}
}

// TestExemptionsApplyWarnRule checks that an exemption matches a warn
// rule's breach and records it under exempted, as it would the violation,
// while an ordinary capacity warning on an exempted metric stays a
// warning.
func TestExemptionsApplyWarnRule(t *testing.T) {
	t.Parallel()

	dup := density("dup_blocks", 0, nil)
	dup.Severity = gate.SeverityWarn
	band := capacity("tokens_est", 16000, 0.75)
	res := gate.Evaluate(metrics.RawMetrics{DupBlocks: 1, TokensEst: 13000}, &metrics.RawMetrics{TokensEst: 13000},
		[]gate.Threshold{dup, band}, nil)
	x := gate.NewExemptions([]gate.Exemption{
		{Package: "internal/x", Metric: "dup_blocks", Reason: "generated tables"},
		{Package: "internal/x", Metric: "tokens_est", Reason: "big on purpose"},
	}, time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC))
	x.Apply("internal/x", &res)
	want := gate.Result{
		Passed:     true,
		Violations: []gate.Violation{},
		Warnings: []gate.Warning{{Metric: "tokens_est", Base: 13000, Head: 13000, HasBase: true, Limit: "max 16000",
			Suggestion: "at 81% of the 16000 ceiling; plan a split before the next feature."}},
		Exempted: []gate.Exempted{{
			Violation: gate.Violation{Metric: "dup_blocks", Head: 1, HasBase: true, Limit: "max_delta +0", Severity: gate.SeverityWarn},
			Reason:    "generated tables",
		}},
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("Apply() =\n%+v\nwant\n%+v", res, want)
	}
	stale := x.Stale(func(gate.Exemption) bool { return false })
	if len(stale) != 1 || stale[0].Metric != "tokens_est" {
		t.Errorf("stale = %+v, want only the tokens_est exemption", stale)
	}
}
