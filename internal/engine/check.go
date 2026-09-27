package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// ErrBaseAndBaselineFile reports CheckOptions naming both a git ref and a
// baseline file.
var ErrBaseAndBaselineFile = errors.New("a base ref and a baseline file are mutually exclusive")

// ErrNoBaseline reports that Check found no baseline to compare against:
// no ref or file was given, no default ref resolved and the module has no
// baseline file. The error's text says what was tried but not how to
// supply a baseline; each caller adds that hint in the terms of its own
// inputs (flags for the CLI, tool arguments for the MCP server).
var ErrNoBaseline = errors.New("no baseline")

// noBaselineError carries the reason no baseline resolved and matches
// ErrNoBaseline, keeping the reason's text as its own.
type noBaselineError struct {
	err error
}

// Error returns the reason's text unchanged.
func (e *noBaselineError) Error() string { return e.err.Error() }

// Unwrap returns the reason and ErrNoBaseline.
func (e *noBaselineError) Unwrap() []error { return []error{e.err, ErrNoBaseline} }

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
	// Packages, when non-empty, are the import paths to check, in order,
	// overriding All and the changed-package selection; the result then
	// lists no deleted packages. The MCP server checks one package this
	// way.
	Packages []string
	// Baselines memoizes baselines across Check calls on one module; nil
	// resolves the baseline afresh on every call.
	Baselines *BaselineCache
}

// BaselineCache holds baselines already read or extracted, so repeated
// checks against the same merge-base commit or file skip that work. A git
// baseline is keyed by its merge-base commit, a file by its absolute path,
// size and modification time, so an edited file is read again. It is safe
// for concurrent use. Create one with NewBaselineCache.
type BaselineCache struct {
	mu    sync.Mutex
	byKey map[string]baseline.Baseline
	loads int
}

// NewBaselineCache returns an empty BaselineCache.
func NewBaselineCache() *BaselineCache {
	return &BaselineCache{byKey: make(map[string]baseline.Baseline)}
}

// Loads returns how many baselines the cache has read or extracted: its
// misses.
func (c *BaselineCache) Loads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loads
}

// get returns the baseline stored under key, loading and storing it with
// load on a miss; a nil c always loads. Concurrent misses on one key may
// each load; the last one stored wins, and they are equal.
func (c *BaselineCache) get(key string, load func() (baseline.Baseline, error)) (baseline.Baseline, error) {
	if c == nil {
		return load()
	}
	c.mu.Lock()
	b, ok := c.byKey[key]
	c.mu.Unlock()
	if ok {
		return b, nil
	}
	b, err := load()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.byKey[key] = b
	c.loads++
	c.mu.Unlock()
	return b, nil
}

// Check runs the gate on t's module against the baseline opts select and
// the configured thresholds. The head tree is listed with one Packages call
// and the baseline tree, when it comes from git, with one more inside
// baseline.FromGit, so each tree is loaded once; opts.Packages skips the
// listing and opts.Baselines may skip the baseline. It returns an error
// when no verdict can be reached: listing fails or the baseline or the
// changed packages cannot be resolved; an error matching ErrNoBaseline
// means no baseline was given or found. A package that fails to extract
// is logged, left out of the result and returned as a *PackageError in
// failed.
func Check(ctx context.Context, t *Target, opts CheckOptions) (c *report.Check, failed []error, err error) {
	if opts.Base != "" && opts.BaselineFile != "" {
		return nil, nil, ErrBaseAndBaselineFile
	}
	var head []string
	if len(opts.Packages) == 0 {
		head, err = t.Ext.Packages(t.Mod.Root)
		if err != nil {
			return nil, nil, err
		}
	}
	logger := t.logger()
	src, err := resolveBaselineSource(ctx, t, opts)
	if err != nil {
		return nil, nil, err
	}
	selected, deleted := opts.Packages, []string(nil)
	if len(selected) == 0 {
		selected, deleted, err = selectPackages(ctx, t, head, src, opts.All)
		if err != nil {
			return nil, nil, err
		}
	}
	base := src.file
	if base == nil && len(selected) > 0 {
		// Nothing selected means nothing to compare; skip the second load.
		base, err = gitBaseline(ctx, t, src.ref, opts.Baselines)
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
// file at baseline.DefaultPath, that file is used and a warning says so;
// without that file the error matches ErrNoBaseline. Files are read
// through opts.Baselines.
func resolveBaselineSource(ctx context.Context, t *Target, opts CheckOptions) (baselineSource, error) {
	if opts.BaselineFile != "" {
		b, err := fileBaseline(opts.BaselineFile, opts.Baselines)
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
		return baselineSource{}, &noBaselineError{err: refErr}
	}
	t.logger().Warn("no default baseline ref; using the baseline file", "path", path, "err", refErr)
	b, err := fileBaseline(path, opts.Baselines)
	if err != nil {
		return baselineSource{}, err
	}
	return baselineSource{file: b}, nil
}

// fileBaseline reads the baseline file at path through cache, keyed by the
// file's absolute path, size and modification time; a nil cache reads it.
func fileBaseline(path string, cache *BaselineCache) (baseline.Baseline, error) {
	load := func() (baseline.Baseline, error) { return baseline.FromFile(path) }
	if cache == nil {
		return load()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving baseline file %s: %w", path, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("reading baseline file: %w", err)
	}
	key := "file:" + abs + "\x00" + strconv.FormatInt(fi.Size(), 10) + "\x00" +
		strconv.FormatInt(fi.ModTime().UnixNano(), 10)
	return cache.get(key, load)
}

// gitBaseline extracts the baseline at the merge-base of HEAD and ref. With
// a cache it resolves the merge-base first and keys the baseline by that
// commit, so later calls whose HEAD keeps the same merge-base reuse it; a
// nil cache leaves the whole resolution to baseline.FromGit.
func gitBaseline(ctx context.Context, t *Target, ref string, cache *BaselineCache) (baseline.Baseline, error) {
	if cache == nil {
		return baseline.FromGit(ctx, t.Mod.Root, ref, t.Ext, t.Mod.ModulePath)
	}
	sha, err := baseline.MergeBase(ctx, t.Mod.Root, ref)
	if err != nil {
		return nil, err
	}
	return cache.get("git:"+sha, func() (baseline.Baseline, error) {
		// The merge-base of HEAD and one of its ancestors is that ancestor.
		return baseline.FromGit(ctx, t.Mod.Root, sha, t.Ext, t.Mod.ModulePath)
	})
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
