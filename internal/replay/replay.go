// Package replay runs the gate over a repository's history as it would have
// run at each commit (SPEC.md 11.3): for every commit of a revision range,
// oldest first along first parents, it checks the commit out into a
// temporary git worktree and runs engine.Check in-process against the
// commit's first parent as the git baseline, selecting the changed packages
// the way `astimate check --base <parent>` does. It writes one row per
// checked package to packages.jsonl, one row per commit to commits.jsonl and
// a run.json describing the run. A commit whose module does not load, whose
// check fails, or whose check selected a package that does not load, is
// recorded with its error and no package rows. A rerun over the same output
// directory resumes: commits already in commits.jsonl are skipped.
//
// The calibration command calibration/replay and `astimate dryrun` wrap
// it; calibration/replay/README.md documents the row schemas.
package replay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Baseline kinds a commit row records.
const (
	baselineParent = "parent"
	baselineEmpty  = "empty"
)

// Options say what to replay, under which configuration, and where to.
type Options struct {
	// Repo is the git repository to replay.
	Repo string
	// Dir is the module root relative to the repository's top level; empty
	// means the top level.
	Dir string
	// Range is the revision range, as git log takes it; empty means the
	// first-parent history of master, else main, else HEAD.
	Range string
	// Last, when positive, keeps only the newest Last commits of the range.
	Last int
	// Only, when set, replays only this commit of the range.
	Only string
	// Out is the output directory; it is created when missing and resumed
	// when it holds rows.
	Out string
	// Config is the configuration every commit is gated under, whatever
	// the commit's own astimate.yaml says.
	Config *config.Config
	// ConfigPath names Config's file in run.json; empty records
	// "embedded".
	ConfigPath string
	// ToolCommit is the astimate commit run.json records; empty means the
	// running binary's vcs.revision, else the commit checked out in the
	// working directory.
	ToolCommit string
	// Parallel is the number of commits replayed at a time; below 1 means
	// 1.
	Parallel int
	// Logger receives the replay's progress, EngineLogger the engine's own
	// logs; nil discards them.
	Logger, EngineLogger *slog.Logger
}

// Replayer replays the commits of one range into one output directory.
type Replayer struct {
	opts    Options
	repo    string
	cfg     *config.Config
	commits []commitInfo
	info    runInfo
	logger  *slog.Logger
	// engineLogger receives the engine's own logs.
	engineLogger *slog.Logger
}

// New resolves opts: the repository and the commits to replay. It fails
// when Config or Out is missing, the repository is not a git repository,
// or the range or Only names no commit.
func New(ctx context.Context, opts Options) (*Replayer, error) {
	if opts.Config == nil {
		return nil, errors.New("no configuration to replay with")
	}
	if opts.Out == "" {
		return nil, errors.New("no output directory to replay into")
	}
	if opts.Dir == "" {
		opts.Dir = "."
	}
	opts.Parallel = max(opts.Parallel, 1)
	repo, err := filepath.Abs(opts.Repo)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", opts.Repo, err)
	}
	if _, err := gitCmd(ctx, repo, "rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", repo, err)
	}
	if opts.Range == "" {
		opts.Range = defaultRange(ctx, repo)
	}
	commits, err := listCommits(ctx, repo, opts.Range, opts.Last)
	if err != nil {
		return nil, err
	}
	if opts.Only != "" {
		if commits, err = selectOnly(ctx, repo, commits, opts.Only); err != nil {
			return nil, err
		}
	}
	if opts.ToolCommit == "" {
		opts.ToolCommit = astimateCommit(ctx)
	}
	rp := &Replayer{
		opts: opts, repo: repo, cfg: opts.Config, commits: commits,
		logger: orDiscard(opts.Logger), engineLogger: orDiscard(opts.EngineLogger),
	}
	rp.info = newRunInfo(ctx, rp)
	return rp, nil
}

// orDiscard returns l, or a logger that discards everything when l is nil.
func orDiscard(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.DiscardHandler)
	}
	return l
}

// Commits returns the number of commits the replay selected, those an
// earlier run already recorded included.
func (rp *Replayer) Commits() int { return len(rp.commits) }

// selectOnly returns the commit of commits that only names.
func selectOnly(ctx context.Context, repo string, commits []commitInfo, only string) ([]commitInfo, error) {
	out, err := gitCmd(ctx, repo, "rev-parse", "--verify", "--quiet", only+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("--only %s: not a commit", only)
	}
	hash := strings.TrimSpace(out)
	for _, c := range commits {
		if c.hash == hash {
			return []commitInfo{c}, nil
		}
	}
	return nil, fmt.Errorf("--only %s: not a first-parent commit of the range", only)
}

// Run replays every commit the output does not record yet, in batches
// of Options.Parallel, and writes each batch's rows in range order, so the
// files' order does not depend on which commit finished first. It then
// rewrites run.json when it wrote anything, or when run.json is missing.
// An interrupted batch writes nothing and returns ctx's error.
func (rp *Replayer) Run(ctx context.Context) error {
	st, err := openStore(rp.opts.Out)
	if err != nil {
		return fmt.Errorf("resuming from %s: %w", rp.opts.Out, err)
	}
	for i := range st.rows {
		if v := st.rows[i].ConfigVersion; v != rp.cfg.Version {
			_ = st.close()
			return fmt.Errorf("%s holds rows gated under %s, not %s; use another --out for another config",
				rp.opts.Out, v, rp.cfg.Version)
		}
	}
	pending := make([]commitInfo, 0, len(rp.commits))
	for _, c := range rp.commits {
		if !st.done[c.hash] {
			pending = append(pending, c)
		}
	}
	rp.info.Totals.Skipped = len(rp.commits) - len(pending)
	rp.logger.Info("replaying", "range", rp.opts.Range, "commits", len(rp.commits),
		"pending", len(pending), "skipped", rp.info.Totals.Skipped, "out", rp.opts.Out)
	written, err := rp.replayPending(ctx, st, pending)
	rp.info.Totals.Written = written
	if _, statErr := os.Stat(filepath.Join(rp.opts.Out, runFile)); err == nil && (written > 0 || statErr != nil) {
		err = writeRunInfo(rp.opts.Out, &rp.info, st)
	} else if err == nil {
		rp.logger.Info("nothing new to replay", "out", rp.opts.Out)
	}
	return errors.Join(err, st.close())
}

// replayPending replays pending in batches and records each batch, and
// returns how many commits it recorded.
func (rp *Replayer) replayPending(ctx context.Context, st *store, pending []commitInfo) (int, error) {
	written := 0
	for start := 0; start < len(pending); start += rp.opts.Parallel {
		batch := pending[start:min(start+rp.opts.Parallel, len(pending))]
		results := make([]commitResult, len(batch))
		var wg sync.WaitGroup
		for i := range batch {
			wg.Go(func() { results[i] = rp.replayCommit(ctx, &batch[i]) })
		}
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return written, err
		}
		for i := range results {
			if err := results[i].setupErr; err != nil {
				return written, err
			}
			if err := st.record(results[i].rows, &results[i].commit); err != nil {
				return written, err
			}
			written++
			c := &results[i].commit
			rp.logger.Info("commit recorded", "n", start+i+1, "of", len(pending), "commit", c.Commit[:12],
				"loaded", c.Loaded, "packages", len(c.PackagesChanged), "violations", c.Violations,
				"wall", time.Duration(c.WallMS)*time.Millisecond)
		}
	}
	return written, nil
}

// commitResult is one replayed commit's rows.
type commitResult struct {
	rows   []packageRow
	commit commitRow
	// setupErr is a failure of the replay rather than of the commit: git
	// could not list the commit's files or check it out. Such a commit is
	// not recorded, so a rerun retries it.
	setupErr error
}

// replayCommit checks c out into a temporary worktree and checks it against
// its first parent. A commit whose module does not load, or whose baseline
// or check fails, is returned with Loaded false and the error, and no
// package rows.
func (rp *Replayer) replayCommit(ctx context.Context, c *commitInfo) (res commitResult) {
	start := time.Now()
	res.commit = newCommitRow(c, rp.cfg.Version)
	cr := &res.commit
	defer func() { cr.WallMS = time.Since(start).Milliseconds() }()
	fail := func(err error, tree string) commitResult {
		cr.Error = scrubPath(err.Error(), tree)
		rp.logger.Warn("commit not checked", "commit", c.hash[:12], "err", cr.Error)
		return res
	}
	files, err := filesChanged(ctx, rp.repo, c)
	var wt string
	remove := func() {}
	if err == nil {
		wt, remove, err = addWorktree(ctx, rp.repo, c.hash)
	}
	if err != nil {
		res.setupErr = fmt.Errorf("commit %s: %w", c.hash, err)
		return res
	}
	cr.FilesChanged = files
	defer remove()
	t, err := engine.LoadTarget(filepath.Join(wt, rp.opts.Dir), engine.TargetOptions{Config: rp.cfg, Logger: rp.engineLogger})
	if err != nil {
		return fail(err, wt)
	}
	opts := engine.CheckOptions{Base: c.parent, Now: c.committerDate}
	if c.parent == "" || !rp.parentHasModule(ctx, c.parent, t.Ext.Language()) {
		cr.Baseline = baselineEmpty
		opts.Base, opts.All = "", true
		if opts.BaselineFile, err = emptyBaseline(filepath.Dir(wt), t.Mod.ModulePath); err != nil {
			return fail(err, wt)
		}
	}
	check, failed, err := engine.Check(ctx, t, opts)
	if err != nil {
		return fail(err, wt)
	}
	if skipped := skippedErrs(failed); len(skipped) > 0 {
		// The commit has no rows, but its row still names the packages
		// that kept it from being checked.
		for _, err := range skipped {
			cr.PackagesFailed = append(cr.PackagesFailed, newPackageFailed(scrubError(err, wt)))
		}
		return fail(errors.Join(skipped...), wt)
	}
	for i := range failed {
		failed[i] = scrubError(failed[i], wt)
	}
	res.rows = checkRows(c.hash, check, failed, cr)
	cr.Loaded = true
	return res
}

// skippedErrs returns the errors of failed that are of a selected package
// the extractor skipped because it does not load (engine.IsSkipped). The
// commit's verdict would be incomplete if there are any, so it is recorded
// as not loaded, as a module that does not load is.
func skippedErrs(failed []error) []error {
	var skipped []error
	for _, err := range failed {
		if engine.IsSkipped(err) {
			skipped = append(skipped, err)
		}
	}
	return skipped
}

// scrubError returns err with the worktree's path scrubbed from its text.
// A *engine.PackageError stays one, with its package path and a scrubbed
// cause, so the commit row can still name the package that failed.
func scrubError(err error, tree string) error {
	var pe *engine.PackageError
	if errors.As(err, &pe) {
		return &engine.PackageError{Path: pe.Path, Err: errors.New(scrubPath(pe.Err.Error(), tree))}
	}
	return errors.New(scrubPath(err.Error(), tree))
}

// newCommitRow returns c's commit row before its check under the
// configuration version: every list empty, judged against its parent.
func newCommitRow(c *commitInfo, version string) commitRow {
	return commitRow{
		Commit: c.hash, Parent: c.parent, AuthorName: c.authorName, AuthorEmail: c.authorEmail,
		CommitterDate: c.committerDate.Format(time.RFC3339), Subject: c.subject,
		CoAuthoredBy: nonNil(coAuthors(c.trailers)), Trailers: c.trailers,
		FilesChanged: []string{}, ConfigVersion: version, Baseline: baselineParent,
		PackagesChanged: []string{}, PackagesDeleted: []string{}, PackagesFailed: []packageFailed{},
	}
}

// nonNil returns s, or an empty slice for nil, so a row lists [] not null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// moduleMarkers are the files that make a directory a module root, by
// language id.
func moduleMarkers() map[string]string {
	return map[string]string{"go": "go.mod", "typescript": "package.json"}
}

// parentHasModule reports whether the parent's tree holds the module root's
// marker file for language, so the parent can serve as the baseline; a
// parent without it, from before the module existed, cannot.
func (rp *Replayer) parentHasModule(ctx context.Context, parent, language string) bool {
	marker, ok := moduleMarkers()[language]
	if !ok {
		return true
	}
	return hasPath(ctx, rp.repo, parent, path.Join(filepath.ToSlash(rp.opts.Dir), marker))
}

// emptyBaseline writes, in dir, a baseline file with no packages and a zero
// module row, so that every package, and every cross-package block, is new.
// It returns the file's path.
func emptyBaseline(dir, modulePath string) (string, error) {
	file := filepath.Join(dir, "empty-baseline.json")
	zero := 0
	err := baseline.WriteContents(file, baseline.Contents{
		ModulePath: modulePath,
		Tokenizer:  engine.TokenizerEst,
		Packages:   map[string]metrics.RawMetrics{metrics.ModuleRowID: {DupBlocksCrossPkg: &zero}},
	})
	if err != nil {
		return "", fmt.Errorf("writing an empty baseline: %w", err)
	}
	return file, nil
}

// scrubPath replaces the temporary worktree's path in msg, in both its
// given and its symlink-resolved form, so error text is the same on every
// run.
func scrubPath(msg, tree string) string {
	if tree == "" {
		return msg
	}
	if resolved, err := filepath.EvalSymlinks(tree); err == nil && resolved != tree {
		msg = strings.ReplaceAll(msg, resolved, "<worktree>")
	}
	return strings.ReplaceAll(msg, tree, "<worktree>")
}
