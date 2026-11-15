// Package detect configures every supported extractor from the
// configuration and finds the module, and with it the language, that a
// directory belongs to (SPEC.md 6.1 and 13). It is the one place engine
// names an extractor package: engine resolves a target through it and
// otherwise reads extractors through metrics.Extractor alone.
package detect

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/lang/typescript"
	"github.com/rfizzle/astimate/internal/metrics"
)

// ErrNoLanguage reports a module root no extractor detects.
var ErrNoLanguage = errors.New("no supported language detected")

// languageGo is the language identifier of the Go extractor.
const languageGo = "go"

// Registry returns every supported extractor configured from cfg, the
// tokenizer and logger, Go first so that it wins a tie.
func Registry(cfg *config.Config, tokenizer string, logger *slog.Logger) (*metrics.Registry, error) {
	reg, err := metrics.NewRegistry(
		golang.New(
			golang.WithCharsPerToken(cfg.CharsPerToken),
			golang.WithDupMinTokens(cfg.Duplication.MinTokens),
			golang.WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
			golang.WithDupFoldSigns(cfg.Duplication.FoldSigns),
			golang.WithDupSplitLiteralRuns(cfg.Duplication.SplitLiteralRuns),
			golang.WithTokenizer(tokenizer),
			golang.WithLogger(logger),
			golang.WithExclude(cfg.Exclude),
		),
		typescript.New(
			typescript.WithCharsPerToken(cfg.CharsPerToken),
			typescript.WithDuplication(cfg.Duplication.MinTokens, cfg.Duplication.IgnoreLiteralOnly,
				cfg.Duplication.FoldSigns),
			typescript.WithDupSplitLiteralRuns(cfg.Duplication.SplitLiteralRuns),
			typescript.WithTokenizer(tokenizer),
			typescript.WithLogger(logger),
			typescript.WithExclude(cfg.Exclude),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("registering extractors: %w", err)
	}
	return reg, nil
}

// Warnings returns one warning for each language id cfg's languages
// section names that reg has no extractor for.
func Warnings(cfg *config.Config, reg *metrics.Registry) []string {
	var warns []string
	for _, id := range cfg.Languages() {
		if _, ok := reg.Lookup(id); !ok {
			warns = append(warns, "languages."+id+": unknown language; known: "+strings.Join(reg.Languages(), ", "))
		}
	}
	return warns
}

// Module walks up from the absolute directory dir to the nearest
// directory some extractor of reg detects, and returns it with that
// extractor. When several detect the same directory, the first registered
// wins and logger, if set, is warned. It wraps golang.ErrNoModule and
// ErrNoLanguage when no directory up to the file system root is detected.
func Module(dir string, reg *metrics.Registry, logger *slog.Logger) (string, metrics.Extractor, error) {
	for {
		ext, err := reg.Detect(dir)
		switch {
		case err == nil:
			return dir, ext, nil
		case errors.Is(err, metrics.ErrAmbiguousLanguage):
			ext = firstDetecting(dir, reg)
			if logger != nil {
				logger.Warn("several languages detected; using the first", "root", dir, "language", ext.Language(), "detail", err.Error())
			}
			return dir, ext, nil
		case !errors.Is(err, metrics.ErrNoExtractor):
			return "", nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, fmt.Errorf("%w (nor any package.json): %w", golang.ErrNoModule, ErrNoLanguage)
		}
		dir = parent
	}
}

// firstDetecting returns the first extractor of reg, in registration order,
// that detects root. Call it only when one does.
func firstDetecting(root string, reg *metrics.Registry) metrics.Extractor {
	var first metrics.Extractor
	for _, lang := range reg.Languages() {
		if ext, ok := reg.Lookup(lang); ok && first == nil && ext.Detect(root) {
			first = ext
		}
	}
	return first
}

// ModulePathFunc returns how to read the module path of a module root of
// language, which names a Go module's packages by import path and keys a
// git baseline's package directories: golang.ModulePath for Go, nil for a
// language whose package identifiers already are module-relative
// directories.
func ModulePathFunc(language string) func(root string) (string, error) {
	if language == languageGo {
		return golang.ModulePath
	}
	return nil
}
