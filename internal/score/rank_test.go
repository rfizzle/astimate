package score

import (
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

func TestRankDupBlocks(t *testing.T) {
	t.Parallel()

	// In size order: four blocks, each with the anchors 10i+1 and 10i+2.
	blocks := []metrics.DupBlock{
		{Anchors: [2]uint64{11, 12}, Locations: []string{"a.go:1-30", "b.go:1-30"}},
		{Anchors: [2]uint64{21, 22}, Locations: []string{"c.go:5-20", "c.go:40-55"}},
		{Anchors: [2]uint64{31, 32}, Locations: []string{"a.go:50-60", "d.go:1-11"}},
		{Anchors: [2]uint64{41, 42}, Locations: []string{"e.go:1-9", "e.go:20-28"}},
	}
	tests := []struct {
		name      string
		base      []uint64
		want      []uint64
		wantAdded int
	}{
		{name: "new blocks lead in size order", base: []uint64{11, 12, 31, 32}, want: []uint64{2, 4, 1, 3}, wantAdded: 2},
		{name: "a block keeping either anchor is legacy", base: []uint64{12, 31}, want: []uint64{2, 4, 1, 3}, wantAdded: 2},
		{name: "every block legacy keeps size order", base: []uint64{11, 21, 31, 41}, want: []uint64{1, 2, 3, 4}},
		{name: "every block new keeps size order", base: []uint64{99}, want: []uint64{1, 2, 3, 4}, wantAdded: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, added := RankDupBlocks(blocks, tt.base)
			fps := make([]uint64, len(got))
			for i, b := range got {
				fps[i] = b.Anchors[0] / 10
			}
			if !slices.Equal(fps, tt.want) || added != tt.wantAdded {
				t.Errorf("RankDupBlocks = %v, %d; want %v, %d", fps, added, tt.want, tt.wantAdded)
			}
		})
	}
	if blocks[0].Anchors[0] != 11 || blocks[1].Anchors[0] != 21 {
		t.Errorf("RankDupBlocks modified its input: %+v", blocks)
	}
	if got, added := RankDupBlocks(nil, []uint64{1}); got != nil || added != 0 {
		t.Errorf("RankDupBlocks(nil) = %v, %d; want nil, 0", got, added)
	}
}

func TestRankUntested(t *testing.T) {
	t.Parallel()

	names := []string{"Alpha", "JoinAgain", "Omega", "Zeta"}
	pos := []metrics.Position{{File: "a.go", Line: 1}, {File: "j.go", Line: 2}, {File: "o.go", Line: 3}, {File: "z.go", Line: 4}}
	got, gotPos, added := RankUntested(names, pos, []string{"Alpha", "Omega", "Gone"})
	if want := []string{"JoinAgain", "Zeta", "Alpha", "Omega"}; !slices.Equal(got, want) || added != 2 {
		t.Errorf("RankUntested = %q, %d; want %q, 2", got, added, want)
	}
	if want := []metrics.Position{pos[1], pos[3], pos[0], pos[2]}; !slices.Equal(gotPos, want) {
		t.Errorf("positions = %+v, want %+v", gotPos, want)
	}
	if names[0] != "Alpha" || pos[0].File != "a.go" {
		t.Errorf("RankUntested modified its input: %q %+v", names, pos)
	}
	// Positions that do not match the names index for index are kept as
	// they are.
	if _, p, _ := RankUntested(names, nil, nil); p != nil {
		t.Errorf("positions of nil = %+v, want nil", p)
	}
}

// crossBlock returns a cross-package block with one occurrence in each of
// pkgs.
func crossBlock(pkgs ...string) metrics.CrossBlock {
	b := metrics.CrossBlock{}
	for _, p := range pkgs {
		b.Occurrences = append(b.Occurrences, metrics.Occurrence{Package: p, File: p + "/x.go", StartLine: 1, EndLine: 9})
	}
	return b
}

func TestRankCrossBlocks(t *testing.T) {
	t.Parallel()

	head := []metrics.CrossBlock{crossBlock("a", "b"), crossBlock("b", "a"), crossBlock("c", "d"), crossBlock("a", "a", "e")}
	base := []metrics.CrossBlock{crossBlock("a", "b"), crossBlock("e", "a")}
	if got, want := NewCrossBlocks(head, base), []bool{false, true, true, false}; !slices.Equal(got, want) {
		t.Errorf("NewCrossBlocks = %v, want %v", got, want)
	}
	got, added := RankCrossBlocks(head, base)
	if added != 2 || len(got) != 4 || got[0].Occurrences[0].Package != "b" || got[1].Occurrences[0].Package != "c" ||
		got[2].Occurrences[0].Package != "a" {
		t.Errorf("RankCrossBlocks = %+v, %d; want the second and third block first", got, added)
	}
}
