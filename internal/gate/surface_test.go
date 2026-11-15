package gate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/gate/exempt"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/role"
)

// TestReshaped checks the reshaped-exports heads-up of a capacity rule on
// exported_symbols (SPEC.md 8.1): a warning on a row with a baseline value
// in the warn band or over the max, never a violation, and nothing below
// the band, without a baseline, for a role the rule leaves out or without
// names.
func TestReshaped(t *testing.T) {
	t.Parallel()

	max60, over5 := 60.0, 5.0
	rule := gate.Threshold{Metric: gate.SurfaceMetric, Kind: gate.Capacity, Max: &max60, WarnAt: 0.75, OverMaxDelta: &over5,
		Roles: []string{role.Library, role.TestSupport, role.GeneratedOnly}}
	warnRule := rule
	warnRule.Severity = gate.SeverityWarn
	sig := []string{"Store.SetLatest (signature changed)"}
	tests := []struct {
		name       string
		rule       gate.Threshold
		base, head int
		noBase     bool
		role       string
		names      []string
		// want is the heads-up's suggestion, empty for none.
		want       string
		wantPassed bool
	}{
		{name: "at the ceiling", rule: rule, base: 60, head: 60, names: sig, wantPassed: true,
			want: "Reshaped without adding an export: Store.SetLatest (signature changed). Changes like these"},
		{name: "in the warn band", rule: rule, base: 46, head: 45, names: []string{"F (signature changed)", "Options (unexported as options)"},
			wantPassed: true, want: "Reshaped without adding an export: F (signature changed) and Options (unexported as options)."},
		{name: "over the max within the allowance", rule: rule, base: 62, head: 64, names: sig, wantPassed: true, want: "Store.SetLatest"},
		{name: "over the max past the allowance fails on its own", rule: rule, base: 50, head: 70, names: sig, want: "Store.SetLatest"},
		{name: "names capped", rule: rule, base: 60, head: 60, names: []string{"A", "B", "C", "D", "E", "F", "G"}, wantPassed: true,
			want: ": A, B, C, D, E and 2 more."},
		{name: "warn rule", rule: warnRule, base: 60, head: 60, names: sig, wantPassed: true, want: "Store.SetLatest"},
		{name: "below the warn band", rule: rule, base: 44, head: 44, names: sig, wantPassed: true},
		{name: "no baseline", rule: rule, head: 60, noBase: true, names: sig, wantPassed: true},
		{name: "main package", rule: rule, base: 60, head: 60, role: role.Main, names: sig, wantPassed: true},
		{name: "nothing reshaped", rule: rule, base: 60, head: 60, wantPassed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			head := metrics.RawMetrics{ExportedSymbols: tt.head, Role: tt.role}
			var base *metrics.RawMetrics
			if !tt.noBase {
				base = &metrics.RawMetrics{ExportedSymbols: tt.base}
			}
			res := gate.Evaluate(head, base, []gate.Threshold{tt.rule}, nil, gate.WithReshaped(tt.names))
			if res.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v: %+v", res.Passed, tt.wantPassed, res)
			}
			var got []gate.Warning
			for _, w := range res.Warnings {
				if gate.IsReshaped(w.Limit) {
					got = append(got, w)
				}
			}
			for _, v := range res.Violations {
				if gate.IsReshaped(v.Limit) {
					t.Errorf("heads-up reported as a violation: %+v", v)
				}
			}
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("heads-up = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("heads-ups = %+v, want one", got)
			}
			w := got[0]
			if !strings.Contains(w.Suggestion, tt.want) || w.Metric != gate.SurfaceMetric || w.Limit != "exports reshaped, max 60" ||
				w.Severity != "" || !w.HasBase || w.Head != float64(tt.head) || w.Base != float64(tt.base) {
				t.Errorf("heads-up = %+v, want metric exported_symbols, limit exports reshaped, max 60, no severity, "+
					"base %d, head %d and a suggestion containing %q", w, tt.base, tt.head, tt.want)
			}
		})
	}
}

// TestReshapedNotExempted checks that an exemption on exported_symbols
// leaves the heads-up among the warnings, as it does every other capacity
// warning, while it silences the rule's violation.
func TestReshapedNotExempted(t *testing.T) {
	t.Parallel()

	max60 := 60.0
	rule := gate.Threshold{Metric: gate.SurfaceMetric, Kind: gate.Capacity, Max: &max60, WarnAt: 0.75}
	res := gate.Evaluate(metrics.RawMetrics{ExportedSymbols: 61}, &metrics.RawMetrics{ExportedSymbols: 59},
		[]gate.Threshold{rule}, nil, gate.WithReshaped([]string{"F (signature changed)"}))
	ex := exempt.New([]exempt.Entry{{Package: "p", Metric: gate.SurfaceMetric, Reason: "by design"}}, time.Now())
	ex.Apply("p", &res)
	if !res.Passed || len(res.Exempted) != 1 || len(res.Warnings) != 1 || !gate.IsReshaped(res.Warnings[0].Limit) {
		t.Errorf("result = %+v, want the violation exempted and the heads-up kept as the one warning", res)
	}
}
