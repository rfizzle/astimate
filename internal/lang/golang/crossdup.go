package golang

// Cross-package duplication (SPEC.md section 6, dup_blocks_cross_pkg).
//
// Stream. Every module package, in import path order, contributes its
// non-test, non-generated files to one normalized stream, scanned exactly
// as duplication scans one package's (see duplication.go), into one
// duptok.Stream. duptok follows each file with its own separator, so no
// match crosses a file boundary, let alone a package boundary; a side table
// maps each file index to its package, which is what attributes an
// occurrence to a package. Cross-package means within one module; repeats
// across modules or repositories are out of scope.
//
// Finder. The same duptok finder and the same merging and literal-only
// rules run over the module stream, through duptok.Stream.Blocks, so a
// block is an exact normalized repeat of at least duplication.min_tokens
// under the same options as dup_blocks. A block is cross-package when its
// occurrences lie in two or more packages. It counts once in
// dup_blocks_cross_pkg of each package it touches, however many of its
// occurrences that package holds, and once in the module row's
// dup_blocks_cross_pkg, which counts distinct blocks rather than summing
// the packages.
//
// Relation to dup_blocks. dup_blocks is computed per package, from that
// package's stream alone, exactly as it would be without this pass; the
// cross-package pass is additive and never adds a block to dup_blocks. That
// is the sense in which the two are never double-counted. A sequence that
// repeats inside one package and also occurs in another counts in both
// metrics for that package: once in dup_blocks for the repeat within the
// package, and once in dup_blocks_cross_pkg for the copy elsewhere.
//
// Cost. The pass runs once per load and duplication options, on the first
// Extract or ModuleRow that needs it, and its counts and the locations of
// the cross-package blocks, not the file bytes, are memoized on loaded, so
// Details and ModuleDetails name the blocks without reading a file again.
// It is not run for the standard-library loads, which are not modules:
// dup_blocks_cross_pkg is null there.

import (
	"cmp"
	"errors"
	"fmt"
	"go/token"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/metrics"
)

// crossDup is the cross-package duplication of one module load.
type crossDup struct {
	// blocks are the distinct duplicate blocks whose occurrences lie in two
	// or more packages, in order of first occurrence, each occurrence named
	// by package import path and file relative to the module root. Its
	// length is the module row's dup_blocks_cross_pkg. Shared read-only;
	// callers clone before handing it out.
	blocks []metrics.CrossBlock
	// perPkg maps a package's import path to the number of those blocks
	// with an occurrence in it; a package with none is absent.
	perPkg map[string]int
}

// crossDupMemo memoizes crossDup per duplication options on a load, so it
// is computed once however many packages are extracted. Keying by options
// keeps a module context shared by extractors configured differently
// correct.
type crossDupMemo struct {
	mu     sync.Mutex
	byOpts map[dupOptions]*crossDupEntry
}

// crossDupEntry is one memoized computation; once guards c and err.
type crossDupEntry struct {
	once sync.Once
	c    crossDup
	err  error
}

// crossApplies reports whether dup_blocks_cross_pkg is computed for the
// packages of l: it is for a module, not for the standard-library loads.
func crossApplies(l *loaded) bool {
	return l.modulePath != stdModulePath && l.modulePath != stdAllModulePath
}

// crossDuplication returns the cross-package duplication of l under opts,
// reading files through src on the first call for those options and
// returning the memoized result afterwards. Concurrent callers wait for one
// computation.
func crossDuplication(l *loaded, src fileSource, opts dupOptions) (crossDup, error) {
	l.cross.mu.Lock()
	if l.cross.byOpts == nil {
		l.cross.byOpts = make(map[dupOptions]*crossDupEntry)
	}
	e, ok := l.cross.byOpts[opts]
	if !ok {
		e = &crossDupEntry{}
		l.cross.byOpts[opts] = e
	}
	l.cross.mu.Unlock()
	e.once.Do(func() { e.c, e.err = computeCrossDup(l, src, opts) })
	return e.c, e.err
}

// computeCrossDup builds the module stream of l and attributes its
// cross-package blocks. See the file comment.
func computeCrossDup(l *loaded, src fileSource, opts dupOptions) (crossDup, error) {
	if opts.minTokens < 1 {
		return crossDup{}, fmt.Errorf("detecting cross-package duplication: minimum of %d tokens is not positive", opts.minTokens)
	}
	var s duptok.Stream
	z := newDupTokenizer(opts)
	fs := token.NewFileSet()
	filePkg := make([]int32, 0, len(l.paths))
	for i, ip := range l.paths {
		before := s.Files()
		if err := z.appendPackage(fs, l, l.pkgs[ip], src, &s); err != nil {
			return crossDup{}, fmt.Errorf("detecting cross-package duplication in %s: %w", ip, err)
		}
		for range s.Files() - before {
			filePkg = append(filePkg, int32(i))
		}
	}
	found, perPkg, err := crossPackage(&s, filePkg, len(l.paths), opts)
	if err != nil {
		return crossDup{}, fmt.Errorf("detecting cross-package duplication: %w", err)
	}
	c := crossDup{blocks: crossBlocks(l, found, filePkg), perPkg: make(map[string]int)}
	for i, n := range perPkg {
		if n > 0 {
			c.perPkg[l.paths[i]] = n
		}
	}
	return c, nil
}

// crossBlocks names the occurrences of the cross-package blocks found in
// the module stream of l, whose file i belongs to package l.paths[filePkg[i]],
// by package import path and file relative to the module root. Occurrences
// are sorted by package, file and line, and blocks by first occurrence.
func crossBlocks(l *loaded, found []duptok.Block, filePkg []int32) []metrics.CrossBlock {
	if len(found) == 0 {
		return nil
	}
	// dirs[k] is the directory of package k relative to the module root,
	// in slash form: its import path less the module path.
	dirs := make([]string, len(l.paths))
	for k, p := range l.paths {
		dirs[k] = strings.TrimPrefix(strings.TrimPrefix(p, l.modulePath), "/")
	}
	out := make([]metrics.CrossBlock, 0, len(found))
	for _, b := range found {
		occ := make([]metrics.Occurrence, len(b.Files))
		for i, f := range b.Files {
			k := filePkg[f]
			loc := b.Locations[i]
			occ[i] = metrics.Occurrence{
				Package:   l.paths[k],
				File:      path.Join(dirs[k], relFile(l.pkgs[l.paths[k]].Dir, loc.File)),
				StartLine: loc.StartLine,
				EndLine:   loc.EndLine,
			}
		}
		slices.SortFunc(occ, compareOccurrence)
		out = append(out, metrics.CrossBlock{Occurrences: occ})
	}
	slices.SortFunc(out, func(a, b metrics.CrossBlock) int {
		return cmp.Or(compareOccurrence(a.Occurrences[0], b.Occurrences[0]),
			cmp.Compare(len(a.Occurrences), len(b.Occurrences)))
	})
	return out
}

// compareOccurrence orders occurrences by package, file, first line and
// last line.
func compareOccurrence(a, b metrics.Occurrence) int {
	return cmp.Or(cmp.Compare(a.Package, b.Package), cmp.Compare(a.File, b.File),
		cmp.Compare(a.StartLine, b.StartLine), cmp.Compare(a.EndLine, b.EndLine))
}

// crossPackage finds the duplicate blocks of s under opts and keeps those
// whose occurrences lie in two or more packages, where filePkg[i] is the
// package index, below npkg, of the stream's file i. It returns those
// blocks and, per package index, how many of them touch that package; a
// block counts once per package however many of its occurrences the
// package holds.
func crossPackage(s *duptok.Stream, filePkg []int32, npkg int, opts dupOptions) (cross []duptok.Block, perPkg []int, err error) {
	if len(filePkg) != s.Files() {
		return nil, nil, errors.New("package table does not cover every file")
	}
	found, err := s.Blocks(opts.finder())
	if err != nil {
		return nil, nil, err
	}
	perPkg = make([]int, npkg)
	// last[k] is the block index that last touched package k, plus one, so
	// each block counts a package once without a per-block set.
	last := make([]int, npkg)
	touched := make([]int32, 0, 4)
	for bi, b := range found {
		touched = touched[:0]
		for _, f := range b.Files {
			k := filePkg[f]
			if last[k] != bi+1 {
				last[k] = bi + 1
				touched = append(touched, k)
			}
		}
		if len(touched) < 2 {
			continue
		}
		cross = append(cross, b)
		for _, k := range touched {
			perPkg[k]++
		}
	}
	return cross, perPkg, nil
}

// crossBlocksOf returns deep copies of the blocks of c with an occurrence
// in the package with import path pkg, or every block when pkg is empty;
// nil when there are none.
func crossBlocksOf(c crossDup, pkg string) []metrics.CrossBlock {
	var out []metrics.CrossBlock
	for _, b := range c.blocks {
		if pkg != "" && !slices.ContainsFunc(b.Occurrences, func(o metrics.Occurrence) bool { return o.Package == pkg }) {
			continue
		}
		out = append(out, metrics.CrossBlock{Occurrences: slices.Clone(b.Occurrences)})
	}
	return out
}
