package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
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
	// Coverage opts into measuring coverage_pct at head for the selected
	// packages, in one run after extraction. The baseline is never
	// measured, so coverage_pct is reported, never compared.
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
// head's coverage so it moves with the change, not with the opt-in.
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
	// text (baseline.TreeRelative); empty for the working tree.
	ht, tmp := t, ""
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
	}
	var head []string
	if len(opts.Packages) == 0 {
		head, err = ht.Ext.Packages(ht.Mod.Root)
		if err != nil {
			return nil, nil, baseline.TreeRelative(err, tmp)
		}
	}
	logger := t.logger()
	src, err := resolveBaselineSource(ctx, t, opts)
	if err != nil {
		return nil, nil, err
	}
	if src.file != nil {
		warnTokenizerMismatch(t, src.file)
	}
	// base is loaded at most once, when selection or the comparison first
	// needs it.
	base := src.file
	loadBase := func() (baseline.Baseline, error) {
		if base == nil {
			b, err := gitBaseline(ctx, t, src.ref, opts.Baselines)
			if err != nil {
				return nil, err
			}
			base = b
		}
		return base, nil
	}
	selected, deleted := opts.Packages, []string(nil)
	if len(selected) == 0 {
		selected, deleted, err = selectPackages(ctx, t, ht.Mod, head, src, opts.All, headOf(ht, opts), loadBase)
		if err != nil {
			return nil, nil, baseline.TreeRelative(err, tmp)
		}
	}
	if len(selected) > 0 {
		// Nothing selected means nothing to compare; skip the second load.
		if _, err := loadBase(); err != nil {
			return nil, nil, err
		}
	}

	c = &report.Check{
		Packages:  make([]report.CheckedPackage, 0, len(selected)),
		Deleted:   deleted,
		ModuleDir: baseline.RepoDir(ctx, t.Mod.Root),
		Tokenizer: t.tokenizer(),
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
		m, err := ht.Ext.Extract(ctx, ht.Mod, pkg)
		if err != nil {
			fail(pkg, err)
			continue
		}
		extracted[pkg] = &m
		order = append(order, pkg)
	}
	measureCoverage(ctx, ht, opts.Coverage, extracted, order)
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
		m, judged, err := jc.Module(ctx, mm, src.file != nil, opts.Packages)
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

// headOf returns the head side of the changed-package diff for a check of
// ht with opts: the index and its copy at ht's module root when
// opts.Staged, the working tree otherwise.
func headOf(ht *Target, opts CheckOptions) baseline.Head {
	if !opts.Staged {
		return baseline.Head{}
	}
	return baseline.Head{Staged: true, IndexFile: opts.IndexFile, Tree: ht.Mod.Root}
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
		b, err := fileBaseline(opts.BaselineFile, opts.Baselines, t.logger())
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
	b, err := fileBaseline(path, opts.Baselines, t.logger())
	if err != nil {
		return baselineSource{}, err
	}
	return baselineSource{file: b}, nil
}

// fileBaseline reads the baseline file at path through cache, keyed by the
// file's absolute path, size and modification time; a nil cache reads it.
// Reading a file that stores the module row under its old key, "module",
// logs one warning to logger saying so, once per read of the file.
func fileBaseline(path string, cache *BaselineCache, logger *slog.Logger) (baseline.Baseline, error) {
	load := func() (baseline.Baseline, error) {
		b, err := baseline.FromFile(path)
		if err == nil && baseline.MigratedModuleRow(b) {
			logger.Warn("baseline file stores the module row under the old key \"module\"; "+
				"run `astimate baseline write` to rewrite it", "path", path, "row", metrics.ModuleRowID)
		}
		return b, err
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
		return baseline.FromGit(ctx, t.Mod.Root, ref, t.Ext, t.Mod.ModulePath, t.tokenizer())
	}
	sha, err := baseline.MergeBase(ctx, t.Mod.Root, ref)
	if err != nil {
		return nil, err
	}
	return cache.get("git:"+sha, func() (baseline.Baseline, error) {
		// The merge-base of HEAD and one of its ancestors is that ancestor.
		return baseline.FromGit(ctx, t.Mod.Root, sha, t.Ext, t.Mod.ModulePath, t.tokenizer())
	})
}

// selectPackages returns the import paths to check, in the order of head
// (the import paths the extractor lists at head), and the module-relative
// directories deleted since the baseline. With all set it returns head.
// Otherwise it takes the packages changed since the merge-base of HEAD and
// the baseline's ref and keeps those in head, which drops the directories
// the go tool ignores. A file baseline whose ref does not resolve in git,
// for example outside a repository, and an extractor that does not
// implement metrics.SourceClassifier select every package and say so. A
// changed file that can move every package, such as a TypeScript
// tsconfig.json, selects every package too. A changed file that can move
// its package's importers, such as a TypeScript declaration file, also
// selects the packages importing that package in hm, the head module, and
// in the baseline, which loadBase returns (selectImporters). tree says
// whether the changes are those of the working tree or of the index.
func selectPackages(ctx context.Context, t *Target, hm *metrics.ModuleContext, head []string, src baselineSource,
	all bool, tree baseline.Head, loadBase func() (baseline.Baseline, error),
) (selected, deleted []string, err error) {
	if all {
		return head, nil, nil
	}
	logger := t.logger()
	sc, ok := t.Ext.(metrics.SourceClassifier)
	if !ok {
		logger.Warn("the extractor cannot tell which files changed a package; checking every package",
			"language", t.Ext.Language())
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
	mergeBase, err := baseline.MergeBase(ctx, t.Mod.Root, ref)
	if err != nil {
		if src.file == nil {
			return nil, nil, err
		}
		logger.Warn("cannot resolve the baseline file's ref; checking every package", "ref", ref, "err", err)
		return head, nil, nil
	}
	change, err := baseline.ChangedPackages(ctx, t.Mod.Root, mergeBase, sc, tree)
	if err != nil {
		return nil, nil, err
	}
	if change.All {
		logger.Info("a changed file can move every package; checking every package")
		return head, change.Deleted, nil
	}
	changed := make(map[string]bool, len(change.Packages))
	for _, dir := range change.Packages {
		changed[judge.ImportPath(t.Mod.ModulePath, dir)] = true
	}
	if il, ok := t.Ext.(metrics.ImporterLister); ok && len(change.Contract)+len(change.RemovedContract) > 0 {
		base, err := loadBase()
		if err != nil {
			return nil, nil, err
		}
		if err := selectImporters(ctx, il, hm, t.Mod.ModulePath, head, change, base, changed, logger); err != nil {
			return nil, nil, err
		}
	}
	for _, pkg := range head {
		if changed[pkg] {
			selected = append(selected, pkg)
		}
	}
	return selected, change.Deleted, nil
}

// importerGraph is the reverse import graph a baseline recorded
// (baseline.Baseline.Importers).
type importerGraph interface {
	// Importers returns the importers of pkg, and false when no graph was
	// recorded.
	Importers(pkg string) ([]string, bool)
}

// selectImporters adds to changed the import paths of the packages, among
// head (the import paths the extractor lists), that import a package whose
// change can move their importers' metrics. change names those packages by
// module-relative directory: the importers of each package of
// change.Contract that head holds are taken from il in mod, the head
// module of module path modPath; the importers of each package of
// change.Contract and change.RemovedContract are also taken from base, the
// baseline's graph, so an edge that a removed or rewritten declaration
// file resolved still selects its importer. Each package it adds is logged
// at info level with the package it imports, the baseline's saying
// "importer at baseline", so the output says why a package no file of
// which changed was checked. When base is nil or records no graph and a
// declaration file was removed, one info line says its former importers
// are not selected.
func selectImporters(ctx context.Context, il metrics.ImporterLister, mod *metrics.ModuleContext, modPath string,
	head []string, change baseline.Change, base importerGraph, changed map[string]bool, logger *slog.Logger,
) error {
	inHead := make(map[string]bool, len(head))
	for _, pkg := range head {
		inHead[pkg] = true
	}
	add := func(importers []string, dir, msg string) {
		for _, imp := range importers {
			if !inHead[imp] || changed[imp] {
				continue
			}
			changed[imp] = true
			logger.Info(msg, "package", judge.Rel(modPath, imp), "importer_of", dir)
		}
	}
	for _, dir := range change.Contract {
		pkg := judge.ImportPath(modPath, dir)
		if !inHead[pkg] {
			continue
		}
		importers, err := il.Importers(ctx, mod, pkg)
		if err != nil {
			return fmt.Errorf("listing the importers of %s: %w", dir, err)
		}
		add(importers, dir, "selected as an importer of a package whose declarations changed")
	}
	dirs := slices.Concat(change.Contract, change.RemovedContract)
	slices.Sort(dirs)
	for _, dir := range slices.Compact(dirs) {
		var importers []string
		known := base != nil
		if known {
			importers, known = base.Importers(judge.ImportPath(modPath, dir))
		}
		if !known {
			if len(change.RemovedContract) > 0 {
				logger.Info("the baseline records no import graph; the former importers of a removed declaration file are not selected",
					"packages", strings.Join(change.RemovedContract, ","))
			}
			return nil
		}
		add(importers, dir, "selected as an importer at baseline of a package whose declarations changed")
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
