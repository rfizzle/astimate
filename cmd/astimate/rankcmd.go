package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/cli"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report/rows"
)

// runRank ranks every package of a module: `rank [<module-root>] [--json]
// [--top N] [--sort passes|days|fan_in|tokens|duplication|churn|hotspot] [--config path]
// [--tokenizer est|o200k] [--coverage] [--coverage-timeout d]`. The root
// defaults to the current directory. A
// package that fails to extract is logged to stderr and left out; the other
// rows are still printed and the command exits 2.
func runRank(args []string, stdout, stderr io.Writer) int {
	fs := cli.NewFlagSet("rank", "usage: astimate rank [<module-root>] [--json] [--top N] "+
		"[--sort passes|days|fan_in|tokens|duplication|churn|hotspot] [--config path] [--tokenizer est|o200k] "+
		"[--coverage] [--coverage-timeout 2m]", stderr)
	asJSON := fs.Bool("json", false, "print a JSON array instead of the table")
	top := fs.Int("top", 0, "print only the first N rows (0 prints all)")
	sortKey := fs.String("sort", rows.SortPasses, "sort key: "+strings.Join(rows.SortKeys(), ", "))
	var tf targetFlags
	tf.register(fs, true)
	dir, ok := cli.ParseModuleArgs("rank", fs, args, stderr)
	if !ok {
		return exitUsage
	}
	if !slices.Contains(rows.SortKeys(), *sortKey) {
		_, _ = fmt.Fprintf(stderr, "astimate: rank: unknown sort key %q: want one of %s\n",
			*sortKey, strings.Join(rows.SortKeys(), ", "))
		return exitUsage
	}
	if *top < 0 {
		_, _ = fmt.Fprintf(stderr, "astimate: rank: --top must not be negative, got %d\n", *top)
		return exitUsage
	}
	coverage, ok := tf.validate("rank", stderr)
	if !ok {
		return exitUsage
	}

	logger := cli.NewLogger(stderr)
	t, err := tf.load(dir, logger)
	if err != nil {
		logger.Error("rank failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	opts := engine.RankOptions{Sort: *sortKey, Top: *top, Coverage: coverage}
	return rankTarget(context.Background(), t, opts, *asJSON, stdout, logger)
}

// rankFooter closes the text table: it says which column is measured, %s the model.
const rankFooter = "DAYS: one engineer's working days by %s, not measured (SPEC.md 7.3). PASSES: measured on agent rebuilds.\n"

// rankTarget ranks t's module with engine.Rank and prints the rows to
// stdout, as a JSON array when asJSON is set and a table otherwise. It
// returns exitAnalysis when listing fails (stdout stays empty) or when any
// package failed to extract (the other rows are printed), else exitOK.
func rankTarget(ctx context.Context, t *engine.Target, opts engine.RankOptions, asJSON bool,
	stdout io.Writer, logger *slog.Logger,
) int {
	ranked, failed, err := engine.Rank(ctx, t, opts)
	if errors.Is(err, rows.ErrUnknownSortKey) {
		logger.Error("rank failed", "err", err)
		return exitUsage
	}
	if err != nil {
		logger.Error("rank failed", "root", t.Mod.Root, "err", err)
		return exitAnalysis
	}

	return emit("rank", t, failed, stdout, logger, func(w io.Writer) error {
		if asJSON {
			return rows.WriteRowsJSON(w, ranked)
		}
		if err := rows.WriteRowsTable(w, ranked); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, rankFooter, t.Cfg.ForLanguage(t.Ext.Language()).Rebuild.Cocomo.Name())
		return err
	})
}

// emit writes what render renders to stdout (cli.WriteBuffered) and returns
// the exit code of the module-wide command name on t: exitAnalysis when
// writing fails or any package failed, each logged, else exitOK.
func emit(name string, t *engine.Target, failed []error, stdout io.Writer, logger *slog.Logger,
	render func(io.Writer) error,
) int {
	if err := cli.WriteBuffered(stdout, render); err != nil {
		logger.Error(name+" failed", "root", t.Mod.Root, "err", err)
		return exitAnalysis
	}
	if len(failed) > 0 {
		logger.Error(name+" incomplete", "failed_packages", len(failed))
		return exitAnalysis
	}
	return exitOK
}
