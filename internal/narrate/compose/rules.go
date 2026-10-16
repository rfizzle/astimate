package compose

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/report/details"
)

// The numbers the rules of SPEC.md 10.3 ship.
const (
	// tierPartition and tierFewPasses are the rebuild tiers the bands read.
	tierPartition = "PARTITION"
	tierFewPasses = "FEW_PASSES"
	// watchDupPct is the duplication_pct at or over which a package is
	// watched.
	watchDupPct = 25
	// copiesWatchPct is where the copies view turns from fine to watch.
	copiesWatchPct = 10
	// easyMax and careMax bound the function buckets.
	easyMax = 10
	careMax = 20
	// daysPerWeek, daysPerMonth and daysPerYear are working days.
	daysPerWeek  = 5
	daysPerMonth = 21
	daysPerYear  = 220
	// oneSitting and fewSittings bound the sittings bands, in passes.
	oneSitting  = 1
	fewSittings = 3
	// meterWatch is the share of a ceiling at which its meter is watched.
	meterWatch = 0.8
	// hotspotCount is how many hotspots the module lists; chunkCount and
	// hardestCount how many chunks and functions a package names.
	hotspotCount = 5
	chunkCount   = 2
	hardestCount = 3
	// libraryRole is the role a package without one is judged as.
	libraryRole = "library"
)

// ceilingValue returns m's value of a ceiling metric, and whether the
// metric is one of the five the bands read.
func ceilingValue(m *metrics.RawMetrics, metric string) (float64, bool) {
	switch metric {
	case "sloc":
		return float64(m.SLOC), true
	case "tokens_est":
		return float64(m.TokensEst), true
	case "largest_file_sloc":
		return float64(m.LargestFileSLOC), true
	case "exported_symbols":
		return float64(m.ExportedSymbols), true
	case "internal_imports":
		return float64(m.InternalImports), true
	}
	return 0, false
}

// judges reports whether l applies to a package of role r, the empty role
// read as a library.
func judges(l narrate.Limit, r string) bool {
	if r == "" {
		r = libraryRole
	}
	return len(l.Roles) == 0 || slices.Contains(l.Roles, r)
}

// ceilings returns m's value against each limit, role r deciding which
// rules judge it, and the metrics it is over.
func ceilings(m *metrics.RawMetrics, r string, limits []narrate.Limit) (cs []narrate.Ceiling, over []string) {
	cs = make([]narrate.Ceiling, 0, len(limits))
	over = []string{}
	for _, l := range limits {
		v, ok := ceilingValue(m, l.Metric)
		if !ok {
			continue
		}
		c := narrate.Ceiling{Metric: l.Metric, Label: metricLabel(l.Metric), Value: v, Max: l.Max, Judged: judges(l, r)}
		c.Over = c.Judged && v > l.Max
		switch {
		case c.Over:
			c.Level = narrate.BandSplit
		case l.Max > 0 && v >= meterWatch*l.Max:
			c.Level = narrate.BandWatch
		default:
			c.Level = narrate.BandFine
		}
		if c.Over {
			over = append(over, l.Metric)
		}
		cs = append(cs, c)
	}
	return cs, over
}

// band returns the band of a package with tier, over its ceilings over
// and with duplication_pct dup.
func band(tier string, over int, dup float64) string {
	switch {
	case tier == tierPartition || over >= 2:
		return narrate.BandSplit
	case over == 1 || tier == tierFewPasses || dup >= watchDupPct:
		return narrate.BandWatch
	}
	return narrate.BandFine
}

// level returns the band a function of cognitive complexity cog is
// coloured with.
func level(cog int) string {
	switch {
	case cog > careMax:
		return narrate.BandSplit
	case cog > easyMax:
		return narrate.BandWatch
	}
	return narrate.BandFine
}

// buckets counts fns by cognitive complexity; nil without any.
func buckets(fns []details.Function) *narrate.Buckets {
	if len(fns) == 0 {
		return nil
	}
	var b narrate.Buckets
	for _, f := range fns {
		switch level(f.Cognitive) {
		case narrate.BandSplit:
			b.Hard++
		case narrate.BandWatch:
			b.Care++
		default:
			b.Easy++
		}
	}
	return &b
}

// hardest returns the hardestCount functions of fns with the highest
// cognitive complexity, ties in declaration order.
func hardest(fns []details.Function) []narrate.Function {
	sorted := slices.Clone(fns)
	slices.SortStableFunc(sorted, func(a, b details.Function) int { return cmp.Compare(b.Cognitive, a.Cognitive) })
	out := make([]narrate.Function, 0, min(hardestCount, len(sorted)))
	for _, f := range sorted[:min(hardestCount, len(sorted))] {
		out = append(out, narrate.Function{Name: f.Name, File: f.File, Line: f.Line, Cognitive: f.Cognitive,
			Cyclomatic: f.Cyclomatic, Level: level(f.Cognitive)})
	}
	return out
}

// chunks returns the first chunkCount occurrences of spans. Spans list
// the occurrences block by block, longest block in tokens first, and every
// block occurs at least twice, so these are two places of the same code.
// Picking by line count instead could pair occurrences of two different
// blocks, and the sentence would name a repeat that is not one.
func chunks(spans []details.Span) []narrate.Chunk {
	out := make([]narrate.Chunk, 0, chunkCount)
	for _, s := range spans[:min(chunkCount, len(spans))] {
		out = append(out, narrate.Chunk{File: s.File, Start: s.StartLine, End: s.EndLine, Lines: s.EndLine - s.StartLine + 1})
	}
	return out
}

// copiesLevel colours a duplication_pct.
func copiesLevel(pct float64) string {
	switch {
	case pct >= watchDupPct:
		return narrate.BandSplit
	case pct >= copiesWatchPct:
		return narrate.BandWatch
	}
	return narrate.BandFine
}

// sittings returns the sittings band of passes: 0 one sitting, 1 a few
// sittings, 2 split it first.
func sittings(passes float64) int {
	switch {
	case passes <= oneSitting:
		return 0
	case passes <= fewSittings:
		return 1
	}
	return 2
}

// shares returns each driver of r as a share of its rebuild_tokens.
func shares(r *report.Rebuild) []narrate.Share {
	total := max(1, r.RebuildTokens)
	out := make([]narrate.Share, 0, len(r.Drivers))
	for _, d := range r.Drivers {
		out = append(out, narrate.Share{Term: d.Term, Label: driverLabel(d.Term), Tokens: d.Tokens,
			Pct: int(math.Round(100 * float64(d.Tokens) / float64(total)))})
	}
	return out
}

// overMax parses the gate's limit text of a capacity rule over its
// ceiling, "over max by N, allowed M", in its first clause. It is the one
// place the text is read.
func overMax(limit string) (by, allowed float64, ok bool) {
	first, _, _ := strings.Cut(limit, ";")
	rest, found := strings.CutPrefix(first, "over max by ")
	if !found {
		return 0, 0, false
	}
	byText, allowedText, found := strings.Cut(rest, ", allowed ")
	if !found {
		return 0, 0, false
	}
	by, err1 := strconv.ParseFloat(strings.ReplaceAll(byText, ",", ""), 64)
	allowed, err2 := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(allowedText), ",", ""), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return by, allowed, true
}

// round1 rounds v to one decimal.
func round1(v float64) float64 { return math.Round(v*10) / 10 }
