package score

import (
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/role"
)

// TestCrossGroupsMatchAddedCrossBlocks checks that the groups count, at
// every floor, the blocks AddedCrossBlocks judges added, and keep only the
// sets on which some floor adds one.
func TestCrossGroupsMatchAddedCrossBlocks(t *testing.T) {
	t.Parallel()

	split := map[string][]string{"p2": {"p"}}
	mains := map[string]string{"cmd/a": role.Main, "cmd/b": role.Main}
	tests := []struct {
		name       string
		head, base []metrics.CrossBlock
		from       map[string][]string
		roles      map[string]string
		// sets are the groups' first head blocks' package lists.
		sets []string
	}{
		{name: "legacy block of the same length", head: []metrics.CrossBlock{crossOf(100, "a", "b")},
			base: []metrics.CrossBlock{crossOf(100, "a", "b")}},
		{name: "a longer head block over a shorter legacy one", head: []metrics.CrossBlock{crossOf(130, "a", "b")},
			base: []metrics.CrossBlock{crossOf(70, "a", "b")}, sets: []string{"a-b"}},
		{name: "a short added block beside a long legacy one",
			head: []metrics.CrossBlock{crossOf(70, "a", "b"), crossOf(130, "a", "b")},
			base: []metrics.CrossBlock{crossOf(125, "a", "b")}, sets: []string{"a-b"}},
		{name: "new set", head: []metrics.CrossBlock{crossOf(100, "a", "b"), crossOf(65, "a", "c")},
			base: []metrics.CrossBlock{crossOf(100, "a", "b")}, sets: []string{"a-c"}},
		{name: "unknown length", head: []metrics.CrossBlock{crossOf(0, "a", "c")},
			base: []metrics.CrossBlock{crossOf(500, "a", "c")}, sets: []string{"a-c"}},
		{name: "moved by a split", head: []metrics.CrossBlock{crossOf(100, "p", "p2")}, from: split},
		{name: "rename group matches its legacy self", head: []metrics.CrossBlock{crossOf(100, "p2", "q")},
			base: []metrics.CrossBlock{crossOf(100, "p", "q")}, from: split},
		{name: "declarative between commands", head: []metrics.CrossBlock{kindOf("declarative", crossOf(100, "cmd/a", "cmd/b"))},
			roles: mains},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			groups := CrossGroups(tt.head, tt.base, tt.from, tt.roles)
			var got []string
			for i := range groups {
				got = append(got, sets(groups[i].Head[:1])...)
			}
			if !slices.Equal(got, tt.sets) {
				t.Errorf("sets = %v, want %v", got, tt.sets)
			}
			for _, floor := range []int{0, 40, 60, 69, 70, 80, 100, 120, 126, 130, 131, 1000} {
				n := 0
				for i := range groups {
					n += groups[i].AddedAt(floor)
				}
				if want := len(AddedCrossBlocks(tt.head, tt.base, floor, tt.from, tt.roles)); n != want {
					t.Errorf("floor %d: groups add %d, AddedCrossBlocks %d", floor, n, want)
				}
			}
		})
	}
}
