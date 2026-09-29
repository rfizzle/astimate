// Package split is the split-or-extraction labeling rule (SPEC.md 11.3):
// a commit is block when a later commit of the replayed range had to split
// a package the commit grew past a capacity ceiling, or extract duplicate
// code the commit added. It reads only a replay's rows (commits.jsonl and
// packages.jsonl), never the repository.
package split

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/calibration/replay/labels"
)

// The rule's constants.
const (
	// growOver is the sloc_delta that marks growth of a package already
	// over a capacity max.
	growOver = 100
	// splitCut is the least share of a package's sloc a split removes.
	splitCut = 0.25
	// movedNotDeleted is the share of the module's total sloc a split
	// commit must lose less of.
	movedNotDeleted = 0.05
	// extractKeep is the share of the package's sloc an extraction must
	// lose less of.
	extractKeep = 0.25
)

// ruleText describes the rule in the labels file; %s is the lookahead.
const ruleText = "block when a later commit %s splits a package this commit grew past a ceiling, or " +
	"extracts duplicate code this commit added, read from the replay's rows. Split: the commit took a package " +
	"over a capacity rule's max (at or under it at the parent, or new, and over it at the commit) or added 100 " +
	"or more SLOC to a package already over one, and a later commit lowers that package's sloc by at least 25%% " +
	"(deleting the package counts) while the module's total sloc falls by less than 5%% (code moved, not " +
	"deleted). Extraction: the commit raised a package's dup_blocks, or the module row's dup_blocks_cross_pkg, " +
	"and a later commit that changes a file of that package (for the module row, of a package the commit " +
	"changed) lowers that count by at least what this commit added while the package's sloc (the module's " +
	"total, for the module row) falls by less than 25%%. Otherwise allow. The module's total is the sum over " +
	"every package the rows show, each carried from its nearest row, so a package no replayed commit changed " +
	"is not counted. A commit the replay did not load has no rows and is allow. Each reason ends with the " +
	"label's lookahead: the commits after it in the range, so the last commits of a range are censored."

// RuleText describes the rule with the given window, 0 for the rest of
// the range.
func RuleText(window int) string {
	look := "in the rest of the replayed range"
	if window > 0 {
		look = "within the next " + strconv.Itoa(window) + " first-parent commits of the range"
	}
	return fmt.Sprintf(ruleText, look)
}

// grown is a package a commit took past a ceiling or grew while over one,
// or into which it added duplicate blocks, with what it did.
type grown struct {
	pkg, what string
	// added is the number of duplicate blocks added, for the extraction
	// part.
	added int
}

// finding is one part of the rule that fired on a commit: the package,
// the later commit's index and a clause saying what that commit did.
type finding struct {
	part, pkg, what, why string
	by                   int
}

// Decide labels every commit of r. window bounds how many later commits
// are looked at; 0 means the rest of the range. Labels carry no Agent.
func Decide(r *Replay, window int) []labels.Label {
	out := make([]labels.Label, len(r.Commits))
	for i := range r.Commits {
		last := len(r.Commits) - 1
		if window > 0 {
			last = min(last, i+window)
		}
		look := fmt.Sprintf(" (lookahead %d commits)", last-i)
		l := labels.Label{Hash: r.Commits[i].Commit, Provenance: labels.Rule}
		overs, dups := r.growth(i)
		var fs []finding
		if f, ok := r.firstSplit(i, last, overs); ok {
			fs = append(fs, f)
		}
		if f, ok := r.firstExtract(i, last, dups); ok {
			fs = append(fs, f)
		}
		if len(fs) == 0 {
			l.Verdict, l.Reason = labels.Allow, r.allowReason(i, overs, dups)+look
			out[i] = l
			continue
		}
		why := make([]string, 0, len(fs))
		for _, f := range fs {
			c := &r.Commits[f.by]
			l.Fired = append(l.Fired, labels.Evidence{Part: f.part, By: c.Commit, Package: f.pkg})
			why = append(why, fmt.Sprintf("%s: %s; %.12s (%q) %d commits later %s", f.part, f.what, c.Commit, c.Subject, f.by-i, f.why))
		}
		l.Verdict, l.Reason = labels.Block, strings.Join(why, "; ")+look
		out[i] = l
	}
	return out
}

// growth returns the packages commit i took over a capacity max or grew
// by growOver SLOC while over one, and those it added duplicate blocks to
// (the module row for cross-package ones), each in row order.
func (r *Replay) growth(i int) (overs, dups []grown) {
	for k := range r.rows[i] {
		row := &r.rows[i][k]
		if !row.module() {
			if what, ok := r.overCeiling(row); ok {
				overs = append(overs, grown{pkg: row.Package, what: what})
			}
		}
		before, after := row.dups(row.Base), row.dups(&row.Metrics)
		if added := after - before; added > 0 {
			kind := "duplicate blocks to " + row.Package
			if row.module() {
				kind = "cross-package duplicate blocks"
			}
			dups = append(dups, grown{pkg: row.Package, added: added, what: fmt.Sprintf("added %d %s (%d to %d)", added, kind, before, after)})
		}
	}
	return overs, dups
}

// overCeiling reports whether row crossed a capacity max, or grew by
// growOver SLOC or more while over one at the parent, and says which.
func (r *Replay) overCeiling(row *Row) (string, bool) {
	var crossed, over []string
	for _, t := range r.capacity(row.Language) {
		head, ok := row.Metrics.Value(t.Metric)
		if !ok || t.Max == nil {
			continue
		}
		base := 0.0
		if row.Base != nil {
			base, _ = row.Base.Value(t.Metric)
		}
		switch {
		case row.Base != nil && base > *t.Max:
			over = append(over, t.Metric+" "+num(*t.Max))
		case head > *t.Max:
			crossed = append(crossed, fmt.Sprintf("%s max %s (%s to %s)", t.Metric, num(*t.Max), num(base), num(head)))
		}
	}
	switch {
	case len(crossed) > 0:
		return "took " + row.Package + " over " + strings.Join(crossed, ", "), true
	case len(over) > 0 && row.SLOCDelta >= growOver:
		return fmt.Sprintf("added %d SLOC to %s, already over %s", row.SLOCDelta, row.Package, strings.Join(over, ", ")), true
	}
	return "", false
}

// num formats a metric value without a trailing .0.
func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// firstSplit returns the first commit after i, up to last, that split a
// package of overs.
func (r *Replay) firstSplit(i, last int, overs []grown) (finding, bool) {
	return first(i, last, overs, r.splitAt, labels.PartSplit)
}

// splitAt reports whether commit j split g's package: cut its sloc by
// splitCut or more while the module's total fell by less than
// movedNotDeleted; the clause says what j did.
func (r *Replay) splitAt(j int, g grown) (string, bool) {
	before, after, ok := r.slocAcross(g.pkg, j)
	cut := ok && before > 0 && float64(before-after) >= splitCut*float64(before)
	if !cut {
		return "", false
	}
	fall, total := r.moduleFall(j)
	return fmt.Sprintf("cut %s from %d to %d SLOC while the module's total changed by %s", g.pkg, before, after, signedPct(-fall, total)),
		below(fall, total, movedNotDeleted)
}

// first returns the finding of part for the first commit j after i, up to
// last, and the first of gs in order, that undid reports undid g, with
// the clause it returns.
func first(i, last int, gs []grown, undid func(j int, g grown) (string, bool), part string) (finding, bool) {
	for j := i + 1; len(gs) > 0 && j <= last; j++ {
		for _, g := range gs {
			if why, ok := undid(j, g); ok {
				return finding{part: part, pkg: g.pkg, by: j, what: g.what, why: why}, true
			}
		}
	}
	return finding{}, false
}

// slocAcross returns package p's sloc before and after commit j, when
// commit j has a row for it or deleted it.
func (r *Replay) slocAcross(p string, j int) (before, after int, ok bool) {
	if row := r.rowOf(j, p); row != nil && row.Base != nil {
		return row.Base.SLOC, row.Metrics.SLOC, true
	}
	for _, d := range r.Commits[j].PackagesDeleted {
		if d == p {
			return r.slocBefore(p, j), 0, true
		}
	}
	return 0, 0, false
}

// firstExtract returns the first commit after i, up to last, that changed
// a file of a package of dups and removed at least as many duplicate
// blocks there as commit i added, without deleting a quarter of its code.
func (r *Replay) firstExtract(i, last int, dups []grown) (finding, bool) {
	changed := r.Commits[i].PackagesChanged
	return first(i, last, dups, func(j int, g grown) (string, bool) { return r.extractAt(j, g, changed) }, labels.PartExtract)
}

// extractAt reports whether commit j extracted g's duplicate blocks: it
// changed a file of g's package (of changed, for the module row), and
// removed at least g.added blocks there while its SLOC (the module's
// total, for the module row) fell by less than extractKeep; the clause
// says what j did.
func (r *Replay) extractAt(j int, g grown, changed []string) (string, bool) {
	row := r.rowOf(j, g.pkg)
	if row == nil || row.Base == nil {
		return "", false
	}
	removed := row.dups(row.Base) - row.dups(&row.Metrics)
	where, touched := g.pkg, r.touches(j, g.pkg)
	before, fall := row.Base.SLOC, row.Base.SLOC-row.Metrics.SLOC
	if row.module() {
		where, touched = "the module's cross-package duplicates", r.touches(j, changed...)
		fall, before = r.moduleFall(j)
	}
	return fmt.Sprintf("removed %d duplicate blocks from %s while the SLOC there changed by %s", removed, where, signedPct(-fall, before)),
		touched && removed >= g.added && below(fall, before, extractKeep)
}

// below reports whether fall is less than share of total; with no total
// nothing fell.
func below(fall, total int, share float64) bool {
	return total <= 0 || float64(fall) < share*float64(total)
}

// signedPct formats n over d as a signed percentage; +0.0% when d is 0.
func signedPct(n, d int) string {
	if d == 0 {
		return "+0.0%"
	}
	return fmt.Sprintf("%+.1f%%", 100*float64(n)/float64(d))
}

// allowReason explains an allow label for commit i.
func (r *Replay) allowReason(i int, overs, dups []grown) string {
	if !r.Commits[i].Loaded {
		return "the replay did not load the commit, so there are no rows to read"
	}
	did := make([]string, 0, len(overs)+len(dups))
	for _, g := range overs {
		did = append(did, g.what)
	}
	for _, g := range dups {
		did = append(did, g.what)
	}
	if len(did) == 0 {
		return "took no package over a capacity max, grew none over one by 100 or more SLOC, and added no duplicate blocks"
	}
	return strings.Join(did, "; ") + "; no later commit split or extracted it"
}
