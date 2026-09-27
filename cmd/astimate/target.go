package main

import (
	"flag"
	"fmt"
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
