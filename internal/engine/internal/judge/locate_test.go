package judge

import (
	"reflect"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

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

func TestLocateCross(t *testing.T) {
	t.Parallel()

	r := report.Report{
		Violations: []report.Finding{{Metric: "dup_blocks_cross_pkg"}, {Metric: "globals"}},
	}
	locateCross(&r, nil)
	if l := r.Violations[0].Location; l != nil {
		t.Errorf("located at %+v with no blocks, want no location", l)
	}
	blocks := []metrics.CrossBlock{{Occurrences: []metrics.Occurrence{
		{Package: "m/a", File: "a/a.go", StartLine: 5, EndLine: 9},
		{Package: "m/b", File: "b/b.go", StartLine: 7, EndLine: 11},
	}}}
	locateCross(&r, blocks)
	if l := r.Violations[0].Location; l == nil || *l != (report.Location{File: "a/a.go", Line: 5}) {
		t.Errorf("dup_blocks_cross_pkg located at %+v, want a/a.go:5", l)
	}
	if l := r.Violations[1].Location; l != nil {
		t.Errorf("globals located at %+v, want no location", l)
	}
}

func TestWorstChanged(t *testing.T) {
	t.Parallel()

	before := []metrics.FunctionInfo{{Name: "Keep", Fingerprint: 1, Cognitive: 30, File: "a.go", Line: 3}}
	tests := []struct {
		name string
		head []metrics.FunctionInfo
		want *changedFunction
	}{
		{name: "nothing changed", head: before, want: &changedFunction{}},
		{
			name: "most complex changed function",
			head: []metrics.FunctionInfo{
				before[0],
				{Receiver: "*T", Name: "Grow", Fingerprint: 2, Cognitive: 12, File: "b.go", Line: 8},
				{Name: "Small", Fingerprint: 3, Cognitive: 2, File: "c.go", Line: 1},
			},
			want: &changedFunction{cognitive: 12, name: "*T.Grow (b.go:8)", file: "b.go", line: 8,
				files: map[string]bool{"b.go": true, "c.go": true}},
		},
		{
			name: "no file known",
			head: []metrics.FunctionInfo{{Name: "New", Fingerprint: 4, Cognitive: 5}},
			want: &changedFunction{cognitive: 5, name: "New"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := worstChanged(before, tt.head); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("worstChanged = %+v, want %+v", got, tt.want)
			}
		})
	}
}
