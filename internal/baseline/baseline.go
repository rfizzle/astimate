package baseline

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// Baseline serves the metrics a change is compared against. Rows are
// keyed by the package directory relative to the module root, in slash
// form ("." for the root package), the package_path reports use (SPEC.md
// 10.2), so a change of the module path does not void the baseline; the
// module-level row stays under metrics.ModuleRowID. A package moved to
// another directory has no row of its own; Seeded serves one from the
// packages its files were renamed from.
type Baseline interface {
	// Row returns the baseline row of the package in the module-relative
	// directory dir, or the module row under metrics.ModuleRowID, and
	// false when the baseline has no such row.
	Row(dir string) (Row, bool)
	// Ref names where the baseline came from: the merge-base commit for a git
	// baseline, or the ref recorded in a baseline file.
	Ref() string
	// Tokenizer names the method that counted the baseline's tokens_est:
	// the extractor's tokenizer for a baseline extracted from a commit, or
	// the tokenizer recorded in a baseline file, "est" when it records none.
	Tokenizer() string
	// CrossBlocks returns the module's cross-package duplicate blocks as
	// the baseline recorded them, each occurrence's Package being a
	// module-relative directory, and false when it recorded none: the
	// extractor does not implement metrics.ModuleDetailer, or the baseline
	// is a file, which stores only the module row's count. A check of
	// named packages matches them against the blocks at head to tell which
	// blocks are new.
	CrossBlocks() ([]metrics.CrossBlock, bool)
	// Importers returns the module-relative directories of the packages
	// that imported the package in dir in the baseline, sorted, as
	// metrics.ImporterLister listed them there (nil when dir was not a
	// package), and false when the baseline recorded no import graph: the
	// extractor does not implement metrics.ImporterLister, or the baseline
	// is a file. Changed-package detection selects a removed declaration
	// file's former importers from it (SPEC.md 8.4).
	Importers(dir string) ([]string, bool)
	// Unavailable returns why the baseline has no row for the package in
	// dir although the package existed there: it failed to load in the
	// baseline tree (metrics.Skipper), so it could not be measured. A
	// package that did not exist at the baseline, and any package of a
	// baseline file, returns false. A check judges an unavailable package
	// against the maxes only, skipping the ratchets from zero, since its
	// history is unknown rather than empty.
	Unavailable(dir string) (reason string, ok bool)
}

// Row is what a baseline records for one package, or for the module row.
// It is the one place a per-package baseline record lives: a new record is
// a field here, filled from the extraction in collected.rows and from a
// baseline file's per-package fileRow in FromFile.
type Row struct {
	// Metrics are the package's metrics at the baseline.
	Metrics metrics.RawMetrics
	// Functions are the package's functions as the baseline recorded
	// them, for the function-level diff behind changed_func_cognitive_max;
	// meaningful only when HasFunctions. A file baseline's functions carry
	// no File or Line.
	Functions []metrics.FunctionInfo
	// HasFunctions is false when the baseline recorded no functions for
	// the package: the extractor does not implement
	// metrics.FunctionLister, or the baseline file predates function
	// records.
	HasFunctions bool
	// From are the module-relative directories, sorted, of the baseline
	// packages a row served by Seeded was seeded from, because the
	// package's files were renamed from them; nil for a row the baseline
	// recorded.
	From []string
	// Named is what the baseline recorded of the package's details, so a
	// check can tell the duplicate blocks and untested exports a change
	// added from those the baseline had; meaningful only when HasNamed.
	Named Named
	// HasNamed is false when the baseline recorded no details for the
	// package: the extractor does not implement metrics.Detailer, or the
	// baseline file predates them.
	HasNamed bool
}

// Named is the part of a package's metrics.Details a baseline records.
type Named struct {
	// DupBlocks are the anchors of the package's duplicate blocks
	// (metrics.DupBlock), both of each block's, sorted and distinct.
	DupBlocks []uint64
	// UntestedExports names the package's untested exports, sorted.
	UntestedExports []string
}

// namedOf returns the part of d a baseline records.
func namedOf(d *metrics.Details) Named {
	n := Named{UntestedExports: slices.Clone(d.UntestedExports)}
	for _, b := range d.DupBlocks {
		n.DupBlocks = append(n.DupBlocks, b.Anchors[:]...)
	}
	slices.Sort(n.DupBlocks)
	n.DupBlocks = slices.Compact(n.DupBlocks)
	slices.Sort(n.UntestedExports)
	return n
}

// snapshot is the map-backed Baseline shared by the git and file sources.
type snapshot struct {
	ref       string
	tokenizer string
	// rows are keyed by module-relative directory, the module row by
	// metrics.ModuleRowID.
	rows map[string]Row
	// cross holds the module's cross-package blocks, packages named by
	// directory; crossKnown says whether they were recorded at all.
	cross      []metrics.CrossBlock
	crossKnown bool
	// importers holds each package's importers, keyed and listed by
	// directory; importersKnown says whether the import graph was recorded
	// at all.
	importers      map[string][]string
	importersKnown bool
	// unavailable holds, by module-relative directory, why each package
	// that failed to load in the baseline tree has no row.
	unavailable map[string]string
	// migrated says a file stored the module row under the key used
	// before metrics.ModuleRowID was reserved.
	migrated bool
	// importPathOnly says a file keyed its packages by import path only,
	// as files written before package_rows existed do.
	importPathOnly bool
}

// collected is a baseline's packages as an extraction or a file holds
// them, keyed by the identifier the extractor gave each (the import path
// for Go).
type collected struct {
	pkgs  map[string]metrics.RawMetrics
	funcs map[string][]metrics.FunctionInfo
	// named holds each package's recorded details; nil when none were
	// recorded.
	named map[string]Named
	// dir maps an identifier to its module-relative directory.
	dir func(id string) string
}

// rows returns c's rows keyed by module-relative directory. A package with
// no entry in c.funcs, and every package when c.funcs is nil, records no
// functions; likewise c.named and details.
func (c *collected) rows() map[string]Row {
	rows := make(map[string]Row, len(c.pkgs))
	for id, m := range c.pkgs {
		r := Row{Metrics: m}
		if id != metrics.ModuleRowID {
			r.Functions, r.HasFunctions = c.funcs[id]
			r.Named, r.HasNamed = c.named[id]
		}
		rows[c.dir(id)] = r
	}
	return rows
}

// PackageDir returns the module-relative directory, in slash form, of the
// package with identifier id in the module of path modulePath: "." for the
// root package, and id itself when modulePath is empty (a TypeScript
// identifier already is that directory) or id is metrics.ModuleRowID.
func PackageDir(modulePath, id string) string {
	switch {
	case modulePath == "" || id == metrics.ModuleRowID:
		return id
	case id == modulePath:
		return "."
	}
	return strings.TrimPrefix(id, modulePath+"/")
}

// CrossBlocks implements Baseline.
func (s *snapshot) CrossBlocks() ([]metrics.CrossBlock, bool) {
	return s.cross, s.crossKnown
}

// Importers implements Baseline.
func (s *snapshot) Importers(dir string) ([]string, bool) {
	return s.importers[dir], s.importersKnown
}

// Unavailable implements Baseline.
func (s *snapshot) Unavailable(dir string) (string, bool) {
	r, ok := s.unavailable[dir]
	return r, ok
}

// Row implements Baseline.
func (s *snapshot) Row(dir string) (Row, bool) {
	r, ok := s.rows[dir]
	return r, ok
}

// Ref implements Baseline.
func (s *snapshot) Ref() string { return s.ref }

// Tokenizer implements Baseline.
func (s *snapshot) Tokenizer() string { return s.tokenizer }

// Collect extracts every package ext lists under mod.Root and returns the
// metrics keyed by package identifier (the import path for Go). When ext
// implements metrics.ModuleMetrics it also adds the module-level row under
// metrics.ModuleRowID, so a check can compare module-wide metrics such as
// dup_blocks_cross_pkg against it. It stops at the first extraction error
// or when ctx is done.
func Collect(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext) (map[string]metrics.RawMetrics, error) {
	names, err := ext.Packages(mod.Root)
	if err != nil {
		return nil, fmt.Errorf("listing packages in %s: %w", mod.Root, err)
	}
	pkgs := make(map[string]metrics.RawMetrics, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("extracting %s: %w", name, err)
		}
		m, err := ext.Extract(ctx, mod, name)
		if err != nil {
			return nil, fmt.Errorf("extracting %s: %w", name, err)
		}
		pkgs[name] = m
	}
	if mm, ok := ext.(metrics.ModuleMetrics); ok {
		m, err := mm.ModuleRow(ctx, mod)
		if err != nil {
			return nil, fmt.Errorf("extracting %s row: %w", metrics.ModuleRowID, err)
		}
		pkgs[metrics.ModuleRowID] = m
	}
	return pkgs, nil
}

// CollectFunctions lists, with ext's metrics.FunctionLister, the functions
// of every package in pkgs except the module row, keyed like pkgs. Call it
// after Collect on the same mod, so ext answers from the extraction it just
// did. It returns nil, and no error, when ext is not a FunctionLister, and
// stops at the first listing error or when ctx is done.
func CollectFunctions(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext,
	pkgs map[string]metrics.RawMetrics,
) (map[string][]metrics.FunctionInfo, error) {
	fl, ok := ext.(metrics.FunctionLister)
	if !ok {
		return nil, nil
	}
	return collectEach(ctx, pkgs, "listing functions of", func(name string) ([]metrics.FunctionInfo, error) {
		fns, err := fl.Functions(ctx, mod, name)
		if fns == nil {
			fns = []metrics.FunctionInfo{}
		}
		return fns, err
	})
}

// CollectNamed records, with ext's metrics.Detailer, the duplicate blocks
// and untested exports of every package in pkgs except the module row,
// keyed like pkgs. Call it after Collect on the same mod, so ext answers
// from the extraction it just did. It returns nil, and no error, when ext
// is not a Detailer, and stops at the first error or when ctx is done.
func CollectNamed(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext,
	pkgs map[string]metrics.RawMetrics,
) (map[string]Named, error) {
	d, ok := ext.(metrics.Detailer)
	if !ok {
		return nil, nil
	}
	return collectEach(ctx, pkgs, "naming the details of", func(name string) (Named, error) {
		det, err := d.Details(ctx, mod, name)
		return namedOf(&det), err
	})
}

// CollectImporters lists, with ext's metrics.ImporterLister, the importers
// of every package in pkgs except the module row, keyed like pkgs, leaving
// out the packages nothing imports. Call it after Collect on the same mod,
// so ext answers from the extraction it just did. It returns false, with
// no error, when ext is not an ImporterLister, and stops at the first
// listing error or when ctx is done.
func CollectImporters(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext,
	pkgs map[string]metrics.RawMetrics,
) (map[string][]string, bool, error) {
	il, ok := ext.(metrics.ImporterLister)
	if !ok {
		return nil, false, nil
	}
	importers, err := collectEach(ctx, pkgs, "listing the importers of", func(name string) ([]string, error) {
		return il.Importers(ctx, mod, name)
	})
	if err != nil {
		return nil, false, err
	}
	maps.DeleteFunc(importers, func(_ string, from []string) bool { return len(from) == 0 })
	return importers, true, nil
}

// collectEach returns what each returns for every package in pkgs except
// the module row, keyed like pkgs. It stops at the first error or when ctx
// is done, wrapping the error with what and the package's name.
func collectEach[T any](ctx context.Context, pkgs map[string]metrics.RawMetrics, what string,
	each func(name string) (T, error),
) (map[string]T, error) {
	out := make(map[string]T, len(pkgs))
	for name := range pkgs {
		if name == metrics.ModuleRowID {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%s %s: %w", what, name, err)
		}
		v, err := each(name)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", what, name, err)
		}
		out[name] = v
	}
	return out, nil
}

// CollectCrossBlocks returns, with ext's metrics.ModuleDetailer, every
// cross-package duplicate block of the module at mod, and false, with no
// error, when ext does not implement it. Call it after Collect on the same
// mod, so ext answers from the extraction it just did.
func CollectCrossBlocks(ctx context.Context, ext metrics.Extractor, mod *metrics.ModuleContext) ([]metrics.CrossBlock, bool, error) {
	d, ok := ext.(metrics.ModuleDetailer)
	if !ok {
		return nil, false, nil
	}
	det, err := d.ModuleDetails(ctx, mod)
	if err != nil {
		return nil, false, fmt.Errorf("naming the cross-package blocks of %s: %w", metrics.ModuleRowID, err)
	}
	return det.CrossBlocks, true, nil
}
