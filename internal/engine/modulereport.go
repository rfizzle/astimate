package engine

import (
	"context"
	"io"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/engine/internal/narration"
	"github.com/rfizzle/astimate/internal/narrate"
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

// Report builds the module report of t's module (narration.Build): every
// package extracted once (as Rank does), with its churn from one history
// walk and its report and details (as Assess builds them), the packages
// left out (judge.SkippedIn) and, with opts.Base, the check of the changed
// packages against it (Check). A package that fails to extract or name its
// details is logged, left out and returned as a *PackageError in failed,
// as are Check's failures.
func Report(ctx context.Context, t *Target, opts ReportOptions) (m *narrate.Module, failed []error, err error) {
	pkgs, err := t.Ext.Packages(t.Mod.Root)
	if err != nil {
		return nil, nil, err
	}
	skips, err := judge.SkippedIn(t.Ext, t.Mod.Root)
	if err != nil {
		return nil, nil, err
	}
	extracted, order, failed := t.extractAll(ctx, pkgs)
	fillChurn(ctx, t, extracted)
	in := narration.Parts{Base: opts.Base, Skipped: skips.Report(t.Mod.ModulePath, nil), Packages: make([]report.Report, 0, len(order))}
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
	return narration.Build(t.reportModule(), &in), failed, nil
}

// CheckReport narrates c, a check of t's module, as the change report
// check --format text prints, from the check's rows alone
// (narration.CheckReport).
func CheckReport(t *Target, c *report.Check, base string) *narrate.Module {
	return narration.CheckReport(t.reportModule(), c, base)
}

// Sentences returns the story and the todo of r, the report of a package
// of t's module, as the module report words them (narration.Sentences).
func Sentences(t *Target, r *report.Report) (story, todo string) {
	return narration.Sentences(t.reportModule(), r)
}

// Glossary returns the glossary a module report of language carries with
// the settings of cfg, nil for the embedded default (narration.Glossary).
func Glossary(cfg *config.Config, language string) []narrate.Term {
	return narration.Glossary(cfg, language)
}

// ChangeOptions shape WriteChange (narration.ChangeOptions).
type ChangeOptions = narration.ChangeOptions

// WriteChange renders the check c, already run on t's module, as the
// change block of the module report, in Markdown or as GitHub
// annotations with an optional step summary (narration.WriteChange).
func WriteChange(t *Target, c *report.Check, opts ChangeOptions, w io.Writer) error {
	return narration.WriteChange(t.reportModule(), c, opts, w)
}

// reportModule returns t's module as its reports narrate it.
func (t *Target) reportModule() *narration.Module {
	return &narration.Module{Path: t.Mod.ModulePath, Root: t.Mod.Root, Language: t.Ext.Language(), Cfg: t.Cfg, Version: t.Version}
}
