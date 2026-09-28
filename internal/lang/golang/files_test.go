package golang

import (
	"go/ast"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/dup"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
)

// countingFiles is a load.FileSource that counts the reads (opens) and length
// lookups of each file before passing them to next.
type countingFiles struct {
	next           load.FileSource
	reads, lengths map[string]int
}

func newCountingFiles() *countingFiles {
	return &countingFiles{next: load.OSFiles{}, reads: make(map[string]int), lengths: make(map[string]int)}
}

func (c *countingFiles) Read(name string) ([]byte, error) {
	c.reads[name]++
	return c.next.Read(name)
}

func (c *countingFiles) Length(name string) (int64, error) {
	c.lengths[name]++
	return c.next.Length(name)
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
	p := l.Pkgs["example.com/fixture/tested"]
	if _, err := l.cross.Cross(l.Module, load.OSFiles{}, dup.DefaultOptions()); err != nil {
		t.Fatalf("Cross: %v", err)
	}
	for _, tc := range []struct {
		counter   inspect.Counter
		readTests bool
	}{
		{inspect.NewRatioCounter(inspect.DefaultCharsPerToken), false},
		{newO200kForTest(t), true},
	} {
		t.Run(tc.counter.Method(), func(t *testing.T) {
			files := newCountingFiles()
			opts := assembleOptions{counter: tc.counter, dup: dup.DefaultOptions(), files: files}
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
	opts := func(files load.FileSource) assembleOptions {
		return assembleOptions{counter: inspect.NewRatioCounter(inspect.DefaultCharsPerToken), dup: dup.DefaultOptions(), files: files}
	}
	first := newCountingFiles()
	if _, err := assemble(t.Context(), l, l.Pkgs["example.com/fixture/tested"], opts(first)); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for _, path := range l.Paths {
		for _, f := range l.SourceSyntax(l.Pkgs[path]) {
			name := l.Fset.File(f.FileStart).Name()
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
	p := l.Pkgs["example.com/fixture/dupes"]
	if _, err := assemble(t.Context(), l, p, opts(second)); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	for name := range second.reads {
		if !slices.Contains(p.GoFiles, name) && !strings.HasSuffix(name, "_test.go") {
			t.Errorf("second extraction opened %s, which is not a file of the package", filepath.Base(name))
		}
	}
}
