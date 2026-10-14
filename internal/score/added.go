package score

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/role"
)

// AddedCrossBlocks returns the cross-package blocks of head, a module's
// blocks at head, that dup_blocks_cross_pkg judges as added by the change
// (SPEC.md 8.1), in head's order: those that are new against base, the
// blocks at the baseline (matched as below), at least floor tokens long,
// not moved and not exempt. A block of unknown length (Tokens 0) is kept
// whatever the floor. A block is exempt when it is declarative and every
// occurrence lies in a package whose role in roles is main: the flag
// tables and registration calls composition roots repeat are counted but
// not gated. roles maps a package, named like head's, to its role
// (metrics.Details.Roles); nil exempts nothing. A block is moved when every package it touches lies
// in one group of packages a rename joined: a package seeded by a rename
// and the packages it was seeded from, groups sharing a package being one
// group, so splitting a package in two moves the code the halves share
// rather than copying it. from maps each seeded package to the packages
// it was seeded from (baseline.Row.From), all named like head's packages;
// nil when nothing was renamed. head is not modified.
//
// Blocks are matched by the set of packages they touch, as NewCrossBlocks
// does, each package replaced by its rename group, so a legacy block that
// the rename carried from a source into the package carved out of it, its
// copy in a third package staying put, is matched with its baseline
// self. Only the blocks that clear the floor and are not exempt on both
// sides are matched, so a short legacy block cannot hide a long copy
// between the same packages, nor a long legacy block be blamed for a short
// repeat added beside it.
func AddedCrossBlocks(head, base []metrics.CrossBlock, floor int, from map[string][]string, roles map[string]string) []metrics.CrossBlock {
	group := renameGroups(from)
	old := make(map[string]int, len(base))
	for i := range base {
		if !underFloor(&base[i], floor) && !mainOnly(&base[i], roles) {
			old[groupSet(&base[i], group)]++
		}
	}
	var out []metrics.CrossBlock
	for i := range head {
		b := &head[i]
		if underFloor(b, floor) || mainOnly(b, roles) || moved(b, group) {
			continue
		}
		if k := groupSet(b, group); old[k] > 0 {
			old[k]--
			continue
		}
		out = append(out, *b)
	}
	return out
}

// underFloor reports whether b is known to be shorter than floor tokens.
func underFloor(b *metrics.CrossBlock, floor int) bool {
	return b.Tokens > 0 && b.Tokens < floor
}

// mainOnly reports whether b is a declarative block every occurrence of
// which lies in a package roles calls main.
func mainOnly(b *metrics.CrossBlock, roles map[string]string) bool {
	if b.Kind != "declarative" || len(b.Occurrences) == 0 {
		return false
	}
	for _, o := range b.Occurrences {
		if roles[o.Package] != role.Main {
			return false
		}
	}
	return true
}

// renameGroups returns, for each package of from, keys and sources alike,
// the representative of the group of packages renames joined it to.
func renameGroups(from map[string][]string) map[string]string {
	parent := make(map[string]string, 2*len(from))
	var find func(p string) string
	find = func(p string) string {
		q, ok := parent[p]
		if !ok {
			parent[p] = p
			return p
		}
		if q == p {
			return p
		}
		r := find(q)
		parent[p] = r
		return r
	}
	for to, srcs := range from {
		root := find(to)
		for _, s := range srcs {
			if r := find(s); r != root {
				parent[r] = root
			}
		}
	}
	group := make(map[string]string, len(parent))
	for p := range parent {
		group[p] = find(p)
	}
	return group
}

// groupSet returns the set of packages b touches, as packageSet does, with
// each package that group holds replaced by its group's representative.
func groupSet(b *metrics.CrossBlock, group map[string]string) string {
	pkgs := make([]string, 0, len(b.Occurrences))
	for _, o := range b.Occurrences {
		p := o.Package
		if g, ok := group[p]; ok {
			p = g
		}
		pkgs = append(pkgs, p)
	}
	slices.Sort(pkgs)
	return strings.Join(slices.Compact(pkgs), "\x00")
}

// moved reports whether every package b touches lies in one rename group.
func moved(b *metrics.CrossBlock, group map[string]string) bool {
	if len(group) == 0 || len(b.Occurrences) == 0 {
		return false
	}
	g, ok := group[b.Occurrences[0].Package]
	if !ok {
		return false
	}
	for _, o := range b.Occurrences[1:] {
		if group[o.Package] != g {
			return false
		}
	}
	return true
}

// AddedCrossSuggestion is the dup_blocks_cross_pkg suggestion when the
// rule judges the blocks the change added (AddedCrossBlocks): it counts
// added, says they are at least floor tokens long when floor is positive,
// and cites the first one's first occurrence and its first occurrence in
// another package. Empty when added is.
func AddedCrossSuggestion(added []metrics.CrossBlock, floor int) string {
	if len(added) == 0 {
		return ""
	}
	s := "The change added " + count(len(added), "duplicate block", "duplicate blocks")
	if floor > 0 {
		s += " of at least " + strconv.Itoa(floor) + " tokens"
	}
	s += " shared with other packages; "
	where := crossPair(added)
	switch {
	case where == "" && len(added) == 1:
		return s + "extract it into one place."
	case where == "":
		return s + "extract them into one place."
	case len(added) == 1:
		return s + "extract the shared block in " + where + " into one package."
	default:
		return s + "extract each into one package, starting with the block in " + where + "."
	}
}

// CrossCountNote follows a dup_blocks_cross_pkg suggestion when the
// baseline recorded no cross-package blocks, a baseline file or an
// extractor that does not name them, so the rule judged the count rather
// than the blocks the change added.
const CrossCountNote = "The baseline records no cross-package blocks, so the whole count is judged rather than the blocks this change added."
