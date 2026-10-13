package report

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// mergeCheck returns a check of one package on which the gate merged
// findings of one cause (SPEC.md 8.1): sloc carrying tokens_est on the
// same growth, and dup_blocks carrying duplication_pct, itself over both
// its max_delta and its max.
func mergeCheck(t *testing.T) *Check {
	t.Helper()
	rules := []gate.Threshold{
		{Metric: "dup_blocks", Kind: gate.Density, MaxDelta: new(0.0), RatchetFromZero: true},
		{Metric: "duplication_pct", Kind: gate.Density, MaxDelta: new(6.0), Max: new(40.0)},
		{Metric: "tokens_est", Kind: gate.Capacity, Max: new(16000.0), WarnAt: 0.75},
		{Metric: "sloc", Kind: gate.Capacity, Max: new(1000.0), WarnAt: 0.75},
	}
	head := metrics.RawMetrics{SLOC: 1100, TokensEst: 17500, DupBlocks: 2, DuplicationPct: 45}
	base := metrics.RawMetrics{SLOC: 900, TokensEst: 15000, DupBlocks: 1, DuplicationPct: 38}
	suggest := func(metric string, _ float64, _ metrics.RawMetrics) string {
		switch metric {
		case "dup_blocks", "duplication_pct":
			return "2 duplicate blocks cover 45% of lines; extract shared helpers."
		}
		return "Split the package."
	}
	res := gate.Evaluate(head, &base, rules, suggest)
	r := Build(&Input{Language: "go", PackagePath: "internal/big", ModulePath: "example.com/app",
		Metrics: head, Params: params(), ConfigVersion: uncalibratedVersion})
	ApplyGate(&r, "a1b2c3d", &base, &res)
	if len(r.Violations) != 2 {
		t.Fatalf("violations = %+v, want dup_blocks and sloc", r.Violations)
	}
	passes := 0.0
	return &Check{Packages: []CheckedPackage{{Report: r, BaseAgentPasses: &passes}}}
}

// TestMergeFormats renders the merged findings in every check format and
// compares each with its golden file: one line or annotation per cause,
// its limit naming each merged rule, and an also list in JSON.
func TestMergeFormats(t *testing.T) {
	t.Parallel()

	for _, f := range checkFormats() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			got, err := f.write(mergeCheck(t))
			if err != nil {
				t.Fatal(err)
			}
			compareGolden(t, filepath.Join("testdata", "merge", f.name+".golden"), got)
		})
	}
}

// TestMergeAlso checks that each merged finding carries the merged rules
// in also, and a finding that merged nothing has none.
func TestMergeAlso(t *testing.T) {
	t.Parallel()

	r := mergeCheck(t).Packages[0].Report
	for _, f := range r.Violations {
		want := map[string][]string{"dup_blocks": {"duplication_pct"}, "sloc": {"tokens_est"}}[f.Metric]
		if !slices.Equal(f.Also, want) {
			t.Errorf("%s: also = %q, want %q", f.Metric, f.Also, want)
		}
	}
	plain := finding(&gate.Violation{Metric: "globals", Limit: "max_delta +0"})
	if plain.Also != nil || mustJSON(t, plain) != `{"metric":"globals","head":0,"limit":"max_delta +0","suggestion":""}` {
		t.Errorf("unmerged finding = %s, want no also", mustJSON(t, plain))
	}
}
