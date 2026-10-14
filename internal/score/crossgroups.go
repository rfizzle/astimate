package score

import (
	"cmp"
	"math"
	"slices"

	"github.com/rfizzle/astimate/internal/metrics"
)

// CrossGroup is the cross-package blocks of one set of packages, as
// AddedCrossBlocks matches them: the head blocks it may judge added, in
// head order, and the lengths of the baseline's blocks on the same set
// that they are matched against. It holds what AddedCrossBlocks needs to
// count the blocks added on the set at any floor (AddedAt).
type CrossGroup struct {
	// Base are the token lengths of the baseline's blocks on the set, 0
	// for a block of unknown length, in baseline order.
	Base []int
	// Head are the head's blocks on the set that are neither exempt nor
	// moved, in head order.
	Head []metrics.CrossBlock
}

// AddedAt returns how many of g's head blocks AddedCrossBlocks judges
// added at floor: those at least floor tokens long, or of unknown
// length, beyond the baseline's blocks on the set that clear the same
// floor.
func (g *CrossGroup) AddedAt(floor int) int {
	n := 0
	for i := range g.Head {
		if !underFloor(&g.Head[i], floor) {
			n++
		}
	}
	for _, t := range g.Base {
		if t == 0 || t >= floor {
			n--
		}
	}
	return max(n, 0)
}

// CrossGroups returns the sets of packages on which AddedCrossBlocks,
// given head, base, from and roles as it takes them, judges a block added
// at some floor, in the order of their first head block, so the sum of
// AddedAt over them equals the length of AddedCrossBlocks at any floor.
// A set on which no floor adds a block is left out; nil when there is
// none. Packages keep head's and base's names; head is not modified.
func CrossGroups(head, base []metrics.CrossBlock, from map[string][]string, roles map[string]string) []CrossGroup {
	group := renameGroups(from)
	idx := map[string]int{}
	var out []CrossGroup
	for i := range head {
		b := &head[i]
		if mainOnly(b, roles) || moved(b, group) {
			continue
		}
		k := groupSet(b, group)
		j, ok := idx[k]
		if !ok {
			j = len(out)
			idx[k] = j
			out = append(out, CrossGroup{})
		}
		out[j].Head = append(out[j].Head, *b)
	}
	for i := range base {
		b := &base[i]
		if mainOnly(b, roles) {
			continue
		}
		if j, ok := idx[groupSet(b, group)]; ok {
			out[j].Base = append(out[j].Base, b.Tokens)
		}
	}
	return slices.DeleteFunc(out, func(g CrossGroup) bool { return !g.addsAtSomeFloor() })
}

// addsAtSomeFloor reports whether AddedAt is positive at some floor: with
// both sides' lengths sorted longest first, an unknown length counting as
// the longest, the i-th head block is longer than the i-th baseline block
// or has none to match it.
func (g *CrossGroup) addsAtSomeFloor() bool {
	long := func(t int) int {
		if t == 0 {
			return math.MaxInt
		}
		return t
	}
	h := make([]int, len(g.Head))
	for i := range g.Head {
		h[i] = long(g.Head[i].Tokens)
	}
	b := make([]int, len(g.Base))
	for i, t := range g.Base {
		b[i] = long(t)
	}
	desc := func(x, y int) int { return cmp.Compare(y, x) }
	slices.SortFunc(h, desc)
	slices.SortFunc(b, desc)
	for i := range h {
		if i >= len(b) || h[i] > b[i] {
			return true
		}
	}
	return false
}
