package gate_test

import (
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/role"
)

func TestCeilings(t *testing.T) {
	t.Parallel()

	notMain := []string{role.Library, role.TestSupport, role.GeneratedOnly}
	capacity := func(metric string, limit float64, roles ...string) gate.Threshold {
		return gate.Threshold{Metric: metric, Kind: gate.Capacity, Max: ptr(limit), WarnAt: 0.75, Roles: roles}
	}
	tokens := capacity("tokens_est", 16000)
	exports := capacity("exported_symbols", 60, notMain...)
	imports := capacity("internal_imports", 10, notMain...)
	imports.Severity = gate.SeverityWarn
	churn := capacity("churn_commits", 50)
	covered := capacity("untested_exports", 20)
	covered.Source = "coverage"
	density := gate.Threshold{Metric: "dup_blocks", Kind: gate.Density, MaxDelta: ptr(0.0), Max: ptr(1.0)}
	noMax := gate.Threshold{Metric: "sloc", Kind: gate.Capacity, WarnAt: 0.75}
	zeroMax := capacity("sloc", 0)
	rules := []gate.Threshold{density, tokens, exports, imports, churn, noMax, zeroMax}

	lib := metrics.RawMetrics{TokensEst: 14100, ExportedSymbols: 30, InternalImports: 11, DupBlocks: 4, SLOC: 900}
	mainRow := lib
	mainRow.Role = role.Main
	seven := 7
	measured := lib
	measured.UntestedExportsCovered = &seven
	measured.UntestedExports = 12
	tests := []struct {
		name  string
		head  metrics.RawMetrics
		rules []gate.Threshold
		want  []gate.Ceiling
	}{
		{
			// The warn rule counts; the density rule, a metric not
			// computed (churn) and a rule without a positive max do not.
			name: "library", head: lib, rules: rules,
			want: []gate.Ceiling{
				{Metric: "tokens_est", Value: 14100, Max: 16000},
				{Metric: "exported_symbols", Value: 30, Max: 60},
				{Metric: "internal_imports", Value: 11, Max: 10},
			},
		},
		{
			name: "main is judged by tokens_est only", head: mainRow, rules: rules,
			want: []gate.Ceiling{{Metric: "tokens_est", Value: 14100, Max: 16000}},
		},
		{name: "no capacity rule", head: lib, rules: []gate.Threshold{density}, want: nil},
		{name: "no rules", head: lib, rules: nil, want: nil},
		{
			name: "source coverage reads the covered count", head: measured, rules: []gate.Threshold{covered},
			want: []gate.Ceiling{{Metric: "untested_exports", Value: 7, Max: 20}},
		},
		{
			name: "source coverage without a coverage run", head: lib, rules: []gate.Threshold{covered},
			want: []gate.Ceiling{{Metric: "untested_exports", Value: 0, Max: 20}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := gate.Ceilings(tt.head, tt.rules); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Ceilings = %+v, want %+v", got, tt.want)
			}
		})
	}
}
