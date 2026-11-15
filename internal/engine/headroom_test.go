package engine

import (
	"reflect"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/metrics/role"
	"github.com/rfizzle/astimate/internal/report/rows"
)

// headroomTarget returns a Target over a fake module of language lang
// whose packages, under example.com/m, have metrics pkgs, judged by cfg.
func headroomTarget(lang string, cfg *config.Config, pkgs map[string]metrics.RawMetrics) *Target {
	const root, modPath = "/mod", "example.com/m"
	byPath := make(map[string]metrics.RawMetrics, len(pkgs))
	for dir, m := range pkgs {
		byPath[modPath+"/"+dir] = m
	}
	return &Target{
		Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Ext: metricstest.NewFake(lang, root, byPath),
		Cfg: cfg,
	}
}

// shippedConfig parses the embedded default, the configuration rank uses
// when no file is given.
func shippedConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatalf("parsing the embedded default: %v", err)
	}
	return cfg
}

// TestRankHeadroom ranks by headroom under the embedded default: each row's
// headroom is its tightest capacity rule, a warn rule included, a main
// package skips the rules its roles leave out, a TypeScript row is judged
// by the TypeScript override's ceilings, and a row no capacity rule
// judges has none and sorts last.
func TestRankHeadroom(t *testing.T) {
	t.Parallel()

	libOnly := &config.Config{Rebuild: rankParams(), Thresholds: []gate.Threshold{{
		Metric: "exported_symbols", Kind: gate.Capacity, Max: new(60.0), WarnAt: 0.75,
		Roles: []string{role.Library},
	}}}
	tests := []struct {
		name string
		lang string
		cfg  *config.Config
		pkgs map[string]metrics.RawMetrics
		// want is each row's headroom in rank order, the zero Ceiling for
		// none.
		want []rows.Ceiling
		// wantRules is the number of ceilings each row carries, in rank
		// order.
		wantRules []int
		wantPaths []string
	}{
		{
			name: "go default",
			lang: "go",
			cfg:  shippedConfig(t),
			pkgs: map[string]metrics.RawMetrics{
				// internal_imports is a warn rule and still the tightest.
				"lib": {TokensEst: 8000, SLOC: 500, LargestFileSLOC: 100, ExportedSymbols: 48, InternalImports: 9},
				// Over the tokens_est ceiling: at the top, remaining negative.
				"big": {TokensEst: 17000, SLOC: 900, LargestFileSLOC: 300, ExportedSymbols: 10, InternalImports: 2},
				// A composition root: exported_symbols and internal_imports
				// do not judge it, so sloc is its tightest.
				"cmd/tool": {Role: role.Main, TokensEst: 4000, SLOC: 300, LargestFileSLOC: 200, ExportedSymbols: 59, InternalImports: 20},
				"tiny":     {TokensEst: 10, SLOC: 1, LargestFileSLOC: 1},
			},
			wantPaths: []string{"big", "lib", "cmd/tool", "tiny"},
			want: []rows.Ceiling{
				rows.NewCeiling("tokens_est", 17000, 16000),
				rows.NewCeiling("internal_imports", 9, 10),
				rows.NewCeiling("largest_file_sloc", 200, 600),
				rows.NewCeiling("largest_file_sloc", 1, 600),
			},
			wantRules: []int{5, 5, 3, 5},
		},
		{
			name: "typescript override",
			lang: "typescript",
			cfg:  shippedConfig(t),
			pkgs: map[string]metrics.RawMetrics{
				// Over the Go ceiling of 16,000, under the override's 35,000.
				"web": {TokensEst: 30000, SLOC: 1500, LargestFileSLOC: 400, ExportedSymbols: 20, InternalImports: 3},
				// internal_imports' override max is 8.
				"api": {TokensEst: 1000, SLOC: 100, LargestFileSLOC: 50, ExportedSymbols: 5, InternalImports: 7},
			},
			wantPaths: []string{"api", "web"},
			want: []rows.Ceiling{
				rows.NewCeiling("internal_imports", 7, 8),
				rows.NewCeiling("tokens_est", 30000, 35000),
			},
			wantRules: []int{5, 5},
		},
		{
			name: "unjudged sorts last",
			lang: "go",
			cfg:  libOnly,
			pkgs: map[string]metrics.RawMetrics{
				"a/main": {Role: role.Main, ExportedSymbols: 59},
				"b/lib":  {ExportedSymbols: 6},
				"c/lib":  {ExportedSymbols: 6},
			},
			// b/lib and c/lib tie and keep path order.
			wantPaths: []string{"b/lib", "c/lib", "a/main"},
			want: []rows.Ceiling{
				rows.NewCeiling("exported_symbols", 6, 60),
				rows.NewCeiling("exported_symbols", 6, 60),
				{},
			},
			wantRules: []int{1, 1, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ranked, failed, err := Rank(t.Context(), headroomTarget(tt.lang, tt.cfg, tt.pkgs), RankOptions{Sort: rows.SortHeadroom})
			if err != nil || len(failed) != 0 {
				t.Fatalf("Rank = (%v, %v), want no error", failed, err)
			}
			if got := rowPaths(ranked); !slices.Equal(got, tt.wantPaths) {
				t.Fatalf("rows = %q, want %q", got, tt.wantPaths)
			}
			for i := range ranked {
				r := &ranked[i]
				var want *rows.Ceiling
				if tt.want[i] != (rows.Ceiling{}) {
					want = &tt.want[i]
				}
				if !reflect.DeepEqual(r.Headroom, want) {
					t.Errorf("%s headroom = %+v, want %+v", r.Path, r.Headroom, want)
				}
				if len(r.Ceilings) != tt.wantRules[i] {
					t.Errorf("%s ceilings = %+v, want %d", r.Path, r.Ceilings, tt.wantRules[i])
				}
			}
		})
	}
}
