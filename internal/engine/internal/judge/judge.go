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
// if the baseline has any, and the rules that apply to a package row, and
// builds its report, its findings located where the extractor's details
// say (locateFindings). The exemptions are applied to the result
// (gate.Exemptions.Apply) before the report is built. unrecorded reports
// that the diff was skipped because the baseline has pkg but no functions
// for it.
func (c *Checker) Package(ctx context.Context, pkg string, m metrics.RawMetrics) (p report.CheckedPackage, unrecorded bool, err error) {
	o := &c.o
	det, names, err := Details(ctx, o.Ext, o.Mod, pkg)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	var bm *metrics.RawMetrics
	if v, ok := o.Base.Metrics(pkg); ok {
		bm = &v
	}
	worst, unrecorded, err := c.changedFunctions(ctx, pkg, bm != nil)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	if worst != nil {
		m.ChangedFuncCognitiveMax = &worst.cognitive
		names.ChangedFunction = worst.name
	}
	suggest := func(metric string, h float64, hm metrics.RawMetrics) string {
		return score.MetricSuggestion(metric, h, hm, names)
	}
	r := c.row(Rel(o.Mod.ModulePath, pkg), m, m, bm, c.pkgRules, suggest, names)
	locateFindings(&r, &det, worst)
	r.Details = c.details(&det)
	p = report.CheckedPackage{Report: r}
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
// suggestions name no locations. It carries no baseline agent passes,
// since its rebuild estimate is of an empty package.
//
// named are the packages a check of named packages checks; empty for any
// other check. When it is set and the extractor implements
// metrics.ModuleDetailer, the rules judge dup_blocks_cross_pkg on the
// blocks blamed on those packages only (blameNamed), so a copy between two
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
	if v, ok := o.Base.Metrics(metrics.ModuleRowID); ok {
		bm = &v
	} else if fromFile && len(rules) > 0 {
		o.Logger.Info("baseline file has no module row; module-wide rules skipped; run `astimate baseline write` to add it")
		rules = nil
	}
	det, err := moduleDetails(ctx, o.Ext, o.Mod)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	names := score.Names{CrossBlocks: det.CrossBlocks}
	gm, blame := m, (*crossBlame)(nil)
	if _, detailed := o.Ext.(metrics.ModuleDetailer); detailed && len(named) > 0 && len(rules) > 0 && m.DupBlocksCrossPkg != nil {
		baseBlocks, known := o.Base.CrossBlocks()
		gm, blame = blameNamed(m, bm, baseBlocks, known, names.CrossBlocks, named)
	}
	suggest := func(metric string, h float64, hm metrics.RawMetrics) string {
		if blame != nil && metric == "dup_blocks_cross_pkg" {
			if s := blame.suggestion(hm, o.Mod.ModulePath); s != "" {
				return s
			}
		}
		return score.MetricSuggestion(metric, h, hm, names)
	}
	r := c.row(metrics.ModuleRowID, m, gm, bm, rules, suggest, score.Names{})
	located := names.CrossBlocks
	if blame != nil && len(blame.blocks) > 0 {
		located = blame.blocks
	}
	locateCross(&r, located)
	r.Details = c.details(&det)
	return report.CheckedPackage{Report: r}, len(rules) > 0, nil
}

// row evaluates gm, the metrics the rules judge, against bm, the row's
// baseline metrics (nil when it has none), and rules, applies the
// exemptions to the result under package path path, logs each rule
// skipped, and returns the report of the row with metrics m and names,
// with the verdict and the baseline's tokenizer applied.
func (c *Checker) row(path string, m, gm metrics.RawMetrics, bm *metrics.RawMetrics, rules []gate.Threshold,
	suggest gate.Suggester, names score.Names,
) report.Report {
	o := &c.o
	res := gate.Evaluate(gm, bm, rules, suggest)
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
	return r
}

// details returns d as the report's details block, with package
// identifiers made module-relative.
func (c *Checker) details(d *metrics.Details) *report.Details {
	return report.NewDetails(d, func(pkg string) string { return Rel(c.o.Mod.ModulePath, pkg) })
}

// changedFunctions diffs pkg's functions at head against the baseline's
// (worstChanged) and returns the most complex changed one, or a zero
// changedFunction when none changed. inBase reports whether the baseline
// has pkg; a package new at head diffs against no functions, so all of its
// functions are changed. It returns nil when there is nothing to diff: the
// extractor does not implement metrics.FunctionLister, or the baseline has
// pkg but recorded no functions for it, which unrecorded reports so the
// caller can say why the metric is null.
func (c *Checker) changedFunctions(ctx context.Context, pkg string, inBase bool) (worst *changedFunction, unrecorded bool, err error) {
	fl, ok := c.o.Ext.(metrics.FunctionLister)
	if !ok {
		return nil, false, nil
	}
	var before []metrics.FunctionInfo
	if inBase {
		if before, ok = c.o.Base.Functions(pkg); !ok {
			return nil, true, nil
		}
	}
	head, err := fl.Functions(ctx, c.o.Mod, pkg)
	if err != nil {
		return nil, false, err
	}
	return worstChanged(before, head), false, nil
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
