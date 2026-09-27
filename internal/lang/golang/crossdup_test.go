package golang

import (
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/duptok"
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
			opts := defaultDupOptions()
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
			if blocks != tc.wantBlocks || !slices.Equal(perPkg, tc.wantPerPkg) {
				t.Errorf("blocks, perPkg = %d, %v, want %d, %v", blocks, perPkg, tc.wantBlocks, tc.wantPerPkg)
			}
		})
	}

	t.Run("package table must cover the stream", func(t *testing.T) {
		var s duptok.Stream
		if err := newDupTokenizer(defaultDupOptions()).scan(token.NewFileSet(), "f.go", []byte("package a\n"), &s); err != nil {
			t.Fatal(err)
		}
		if _, _, err := crossPackage(&s, nil, 1, defaultDupOptions()); err == nil {
			t.Error("crossPackage with no package table returned no error")
		}
		bad := defaultDupOptions()
		bad.minTokens = 0
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
	got, err := crossDuplication(l, osFiles{}, defaultDupOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"example.com/fixture/a": 1, "example.com/fixture/b": 1}
	if got.blocks != 1 || len(got.perPkg) != len(want) {
		t.Fatalf("blocks, perPkg = %d, %v, want 1, %v", got.blocks, got.perPkg, want)
	}
	for path, n := range want {
		if got.perPkg[path] != n {
			t.Errorf("%s: dup_blocks_cross_pkg = %d, want %d", path, got.perPkg[path], n)
		}
	}
	again, err := crossDuplication(l, failing{}, defaultDupOptions())
	if err != nil || again.blocks != got.blocks {
		t.Errorf("second call = %d, %v, want the memoized %d without reading", again.blocks, err, got.blocks)
	}
	short := defaultDupOptions()
	short.minTokens = 1000
	other, err := crossDuplication(l, osFiles{}, short)
	if err != nil || other.blocks != 0 {
		t.Errorf("with min_tokens 1000 = %d, %v, want 0: options must not share a memo", other.blocks, err)
	}
	bad := defaultDupOptions()
	bad.minTokens = 0
	if _, err := crossDuplication(l, osFiles{}, bad); err == nil || !strings.Contains(err.Error(), "not positive") {
		t.Errorf("min_tokens 0: error = %v, want one saying it is not positive", err)
	}
}

// failing is a fileSource whose every call fails, for checking that a
// memoized result reads nothing.
type failing struct{}

func (failing) read(string) ([]byte, error)  { return nil, errNotYet }
func (failing) length(string) (int64, error) { return 0, errNotYet }

func TestModuleRow(t *testing.T) {
	var ext metrics.Extractor = New()
	mm, ok := ext.(metrics.ModuleMetrics)
	if !ok {
		t.Fatal("the Go extractor does not implement metrics.ModuleMetrics")
	}
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	row, err := mm.ModuleRow(t.Context(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Validate(); err != nil {
		t.Errorf("module row: %v", err)
	}
	if row.DupBlocksCrossPkg == nil || *row.DupBlocksCrossPkg != 1 {
		t.Errorf("module dup_blocks_cross_pkg = %v, want 1", row.DupBlocksCrossPkg)
	}
	want := metrics.RawMetrics{DupBlocksCrossPkg: row.DupBlocksCrossPkg}
	if row != want {
		t.Errorf("module row = %+v, want only dup_blocks_cross_pkg set", row)
	}

	// The module row counts distinct blocks; the packages each count it.
	sum := 0
	for _, pkg := range fixturePackages() {
		m, err := ext.Extract(t.Context(), mod, pkg)
		if err != nil {
			t.Fatal(err)
		}
		sum += *m.DupBlocksCrossPkg
	}
	if sum != 2 {
		t.Errorf("sum of per-package dup_blocks_cross_pkg = %d, want 2", sum)
	}
}

// TestCrossDuplicationStdlibNull checks that the standard-library loads,
// which are not modules, leave dup_blocks_cross_pkg null while still
// reporting the opacity flags.
func TestCrossDuplicationStdlibNull(t *testing.T) {
	m := extractStdlibOrSkip(t, "reflect")
	if m.DupBlocksCrossPkg != nil {
		t.Errorf("reflect: dup_blocks_cross_pkg = %d, want null", *m.DupBlocksCrossPkg)
	}
	if m.UsesReflect == nil || !*m.UsesReflect {
		t.Errorf("reflect: uses_reflect = %v, want true (it imports unsafe)", m.UsesReflect)
	}
}

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
		l, err := loadModule(&packages.Config{Dir: roots[name]}, packages.Load)
		if err != nil {
			b.Fatalf("loading %s: %v", name, err)
		}
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if _, err := computeCrossDup(l, osFiles{}, defaultDupOptions()); err != nil {
					b.Fatal(err)
				}
			}
		})
		if name != "module" {
			continue
		}
		b.Run("opacity", func(b *testing.B) {
			for b.Loop() {
				for _, path := range l.paths {
					_ = opacity(l, l.pkgs[path])
				}
			}
		})
	}
}
