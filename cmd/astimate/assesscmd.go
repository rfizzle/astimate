package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// runAssess scores one package: `assess <package-dir> [--json] [--config
// path] [--tokenizer est|o200k]`. It prints the table report, or the SPEC.md
// 10.2 JSON report with --json, to stdout. On failure stdout stays empty and
// the error is logged to stderr.
func runAssess(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("astimate assess", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the JSON report instead of the table")
	configPath := fs.String("config", "", "configuration file (default ./astimate.yaml, then the embedded default)")
	tokenizer := fs.String("tokenizer", tokenizerEst, "token counting method: est or o200k")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: astimate assess <package-dir> [--json] [--config path] [--tokenizer est|o200k]")
		fs.PrintDefaults()
	}
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage // the flag package has printed the error and usage
	}
	if len(positional) != 1 {
		_, _ = fmt.Fprintf(stderr, "astimate: assess: want one package directory, got %d arguments\n", len(positional))
		fs.Usage()
		return exitUsage
	}
	dir := positional[0]
	if !validTokenizer(*tokenizer) {
		_, _ = fmt.Fprintf(stderr, "astimate: assess: unknown tokenizer %q: want %s or %s\n",
			*tokenizer, tokenizerEst, tokenizerO200k)
		return exitUsage
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	r, err := assess(context.Background(), dir, *configPath, *tokenizer, logger)
	if err != nil {
		logger.Error("assess failed", "dir", dir, "err", err)
		return exitAnalysis
	}

	// Render into a buffer so a write error cannot leave partial output.
	var buf bytes.Buffer
	if *asJSON {
		err = report.WriteJSON(&buf, r)
	} else {
		err = report.WriteTable(&buf, r)
	}
	if err != nil {
		logger.Error("assess failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	if _, err := stdout.Write(buf.Bytes()); err != nil {
		logger.Error("assess failed", "dir", dir, "err", err)
		return exitAnalysis
	}
	return exitOK
}

// parseInterspersed parses args with fs, accepting flags before and after
// positional arguments, and returns the positional arguments in order.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// assess resolves dir and builds its report with engine.Assess.
func assess(ctx context.Context, dir, configPath, tokenizer string, logger *slog.Logger) (*report.Report, error) {
	t, err := loadTarget(dir, configPath, tokenizer, logger)
	if err != nil {
		return nil, err
	}
	return engine.Assess(ctx, t)
}

// astimateVersion is the version `astimate version` prints: the linked
// buildVersion, else the module version from the runtime build info.
func astimateVersion() string {
	info, ok := debug.ReadBuildInfo()
	return resolveBuildMeta(buildMeta{buildVersion, buildCommit, buildDate}, info, ok).version
}
