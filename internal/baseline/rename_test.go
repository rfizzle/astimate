package baseline

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
)

// moverSource is a Go file of package name long enough that a rename that
// changes only its package clause is detected as one.
func moverSource(name string) string {
	return funcsSource(name, "One", "Two", "Three", "Four", "Five", "Six")
}

// funcsSource is a Go file of package name declaring one function per name
// in funcs.
func funcsSource(name string, funcs ...string) string {
	body := "package " + name + "\n\n"
	for _, f := range funcs {
		body += "// " + f + " returns its name.\nfunc " + f + "() string {\n\ts := \"" + f + "\"\n\treturn s + s\n}\n\n"
	}
	return body
}

// TestFindRenamesAcrossEdits follows a file renamed in one commit and
// edited in a later one, and code moved out of a file that kept its path
// into a new package, which git lists as a copy.
func TestFindRenamesAcrossEdits(t *testing.T) {
	t.Parallel()

	r := newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	greek := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"}
	colors := []string{"Red", "Green", "Blue", "Cyan", "Magenta", "Yellow"}
	r.write("f/old.go", funcsSource("f", greek...))
	r.write("g/big.go", moverSource("g"))
	r.write("u/same.go", funcsSource("u", colors...))
	base := r.commit("base")

	if err := os.MkdirAll(filepath.Join(r.dir, "h"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("mv", "f/old.go", "h/old.go")
	r.commit("move")
	// The moved file is edited after the move: its package clause and two
	// more functions.
	r.write("h/old.go", funcsSource("h", append(greek, "Eta", "Theta")...))
	r.commit("edit")
	// Four of g's six functions move out to g/part; big.go keeps its path.
	r.write("g/big.go", funcsSource("g", "One", "Two"))
	r.write("g/part/part.go", funcsSource("part", "Three", "Four", "Five", "Six"))
	// A copy of a file no commit modified is not followed: git lists no
	// copy from an unchanged source.
	r.write("v/same.go", funcsSource("v", colors...))
	r.commit("split")

	got, err := FindRenames(context.Background(), r.dir, base, golang.New(), Head{})
	if err != nil {
		t.Fatalf("FindRenames: %v", err)
	}
	want := Renames{
		"h":      {{Dir: "f", Files: 1}},
		"g/part": {{Dir: "g", Files: 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindRenames = %+v, want %+v", got, want)
	}
}

func TestFindRenames(t *testing.T) {
	t.Parallel()

	r := newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	r.write("a/x.go", moverSource("a"))
	r.write("a/y.go", moverSource("a"))
	r.write("a/a_test.go", moverSource("a"))
	r.write("b/z.go", moverSource("b"))
	r.write("c/w.go", moverSource("c"))
	r.write("d/v.go", moverSource("d"))
	base := r.commit("base")

	mv := func(from, to string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(r.dir, filepath.Dir(to)), 0o755); err != nil {
			t.Fatal(err)
		}
		r.git("mv", from, to)
	}
	// Two files of a and one of b move into a/sub; a test file of a moves
	// alone into t; a file of c moves into a nested module.
	mv("a/x.go", "a/sub/x.go")
	mv("a/y.go", "a/sub/y.go")
	mv("b/z.go", "a/sub/z.go")
	mv("a/a_test.go", "t/a_test.go")
	r.write("nested/go.mod", "module example.com/nested\n")
	mv("c/w.go", "nested/w.go")
	r.commit("move")
	// A staged but uncommitted rename is a rename against the working
	// tree too.
	mv("d/v.go", "e/v.go")

	got, err := FindRenames(context.Background(), r.dir, base, golang.New(), Head{})
	if err != nil {
		t.Fatalf("FindRenames: %v", err)
	}
	want := Renames{
		"a/sub": {{Dir: "a", Files: 2}, {Dir: "b", Files: 1}},
		"e":     {{Dir: "d", Files: 1}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindRenames = %+v, want %+v", got, want)
	}

	staged, err := FindRenames(context.Background(), r.dir, base, golang.New(), Head{Staged: true})
	if err != nil {
		t.Fatalf("FindRenames staged: %v", err)
	}
	if !reflect.DeepEqual(staged, want) {
		t.Errorf("FindRenames staged = %+v, want %+v", staged, want)
	}

	if _, err := FindRenames(context.Background(), r.dir, "-x", golang.New(), Head{}); err == nil {
		t.Error("FindRenames accepted a merge-base starting with a dash")
	}
}

// rowsBaseline returns a snapshot over rows, keyed by directory.
func rowsBaseline(rows map[string]Row) *snapshot {
	return &snapshot{ref: "base", tokenizer: "est", rows: rows}
}

func TestSeeded(t *testing.T) {
	t.Parallel()

	fa := metrics.FunctionInfo{Name: "A", Fingerprint: 1}
	fb := metrics.FunctionInfo{Name: "B", Fingerprint: 2}
	cross := 4
	base := rowsBaseline(map[string]Row{
		"a":                 {Metrics: metrics.RawMetrics{SLOC: 300, DupBlocks: 3, HasTests: true, InternalImports: 2, FanIn: 5}, Functions: []metrics.FunctionInfo{fa}, HasFunctions: true},
		"b":                 {Metrics: metrics.RawMetrics{SLOC: 50}, Functions: []metrics.FunctionInfo{fb}, HasFunctions: true},
		"kept":              {Metrics: metrics.RawMetrics{SLOC: 10}},
		metrics.ModuleRowID: {Metrics: metrics.RawMetrics{DupBlocksCrossPkg: &cross}},
	})
	renames := Renames{
		"a/sub":  {{Dir: "a", Files: 2}, {Dir: "b", Files: 1}},
		"kept":   {{Dir: "a", Files: 1}},
		"orphan": {{Dir: "gone", Files: 3}},
	}
	// a imports a/sub at head; b does not.
	imports := func(from, to string) bool { return from == "a" && to == "a/sub" }
	s := Seeded(base, renames, imports)

	sub, ok := s.Row("a/sub")
	if !ok {
		t.Fatal("Row(a/sub) not found, want a row seeded from a and b")
	}
	// The source most files came from gives the metrics; the edge from a
	// to the package carved out of it adds to the seeded fan_in.
	wantMetrics := metrics.RawMetrics{SLOC: 300, DupBlocks: 3, HasTests: true, InternalImports: 2, FanIn: 6}
	if !reflect.DeepEqual(sub.Metrics, wantMetrics) {
		t.Errorf("Row(a/sub).Metrics = %+v, want %+v", sub.Metrics, wantMetrics)
	}
	if !sub.HasFunctions || !slices.Equal(sub.Functions, []metrics.FunctionInfo{fa, fb}) {
		t.Errorf("Row(a/sub) functions = %+v, %v, want A and B", sub.Functions, sub.HasFunctions)
	}
	if !slices.Equal(sub.From, []string{"a", "b"}) {
		t.Errorf("Row(a/sub).From = %q, want [a b]", sub.From)
	}

	// a imports the package carved out of it, which is not a rise in its
	// internal_imports; b does not import it.
	if r, _ := s.Row("a"); r.Metrics.InternalImports != 3 || r.From != nil {
		t.Errorf("Row(a) = %+v, want internal_imports 3 and no From", r)
	}
	if r, _ := s.Row("b"); r.Metrics.InternalImports != 0 {
		t.Errorf("Row(b).Metrics.InternalImports = %d, want 0", r.Metrics.InternalImports)
	}
	// A package with a row of its own keeps it.
	if r, _ := s.Row("kept"); r.Metrics.SLOC != 10 || r.From != nil {
		t.Errorf("Row(kept) = %+v, want its own row", r)
	}
	// A package whose files came from no baseline package is new.
	if _, ok := s.Row("orphan"); ok {
		t.Error("Row(orphan) found, want none: its source has no row")
	}
	if r, ok := s.Row(metrics.ModuleRowID); !ok || r.Metrics.DupBlocksCrossPkg != &cross {
		t.Errorf("module row = %+v, %v, want the baseline's", r, ok)
	}
	if s.Ref() != "base" || s.Tokenizer() != "est" {
		t.Errorf("Ref, Tokenizer = %q, %q, want the baseline's", s.Ref(), s.Tokenizer())
	}
	if got := Seeded(base, nil, nil); got != Baseline(base) {
		t.Error("Seeded with no renames did not return the baseline itself")
	}
}

// TestSeededMixedNamed seeds from two sources, one of which recorded no
// duplicate blocks or untested exports: the seeded row records none
// rather than reading the other source's as the whole baseline.
func TestSeededMixedNamed(t *testing.T) {
	t.Parallel()

	withNamed := Row{Named: Named{DupBlocks: []uint64{2, 1}, UntestedExports: []string{"F"}}, HasNamed: true}
	base := rowsBaseline(map[string]Row{"a": withNamed, "b": {}, "c": withNamed})
	s := Seeded(base, Renames{
		"mixed": {{Dir: "a", Files: 2}, {Dir: "b", Files: 1}},
		"named": {{Dir: "a", Files: 1}, {Dir: "c", Files: 1}},
	}, nil)

	if r, ok := s.Row("mixed"); !ok || r.HasNamed || len(r.Named.DupBlocks)+len(r.Named.UntestedExports) != 0 {
		t.Errorf("Row(mixed) = %+v, %v, want a row without named details", r, ok)
	}
	r, ok := s.Row("named")
	if !ok || !r.HasNamed || !slices.Equal(r.Named.DupBlocks, []uint64{1, 2}) ||
		!slices.Equal(r.Named.UntestedExports, []string{"F", "F"}) {
		t.Errorf("Row(named) = %+v, %v, want both sources' details", r, ok)
	}
}

// TestSeededImporters exempts from internal_imports the edge to a package
// carved out in this change of a package that imported one of its sources
// at the baseline and still imports it at head.
func TestSeededImporters(t *testing.T) {
	t.Parallel()

	two := metrics.RawMetrics{InternalImports: 2}
	base := rowsBaseline(map[string]Row{
		"a": {}, "user": {Metrics: two}, "swap": {Metrics: two}, "fresh": {Metrics: two}, "other": {Metrics: two},
	})
	base.importers = map[string][]string{"a": {"other", "swap", "user"}}
	base.importersKnown = true
	// At head user imports a and a/sub, swap only a/sub, fresh only a/sub
	// (it never imported a), other only a.
	head := map[[2]string]bool{
		{"a", "a/sub"}: true, {"user", "a/sub"}: true, {"user", "a"}: true,
		{"swap", "a/sub"}: true, {"fresh", "a/sub"}: true, {"other", "a"}: true,
	}
	s := Seeded(base, Renames{"a/sub": {{Dir: "a", Files: 1}}}, func(from, to string) bool { return head[[2]string{from, to}] })

	for dir, want := range map[string]int{"a": 1, "user": 3, "swap": 2, "fresh": 2, "other": 2} {
		if r, _ := s.Row(dir); r.Metrics.InternalImports != want {
			t.Errorf("Row(%s).Metrics.InternalImports = %d, want %d", dir, r.Metrics.InternalImports, want)
		}
	}
	// Without a recorded import graph only the source's edge is exempt.
	base.importersKnown, base.importers = false, nil
	s = Seeded(base, Renames{"a/sub": {{Dir: "a", Files: 1}}}, func(from, to string) bool { return head[[2]string{from, to}] })
	if r, _ := s.Row("user"); r.Metrics.InternalImports != 2 {
		t.Errorf("without a graph Row(user).Metrics.InternalImports = %d, want 2", r.Metrics.InternalImports)
	}
}

// TestFileKeyMigration reads a file written before package_rows existed,
// keyed by import path only, and one that records a package_path.
func TestFileKeyMigration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		wantDirs    []string
		wantMissing []string
		importOnly  bool
	}{
		{
			name: "import path only",
			body: `{"ref": "abc", "module_path": "example.com/m",
				"packages": {"example.com/m": {"files": 1}, "example.com/m/a": {"files": 2}, "<module>": {}},
				"functions": {"example.com/m/a": [{"name": "F", "fingerprint": "00000000000000ff", "cognitive": 1}]}}`,
			wantDirs:    []string{".", "a", metrics.ModuleRowID},
			wantMissing: []string{"example.com/m/a"},
			importOnly:  true,
		},
		{
			name:     "module row only",
			body:     `{"ref": "abc", "module_path": "example.com/m", "packages": {"<module>": {}}}`,
			wantDirs: []string{metrics.ModuleRowID},
		},
		{
			name: "package_path recorded",
			body: `{"ref": "abc", "module_path": "example.com/old",
				"packages": {"example.com/old/a": {"files": 2}},
				"package_rows": {"example.com/old/a": {"package_path": "moved/a"}}}`,
			wantDirs:    []string{"moved/a"},
			wantMissing: []string{"a"},
		},
	}
	dir := t.TempDir()
	for i, tt := range tests {
		path := filepath.Join(dir, "b"+string(rune('0'+i))+".json")
		if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := FromFile(path)
			if err != nil {
				t.Fatalf("FromFile: %v", err)
			}
			if got := KeyedByImportPath(b); got != tt.importOnly {
				t.Errorf("KeyedByImportPath = %v, want %v", got, tt.importOnly)
			}
			for _, d := range tt.wantDirs {
				if _, ok := b.Row(d); !ok {
					t.Errorf("Row(%q) not found", d)
				}
			}
			for _, d := range tt.wantMissing {
				if _, ok := b.Row(d); ok {
					t.Errorf("Row(%q) found, want none", d)
				}
			}
		})
	}
	if r, _ := mustFromFile(t, filepath.Join(dir, "b0.json")).Row("a"); !r.HasFunctions || len(r.Functions) != 1 {
		t.Errorf("Row(a) functions = %+v, %v, want F under the directory key", r.Functions, r.HasFunctions)
	}
}

// mustFromFile reads the baseline file at path, failing t on error.
func mustFromFile(t *testing.T, path string) Baseline {
	t.Helper()
	b, err := FromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestFromGitModulePathChange checks that a baseline extracted at a commit
// whose go.mod declared another module path still keys every package by
// its directory, so head matches it.
func TestFromGitModulePathChange(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	r := newRepo(t, "master")
	r.write("go.mod", "module example.com/m\n\ngo 1.27\n")
	r.write("root.go", "package m\n\nimport _ \"example.com/m/a\"\n")
	r.write("a/a.go", "package a\n\nvar X = 1\n")
	first := r.commit("first")
	r.write("go.mod", "module example.com/m/v2\n\ngo 1.27\n")
	r.write("root.go", "package m\n\nimport _ \"example.com/m/v2/a\"\n")
	r.commit("v2")

	b, err := FromGit(context.Background(), r.dir, first, golang.New(), golang.ModulePath, "est")
	if err != nil {
		t.Fatalf("FromGit: %v", err)
	}
	for _, dir := range []string{".", "a"} {
		if _, ok := b.Row(dir); !ok {
			t.Errorf("Row(%q) not found", dir)
		}
	}
	if got, ok := b.Importers("a"); !ok || !slices.Equal(got, []string{"."}) {
		t.Errorf("Importers(a) = %q, %v, want the root package by directory", got, ok)
	}
}
