// Command replay runs the gate over a repository's history as it would have
// run at each commit (SPEC.md 11): for every commit of a revision range,
// oldest first along first parents, it checks the commit out into a
// temporary git worktree and runs engine.Check in-process against the
// commit's first parent as the git baseline, selecting the changed packages
// the way `astimate check --base <parent>` does. It writes one row per
// checked package to packages.jsonl, one row per commit to commits.jsonl and
// a run.json describing the run. A commit whose module does not load, or
// whose check fails, is recorded with its error and skipped. A rerun over
// the same output directory resumes: commits already in commits.jsonl are
// skipped.
//
// Usage, from the repository root:
//
//	go run ./calibration/replay --repo <path> [--range <rev-range>] [--dir <module dir>]
//	    [--config <file>] [--out <dir>] [--only <hash>] [--parallel N]
//
// See calibration/replay/README.md for the row schemas.
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rfizzle/astimate/internal/config"
)

// Exit codes: 0 when every selected commit is recorded, 1 when the run
// stopped early (interrupted, or a row could not be written; rerun to
// resume), 2 for a usage or setup error that stopped the run before
// replaying.
const (
	exitOK      = 0
	exitPartial = 1
	exitUsage   = 2
)

// options are the parsed command-line flags.
type options struct {
	repo       string
	dir        string
	revRange   string
	configPath string
	out        string
	only       string
	parallel   int
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stderr)
	stop()
	os.Exit(code)
}

// run parses args, replays the selected commits and returns the exit code.
func run(ctx context.Context, args []string, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return exitUsage
	}
	logger := newLogger(stderr, slog.LevelInfo)
	rp, err := newReplayer(ctx, opts, logger, newLogger(stderr, slog.LevelWarn))
	if err != nil {
		logger.Error("replay failed", "err", err)
		return exitUsage
	}
	if err := rp.replay(ctx); err != nil {
		logger.Error("replay stopped; rerun the same command to resume", "err", err)
		return exitPartial
	}
	return exitOK
}

// parseFlags parses the command line; the flag package reports errors on
// stderr.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	o := options{repo: ".", dir: ".", parallel: 1}
	usage := map[string]string{
		"repo":     "git repository to replay",
		"dir":      "module root relative to the repository's top level",
		"range":    "revision range, as git log takes it; default the whole first-parent history of master, main or HEAD",
		"config":   "astimate configuration file; default the embedded default",
		"out":      "output directory; default calibration/data/replay-<repo>-<date>",
		"only":     "replay only this commit of the range (full or abbreviated hash)",
		"parallel": "commits replayed at a time",
	}
	strs := map[string]*string{
		"repo": &o.repo, "dir": &o.dir, "range": &o.revRange, "config": &o.configPath, "out": &o.out, "only": &o.only,
	}
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	for name, p := range strs {
		fs.StringVar(p, name, *p, usage[name])
	}
	fs.IntVar(&o.parallel, "parallel", o.parallel, usage["parallel"])
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	err := o.validate(fs.Args())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "replay:", err)
	}
	return o, err
}

// validate reports a flag combination replay cannot run with; rest are the
// arguments left after the flags.
func (o *options) validate(rest []string) error {
	if len(rest) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(rest, " "))
	}
	if o.parallel < 1 {
		return errors.New("--parallel must be at least 1")
	}
	if strings.HasPrefix(o.revRange, "-") || strings.HasPrefix(o.only, "-") {
		return errors.New("--range and --only take revisions, not options")
	}
	return nil
}

// newLogger returns a text logger writing records at level and above to w.
func newLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// loadConfig returns the configuration at path, or the embedded default
// when path is empty.
func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		return config.Parse(config.Default())
	}
	return config.Load(path)
}

// defaultOut is the output directory for a repository whose name is name,
// replayed on day.
func defaultOut(name string, day time.Time) string {
	return filepath.Join("calibration", "data", "replay-"+name+"-"+day.Format(time.DateOnly))
}

// setGoEnv makes the go command see only the checked-out module, as the
// collector does for cloned modules: no go.work from outside it, and a
// go.sum that may be completed rather than failing the load. A value the
// caller already set is kept.
func setGoEnv() error {
	for _, kv := range [][2]string{{"GOWORK", "off"}, {"GOFLAGS", "-mod=mod"}} {
		if _, ok := os.LookupEnv(kv[0]); ok {
			continue
		}
		if err := os.Setenv(kv[0], kv[1]); err != nil {
			return fmt.Errorf("setting %s: %w", kv[0], err)
		}
	}
	return nil
}
