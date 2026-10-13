package judge

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
)

// seededCrossBaseline is a crossBaseline that serves the package in p2 a
// row seeded from p, as baseline.Seeded does after a rename.
type seededCrossBaseline struct {
	crossBaseline
}

func (b seededCrossBaseline) Row(dir string) (baseline.Row, bool) {
	if dir == "p2" {
		return baseline.Row{From: []string{"p"}}, true
	}
	return b.crossBaseline.Row(dir)
}

// TestCheckerModuleJudgesAddedBlocks judges a module row whose baseline,
// with 860 blocks, recorded them, under max_delta 0 and the 80-token floor,
// with p2 split out of p by a rename. Only a new block of at least 80
// tokens that is not between the halves of the split is a rise; the row
// reports the full count, and the finding names the added block only. A
// check of named packages judges the same blocks.
func TestCheckerModuleJudgesAddedBlocks(t *testing.T) {
	t.Parallel()

	legacy := sized(100, "a", "b")
	short := sized(45, "a", "c")
	moved := sized(200, "p", "p2")
	copied := sized(90, "c", "d")
	tests := []struct {
		name     string
		head     []metrics.CrossBlock
		named    []string
		wantFail bool
	}{
		{name: "legacy, short and moved blocks", head: []metrics.CrossBlock{legacy, short, moved}},
		{name: "a copy over the floor", head: []metrics.CrossBlock{legacy, short, moved, copied}, wantFail: true},
		{name: "named: short and moved blocks", head: []metrics.CrossBlock{legacy, short, moved}, named: []string{"a", "p2"}},
		{name: "named: a copy over the floor", head: []metrics.CrossBlock{legacy, short, copied}, named: []string{"c"}, wantFail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const root, modPath = "/mod", "example.com/m"
			full, baseCount := len(tt.head), 860
			ext := metricstest.NewFake("go", root, map[string]metrics.RawMetrics{modPath + "/a": {}},
				metricstest.WithModuleRow(metrics.RawMetrics{DupBlocksCrossPkg: &full}),
				metricstest.WithModuleDetails(metrics.Details{CrossBlocks: tt.head}))
			zero, ceiling := 0.0, 450.0
			jc := New(Options{
				Ext: ext,
				Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
				Base: seededCrossBaseline{crossBaseline{
					fakeBaseline: fakeBaseline{metrics.ModuleRowID: {DupBlocksCrossPkg: &baseCount}},
					cross:        metrics.MapPackages([]metrics.CrossBlock{legacy}, func(p string) string { return Rel(modPath, p) }),
				}},
				Config: config.Effective{Version: "v", MinTokensCross: 80, Thresholds: []gate.Threshold{
					{Metric: "dup_blocks_cross_pkg", Kind: gate.Density, MaxDelta: &zero, Max: &ceiling},
				}},
				Tokenizer: "est",
				Now:       time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
				Logger:    slog.New(slog.DiscardHandler),
			})
			var named []string
			for _, p := range tt.named {
				named = append(named, modPath+"/"+p)
			}
			p, _, err := jc.Module(t.Context(), ext.(metrics.ModuleMetrics), false, named)
			if err != nil {
				t.Fatal(err)
			}
			r := p.Report
			if n := r.Metrics.DupBlocksCrossPkg; n == nil || *n != full {
				t.Errorf("row dup_blocks_cross_pkg = %v, want the full count %d", n, full)
			}
			if !tt.wantFail {
				if len(r.Violations) != 0 {
					t.Errorf("violations = %+v, want none", r.Violations)
				}
				return
			}
			if len(r.Violations) != 1 {
				t.Fatalf("violations = %+v, want one on dup_blocks_cross_pkg", r.Violations)
			}
			v := r.Violations[0]
			if v.Head != 861 {
				t.Errorf("judged head = %v, want the baseline's 860 plus the one added block", v.Head)
			}
			if !strings.Contains(v.Suggestion, "c/c.go:10-15 and d/d.go:20-25") || strings.Contains(v.Suggestion, "a/a.go") {
				t.Errorf("suggestion = %q, want it to name the added block only", v.Suggestion)
			}
			if v.Location == nil || v.Location.File != "c/c.go" || v.Location.Line != 10 {
				t.Errorf("located at %+v, want c/c.go:10", v.Location)
			}
		})
	}
}

// TestCheckerModuleCountNote checks that against a baseline that recorded
// no blocks, a baseline file, the full count is judged and the suggestion
// says so.
func TestCheckerModuleCountNote(t *testing.T) {
	t.Parallel()

	one := 1
	jc := checker(t, &one, nil)
	jc.o.Ext = metricstest.NewFake("go", "/mod", map[string]metrics.RawMetrics{"example.com/m/a": {}},
		metricstest.WithModuleRow(metrics.RawMetrics{DupBlocksCrossPkg: &one}))
	p, _, err := jc.Module(t.Context(), jc.o.Ext.(metrics.ModuleMetrics), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v := p.Report.Violations; len(v) != 1 || !strings.HasSuffix(v[0].Suggestion, "rather than the blocks this change added.") {
		t.Errorf("violations = %+v, want one whose suggestion says the count was judged", v)
	}
}
