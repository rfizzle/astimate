package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
)

// excludedSegments are the directory names that mark a TypeScript package
// as tests, fixtures, examples, benchmarks or documentation rather than
// the code a repository ships. TypeScript has no file convention as strict
// as Go's _test.go: a repository's test tree often holds plain .ts files
// (typeorm's test/, for one), which the extractor counts as packages.
func excludedSegments() []string {
	return []string{
		"__fixtures__", "__mocks__", "__tests__",
		"bench", "benchmark", "benchmarks",
		"demo", "demos", "docs",
		"e2e", "example", "examples",
		"fixture", "fixtures",
		"playground", "sandbox",
		"test", "tests",
		"website", "www",
	}
}

// excludedWords are the words that mark a directory name as a test tree
// when it is made of several: runtime-tests, __performance_tests__,
// dts-test.
func excludedWords() []string {
	return []string{"e2e", "spec", "specs", "test", "tests"}
}

// excludedPackage reports whether the package id, a slash path relative
// to its module root, lies under an excluded directory: one of
// excludedSegments, or a name one of whose words, split at '-', '_' and
// '.', is one of excludedWords. Such a package is left out of the pooled
// rows; it still counts in the other packages' fan_in and imports, which
// the module load measures.
func excludedPackage(id string) bool {
	isSep := func(r rune) bool { return r == '-' || r == '_' || r == '.' }
	for seg := range strings.SplitSeq(id, "/") {
		if slices.Contains(excludedSegments(), seg) {
			return true
		}
		for _, w := range strings.FieldsFunc(seg, isSep) {
			if slices.Contains(excludedWords(), w) {
				return true
			}
		}
	}
	return false
}

// moduleRoots expands the module root patterns of a TypeScript corpus
// entry against the clone at dir and returns the matching directories that
// hold a package.json, as sorted, de-duplicated slash paths relative to
// dir. A pattern that matches no such directory is an error, so a corpus
// entry that drifted from its repository fails loudly.
func moduleRoots(dir string, patterns []string) ([]string, error) {
	var roots []string
	for _, p := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return nil, fmt.Errorf("module root %q: %w", p, err)
		}
		n := 0
		for _, m := range matches {
			if fi, err := os.Stat(filepath.Join(m, "package.json")); err != nil || fi.IsDir() {
				continue
			}
			rel, err := filepath.Rel(dir, m)
			if err != nil {
				return nil, fmt.Errorf("module root %q: %w", p, err)
			}
			roots = append(roots, filepath.ToSlash(rel))
			n++
		}
		if n == 0 {
			return nil, fmt.Errorf("module root %q matches no directory with a package.json", p)
		}
	}
	slices.Sort(roots)
	return slices.Compact(roots), nil
}

// tsModuleName is the Module of the rows of the TypeScript module at root,
// relative to the repository of the corpus entry named repo.
func tsModuleName(repo, root string) string {
	if root == "." {
		return repo
	}
	return path.Join(repo, root)
}

// collectTSEntry collects one TypeScript corpus entry from a temporary
// shallow clone at its pin: every module root it names, each ranked on its
// own the way `astimate rank --json` ranks it.
func collectTSEntry(ctx context.Context, e Entry, cfg *config.Config, logger *slog.Logger) ([]Row, ModuleRun) {
	logger = logger.With("module", e.Module)
	logger.Info("collecting")
	mr := ModuleRun{Module: e.Module, Commit: e.Commit}
	dir, err := os.MkdirTemp("", "astimate-collect-*")
	if err != nil {
		mr.Error = err.Error()
		return nil, mr
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := cloneAt(ctx, e.Repo, e.Commit, dir); err != nil {
		logger.Error("clone failed", "err", err)
		mr.Error = err.Error()
		return nil, mr
	}
	mr.CommitDate = commitDate(ctx, dir)
	return collectTSTree(ctx, dir, e, cfg, logger, mr)
}

// commitDate returns the committer date of HEAD in the repository at dir,
// YYYY-MM-DD, or empty when git cannot read it. It is the evidence for the
// corpus's activity criterion.
func commitDate(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "log", "-1", "--format=%cs").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// collectTSTree collects the module roots of the TypeScript corpus entry e
// from the checkout at dir into mr. Each module is loaded once and ranked
// like a Go module; rows of excluded packages (excludedPackage) are dropped
// and counted, and every row is tagged with the module's name
// (tsModuleName), the entry's commit and the language.
func collectTSTree(ctx context.Context, dir string, e Entry, cfg *config.Config, logger *slog.Logger, mr ModuleRun,
) ([]Row, ModuleRun) {
	roots, err := moduleRoots(dir, e.Modules)
	if err != nil {
		logger.Error("collect failed", "err", err)
		mr.Error = err.Error()
		return nil, mr
	}
	mr.Roots = roots
	var rows []Row
	for _, root := range roots {
		name := tsModuleName(e.Module, root)
		res, err := collectTSModule(ctx, filepath.Join(dir, filepath.FromSlash(root)), e.Commit, cfg, logger.With("root", root))
		if err != nil {
			logger.Error("collect failed", "root", root, "err", err)
			mr.Failed = append(mr.Failed, packageFailure{Package: name, Err: err.Error()})
			continue
		}
		for _, f := range res.Failed {
			mr.Failed = append(mr.Failed, packageFailure{Package: path.Join(name, f.Package), Err: f.Err})
		}
		for i := range res.Rows {
			r := res.Rows[i]
			if excludedPackage(r.Package) {
				mr.Excluded++
				continue
			}
			r.Module, r.Language = name, languageTypeScript
			rows = append(rows, r)
		}
	}
	sortRows(rows)
	mr.Packages = len(rows)
	return rows, mr
}

// collectTSModule ranks the TypeScript module rooted at dir with
// collectModule, after checking that the root resolves to the TypeScript
// extractor: a root that also holds a go.mod resolves to Go.
func collectTSModule(ctx context.Context, dir, commit string, cfg *config.Config, logger *slog.Logger) (moduleResult, error) {
	t, err := engine.LoadTarget(dir, engine.TargetOptions{Config: cfg, Logger: logger})
	if err != nil {
		return moduleResult{}, err
	}
	if lang := t.Ext.Language(); lang != languageTypeScript {
		return moduleResult{}, fmt.Errorf("%s resolves to the %s extractor, not %s", dir, lang, languageTypeScript)
	}
	if t.Mod.Root != dir {
		return moduleResult{}, fmt.Errorf("%s resolves to the module at %s", dir, t.Mod.Root)
	}
	return collectModule(ctx, dir, commit, cfg, logger, false)
}
