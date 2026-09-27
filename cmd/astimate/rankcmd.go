package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// rankOptions are the rank flags that shape the output.
type rankOptions struct {
	// sortKey is the --sort value, one of report.SortKeys.
	sortKey string
	// top is the --top value; 0 keeps every row.
	top int
	// asJSON selects the JSON array instead of the table.
	asJSON bool
}

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
	t, err := loadTarget(dir, targetFlags{configPath: *configPath, tokenizer: *tokenizer})
	if err != nil {
		logger.Error("rank failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	opts := rankOptions{sortKey: *sortKey, top: *top, asJSON: *asJSON}
	return rankModule(context.Background(), t.extractor, t.module, t.cfg.Rebuild, opts, stdout, logger)
}

// rankModule lists the packages of mod with one Packages call, extracts and
// estimates each, then sorts, truncates and prints the rows to stdout. It
// returns exitAnalysis when listing fails (stdout stays empty) or when any
// package failed to extract (the other rows are printed), else exitOK.
func rankModule(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext,
	params score.RebuildParams, opts rankOptions, stdout io.Writer, logger *slog.Logger,
) int {
	pkgs, err := ext.Packages(mod.Root)
	if err != nil {
		logger.Error("rank failed", "root", mod.Root, "err", err)
		return exitAnalysis
	}
	rows := make([]report.Row, 0, len(pkgs))
	failed := 0
	for _, pkg := range pkgs {
		path := modulePathRel(mod.ModulePath, pkg)
		m, err := ext.Extract(ctx, mod, pkg)
		if err != nil {
			logger.Error("extracting package failed", "path", path, "err", err)
			failed++
			continue
		}
		rows = append(rows, report.NewRow(path, &m, params))
	}
	if err := report.SortRows(rows, opts.sortKey); err != nil {
		logger.Error("rank failed", "err", err)
		return exitUsage
	}
	if opts.top > 0 && opts.top < len(rows) {
		rows = rows[:opts.top]
	}

	// Render into a buffer so a write error cannot leave partial output.
	var buf bytes.Buffer
	if opts.asJSON {
		err = report.WriteRowsJSON(&buf, rows)
	} else {
		err = report.WriteRowsTable(&buf, rows)
	}
	if err == nil {
		_, err = stdout.Write(buf.Bytes())
	}
	if err != nil {
		logger.Error("rank failed", "root", mod.Root, "err", err)
		return exitAnalysis
	}
	if failed > 0 {
		logger.Error("rank incomplete", "failed_packages", failed)
		return exitAnalysis
	}
	return exitOK
}

// modulePathRel returns the directory of the package with import path
// importPath relative to the root of module modPath, in slash form: "." for
// the root package, matching assess's package_path.
func modulePathRel(modPath, importPath string) string {
	if importPath == modPath {
		return "."
	}
	return strings.TrimPrefix(importPath, modPath+"/")
}
