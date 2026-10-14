// Package selection picks the packages a check gates: the ones changed
// since the baseline's merge-base, the importers of a package whose
// declarations changed, or every package when what changed cannot be told,
// leaving out the skipped packages that did not change. It also seeds the
// baseline with the renames found in the same diff, so a moved package is
// compared with the one it came from. Package engine composes it; it
// resolves no baseline and judges no row.
package selection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Source is where the baseline comes from: a git ref or a file, never
// both.
type Source struct {
	// Ref is the git ref whose merge-base with HEAD is the baseline; empty
	// when File is set.
	Ref string
	// File is the baseline read from a file; nil for a git baseline.
	File baseline.Baseline
}

// Request is what Packages selects from and Seed seeds with.
type Request struct {
	// Ext is the target's extractor.
	Ext metrics.Extractor
	// Mod is the target's module: git runs at its root and its module path
	// names the packages.
	Mod *metrics.ModuleContext
	// Head is the head module, whose import graph lists importers; it is
	// Mod's counterpart in a staged copy when the check is staged.
	Head *metrics.ModuleContext
	// Packages are the import paths the extractor lists at head.
	Packages []string
	// Source is the baseline's source; a file's own ref names its
	// merge-base.
	Source Source
	// All selects every package.
	All bool
	// Tree says whether the changes are those of the working tree or of
	// the index.
	Tree baseline.Head
	// Skips are the packages the extractor skipped.
	Skips judge.Skips
	// LoadBase returns the baseline, loading it on first use.
	LoadBase func() (baseline.Baseline, error)
	// Logger receives the info and warning lines that say why a package
	// was selected.
	Logger *slog.Logger
}

// Packages returns the import paths to check, in the order of
// r.Packages, and the module-relative directories deleted since the
// baseline. With r.All set it returns r.Packages. Every return of all of
// r.Packages leaves out the packages r.Skips holds whose own files did not
// change, since a package that does not load is selected only when it
// changed (judge.Skips.Unchanged). Otherwise it takes the packages changed
// since the merge-base of HEAD and the baseline's ref and keeps those in
// r.Packages, which drops the directories the go tool ignores. A file
// baseline whose ref does not resolve in git, for example outside a
// repository, and an extractor that does not implement
// metrics.SourceClassifier select every package and say so. A changed
// file that can move every package, such as a TypeScript tsconfig.json,
// selects every package too. A changed file that can move its package's
// importers, such as a TypeScript declaration file, also selects the
// packages importing that package in r.Head and in the baseline, which
// r.LoadBase returns (selectImporters).
func Packages(ctx context.Context, r Request) (selected, deleted []string, err error) {
	logger, head, skips := r.Logger, r.Packages, r.Skips
	sc, ok := r.Ext.(metrics.SourceClassifier)
	ref := r.Source.Ref
	if r.Source.File != nil {
		ref = r.Source.File.Ref()
	}
	if r.All {
		return skips.Unchanged(head, changedForAll(ctx, r, sc, ref)), nil, nil
	}
	if !ok {
		logger.Warn("the extractor cannot tell which files changed a package; checking every package",
			"language", r.Ext.Language())
		return skips.Unchanged(head, nil), nil, nil
	}
	if ref == "" {
		logger.Warn("baseline file records no git ref; checking every package")
		return skips.Unchanged(head, nil), nil, nil
	}
	mergeBase, err := baseline.MergeBase(ctx, r.Mod.Root, ref)
	if err != nil {
		if r.Source.File == nil {
			return nil, nil, err
		}
		logger.Warn("cannot resolve the baseline file's ref; checking every package", "ref", ref, "err", err)
		return skips.Unchanged(head, nil), nil, nil
	}
	change, err := baseline.ChangedPackages(ctx, r.Mod.Root, mergeBase, sc, r.Tree)
	if err != nil {
		return nil, nil, err
	}
	changed := changedImportPaths(r.Mod.ModulePath, change)
	if change.All {
		logger.Info("a changed file can move every package; checking every package")
		return skips.Unchanged(head, changed), change.Deleted, nil
	}
	if err := addImporters(ctx, r, change, changed); err != nil {
		return nil, nil, err
	}
	for _, pkg := range head {
		if changed[pkg] {
			selected = append(selected, pkg)
		}
	}
	return selected, change.Deleted, nil
}

// changedForAll returns the import paths of the packages changed since the
// merge-base of HEAD and ref, for a selection of every package: only a
// skipped package that changed is selected by --all. The changes are
// listed on a best effort, since --all needs no baseline to select the
// packages that load, so it returns an empty set when sc is nil, ref is
// empty, nothing was skipped or the diff cannot be listed.
func changedForAll(ctx context.Context, r Request, sc metrics.SourceClassifier, ref string) map[string]bool {
	changed := map[string]bool{}
	if sc == nil || ref == "" || !r.Skips.Any() {
		return changed
	}
	mergeBase, err := baseline.MergeBase(ctx, r.Mod.Root, ref)
	if err != nil {
		return changed
	}
	change, err := baseline.ChangedPackages(ctx, r.Mod.Root, mergeBase, sc, r.Tree)
	if err != nil {
		return changed
	}
	return changedImportPaths(r.Mod.ModulePath, change)
}

// addImporters adds to changed the importers of the packages whose
// declarations change names (selectImporters), loading the baseline for
// its import graph, when r.Ext implements metrics.ImporterLister and
// change has such packages.
func addImporters(ctx context.Context, r Request, change baseline.Change, changed map[string]bool) error {
	il, ok := r.Ext.(metrics.ImporterLister)
	if !ok || len(change.Contract)+len(change.RemovedContract) == 0 {
		return nil
	}
	base, err := r.LoadBase()
	if err != nil {
		return err
	}
	return selectImporters(ctx, il, r.Head, r.Mod.ModulePath, r.Packages, change, base, changed, r.Logger)
}

// importerGraph is the reverse import graph a baseline recorded
// (baseline.Baseline.Importers).
type importerGraph interface {
	// Importers returns the module-relative directories of the importers
	// of the package in dir, and false when no graph was recorded.
	Importers(dir string) ([]string, bool)
}

// selectImporters adds to changed the import paths of the packages, among
// head (the import paths the extractor lists), that import a package whose
// change can move their importers' metrics. change names those packages by
// module-relative directory: the importers of each package of
// change.Contract that head holds are taken from il in mod, the head
// module of module path modPath; the importers of each package of
// change.Contract and change.RemovedContract are also taken from base, the
// baseline's graph, so an edge that a removed or rewritten declaration
// file resolved still selects its importer. Each package it adds is logged
// at info level with the package it imports, the baseline's saying
// "importer at baseline", so the output says why a package no file of
// which changed was checked. When base is nil or records no graph and a
// declaration file was removed, one info line says its former importers
// are not selected.
func selectImporters(ctx context.Context, il metrics.ImporterLister, mod *metrics.ModuleContext, modPath string,
	head []string, change baseline.Change, base importerGraph, changed map[string]bool, logger *slog.Logger,
) error {
	inHead := make(map[string]bool, len(head))
	for _, pkg := range head {
		inHead[pkg] = true
	}
	add := func(importers []string, dir, msg string) {
		for _, imp := range importers {
			if !inHead[imp] || changed[imp] {
				continue
			}
			changed[imp] = true
			logger.Info(msg, "package", judge.Rel(modPath, imp), "importer_of", dir)
		}
	}
	for _, dir := range change.Contract {
		pkg := judge.ImportPath(modPath, dir)
		if !inHead[pkg] {
			continue
		}
		importers, err := il.Importers(ctx, mod, pkg)
		if err != nil {
			return fmt.Errorf("listing the importers of %s: %w", dir, err)
		}
		add(importers, dir, "selected as an importer of a package whose declarations changed")
	}
	dirs := slices.Concat(change.Contract, change.RemovedContract)
	slices.Sort(dirs)
	for _, dir := range slices.Compact(dirs) {
		importers, known := baseImporters(base, modPath, dir)
		if !known {
			if len(change.RemovedContract) > 0 {
				logger.Info("the baseline records no import graph; the former importers of a removed declaration file are not selected",
					"packages", strings.Join(change.RemovedContract, ","))
			}
			return nil
		}
		add(importers, dir, "selected as an importer at baseline of a package whose declarations changed")
	}
	return nil
}

// baseImporters returns the import paths, in module modPath, of the
// importers base records for the package in dir, and false when base is
// nil or records no graph.
func baseImporters(base importerGraph, modPath, dir string) ([]string, bool) {
	if base == nil {
		return nil, false
	}
	dirs, known := base.Importers(dir)
	var importers []string
	for _, d := range dirs {
		importers = append(importers, judge.ImportPath(modPath, d))
	}
	return importers, known
}

// changedImportPaths returns the import paths, in module modPath, of the
// packages change lists as changed.
func changedImportPaths(modPath string, change baseline.Change) map[string]bool {
	changed := make(map[string]bool, len(change.Packages))
	for _, dir := range change.Packages {
		changed[judge.ImportPath(modPath, dir)] = true
	}
	return changed
}

// Seed returns base seeded with the renames git detects between the
// baseline's merge-base and r.Tree, the head side of the diff
// (baseline.FindRenames, baseline.Seeded), so a package whose non-test
// files were moved since the baseline is judged against the packages they
// came from rather than as new (SPEC.md 8.4). r.Head's import graph says
// which source imports a package carved out of it, when r.Ext implements
// metrics.ImporterLister. base is returned unchanged when r.Ext does not
// implement metrics.SourceClassifier, and for a baseline file that
// records no ref or one that does not resolve, since no merge-base is
// known.
func Seed(ctx context.Context, r Request, base baseline.Baseline) (baseline.Baseline, error) {
	sc, ok := r.Ext.(metrics.SourceClassifier)
	if !ok {
		return base, nil
	}
	mergeBase := base.Ref()
	if r.Source.File != nil {
		if mergeBase == "" {
			return base, nil
		}
		mb, err := baseline.MergeBase(ctx, r.Mod.Root, mergeBase)
		if err != nil {
			// No merge-base, so no renames to follow; the file still
			// serves every package that kept its directory.
			return base, nil
		}
		mergeBase = mb
	}
	renames, err := baseline.FindRenames(ctx, r.Mod.Root, mergeBase, sc, r.Tree)
	if err != nil {
		return nil, err
	}
	if len(renames) == 0 {
		return base, nil
	}
	edges, err := carvedEdges(ctx, r, base, renames)
	if err != nil {
		return nil, err
	}
	return baseline.Seeded(base, renames, func(from, to string) bool { return edges[to][from] }), nil
}

// carvedEdges returns, for each package of renames without a row of its own
// in base, the module-relative directories of the packages importing it in
// r.Head's import graph; nil when r.Ext does not implement
// metrics.ImporterLister. A renamed-into directory that is no package at
// head has no importers.
func carvedEdges(ctx context.Context, r Request, base baseline.Baseline, renames baseline.Renames,
) (map[string]map[string]bool, error) {
	il, ok := r.Ext.(metrics.ImporterLister)
	if !ok {
		return nil, nil
	}
	modPath := r.Head.ModulePath
	edges := make(map[string]map[string]bool, len(renames))
	for to := range renames {
		if _, own := base.Row(to); own {
			continue
		}
		importers, err := il.Importers(ctx, r.Head, judge.ImportPath(modPath, to))
		if errors.Is(err, metrics.ErrUnknownPackage) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("listing the importers of %s: %w", to, err)
		}
		set := make(map[string]bool, len(importers))
		for _, imp := range importers {
			set[judge.Rel(modPath, imp)] = true
		}
		edges[to] = set
	}
	return edges, nil
}
