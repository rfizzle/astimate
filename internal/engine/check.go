package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/engine/internal/selection"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
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

// IsSkipped reports whether err, from Check's failed, is that of a
// selected package the extractor skipped because it does not load.
func IsSkipped(err error) bool { return errors.Is(err, judge.ErrSkipped) }

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
	// Staged checks the tree the git index holds instead of the working
	// tree, so a partial commit is judged on what it commits: head is
	// extracted from a temporary copy of the index (baseline.StagedTree)
	// and only staged changes select packages. The baseline is unchanged.
	// It needs a git repository.
	Staged bool
	// IndexFile is the index Staged reads, as GIT_INDEX_FILE names it
	// inside a git hook; empty means the repository's own index. Ignored
	// unless Staged.
	IndexFile string
	// Coverage opts into measuring coverage_pct and
	// untested_exports_covered at head for the selected packages, in one
	// call after extraction. The baseline is never measured, so
	// coverage_pct is reported, never compared; a source: coverage rule
	// judges untested_exports_covered against the baseline's count
	// estimated from head's run.
	Coverage CoverageOptions
	// Now is the time an exemption's expiry is judged at
	// (gate.Exemption.Expired); the zero time means time.Now().
	Now time.Time
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
// the thresholds configured for t's language (config.Config.ForLanguage),
// resolved once per call. The head tree is listed with one Packages call
// and the baseline tree, when it comes from git, with one more inside
// baseline.FromGit, so each tree is loaded once; opts.Packages skips the
// listing and opts.Baselines may skip the baseline. It returns an error
// when no verdict can be reached: listing fails or the baseline or the
// changed packages cannot be resolved; an error matching ErrNoBaseline
// means no baseline was given or found. A package that fails to extract
// is logged, left out of the result and returned as a *PackageError in
// failed.
//
// A package the extractor skipped because it does not load
// (metrics.Skipper) is listed in the result's Skipped, every one of them,
// with Selected false unless its own files changed since the baseline
// (selection.Packages) or opts.Packages names it. One that is selected
// cannot be judged, so it is logged and returned in failed with an error
// IsSkipped reports. The rest are gated as usual.
//
// A package the extractor skipped in a git baseline's tree, because it
// failed to load there, has no baseline row but is not new: the baseline
// records why (baseline.Baseline.Unavailable), and the package is judged
// against the maxes only, with the ratchets skipped and the reason in its
// report (judge.Checker.Package).
//
// When t's extractor implements metrics.ModuleMetrics and at least one
// package is selected, the result also carries the module-level row
// (judge.Checker.Module), gated against the baseline's row of the same id.
// Rules on module-wide metrics (metrics.ModuleWide) are evaluated on that
// row only and every other rule on package rows only (gate.ForRow), so one
// cross-package copy is one finding. A baseline file without that row, one
// written before it existed, skips the module-wide rules with one info log
// rather than treating the row as new. A check of opts.Packages carries the
// row too, so a package's self-check (the MCP check_package tool) fails on
// a cross-package copy made in it, but not on one between two other
// packages (judge.Checker.Module). A module row that fails to extract is
// logged and returned in failed like a package.
//
// When t's extractor implements metrics.FunctionLister, each package's
// changed_func_cognitive_max is computed here, from its functions at head
// and in the baseline (judge.Checker.Package); otherwise, and for a
// baseline file that records no functions, it stays null and its rule is
// skipped, the latter with one info log.
//
// Each finding is located on the file and line that caused it where the
// extractor can say (judge.Checker.Package, judge.Checker.Module), and the
// result carries the module root's directory in its repository
// (baseline.RepoDir) so renderers can make those paths
// repository-relative. Each baseline block
// records the baseline's tokenizer and whether it is t's
// (report.MarkTokenizer).
//
// The selected packages are all extracted before any is gated, so that
// with opts.Coverage their coverage is measured at head in one run
// (measureCoverage). coverage_pct is then reported only: the baseline is
// never measured, and the baseline's agent_passes in the summary borrows
// head's coverage so it moves with the change, not with the opt-in. A
// source: coverage rule on untested_exports judges untested_exports_covered
// against the baseline's untested exports less those head's run executed
// (judge).
//
// Each selected package's churn is read at HEAD of t's repository, from
// one history walk (MeasureChurn), and reported only: a git baseline
// carries none, a baseline file what it recorded, and no rule reads it.
//
// With opts.Staged the head packages are listed and extracted from a
// temporary copy of the index (stagedTarget), removed before Check
// returns, also on SIGINT or SIGTERM; git commands and the baseline still
// use t's module root, and report paths are module-relative as always.
// Errors, logged or returned, name paths relative to the module root
// instead of the temporary copy (baseline.TreeRelative).
//
// The configured exemptions (config.Effective.Exemptions, SPEC.md 8.6) are
// applied to every row after it is evaluated (gate.Exemptions.Apply): a
// violation an unexpired exemption matches moves to the row's exempted
// findings, located like the others, and the row's verdict is taken from
// the violations left. Each exemption expired at opts.Now is ignored and
// reported, in every check, as a warning on its row when the check has
// that row, else in a warning log. A check with opts.All and no
// opts.Packages judges every row, so it also reports each exemption that
// matched no violation as stale (judge.Checker.Notices); no other check
// can tell, since the rows it did not select were never evaluated.
func Check(ctx context.Context, t *Target, opts CheckOptions) (c *report.Check, failed []error, err error) {
	if opts.Base != "" && opts.BaselineFile != "" {
		return nil, nil, ErrBaseAndBaselineFile
	}
	// tmp is the module root in the staged copy, rewritten out of error
	// text (baseline.TreeRelative); empty for the working tree. tree is
	// the head side of the changed-package diff.
	ht, tmp, tree := t, "", baseline.Head{}
	if opts.Staged {
		var stop, cleanup func()
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		ht, cleanup, err = stagedTarget(ctx, t, opts.IndexFile)
		if err != nil {
			return nil, nil, err
		}
		defer cleanup()
		tmp = ht.Mod.Root
		tree = baseline.Head{Staged: true, IndexFile: opts.IndexFile, Tree: tmp}
	}
	var head []string
	if len(opts.Packages) == 0 {
		head, err = ht.Ext.Packages(ht.Mod.Root)
		if err != nil {
			return nil, nil, baseline.TreeRelative(err, tmp)
		}
	}
	skipped, err := judge.SkippedIn(ht.Ext, ht.Mod.Root)
	if err != nil {
		return nil, nil, baseline.TreeRelative(err, tmp)
	}
	logger := t.logger()
	src, err := resolveBaselineSource(ctx, t, opts)
	if err != nil {
		return nil, nil, err
	}
	if src.File != nil {
		warnTokenizerMismatch(t, src.File)
	}
	// base is loaded at most once, when selection or the comparison first
	// needs it.
	base := src.File
	loadBase := func() (baseline.Baseline, error) {
		if base == nil {
			b, err := gitBaseline(ctx, t, src.Ref, opts.Baselines)
			if err != nil {
				return nil, err
			}
			base = b
		}
		return base, nil
	}
	req := selection.Request{
		Ext: t.Ext, Mod: t.Mod, Head: ht.Mod, Source: src,
		All: opts.All, Tree: tree, Skips: skipped, LoadBase: loadBase, Logger: logger,
	}
	selected, deleted := opts.Packages, []string(nil)
	if len(selected) == 0 {
		req.Packages = skipped.With(head)
		selected, deleted, err = selection.Packages(ctx, req)
		if err != nil {
			return nil, nil, baseline.TreeRelative(err, tmp)
		}
	}
	if len(selected) > 0 {
		// Nothing selected means nothing to compare; skip the second load.
		if _, err := loadBase(); err != nil {
			return nil, nil, err
		}
		if base, err = selection.Seed(ctx, req, base); err != nil {
			return nil, nil, baseline.TreeRelative(err, tmp)
		}
	}

	c = &report.Check{
		Packages:  make([]report.CheckedPackage, 0, len(selected)),
		Deleted:   deleted,
		ModuleDir: baseline.RepoDir(ctx, t.Mod.Root),
		Tokenizer: t.tokenizer(),
		Skipped:   skipped.Report(t.Mod.ModulePath, selected),
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	jc := judge.New(judge.Options{
		Ext: ht.Ext, Mod: ht.Mod, Base: base, Config: t.langConfig(),
		Tokenizer: t.tokenizer(), Version: t.Version, Now: now, Logger: logger,
	})
	fail := func(pkg string, err error) {
		err = baseline.TreeRelative(err, tmp)
		path := judge.Rel(t.Mod.ModulePath, pkg)
		logger.Error("checking package failed", "path", path, "err", err)
		failed = append(failed, &PackageError{Path: path, Err: err})
	}
	extracted := make(map[string]*metrics.RawMetrics, len(selected))
	order := make([]string, 0, len(selected))
	for _, pkg := range selected {
		if err := skipped.Failure(pkg); err != nil {
			fail(pkg, err)
			continue
		}
		m, err := ht.Ext.Extract(ctx, ht.Mod, pkg)
		if err != nil {
			fail(pkg, err)
			continue
		}
		extracted[pkg] = &m
		order = append(order, pkg)
	}
	measureCoverage(ctx, ht, opts.Coverage, extracted, order)
	// Churn reads the history of the repository, never the staged copy.
	fillChurn(ctx, t, extracted)
	noFunctions := false
	for _, pkg := range order {
		p, unrecorded, err := jc.Package(ctx, pkg, *extracted[pkg])
		if err != nil {
			fail(pkg, err)
			continue
		}
		noFunctions = noFunctions || unrecorded
		c.Packages = append(c.Packages, p)
	}
	if noFunctions {
		logger.Info("rule skipped", "metric", "changed_func_cognitive_max",
			"reason", "the baseline records no functions to diff; rewrite the baseline file with astimate baseline write")
	}
	moduleJudged := false
	if mm, ok := t.Ext.(metrics.ModuleMetrics); ok && len(selected) > 0 {
		m, judged, err := jc.Module(ctx, mm, src.File != nil, opts.Packages)
		if err != nil {
			err = baseline.TreeRelative(err, tmp)
			logger.Error("checking module row failed", "err", err)
			failed = append(failed, &PackageError{Path: metrics.ModuleRowID, Err: err})
		} else {
			c.Module = &m
			moduleJudged = judged
		}
	}
	jc.Notices(c, judge.Scope{
		All:          opts.All && len(opts.Packages) == 0,
		Failed:       failedPaths(failed),
		ModuleJudged: moduleJudged,
	})
	return c, failed, nil
}

// stagedTarget copies the git index of the repository holding t's module
// into a temporary directory (baseline.StagedTree) and returns a copy of t
// whose module root is the module's counterpart there, with a cleanup that
// makes t's extractor forget that root, when it caches loads, and removes
// the directory. indexFile is passed to baseline.StagedTree.
func stagedTarget(ctx context.Context, t *Target, indexFile string) (*Target, func(), error) {
	root, remove, err := baseline.StagedTree(ctx, t.Mod.Root, indexFile)
	if err != nil {
		return nil, nil, fmt.Errorf("checking the staged tree: %w", err)
	}
	ht := *t
	ht.Mod = &metrics.ModuleContext{Root: root, ModulePath: t.Mod.ModulePath}
	cleanup := func() {
		if f, ok := t.Ext.(metrics.Forgetter); ok {
			f.Forget(root)
		}
		remove()
	}
	return &ht, cleanup, nil
}

// tokenizer returns t.Tokenizer, or TokenizerEst when it is empty.
func (t *Target) tokenizer() string {
	if t.Tokenizer == "" {
		return TokenizerEst
	}
	return t.Tokenizer
}

// warnTokenizerMismatch logs one warning when the file baseline b counted
// tokens with another tokenizer than t does. The gate still runs: capacity
// rules are absolute, but tokens_est deltas against b are not comparable.
func warnTokenizerMismatch(t *Target, b baseline.Baseline) {
	if bt, ct := b.Tokenizer(), t.tokenizer(); bt != ct {
		t.logger().Warn("baseline tokenizer " + bt + " differs from check tokenizer " + ct +
			"; token counts are not comparable")
	}
}

// resolveBaselineSource picks the baseline per SPEC.md 8.3: BaselineFile
// reads the file, Base names the ref, and with neither the first default
// ref is used. When no default ref exists but the module has a baseline
// file at baseline.DefaultPath, that file is used and a warning says so;
// without that file the error matches ErrNoBaseline. Files are read
// through opts.Baselines.
func resolveBaselineSource(ctx context.Context, t *Target, opts CheckOptions) (selection.Source, error) {
	if opts.BaselineFile != "" {
		b, err := fileBaseline(opts.BaselineFile, opts.Baselines, t.logger())
		if err != nil {
			return selection.Source{}, err
		}
		return selection.Source{File: b}, nil
	}
	if opts.Base != "" {
		return selection.Source{Ref: opts.Base}, nil
	}
	root := t.Mod.Root
	ref, refErr := baseline.DefaultRef(ctx, root)
	if refErr == nil {
		return selection.Source{Ref: ref}, nil
	}
	path := filepath.Join(root, baseline.DefaultPath)
	if _, err := os.Stat(path); err != nil {
		return selection.Source{}, &noBaselineError{err: refErr}
	}
	t.logger().Warn("no default baseline ref; using the baseline file", "path", path, "err", refErr)
	b, err := fileBaseline(path, opts.Baselines, t.logger())
	if err != nil {
		return selection.Source{}, err
	}
	return selection.Source{File: b}, nil
}

// fileBaseline reads the baseline file at path through cache, keyed by the
// file's absolute path, size and modification time; a nil cache reads it.
// Reading a file that stores the module row under its old key, "module",
// logs one warning to logger saying so, and reading one that keys its
// packages by import path only another, each once per read of the file.
func fileBaseline(path string, cache *BaselineCache, logger *slog.Logger) (baseline.Baseline, error) {
	load := func() (baseline.Baseline, error) {
		b, err := baseline.FromFile(path)
		if err != nil {
			return nil, err
		}
		if baseline.MigratedModuleRow(b) {
			logger.Warn("baseline file stores the module row under the old key \"module\"; "+
				"run `astimate baseline write` to rewrite it", "path", path, "row", metrics.ModuleRowID)
		}
		if baseline.KeyedByImportPath(b) {
			logger.Warn("baseline file keys packages by import path only; "+
				"run `astimate baseline write` to record each package_path", "path", path)
		}
		return b, nil
	}
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
		return baseline.FromGit(ctx, t.Mod.Root, ref, t.Ext, t.modulePathFunc(), t.tokenizer())
	}
	sha, err := baseline.MergeBase(ctx, t.Mod.Root, ref)
	if err != nil {
		return nil, err
	}
	return cache.get("git:"+sha, func() (baseline.Baseline, error) {
		// The merge-base of HEAD and one of its ancestors is that ancestor.
		return baseline.FromGit(ctx, t.Mod.Root, sha, t.Ext, t.modulePathFunc(), t.tokenizer())
	})
}

// modulePathFunc returns how to read the module path of a tree of t's
// language, which a git baseline keys its packages' directories by:
// golang.ModulePath for Go, nil for a language whose package identifiers
// already are module-relative directories.
func (t *Target) modulePathFunc() baseline.ModulePathFunc {
	if t.Ext.Language() == languageGo {
		return golang.ModulePath
	}
	return nil
}

// failedPaths returns the package paths of the rows in failed, each a
// *PackageError.
func failedPaths(failed []error) []string {
	paths := make([]string, 0, len(failed))
	for _, err := range failed {
		var pe *PackageError
		if errors.As(err, &pe) {
			paths = append(paths, pe.Path)
		}
	}
	return paths
}
