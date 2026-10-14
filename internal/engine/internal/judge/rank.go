package judge

import (
	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// leads says which of a package's findings cite something the change
// added: the first duplicate block, the first code duplicate block, the
// first untested export. A finding that does is located on it, wherever
// the changed functions lie.
type leads struct {
	dup, code, untested bool
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
		first := firstCode(blocks)
		l.code = first >= 0 && first < added
	}
	var added int
	d.UntestedExports, d.UntestedPositions, added = score.RankUntested(d.UntestedExports, d.UntestedPositions,
		row.Named.UntestedExports)
	l.untested = added > 0
	names.DupLocations, names.DupAdded = d.DupLocations, l.dup
	names.DupCodeLocations, names.DupCodeAdded = codeLocations(d.DupBlocks), l.code
	names.UntestedExports = d.UntestedExports
	return l
}

// firstCode returns the index in blocks of the first block of kind code
// (SPEC.md 6.3), the one dup_blocks_code's finding cites, or -1.
func firstCode(blocks []metrics.DupBlock) int {
	for i := range blocks {
		if blocks[i].Kind == codeKind {
			return i
		}
	}
	return -1
}

// codeLocations returns the occurrences of the blocks of kind code among
// blocks, in their order, the ones dup_blocks_code counts; nil when none
// is code.
func codeLocations(blocks []metrics.DupBlock) []string {
	var out []string
	for i := range blocks {
		if blocks[i].Kind == codeKind {
			out = append(out, blocks[i].Locations...)
		}
	}
	return out
}

// codeKind is the metrics.DupBlock kind of a block of code, neither
// declarative, idiom nor self-overlap (SPEC.md 6.3).
const codeKind = "code"
