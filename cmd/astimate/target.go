package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Tokenizer names accepted by --tokenizer (SPEC.md 6.1).
const (
	tokenizerEst   = "est"
	tokenizerO200k = "o200k"
)

// errNotDir reports a target path that is not a directory.
var errNotDir = errors.New("not a directory")

// errNoLanguage reports a module root no extractor detects.
var errNoLanguage = errors.New("no supported language detected")

// targetFlags are the command-line settings loadTarget honours.
type targetFlags struct {
	// configPath is the --config value; empty means the default resolution.
	configPath string
	// tokenizer is the --tokenizer value, tokenizerEst or tokenizerO200k.
	tokenizer string
}

// target is a package directory resolved to its module and paired with a
// configured extractor.
type target struct {
	// module is the module root and path; its Cache is filled by Extract.
	module *metrics.ModuleContext
	// importPath is the package's import path, used to call Extract.
	importPath string
	// packagePath is the directory relative to the module root in slash
	// form, "." for the root itself.
	packagePath string
	// extractor is the Go extractor configured from cfg and the flags.
	extractor *golang.Extractor
	// cfg is the resolved configuration.
	cfg *config.Config
}

// validTokenizer reports whether name is a tokenizer --tokenizer accepts.
func validTokenizer(name string) bool {
	return name == tokenizerEst || name == tokenizerO200k
}

// loadTarget resolves dir to its module root, module path and import path,
// resolves the configuration and builds an extractor from both. It is shared
// by assess and by the later rank, baseline and check commands so they
// resolve targets the same way. A dir outside any module yields an error
// wrapping golang.ErrNoModule, whose text names go.mod.
func loadTarget(dir string, f targetFlags) (*target, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("resolving %s: %w", dir, errNotDir)
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

	cfg, _, err := config.Resolve(f.configPath)
	if err != nil {
		return nil, fmt.Errorf("resolving config: %w", err)
	}
	ext := golang.New(
		golang.WithCharsPerToken(cfg.CharsPerToken),
		golang.WithDupMinTokens(cfg.DupMinTokens),
		golang.WithTokenizer(f.tokenizer),
	)
	if !ext.Detect(root) {
		return nil, fmt.Errorf("detecting language of %s: %w", root, errNoLanguage)
	}
	return &target{
		module:      &metrics.ModuleContext{Root: root, ModulePath: modPath},
		importPath:  importPath,
		packagePath: pkgPath,
		extractor:   ext,
		cfg:         cfg,
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
