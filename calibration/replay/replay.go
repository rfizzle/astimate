package main

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

// replayer replays the commits of one range into one output directory.
type replayer struct {
	opts    options
	repo    string
	cfg     *config.Config
	commits []commitInfo
	info    runInfo
	logger  *slog.Logger
	// engineLogger receives the engine's own logs.
	engineLogger *slog.Logger
}

// newReplayer resolves opts: the repository, the configuration, the output
// directory and the commits to replay. logger receives the replay's
// progress, engineLogger the engine's own logs.
func newReplayer(ctx context.Context, opts options, logger, engineLogger *slog.Logger) (*replayer, error) {
	repo, err := filepath.Abs(opts.repo)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", opts.repo, err)
	}
	if _, err := gitCmd(ctx, repo, "rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", repo, err)
	}
	cfg, err := loadConfig(opts.configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if err := setGoEnv(); err != nil {
		return nil, err
	}
	if opts.revRange == "" {
		opts.revRange = defaultRange(ctx, repo)
	}
	commits, err := listCommits(ctx, repo, opts.revRange)
	if err != nil {
		return nil, err
	}
	if opts.only != "" {
		if commits, err = selectOnly(ctx, repo, commits, opts.only); err != nil {
			return nil, err
		}
	}
	if opts.out == "" {
		opts.out = defaultOut(repoName(ctx, repo), time.Now())
	}
	rp := &replayer{
		opts: opts, repo: repo, cfg: cfg, commits: commits, logger: logger, engineLogger: engineLogger,
	}
	rp.info = newRunInfo(ctx, rp)
	return rp, nil
}

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

// replay replays every commit the output does not record yet, in batches
// of opts.parallel, and writes each batch's rows in range order, so the
// files' order does not depend on which commit finished first. It then
// rewrites run.json when it wrote anything, or when run.json is missing.
// An interrupted batch writes nothing and returns ctx's error.
func (rp *replayer) replay(ctx context.Context) error {
	st, err := openStore(rp.opts.out)
	if err != nil {
		return fmt.Errorf("resuming from %s: %w", rp.opts.out, err)
	}
	for i := range st.rows {
		if v := st.rows[i].ConfigVersion; v != rp.cfg.Version {
			_ = st.close()
			return fmt.Errorf("%s holds rows gated under %s, not %s; use another --out for another config",
				rp.opts.out, v, rp.cfg.Version)
		}
	}
	pending := make([]commitInfo, 0, len(rp.commits))
	for _, c := range rp.commits {
		if !st.done[c.hash] {
			pending = append(pending, c)
		}
	}
	rp.info.Totals.Skipped = len(rp.commits) - len(pending)
	rp.logger.Info("replaying", "range", rp.opts.revRange, "commits", len(rp.commits),
		"pending", len(pending), "skipped", rp.info.Totals.Skipped, "out", rp.opts.out)
	written, err := rp.replayPending(ctx, st, pending)
	rp.info.Totals.Written = written
	if _, statErr := os.Stat(filepath.Join(rp.opts.out, runFile)); err == nil && (written > 0 || statErr != nil) {
		err = writeRunInfo(rp.opts.out, &rp.info, st)
	} else if err == nil {
		rp.logger.Info("nothing new to replay", "out", rp.opts.out)
	}
	return errors.Join(err, st.close())
}

// replayPending replays pending in batches and records each batch, and
// returns how many commits it recorded.
func (rp *replayer) replayPending(ctx context.Context, st *store, pending []commitInfo) (int, error) {
	written := 0
	for start := 0; start < len(pending); start += rp.opts.parallel {
		batch := pending[start:min(start+rp.opts.parallel, len(pending))]
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
func (rp *replayer) replayCommit(ctx context.Context, c *commitInfo) (res commitResult) {
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
	t, err := engine.LoadTarget(filepath.Join(wt, rp.opts.dir), engine.TargetOptions{Config: rp.cfg, Logger: rp.engineLogger})
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
	for i := range failed {
		failed[i] = errors.New(scrubPath(failed[i].Error(), wt))
	}
	res.rows = checkRows(c.hash, check, failed, cr)
	cr.Loaded = true
	return res
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
func (rp *replayer) parentHasModule(ctx context.Context, parent, language string) bool {
	marker, ok := moduleMarkers()[language]
	if !ok {
		return true
	}
	return hasPath(ctx, rp.repo, parent, path.Join(filepath.ToSlash(rp.opts.dir), marker))
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
