package engine

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// degradedDir is the degraded copy of the fixture: its tested package adds
// a duplicate function, an untested export, a global and a complex function.
const degradedDir = "../../testdata/go/fixture-degraded"

// TestCheckLocatesDegradedFindings checks the degraded fixture against a
// baseline of the pristine one: each violation of its tested package is
// located on the line the degraded copy added, module-relative, and the
// check carries the module root's directory in its repository.
func TestCheckLocatesDegradedFindings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	fx, err := LoadTarget(fixtureDir, TargetOptions{Tokenizer: TokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := baseline.Collect(t.Context(), fx.Ext, fx.Mod)
	if err != nil {
		t.Fatal(err)
	}
	funcs, err := baseline.CollectFunctions(t.Context(), fx.Ext, fx.Mod, pkgs)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := baseline.WriteContents(path, baseline.Contents{
		Ref: "fixture", ModulePath: fx.Mod.ModulePath, Tokenizer: TokenizerEst, Packages: pkgs, Functions: funcs,
	}); err != nil {
		t.Fatal(err)
	}

	tg, err := LoadTarget(degradedDir, TargetOptions{Tokenizer: TokenizerEst})
	if err != nil {
		t.Fatal(err)
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, Packages: []string{tg.Mod.ModulePath + "/tested"}})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check = (%v, %v), want no error", failed, err)
	}
	if want := baseline.RepoDir(t.Context(), tg.Mod.Root); c.ModuleDir != want {
		t.Errorf("ModuleDir = %q, want %q", c.ModuleDir, want)
	}
	if want := "testdata/go/fixture-degraded"; c.ModuleDir != "" && c.ModuleDir != want {
		t.Errorf("ModuleDir = %q, want %q inside this repository", c.ModuleDir, want)
	}
	if len(c.Packages) != 1 {
		t.Fatalf("checked %d packages, want tested alone", len(c.Packages))
	}
	r := &c.Packages[0].Report
	if b := r.Baseline; b == nil || b.Tokenizer != TokenizerEst || !b.TokensComparable {
		t.Errorf("baseline block = %+v, want tokenizer est, comparable", b)
	}
	want := map[string]string{
		"changed_func_cognitive_max": "tested/grade.go:8",
		"dup_blocks":                 "tested/degraded.go:15",
		"globals":                    "tested/degraded.go:9",
		"untested_exports":           "tested/degraded.go:13",
	}
	seen := 0
	for _, v := range r.Violations {
		if v.Location == nil || v.Location.File == "" || v.Location.Line == 0 {
			t.Errorf("%s has no location", v.Metric)
			continue
		}
		loc := v.Location.File + ":" + strconv.Itoa(v.Location.Line)
		if w, ok := want[v.Metric]; ok {
			seen++
			if loc != w {
				t.Errorf("%s located at %s, want %s", v.Metric, loc, w)
			}
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of the %d degraded violations: %+v", seen, len(want), r.Violations)
	}
}

func TestFindingPosition(t *testing.T) {
	t.Parallel()

	d := &metrics.Details{
		DupLocations:      []string{"a.go:3-9", "b.go:12-18", "bad"},
		UntestedPositions: []metrics.Position{{File: "a.go", Line: 4}, {File: "b.go", Line: 20}},
		GlobalPositions:   []metrics.Position{{File: "a.go", Line: 1}},
		LargestFile:       "a.go",
		SourceFiles:       []string{"a.go", "b.go", "doc.go"},
	}
	worst := &changedFunction{file: "b.go", line: 11, files: map[string]bool{"b.go": true}}
	tests := []struct {
		name   string
		metric string
		d      *metrics.Details
		worst  *changedFunction
		want   metrics.Position
	}{
		{name: "duplicate in a changed file", metric: "dup_blocks", d: d, worst: worst, want: metrics.Position{File: "b.go", Line: 12}},
		{name: "first duplicate without changes", metric: "duplication_pct", d: d, want: metrics.Position{File: "a.go", Line: 3}},
		{name: "untested export in a changed file", metric: "untested_exports", d: d, worst: worst, want: metrics.Position{File: "b.go", Line: 20}},
		{name: "global in no changed file", metric: "globals", d: d, worst: worst, want: metrics.Position{File: "a.go", Line: 1}},
		{name: "largest file", metric: "tokens_est", d: d, want: metrics.Position{File: "a.go", Line: 1}},
		{name: "changed function", metric: "changed_func_cognitive_max", d: d, worst: worst, want: metrics.Position{File: "b.go", Line: 11}},
		{name: "other metric on doc.go", metric: "fan_in", d: d, want: metrics.Position{File: "doc.go", Line: 1}},
		{name: "first file without doc.go", metric: "fan_in", d: &metrics.Details{SourceFiles: []string{"x.go", "y.go"}},
			want: metrics.Position{File: "x.go", Line: 1}},
		{name: "nothing recorded", metric: "globals", d: &metrics.Details{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := findingPosition(tt.metric, tt.d, tt.worst); got != tt.want {
				t.Errorf("findingPosition(%s) = %+v, want %+v", tt.metric, got, tt.want)
			}
		})
	}
}

func TestLocateFindingsJoinsPackagePath(t *testing.T) {
	t.Parallel()

	r := report.Report{
		PackagePath: "internal/billing",
		Violations:  []report.Finding{{Metric: "globals"}},
		Warnings:    []report.Finding{{Metric: "tokens_est"}},
	}
	d := &metrics.Details{GlobalPositions: []metrics.Position{{File: "state.go", Line: 7}}}
	locateFindings(&r, d, nil)
	if l := r.Violations[0].Location; l == nil || *l != (report.Location{File: "internal/billing/state.go", Line: 7}) {
		t.Errorf("globals located at %+v, want internal/billing/state.go:7", l)
	}
	if l := r.Warnings[0].Location; l != nil {
		t.Errorf("tokens_est located at %+v with nothing recorded, want no location", l)
	}
}
