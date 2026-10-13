package gate_test

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

func TestThresholdValidateSource(t *testing.T) {
	t.Parallel()

	known := func(string) bool { return true }
	tests := []struct {
		name    string
		metric  string
		source  string
		wantErr string
	}{
		{"static", "untested_exports", "", ""},
		{"coverage on untested_exports", "untested_exports", "coverage", ""},
		{"unknown source", "untested_exports", "profile", `source: unknown source "profile"; want coverage`},
		{"coverage on another metric", "globals", "coverage", "source: coverage applies only to untested_exports"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			th := gate.Threshold{Metric: tt.metric, Kind: gate.Density, MaxDelta: ptr(0.0), Source: tt.source}
			err := th.Validate(known)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestEvaluateSourceCoverage checks that a rule with source coverage
// judges untested_exports_covered when head and the baseline both have it,
// falls back to untested_exports otherwise, and that a static rule never
// reads the covered count.
func TestEvaluateSourceCoverage(t *testing.T) {
	t.Parallel()

	rows := func(static, covered *int) metrics.RawMetrics {
		m := metrics.RawMetrics{UntestedExportsCovered: covered}
		if static != nil {
			m.UntestedExports = *static
		}
		return m
	}
	n := func(v int) *int { return &v }
	tests := []struct {
		name      string
		source    string
		head      metrics.RawMetrics
		base      *metrics.RawMetrics
		wantFail  bool
		wantLimit string
	}{
		{
			name:   "covered count settles an export a static rule fails",
			source: "coverage",
			head:   rows(n(5), n(2)),
			base:   ptr(rows(n(4), n(2))),
		},
		{
			name:      "covered count rising fails, named in the limit",
			source:    "coverage",
			head:      rows(n(5), n(3)),
			base:      ptr(rows(n(5), n(2))),
			wantFail:  true,
			wantLimit: "max_delta +0 on untested_exports_covered",
		},
		{
			name:      "static rule ignores the covered count",
			head:      rows(n(5), n(2)),
			base:      ptr(rows(n(4), n(2))),
			wantFail:  true,
			wantLimit: "max_delta +0",
		},
		{
			name:      "no covered count at head falls back to static",
			source:    "coverage",
			head:      rows(n(5), nil),
			base:      ptr(rows(n(4), n(1))),
			wantFail:  true,
			wantLimit: "max_delta +0",
		},
		{
			name:      "no covered count at the baseline falls back to static on both sides",
			source:    "coverage",
			head:      rows(n(5), n(0)),
			base:      ptr(rows(n(4), nil)),
			wantFail:  true,
			wantLimit: "max_delta +0",
		},
		{
			name:   "new package ratchets the covered count from zero",
			source: "coverage",
			head:   rows(n(3), n(0)),
		},
		{
			name:      "new package with an export no test runs fails",
			source:    "coverage",
			head:      rows(n(3), n(1)),
			wantFail:  true,
			wantLimit: "max_delta +0 on untested_exports_covered",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rule := gate.Threshold{Metric: "untested_exports", Kind: gate.Density, MaxDelta: ptr(0.0), RatchetFromZero: true, Source: tt.source}
			res := gate.Evaluate(tt.head, tt.base, []gate.Threshold{rule}, nil)
			if res.Passed == tt.wantFail {
				t.Fatalf("Passed = %v, want %v; violations %+v", res.Passed, !tt.wantFail, res.Violations)
			}
			if !tt.wantFail {
				return
			}
			v := res.Violations[0]
			if v.Metric != "untested_exports" || v.Limit != tt.wantLimit {
				t.Errorf("violation = %s %q, want untested_exports %q", v.Metric, v.Limit, tt.wantLimit)
			}
		})
	}
}
