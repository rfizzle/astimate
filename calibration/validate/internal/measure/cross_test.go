package measure

import (
	"testing"

	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// moduleRow returns a module row with dup_blocks_cross_pkg head and base
// and the given recorded groups, each group's head block lengths and base
// lengths, nil groups recording none.
func moduleRow(head, base int, groups [][2][]int) corpus.Row {
	r := corpus.Row{Package: metrics.ModuleRowID, Language: "go",
		Head: metrics.RawMetrics{DupBlocksCrossPkg: &head}, Base: &metrics.RawMetrics{DupBlocksCrossPkg: &base}}
	if groups == nil {
		return r
	}
	r.Cross = []score.CrossGroup{}
	for _, g := range groups {
		cg := score.CrossGroup{Base: g[1]}
		for _, t := range g[0] {
			cg.Head = append(cg.Head, metrics.CrossBlock{Tokens: t})
		}
		r.Cross = append(r.Cross, cg)
	}
	return r
}

// TestFiresCrossAtTwoFloors re-derives a module row's dup_blocks_cross_pkg
// at two floors from its recorded added blocks, as the gate matches them
// by set of packages among the blocks that clear the floor: a 100-token
// copy fires at 80 and not at 120; a 130-token block whose set had a
// 100-token block at the parent fires at 120, where the legacy block no
// longer matches it, and not at 80; a row recording none is judged on its
// full count, which rose, at any floor.
func TestFiresCrossAtTwoFloors(t *testing.T) {
	t.Parallel()

	s := defaultRules(t)
	rule := s.List[index(t, s, "dup_blocks_cross_pkg")].Threshold
	tests := []struct {
		name          string
		row           corpus.Row
		at80, at120   bool
		added80, at60 int
	}{
		{name: "a 100-token copy",
			row:  moduleRow(12, 10, [][2][]int{{{100}, {}}}),
			at80: true, added80: 1, at60: 1},
		{name: "a short block beside a long legacy block",
			row:     moduleRow(12, 10, [][2][]int{{{70, 130}, {125}}}),
			added80: 0, at60: 1},
		{name: "a long block over a legacy block shorter than 120",
			row:   moduleRow(10, 10, [][2][]int{{{130}, {100}}}),
			at120: true, added80: 0, at60: 0},
		{name: "only a short block added",
			row:     moduleRow(11, 10, [][2][]int{{{65}, {}}}),
			added80: 0, at60: 1},
		{name: "nothing added at any floor",
			row: moduleRow(13, 10, [][2][]int{})},
		{name: "no recorded blocks: the full count",
			row:  moduleRow(11, 10, nil),
			at80: true, at120: true, added80: -1, at60: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(rule, &tt.row, 80); got != tt.at80 {
				t.Errorf("fires at 80 = %v, want %v", got, tt.at80)
			}
			if got := fires(rule, &tt.row, 120); got != tt.at120 {
				t.Errorf("fires at 120 = %v, want %v", got, tt.at120)
			}
			for floor, want := range map[int]int{80: tt.added80, 60: tt.at60} {
				n, ok := tt.row.AddedCross(floor)
				if !ok {
					n = -1
				}
				if n != want {
					t.Errorf("AddedCross(%d) = %d, want %d", floor, n, want)
				}
			}
		})
	}
}

// TestRecheckCrossFloor rechecks a module row whose recorded violation is
// the gate's at its floor: none disagree at the configuration's floor of
// 80, and the full count, which the re-evaluation used to read, would.
func TestRecheckCrossFloor(t *testing.T) {
	t.Parallel()

	s := defaultRules(t)
	s.CrossFloor = 80
	// The head count rose by 5, but the change added no block of 80 tokens.
	quiet := moduleRow(15, 10, [][2][]int{{{65}, {}}})
	loud := moduleRow(11, 10, [][2][]int{{{100}, {}}})
	loud.Violated = []string{"dup_blocks_cross_pkg"}
	c := &corpus.Corpus{Name: "cross", Commits: []corpus.Commit{commit("q", false, quiet), commit("l", true, loud)}}
	sc, err := Score(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if checked, mismatched := sc.Recheck(); mismatched != 0 || checked == 0 {
		t.Errorf("recheck %d of %d disagree, want 0 of some", mismatched, checked)
	}
	s.CrossFloor = 60
	if _, mismatched := sc.Recheck(); mismatched != 1 {
		t.Errorf("recheck at floor 60: %d disagree, want 1 (the short block)", mismatched)
	}
}
