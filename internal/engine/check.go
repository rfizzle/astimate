package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// ErrBaseAndBaselineFile reports CheckOptions naming both a git ref and a
// baseline file.
var ErrBaseAndBaselineFile = errors.New("a base ref and a baseline file are mutually exclusive")

// CheckOptions select the baseline and the packages Check gates.
type CheckOptions struct {
	// Base is the git ref whose merge-base with HEAD is the baseline; empty
	// means the default ref (SPEC.md 8.3).
	Base string
	// BaselineFile is a baseline file to compare against instead of a git
	// ref; empty means a git baseline.
	BaselineFile string
	// All checks every package instead of the changed ones.
	All bool
}

// Check runs the gate on t's module against the baseline opts select and
// the configured thresholds. The head tree is listed with one Packages call
// and the baseline tree, when it comes from git, with one more inside
// baseline.FromGit, so each tree is loaded once. It returns an error when
// no verdict can be reached: listing fails or the baseline or the changed
// packages cannot be resolved. A package that fails to extract is logged,
// left out of the result and returned as a *PackageError in failed.
func Check(ctx context.Context, t *Target, opts CheckOptions) (c *report.Check, failed []error, err error) {
	if opts.Base != "" && opts.BaselineFile != "" {
		return nil, nil, ErrBaseAndBaselineFile
	}
	head, err := t.Ext.Packages(t.Mod.Root)
	if err != nil {
		return nil, nil, err
	}
	logger := t.logger()
	src, err := resolveBaselineSource(ctx, t, opts)
	if err != nil {
		return nil, nil, err
	}
	selected, deleted, err := selectPackages(ctx, t, head, src, opts.All)
	if err != nil {
		return nil, nil, err
	}
	base := src.file
	if base == nil && len(selected) > 0 {
		// Nothing selected means nothing to compare; skip the second load.
		base, err = baseline.FromGit(ctx, t.Mod.Root, src.ref, t.Ext, t.Mod.ModulePath)
		if err != nil {
			return nil, nil, err
		}
	}

	c = &report.Check{Packages: make([]report.CheckedPackage, 0, len(selected)), Deleted: deleted}
	for _, pkg := range selected {
		p, err := checkPackage(ctx, t, base, pkg)
		if err != nil {
			path := modulePathRel(t.Mod.ModulePath, pkg)
			logger.Error("checking package failed", "path", path, "err", err)
			failed = append(failed, &PackageError{Path: path, Err: err})
			continue
		}
		c.Packages = append(c.Packages, p)
	}
	return c, failed, nil
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

// resolveBaselineSource picks the baseline per SPEC.md 8.3: BaselineFile
// reads the file, Base names the ref, and with neither the first default
// ref is used. When no default ref exists but the module has a baseline
// file at baseline.DefaultPath, that file is used and a warning says so.
func resolveBaselineSource(ctx context.Context, t *Target, opts CheckOptions) (baselineSource, error) {
	if opts.BaselineFile != "" {
		b, err := baseline.FromFile(opts.BaselineFile)
		if err != nil {
			return baselineSource{}, err
		}
		return baselineSource{file: b}, nil
	}
	if opts.Base != "" {
		return baselineSource{ref: opts.Base}, nil
	}
	root := t.Mod.Root
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
	t.logger().Warn("no default baseline ref; using the baseline file", "path", path, "err", refErr)
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
func selectPackages(ctx context.Context, t *Target, head []string, src baselineSource,
	all bool,
) (selected, deleted []string, err error) {
	if all {
		return head, nil, nil
	}
	logger := t.logger()
	ref := src.ref
	if src.file != nil {
		ref = src.file.Ref()
	}
	if ref == "" {
		logger.Warn("baseline file records no git ref; checking every package")
		return head, nil, nil
	}
	mergeBase, err := baseline.MergeBase(ctx, t.Mod.Root, ref)
	if err != nil {
		if src.file == nil {
			return nil, nil, err
		}
		logger.Warn("cannot resolve the baseline file's ref; checking every package", "ref", ref, "err", err)
		return head, nil, nil
	}
	change, err := baseline.ChangedPackages(ctx, t.Mod.Root, mergeBase)
	if err != nil {
		return nil, nil, err
	}
	changed := make(map[string]bool, len(change.Packages))
	for _, dir := range change.Packages {
		changed[importPathOf(t.Mod.ModulePath, dir)] = true
	}
	for _, pkg := range head {
		if changed[pkg] {
			selected = append(selected, pkg)
		}
	}
	return selected, change.Deleted, nil
}

// checkPackage extracts pkg at head, evaluates it against its baseline
// metrics, if base has any, and the configured thresholds, and builds its
// report.
func checkPackage(ctx context.Context, t *Target, base baseline.Baseline, pkg string) (report.CheckedPackage, error) {
	m, err := t.Ext.Extract(ctx, t.Mod, pkg)
	if err != nil {
		return report.CheckedPackage{}, err
	}
	names, err := Names(ctx, t, pkg)
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
	res := gate.Evaluate(m, bm, t.Cfg.Thresholds, suggest)
	path := modulePathRel(t.Mod.ModulePath, pkg)
	logger := t.logger()
	for _, n := range res.Notes {
		logger.Info("rule skipped", "path", path, "metric", n.Metric, "reason", n.Text)
	}

	r := report.Build(&report.Input{
		Language:        t.Ext.Language(),
		PackagePath:     path,
		ModulePath:      t.Mod.ModulePath,
		Metrics:         m,
		Names:           names,
		Params:          t.Cfg.Rebuild,
		ConfigVersion:   t.Cfg.Version,
		AstimateVersion: t.Version,
	})
	report.ApplyGate(&r, base.Ref(), bm, &res)
	p := report.CheckedPackage{Report: r}
	if bm != nil {
		passes := score.Estimate(*bm, t.Cfg.Rebuild).AgentPassesRounded()
		p.BaseAgentPasses = &passes
	}
	return p, nil
}
