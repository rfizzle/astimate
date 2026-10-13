package judge

import (
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// crossBlame is the part of the module row's cross-package duplication a
// check of named packages holds those packages responsible for.
type crossBlame struct {
	// blocks are the head blocks blamed on the named packages, in head
	// order: the added ones (crossJudgment.added) touching a named package
	// when the baseline recorded its blocks, every one touching a named
	// package otherwise.
	blocks []metrics.CrossBlock
	// known says the baseline recorded its blocks, so blocks holds only new
	// ones.
	known bool
}

// blameNamed returns the module row m with dup_blocks_cross_pkg replaced by
// the value the rules judge in a check of the packages named
// (crossBlame.gatedCross), and the blame behind it. head are the row's
// blocks at head; cj says whether the baseline recorded its blocks
// (baseline.Baseline.CrossBlocks) and which head blocks the change added,
// and bm is its module row, nil when it has none. m.DupBlocksCrossPkg must
// be non-nil.
func blameNamed(m metrics.RawMetrics, bm *metrics.RawMetrics, cj crossJudgment,
	head []metrics.CrossBlock, named []string,
) (metrics.RawMetrics, *crossBlame) {
	set := make(map[string]bool, len(named))
	for _, p := range named {
		set[p] = true
	}
	b := blameCross(head, cj, set)
	baseVal := 0
	if bm != nil && bm.DupBlocksCrossPkg != nil {
		baseVal = *bm.DupBlocksCrossPkg
	}
	v := b.gatedCross(*m.DupBlocksCrossPkg, baseVal)
	m.DupBlocksCrossPkg = &v
	return m, &b
}

// blameCross returns the head blocks a check of the packages in named
// blames. When cj.known, the baseline recorded its blocks and the
// candidates are those the change added (score.AddedCrossBlocks): new by
// the set of packages they touch, at least duplication.min_tokens_cross
// tokens long and not moved by a rename, as a check of every package
// judges them. Otherwise every head block is a candidate. A candidate is
// blamed when one of its occurrences lies in a named package.
func blameCross(head []metrics.CrossBlock, cj crossJudgment, named map[string]bool) crossBlame {
	cand := head
	if cj.known {
		cand = cj.added
	}
	b := crossBlame{known: cj.known}
	for i := range cand {
		if touchesAny(&cand[i], named) {
			b.blocks = append(b.blocks, cand[i])
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
		sb.WriteString(" Added blocks touching the checked package are shared by ")
	} else {
		sb.WriteString(" " + score.CrossCountNote + " Blocks touching the checked package are shared by ")
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
			sb.WriteString(Rel(modulePath, p))
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

// touchesAny reports whether an occurrence of b lies in a package in named.
func touchesAny(b *metrics.CrossBlock, named map[string]bool) bool {
	for _, o := range b.Occurrences {
		if named[o.Package] {
			return true
		}
	}
	return false
}

// suggestion returns the module row's dup_blocks_cross_pkg suggestion:
// blame's when a check of named packages blamed a block, else the sentence
// naming the blocks the change added when it added any, else def, the
// row's usual sentence, which is followed by score.CrossCountNote when the
// baseline recorded no blocks, so the full count was judged. m is the
// module row and modulePath the module's path.
func (j crossJudgment) suggestion(blame *crossBlame, m metrics.RawMetrics, modulePath, def string) string {
	if blame != nil {
		if s := blame.suggestion(m, modulePath); s != "" {
			return s
		}
	} else if s := score.AddedCrossSuggestion(j.added, j.floor); s != "" {
		return s
	}
	if !j.known {
		return def + " " + score.CrossCountNote
	}
	return def
}
