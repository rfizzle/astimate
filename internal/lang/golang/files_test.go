package golang

import (
	"errors"
	"go/ast"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// countingFiles is a fileSource that counts the reads (opens) and length
// lookups of each file before passing them to next.
type countingFiles struct {
	next           fileSource
	reads, lengths map[string]int
}

func newCountingFiles() *countingFiles {
	return &countingFiles{next: osFiles{}, reads: make(map[string]int), lengths: make(map[string]int)}
}

func (c *countingFiles) read(name string) ([]byte, error) {
	c.reads[name]++
	return c.next.read(name)
}

func (c *countingFiles) length(name string) (int64, error) {
	c.lengths[name]++
	return c.next.length(name)
}

// TestAssembleReadsEachFileOnce extracts fixture tested, which has non-test,
// in-package test and external test files, with both token counters. size,
// duplication and tokens together must open each non-test file exactly once
// and touch each _test.go file once: opened by the o200k counter, which
// needs its text, and only sized by the ratio counter. The module-wide
// cross-package pass, which runs once per load and is checked by
// TestCrossDuplicationReadsEachFileOnce, is run first so it is out of the
// count.
func TestAssembleReadsEachFileOnce(t *testing.T) {
	l := loadFixture(t)
	p := l.pkgs["example.com/fixture/tested"]
	if _, err := crossDuplication(l, osFiles{}, defaultDupOptions()); err != nil {
		t.Fatalf("crossDuplication: %v", err)
	}
	for _, tc := range []struct {
		counter   tokenCounter
		readTests bool
	}{
		{newRatioCounter(defaultCharsPerToken), false},
		{newO200kForTest(t), true},
	} {
		t.Run(tc.counter.Method(), func(t *testing.T) {
			files := newCountingFiles()
			opts := assembleOptions{counter: tc.counter, dup: defaultDupOptions(), files: files}
			if _, err := assemble(t.Context(), l, p, opts); err != nil {
				t.Fatalf("assemble: %v", err)
			}
			for _, name := range p.GoFiles {
				if files.reads[name] != 1 || files.lengths[name] != 0 {
					t.Errorf("non-test file %s: %d reads, %d length lookups, want 1 and 0",
						filepath.Base(name), files.reads[name], files.lengths[name])
				}
			}
			tests := 0
			touched := maps.Clone(files.reads)
			maps.Copy(touched, files.lengths)
			for name := range touched {
				if !strings.HasSuffix(name, "_test.go") {
					if !slices.Contains(p.GoFiles, name) {
						t.Errorf("touched %s, which is not a file of the package", name)
					}
					continue
				}
				tests++
				want := [2]int{0, 1}
				if tc.readTests {
					want = [2]int{1, 0}
				}
				if got := [2]int{files.reads[name], files.lengths[name]}; got != want {
					t.Errorf("test file %s: reads and length lookups %v, want %v", filepath.Base(name), got, want)
				}
			}
			if tests < 2 {
				t.Errorf("touched %d _test.go files, want the in-package and external test files", tests)
			}
		})
	}
}

// TestCrossDuplicationReadsEachFileOnce runs the first extraction of a load,
// which also runs the module-wide cross-package pass: every non-test file of
// the package and every non-test, non-generated file of the rest of the
// module is opened exactly once, and no other file of the module is. A
// second extraction reuses the memoized pass and opens only its own
// package's files.
func TestCrossDuplicationReadsEachFileOnce(t *testing.T) {
	l := loadFixture(t)
	opts := func(files fileSource) assembleOptions {
		return assembleOptions{counter: newRatioCounter(defaultCharsPerToken), dup: defaultDupOptions(), files: files}
	}
	first := newCountingFiles()
	if _, err := assemble(t.Context(), l, l.pkgs["example.com/fixture/tested"], opts(first)); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for _, path := range l.paths {
		for _, f := range sourceSyntax(l, l.pkgs[path]) {
			name := l.fset.File(f.FileStart).Name()
			want := 1
			if ast.IsGenerated(f) && path != "example.com/fixture/tested" {
				want = 0 // not duplication input, and not a file of tested
			}
			if first.reads[name] != want {
				t.Errorf("first extraction opened %s %d times, want %d", filepath.Base(name), first.reads[name], want)
			}
		}
	}
	for name, n := range first.reads {
		if strings.HasSuffix(name, "_test.go") {
			t.Errorf("first extraction opened test file %s %d times, want 0", filepath.Base(name), n)
		}
	}

	second := newCountingFiles()
	p := l.pkgs["example.com/fixture/dupes"]
	if _, err := assemble(t.Context(), l, p, opts(second)); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for name := range second.reads {
		if !slices.Contains(p.GoFiles, name) && !strings.HasSuffix(name, "_test.go") {
			t.Errorf("second extraction opened %s, which is not a file of the package", filepath.Base(name))
		}
	}
}

// failingFiles is a fileSource whose reads fail until ok is set.
type failingFiles struct {
	ok             bool
	reads, lengths int
}

var errNotYet = errors.New("not yet")

func (f *failingFiles) read(string) ([]byte, error) {
	f.reads++
	if !f.ok {
		return nil, errNotYet
	}
	return []byte("package x\n"), nil
}

func (f *failingFiles) length(string) (int64, error) {
	f.lengths++
	return 99, nil
}

func TestFileCache(t *testing.T) {
	next := &failingFiles{}
	c := newFileCache(next)
	if n, err := c.length("x.go"); err != nil || n != 99 || next.lengths != 1 {
		t.Errorf("length before read = %d, %v after %d lookups, want next's 99 after 1", n, err, next.lengths)
	}
	if _, err := c.read("x.go"); !errors.Is(err, errNotYet) {
		t.Fatalf("first read error = %v, want %v", err, errNotYet)
	}
	next.ok = true
	for range 2 {
		data, err := c.read("x.go")
		if err != nil || string(data) != "package x\n" {
			t.Fatalf("read = %q, %v, want the file", data, err)
		}
	}
	if next.reads != 2 {
		t.Errorf("underlying reads = %d, want 2: one failed, one cached", next.reads)
	}
	if n, err := c.length("x.go"); err != nil || n != int64(len("package x\n")) || next.lengths != 1 {
		t.Errorf("length after read = %d, %v after %d lookups, want the cached length without a lookup", n, err, next.lengths)
	}
}
