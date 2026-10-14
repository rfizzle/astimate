package judge

import (
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// crossJudgment is how the module row's dup_blocks_cross_pkg is judged.
type crossJudgment struct {
	// known says the baseline recorded its cross-package blocks, so added
	// holds the blocks the change added; otherwise the count is judged.
	known bool
	// added are the head blocks the change added that the rule judges
	// (score.AddedCrossBlocks): new, at least floor tokens long and not
	// moved by a rename.
	added []metrics.CrossBlock
	// groups are the sets of packages on which the change added a block
	// at some floor (score.CrossGroups), so the count can be re-derived at
	// any floor; nil when the count is judged.
	groups []score.CrossGroup
	// floor is duplication.min_tokens_cross.
	floor int
}

// judgeCross returns how the module row's dup_blocks_cross_pkg is judged
// given head, its blocks at head, base, the baseline's blocks named like
// head's and known when the baseline recorded them, and roles, the role
// of each package of head's blocks.
func (c *Checker) judgeCross(head, base []metrics.CrossBlock, known bool, roles map[string]string) crossJudgment {
	j := crossJudgment{known: known, floor: c.o.Config.MinTokensCross}
	if known {
		from := c.renamedFrom(head)
		j.added = score.AddedCrossBlocks(head, base, j.floor, from, roles)
		j.groups = score.CrossGroups(head, base, from, roles)
	}
	return j
}

// judgesAdded reports whether the rules judge the added blocks rather than
// the full count of m, given bm, the baseline's module row: the baseline
// recorded its blocks and both rows have a count.
func (j crossJudgment) judgesAdded(m metrics.RawMetrics, bm *metrics.RawMetrics) bool {
	return j.known && bm != nil && bm.DupBlocksCrossPkg != nil && m.DupBlocksCrossPkg != nil
}

// recorded returns the groups the rules judged the added blocks from,
// with every occurrence's package renamed by rel, as the gate replay
// records them: an empty list when no set gained a block, nil when the
// full count of m was judged (judgesAdded).
func (j crossJudgment) recorded(m metrics.RawMetrics, bm *metrics.RawMetrics, rel func(string) string) []score.CrossGroup {
	if !j.judgesAdded(m, bm) {
		return nil
	}
	out := make([]score.CrossGroup, len(j.groups))
	for i, g := range j.groups {
		out[i] = score.CrossGroup{Base: g.Base, Head: metrics.MapPackages(g.Head, rel)}
	}
	return out
}

// gated returns m with dup_blocks_cross_pkg replaced by the value a check
// of every package judges: the baseline's value plus the added blocks,
// so only those count as a rise under max_delta. m is returned unchanged
// when the baseline recorded no blocks or bm, the baseline's module row,
// has no value to add to, and the full count is judged.
func (j crossJudgment) gated(m metrics.RawMetrics, bm *metrics.RawMetrics) metrics.RawMetrics {
	if !j.judgesAdded(m, bm) {
		return m
	}
	v := *bm.DupBlocksCrossPkg + len(j.added)
	m.DupBlocksCrossPkg = &v
	return m
}

// renamedFrom returns, for each package an occurrence of blocks lies in
// whose baseline row was seeded by a rename (baseline.Row.From), the
// packages it was seeded from, all named by identifier as blocks are; nil
// when there is none.
func (c *Checker) renamedFrom(blocks []metrics.CrossBlock) map[string][]string {
	modPath := c.o.Mod.ModulePath
	var from map[string][]string
	seen := make(map[string]bool)
	for i := range blocks {
		for _, o := range blocks[i].Occurrences {
			if seen[o.Package] {
				continue
			}
			seen[o.Package] = true
			row, ok := c.o.Base.Row(Rel(modPath, o.Package))
			if !ok || len(row.From) == 0 {
				continue
			}
			srcs := make([]string, len(row.From))
			for k, dir := range row.From {
				srcs[k] = ImportPath(modPath, dir)
			}
			if from == nil {
				from = make(map[string][]string)
			}
			from[o.Package] = srcs
		}
	}
	return from
}
