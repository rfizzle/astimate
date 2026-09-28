package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
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
	// staged checks the tree the git index holds instead of the working
	// tree.
	staged bool
	// indexFile is the index a staged check reads (hookIndexFile); empty
	// means the repository's own index.
	indexFile string
	// format is the --format value, one of checkFormats.
	format string
	// coverage measures coverage_pct at head (--coverage).
	coverage engine.CoverageOptions
	// hookStdin returns the Claude Code hook input read in the hook format,
	// or nil when there is none; nil hookStdin means no input.
	hookStdin func() io.Reader
}

// maxHookInput bounds how much of the hook input check reads.
const maxHookInput = 64 << 10

// processHookStdin returns os.Stdin when it is not a terminal, and nil when
// it is one or cannot be inspected, so an interactive check never waits for
// input.
func processHookStdin() io.Reader {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	return os.Stdin
}

// stopHookActive reports whether the Claude Code Stop hook input in r has
// stop_hook_active set to true. A nil reader, a read error and missing or
// malformed input all report false, so the check runs as usual.
func stopHookActive(r io.Reader) bool {
	if r == nil {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(r, maxHookInput))
	if err != nil {
		return false
	}
	var in struct {
		StopHookActive bool `json:"stop_hook_active"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return false
	}
	return in.StopHookActive
}

// runCheck gates the packages of a module against a baseline and the
// configured thresholds: `check [<module-root>] [--base ref | --baseline
// file] [--all] [--staged] [--config|--thresholds file] [--format
// text|json|hook|github] [--tokenizer est|o200k] [--coverage]
// [--coverage-timeout d]`. It exits 3 when any
// package has a violation (0 with --format hook, whose JSON carries the
// decision), 2 when analysis failed, and 0 otherwise; warnings never change
// the exit code. In the hook format it reads the Stop hook input from stdin
// when stdin is not a terminal.
func runCheck(args []string, stdout, stderr io.Writer) int {
	return runCheckInput(args, processHookStdin, stdout, stderr)
}

// runCheckInput is runCheck with the hook input taken from hookStdin. When
// the format is hook and that input has stop_hook_active true, it prints
// {} and exits 0 without analysis: the hook already blocked once, and
// blocking again could keep the agent looping.
func runCheckInput(args []string, hookStdin func() io.Reader, stdout, stderr io.Writer) int {
	fs := newFlagSet("check", "usage: astimate check [<module-root>] [--base ref | --baseline file] [--all] [--staged] "+
		"[--config|--thresholds file] [--format text|json|hook|github] [--tokenizer est|o200k] "+
		"[--coverage] [--coverage-timeout 2m]", stderr)
	opts := checkOptions{hookStdin: hookStdin}
	var tf targetFlags
	tf.register(fs, true)
	fs.StringVar(&opts.base, "base", "", "compare against the merge-base of HEAD and this git ref "+
		"(default origin/master, then master, origin/main, main)")
	fs.StringVar(&opts.baselineFile, "baseline", "", "compare against this baseline file instead of a git ref")
	fs.BoolVar(&opts.all, "all", false, "check every package, not only the changed ones")
	fs.BoolVar(&opts.staged, "staged", false, "check the tree the git index holds, as a commit would record it, "+
		"instead of the working tree")
	fs.StringVar(&tf.config, "thresholds", "", "alias of --config")
	fs.StringVar(&opts.format, "format", formatText, "output format: "+strings.Join(checkFormats(), ", "))
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage // the flag package has printed the error and usage
	}
	dir, ok := moduleRoot("check", positional, fs, stderr)
	if !ok {
		return exitUsage
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
	if opts.coverage, ok = tf.validate("check", stderr); !ok {
		return exitUsage
	}

	logger := newLogger(stderr)
	if opts.format == formatHook && opts.hookStdin != nil && stopHookActive(opts.hookStdin()) {
		logger.Info("stop hook already blocked once; allowing the stop without a check")
		if _, err := io.WriteString(stdout, "{}\n"); err != nil {
			logger.Error("check failed", "err", fmt.Errorf("writing check output: %w", err))
			return exitAnalysis
		}
		return exitOK
	}
	if opts.staged {
		if opts.indexFile, err = hookIndexFile(); err != nil {
			logger.Error("check failed", "dir", dir, "err", err)
			return exitAnalysis
		}
	}
	t, err := tf.load(dir, logger)
	if err != nil {
		logger.Error("check failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	return checkTarget(context.Background(), t, opts, stdout, stderr, logger)
}

// checkTarget runs the gate on t's module with engine.Check and renders the
// result to stdout in opts.format. A package that fails to extract is
// logged and skipped; the rest are still rendered.
func checkTarget(ctx context.Context, t *engine.Target, opts checkOptions, stdout, stderr io.Writer,
	logger *slog.Logger,
) int {
	c, failed, err := engine.Check(ctx, t, engine.CheckOptions{
		Base:         opts.base,
		BaselineFile: opts.baselineFile,
		All:          opts.all,
		Staged:       opts.staged,
		IndexFile:    opts.indexFile,
		Coverage:     opts.coverage,
	})
	if errors.Is(err, engine.ErrNoBaseline) {
		err = fmt.Errorf("%w; pass --base <ref> or --baseline <file>", err)
	}
	if err != nil {
		logger.Error("check failed", "root", t.Mod.Root, "err", err)
		return exitAnalysis
	}

	if err := renderCheck(c, opts.format, stdout, stderr); err != nil {
		logger.Error("check failed", "root", t.Mod.Root, "err", err)
		return exitAnalysis
	}
	if len(failed) > 0 {
		logger.Error("check incomplete", "failed_packages", len(failed))
	}
	return checkExitCode(opts.format, c.Failed(), len(failed))
}

// hookIndexFile returns the index a --staged check reads: the absolute form
// of GIT_INDEX_FILE, which git sets for its hooks, relative to the working
// directory it runs them in, and which names a temporary index under `git
// commit -a` and `git commit <paths>`; empty, meaning the repository's own
// index, when it is unset.
func hookIndexFile() (string, error) {
	idx := os.Getenv("GIT_INDEX_FILE")
	if idx == "" {
		return "", nil
	}
	abs, err := filepath.Abs(idx)
	if err != nil {
		return "", fmt.Errorf("resolving GIT_INDEX_FILE %s: %w", idx, err)
	}
	return abs, nil
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
