package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gocache"
	"github.com/rfizzle/astimate/internal/replay"
	"github.com/rfizzle/astimate/internal/replay/summary"
)

// dryrunFlags are the parsed flags of `astimate dryrun`.
type dryrunFlags struct {
	last     int
	budget   float64
	suggest  string
	out      string
	asJSON   bool
	timeout  time.Duration
	parallel int
	config   string
}

// Defaults of the dryrun flags.
const (
	defaultDryrunLast    = 50
	defaultDryrunBudget  = 10.0
	defaultDryrunTimeout = 30 * time.Minute
)

// runDryrun replays the last commits of the module's repository with the
// gate and reports how often it would have failed: `dryrun [<module-root>]
// [--last N] [--budget P] [--suggest file] [--json] [--timeout d]
// [--parallel N] [--config path] [--out dir]`. It exits 0 whatever the gate would have
// said, 1 on a usage error, and 2 when the replay itself fails or stops at
// --timeout or on an interrupt, after printing what it replayed.
func runDryrun(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dryrun", "usage: astimate dryrun [<module-root>] [--last N] [--budget P] [--suggest file] "+
		"[--json] [--timeout 30m] [--parallel N] [--config path] [--out dir]", stderr)
	f := dryrunFlags{last: defaultDryrunLast, budget: defaultDryrunBudget, timeout: defaultDryrunTimeout, parallel: 1}
	f.register(fs)
	dir, ok := parseModuleArgs("dryrun", fs, args, stderr)
	if !ok {
		return exitUsage
	}
	if err := f.validate(); err != nil {
		_, _ = fmt.Fprintf(stderr, "astimate: dryrun: %v\n", err)
		return exitUsage
	}
	// An interrupt cancels the replay, which removes its worktrees from the
	// repository before the command exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return dryrun(ctx, dir, &f, stdout, stderr)
}

// register adds the flags to fs, each defaulting to f's value.
func (f *dryrunFlags) register(fs *flag.FlagSet) {
	fs.IntVar(&f.last, "last", f.last, "replay the last N first-parent commits of HEAD")
	fs.Float64Var(&f.budget, "budget", f.budget, "percent of the commits checked a rule may fail "+
		"before --suggest sets it to warn")
	fs.StringVar(&f.suggest, "suggest", f.suggest, "write the configuration with severity warn "+
		"on every rule over --budget "+"to this file")
	fs.BoolVar(&f.asJSON, "json", f.asJSON, "print the replay's package and commit rows as JSON lines")
	fs.DurationVar(&f.timeout, "timeout", f.timeout, "stop replaying after this long "+
		"(0 for no limit)"+"; the summary covers the commits replayed before it")
	fs.IntVar(&f.parallel, "parallel", f.parallel, "commits replayed at a time, "+"each in its own worktree")
	fs.StringVar(&f.config, "config", f.config, "configuration file (default ./astimate.yaml, then the embedded default)")
	fs.StringVar(&f.out, "out", f.out, "keep the replay's rows (commits.jsonl, packages.jsonl, run.json) in this "+
		"new or empty directory, for the calibration tools to read")
}

// validate reports a flag value dryrun cannot run with.
func (f *dryrunFlags) validate() error {
	switch {
	case f.last < 1:
		return fmt.Errorf("--last must be at least 1, got %d", f.last)
	case f.parallel < 1:
		return fmt.Errorf("--parallel must be at least 1, got %d", f.parallel)
	case f.budget < 0 || f.budget > 100:
		return fmt.Errorf("--budget must be a percentage from 0 to 100, got %g", f.budget)
	case f.timeout < 0:
		return fmt.Errorf("--timeout must not be negative, got %s", f.timeout)
	}
	if f.out != "" {
		entries, err := os.ReadDir(f.out)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("--out %s: %w", f.out, err)
		}
		if len(entries) > 0 {
			return fmt.Errorf("--out %s must be a new or empty directory, it holds %d entries", f.out, len(entries))
		}
	}
	return nil
}

// dryrun replays the last commits of dir's repository into a temporary
// directory under the resolved configuration, then prints the summary or
// the rows and writes the suggestion.
func dryrun(ctx context.Context, dir string, f *dryrunFlags, stdout, stderr io.Writer) int {
	logger := newLogger(stderr)
	fail := func(msg string, err error) int {
		logger.Error(msg, "err", err)
		return exitAnalysis
	}
	cfg, src, configPath, err := resolveConfigSource(f.config)
	if err != nil {
		return fail("dryrun failed", err)
	}
	repo, rel, err := replay.Locate(ctx, dir)
	if err != nil {
		return fail("dryrun failed", err)
	}
	out, cleanOut, err := replayDir(f.out)
	if err != nil {
		return fail("dryrun failed", err)
	}
	defer cleanOut()
	cleanCache, err := gocache.Isolate("astimate-dryrun-gocache-")
	if err != nil {
		return fail("dryrun failed", err)
	}
	defer cleanCache()
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}
	start := time.Now()
	rp, err := replay.New(ctx, replay.Options{
		Repo: repo, Dir: rel, Range: "HEAD", Last: f.last, Out: out,
		Config: cfg, ConfigPath: configPath, ToolCommit: "astimate " + astimateVersion(), Parallel: f.parallel,
		Logger:       logger,
		EngineLogger: slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		return fail("dryrun failed", err)
	}
	code := exitOK
	if err := rp.Run(ctx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return fail("dryrun failed", err)
		}
		logger.Error("dryrun stopped early; the summary covers the commits replayed before it", "timeout", f.timeout, "err", err)
		code = exitAnalysis
	}
	s, err := summary.Summarize(out)
	if err != nil {
		return fail("dryrun failed", err)
	}
	if f.out != "" {
		logger.Info("kept the replay's rows", "dir", out)
	}
	err = writeBuffered(stdout, func(w io.Writer) error {
		if f.asJSON {
			return summary.WriteRows(w, out)
		}
		return writeDryrun(w, s, dryrunHeader{Selected: rp.Commits(), Budget: f.budget / 100, Wall: time.Since(start)})
	})
	if err != nil {
		return fail("dryrun failed", err)
	}
	if f.suggest != "" {
		warn := s.Warn(f.budget / 100)
		data, err := summary.Suggest(src, warn)
		if err == nil {
			err = os.WriteFile(f.suggest, data, 0o644)
		}
		if err != nil {
			return fail("writing the suggestion failed", err)
		}
		logger.Info("wrote the suggested configuration", "file", f.suggest, "warn", strings.Join(warn, ","))
	}
	return code
}

// replayDir returns the directory the replay writes into and the function
// that disposes of it: out, created when missing and kept, or when out is
// empty a new temporary directory, removed.
func replayDir(out string) (string, func(), error) {
	if out != "" {
		return out, func() {}, os.MkdirAll(out, 0o755)
	}
	dir, err := os.MkdirTemp("", "astimate-dryrun-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// resolveConfigSource resolves the configuration as every command does
// (config.Resolve) and returns it with its file's contents, which a
// suggestion edits, and its path for the replay's records, empty for the
// embedded default.
func resolveConfigSource(flagPath string) (*config.Config, []byte, string, error) {
	cfg, source, err := config.Resolve(flagPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolving config: %w", err)
	}
	path := ""
	switch source {
	case config.SourceFlag:
		path = flagPath
	case config.SourceWorkDir:
		path = config.FileName
	default:
		return cfg, config.Default(), "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("reading config: %w", err)
	}
	return cfg, data, path, nil
}

// dryrunHeader is what the summary says about the run itself.
type dryrunHeader struct {
	// Selected is the number of commits the replay selected.
	Selected int
	// Budget is --budget as a share.
	Budget float64
	// Wall is the run's wall time.
	Wall time.Duration
}

// writeDryrun renders s as text: the failed share of the commits checked
// and of those that touched a package, a per-rule table, each rule's share
// of the commits checked, marking the rules over the budget, the
// most-failed packages and the commits that could not be checked.
func writeDryrun(w io.Writer, s *summary.Summary, h dryrunHeader) error {
	var b strings.Builder
	fmt.Fprintf(&b, "replayed %d of %d commits in %s: %d checked, %d not loaded, %d touched a package\n",
		s.Commits, h.Selected, h.Wall.Round(time.Second), s.Checked, len(s.NotLoaded), s.Touched)
	fmt.Fprintf(&b, "would have failed %d of %d commits checked (%s), %d of the %d that touched a package (%s)\n",
		s.Failed, s.Checked, pct(s.Share(s.Failed)), s.Failed, s.Touched, pct(s.TouchedShare(s.Failed)))
	if len(s.Rules) > 0 {
		width := len("rule")
		for _, r := range s.Rules {
			width = max(width, len(r.Metric))
		}
		fmt.Fprintf(&b, "\n%-*s  %6s  %6s  %5s\n", width, "rule", "failed", "share", "alone")
		for _, r := range s.Rules {
			mark := ""
			if s.Share(r.Failed) > h.Budget {
				mark = "  over budget"
			}
			fmt.Fprintf(&b, "%-*s  %6d  %6s  %5d%s\n", width, r.Metric, r.Failed, pct(s.Share(r.Failed)), r.Alone, mark)
		}
		fmt.Fprintf(&b, "budget %s of the commits checked: --suggest sets severity warn on the rules over it\n", pct(h.Budget))
	}
	if len(s.Packages) > 0 {
		b.WriteString("\nmost-failed packages\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, p := range s.Packages {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\n", p.Package, commitCount(p.Failed))
		}
		_ = tw.Flush()
	}
	if len(s.NotLoaded) > 0 {
		b.WriteString("\nnot loaded\n")
		for _, c := range s.NotLoaded {
			fmt.Fprintf(&b, "  %s %s: %s\n", shortHash(c.Commit), c.Subject, strings.Join(strings.Fields(c.Error), " "))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// pct formats a share as a percentage with one decimal.
func pct(share float64) string {
	return strconv.FormatFloat(100*share, 'f', 1, 64) + "%"
}

// commitCount formats n commits, "1 commit" or "n commits".
func commitCount(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return strconv.Itoa(n) + " commits"
}

// shortHash abbreviates a commit hash to 12 characters.
func shortHash(h string) string {
	return h[:min(len(h), 12)]
}
