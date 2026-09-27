package golang

// Cross-package duplication (SPEC.md section 6, dup_blocks_cross_pkg).
//
// Stream. Every module package, in import path order, contributes its
// non-test, non-generated files to one normalized stream, scanned exactly
// as duplication scans one package's (see duplication.go). Each file is
// followed by its own separator, dupSeparatorBase plus the file's index in
// the whole stream, so no match crosses a file boundary, let alone a
// package boundary; a side table maps each file index to its package, which
// is what attributes an occurrence to a package. Cross-package means within
// one module; repeats across modules or repositories are out of scope.
//
// Finder. The same suffix-array finder and the same merging and
// literal-only rules run over the module stream, so a block is an exact
// normalized repeat of at least duplication.min_tokens under the same
// options as dup_blocks. A block is cross-package when its occurrences lie
// in two or more packages. It counts once in dup_blocks_cross_pkg of each
// package it touches, however many of its occurrences that package holds,
// and once in the module row's dup_blocks_cross_pkg, which counts distinct
// blocks rather than summing the packages.
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
// Extract or ModuleRow that needs it, and its counts, not the file bytes,
// are memoized on loaded. It is not run for the standard-library loads,
// which are not modules: dup_blocks_cross_pkg is null there.

import (
	"errors"
	"fmt"
	"go/token"
	"sync"
)

// crossDup is the cross-package duplication of one module load.
type crossDup struct {
	// blocks is the number of distinct duplicate blocks whose occurrences
	// lie in two or more packages: the module row's dup_blocks_cross_pkg.
	blocks int
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
	s := newDupStream()
	fs := token.NewFileSet()
	filePkg := make([]int32, 0, len(l.paths))
	for i, path := range l.paths {
		before := len(s.files)
		if err := s.appendPackage(fs, l, l.pkgs[path], src, opts); err != nil {
			return crossDup{}, fmt.Errorf("detecting cross-package duplication in %s: %w", path, err)
		}
		for range len(s.files) - before {
			filePkg = append(filePkg, int32(i))
		}
	}
	blocks, perPkg, err := s.crossPackage(filePkg, len(l.paths), opts)
	if err != nil {
		return crossDup{}, fmt.Errorf("detecting cross-package duplication: %w", err)
	}
	c := crossDup{blocks: blocks, perPkg: make(map[string]int)}
	for i, n := range perPkg {
		if n > 0 {
			c.perPkg[l.paths[i]] = n
		}
	}
	return c, nil
}

// crossPackage finds the duplicate blocks of s under opts and counts those
// whose occurrences lie in two or more packages, where filePkg[i] is the
// package index, below npkg, of the stream's file i. It returns the number
// of such blocks and, per package index, how many of them touch that
// package; a block counts once per package however many of its occurrences
// the package holds.
func (s *dupStream) crossPackage(filePkg []int32, npkg int, opts dupOptions) (blocks int, perPkg []int, err error) {
	if len(filePkg) != len(s.files) {
		return 0, nil, errors.New("package table does not cover every file")
	}
	perPkg = make([]int, npkg)
	sa, reps := s.find(opts)
	// last[k] is the block index that last touched package k, plus one, so
	// each block counts a package once without a per-block set.
	last := make([]int, npkg)
	touched := make([]int32, 0, 4)
	for bi, r := range reps {
		touched = touched[:0]
		for _, pos := range sa[r.lb : r.rb+1] {
			k := filePkg[s.file[pos]]
			if last[k] != bi+1 {
				last[k] = bi + 1
				touched = append(touched, k)
			}
		}
		if len(touched) < 2 {
			continue
		}
		blocks++
		for _, k := range touched {
			perPkg[k]++
		}
	}
	return blocks, perPkg, nil
}
