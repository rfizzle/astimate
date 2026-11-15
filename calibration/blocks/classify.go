package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/blocklog"
	"github.com/rfizzle/astimate/internal/metrics"
)

// The resolutions of a block, in the order their rules are tried (SPEC.md
// 11.5); the first that matches wins.
const (
	resExempted    = "exempted"
	resSplit       = "split"
	resReshaped    = "reshaped"
	resReduced     = "reduced"
	resUnexplained = "unexplained"
	resOpen        = "open"
)

// resolutions returns every resolution in rule order, open last.
func resolutions() []string {
	return []string{resExempted, resSplit, resReshaped, resReduced, resUnexplained, resOpen}
}

// splitFall is the share by which a capacity metric must fall for a block
// to read as split: the 25% by which the split-or-extraction labels of
// SPEC.md 11.3 require a package's sloc to fall.
const splitFall = 0.25

// surfaceMetric is the metric the reshaped-exports heads-up is about
// (SPEC.md 8.1).
const surfaceMetric = "exported_symbols"

// The rule kinds classify reads: the split threshold applies to capacity
// rules, and a requirement's value is reduced when it moved either way.
const (
	capacity    = "capacity"
	requirement = "requirement"
)

// episode is one block and how it was resolved: the block lines on one
// package and metric up to the first pass line after them.
type episode struct {
	Module  string `json:"module"`
	Package string `json:"package"`
	Metric  string `json:"metric"`
	// Kind is the rule's kind as the last block line recorded it.
	Kind string `json:"kind"`
	// Blocks counts the block lines before the resolution: a block the
	// next check fired again counts once more.
	Blocks int `json:"blocks"`
	// BlockedAt and BlockCommit are the first block line's time and
	// commit.
	BlockedAt   string `json:"blocked_at"`
	BlockCommit string `json:"block_commit,omitempty"`
	// Base, Head and Limit are the last block line's.
	Base  *float64 `json:"base"`
	Head  float64  `json:"head"`
	Limit string   `json:"limit"`
	// Resolution is one of resolutions, and Evidence says what matched.
	Resolution string `json:"resolution"`
	Evidence   string `json:"evidence"`
	// ResolvedAt, ResolveCommit and Value are the pass line's time,
	// commit and value of the metric; empty or nil while open.
	ResolvedAt    string   `json:"resolved_at,omitempty"`
	ResolveCommit string   `json:"resolve_commit,omitempty"`
	Value         *float64 `json:"value"`
	// ConfigChanged reports a pass under another config_version than the
	// last block's.
	ConfigChanged bool `json:"config_changed"`
	// Deleted reports that the package no longer existed at the pass.
	Deleted bool `json:"deleted,omitempty"`

	lastConfig string
}

// episodes pairs lines, one log's in file order, into episodes in the
// order their first block appeared. A pass line with no block open on its
// package and metric is ignored.
func episodes(lines []blocklog.Line) []episode {
	var out []episode
	open := make(map[blocklog.Key]int)
	for _, l := range lines {
		k := l.Key()
		i, ok := open[k]
		if l.Event == blocklog.EventBlock {
			if !ok {
				out = append(out, episode{Module: l.Module, Package: l.Package, Metric: l.Metric,
					BlockedAt: l.Time, BlockCommit: l.Commit, Resolution: resOpen,
					Evidence: "no check has passed on the package since"})
				i = len(out) - 1
				open[k] = i
			}
			e := &out[i]
			e.Blocks++
			e.Kind, e.Base, e.Head, e.Limit, e.lastConfig = l.Block.Kind, l.Block.Base, l.Block.Head, l.Block.Limit, l.ConfigVersion
			continue
		}
		if !ok {
			continue
		}
		e := &out[i]
		e.ResolvedAt, e.ResolveCommit, e.Value = l.Time, l.Commit, l.Pass.Value
		e.ConfigChanged, e.Deleted = l.ConfigVersion != e.lastConfig, !l.Pass.Exists
		e.Resolution, e.Evidence = classify(e, l.Pass)
		delete(open, k)
	}
	return out
}

// classify returns the resolution of e by the first rule p matches, and
// the evidence for it.
func classify(e *episode, p *blocklog.Pass) (resolution, evidence string) {
	value := p.Value
	if !p.Exists {
		zero := 0.0
		value = &zero
	}
	moduleWide := metrics.ModuleWide(e.Metric)
	switch {
	case p.Exempted:
		return resExempted, "an exemption covers " + e.Metric + " on " + e.Package
	case seededFrom(p) != "":
		return resSplit, seededFrom(p) + " was seeded by a rename from " + e.Package
	case !p.Exists && e.Kind == capacity:
		return resSplit, e.Package + " was deleted"
	case e.Kind == capacity && !moduleWide && value != nil && e.Head > 0 && (e.Head-*value)/e.Head >= splitFall:
		return resSplit, fell(e.Metric, e.Head, *value) + ", at least the 25% a split takes"
	case e.Metric == surfaceMetric && p.Reshaped:
		return resReshaped, "the reshaped-exports heads-up named " + strings.Join(p.ReshapedNames, ", ")
	case moduleWide:
		return resReduced, "the module row passed; the log records its full count, not the count the gate judged, " +
			"so how far it fell is not known"
	case value != nil && *value < e.Head && e.Kind != requirement,
		value != nil && *value != e.Head && e.Kind == requirement:
		return resReduced, residual(e, *value)
	}
	ev := "passed with " + e.Metric + " at " + valueText(value) + " where the block had " + num(e.Head) +
		": the limit, the baseline, a guard or the configuration moved, which the log does not tell apart"
	if e.ConfigChanged {
		ev += " (config_version changed)"
	}
	return resUnexplained, ev
}

// seededFrom returns the first new package of p a rename seeded from the
// blocked package, or empty.
func seededFrom(p *blocklog.Pass) string {
	for _, n := range p.NewPackages {
		if n.FromBlocked {
			return n.Package
		}
	}
	return ""
}

// residual is the evidence of a reduced block, which says what the log
// cannot tell apart rather than guess.
func residual(e *episode, v float64) string {
	if e.Kind == requirement {
		return e.Metric + " went from " + num(e.Head) + " to " + num(v) + "; the log cannot tell how"
	}
	return fell(e.Metric, e.Head, v) + ": a smaller change, an extraction or a workaround the gate does not see; " +
		"the log cannot tell which"
}

// fell says how far metric fell from head to v.
func fell(metric string, head, v float64) string {
	pct := 0.0
	if head > 0 {
		pct = math.Round((head - v) / head * 100)
	}
	return fmt.Sprintf("%s fell %s%% (%s -> %s)", metric, num(pct), num(head), num(v))
}

// valueText formats v, or "null" when the row did not compute it.
func valueText(v *float64) string {
	if v == nil {
		return "null"
	}
	return num(*v)
}

// num formats v in the shortest form that reads it back, as JSON does.
func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
