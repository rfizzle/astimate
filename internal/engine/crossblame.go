package engine

import (
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// crossBlame is the part of the module row's cross-package duplication a
// check of named packages holds those packages responsible for.
type crossBlame struct {
	// blocks are the head blocks blamed on the named packages, in head
	// order: the new ones touching a named package when the baseline
	// recorded its blocks, every one touching a named package otherwise.
	blocks []metrics.CrossBlock
	// known says the baseline recorded its blocks, so blocks holds only new
	// ones.
	known bool
}

// blameNamed returns the module row m with dup_blocks_cross_pkg replaced by
// the value the rules judge in a check of the packages named (gatedCross),
// and the blame behind it. head are the row's blocks at head; the
// baseline's blocks come from base, and its count from bm, nil when it has
// no module row. m.DupBlocksCrossPkg must be non-nil.
func blameNamed(m metrics.RawMetrics, bm *metrics.RawMetrics, base baseline.Baseline,
	head []metrics.CrossBlock, named []string,
) (metrics.RawMetrics, *crossBlame) {
	set := make(map[string]bool, len(named))
	for _, p := range named {
		set[p] = true
	}
	baseBlocks, known := base.CrossBlocks()
	b := blameCross(head, baseBlocks, known, set)
	baseVal := 0
	if bm != nil && bm.DupBlocksCrossPkg != nil {
		baseVal = *bm.DupBlocksCrossPkg
	}
	v := b.gatedCross(*m.DupBlocksCrossPkg, baseVal)
	m.DupBlocksCrossPkg = &v
	return m, &b
}

// blameCross returns the head blocks a check of the packages in named
// blames. When known, base holds the baseline's blocks and a head block is
// new when its set of packages occurs more often at head than in base;
// blocks are matched by the packages they touch because their lines move
// between commits. A head block is blamed when it is new, or known is
// false, and one of its occurrences lies in a named package.
func blameCross(head, base []metrics.CrossBlock, known bool, named map[string]bool) crossBlame {
	var old map[string]int
	if known {
		old = make(map[string]int, len(base))
		for i := range base {
			old[packageSetKey(&base[i])]++
		}
	}
	b := crossBlame{known: known}
	for i := range head {
		blk := &head[i]
		if known {
			if k := packageSetKey(blk); old[k] > 0 {
				old[k]--
				continue
			}
		}
		if touchesAny(blk, named) {
			b.blocks = append(b.blocks, *blk)
		}
	}
	return b
}

// gatedCross returns the dup_blocks_cross_pkg value the module rule judges
// in a check of named packages, given the row's value at head and at the
// baseline (0 when it has none): the baseline's value plus the blamed new
// blocks when the baseline recorded its blocks, so only those count as an
// increase. Otherwise the head value when a block touching a named package
// exists and the baseline's value when none does, so a copy between other
// packages never fails the check.
func (b crossBlame) gatedCross(head, base int) int {
	switch {
	case b.known:
		return base + len(b.blocks)
	case len(b.blocks) > 0:
		return head
	default:
		return base
	}
}

// suggestion returns the dup_blocks_cross_pkg suggestion of a check of
// named packages: the usual sentence over the blamed blocks, followed by
// the packages sharing each one, module-relative under modulePath. Empty
// when no block is blamed, so the caller keeps the default suggestion.
func (b crossBlame) suggestion(m metrics.RawMetrics, modulePath string) string {
	if len(b.blocks) == 0 {
		return ""
	}
	s := score.MetricSuggestion("dup_blocks_cross_pkg", float64(len(b.blocks)), m,
		score.Names{CrossBlocks: b.blocks})
	var sb strings.Builder
	sb.WriteString(s)
	if b.known {
		sb.WriteString(" New blocks touching the checked package are shared by ")
	} else {
		sb.WriteString(" Blocks touching the checked package are shared by ")
	}
	for i := range b.blocks {
		if i > 0 {
			sb.WriteString("; ")
		}
		pkgs := blockPackages(&b.blocks[i])
		for j, p := range pkgs {
			switch {
			case j == 0:
			case j == len(pkgs)-1:
				sb.WriteString(" and ")
			default:
				sb.WriteString(", ")
			}
			sb.WriteString(modulePathRel(modulePath, p))
		}
	}
	sb.WriteString(".")
	return sb.String()
}

// blockPackages returns the distinct packages holding b's occurrences, in
// occurrence order, which is package order.
func blockPackages(b *metrics.CrossBlock) []string {
	pkgs := make([]string, 0, len(b.Occurrences))
	for _, o := range b.Occurrences {
		if !slices.Contains(pkgs, o.Package) {
			pkgs = append(pkgs, o.Package)
		}
	}
	return pkgs
}

// packageSetKey identifies b by the sorted set of packages it touches.
func packageSetKey(b *metrics.CrossBlock) string {
	pkgs := blockPackages(b)
	slices.Sort(pkgs)
	return strings.Join(pkgs, "\x00")
}

// touchesAny reports whether an occurrence of b lies in a package in named.
func touchesAny(b *metrics.CrossBlock, named map[string]bool) bool {
	for _, o := range b.Occurrences {
		if named[o.Package] {
			return true
		}
	}
	return false
}
