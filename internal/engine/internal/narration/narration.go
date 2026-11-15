// Package narration builds the module report and the change report
// (SPEC.md 10.3) from rows engine has already extracted, checked and
// estimated: it fills compose.Input with the module's facts and the
// limits, churn window, duplicate-block floor and tiers of its effective
// configuration, narrates it with compose.Build, and writes a check's
// change block as Markdown or GitHub annotations (WriteChange). It
// extracts nothing and runs no check; engine does both and hands the rows
// here.
package narration

import (
	"path/filepath"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/narrate"
	"github.com/rfizzle/astimate/internal/narrate/compose"
	"github.com/rfizzle/astimate/internal/report"
)

// Module is the module a report narrates and the configuration it is
// narrated with.
type Module struct {
	// Path is the module's path; empty for a module without one.
	Path string
	// Root is the module root directory, whose base name names a module
	// without a path.
	Root string
	// Language is the extractor's language, which selects the effective
	// configuration (config.Config.ForLanguage).
	Language string
	// Cfg is the resolved configuration.
	Cfg *config.Config
	// Version is the astimate version recorded on the report.
	Version string
}

// Parts are the rows a module report narrates.
type Parts struct {
	// Base is the git ref the change section is checked against; empty
	// without one.
	Base string
	// Head is the commit at HEAD; empty outside git or before the first
	// commit.
	Head string
	// Packages are every package's report, its details included.
	Packages []report.Report
	// Skipped are the module's packages that were not analysed.
	Skipped []report.Skipped
	// Check is the check of the changed packages against Base; nil
	// without one.
	Check *report.Check
}

// Build narrates p, the rows of m, as the module report (compose.Build).
func Build(m *Module, p *Parts) *narrate.Module {
	in := m.input(p.Base, p.Packages)
	in.Head, in.Skipped, in.Check = p.Head, p.Skipped, p.Check
	built := compose.Build(&in)
	return &built
}

// CheckReport narrates c, a check of m, as the change report check
// --format text prints (compose.Build): the checked packages' views,
// without the rest of the module's, and the change against base, the ref
// or file the check compared with (empty for the default ref). It reads
// the check's rows only, so unlike a module report given the check it
// extracts nothing again; a package whose estimate is uncalibrated has no
// view (SPEC.md 8.5), so its summary counts findings only.
func CheckReport(m *Module, c *report.Check, base string) *narrate.Module {
	pkgs := make([]report.Report, 0, len(c.Packages))
	for i := range c.Packages {
		if c.Packages[i].Report.Rebuild.Calibrated {
			pkgs = append(pkgs, c.Packages[i].Report)
		}
	}
	in := m.input(base, pkgs)
	in.Check = c
	built := compose.Build(&in)
	return &built
}

// Sentences returns the story (what is wrong with the package) and the
// todo (what to do about it) of r, the report of a package of m, as the
// module report words them (compose.Build). Both are empty for a package
// whose estimate is uncalibrated, which has no view.
func Sentences(m *Module, r *report.Report) (story, todo string) {
	if !r.Rebuild.Calibrated {
		return "", ""
	}
	in := m.input("", []report.Report{*r})
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

// input returns the input of compose.Build for m, with the packages pkgs
// and the change against base.
func (m *Module) input(base string, pkgs []report.Report) compose.Input {
	eff := m.Cfg.ForLanguage(m.Language)
	return compose.Input{
		ModulePath: m.Path, RootName: filepath.Base(m.Root), Language: m.Language,
		Base: base, Limits: ceilings(&eff), ChurnWindow: m.Cfg.Churn.Commits,
		MinTokens: m.Cfg.Duplication.MinTokens, Packages: pkgs, Basis: report.NewHumanBasis(eff.Rebuild),
		OnePassMax: eff.Rebuild.Tiers.OnePassMax, FewPassesMax: eff.Rebuild.Tiers.FewPassesMax,
		AstimateVersion: m.Version, ConfigVersion: eff.Version,
	}
}

// ceilings returns eff's rules with a ceiling (gate.Threshold.Ceiling)
// on the five size metrics, by metric in the order of SPEC.md 10.3.
func ceilings(eff *config.Effective) []narrate.Limit {
	out := make([]narrate.Limit, 0, 5)
	for _, metric := range []string{"sloc", "tokens_est", "largest_file_sloc", "exported_symbols", "internal_imports"} {
		for _, th := range eff.Thresholds {
			if limit, ok := th.Ceiling(); ok && th.Metric == metric {
				out = append(out, narrate.Limit{Metric: metric, Max: limit, Roles: th.Roles})
			}
		}
	}
	return out
}
