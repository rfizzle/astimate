package compose

import (
	"cmp"
	"math"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/report/rows"
)

// moduleRow is the package_path of a check's module row.
const moduleRow = "<module>"

// Input is what Build narrates: the engine's results and the settings
// the sentences quote.
type Input struct {
	// ModulePath is the module's path; empty for a module without one.
	ModulePath string
	// RootName names the module when ModulePath is empty: the base name
	// of its root directory.
	RootName string
	// Language is the extractor's language.
	Language string
	// Head is the commit at HEAD; empty outside git.
	Head string
	// Base is the ref the change is compared against; empty without a
	// change section.
	Base string
	// Limits are the capacity ceilings of the five size metrics.
	Limits []narrate.Limit
	// ChurnWindow is the churn window in commits; 0 when churn is off.
	ChurnWindow int
	// MinTokens is the duplicate-block floor, for the glossary.
	MinTokens int
	// Basis is what the human estimate is computed with, for the labels
	// that say where the engineer time comes from.
	Basis report.HumanBasis
	// OnePassMax and FewPassesMax are the configured tier bounds, in agent
	// passes, that the sittings words follow; unset, the SPEC.md 7.4
	// defaults.
	OnePassMax, FewPassesMax float64
	// Packages are every package's report, its details included.
	Packages []report.Report
	// Check is the check of the changed packages against Base; nil
	// without one.
	Check *report.Check
	// Skipped are the module's packages that were not analysed, as
	// report.Check.Skipped lists them; when nil, Check's are used.
	Skipped []report.Skipped
	// AstimateVersion and ConfigVersion are recorded on the report.
	AstimateVersion string
	ConfigVersion   string
}

// Build narrates in as the module report.
func Build(in *Input) narrate.Module {
	m := narrate.Module{
		ModulePath:      in.ModulePath,
		Name:            moduleName(in.ModulePath, in.RootName),
		Language:        in.Language,
		Head:            in.Head,
		Limits:          slices.Clone(in.Limits),
		ChurnWindow:     in.ChurnWindow,
		Packages:        make([]narrate.Package, 0, len(in.Packages)),
		AstimateVersion: in.AstimateVersion,
		ConfigVersion:   in.ConfigVersion,
		Glossary:        narrate.Glossary(in.Language, in.Limits, in.MinTokens, in.ChurnWindow, in.Basis),
	}
	if m.Limits == nil {
		m.Limits = []narrate.Limit{}
	}
	bounds := tiersOf(in)
	days, low, high := 0.0, 0.0, 0.0
	for i := range in.Packages {
		p := pkg(&in.Packages[i], in.Limits, in.ChurnWindow, bounds)
		days, low, high = days+p.Rebuild.HumanDays, low+p.Rebuild.HumanDaysLow, high+p.Rebuild.HumanDaysHigh
		m.Counts.Packages++
		if sittings(in.Packages[i].Rebuild.AgentPasses, bounds) > 0 {
			m.Counts.MultiSitting++
		}
		switch p.Band {
		case narrate.BandSplit:
			m.Counts.Split++
		case narrate.BandWatch:
			m.Counts.Watch++
		default:
			m.Counts.Fine++
		}
		if !p.Tests.HasTests {
			m.Counts.WithoutTests++
		}
		m.Packages = append(m.Packages, p)
	}
	slices.SortFunc(m.Packages, func(a, b narrate.Package) int { return strings.Compare(a.Path, b.Path) })
	m.Counts.HumanDays, m.Counts.HumanDaysLow, m.Counts.HumanDaysHigh = math.Round(days), math.Round(low), math.Round(high)
	m.Counts.HumanTime = human(m.Counts.HumanDaysLow, m.Counts.HumanDaysHigh)
	m.Counts.HumanBasis = narrate.HumanBasis(in.Basis)
	m.Counts.HumanBasisShort = narrate.HumanBasisShort(in.Basis)
	hot := hotOrder(m.Packages)
	m.Hotspots = make([]string, 0, hotspotCount)
	var first, hottest *narrate.Package
	churned := false
	for _, p := range hot {
		churned = churned || p.Churn != nil
		if p.Band == narrate.BandFine {
			continue
		}
		if first == nil {
			first = p
		}
		if len(m.Hotspots) < hotspotCount {
			m.Hotspots = append(m.Hotspots, p.Path)
		}
		if hottest == nil && p.Band == narrate.BandSplit && p.Hotspot > 0 {
			hottest = p
		}
	}
	m.Skipped, m.NotAnalysed = notAnalysedOf(in)
	m.Headline = headline(&m, first)
	m.Lead = lead(&m, hottest, churned)
	if in.Check != nil {
		m.Change = change(in.Check, in.Base, &m)
	}
	return m
}

// moduleName is the module's short name: the last element of its path
// that is not a major-version suffix, else rootName, with its first letter
// upper-cased.
func moduleName(modulePath, rootName string) string {
	name := rootName
	if modulePath != "" {
		name = path.Base(modulePath)
		if dir := path.Dir(modulePath); isMajor(name) && dir != "." {
			name = path.Base(dir)
		}
	}
	return capitalize(name)
}

// capitalize returns s with its first letter upper-cased.
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// isMajor reports whether s is a module major-version suffix, v2 or above.
func isMajor(s string) bool {
	rest, ok := strings.CutPrefix(s, "v")
	return ok && rest != "" && strings.Trim(rest, "0123456789") == ""
}

// hotOrder returns pointers to ps in narrate.Hotter order.
func hotOrder(ps []narrate.Package) []*narrate.Package {
	out := make([]*narrate.Package, len(ps))
	for i := range ps {
		out[i] = &ps[i]
	}
	slices.SortStableFunc(out, narrate.Hotter)
	return out
}

// pkg narrates one package's report.
func pkg(r *report.Report, limits []narrate.Limit, window int, t tiers) narrate.Package {
	met := &r.Metrics
	p := narrate.Package{
		Path:  r.PackagePath,
		Role:  r.Role,
		Tier:  string(r.Rebuild.Tier),
		FanIn: met.FanIn,
		Tests: narrate.Tests{TestFuncs: met.TestFuncs, HasTests: met.HasTests, Untested: met.UntestedExports,
			Exports: met.ExportedSymbols},
	}
	p.Ceilings, p.Over = ceilings(met, r.Role, limits)
	p.Band = band(p.Tier, len(p.Over), met.DuplicationPct)
	p.Hotspot = rows.Hotspot(met.ChurnCommits, r.Rebuild.AgentPasses)
	if met.ChurnCommits != nil {
		p.Churn = &narrate.Churn{Commits: *met.ChurnCommits}
		if met.ChurnAuthors != nil {
			p.Churn.Authors = *met.ChurnAuthors
		}
	}
	p.Rebuild = narrate.Rebuild{
		AgentPasses:   r.Rebuild.AgentPasses,
		HumanDays:     r.Rebuild.HumanDays,
		HumanDaysLow:  r.Rebuild.HumanDaysLow,
		HumanDaysHigh: r.Rebuild.HumanDaysHigh,
		HumanTime:     human(r.Rebuild.HumanDaysLow, r.Rebuild.HumanDaysHigh),
		HumanShort:    humanShort(r.Rebuild.HumanDaysLow, r.Rebuild.HumanDaysHigh),
		RebuildTokens: r.Rebuild.RebuildTokens,
		Sittings:      sittingsBand(sittings(r.Rebuild.AgentPasses, t)),
		SittingsText:  sittingsText(r.Rebuild.AgentPasses, t.one),
		Drivers:       shares(&r.Rebuild),
	}
	p.Rebuild.Say = rebuildSay(&p.Rebuild)
	p.Copies = narrate.Copies{Pct: met.DuplicationPct, Blocks: met.DupBlocks, CodeBlocks: met.DupBlocksCode, Lines: met.SLOC,
		Level: copiesLevel(met.DuplicationPct), Chunks: []narrate.Chunk{}}
	p.Complexity = narrate.Complexity{Functions: met.FuncCount, CognitiveP90: met.CognitiveP90,
		CyclomaticP90: met.CyclomaticP90, Nesting: met.MaxNesting, Hardest: []narrate.Function{}}
	if d := r.Details; d != nil {
		p.Copies.Chunks = chunks(d.Duplicates)
		p.Complexity.Buckets = buckets(d.Functions)
		p.Complexity.Hardest = hardest(d.Functions)
		if len(d.Functions) > 0 {
			p.Complexity.Functions = len(d.Functions)
		}
	}
	p.Copies.Say = copiesSay(met.DuplicationPct)
	p.Copies.Longest = longest(p.Copies.Chunks)
	p.Complexity.Say = complexitySay(&p.Complexity)
	p.Story = story(&p)
	p.Todo = todo(&p)
	p.Changes = changes(&p, window)
	p.Facts = facts(&p, window)
	return p
}

// change narrates a check of the changed packages against base, m
// supplying each package's view.
func change(c *report.Check, base string, m *narrate.Module) *narrate.Change {
	byPath := make(map[string]*narrate.Package, len(m.Packages))
	for i := range m.Packages {
		byPath[m.Packages[i].Path] = &m.Packages[i]
	}
	ch := &narrate.Change{Base: base, Changed: len(c.Packages), Failing: []narrate.Failing{}, WarningGroups: []narrate.WarningGroup{},
		PassedPaths: []string{}, Deleted: c.Deleted, Notices: c.Notices()}
	total := c.Listed
	if total == 0 {
		total = len(m.Packages)
	}
	ch.Unchanged = max(0, total-ch.Changed)
	ch.NotAnalysed = len(m.Skipped)
	rows := make([]*report.Report, 0, len(c.Packages)+1)
	if c.Module != nil {
		rows = append(rows, &c.Module.Report)
		if mt := c.Module.Report.Metrics.DupBlocksCrossPkg; mt != nil {
			ch.Cross = &narrate.Cross{Head: float64(*mt)}
			if b := c.Module.Report.Baseline; b != nil && b.Metrics.DupBlocksCrossPkg != nil {
				v := float64(*b.Metrics.DupBlocksCrossPkg)
				ch.Cross.Base = &v
			}
		}
	}
	for i := range c.Packages {
		rows = append(rows, &c.Packages[i].Report)
	}
	groups := map[string]*narrate.WarningGroup{}
	untested := false
	for _, r := range rows {
		if b := r.Baseline; b != nil && ch.BaseCommit == "" {
			ch.BaseCommit = b.Ref
		}
		for i := range r.Exemptions {
			e := &r.Exemptions[i]
			ch.Exempted = append(ch.Exempted, narrate.Exempted{Path: r.PackagePath, Metric: e.Metric,
				Label: strings.ReplaceAll(e.Metric, "_", " "), Reason: e.Reason})
		}
		for i := range r.Warnings {
			w := &r.Warnings[i]
			untested = untested || w.Metric == "untested_exports"
			g := groups[w.Metric]
			if g == nil {
				g = &narrate.WarningGroup{Metric: w.Metric, Label: strings.ReplaceAll(w.Metric, "_", " "), Why: warnWhy(w.Metric, m.Limits)}
				groups[w.Metric] = g
			}
			g.Packages = append(g.Packages, narrate.Warned{Path: r.PackagePath, Base: w.Base, Head: w.Head,
				Limit: w.Limit, Do: firstClause(w.Suggestion)})
			ch.Warnings++
		}
		if len(r.Violations) == 0 {
			if len(r.Warnings) == 0 && r.PackagePath != moduleRow {
				ch.PassedPaths = append(ch.PassedPaths, r.PackagePath)
			}
			continue
		}
		f := narrate.Failing{Path: r.PackagePath, Summary: failingSummary(len(r.Violations), byPath[r.PackagePath]),
			Findings: make([]narrate.Finding, 0, len(r.Violations))}
		for i := range r.Violations {
			v := &r.Violations[i]
			untested = untested || v.Metric == "untested_exports" || v.Metric == "has_tests"
			f.Findings = append(f.Findings, finding(v))
		}
		ch.Violations += len(f.Findings)
		ch.Failing = append(ch.Failing, f)
	}
	hotOf := func(path string) float64 {
		if p := byPath[path]; p != nil {
			return p.Hotspot
		}
		return 0
	}
	slices.SortStableFunc(ch.Failing, func(a, b narrate.Failing) int {
		return cmp.Or(cmp.Compare(len(b.Findings), len(a.Findings)), cmp.Compare(hotOf(b.Path), hotOf(a.Path)))
	})
	for _, g := range groups {
		ch.WarningGroups = append(ch.WarningGroups, *g)
	}
	slices.SortFunc(ch.WarningGroups, func(a, b narrate.WarningGroup) int {
		return cmp.Or(cmp.Compare(len(b.Packages), len(a.Packages)), strings.Compare(a.Metric, b.Metric))
	})
	ch.Failed = len(ch.Failing)
	ch.Passed = len(ch.PassedPaths)
	ch.Verdict = "PASS"
	if ch.Failed > 0 {
		ch.Verdict = "FAIL"
	}
	ch.Headline = changeHeadline(ch)
	ch.Lead = changeLead(ch, untested)
	ch.PassedSummary = passedSummary(ch)
	return ch
}

// notAnalysedOf returns in's packages that were not analysed, from
// in.Skipped or else in.Check, by path, and the line counting them.
func notAnalysedOf(in *Input) ([]narrate.Skipped, string) {
	src := in.Skipped
	if src == nil && in.Check != nil {
		src = in.Check.Skipped
	}
	if len(src) == 0 {
		return nil, ""
	}
	out := make([]narrate.Skipped, 0, len(src))
	for i := range src {
		out = append(out, narrate.Skipped{Path: src[i].Package, Reason: src[i].Reason, Error: src[i].Error})
	}
	slices.SortFunc(out, func(a, b narrate.Skipped) int { return strings.Compare(a.Path, b.Path) })
	return out, notAnalysed(narrate.GroupSkipped(out))
}

// finding narrates one violation.
func finding(v *report.Finding) narrate.Finding {
	f := narrate.Finding{Metric: v.Metric, Label: strings.ReplaceAll(v.Metric, "_", " "), Base: v.Base, Head: v.Head,
		Limit: v.Limit, Why: why(v.Metric), Do: firstClause(v.Suggestion), Location: v.Location}
	if by, allowed, ok := overMax(v.Limit); ok {
		f.OverBy, f.Allowed = &by, &allowed
	}
	f.Moved, f.MovedShort = moved(&f)
	return f
}
