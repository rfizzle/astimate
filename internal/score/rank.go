package score

import (
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// RankDupBlocks returns blocks, a package's duplicate blocks at head, with
// the blocks the change added first: those neither of whose anchors is
// among base, the anchors of the package's blocks at the baseline (SPEC.md
// 8.5). A block that kept one end of a baseline block, which a change
// lengthened or shortened at the other, is not added. Each part keeps the
// order of blocks, which is by length in tokens, longest first. added is
// how many blocks lead. blocks is not modified.
func RankDupBlocks(blocks []metrics.DupBlock, base []uint64) (ranked []metrics.DupBlock, added int) {
	isNew := make([]bool, len(blocks))
	for i := range blocks {
		a := blocks[i].Anchors
		isNew[i] = !slices.Contains(base, a[0]) && !slices.Contains(base, a[1])
	}
	return newFirst(blocks, isNew)
}

// RankUntested returns names, a package's untested exports at head, with
// those the change left untested first: those not among base, its
// untested exports at the baseline. pos, the declarations of names index
// for index, or nil when unknown, is reordered alike. Each part keeps the
// order of names. added is how many names lead. Neither input is modified.
func RankUntested(names []string, pos []metrics.Position, base []string) (ranked []string, rankedPos []metrics.Position, added int) {
	isNew := make([]bool, len(names))
	for i, n := range names {
		isNew[i] = !slices.Contains(base, n)
	}
	ranked, added = newFirst(names, isNew)
	if len(pos) == len(names) {
		rankedPos, _ = newFirst(pos, isNew)
	} else {
		rankedPos = slices.Clone(pos)
	}
	return ranked, rankedPos, added
}

// RankCrossBlocks returns head, a module's cross-package blocks at head,
// with the blocks the change added first (NewCrossBlocks against base, the
// blocks at the baseline), each part in the order of head. added is how
// many blocks lead. head is not modified.
func RankCrossBlocks(head, base []metrics.CrossBlock) (ranked []metrics.CrossBlock, added int) {
	return newFirst(head, NewCrossBlocks(head, base))
}

// NewCrossBlocks reports, for each of head, the cross-package blocks at
// head, whether the change added it. Blocks are matched by the set of
// packages they touch, since their lines move between commits: a head
// block is new when more head blocks than blocks of base, the baseline's,
// touch its set, the excess being the later ones in head's order. The
// packages of both must be named alike.
func NewCrossBlocks(head, base []metrics.CrossBlock) []bool {
	old := make(map[string]int, len(base))
	for i := range base {
		old[packageSet(&base[i])]++
	}
	isNew := make([]bool, len(head))
	for i := range head {
		k := packageSet(&head[i])
		if old[k] > 0 {
			old[k]--
			continue
		}
		isNew[i] = true
	}
	return isNew
}

// packageSet identifies b by the sorted set of packages it touches.
func packageSet(b *metrics.CrossBlock) string {
	pkgs := make([]string, 0, len(b.Occurrences))
	for _, o := range b.Occurrences {
		pkgs = append(pkgs, o.Package)
	}
	slices.Sort(pkgs)
	return strings.Join(slices.Compact(pkgs), "\x00")
}

// newFirst returns a copy of items with those isNew marks first, each part
// in the order of items, and how many lead.
func newFirst[T any](items []T, isNew []bool) ([]T, int) {
	if items == nil {
		return nil, 0
	}
	out := make([]T, 0, len(items))
	for i, it := range items {
		if isNew[i] {
			out = append(out, it)
		}
	}
	added := len(out)
	for i, it := range items {
		if !isNew[i] {
			out = append(out, it)
		}
	}
	return out, added
}
