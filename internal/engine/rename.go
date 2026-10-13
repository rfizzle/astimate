package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// modulePathFunc returns how to read the module path of a tree of t's
// language, which a git baseline keys its packages' directories by:
// golang.ModulePath for Go, nil for a language whose package identifiers
// already are module-relative directories.
func (t *Target) modulePathFunc() baseline.ModulePathFunc {
	if t.Ext.Language() == languageGo {
		return golang.ModulePath
	}
	return nil
}

// seedRenames returns base seeded with the renames git detects between the
// baseline's merge-base and tree, the head side of the diff
// (baseline.FindRenames, baseline.Seeded), so a package whose non-test
// files were moved since the baseline is judged against the packages they
// came from rather than as new (SPEC.md 8.4). ht is the head target, whose
// import graph says which source imports a package carved out of it, when
// its extractor implements metrics.ImporterLister. base is returned
// unchanged when t's extractor does not implement
// metrics.SourceClassifier, and for a baseline file that records no ref
// or one that does not resolve, since no merge-base is known.
func seedRenames(ctx context.Context, t, ht *Target, base baseline.Baseline, src baselineSource,
	tree baseline.Head,
) (baseline.Baseline, error) {
	sc, ok := t.Ext.(metrics.SourceClassifier)
	if !ok {
		return base, nil
	}
	mergeBase := base.Ref()
	if src.file != nil {
		if mergeBase == "" {
			return base, nil
		}
		mb, err := baseline.MergeBase(ctx, t.Mod.Root, mergeBase)
		if err != nil {
			// No merge-base, so no renames to follow; the file still
			// serves every package that kept its directory.
			return base, nil
		}
		mergeBase = mb
	}
	renames, err := baseline.FindRenames(ctx, t.Mod.Root, mergeBase, sc, tree)
	if err != nil {
		return nil, err
	}
	if len(renames) == 0 {
		return base, nil
	}
	edges, err := carvedEdges(ctx, ht, base, renames)
	if err != nil {
		return nil, err
	}
	return baseline.Seeded(base, renames, func(from, to string) bool { return edges[to][from] }), nil
}

// carvedEdges returns, for each package of renames without a row of its own
// in base, the module-relative directories of the packages importing it in
// ht's head import graph; nil when ht's extractor does not implement
// metrics.ImporterLister. A renamed-into directory that is no package at
// head has no importers.
func carvedEdges(ctx context.Context, ht *Target, base baseline.Baseline, renames baseline.Renames,
) (map[string]map[string]bool, error) {
	il, ok := ht.Ext.(metrics.ImporterLister)
	if !ok {
		return nil, nil
	}
	modPath := ht.Mod.ModulePath
	edges := make(map[string]map[string]bool, len(renames))
	for to := range renames {
		if _, own := base.Row(to); own {
			continue
		}
		importers, err := il.Importers(ctx, ht.Mod, judge.ImportPath(modPath, to))
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
