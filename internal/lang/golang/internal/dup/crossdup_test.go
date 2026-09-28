package dup

import (
	"errors"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// crossBody returns a function of well over 40 normalized tokens, the same
// for every name, so copies of it under different names are one block.
func crossBody(name string) string {
	return "func " + name + `(s string) int {
	sum := 7
	for i := 0; i < len(s); i++ {
		sum = (sum*31 + int(s[i])) % 1000003
		if sum < 0 {
			sum = -sum
		}
	}
	return sum
}
`
}

// otherBody returns a second function of over 40 normalized tokens that
// shares no 40-token run with crossBody.
func otherBody(name string) string {
	return "func " + name + `(xs []string, sep string) (out string, n int) {
	switch len(xs) {
	case 0:
		return "", 0
	case 1:
		return xs[0], 1
	}
	for _, x := range xs {
		out += x + sep
		n++
	}
	return out, n
}
`
}

// TestCrossPackageAttribution scans synthetic files, each tagged with a
// package index, into one stream and checks which blocks count as
// cross-package and how they are attributed. Each copy of a body is
// preceded by a different token (a package name, a literal, a closing
// brace), so no copy extends into a longer repeat and every body is
// exactly one block.
func TestCrossPackageAttribution(t *testing.T) {
	type file struct {
		pkg int32
		src string
	}
	cases := []struct {
		name       string
		npkg       int
		files      []file
		wantBlocks int
		wantPerPkg []int
	}{
		{
			name: "one block across two packages",
			npkg: 2,
			files: []file{
				{0, "package a\n\n" + crossBody("A")},
				{1, "package b\n\nconst k = 1\n\n" + crossBody("B")},
			},
			wantBlocks: 1, wantPerPkg: []int{1, 1},
		},
		{
			name: "a repeat inside one package is not cross-package",
			npkg: 2,
			files: []file{
				{0, "package a\n\n" + crossBody("A") + "\nconst k = 1\n\n" + crossBody("B")},
				{1, "package b\n\n" + otherBody("C")},
			},
			wantBlocks: 0, wantPerPkg: []int{0, 0},
		},
		{
			name: "two files of one package do not make it cross-package",
			npkg: 1,
			files: []file{
				{0, "package a\n\n" + crossBody("A")},
				{0, "package a\n\nconst k = 1\n\n" + crossBody("B")},
			},
			wantBlocks: 0, wantPerPkg: []int{0},
		},
		{
			name: "a package with several occurrences counts the block once",
			npkg: 3,
			files: []file{
				{0, "package a\n\n" + crossBody("A") + "\nconst k = 1\n\n" + crossBody("B")},
				{1, "package b\n\ntype t struct{}\n\n" + crossBody("C")},
				{2, "package c\n\n" + otherBody("D")},
			},
			wantBlocks: 1, wantPerPkg: []int{1, 1, 0},
		},
		{
			name: "three packages share one block, two share another",
			npkg: 3,
			files: []file{
				{0, "package a\n\n" + crossBody("A") + "\nconst k = 1\n\n" + otherBody("B")},
				{1, "package b\n\nvar v = 2\n\n" + crossBody("C") + "\ntype t int\n\n" + otherBody("D")},
				{2, "package c\n\ntype t struct{}\n\n" + crossBody("E")},
			},
			wantBlocks: 2, wantPerPkg: []int{2, 2, 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := DefaultOptions()
			var s duptok.Stream
			z := newDupTokenizer(opts)
			fs := token.NewFileSet()
			filePkg := make([]int32, 0, len(tc.files))
			for i, f := range tc.files {
				if err := z.scan(fs, "f"+strconv.Itoa(i)+".go", []byte(f.src), &s); err != nil {
					t.Fatalf("scan: %v", err)
				}
				filePkg = append(filePkg, f.pkg)
			}
			blocks, perPkg, err := crossPackage(&s, filePkg, tc.npkg, opts)
			if err != nil {
				t.Fatalf("crossPackage: %v", err)
			}
			if len(blocks) != tc.wantBlocks || !slices.Equal(perPkg, tc.wantPerPkg) {
				t.Errorf("blocks, perPkg = %d, %v, want %d, %v", len(blocks), perPkg, tc.wantBlocks, tc.wantPerPkg)
			}
		})
	}

	t.Run("package table must cover the stream", func(t *testing.T) {
		var s duptok.Stream
		if err := newDupTokenizer(DefaultOptions()).scan(token.NewFileSet(), "f.go", []byte("package a\n"), &s); err != nil {
			t.Fatal(err)
		}
		if _, _, err := crossPackage(&s, nil, 1, DefaultOptions()); err == nil {
			t.Error("crossPackage with no package table returned no error")
		}
		bad := DefaultOptions()
		bad.MinTokens = 0
		if _, _, err := crossPackage(&s, []int32{0}, 1, bad); err == nil {
			t.Error("crossPackage with minTokens 0 returned no error")
		}
	})
}

// TestCrossDuplicationFixture checks the module pass on the fixture: a and
// b share Checksum and Digest, one 65-token block, and nothing else is
// shared across packages. dupes' three copies are within one package.
func TestCrossDuplicationFixture(t *testing.T) {
	l := loadFixture(t)
	var memo Memo
	got, err := memo.Cross(l, load.OSFiles{}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"example.com/fixture/a": 1, "example.com/fixture/b": 1}
	if len(got.Blocks) != 1 || len(got.PerPkg) != len(want) {
		t.Fatalf("blocks, perPkg = %d, %v, want 1, %v", len(got.Blocks), got.PerPkg, want)
	}
	for path, n := range want {
		if got.PerPkg[path] != n {
			t.Errorf("%s: dup_blocks_cross_pkg = %d, want %d", path, got.PerPkg[path], n)
		}
	}
	again, err := memo.Cross(l, failing{}, DefaultOptions())
	if err != nil || len(again.Blocks) != len(got.Blocks) {
		t.Errorf("second call = %d, %v, want the memoized %d without reading", len(again.Blocks), err, len(got.Blocks))
	}
	short := DefaultOptions()
	short.MinTokens = 1000
	other, err := memo.Cross(l, load.OSFiles{}, short)
	if err != nil || len(other.Blocks) != 0 {
		t.Errorf("with min_tokens 1000 = %d, %v, want 0: options must not share a memo", len(other.Blocks), err)
	}
	bad := DefaultOptions()
	bad.MinTokens = 0
	if _, err := memo.Cross(l, load.OSFiles{}, bad); err == nil || !strings.Contains(err.Error(), "not positive") {
		t.Errorf("min_tokens 0: error = %v, want one saying it is not positive", err)
	}
}

// failing is a load.FileSource whose every call fails, for checking that a
// memoized result reads nothing.
type failing struct{}

func (failing) Read(string) ([]byte, error) { return nil, errNotYet }

func (failing) Length(string) (int64, error) { return 0, errNotYet }

// BenchmarkCrossDuplication runs the module-wide pass, unmemoized, over the
// fixture and over this repository's own module, and the opacity flags,
// which run per file, over every package of the repository.
func BenchmarkCrossDuplication(b *testing.B) {
	fixture := fixtureRoot(b)
	roots := map[string]string{
		"fixture": fixture,
		"module":  filepath.Dir(filepath.Dir(filepath.Dir(fixture))),
	}
	for _, name := range []string{"fixture", "module"} {
		l, err := load.Load(&packages.Config{Dir: roots[name]}, packages.Load)
		if err != nil {
			b.Fatalf("loading %s: %v", name, err)
		}
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if _, err := computeCross(l, load.OSFiles{}, DefaultOptions()); err != nil {
					b.Fatal(err)
				}
			}
		})
		if name != "module" {
			continue
		}
		b.Run("opacity", func(b *testing.B) {
			for b.Loop() {
				for _, path := range l.Paths {
					_ = inspect.Opacity(l, l.Pkgs[path])
				}
			}
		})
	}
}

// errNotYet is what failing returns.
var errNotYet = errors.New("not yet")

// TestCrossBlocksOf checks that BlocksOf filters by package and copies.
func TestCrossBlocksOf(t *testing.T) {
	occ := func(pkg string) metrics.Occurrence {
		return metrics.Occurrence{Package: pkg, File: pkg + ".go", StartLine: 1, EndLine: 2}
	}
	c := Cross{Blocks: []metrics.CrossBlock{
		{Occurrences: []metrics.Occurrence{occ("a"), occ("b")}},
		{Occurrences: []metrics.Occurrence{occ("b"), occ("c")}},
	}}
	if got := c.BlocksOf("a"); len(got) != 1 || got[0].Occurrences[1].Package != "b" {
		t.Errorf("BlocksOf(a) = %+v, want the a-b block", got)
	}
	if got := c.BlocksOf("d"); got != nil {
		t.Errorf("BlocksOf(d) = %+v, want nil", got)
	}
	all := c.BlocksOf("")
	if len(all) != 2 {
		t.Fatalf("BlocksOf(\"\") = %d blocks, want 2", len(all))
	}
	all[0].Occurrences[0].File = "changed"
	if c.Blocks[0].Occurrences[0].File != "a.go" {
		t.Error("BlocksOf returned blocks that share occurrences with c")
	}
}

func TestApplies(t *testing.T) {
	for path, want := range map[string]bool{"example.com/m": true, load.StdModulePath: false, load.StdAllModulePath: false} {
		if got := Applies(&load.Module{Path: path}); got != want {
			t.Errorf("Applies(%q) = %v, want %v", path, got, want)
		}
	}
}
