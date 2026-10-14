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
	eff := t.langConfig()
	in := compose.Input{
		ModulePath: t.Mod.ModulePath, RootName: filepath.Base(t.Mod.Root), Language: t.Ext.Language(),
		Base: opts.Base, Limits: ceilings(&eff), ChurnWindow: t.Cfg.Churn.Commits,
		MinTokens: t.Cfg.Duplication.MinTokens, Packages: make([]report.Report, 0, len(order)),
		AstimateVersion: t.Version, ConfigVersion: eff.Version,
	}
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
