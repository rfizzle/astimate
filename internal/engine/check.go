package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
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
// (checkModule), gated against the baseline's row of the same id. Rules on
// module-wide metrics (metrics.ModuleWide) are evaluated on that row only
// and every other rule on package rows only (gate.ForRow), so one
// cross-package copy is one finding. A baseline file without that row, one
// written before it existed, skips the module-wide rules with one info log
// rather than treating the row as new. A check of opts.Packages carries the
// row too, so a package's self-check (the MCP check_package tool) fails on
// a cross-package copy made in it, but not on one between two other
// packages (checkModule). A module row that fails to extract is
// logged and returned in failed like a package.
//
// When t's extractor implements metrics.FunctionLister, each package's
// changed_func_cognitive_max is computed here, from its functions at head
// and in the baseline (changedFunctions); otherwise, and for a baseline
// file that records no functions, it stays null and its rule is skipped,
// the latter with one info log.
//
// Each finding is located on the file and line that caused it where the
// extractor can say (locateFindings, locateCross), and the result carries
// the module root's directory in its repository (baseline.RepoDir) so
// renderers can make those paths repository-relative. Each baseline block
// records the baseline's tokenizer and whether it is t's
// (report.MarkTokenizer).
//
// With opts.Staged the head packages are listed and extracted from a
// temporary copy of the index (stagedTarget), removed before Check
// returns, also on SIGINT or SIGTERM; git commands and the baseline still
// use t's module root, and report paths are module-relative as always.
// Errors, logged or returned, name paths relative to the module root
// instead of the temporary copy (baseline.TreeRelative).
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
	selected, deleted := opts.Packages, []string(nil)
	if len(selected) == 0 {
		selected, deleted, err = selectPackages(ctx, t, ht.Mod, head, src, opts.All, headOf(ht, opts))
		if err != nil {
			return nil, nil, baseline.TreeRelative(err, tmp)
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

	c = &report.Check{
		Packages:  make([]report.CheckedPackage, 0, len(selected)),
		Deleted:   deleted,
		ModuleDir: baseline.RepoDir(ctx, t.Mod.Root),
		Tokenizer: t.tokenizer(),
	}
	noFunctions := false
	eff := t.langConfig()
	pkgRules := gate.ForRow(eff.Thresholds, gate.PackageRow)
	for _, pkg := range selected {
		p, unrecorded, err := checkPackage(ctx, ht, base, pkg, eff, pkgRules)
		if err != nil {
			err = baseline.TreeRelative(err, tmp)
			path := modulePathRel(t.Mod.ModulePath, pkg)
			logger.Error("checking package failed", "path", path, "err", err)
			failed = append(failed, &PackageError{Path: path, Err: err})
			continue
		}
		noFunctions = noFunctions || unrecorded
		c.Packages = append(c.Packages, p)
	}
	if noFunctions {
		logger.Info("rule skipped", "metric", "changed_func_cognitive_max",
			"reason", "the baseline records no functions to diff; rewrite the baseline file with astimate baseline write")
	}
	if mm, ok := t.Ext.(metrics.ModuleMetrics); ok && len(selected) > 0 {
		m, err := checkModule(ctx, ht, mm, base, src.file != nil, opts.Packages, eff, gate.ForRow(eff.Thresholds, gate.ModuleRow))
		if err != nil {
			err = baseline.TreeRelative(err, tmp)
			logger.Error("checking module row failed", "err", err)
			failed = append(failed, &PackageError{Path: metrics.ModuleRowID, Err: err})
		} else {
			c.Module = &m
		}
	}
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
// selects the packages importing that package in hm, the head module
// (selectImporters). tree says whether the changes are those of the
// working tree or of the index.
func selectPackages(ctx context.Context, t *Target, hm *metrics.ModuleContext, head []string, src baselineSource,
	all bool, tree baseline.Head,
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
		changed[importPathOf(t.Mod.ModulePath, dir)] = true
	}
	if il, ok := t.Ext.(metrics.ImporterLister); ok && len(change.Contract) > 0 {
		if err := selectImporters(ctx, il, hm, t.Mod.ModulePath, head, change.Contract, changed, logger); err != nil {
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

// selectImporters adds to changed the import paths of the packages that
// import a package of contract, the module-relative directories of the
// changed packages whose change can move their importers' metrics
// (baseline.Change.Contract), as il lists them in mod, the head module of
// module path modPath. A package of contract that head, the import paths
// the extractor lists, does not hold is skipped. Each package it adds is
// logged at info level with the package it imports, so the output says why
// a package no file of which changed was checked.
func selectImporters(ctx context.Context, il metrics.ImporterLister, mod *metrics.ModuleContext, modPath string,
	head, contract []string, changed map[string]bool, logger *slog.Logger,
) error {
	for _, dir := range contract {
		pkg := importPathOf(modPath, dir)
		if !slices.Contains(head, pkg) {
			continue
		}
		importers, err := il.Importers(ctx, mod, pkg)
		if err != nil {
			return fmt.Errorf("listing the importers of %s: %w", dir, err)
		}
		for _, imp := range importers {
			if changed[imp] {
				continue
			}
			changed[imp] = true
			logger.Info("selected as an importer of a package whose declarations changed",
				"package", modulePathRel(modPath, imp), "importer_of", dir)
		}
	}
	return nil
}

// checkModule builds the module-level row of t's module with mm, evaluates
// it against the baseline's row under metrics.ModuleRowID, if base has one,
// and rules, the configured thresholds that apply to the module row, as
// checkPackage does for a package, and builds its report under package
// path metrics.ModuleRowID with eff, the configuration of t's language.
// When t's extractor implements metrics.ModuleDetailer, the
// dup_blocks_cross_pkg suggestion names where the first shared block lives
// and its findings are located on the block's first occurrence; otherwise
// the row's suggestions name no locations. It carries no baseline agent
// passes, since its rebuild estimate is of an empty package.
//
// named are the packages a check of named packages checks
// (CheckOptions.Packages); empty for any other check. When it is set and
// t's extractor implements metrics.ModuleDetailer, the rules judge
// dup_blocks_cross_pkg on the blocks blamed on those packages only
// (blameNamed), so a copy between two other packages neither fails the
// check nor appears in its findings; the row still reports the full count,
// and the suggestion names the packages sharing each blamed block.
//
// fromFile says base was read from a baseline file. A file written before
// the module row existed has none, and the blocks it would have counted
// are not new: the rules are skipped for this run with one info log, and
// the row's metrics are still reported. A baseline extracted from a commit
// always has the row.
func checkModule(ctx context.Context, t *Target, mm metrics.ModuleMetrics, base baseline.Baseline, fromFile bool,
	named []string, eff config.Effective, rules []gate.Threshold,
) (report.CheckedPackage, error) {
	m, err := mm.ModuleRow(ctx, t.Mod)
	if err != nil {
		return report.CheckedPackage{}, err
	}
	logger := t.logger()
	var bm *metrics.RawMetrics
	if v, ok := base.Metrics(metrics.ModuleRowID); ok {
		bm = &v
	} else if fromFile && len(rules) > 0 {
		logger.Info("baseline file has no module row; module-wide rules skipped; run `astimate baseline write` to add it")
		rules = nil
	}
	names, err := moduleNames(ctx, t.Ext, t.Mod)
	if err != nil {
		return report.CheckedPackage{}, err
	}
	gm, blame := m, (*crossBlame)(nil)
	if _, detailed := t.Ext.(metrics.ModuleDetailer); detailed && len(named) > 0 && len(rules) > 0 && m.DupBlocksCrossPkg != nil {
		gm, blame = blameNamed(m, bm, base, names.CrossBlocks, named)
	}
	suggest := func(metric string, h float64, hm metrics.RawMetrics) string {
		if blame != nil && metric == "dup_blocks_cross_pkg" {
			if s := blame.suggestion(hm, t.Mod.ModulePath); s != "" {
				return s
			}
		}
		return score.MetricSuggestion(metric, h, hm, names)
	}
	res := gate.Evaluate(gm, bm, rules, suggest)
	for _, n := range res.Notes {
		logger.Info("rule skipped", "path", metrics.ModuleRowID, "metric", n.Metric, "reason", n.Text)
	}
	r := report.Build(&report.Input{
		Language:        t.Ext.Language(),
		PackagePath:     metrics.ModuleRowID,
		ModulePath:      t.Mod.ModulePath,
		Metrics:         m,
		Params:          eff.Rebuild,
		ConfigVersion:   eff.Version,
		AstimateVersion: t.Version,
	})
	report.ApplyGate(&r, base.Ref(), bm, &res)
	report.MarkTokenizer(&r, base.Tokenizer(), t.tokenizer())
	located := names.CrossBlocks
	if blame != nil && len(blame.blocks) > 0 {
		located = blame.blocks
	}
	locateCross(&r, located)
	return report.CheckedPackage{Report: r}, nil
}

// moduleNames returns the names behind the module row's counts for
// suggestions when ext implements metrics.ModuleDetailer, and the zero
// score.Names otherwise.
func moduleNames(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext) (score.Names, error) {
	d, ok := ext.(metrics.ModuleDetailer)
	if !ok {
		return score.Names{}, nil
	}
	det, err := d.ModuleDetails(ctx, mod)
	if err != nil {
		return score.Names{}, fmt.Errorf("naming suggestions for %s: %w", metrics.ModuleRowID, err)
	}
	return score.Names{CrossBlocks: det.CrossBlocks}, nil
}

// locateCross locates r's dup_blocks_cross_pkg findings on the first
// occurrence of the first of blocks, the one their suggestion names first,
// so a renderer can annotate that file and line.
func locateCross(r *report.Report, blocks []metrics.CrossBlock) {
	if len(blocks) == 0 || len(blocks[0].Occurrences) == 0 {
		return
	}
	o := blocks[0].Occurrences[0]
	for _, fs := range [][]report.Finding{r.Violations, r.Warnings} {
		for i := range fs {
			if fs[i].Metric == "dup_blocks_cross_pkg" {
				fs[i].File, fs[i].Line = o.File, o.StartLine
			}
		}
	}
}

// checkPackage extracts pkg at head, fills changed_func_cognitive_max from
// the function-level diff against base (changedFunctions), evaluates it
// against its baseline metrics, if base has any, and rules, the configured
// thresholds that apply to a package row, and builds its report with eff,
// the configuration of t's language, its findings located where the
// extractor's details say (locateFindings). unrecorded reports that the
// diff was skipped because base has pkg but no functions for it.
func checkPackage(ctx context.Context, t *Target, base baseline.Baseline, pkg string, eff config.Effective, rules []gate.Threshold) (p report.CheckedPackage, unrecorded bool, err error) {
	m, err := t.Ext.Extract(ctx, t.Mod, pkg)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	det, err := packageDetails(ctx, t.Ext, t.Mod, pkg)
	if err != nil {
		return report.CheckedPackage{}, false, err
	}
	names := namesOf(&det)
	var bm *metrics.RawMetrics
	if v, ok := base.Metrics(pkg); ok {
		bm = &v
	}
	worst, unrecorded, err := changedFunctions(ctx, t, base, pkg, bm != nil)
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
	res := gate.Evaluate(m, bm, rules, suggest)
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
		Params:          eff.Rebuild,
		ConfigVersion:   eff.Version,
		AstimateVersion: t.Version,
	})
	report.ApplyGate(&r, base.Ref(), bm, &res)
	report.MarkTokenizer(&r, base.Tokenizer(), t.tokenizer())
	locateFindings(&r, &det, worst)
	p = report.CheckedPackage{Report: r}
	if bm != nil {
		passes := score.Estimate(*bm, eff.Rebuild).AgentPassesRounded()
		p.BaseAgentPasses = &passes
	}
	return p, unrecorded, nil
}

// changedFunction is the most complex function added or modified since the
// baseline: its cognitive complexity, its display name and its location,
// with the files every changed function lies in.
type changedFunction struct {
	// cognitive is the function's cognitive complexity; 0 when no function
	// changed.
	cognitive int
	// name is the qualified name with "(file:line)" when the location is
	// known; empty when no function changed.
	name string
	// file and line locate the function's declaration, file relative to
	// the package directory; empty when unknown or no function changed.
	file string
	line int
	// files holds the file, relative to the package directory, of each
	// changed function whose file is known; nil when none is.
	files map[string]bool
}

// changedFunctions diffs pkg's functions at head against base's
// (metrics.ChangedFunctions) and returns the most complex changed one, or
// a zero changedFunction when none changed. inBase reports whether base
// has pkg; a package new at head diffs against no functions, so all of its
// functions are changed. It returns nil when there is nothing to diff:
// t's extractor does not implement metrics.FunctionLister, or base has pkg
// but recorded no functions for it, which unrecorded reports so the caller
// can say why the metric is null.
func changedFunctions(ctx context.Context, t *Target, base baseline.Baseline, pkg string, inBase bool) (worst *changedFunction, unrecorded bool, err error) {
	fl, ok := t.Ext.(metrics.FunctionLister)
	if !ok {
		return nil, false, nil
	}
	var before []metrics.FunctionInfo
	if inBase {
		if before, ok = base.Functions(pkg); !ok {
			return nil, true, nil
		}
	}
	head, err := fl.Functions(ctx, t.Mod, pkg)
	if err != nil {
		return nil, false, err
	}
	changed := metrics.ChangedFunctions(before, head)
	i := metrics.MostComplex(changed)
	if i < 0 {
		return &changedFunction{}, false, nil
	}
	f := &changed[i]
	name := f.QualifiedName()
	if f.File != "" {
		name += " (" + f.File + ":" + strconv.Itoa(f.Line) + ")"
	}
	w := &changedFunction{cognitive: f.Cognitive, name: name, file: f.File, line: f.Line}
	for j := range changed {
		if file := changed[j].File; file != "" {
			if w.files == nil {
				w.files = make(map[string]bool)
			}
			w.files[file] = true
		}
	}
	return w, false, nil
}

// packageDetails returns the details of pkg when ext implements
// metrics.Detailer, and the zero metrics.Details otherwise. Call it after
// Extract for pkg on mod.
func packageDetails(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext, pkg string) (metrics.Details, error) {
	d, ok := ext.(metrics.Detailer)
	if !ok {
		return metrics.Details{}, nil
	}
	det, err := d.Details(ctx, mod, pkg)
	if err != nil {
		return metrics.Details{}, fmt.Errorf("naming suggestions for %s: %w", pkg, err)
	}
	return det, nil
}

// namesOf returns the names in d that suggestions quote.
func namesOf(d *metrics.Details) score.Names {
	return score.Names{UntestedExports: d.UntestedExports, DupLocations: d.DupLocations, CrossBlocks: d.CrossBlocks}
}

// locateFindings locates each of r's findings on the file and line that
// caused it, as far as d, the package's details, and worst, its most
// complex changed function (nil when unknown), can say:
//
//   - dup_blocks and duplication_pct on a duplicate block's occurrence,
//   - untested_exports on an untested export's declaration,
//   - globals on a global's declaration,
//   - sloc, largest_file_sloc, tokens_est and tokens_est_with_tests on the
//     largest file,
//   - changed_func_cognitive_max on the function it measures,
//   - any other metric, or one of the above with nothing recorded, on the
//     package's doc.go, else its first source file.
//
// Among several candidates it takes the first in a file holding a changed
// function, so the annotation lands on the diff, else the first. Files are
// made module-relative by joining r's package path. A finding with no
// candidate keeps no location, and renderers fall back to the package
// directory.
func locateFindings(r *report.Report, d *metrics.Details, worst *changedFunction) {
	for _, fs := range [][]report.Finding{r.Violations, r.Warnings} {
		for i := range fs {
			if pos := findingPosition(fs[i].Metric, d, worst); pos.File != "" {
				fs[i].File, fs[i].Line = path.Join(r.PackagePath, pos.File), pos.Line
			}
		}
	}
}

// findingPosition returns the package-relative position locateFindings
// puts a finding on metric at; its File is empty when there is none.
func findingPosition(metric string, d *metrics.Details, worst *changedFunction) metrics.Position {
	var changed map[string]bool
	if worst != nil {
		changed = worst.files
	}
	var pos metrics.Position
	switch metric {
	case "dup_blocks", "duplication_pct":
		pos = pickPosition(dupPositions(d.DupLocations), changed)
	case "untested_exports":
		pos = pickPosition(d.UntestedPositions, changed)
	case "globals":
		pos = pickPosition(d.GlobalPositions, changed)
	case "sloc", "largest_file_sloc", "tokens_est", "tokens_est_with_tests":
		pos = metrics.Position{File: d.LargestFile, Line: 1}
	case "changed_func_cognitive_max":
		if worst != nil {
			pos = metrics.Position{File: worst.file, Line: worst.line}
		}
	}
	if pos.File != "" {
		return pos
	}
	if slices.Contains(d.SourceFiles, "doc.go") {
		return metrics.Position{File: "doc.go", Line: 1}
	}
	if len(d.SourceFiles) > 0 {
		return metrics.Position{File: d.SourceFiles[0], Line: 1}
	}
	return metrics.Position{}
}

// pickPosition returns the first of ps in a file of changed, else the first
// with a file, else the zero Position.
func pickPosition(ps []metrics.Position, changed map[string]bool) metrics.Position {
	first := metrics.Position{}
	for _, p := range ps {
		if p.File == "" {
			continue
		}
		if changed[p.File] {
			return p
		}
		if first.File == "" {
			first = p
		}
	}
	return first
}

// dupPositions parses metrics.Details.DupLocations, each "file:start-end",
// into the position of each occurrence's first line, skipping any that
// does not parse.
func dupPositions(locs []string) []metrics.Position {
	ps := make([]metrics.Position, 0, len(locs))
	for _, loc := range locs {
		i := strings.LastIndexByte(loc, ':')
		if i <= 0 {
			continue
		}
		start, _, _ := strings.Cut(loc[i+1:], "-")
		line, err := strconv.Atoi(start)
		if err != nil {
			continue
		}
		ps = append(ps, metrics.Position{File: loc[:i], Line: line})
	}
	return ps
}
