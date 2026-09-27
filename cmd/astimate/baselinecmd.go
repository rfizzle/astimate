package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/baseline"
)

const baselineUsage = `usage: astimate baseline <subcommand> [arguments]

Subcommands:
  write  write a baseline file for the module
`

// baselineCommands is the dispatch table for `astimate baseline <subcommand>`.
func baselineCommands() map[string]command {
	return map[string]command{
		"write": runBaselineWrite,
	}
}

// runBaseline dispatches `astimate baseline` to its subcommands.
func runBaseline(args []string, stdout, stderr io.Writer) int {
	return dispatch(baselineCommands(), baselineUsage, args, stdout, stderr)
}

// runBaselineWrite extracts every package of the module containing the
// optional <module-root> argument (default ".") and writes them as a baseline
// file: `baseline write [<module-root>] [--out path] [--config path]`.
// Without --out the file goes to .astimate/baseline.json under the module
// root. The file's directory is created if missing.
func runBaselineWrite(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("astimate baseline write", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "path of the baseline file (default <module-root>/"+baseline.DefaultPath+")")
	configPath := fs.String("config", "", "configuration file (default ./astimate.yaml, then the embedded default)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: astimate baseline write [<module-root>] [--out path] [--config path]")
		fs.PrintDefaults()
	}
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage // the flag package has printed the error and usage
	}
	if len(positional) > 1 {
		_, _ = fmt.Fprintf(stderr, "astimate: baseline write: want at most one module root, got %d arguments\n", len(positional))
		fs.Usage()
		return exitUsage
	}
	dir := "."
	if len(positional) == 1 {
		dir = positional[0]
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	path, n, err := writeBaseline(context.Background(), dir, *out, *configPath)
	if err != nil {
		logger.Error("baseline write failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s (%d packages)\n", path, n)
	return exitOK
}

// writeBaseline extracts the module containing dir and writes its baseline
// file to out, or to the default path under the module root when out is
// empty. It returns the path written and the number of packages.
func writeBaseline(ctx context.Context, dir, out, configPath string) (string, int, error) {
	// The baseline must be comparable with the metrics check computes, which
	// always use the default tokenizer.
	t, err := loadTarget(dir, targetFlags{configPath: configPath, tokenizer: tokenizerEst})
	if err != nil {
		return "", 0, err
	}
	pkgs, err := baseline.Collect(ctx, t.extractor, t.module)
	if err != nil {
		return "", 0, err
	}
	// Outside git, or before the first commit, the file records no ref.
	ref, _ := baseline.HeadCommit(ctx, t.module.Root)

	path := out
	if path == "" {
		path = filepath.Join(t.module.Root, baseline.DefaultPath)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", 0, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := baseline.Write(path, ref, t.module.ModulePath, pkgs); err != nil {
		return "", 0, err
	}
	return path, len(pkgs), nil
}
