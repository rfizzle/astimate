package judge

import (
	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// leads says which of a package's findings cite something the change
// added: the first duplicate block, the first untested export. A finding
// that does is located on it, wherever the changed functions lie.
type leads struct {
	dup, untested bool
}

// rankNew orders d's duplicate blocks and untested exports with those the
// change added first (score.RankDupBlocks, score.RankUntested), against
// row, the package's baseline row, and sets names, the names suggestions
// quote, to match. inBase reports whether the baseline has the package; a
// package new at head added all of them. When the row recorded no details
// (baseline.Row.HasNamed false) nothing can be told new, and d and names
// keep their order.
func rankNew(d *metrics.Details, names *score.Names, row *baseline.Row, inBase bool) leads {
	var l leads
	if inBase && !row.HasNamed {
		return l
	}
	if d.DupBlocks != nil {
		blocks, added := score.RankDupBlocks(d.DupBlocks, row.Named.DupBlocks)
		locs := make([]string, 0, len(d.DupLocations))
		for _, b := range blocks {
			locs = append(locs, b.Locations...)
		}
		d.DupBlocks, d.DupLocations = blocks, locs
		l.dup = added > 0
	}
	var added int
	d.UntestedExports, d.UntestedPositions, added = score.RankUntested(d.UntestedExports, d.UntestedPositions,
		row.Named.UntestedExports)
	l.untested = added > 0
	names.DupLocations, names.DupAdded = d.DupLocations, l.dup
	names.UntestedExports = d.UntestedExports
	return l
}
