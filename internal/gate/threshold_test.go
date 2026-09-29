package gate

import (
	"math"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestParseCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    Condition
		wantErr bool
	}{
		{in: "sloc > 100", want: Condition{Metric: "sloc", Value: 100}},
		{in: "  sloc>100  ", want: Condition{Metric: "sloc", Value: 100}},
		{in: "duplication_pct > 2.5", want: Condition{Metric: "duplication_pct", Value: 2.5}},
		{in: "fan_in > -1", want: Condition{Metric: "fan_in", Value: -1}},
		{in: "", wantErr: true},
		{in: "sloc", wantErr: true},
		{in: "sloc < 100", wantErr: true},
		{in: "sloc >= 100", wantErr: true},
		{in: "sloc >> 100", wantErr: true},
		{in: "> 100", wantErr: true},
		{in: "sloc files > 100", wantErr: true},
		{in: "sloc > abc", wantErr: true},
		{in: "sloc >", wantErr: true},
		{in: "sloc > NaN", wantErr: true},
		{in: "sloc > Inf", wantErr: true},
		{in: "sloc > 100 && files > 1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseCondition(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCondition(%q) = %+v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCondition(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseCondition(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestThresholdValidate(t *testing.T) {
	t.Parallel()

	known := func(n string) bool { return n == "sloc" || n == "dup_blocks" || n == "has_tests" || n == "tokens_est" }
	tests := []struct {
		name    string
		th      Threshold
		wantErr string
	}{
		{name: "density", th: Threshold{Metric: "dup_blocks", Kind: Density, MaxDelta: ptr(0.0)}},
		{name: "density with max", th: Threshold{Metric: "dup_blocks", Kind: Density, MaxDelta: ptr(0.5), Max: ptr(5.0)}},
		{name: "capacity", th: Threshold{Metric: "tokens_est", Kind: Capacity, Max: ptr(30000.0), WarnAt: 0.75}},
		{name: "requirement with when", th: Threshold{Metric: "has_tests", Kind: Requirement, Require: ptr(true), When: &Condition{Metric: "sloc", Value: 100}}},
		{name: "warn severity", th: Threshold{Metric: "dup_blocks", Kind: Density, MaxDelta: ptr(0.0), Severity: SeverityWarn}},
		{name: "fail severity", th: Threshold{Metric: "has_tests", Kind: Requirement, Require: ptr(true), Severity: SeverityFail}},
		{name: "unknown severity", th: Threshold{Metric: "tokens_est", Kind: Capacity, Max: ptr(1.0), WarnAt: 0.75, Severity: "error"}, wantErr: `"tokens_est": unknown severity "error"; want fail or warn`},
		{name: "unknown metric", th: Threshold{Metric: "nope", Kind: Density, MaxDelta: ptr(0.0)}, wantErr: `"nope": unknown metric`},
		{name: "empty metric", th: Threshold{Kind: Density, MaxDelta: ptr(0.0)}, wantErr: "metric is required"},
		{name: "missing kind", th: Threshold{Metric: "sloc", Max: ptr(1.0)}, wantErr: "kind is required"},
		{name: "unknown kind", th: Threshold{Metric: "sloc", Kind: "ceiling"}, wantErr: "unknown kind"},
		{name: "density no limit", th: Threshold{Metric: "dup_blocks", Kind: Density}, wantErr: `"dup_blocks": density rule needs max_delta or max`},
		{name: "density with max only", th: Threshold{Metric: "dup_blocks", Kind: Density, Max: ptr(30.0)}},
		{name: "ratchet_from_zero without max_delta", th: Threshold{Metric: "dup_blocks", Kind: Density, Max: ptr(3.0), RatchetFromZero: true}, wantErr: `"dup_blocks": ratchet_from_zero needs max_delta`},
		{name: "density with warn_at", th: Threshold{Metric: "dup_blocks", Kind: Density, MaxDelta: ptr(0.0), WarnAt: 0.5}, wantErr: "warn_at"},
		{name: "capacity no limit", th: Threshold{Metric: "tokens_est", Kind: Capacity, WarnAt: 0.75}, wantErr: `"tokens_est": capacity rule needs max`},
		{name: "capacity non-positive max", th: Threshold{Metric: "tokens_est", Kind: Capacity, Max: ptr(0.0), WarnAt: 0.75}, wantErr: "max must be > 0"},
		{name: "capacity with max_delta", th: Threshold{Metric: "tokens_est", Kind: Capacity, Max: ptr(1.0), MaxDelta: ptr(0.0), WarnAt: 0.75}, wantErr: "must not set max_delta"},
		{name: "capacity warn_at zero", th: Threshold{Metric: "tokens_est", Kind: Capacity, Max: ptr(1.0)}, wantErr: "warn_at must be in (0, 1)"},
		{name: "capacity warn_at one", th: Threshold{Metric: "tokens_est", Kind: Capacity, Max: ptr(1.0), WarnAt: 1}, wantErr: "warn_at must be in (0, 1)"},
		{name: "requirement no require", th: Threshold{Metric: "has_tests", Kind: Requirement}, wantErr: `"has_tests": requirement rule needs require`},
		{name: "requirement with max", th: Threshold{Metric: "has_tests", Kind: Requirement, Require: ptr(true), Max: ptr(1.0)}, wantErr: "must not set max"},
		{name: "density ratchet from zero", th: Threshold{Metric: "dup_blocks", Kind: Density, MaxDelta: ptr(0.0), RatchetFromZero: true}},
		{name: "capacity ratchet from zero", th: Threshold{Metric: "tokens_est", Kind: Capacity, Max: ptr(1.0), WarnAt: 0.75, RatchetFromZero: true}, wantErr: `"tokens_est": ratchet_from_zero applies only to density rules`},
		{name: "requirement ratchet from zero", th: Threshold{Metric: "has_tests", Kind: Requirement, Require: ptr(true), RatchetFromZero: true}, wantErr: `"has_tests": ratchet_from_zero applies only to density rules`},
		{name: "require on density", th: Threshold{Metric: "dup_blocks", Kind: Density, MaxDelta: ptr(0.0), Require: ptr(true)}, wantErr: "require applies only"},
		{name: "capacity over_max_delta", th: Threshold{Metric: "sloc", Kind: Capacity, Max: ptr(1000.0), WarnAt: 0.75, OverMaxDelta: ptr(100.0)}},
		{name: "capacity over_max_delta zero", th: Threshold{Metric: "sloc", Kind: Capacity, Max: ptr(1000.0), WarnAt: 0.75, OverMaxDelta: ptr(0.0)}},
		{name: "capacity negative over_max_delta", th: Threshold{Metric: "sloc", Kind: Capacity, Max: ptr(1000.0), WarnAt: 0.75, OverMaxDelta: ptr(-1.0)}, wantErr: `"sloc": over_max_delta must be a finite number >= 0, got -1`},
		{name: "capacity NaN over_max_delta", th: Threshold{Metric: "sloc", Kind: Capacity, Max: ptr(1000.0), WarnAt: 0.75, OverMaxDelta: ptr(math.NaN())}, wantErr: "over_max_delta must be a finite number >= 0"},
		{name: "capacity infinite over_max_delta", th: Threshold{Metric: "sloc", Kind: Capacity, Max: ptr(1000.0), WarnAt: 0.75, OverMaxDelta: ptr(math.Inf(1))}, wantErr: "over_max_delta must be a finite number >= 0"},
		{name: "density over_max_delta", th: Threshold{Metric: "dup_blocks", Kind: Density, MaxDelta: ptr(0.0), OverMaxDelta: ptr(0.0)}, wantErr: `"dup_blocks": over_max_delta applies only to capacity rules`},
		{name: "requirement over_max_delta", th: Threshold{Metric: "has_tests", Kind: Requirement, Require: ptr(true), OverMaxDelta: ptr(10.0)}, wantErr: `"has_tests": over_max_delta applies only to capacity rules`},
		{name: "when unknown metric", th: Threshold{Metric: "has_tests", Kind: Requirement, Require: ptr(true), When: &Condition{Metric: "nope"}}, wantErr: `when: unknown metric "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.th.Validate(known)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
