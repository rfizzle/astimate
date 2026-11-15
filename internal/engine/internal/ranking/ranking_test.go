package ranking

import (
	"errors"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report/rows"
)

func TestRows(t *testing.T) {
	t.Parallel()

	capacity := func(metric string, limit float64) gate.Threshold {
		return gate.Threshold{Metric: metric, Kind: gate.Capacity, Max: new(limit), WarnAt: 0.75}
	}
	// Each package reports dup_blocks_cross_pkg, over the module rule's max.
	pkgs := map[string]*metrics.RawMetrics{
		"example.com/m":        {TokensEst: 4000, SLOC: 100, DupBlocksCrossPkg: new(5)},
		"example.com/m/big":    {TokensEst: 12000, SLOC: 400, DupBlocksCrossPkg: new(5)},
		"example.com/m/x/tiny": {TokensEst: 10, SLOC: 1, DupBlocksCrossPkg: new(5)},
	}
	order := []string{"example.com/m", "example.com/m/big", "example.com/m/x/tiny"}
	tokens := capacity("tokens_est", 16000)
	sloc := capacity("sloc", 1000)
	// A capacity rule on a module-wide metric judges the module row only
	// (gate.ForRow), so no package row carries it.
	cross := capacity("dup_blocks_cross_pkg", 1)
	tests := []struct {
		name      string
		rules     []gate.Threshold
		sort      string
		top       int
		wantPaths []string
		wantRules int
	}{
		{name: "headroom", rules: []gate.Threshold{sloc, tokens, cross}, sort: rows.SortHeadroom,
			wantPaths: []string{"big", ".", "x/tiny"}, wantRules: 2},
		{name: "top", rules: []gate.Threshold{tokens}, sort: rows.SortHeadroom, top: 2,
			wantPaths: []string{"big", "."}, wantRules: 1},
		{name: "no capacity rule", rules: []gate.Threshold{cross}, sort: rows.SortHeadroom,
			wantPaths: []string{".", "big", "x/tiny"}, wantRules: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Rows(&Input{
				ModulePath: "example.com/m", Packages: pkgs, Order: order,
				Rules: tt.rules, Sort: tt.sort, Top: tt.top,
			})
			if err != nil {
				t.Fatalf("Rows: %v", err)
			}
			paths := make([]string, 0, len(got))
			for i := range got {
				paths = append(paths, got[i].Path)
				if len(got[i].Ceilings) != tt.wantRules || (got[i].Headroom == nil) != (tt.wantRules == 0) {
					t.Errorf("%s ceilings = %+v, headroom %+v, want %d ceilings", got[i].Path, got[i].Ceilings, got[i].Headroom, tt.wantRules)
				}
			}
			if !slices.Equal(paths, tt.wantPaths) {
				t.Errorf("rows = %q, want %q", paths, tt.wantPaths)
			}
		})
	}

	t.Run("unknown sort key", func(t *testing.T) {
		t.Parallel()

		got, err := Rows(&Input{Packages: pkgs, Order: order, Sort: "size"})
		if !errors.Is(err, rows.ErrUnknownSortKey) || got != nil {
			t.Errorf("Rows = (%v, %v), want no rows and rows.ErrUnknownSortKey", got, err)
		}
	})
}
