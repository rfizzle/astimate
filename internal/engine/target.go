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

// LoadTarget resolves dir to its module root, module path and import path,
// resolves the configuration and builds an extractor from both. Every
// operation resolves its target this way. A dir outside any module yields
// an error wrapping golang.ErrNoModule, whose text names go.mod; an unknown
// tokenizer yields ErrUnknownTokenizer.
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
	root, err := golang.FindModuleRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}
	modPath, err := golang.ModulePath(root)
	if err != nil {
		return nil, fmt.Errorf("reading module path of %s: %w", root, err)
	}
	pkgPath, importPath, err := packagePaths(root, abs, modPath)
	if err != nil {
		return nil, err
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
	ext := golang.New(
		golang.WithCharsPerToken(cfg.CharsPerToken),
		golang.WithDupMinTokens(cfg.Duplication.MinTokens),
		golang.WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
		golang.WithDupFoldSigns(cfg.Duplication.FoldSigns),
		golang.WithTokenizer(tokenizer),
	)
	if !ext.Detect(root) {
		return nil, fmt.Errorf("detecting language of %s: %w", root, ErrNoLanguage)
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

// packagePaths returns the slash-separated path of dir relative to root ("."
// for root itself) and the import path it has in module modPath. Both root
// and dir must be absolute, with dir at or below root.
func packagePaths(root, dir, modPath string) (pkgPath, importPath string, err error) {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return "", "", fmt.Errorf("relating %s to module root %s: %w", dir, root, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return rel, modPath, nil
	}
	return rel, modPath + "/" + rel, nil
}

// modulePathRel returns the directory of the package with import path
// importPath relative to the root of module modPath, in slash form: "." for
// the root package, matching assess's package_path.
func modulePathRel(modPath, importPath string) string {
	if importPath == modPath {
		return "."
	}
	return strings.TrimPrefix(importPath, modPath+"/")
}

// importPathOf returns the import path of the package in the module-relative
// slash directory dir of module modPath; the inverse of modulePathRel.
func importPathOf(modPath, dir string) string {
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
	return score.Names{UntestedExports: det.UntestedExports, DupLocations: det.DupLocations}, nil
}
