package main

import (
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
		"run the tests with go test -cover to report coverage_pct and untested_exports_covered and scale the estimate by coverage_pct")
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
