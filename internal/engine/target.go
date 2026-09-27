package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/lang/typescript"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// Tokenizer names a Target's extractor accepts (SPEC.md 6.1).
const (
	// TokenizerEst estimates tokens from characters; the default.
	TokenizerEst = "est"
	// TokenizerO200k counts tokens exactly with the o200k encoding.
	TokenizerO200k = "o200k"
)

// ErrNotDir reports a target path that is not a directory.
var ErrNotDir = errors.New("not a directory")

// ErrNoLanguage reports a module root no extractor detects.
var ErrNoLanguage = errors.New("no supported language detected")

// ErrUnknownTokenizer reports a tokenizer name other than TokenizerEst and
// TokenizerO200k.
var ErrUnknownTokenizer = errors.New("unknown tokenizer")

// TargetOptions are the settings LoadTarget honours.
type TargetOptions struct {
	// ConfigPath is the configuration file; empty means the default
	// resolution (./astimate.yaml, then the embedded default). Ignored when
	// Config is set.
	ConfigPath string
	// Config is an already resolved configuration to use instead of
	// resolving ConfigPath.
	Config *config.Config
	// Tokenizer is TokenizerEst or TokenizerO200k; empty means TokenizerEst.
	Tokenizer string
	// Version is the astimate version recorded in reports.
	Version string
	// Logger receives warnings, skipped rules and per-package failures. Nil
	// discards them.
	Logger *slog.Logger
}

// Target is a package directory resolved to its module and paired with a
// configured extractor.
type Target struct {
	// Mod is the module root and path; its Cache is filled by Extract.
	Mod *metrics.ModuleContext
	// Dir is the package directory relative to the module root in slash
	// form, "." for the root itself.
	Dir string
	// ImportPath is the package's import path, used to call Extract.
	ImportPath string
	// Ext is the extractor configured from Cfg and the options.
	Ext metrics.Extractor
	// Cfg is the resolved configuration.
	Cfg *config.Config
	// Tokenizer is the method Ext counts tokens with, TokenizerEst or
	// TokenizerO200k; empty means TokenizerEst.
	Tokenizer string
	// ConfigSource says where Cfg came from, as config.Resolve reports it;
	// empty when it was passed in TargetOptions.Config.
	ConfigSource string
	// Version is the astimate version recorded in reports.
	Version string
	// Logger receives warnings, skipped rules and per-package failures. Nil
	// discards them.
	Logger *slog.Logger
}

// logger returns t.Logger, or a logger that discards everything.
func (t *Target) logger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// ValidTokenizer reports whether name is a tokenizer a Target accepts.
func ValidTokenizer(name string) bool {
	return name == TokenizerEst || name == TokenizerO200k
}

// LoadTarget resolves dir to its module root, module path and package
// identifier, resolves the configuration and builds the extractor for the
// module's language from both. Every operation resolves its target this
// way. The module root is the nearest directory at or above dir that a
// registered extractor detects: a go.mod for Go, a package.json for
// TypeScript. When both are at the same root, Go is chosen and a warning
// logged. A Go module's packages are named by import path; a TypeScript
// module has no module path and names its packages by their directory
// relative to the root. When it resolves the configuration itself, it logs
// the configuration's warnings, and one for each languages override whose id
// no registered extractor reports. A dir outside any module yields an error wrapping
// golang.ErrNoModule, whose text names go.mod; an unknown tokenizer yields
// ErrUnknownTokenizer.
func LoadTarget(dir string, opts TargetOptions) (*Target, error) {
	tokenizer := opts.Tokenizer
	if tokenizer == "" {
		tokenizer = TokenizerEst
	}
	if !ValidTokenizer(tokenizer) {
		return nil, fmt.Errorf("%w %q: want %s or %s", ErrUnknownTokenizer, tokenizer, TokenizerEst, TokenizerO200k)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("resolving %s: %w", dir, ErrNotDir)
	}

	cfg, source := opts.Config, ""
	if cfg == nil {
		cfg, source, err = config.Resolve(opts.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("resolving config: %w", err)
		}
		if opts.Logger != nil {
			for _, w := range cfg.Warnings {
				opts.Logger.Warn("config", "source", source, "warning", w)
			}
		}
	}
	reg, err := newRegistry(cfg, tokenizer, opts.Logger)
	if err != nil {
		return nil, err
	}
	if opts.Config == nil && opts.Logger != nil {
		for _, w := range languageWarnings(cfg, reg) {
			opts.Logger.Warn("config", "source", source, "warning", w)
		}
	}
	root, ext, err := detectModule(abs, reg, opts.Logger)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}
	modPath := ""
	if ext.Language() == languageGo {
		modPath, err = golang.ModulePath(root)
		if err != nil {
			return nil, fmt.Errorf("reading module path of %s: %w", root, err)
		}
	}
	pkgPath, importPath, err := packagePaths(root, abs, modPath)
	if err != nil {
		return nil, err
	}
	return &Target{
		Mod:          &metrics.ModuleContext{Root: root, ModulePath: modPath},
		Dir:          pkgPath,
		ImportPath:   importPath,
		Ext:          ext,
		Cfg:          cfg,
		Tokenizer:    tokenizer,
		ConfigSource: source,
		Version:      opts.Version,
		Logger:       opts.Logger,
	}, nil
}

// LanguageWarnings returns one warning for each language id cfg's languages
// section names that no supported extractor reports, since such an override
// can never apply. The extractor registry, not the config package, is the
// source of truth for language ids.
func LanguageWarnings(cfg *config.Config) ([]string, error) {
	reg, err := newRegistry(cfg, TokenizerEst, nil)
	if err != nil {
		return nil, err
	}
	return languageWarnings(cfg, reg), nil
}

// languageWarnings returns one warning for each language id cfg's languages
// section names that reg has no extractor for.
func languageWarnings(cfg *config.Config, reg *metrics.Registry) []string {
	var warns []string
	for _, id := range cfg.Languages() {
		if _, ok := reg.Lookup(id); !ok {
			warns = append(warns, "languages."+id+": unknown language; known: "+strings.Join(reg.Languages(), ", "))
		}
	}
	return warns
}

// languageGo is the language identifier of the Go extractor.
const languageGo = "go"

// newRegistry returns every supported extractor configured from cfg, the
// tokenizer and logger, Go first so that it wins a tie.
func newRegistry(cfg *config.Config, tokenizer string, logger *slog.Logger) (*metrics.Registry, error) {
	reg, err := metrics.NewRegistry(
		golang.New(
			golang.WithCharsPerToken(cfg.CharsPerToken),
			golang.WithDupMinTokens(cfg.Duplication.MinTokens),
			golang.WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
			golang.WithDupFoldSigns(cfg.Duplication.FoldSigns),
			golang.WithTokenizer(tokenizer),
			golang.WithLogger(logger),
		),
		typescript.New(
			typescript.WithCharsPerToken(cfg.CharsPerToken),
			typescript.WithDuplication(cfg.Duplication.MinTokens, cfg.Duplication.IgnoreLiteralOnly,
				cfg.Duplication.FoldSigns),
			typescript.WithTokenizer(tokenizer),
			typescript.WithLogger(logger),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("registering extractors: %w", err)
	}
	return reg, nil
}

// detectModule walks up from the absolute directory dir to the nearest
// directory some extractor of reg detects, and returns it with that
// extractor. When several detect the same directory, the first registered
// wins and logger, if set, is warned. It wraps golang.ErrNoModule when no
// directory up to the file system root is detected.
func detectModule(dir string, reg *metrics.Registry, logger *slog.Logger) (string, metrics.Extractor, error) {
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

// packagePaths returns the slash-separated path of dir relative to root ("."
// for root itself) and the package identifier it has in module modPath: its
// import path, or, when modPath is empty as for a TypeScript module, the
// relative path itself. Both root and dir must be absolute, with dir at or
// below root.
func packagePaths(root, dir, modPath string) (pkgPath, importPath string, err error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", "", fmt.Errorf("relating %s to module root %s: %w", dir, root, err)
	}
	rel = filepath.ToSlash(rel)
	return rel, importPathOf(modPath, rel), nil
}

// modulePathRel returns the directory of the package with import path
// importPath relative to the root of module modPath, in slash form: "." for
// the root package, matching assess's package_path. With an empty modPath
// the identifier already is that directory.
func modulePathRel(modPath, importPath string) string {
	if modPath == "" {
		return importPath
	}
	if importPath == modPath {
		return "."
	}
	return strings.TrimPrefix(importPath, modPath+"/")
}

// importPathOf returns the import path of the package in the module-relative
// slash directory dir of module modPath, or dir itself when modPath is
// empty; the inverse of modulePathRel.
func importPathOf(modPath, dir string) string {
	if modPath == "" {
		return dir
	}
	if dir == "." {
		return modPath
	}
	return modPath + "/" + dir
}

// Names returns the names behind the counts of the package with import path
// pkg in t's module, for suggestions: identifiers when t.Ext implements
// metrics.Detailer, the zero score.Names otherwise. Call it after Extract
// for pkg.
func Names(ctx context.Context, t *Target, pkg string) (score.Names, error) {
	return suggestionNames(ctx, t.Ext, t.Mod, pkg)
}

// suggestionNames returns the names behind pkg's counts for suggestions when
// ext implements metrics.Detailer, and the zero score.Names, which yields
// suggestions with counts only, when it does not. Call it after Extract for
// pkg on mod.
func suggestionNames(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext, pkg string) (score.Names, error) {
	d, ok := ext.(metrics.Detailer)
	if !ok {
		return score.Names{}, nil
	}
	det, err := d.Details(ctx, mod, pkg)
	if err != nil {
		return score.Names{}, fmt.Errorf("naming suggestions for %s: %w", pkg, err)
	}
	return score.Names{UntestedExports: det.UntestedExports, DupLocations: det.DupLocations, CrossBlocks: det.CrossBlocks}, nil
}
