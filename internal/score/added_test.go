package score

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics/inventory"
	"github.com/rfizzle/astimate/internal/metrics/role"
)

// crossOf returns a block of tokens normalized tokens with one occurrence
// in each of pkgs, in order.
func crossOf(tokens int, pkgs ...string) inventory.CrossBlock {
	b := inventory.CrossBlock{Tokens: tokens}
	for i, p := range pkgs {
		b.Occurrences = append(b.Occurrences, inventory.Occurrence{Package: p, File: p + "/x.go", StartLine: 10*i + 1, EndLine: 10*i + 9})
	}
	return b
}

// kindOf returns b with its kind set to kind.
func kindOf(kind string, b inventory.CrossBlock) inventory.CrossBlock {
	b.Kind = kind
	return b
}

// sets renders blocks as their package lists, "a-b".
func sets(blocks []inventory.CrossBlock) []string {
	out := make([]string, 0, len(blocks))
	for i := range blocks {
		var pkgs []string
		for _, o := range blocks[i].Occurrences {
			pkgs = append(pkgs, o.Package)
		}
		out = append(out, strings.Join(pkgs, "-"))
	}
	return out
}

func TestAddedCrossBlocks(t *testing.T) {
	t.Parallel()

	split := map[string][]string{"p2": {"p"}}
	mains := map[string]string{"cmd/a": role.Main, "cmd/b": role.Main, "lib": role.Library}
	tests := []struct {
		name       string
		head, base []inventory.CrossBlock
		floor      int
		from       map[string][]string
		roles      map[string]string
		want       []string
	}{
		{name: "legacy block", head: []inventory.CrossBlock{crossOf(100, "a", "b")},
			base: []inventory.CrossBlock{crossOf(100, "a", "b")}, floor: 80},
		{name: "new block over the floor", head: []inventory.CrossBlock{crossOf(100, "a", "b"), crossOf(100, "a", "c")},
			base: []inventory.CrossBlock{crossOf(100, "a", "b")}, floor: 80, want: []string{"a-c"}},
		{name: "new block at the floor", head: []inventory.CrossBlock{crossOf(80, "a", "c")}, floor: 80, want: []string{"a-c"}},
		{name: "new block under the floor", head: []inventory.CrossBlock{crossOf(45, "a", "c")}, floor: 80},
		{name: "unknown length is kept", head: []inventory.CrossBlock{crossOf(0, "a", "c")}, floor: 80, want: []string{"a-c"}},
		{name: "no floor", head: []inventory.CrossBlock{crossOf(45, "a", "c")}, want: []string{"a-c"}},
		{name: "block between a split's halves", head: []inventory.CrossBlock{crossOf(200, "p", "p2")}, floor: 80, from: split},
		{name: "block from a half to another package", head: []inventory.CrossBlock{crossOf(200, "p2", "q")}, floor: 80,
			from: split, want: []string{"p2-q"}},
		{name: "legacy block carried into a half", head: []inventory.CrossBlock{crossOf(200, "p2", "q"), crossOf(200, "p", "p2", "r")},
			base: []inventory.CrossBlock{crossOf(200, "p", "q"), crossOf(200, "p", "r")}, floor: 80, from: split},
		{name: "second copy beside a carried legacy block", head: []inventory.CrossBlock{crossOf(200, "p2", "q"), crossOf(200, "p", "q")},
			base: []inventory.CrossBlock{crossOf(200, "p", "q")}, floor: 80, from: split, want: []string{"p-q"}},
		{name: "block between two halves of one source", head: []inventory.CrossBlock{crossOf(200, "p2", "p3")}, floor: 80,
			from: map[string][]string{"p2": {"p"}, "p3": {"p"}}},
		{name: "block across two unrelated renames", head: []inventory.CrossBlock{crossOf(200, "a2", "b2")}, floor: 80,
			from: map[string][]string{"a2": {"a"}, "b2": {"b"}}, want: []string{"a2-b2"}},
		{name: "long copy beside a short legacy block, legacy last", head: []inventory.CrossBlock{crossOf(100, "a", "b"), crossOf(45, "a", "b")},
			base: []inventory.CrossBlock{crossOf(45, "a", "b")}, floor: 80, want: []string{"a-b"}},
		{name: "short repeat beside a long legacy block, repeat first", head: []inventory.CrossBlock{crossOf(45, "a", "b"), crossOf(100, "a", "b")},
			base: []inventory.CrossBlock{crossOf(100, "a", "b")}, floor: 80},
		{name: "head order kept", head: []inventory.CrossBlock{crossOf(90, "c", "d"), crossOf(90, "a", "b")}, floor: 80,
			want: []string{"c-d", "a-b"}},
		{name: "declarative block between main packages", head: []inventory.CrossBlock{kindOf("declarative", crossOf(200, "cmd/a", "cmd/b"))},
			floor: 80, roles: mains},
		{name: "code block between main packages", head: []inventory.CrossBlock{kindOf("code", crossOf(200, "cmd/a", "cmd/b"))},
			floor: 80, roles: mains, want: []string{"cmd/a-cmd/b"}},
		{name: "declarative block from a main package to a library", head: []inventory.CrossBlock{kindOf("declarative", crossOf(200, "cmd/a", "lib"))},
			floor: 80, roles: mains, want: []string{"cmd/a-lib"}},
		{name: "declarative block between main packages without roles", head: []inventory.CrossBlock{kindOf("declarative", crossOf(200, "cmd/a", "cmd/b"))},
			floor: 80, want: []string{"cmd/a-cmd/b"}},
		{name: "exempt legacy block hides no copy", head: []inventory.CrossBlock{crossOf(200, "cmd/a", "cmd/b")},
			base: []inventory.CrossBlock{kindOf("declarative", crossOf(200, "cmd/a", "cmd/b"))}, floor: 80, roles: mains,
			want: []string{"cmd/a-cmd/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sets(AddedCrossBlocks(tt.head, tt.base, tt.floor, tt.from, tt.roles))
			if !slices.Equal(got, tt.want) {
				t.Errorf("AddedCrossBlocks = %v, want %v", got, tt.want)
			}
		})
	}
}
