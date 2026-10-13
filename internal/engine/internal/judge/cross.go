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
	// floor is duplication.min_tokens_cross.
	floor int
}

// judgeCross returns how the module row's dup_blocks_cross_pkg is judged
// given head, its blocks at head, and base, the baseline's blocks named
// like head's and known when the baseline recorded them.
func (c *Checker) judgeCross(head, base []metrics.CrossBlock, known bool) crossJudgment {
	j := crossJudgment{known: known, floor: c.o.Config.MinTokensCross}
	if known {
		j.added = score.AddedCrossBlocks(head, base, j.floor, c.renamedFrom(head))
	}
	return j
}

// gated returns m with dup_blocks_cross_pkg replaced by the value a check
// of every package judges: the baseline's value plus the added blocks,
// so only those count as a rise under max_delta. m is returned unchanged
// when the baseline recorded no blocks or bm, the baseline's module row,
// has no value to add to, and the full count is judged.
func (j crossJudgment) gated(m metrics.RawMetrics, bm *metrics.RawMetrics) metrics.RawMetrics {
	if !j.known || bm == nil || bm.DupBlocksCrossPkg == nil || m.DupBlocksCrossPkg == nil {
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
