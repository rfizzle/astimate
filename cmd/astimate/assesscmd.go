package main

import (
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
// path] [--tokenizer est|o200k] [--coverage] [--coverage-timeout d]`. It
// prints the table report, or the SPEC.md
// 10.2 JSON report with --json, to stdout. On failure stdout stays empty and
// the error is logged to stderr.
func runAssess(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("assess", "usage: astimate assess <package-dir> [--json] [--config path] [--tokenizer est|o200k] "+
		"[--coverage] [--coverage-timeout 2m]", stderr)
	asJSON := fs.Bool("json", false, "print the JSON report instead of the table")
	var tf targetFlags
	tf.register(fs, true)
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
	coverage, ok := tf.validate("assess", stderr)
	if !ok {
		return exitUsage
	}

	logger := newLogger(stderr)
	r, err := assess(context.Background(), dir, tf.config, tf.tokenizer, engine.AssessOptions{Coverage: coverage}, logger)
	if err == nil {
		err = writeBuffered(stdout, func(w io.Writer) error {
			if *asJSON {
				return report.WriteJSON(w, r)
			}
			return report.WriteTable(w, r)
		})
	}
	if err != nil {
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

// assess resolves dir and builds its report with engine.Assess under opts.
func assess(ctx context.Context, dir, configPath, tokenizer string, opts engine.AssessOptions, logger *slog.Logger) (*report.Report, error) {
	t, err := loadTarget(dir, configPath, tokenizer, logger)
	if err != nil {
		return nil, err
	}
	return engine.Assess(ctx, t, opts)
}

// astimateVersion is the version `astimate version` prints: the linked
// buildVersion, else the module version from the runtime build info.
func astimateVersion() string {
	info, ok := debug.ReadBuildInfo()
	return resolveBuildMeta(buildMeta{buildVersion, buildCommit, buildDate}, info, ok).version
}
