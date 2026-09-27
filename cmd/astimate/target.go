package main

import (
	"log/slog"

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
