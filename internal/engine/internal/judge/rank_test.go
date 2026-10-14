package judge

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/score"
)

// legacyThenNew returns the details of a package whose legacy duplicate
// block, the longer one, comes before a block the change added, and whose
// legacy untested export sorts before the added one.
func legacyThenNew() metrics.Details {
	return metrics.Details{
		DupLocations: []string{"a.go:1-30", "a.go:40-69", "z.go:5-12", "z.go:20-27"},
		DupBlocks: []metrics.DupBlock{
			{Anchors: [2]uint64{1, 2}, Locations: []string{"a.go:1-30", "a.go:40-69"}},
			{Anchors: [2]uint64{3, 4}, Locations: []string{"z.go:5-12", "z.go:20-27"}},
		},
		UntestedExports:   []string{"Legacy", "Zeta"},
		UntestedPositions: []metrics.Position{{File: "a.go", Line: 1}, {File: "z.go", Line: 5}},
	}
}

// legacyRow is the baseline row of legacyThenNew: its legacy block and
// export.
func legacyRow() baseline.Row {
	return baseline.Row{HasNamed: true, Named: baseline.Named{DupBlocks: []uint64{1, 2}, UntestedExports: []string{"Legacy"}}}
}

// TestRankNewCodeBlocks checks the names a dup_blocks_code finding cites:
// the code blocks only, an added one first, and whether the change added
// it, which an added block of another kind does not make it.
func TestRankNewCodeBlocks(t *testing.T) {
	t.Parallel()

	legacy := metrics.DupBlock{Anchors: [2]uint64{1, 2}, Kind: "code", Locations: []string{"a.go:1-30", "a.go:40-69"}}
	overlap := metrics.DupBlock{Anchors: [2]uint64{3, 4}, Kind: "self-overlap", Locations: []string{"s.go:1-9", "s.go:5-13"}}
	pasted := metrics.DupBlock{Anchors: [2]uint64{5, 6}, Kind: "code", Locations: []string{"p.go:2-20", "q.go:8-26"}}
	row := baseline.Row{HasNamed: true, Named: baseline.Named{DupBlocks: []uint64{1, 2}}}
	tests := []struct {
		name     string
		blocks   []metrics.DupBlock
		wantCode []string
		want     leads
	}{
		{name: "an added code block leads", blocks: []metrics.DupBlock{legacy, overlap, pasted},
			wantCode: []string{"p.go:2-20", "q.go:8-26", "a.go:1-30", "a.go:40-69"}, want: leads{dup: true, code: true}},
		{name: "an added self-overlap block is no added code block", blocks: []metrics.DupBlock{legacy, overlap},
			wantCode: []string{"a.go:1-30", "a.go:40-69"}, want: leads{dup: true}},
		{name: "no code block", blocks: []metrics.DupBlock{overlap}, want: leads{dup: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := metrics.Details{DupBlocks: tt.blocks}
			var names score.Names
			if got := rankNew(&d, &names, &row, true); got != tt.want {
				t.Errorf("leads = %+v, want %+v", got, tt.want)
			}
			if !slices.Equal(names.DupCodeLocations, tt.wantCode) || names.DupCodeAdded != tt.want.code {
				t.Errorf("code locations %q (added %v), want %q (added %v)", names.DupCodeLocations, names.DupCodeAdded,
					tt.wantCode, tt.want.code)
			}
		})
	}
}

func TestRankNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		row          baseline.Row
		inBase       bool
		wantLoc      string
		wantUntested []string
		want         leads
	}{
		{name: "added ones lead", row: legacyRow(), inBase: true, wantLoc: "z.go:5-12",
			wantUntested: []string{"Zeta", "Legacy"}, want: leads{dup: true, untested: true}},
		{name: "row recording no details keeps the order", row: baseline.Row{}, inBase: true, wantLoc: "a.go:1-30",
			wantUntested: []string{"Legacy", "Zeta"}},
		{name: "new package added everything", inBase: false, wantLoc: "a.go:1-30",
			wantUntested: []string{"Legacy", "Zeta"}, want: leads{dup: true, untested: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := legacyThenNew()
			names := score.Names{DupLocations: d.DupLocations, UntestedExports: d.UntestedExports}
			got := rankNew(&d, &names, &tt.row, tt.inBase)
			if got != tt.want {
				t.Errorf("leads = %+v, want %+v", got, tt.want)
			}
			if d.DupLocations[0] != tt.wantLoc || names.DupLocations[0] != tt.wantLoc || names.DupAdded != tt.want.dup {
				t.Errorf("first location %q (names %q, added %v), want %q", d.DupLocations[0], names.DupLocations[0],
					names.DupAdded, tt.wantLoc)
			}
			if len(d.DupLocations) != 4 || d.DupBlocks[0].Locations[0] != tt.wantLoc {
				t.Errorf("details = %q, %+v, want every location, grouped as ranked", d.DupLocations, d.DupBlocks)
			}
			if !slices.Equal(d.UntestedExports, tt.wantUntested) || !slices.Equal(names.UntestedExports, tt.wantUntested) {
				t.Errorf("untested = %q (names %q), want %q", d.UntestedExports, names.UntestedExports, tt.wantUntested)
			}
			if p := d.UntestedPositions[0]; (p.File == "z.go") != (tt.wantUntested[0] == "Zeta") {
				t.Errorf("first untested position %+v does not follow %q", p, tt.wantUntested[0])
			}
		})
	}
}

// namedBaseline is a fakeBaseline whose package rows carry recorded
// details.
type namedBaseline struct {
	fakeBaseline
	named map[string]baseline.Row
}

func (b namedBaseline) Row(dir string) (baseline.Row, bool) {
	r, ok := b.fakeBaseline.Row(dir)
	if n, has := b.named[dir]; has {
		r.Named, r.HasNamed = n.Named, n.HasNamed
	}
	return r, ok
}

// TestCheckerPackageCitesAdded judges a package whose legacy duplicate
// block comes before the one the change added: the finding cites the
// added block, says so and is located on it, and the untested export the
// change added is named first.
func TestCheckerPackageCitesAdded(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	head := metrics.RawMetrics{DupBlocks: 2, DuplicationPct: 30, UntestedExports: 2}
	ext := metricstest.NewFake("go", root, map[string]metrics.RawMetrics{modPath + "/a": head},
		metricstest.WithDetails(map[string]metrics.Details{modPath + "/a": legacyThenNew()}))
	zero := 0.0
	jc := New(Options{
		Ext: ext,
		Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Base: namedBaseline{
			fakeBaseline: fakeBaseline{"a": {DupBlocks: 1, DuplicationPct: 20, UntestedExports: 1}},
			named:        map[string]baseline.Row{"a": legacyRow()},
		},
		Config: config.Effective{Version: "v", Thresholds: []gate.Threshold{
			{Metric: "dup_blocks", Kind: gate.Density, MaxDelta: &zero},
			{Metric: "untested_exports", Kind: gate.Density, MaxDelta: &zero},
		}},
		Tokenizer: "est",
		Now:       time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC),
		Logger:    slog.New(slog.DiscardHandler),
	})
	p, _, err := jc.Package(t.Context(), modPath+"/a", head)
	if err != nil {
		t.Fatal(err)
	}
	r := p.Report
	want := map[string]struct{ text, loc string }{
		"dup_blocks":       {"starting with z.go:5-12, added in this change.", "a/z.go:5"},
		"untested_exports": {"(Zeta, Legacy)", "a/z.go:5"},
	}
	for _, f := range r.Violations {
		w, ok := want[f.Metric]
		if !ok {
			continue
		}
		delete(want, f.Metric)
		if !strings.Contains(f.Suggestion, w.text) {
			t.Errorf("%s suggestion %q, want it to contain %q", f.Metric, f.Suggestion, w.text)
		}
		if f.Location == nil || f.Location.File+":"+strconv.Itoa(f.Location.Line) != w.loc {
			t.Errorf("%s located at %+v, want %s", f.Metric, f.Location, w.loc)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing violations %v in %+v", want, r.Violations)
	}
	if r.Details == nil || len(r.Details.Duplicates) == 0 || r.Details.Duplicates[0].File != "z.go" {
		t.Errorf("details = %+v, want the added block's occurrences first", r.Details)
	}
}

// crossBaseline is a fakeBaseline that recorded the module's cross-package
// blocks.
type crossBaseline struct {
	fakeBaseline
	cross []metrics.CrossBlock
}

func (b crossBaseline) CrossBlocks() ([]metrics.CrossBlock, bool) { return b.cross, true }

// TestCheckerModuleCitesAdded judges a module row whose legacy
// cross-package block comes before the one the change added: the
// suggestion, the location and the details all lead with the added one.
func TestCheckerModuleCitesAdded(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	occ := func(pkg string, line int) metrics.Occurrence {
		return metrics.Occurrence{Package: modPath + "/" + pkg, File: pkg + "/x.go", StartLine: line, EndLine: line + 9}
	}
	legacy := metrics.CrossBlock{Occurrences: []metrics.Occurrence{occ("a", 1), occ("b", 1)}}
	added := metrics.CrossBlock{Occurrences: []metrics.Occurrence{occ("c", 20), occ("d", 20)}}
	two, one := 2, 1
	ext := metricstest.NewFake("go", root, map[string]metrics.RawMetrics{modPath + "/a": {}},
		metricstest.WithModuleRow(metrics.RawMetrics{DupBlocksCrossPkg: &two}),
		metricstest.WithModuleDetails(metrics.Details{CrossBlocks: []metrics.CrossBlock{legacy, added}}))
	zero := 0.0
	jc := New(Options{
		Ext: ext,
		Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Base: crossBaseline{
			fakeBaseline: fakeBaseline{metrics.ModuleRowID: {DupBlocksCrossPkg: &one}},
			cross: []metrics.CrossBlock{{Occurrences: []metrics.Occurrence{
				{Package: "a", File: "a/x.go", StartLine: 3, EndLine: 12}, {Package: "b", File: "b/x.go", StartLine: 3, EndLine: 12},
			}}},
		},
		Config:    config.Effective{Version: "v", Thresholds: []gate.Threshold{{Metric: "dup_blocks_cross_pkg", Kind: gate.Density, MaxDelta: &zero}}},
		Tokenizer: "est",
		Now:       time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC),
		Logger:    slog.New(slog.DiscardHandler),
	})
	p, _, err := jc.Module(t.Context(), ext.(metrics.ModuleMetrics), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := p.Report
	if len(r.Violations) != 1 {
		t.Fatalf("violations = %+v, want dup_blocks_cross_pkg", r.Violations)
	}
	v := r.Violations[0]
	if !strings.HasPrefix(v.Suggestion, "The change added 1 duplicate block shared") ||
		!strings.Contains(v.Suggestion, "c/x.go:20-29 and d/x.go:20-29") {
		t.Errorf("suggestion %q, want the added block cited and said to be added", v.Suggestion)
	}
	if v.Location == nil || v.Location.File != "c/x.go" || v.Location.Line != 20 {
		t.Errorf("located at %+v, want c/x.go:20", v.Location)
	}
	if r.Details == nil || len(r.Details.CrossBlocks) != 2 || r.Details.CrossBlocks[0].Occurrences[0].File != "c/x.go" {
		t.Errorf("details = %+v, want the added block first", r.Details)
	}
}
