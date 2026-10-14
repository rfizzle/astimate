package gate_test

import (
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// TestEvaluateBaselineUnavailable checks that a row whose baseline is
// unavailable is judged like a new package against the maxes, but is not
// ratcheted from zero and is not held to a requirement.
func TestEvaluateBaselineUnavailable(t *testing.T) {
	t.Parallel()

	dup := density("dup_blocks", 0, nil)
	dup.RatchetFromZero = true
	rules := []gate.Threshold{
		dup,
		density("duplication_pct", 0.5, ptr(40.0)),
		capacity("sloc", 100, 0.75),
		require("has_tests", true, nil),
	}
	tests := []struct {
		name        string
		head        metrics.RawMetrics
		unavailable bool
		want        []string
	}{
		{
			name: "new package ratchets from zero",
			head: metrics.RawMetrics{DupBlocks: 2, SLOC: 10, HasTests: true},
			want: []string{"dup_blocks"},
		},
		{
			name:        "unavailable baseline skips the ratchet",
			head:        metrics.RawMetrics{DupBlocks: 2, SLOC: 10, HasTests: true},
			unavailable: true,
		},
		{
			name:        "unavailable baseline still faces the density max",
			head:        metrics.RawMetrics{DupBlocks: 2, SLOC: 10, DuplicationPct: 60, HasTests: true},
			unavailable: true,
			want:        []string{"duplication_pct"},
		},
		{
			name:        "unavailable baseline still faces the capacity max",
			head:        metrics.RawMetrics{SLOC: 150, HasTests: true},
			unavailable: true,
			want:        []string{"sloc"},
		},
		{
			name:        "unavailable baseline judges no requirement",
			head:        metrics.RawMetrics{SLOC: 10},
			unavailable: true,
		},
		{
			name: "new package judges the requirement",
			head: metrics.RawMetrics{SLOC: 10},
			want: []string{"has_tests"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var opts []gate.Option
			if tt.unavailable {
				opts = append(opts, gate.WithBaselineUnavailable())
			}
			res := gate.Evaluate(tt.head, nil, rules, nil, opts...)
			var got []string
			for _, v := range res.Violations {
				got = append(got, v.Metric)
			}
			if len(got) != len(tt.want) || (len(got) > 0 && got[0] != tt.want[0]) {
				t.Errorf("violations %v, want %v", got, tt.want)
			}
			if res.Passed != (len(tt.want) == 0) {
				t.Errorf("passed %v, want %v", res.Passed, len(tt.want) == 0)
			}
		})
	}
}
