package narrate

import (
	"slices"
	"strings"
	"testing"
)

// TestHotterWithoutChurn checks that packages of equal hotspot 0 order by
// passes, then low days, then path.
func TestHotterWithoutChurn(t *testing.T) {
	t.Parallel()

	mk := func(path string, passes, low float64) Package {
		return Package{Path: path, Rebuild: Rebuild{AgentPasses: passes, HumanDaysLow: low}}
	}
	ps := []Package{mk("d", 1, 9), mk("c", 2, 1), mk("b", 1, 3), mk("a", 1, 3), mk("e", 3, 0)}
	slices.SortStableFunc(ps, func(a, b Package) int { return Hotter(&a, &b) })
	got := make([]string, 0, len(ps))
	for _, p := range ps {
		got = append(got, p.Path)
	}
	if want := "e c d a b"; strings.Join(got, " ") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
}
