// Package judge gates the rows of a check, each package and the module
// row, against their baseline: it fills changed_func_cognitive_max from the
// function-level diff, evaluates the rules, applies the exemptions, builds
// each row's report with its findings located on the file and line that
// caused them, blames a check of named packages only for the cross-package
// copies it made, and reports the exemptions the check ignored or did not
// need. Package engine composes it; it knows nothing of targets or
// package selection.
package judge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// Options are what a Checker judges rows with.
type Options struct {
	// Ext is the extractor the head rows were extracted with, and Mod the
	// head module.
	Ext metrics.Extractor
	Mod *metrics.ModuleContext
	// Base is the baseline the rows are compared against.
	Base baseline.Baseline
	// Config is the configuration of the module's language.
	Config config.Effective
	// Tokenizer is the method Ext counts tokens with.
	Tokenizer string
	// Version is the astimate version recorded in reports.
	Version string
	// Now is the time an exemption's expiry is judged at.
	Now time.Time
	// Logger receives skipped rules and exemption notices without a row.
	Logger *slog.Logger
}

// Checker judges the rows of one check.
type Checker struct {
	o           Options
	pkgRules    []gate.Threshold
	moduleRules []gate.Threshold
	ex          *gate.Exemptions
}

// New returns a Checker judging with o: the configured thresholds split
// into those of package rows and of the module row (gate.ForRow), and the
// configured exemptions as of o.Now (gate.NewExemptions).
func New(o Options) *Checker {
	return &Checker{
		o:           o,
		pkgRules:    gate.ForRow(o.Config.Thresholds, gate.PackageRow),
		moduleRules: gate.ForRow(o.Config.Thresholds, gate.ModuleRow),
		ex:          gate.NewExemptions(o.Config.Exemptions, o.Now),
	}
}

// Package takes m, pkg's metrics extracted at head, fills
// changed_func_cognitive_max from the function-level diff against the
// baseline (changedFunctions), evaluates it against its baseline metrics,
// if the baseline has any, and the rules that apply to a package row, the
// changed functions given to the gate so changed_func_cognitive_max judges
// each against its own baseline complexity (gate.WithChangedFunctions),
// and builds its report, its findings located where the extractor's
// details say (locateFindings) and a changed_func_cognitive_max finding
// named and located on the function it judged (nameJudged). The duplicate
// blocks and untested exports the change added lead the details, the
// suggestions and the locations (rankNew); when head's coverage run
// counted untested_exports_covered, the exports it executed follow the
// others (coveredLast), and the rules judge it against the baseline's
// count estimated from the same run (coveredBase). The result
// carries the changed functions (report.CheckedPackage.ChangedFunctions).
// The exemptions are applied to the result
// (gate.Exemptions.Apply) before the report is built. unrecorded reports
// that the diff was skipped because the baseline has pkg but no functions
// for it.
func (c *Checker) Package(ctx context.Context, pkg string, m metrics.RawMetrics) (p report.CheckedPackage, unrecorded bool, err error) {
	o := &c.o
	det, names, err := Details(ctx, o.Ext, o.Mod, pkg)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	path := Rel(o.Mod.ModulePath, pkg)
	var bm *metrics.RawMetrics
	row, inBase := o.Base.Row(path)
	if inBase {
		bm = &row.Metrics
	}
	lead := rankNew(&det, &names, &row, inBase)
	if m.UntestedExportsCovered != nil {
		coveredLast(&det, &names, &lead, row.Named.UntestedExports, inBase)
	}
	changed, listed, unrecorded, err := c.changedFunctions(ctx, pkg, row, inBase)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	var worst *changedFunction
	var opts []gate.Option
	if listed {
		worst = worstChanged(changed)
		m.ChangedFuncCognitiveMax = &worst.cognitive
		names.ChangedFunction = worst.name
		opts = append(opts, gate.WithChangedFunctions(changed))
	}
	suggest := func(metric string, h float64, hm metrics.RawMetrics) string {
		return score.MetricSuggestion(metric, h, hm, names)
	}
	r, res := c.row(path, m, m, bm, coveredBase(m, bm, &row, det.UntestedCovered), c.pkgRules, suggest, names, opts...)
	r.BaselineFrom = row.From
	locateFindings(&r, &det, nameJudged(&r, &res, worst, m, names), lead)
	r.Details = c.details(&det)
	p = report.CheckedPackage{Report: r, ChangedFunctions: changed}
	if bm != nil {
		// Baselines never measure coverage. When head did, the base is
		// estimated with head's coverage so that the two agent_passes
		// differ by the change, not by the opt-in.
		be := *bm
		if be.CoveragePct == nil && m.CoveragePct != nil {
			be.CoveragePct = m.CoveragePct
		}
		passes := score.Estimate(be, o.Config.Rebuild).AgentPassesRounded()
		p.BaseAgentPasses = &passes
	}
	return p, unrecorded, nil
}

// Module builds the module-level row with mm, evaluates it against the
// baseline's row under metrics.ModuleRowID, if the baseline has one, and
// the rules that apply to the module row, as Package does for a package,
// and builds its report under package path metrics.ModuleRowID. When the
// extractor implements metrics.ModuleDetailer, the dup_blocks_cross_pkg
// suggestion names where the first shared block lives and its findings
// are located on the block's first occurrence; otherwise the row's
// suggestions name no locations. When the baseline recorded its blocks,
// the blocks the change added come first (score.RankCrossBlocks); the
// row's details list them in that order. It carries no baseline agent
// passes, since its rebuild estimate is of an empty package.
//
// When the baseline recorded its blocks and has the row, the rules judge
// dup_blocks_cross_pkg as the baseline's value plus the blocks the change
// added (judgeCross): new ones of at least duplication.min_tokens_cross
// tokens that a rename did not move (score.AddedCrossBlocks), so a short
// repeated preamble or the code a split left shared between its halves is
// not a rise. The finding names and is located on the added blocks only,
// while the row reports the full count. Against a baseline file, which
// records no blocks, the full count is judged and the suggestion says so.
//
// named are the packages a check of named packages checks; empty for any
// other check. When it is set and the extractor implements
// metrics.ModuleDetailer, the rules judge dup_blocks_cross_pkg on the
// added blocks blamed on those packages only (blameNamed), so a copy between two
// other packages neither fails the check nor appears in its findings; the
// row still reports the full count, and the suggestion names the packages
// sharing each blamed block.
//
// fromFile says the baseline was read from a baseline file. A file written
// before the module row existed has none, and the blocks it would have
// counted are not new: the rules are skipped for this run with one info
// log, and the row's metrics are still reported. A baseline extracted from
// a commit always has the row.
//
// The exemptions are applied to the row's result. judged reports that the
// module-wide rules were evaluated: false when there are none or they were
// skipped for a baseline file without the row.
func (c *Checker) Module(ctx context.Context, mm metrics.ModuleMetrics, fromFile bool, named []string,
) (p report.CheckedPackage, judged bool, err error) {
	o := &c.o
	m, err := mm.ModuleRow(ctx, o.Mod)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	rules := c.moduleRules
	var bm *metrics.RawMetrics
	if row, ok := o.Base.Row(metrics.ModuleRowID); ok {
		bm = &row.Metrics
	} else if fromFile && len(rules) > 0 {
		o.Logger.Info("baseline file has no module row; module-wide rules skipped; run `astimate baseline write` to add it")
		rules = nil
	}
	det, err := moduleDetails(ctx, o.Ext, o.Mod)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	baseBlocks, known := o.Base.CrossBlocks()
	// The baseline names packages by directory; head by identifier.
	baseBlocks = metrics.MapPackages(baseBlocks, func(dir string) string { return ImportPath(o.Mod.ModulePath, dir) })
	names := score.Names{CrossBlocks: det.CrossBlocks}
	if known {
		var added int
		names.CrossBlocks, added = score.RankCrossBlocks(det.CrossBlocks, baseBlocks)
		names.CrossAdded = added > 0
	}
	cj := c.judgeCross(det.CrossBlocks, baseBlocks, known, det.Roles)
	gm, blame, cited := cj.gated(m, bm), (*crossBlame)(nil), cj.added
	if _, detailed := o.Ext.(metrics.ModuleDetailer); detailed && len(named) > 0 && len(rules) > 0 && m.DupBlocksCrossPkg != nil {
		gm, blame = blameNamed(m, bm, cj, det.CrossBlocks, named)
		cited = blame.blocks
	}
	// The details list the cross-package blocks in the order the suggestion
	// cites them.
	det.CrossBlocks = names.CrossBlocks
	suggest := func(metric string, h float64, hm metrics.RawMetrics) string {
		if metric != "dup_blocks_cross_pkg" {
			return score.MetricSuggestion(metric, h, hm, names)
		}
		return cj.suggestion(blame, hm, o.Mod.ModulePath, score.MetricSuggestion(metric, h, hm, names))
	}
	r, _ := c.row(metrics.ModuleRowID, m, gm, bm, bm, rules, suggest, score.Names{})
	located := names.CrossBlocks
	if len(cited) > 0 {
		located = cited
	}
	locateCross(&r, located)
	r.Details = c.details(&det)
	return report.CheckedPackage{Report: r}, len(rules) > 0, nil
}

// row evaluates gm, the metrics the rules judge, against gb, the baseline
// metrics they judge it against (nil when the row has none), and rules,
// with opts (gate.Evaluate), applies the exemptions to the result under
// package path path, logs each rule skipped, and returns the report of the
// row with metrics m, baseline metrics bm and names, with the verdict and
// the baseline's tokenizer applied, and the result it was built from.
func (c *Checker) row(path string, m, gm metrics.RawMetrics, bm, gb *metrics.RawMetrics, rules []gate.Threshold,
	suggest gate.Suggester, names score.Names, opts ...gate.Option,
) (report.Report, gate.Result) {
	o := &c.o
	res := gate.Evaluate(gm, gb, rules, suggest, opts...)
	c.ex.Apply(path, &res)
	for _, n := range res.Notes {
		o.Logger.Info("rule skipped", "path", path, "metric", n.Metric, "reason", n.Text)
	}
	r := report.Build(&report.Input{
		Language:        o.Ext.Language(),
		PackagePath:     path,
		ModulePath:      o.Mod.ModulePath,
		Metrics:         m,
		Names:           names,
		Params:          o.Config.Rebuild,
		ConfigVersion:   o.Config.Version,
		AstimateVersion: o.Version,
	})
	report.ApplyGate(&r, o.Base.Ref(), bm, &res)
	report.MarkTokenizer(&r, o.Base.Tokenizer(), o.Tokenizer)
	return r, res
}

// details returns d as the report's details block, with package
// identifiers made module-relative.
func (c *Checker) details(d *metrics.Details) *report.Details {
	return report.NewDetails(d, func(pkg string) string { return Rel(c.o.Mod.ModulePath, pkg) })
}

// changedFunctions diffs pkg's functions at head against those of row, its
// baseline row, and returns the changed ones, each with its baseline
// complexity (metrics.ChangedFunctionsWithBase), with listed set. inBase
// reports whether the baseline has a row for pkg; a package new at head
// diffs against no functions, so all of its functions are changed. listed
// is false when there is nothing to diff: the extractor does not
// implement metrics.FunctionLister, or the row recorded no functions,
// which unrecorded reports so the caller can say why the metric is null.
func (c *Checker) changedFunctions(ctx context.Context, pkg string, row baseline.Row, inBase bool,
) (changed []metrics.ChangedFunction, listed, unrecorded bool, err error) {
	fl, ok := c.o.Ext.(metrics.FunctionLister)
	if !ok {
		return nil, false, false, nil
	}
	var before []metrics.FunctionInfo
	if inBase {
		if !row.HasFunctions {
			return nil, false, true, nil
		}
		before = row.Functions
	}
	head, err := fl.Functions(ctx, c.o.Mod, pkg)
	if err != nil {
		return nil, false, false, err
	}
	return metrics.ChangedFunctionsWithBase(before, head), true, false, nil
}

// Details returns the details of pkg when ext implements metrics.Detailer,
// and the zero metrics.Details otherwise, with the names in them that
// suggestions quote; zero names yield suggestions with counts only. Call
// it after Extract for pkg on mod.
func Details(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext, pkg string) (metrics.Details, score.Names, error) {
	d, ok := ext.(metrics.Detailer)
	if !ok {
		return metrics.Details{}, score.Names{}, nil
	}
	det, err := d.Details(ctx, mod, pkg)
	if err != nil {
		return metrics.Details{}, score.Names{}, fmt.Errorf("naming suggestions for %s: %w", pkg, err)
	}
	names := score.Names{UntestedExports: det.UntestedExports, DupLocations: det.DupLocations,
		CrossBlocks: det.CrossBlocks, Globals: det.GlobalNames}
	return det, names, nil
}

// moduleDetails returns the details behind the module row's counts, for
// suggestions and the report's details block, when ext implements
// metrics.ModuleDetailer, and the zero metrics.Details otherwise.
func moduleDetails(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext) (metrics.Details, error) {
	d, ok := ext.(metrics.ModuleDetailer)
	if !ok {
		return metrics.Details{}, nil
	}
	det, err := d.ModuleDetails(ctx, mod)
	if err != nil {
		return metrics.Details{}, fmt.Errorf("naming suggestions for %s: %w", metrics.ModuleRowID, err)
	}
	return det, nil
}
