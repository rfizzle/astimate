package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// Output formats accepted by check --format (SPEC.md 8.5).
const (
	formatText   = "text"
	formatJSON   = "json"
	formatHook   = "hook"
	formatGitHub = "github"
)

// checkFormats returns the formats check --format accepts; formatText is
// the default.
func checkFormats() []string {
	return []string{formatText, formatJSON, formatHook, formatGitHub}
}

// checkOptions are the check flags that select the baseline, the packages
// and the output.
type checkOptions struct {
	// base is the --base ref; empty means the default ref.
	base string
	// baselineFile is the --baseline file; empty means a git baseline.
	baselineFile string
	// all checks every package instead of the changed ones.
	all bool
	// format is the --format value, one of checkFormats.
	format string
}

// runCheck gates the packages of a module against a baseline and the
// configured thresholds: `check [<module-root>] [--base ref | --baseline
// file] [--all] [--config|--thresholds file] [--format
// text|json|hook|github] [--tokenizer est|o200k]`. It exits 3 when any
// package has a violation (0 with --format hook, whose JSON carries the
// decision), 2 when analysis failed, and 0 otherwise; warnings never change
// the exit code.
func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("astimate check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts checkOptions
	var configPath string
	fs.StringVar(&opts.base, "base", "", "compare against the merge-base of HEAD and this git ref "+
		"(default origin/master, then master, origin/main, main)")
	fs.StringVar(&opts.baselineFile, "baseline", "", "compare against this baseline file instead of a git ref")
	fs.BoolVar(&opts.all, "all", false, "check every package, not only the changed ones")
	fs.StringVar(&configPath, "config", "", "configuration file (default ./astimate.yaml, then the embedded default)")
	fs.StringVar(&configPath, "thresholds", "", "alias of --config")
	fs.StringVar(&opts.format, "format", formatText, "output format: "+strings.Join(checkFormats(), ", "))
	tokenizer := fs.String("tokenizer", tokenizerEst, "token counting method: est or o200k")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: astimate check [<module-root>] [--base ref | --baseline file] [--all] "+
			"[--config|--thresholds file] [--format text|json|hook|github] [--tokenizer est|o200k]")
		fs.PrintDefaults()
	}
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage // the flag package has printed the error and usage
	}
	if len(positional) > 1 {
		_, _ = fmt.Fprintf(stderr, "astimate: check: want at most one module root, got %d arguments\n", len(positional))
		fs.Usage()
		return exitUsage
	}
	dir := "."
	if len(positional) == 1 {
		dir = positional[0]
	}
	if opts.base != "" && opts.baselineFile != "" {
		_, _ = fmt.Fprintln(stderr, "astimate: check: --base and --baseline are mutually exclusive")
		return exitUsage
	}
	if !slices.Contains(checkFormats(), opts.format) {
		_, _ = fmt.Fprintf(stderr, "astimate: check: unknown format %q: want one of %s\n",
			opts.format, strings.Join(checkFormats(), ", "))
		return exitUsage
	}
	if !validTokenizer(*tokenizer) {
		_, _ = fmt.Fprintf(stderr, "astimate: check: unknown tokenizer %q: want %s or %s\n",
			*tokenizer, tokenizerEst, tokenizerO200k)
		return exitUsage
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	t, err := loadTarget(dir, targetFlags{configPath: configPath, tokenizer: *tokenizer})
	if err != nil {
		logger.Error("check failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	return checkModule(context.Background(), t.extractor, t.module, t.cfg, opts, stdout, stderr, logger)
}

// checkModule runs the gate on the module mod with ext and cfg and renders
// the result to stdout in opts.format. The head tree is listed with one
// Packages call and the baseline tree, when it comes from git, with one more
// inside baseline.FromGit, so each tree is loaded once. A package that fails
// to extract is logged and skipped; the rest are still rendered.
func checkModule(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext, cfg *config.Config,
	opts checkOptions, stdout, stderr io.Writer, logger *slog.Logger,
) int {
	head, err := ext.Packages(mod.Root)
	if err != nil {
		logger.Error("check failed", "root", mod.Root, "err", err)
		return exitAnalysis
	}
	src, err := resolveBaselineSource(ctx, mod.Root, opts, logger)
	if err != nil {
		logger.Error("check failed", "root", mod.Root, "err", err)
		return exitAnalysis
	}
	selected, deleted, err := selectPackages(ctx, mod, head, src, opts.all, logger)
	if err != nil {
		logger.Error("check failed", "root", mod.Root, "err", err)
		return exitAnalysis
	}
	base := src.file
	if base == nil && len(selected) > 0 {
		// Nothing selected means nothing to compare; skip the second load.
		base, err = baseline.FromGit(ctx, mod.Root, src.ref, ext, mod.ModulePath)
		if err != nil {
			logger.Error("check failed", "root", mod.Root, "err", err)
			return exitAnalysis
		}
	}

	c := report.Check{Packages: make([]report.CheckedPackage, 0, len(selected)), Deleted: deleted}
	failed := 0
	for _, pkg := range selected {
		p, err := checkPackage(ctx, ext, mod, cfg, base, pkg, logger)
		if err != nil {
			logger.Error("checking package failed", "path", modulePathRel(mod.ModulePath, pkg), "err", err)
			failed++
			continue
		}
		c.Packages = append(c.Packages, p)
	}

	if err := renderCheck(&c, opts.format, stdout, stderr); err != nil {
		logger.Error("check failed", "root", mod.Root, "err", err)
		return exitAnalysis
	}
	if failed > 0 {
		logger.Error("check incomplete", "failed_packages", failed)
	}
	return checkExitCode(opts.format, c.Failed(), failed)
}

// checkExitCode maps a check outcome in format to the process exit code
// (SPEC.md 8.5 and 9): exitGateFailed when any package has a violation, even
// if others failed to extract, since a verdict was reached; exitAnalysis when
// a package failed to extract; exitOK otherwise. Warnings never change it.
// The hook format exits exitOK whenever its output carries a block decision,
// because Claude Code reads a hook's JSON only on exit 0; with no violation
// an analysis failure still exits exitAnalysis.
func checkExitCode(format string, violations bool, failedPackages int) int {
	switch {
	case violations && format == formatHook:
		return exitOK
	case violations:
		return exitGateFailed
	case failedPackages > 0:
		return exitAnalysis
	default:
		return exitOK
	}
}

// baselineSource is where the baseline comes from: a git ref or a file,
// never both.
type baselineSource struct {
	// ref is the git ref whose merge-base with HEAD is the baseline; empty
	// when file is set.
	ref string
	// file is the baseline read from a file; nil for a git baseline.
	file baseline.Baseline
}

// resolveBaselineSource picks the baseline per SPEC.md 8.3: --baseline
// reads the file, --base names the ref, and with neither the first default
// ref is used. When no default ref exists but the module has a baseline
// file at baseline.DefaultPath, that file is used and a warning says so.
func resolveBaselineSource(ctx context.Context, root string, opts checkOptions, logger *slog.Logger) (baselineSource, error) {
	if opts.baselineFile != "" {
		b, err := baseline.FromFile(opts.baselineFile)
		if err != nil {
			return baselineSource{}, err
		}
		return baselineSource{file: b}, nil
	}
	if opts.base != "" {
		return baselineSource{ref: opts.base}, nil
	}
	ref, refErr := baseline.DefaultRef(ctx, root)
	if refErr == nil {
		return baselineSource{ref: ref}, nil
	}
	path := filepath.Join(root, baseline.DefaultPath)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(refErr, baseline.ErrNoDefaultRef) {
			return baselineSource{}, refErr
		}
		return baselineSource{}, fmt.Errorf("%w; pass --base <ref> or --baseline <file>", refErr)
	}
	logger.Warn("no default baseline ref; using the baseline file", "path", path, "err", refErr)
	b, err := baseline.FromFile(path)
	if err != nil {
		return baselineSource{}, err
	}
	return baselineSource{file: b}, nil
}

// selectPackages returns the import paths to check, in the order of head
// (the import paths the extractor lists at head), and the module-relative
// directories deleted since the baseline. With all set it returns head.
// Otherwise it takes the packages changed since the merge-base of HEAD and
// the baseline's ref and keeps those in head, which drops the directories
// the go tool ignores. A file baseline whose ref does not resolve in git,
// for example outside a repository, selects every package and says so.
func selectPackages(ctx context.Context, mod *metrics.ModuleContext, head []string, src baselineSource,
	all bool, logger *slog.Logger,
) (selected, deleted []string, err error) {
	if all {
		return head, nil, nil
	}
	ref := src.ref
	if src.file != nil {
		ref = src.file.Ref()
	}
	if ref == "" {
		logger.Warn("baseline file records no git ref; checking every package")
		return head, nil, nil
	}
	mergeBase, err := baseline.MergeBase(ctx, mod.Root, ref)
	if err != nil {
		if src.file == nil {
			return nil, nil, err
		}
		logger.Warn("cannot resolve the baseline file's ref; checking every package", "ref", ref, "err", err)
		return head, nil, nil
	}
	change, err := baseline.ChangedPackages(ctx, mod.Root, mergeBase)
	if err != nil {
		return nil, nil, err
	}
	changed := make(map[string]bool, len(change.Packages))
	for _, dir := range change.Packages {
		changed[importPathOf(mod.ModulePath, dir)] = true
	}
	for _, pkg := range head {
		if changed[pkg] {
			selected = append(selected, pkg)
		}
	}
	return selected, change.Deleted, nil
}

// importPathOf returns the import path of the package in the module-relative
// slash directory dir of module modPath; the inverse of modulePathRel.
func importPathOf(modPath, dir string) string {
	if dir == "." {
		return modPath
	}
	return modPath + "/" + dir
}

// checkPackage extracts pkg at head, evaluates it against its baseline
// metrics, if base has any, and the configured thresholds, and builds its
// report.
func checkPackage(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext, cfg *config.Config,
	base baseline.Baseline, pkg string, logger *slog.Logger,
) (report.CheckedPackage, error) {
	m, err := ext.Extract(ctx, mod, pkg)
	if err != nil {
		return report.CheckedPackage{}, err
	}
	names, err := suggestionNames(ctx, ext, mod, pkg)
	if err != nil {
		return report.CheckedPackage{}, err
	}
	var bm *metrics.RawMetrics
	if v, ok := base.Metrics(pkg); ok {
		bm = &v
	}
	suggest := func(metric string, h float64, hm metrics.RawMetrics) string {
		return score.MetricSuggestion(metric, h, hm, names)
	}
	res := gate.Evaluate(m, bm, cfg.Thresholds, suggest)
	path := modulePathRel(mod.ModulePath, pkg)
	for _, n := range res.Notes {
		logger.Info("rule skipped", "path", path, "metric", n.Metric, "reason", n.Text)
	}

	r := report.Build(&report.Input{
		Language:        ext.Language(),
		PackagePath:     path,
		ModulePath:      mod.ModulePath,
		Metrics:         m,
		Names:           names,
		Params:          cfg.Rebuild,
		ConfigVersion:   cfg.Version,
		AstimateVersion: astimateVersion(),
	})
	report.ApplyGate(&r, base.Ref(), bm, &res)
	p := report.CheckedPackage{Report: r}
	if bm != nil {
		passes := score.Estimate(*bm, cfg.Rebuild).AgentPassesRounded()
		p.BaseAgentPasses = &passes
	}
	return p, nil
}

// renderCheck renders c in format to stdout, writing hook warnings to
// stderr. Output is buffered so a render error leaves stdout empty.
func renderCheck(c *report.Check, format string, stdout, stderr io.Writer) error {
	var buf, warnings bytes.Buffer
	var err error
	switch format {
	case formatJSON:
		err = report.WriteCheckJSON(&buf, c)
	case formatHook:
		err = report.WriteHook(&buf, &warnings, c)
	case formatGitHub:
		err = report.WriteGitHub(&buf, c)
	default:
		err = report.WriteCheckText(&buf, c)
	}
	if err != nil {
		return err
	}
	if _, err := stdout.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing check output: %w", err)
	}
	if _, err := stderr.Write(warnings.Bytes()); err != nil {
		return fmt.Errorf("writing check warnings: %w", err)
	}
	return nil
}
