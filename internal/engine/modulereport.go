package engine

import (
	"context"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/compose"
	"github.com/rfizzle/astimate/internal/report"
)

// ReportOptions shape Report's module report.
type ReportOptions struct {
	// Base is the git ref the change section is checked against, as
	// CheckOptions.Base; empty leaves the section out.
	Base string
	// Check, when set, is the check of the change the caller
	// already ran; Report narrates it instead of running Check again.
	Check *report.Check
}

// Report builds the module report of t's module (compose.Build): every
// package extracted once (as Rank does), with its churn from one history
// walk and its report and details (as Assess builds them), and, with
// opts.Base, the check of the changed packages against it (Check). A
// package that fails to extract or name its details is logged, left out
// and returned as a *PackageError in failed, as are Check's failures.
func Report(ctx context.Context, t *Target, opts ReportOptions) (m *narrate.Module, failed []error, err error) {
	pkgs, err := t.Ext.Packages(t.Mod.Root)
	if err != nil {
		return nil, nil, err
	}
	extracted, order, failed := t.extractAll(ctx, pkgs)
	fillChurn(ctx, t, extracted)
	in := t.composeInput(opts.Base, len(order))
	for _, pkg := range order {
		dir := judge.Rel(t.Mod.ModulePath, pkg)
		r, err := t.build(ctx, pkg, dir, extracted[pkg])
		if err != nil {
			t.logger().Error("reporting package failed", "path", dir, "err", err)
			failed = append(failed, &PackageError{Path: dir, Err: err})
			continue
		}
		in.Packages = append(in.Packages, *r)
	}
	if opts.Check != nil {
		in.Check = opts.Check
	} else if opts.Base != "" {
		c, cf, err := Check(ctx, t, CheckOptions{Base: opts.Base})
		if err != nil {
			return nil, failed, err
		}
		in.Check, failed = c, append(failed, cf...)
	}
	// Outside git, or before the first commit, the report names no head.
	in.Head, _ = baseline.HeadCommit(ctx, t.Mod.Root)
	built := compose.Build(&in)
	return &built, failed, nil
}

// CheckReport narrates c, a check of t's module, as the change report
// check --format text prints (compose.Build): the checked packages'
// views, without the rest of the module's, and the change against base,
// the ref or file the check compared with (empty for the default ref).
// It reads the check's rows only, so unlike Report with
// ReportOptions.Check it extracts nothing again; a package whose estimate
// is uncalibrated has no view, so its summary names no rebuild days.
func CheckReport(t *Target, c *report.Check, base string) *narrate.Module {
	in := t.composeInput(base, len(c.Packages))
	for i := range c.Packages {
		// Gate output leaves an uncalibrated estimate out (SPEC.md 8.5):
		// without its view a failing package's summary counts findings
		// only.
		if c.Packages[i].Report.Rebuild.Calibrated {
			in.Packages = append(in.Packages, c.Packages[i].Report)
		}
	}
	in.Check = c
	built := compose.Build(&in)
	return &built
}

// Sentences returns the story (what is wrong with the package) and the
// todo (what to do about it) of r, the report of a package of t's module,
// as the module report words them (compose.Build). Both are empty for a
// package whose estimate is uncalibrated, which has no view.
func Sentences(t *Target, r *report.Report) (story, todo string) {
	if !r.Rebuild.Calibrated {
		return "", ""
	}
	in := t.composeInput("", 1)
	in.Packages = append(in.Packages, *r)
	built := compose.Build(&in)
	if len(built.Packages) == 0 {
		return "", ""
	}
	return built.Packages[0].Story, built.Packages[0].Todo
}

// Glossary returns the glossary a module report of language carries
// (narrate.Glossary), with the limits, duplicate-block floor and churn
// window of cfg; a nil cfg is the embedded default configuration. It is
// what the report page prints, so explain_metric can answer with the same
// words.
func Glossary(cfg *config.Config, language string) []narrate.Term {
	if cfg == nil {
		var err error
		if cfg, err = config.Parse(config.Default()); err != nil {
			return narrate.Glossary(language, nil, 0, 0, report.HumanBasis{})
		}
	}
	eff := cfg.ForLanguage(language)
	return narrate.Glossary(language, ceilings(&eff), cfg.Duplication.MinTokens, cfg.Churn.Commits, report.NewHumanBasis(eff.Rebuild))
}

// composeInput returns the input of compose.Build for t's module, with
// room for n packages and the change against base.
func (t *Target) composeInput(base string, n int) compose.Input {
	eff := t.langConfig()
	return compose.Input{
		ModulePath: t.Mod.ModulePath, RootName: filepath.Base(t.Mod.Root), Language: t.Ext.Language(),
		Base: base, Limits: ceilings(&eff), ChurnWindow: t.Cfg.Churn.Commits,
		MinTokens: t.Cfg.Duplication.MinTokens, Packages: make([]report.Report, 0, n), Basis: report.NewHumanBasis(eff.Rebuild),
		AstimateVersion: t.Version, ConfigVersion: eff.Version,
	}
}

// ceilings returns the ceilings of eff's rules on the five size metrics,
// the first rule with a max on each, in the order of SPEC.md 10.3.
func ceilings(eff *config.Effective) []narrate.Limit {
	out := make([]narrate.Limit, 0, 5)
	for _, metric := range []string{"sloc", "tokens_est", "largest_file_sloc", "exported_symbols", "internal_imports"} {
		for _, th := range eff.Thresholds {
			if th.Metric == metric && th.Max != nil {
				out = append(out, narrate.Limit{Metric: metric, Max: *th.Max, Roles: th.Roles})
				break
			}
		}
	}
	return out
}
