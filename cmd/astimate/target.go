package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/rfizzle/astimate/internal/engine"
)

// Tokenizer names accepted by --tokenizer (SPEC.md 6.1).
const (
	tokenizerEst   = engine.TokenizerEst
	tokenizerO200k = engine.TokenizerO200k
)

// validTokenizer reports whether name is a tokenizer --tokenizer accepts.
func validTokenizer(name string) bool {
	return engine.ValidTokenizer(name)
}

// coverageFlags are the --coverage and --coverage-timeout flags assess,
// rank and check share.
type coverageFlags struct {
	enabled bool
	timeout time.Duration
}

// register adds the coverage flags to fs.
func (c *coverageFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&c.enabled, "coverage", false,
		"run the tests with go test -cover to report coverage_pct and scale the estimate by it")
	fs.DurationVar(&c.timeout, "coverage-timeout", engine.DefaultCoverageTimeout,
		"bound on the one go test run --coverage makes")
}

// options returns the engine options the flags select, or an error when
// the timeout is not positive.
func (c *coverageFlags) options() (engine.CoverageOptions, error) {
	if c.timeout <= 0 {
		return engine.CoverageOptions{}, fmt.Errorf("--coverage-timeout must be positive, got %s", c.timeout)
	}
	return engine.CoverageOptions{Enabled: c.enabled, Timeout: c.timeout}, nil
}

// targetFlags are the flags every command that resolves a target shares:
// --config and --tokenizer, and on assess, rank and check the coverage
// flags.
type targetFlags struct {
	config    string
	tokenizer string
	// cov holds the coverage flags; nil when they are not registered.
	cov *coverageFlags
}

// register adds the flags to fs, the coverage flags only when coverage is
// set.
func (f *targetFlags) register(fs *flag.FlagSet, coverage bool) {
	fs.StringVar(&f.config, "config", "", "configuration file (default ./astimate.yaml, then the embedded default)")
	fs.StringVar(&f.tokenizer, "tokenizer", tokenizerEst, "token counting method: est or o200k")
	if coverage {
		f.cov = &coverageFlags{}
		f.cov.register(fs)
	}
}

// validate returns the coverage options the flags select. On an unknown
// tokenizer or a bad coverage flag it prints the usage error of command
// name to stderr and returns false.
func (f *targetFlags) validate(name string, stderr io.Writer) (engine.CoverageOptions, bool) {
	if !validTokenizer(f.tokenizer) {
		_, _ = fmt.Fprintf(stderr, "astimate: %s: unknown tokenizer %q: want %s or %s\n",
			name, f.tokenizer, tokenizerEst, tokenizerO200k)
		return engine.CoverageOptions{}, false
	}
	if f.cov == nil {
		return engine.CoverageOptions{}, true
	}
	cov, err := f.cov.options()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "astimate: %s: %v\n", name, err)
		return engine.CoverageOptions{}, false
	}
	return cov, true
}

// load resolves dir with the flags' configuration and tokenizer
// (loadTarget).
func (f *targetFlags) load(dir string, logger *slog.Logger) (*engine.Target, error) {
	return loadTarget(dir, f.config, f.tokenizer, logger)
}

// moduleRoot returns the module root among a command's positional
// arguments, "." when there is none. With more than one it prints the
// usage error of command name and fs's usage to stderr and returns false.
func moduleRoot(name string, positional []string, fs *flag.FlagSet, stderr io.Writer) (string, bool) {
	switch len(positional) {
	case 0:
		return ".", true
	case 1:
		return positional[0], true
	}
	_, _ = fmt.Fprintf(stderr, "astimate: %s: want at most one module root, got %d arguments\n", name, len(positional))
	fs.Usage()
	return "", false
}

// newFlagSet returns the flag set of command name, "astimate <name>",
// which prints its errors to stderr and, on -h or a usage error, usageLine
// followed by the flag defaults.
func newFlagSet(name, usageLine string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("astimate "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, usageLine)
		fs.PrintDefaults()
	}
	return fs
}

// newLogger returns the logger every command logs to stderr with, at info
// level.
func newLogger(stderr io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// writeBuffered renders into a buffer with render and writes it to stdout
// in one call, so a render error leaves stdout empty and a write error
// cannot leave a partial report.
func writeBuffered(stdout io.Writer, render func(io.Writer) error) error {
	var buf bytes.Buffer
	if err := render(&buf); err != nil {
		return err
	}
	_, err := stdout.Write(buf.Bytes())
	return err
}

// loadTarget resolves dir with engine.LoadTarget, recording the astimate
// version in reports and sending engine diagnostics to logger.
func loadTarget(dir, configPath, tokenizer string, logger *slog.Logger) (*engine.Target, error) {
	return engine.LoadTarget(dir, engine.TargetOptions{
		ConfigPath: configPath,
		Tokenizer:  tokenizer,
		Version:    astimateVersion(),
		Logger:     logger,
	})
}
