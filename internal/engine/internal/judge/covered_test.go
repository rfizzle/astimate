package judge

import (
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

func TestCoveredBase(t *testing.T) {
	t.Parallel()

	n := func(v int) *int { return &v }
	named := &baseline.Row{HasNamed: true, Named: baseline.Named{UntestedExports: []string{"Gone", "Hello", "Unused"}}}
	tests := []struct {
		name    string
		head    metrics.RawMetrics
		base    *metrics.RawMetrics
		row     *baseline.Row
		covered []string
		want    *int // nil: bm returned unchanged
	}{
		{
			name:    "baseline less what head's run executed",
			head:    metrics.RawMetrics{UntestedExportsCovered: n(1)},
			base:    &metrics.RawMetrics{UntestedExports: 3},
			row:     named,
			covered: []string{"Hello", "New"},
			want:    n(2),
		},
		{
			name: "no coverage at head",
			head: metrics.RawMetrics{},
			base: &metrics.RawMetrics{UntestedExports: 3},
			row:  named,
		},
		{
			name: "no names at the baseline",
			head: metrics.RawMetrics{UntestedExportsCovered: n(1)},
			base: &metrics.RawMetrics{UntestedExports: 3},
			row:  &baseline.Row{},
		},
		{
			name: "new package",
			head: metrics.RawMetrics{UntestedExportsCovered: n(1)},
			row:  &baseline.Row{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := coveredBase(tt.head, tt.base, tt.row, tt.covered)
			if tt.want == nil {
				if got != tt.base {
					t.Errorf("coveredBase = %+v, want the baseline unchanged", got)
				}
				return
			}
			if got == tt.base || got.UntestedExportsCovered == nil || *got.UntestedExportsCovered != *tt.want {
				t.Errorf("coveredBase = %+v, want a copy with untested_exports_covered %d", got, *tt.want)
			}
			if tt.base.UntestedExportsCovered != nil {
				t.Error("coveredBase modified the baseline row")
			}
		})
	}
}

func TestCoveredLast(t *testing.T) {
	t.Parallel()

	pos := func(lines ...int) []metrics.Position {
		out := make([]metrics.Position, len(lines))
		for i, l := range lines {
			out[i] = metrics.Position{File: "a.go", Line: l}
		}
		return out
	}
	tests := []struct {
		name      string
		exports   []string
		covered   []string
		base      []string
		inBase    bool
		lead      bool
		want      []string
		wantLines []int
		wantLead  bool
	}{
		{
			name:    "added and run moves behind an old one no test ran",
			exports: []string{"Added", "Old"}, covered: []string{"Added"},
			base: []string{"Old"}, inBase: true, lead: true,
			want: []string{"Old", "Added"}, wantLines: []int{2, 1}, wantLead: false,
		},
		{
			name:    "added and not run keeps the lead",
			exports: []string{"Added", "Ran", "Old"}, covered: []string{"Ran"},
			base: []string{"Old", "Ran"}, inBase: true, lead: true,
			want: []string{"Added", "Old", "Ran"}, wantLines: []int{1, 3, 2}, wantLead: true,
		},
		{
			name:    "new package: every export is added",
			exports: []string{"A", "B"}, covered: []string{"A"}, lead: true,
			want: []string{"B", "A"}, wantLines: []int{2, 1}, wantLead: true,
		},
		{
			name:    "all run",
			exports: []string{"A"}, covered: []string{"A"}, lead: true,
			want: []string{"A"}, wantLines: []int{1}, wantLead: false,
		},
		{
			name:    "no run",
			exports: []string{"A", "B"}, lead: true,
			want: []string{"A", "B"}, wantLines: []int{1, 2}, wantLead: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := make([]int, len(tt.exports))
			for i := range lines {
				lines[i] = i + 1
			}
			d := metrics.Details{UntestedExports: slices.Clone(tt.exports), UntestedPositions: pos(lines...), UntestedCovered: tt.covered}
			names := score.Names{UntestedExports: d.UntestedExports}
			l := leads{untested: tt.lead}
			coveredLast(&d, &names, &l, tt.base, tt.inBase)
			if !slices.Equal(d.UntestedExports, tt.want) || !slices.Equal(names.UntestedExports, tt.want) {
				t.Errorf("exports = %v, names = %v, want %v", d.UntestedExports, names.UntestedExports, tt.want)
			}
			if !slices.Equal(d.UntestedPositions, pos(tt.wantLines...)) {
				t.Errorf("positions = %v, want lines %v", d.UntestedPositions, tt.wantLines)
			}
			if l.untested != tt.wantLead {
				t.Errorf("lead = %v, want %v", l.untested, tt.wantLead)
			}
		})
	}
}
