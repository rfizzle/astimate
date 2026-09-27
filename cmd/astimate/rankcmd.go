package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// runRank ranks every package of a module: `rank [<module-root>] [--json]
// [--top N] [--sort passes|days|fan_in|tokens|duplication] [--config path]
// [--tokenizer est|o200k]`. The root defaults to the current directory. A
// package that fails to extract is logged to stderr and left out; the other
// rows are still printed and the command exits 2.
func runRank(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("astimate rank", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print a JSON array instead of the table")
	top := fs.Int("top", 0, "print only the first N rows (0 prints all)")
	sortKey := fs.String("sort", report.SortPasses, "sort key: "+strings.Join(report.SortKeys(), ", "))
	configPath := fs.String("config", "", "configuration file (default ./astimate.yaml, then the embedded default)")
	tokenizer := fs.String("tokenizer", tokenizerEst, "token counting method: est or o200k")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: astimate rank [<module-root>] [--json] [--top N] "+
			"[--sort passes|days|fan_in|tokens|duplication] [--config path] [--tokenizer est|o200k]")
		fs.PrintDefaults()
	}
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage // the flag package has printed the error and usage
	}
	if len(positional) > 1 {
		_, _ = fmt.Fprintf(stderr, "astimate: rank: want at most one module root, got %d arguments\n", len(positional))
		fs.Usage()
		return exitUsage
	}
	dir := "."
	if len(positional) == 1 {
		dir = positional[0]
	}
	if !slices.Contains(report.SortKeys(), *sortKey) {
		_, _ = fmt.Fprintf(stderr, "astimate: rank: unknown sort key %q: want one of %s\n",
			*sortKey, strings.Join(report.SortKeys(), ", "))
		return exitUsage
	}
	if *top < 0 {
		_, _ = fmt.Fprintf(stderr, "astimate: rank: --top must not be negative, got %d\n", *top)
		return exitUsage
	}
	if !validTokenizer(*tokenizer) {
		_, _ = fmt.Fprintf(stderr, "astimate: rank: unknown tokenizer %q: want %s or %s\n",
			*tokenizer, tokenizerEst, tokenizerO200k)
		return exitUsage
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	t, err := loadTarget(dir, *configPath, *tokenizer, logger)
	if err != nil {
		logger.Error("rank failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	opts := engine.RankOptions{Sort: *sortKey, Top: *top}
	return rankTarget(context.Background(), t, opts, *asJSON, stdout, logger)
}

// rankTarget ranks t's module with engine.Rank and prints the rows to
// stdout, as a JSON array when asJSON is set and a table otherwise. It
// returns exitAnalysis when listing fails (stdout stays empty) or when any
// package failed to extract (the other rows are printed), else exitOK.
func rankTarget(ctx context.Context, t *engine.Target, opts engine.RankOptions, asJSON bool,
	stdout io.Writer, logger *slog.Logger,
) int {
	rows, failed, err := engine.Rank(ctx, t, opts)
	if errors.Is(err, report.ErrUnknownSortKey) {
		logger.Error("rank failed", "err", err)
		return exitUsage
	}
	if err != nil {
		logger.Error("rank failed", "root", t.Mod.Root, "err", err)
		return exitAnalysis
	}

	// Render into a buffer so a write error cannot leave partial output.
	var buf bytes.Buffer
	if asJSON {
		err = report.WriteRowsJSON(&buf, rows)
	} else {
		err = report.WriteRowsTable(&buf, rows)
	}
	if err == nil {
		_, err = stdout.Write(buf.Bytes())
	}
	if err != nil {
		logger.Error("rank failed", "root", t.Mod.Root, "err", err)
		return exitAnalysis
	}
	if len(failed) > 0 {
		logger.Error("rank incomplete", "failed_packages", len(failed))
		return exitAnalysis
	}
	return exitOK
}
