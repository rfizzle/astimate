package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/rfizzle/astimate/internal/engine"
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
// file: `baseline write [<module-root>] [--out path] [--config path]
// [--tokenizer est|o200k]`. Without --out the file goes to
// .astimate/baseline.json under the module root. The file's directory is
// created if missing, and the file records the tokenizer.
func runBaselineWrite(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("astimate baseline write", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "path of the baseline file (default <module-root>/"+engine.DefaultBaselinePath+")")
	configPath := fs.String("config", "", "configuration file (default ./astimate.yaml, then the embedded default)")
	tokenizer := fs.String("tokenizer", tokenizerEst, "token counting method: est or o200k")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: astimate baseline write [<module-root>] [--out path] [--config path] [--tokenizer est|o200k]")
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
	if !validTokenizer(*tokenizer) {
		_, _ = fmt.Fprintf(stderr, "astimate: baseline write: unknown tokenizer %q: want %s or %s\n",
			*tokenizer, tokenizerEst, tokenizerO200k)
		return exitUsage
	}
	dir := "."
	if len(positional) == 1 {
		dir = positional[0]
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	path, n, err := writeBaseline(context.Background(), dir, *out, *configPath, *tokenizer, logger)
	if err != nil {
		logger.Error("baseline write failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s (%d packages)\n", path, n)
	return exitOK
}

// writeBaseline resolves dir with tokenizer and writes its module's baseline
// file with engine.WriteBaseline. It returns the path written and the number
// of packages.
func writeBaseline(ctx context.Context, dir, out, configPath, tokenizer string, logger *slog.Logger) (string, int, error) {
	// The file records the tokenizer, so check warns when its own differs.
	t, err := loadTarget(dir, configPath, tokenizer, logger)
	if err != nil {
		return "", 0, err
	}
	return engine.WriteBaseline(ctx, t, out)
}
