package typescript

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

// writeTree writes files, keyed by slash path relative to root, under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
	}
}

func TestLanguage(t *testing.T) {
	if got := New().Language(); got != "typescript" {
		t.Fatalf("Language() = %q, want %q", got, "typescript")
	}
}

func TestDetect(t *testing.T) {
	root := t.TempDir()
	e := New()
	if e.Detect(root) {
		t.Error("Detect on an empty directory = true")
	}
	writeTree(t, root, map[string]string{"package.json/x": "not a file"})
	if e.Detect(root) {
		t.Error("Detect with a package.json directory = true")
	}
	other := t.TempDir()
	writeTree(t, other, map[string]string{"package.json": "{}"})
	if !e.Detect(other) {
		t.Error("Detect with a package.json = false")
	}
}

func TestPackagesLayout(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":            "{}",
		"index.ts":                "export const r = 1;\n",
		"src/a.ts":                "export const a = 1;\n",
		"src/view.tsx":            "export const v = <div>{1}</div>;\n",
		"types/only.d.ts":         "export declare const d: number;\n",
		"tests/only.test.ts":      "it(\"x\", () => {});\n",
		"src/__tests__/a.test.ts": "it(\"x\", () => {});\n",
		"node_modules/dep/x.ts":   "export const n = 1;\n",
		"dist/x.ts":               "export const d = 1;\n",
		"build/x.ts":              "export const b = 1;\n",
		".cache/x.ts":             "export const c = 1;\n",
		"nested/package.json":     "{}",
		"nested/x.ts":             "export const n = 1;\n",
		"js/plain.js":             "export const j = 1;\n",
		"esm/m.mts":               "export const m = 1;\n",
		"cjs/c.cts":               "export const c = 1;\n",
		"cjs/c.test.cts":          "it(\"c\", () => {});\n",
		"decl/only.d.mts":         "export declare const d: number;\n",
		"decl/only.d.cts":         "export declare const d: number;\n",
		"deep/er/x.ts":            "export const e = 1;\n",
	})
	e := New()
	got, err := e.Packages(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".", "cjs", "deep/er", "esm", "src"}
	if !slices.Equal(got, want) {
		t.Errorf("Packages = %q, want %q", got, want)
	}
	m, err := e.Extract(t.Context(), &metrics.ModuleContext{Root: root}, "src")
	if err != nil {
		t.Fatal(err)
	}
	if m.Files != 2 || m.TestFiles != 1 || m.TestFuncs != 1 {
		t.Errorf("src files, test files, test funcs = %d, %d, %d, want 2, 1, 1", m.Files, m.TestFiles, m.TestFuncs)
	}
}

func TestImportGraph(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json": "{}",
		"a/a.ts":       "import { b } from \"../b/b\";\nimport { x } from \"./x\";\nexport { c1, c2 } from \"../c/c\";\nexport { y } from \"./x\";\nexport const a = b;\n",
		"a/x.ts":       "export const x = 1, y = 2;\n",
		"b/b.ts":       "import \"../c/c\";\nexport const b = 1;\n",
		"b/b.test.ts":  "import { a } from \"../a/a\";\nimport { c1 } from \"../c/c\";\nit(\"b\", () => {});\n",
		"c/c.ts":       "export const c1 = 1, c2 = 2;\n",
	})
	e := New()
	mod := &metrics.ModuleContext{Root: root}
	got := map[string]metrics.RawMetrics{}
	for _, id := range []string{"a", "b", "c"} {
		m, err := e.Extract(t.Context(), mod, id)
		if err != nil {
			t.Fatal(err)
		}
		got[id] = m
	}
	check := func(id, field string, have, want int) {
		t.Helper()
		if have != want {
			t.Errorf("%s: %s = %d, want %d", id, field, have, want)
		}
	}
	// a imports b and, by re-export, c; ./x is intra-package.
	check("a", "internal_imports", got["a"].InternalImports, 2)
	// a's own a, x and y, plus the two names re-exported from c; y is
	// re-exported from a's own package and not counted twice.
	check("a", "exported_symbols", got["a"].ExportedSymbols, 5)
	check("b", "fan_in", got["b"].FanIn, 1)
	check("c", "fan_in", got["c"].FanIn, 2)
	// b's test imports a, which b's source does not: a test-only edge. Its
	// import of c is already a source edge of b.
	check("a", "fan_in_tests", got["a"].FanInTests, 1)
	check("c", "fan_in_tests", got["c"].FanInTests, 0)
}

// TestCouplingRatiosRounded checks that assemble rounds instability and
// abstractness to three decimals and computes main_sequence_distance from
// the unrounded ratios: |1/3 + 1/3 - 1| is 0.333, where the rounded ratios
// would give 0.334.
func TestCouplingRatiosRounded(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json": "{}",
		"p/p.ts":       "import { q } from \"../q/q\";\nexport interface I { n: number }\nexport type T = number;\nexport class C {}\nexport const v = q;\n",
		"q/q.ts":       "export const q = 1;\n",
		"r/r.ts":       "import { v } from \"../p/p\";\nexport const r = v;\n",
		"s/s.ts":       "import { v } from \"../p/p\";\nexport const s = v;\n",
	})
	m, err := New().Extract(t.Context(), &metrics.ModuleContext{Root: root}, "p")
	if err != nil {
		t.Fatal(err)
	}
	if m.FanIn != 2 || m.InternalImports != 1 {
		t.Fatalf("fan_in, internal_imports = %d, %d, want 2, 1", m.FanIn, m.InternalImports)
	}
	for _, r := range []struct {
		name string
		v    *float64
	}{
		{"instability", m.Instability},
		{"abstractness", m.Abstractness},
		{"main_sequence_distance", m.MainSequenceDistance},
	} {
		switch {
		case r.v == nil:
			t.Errorf("%s = null, want 0.333", r.name)
		case *r.v != 0.333:
			t.Errorf("%s = %v, want 0.333", r.name, *r.v)
		}
	}
}

// TestDetailsGlobals checks that Details names and locates the globals of
// the fixture's hidden package, index for index, in the order the count
// reads them.
func TestDetailsGlobals(t *testing.T) {
	d, err := New().Details(t.Context(), &metrics.ModuleContext{Root: fixtureRoot(t)}, "hidden")
	if err != nil {
		t.Fatal(err)
	}
	wantPos := []metrics.Position{
		{File: "hidden.ts", Line: 5}, {File: "hidden.ts", Line: 6},
		{File: "hidden.ts", Line: 6}, {File: "hidden.ts", Line: 7},
	}
	if !slices.Equal(d.GlobalPositions, wantPos) {
		t.Errorf("GlobalPositions = %+v, want %+v", d.GlobalPositions, wantPos)
	}
	if want := []string{"limit", "events", "done", "counter"}; !slices.Equal(d.GlobalNames, want) {
		t.Errorf("GlobalNames = %q, want %q", d.GlobalNames, want)
	}
}

func TestDetails(t *testing.T) {
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	type pos = metrics.Position
	cases := []struct {
		pkg                string
		untested, excluded []string
		dupLocations       []string
		untestedPos        []pos
		largest            string
		files              []string
	}{
		{
			"dupes", []string{"countVisits"}, nil, []string{"dupes.ts:6-23", "dupes.ts:27-44", "dupes.ts:48-65"},
			[]pos{{File: "dupes.ts", Line: 48}}, "dupes.ts", []string{"dupes.ts"},
		},
		{
			"hub", []string{"Counter.add", "clamp", "normalize", "twice"}, nil, nil,
			[]pos{{File: "index.ts", Line: 33}, {File: "index.ts", Line: 12}, {File: "index.ts", Line: 8}, {File: "index.ts", Line: 22}},
			"index.ts", []string{"index.ts"},
		},
		{"tested", nil, nil, nil, nil, "tested.ts", []string{"count.ts", "tested.ts"}},
		{
			"trivial", []string{"Box.close", "answer", "detached", "spaced"}, []string{"Box.open", "listed", "wrapped"}, nil,
			[]pos{{File: "wrappers.ts", Line: 23}, {File: "trivial.ts", Line: 2}, {File: "wrappers.ts", Line: 13}, {File: "wrappers.ts", Line: 9}},
			"wrappers.ts", []string{"trivial.ts", "wrappers.ts"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.pkg, func(t *testing.T) {
			d, err := e.Details(t.Context(), mod, tc.pkg)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(d.UntestedExports, tc.untested) {
				t.Errorf("UntestedExports = %q, want %q", d.UntestedExports, tc.untested)
			}
			if !slices.Equal(d.UntestedExcluded, tc.excluded) {
				t.Errorf("UntestedExcluded = %q, want %q", d.UntestedExcluded, tc.excluded)
			}
			if !slices.Equal(d.DupLocations, tc.dupLocations) {
				t.Errorf("DupLocations = %q, want %q", d.DupLocations, tc.dupLocations)
			}
			if !slices.Equal(d.UntestedPositions, tc.untestedPos) {
				t.Errorf("UntestedPositions = %+v, want %+v", d.UntestedPositions, tc.untestedPos)
			}
			if d.GlobalPositions != nil || d.GlobalNames != nil {
				t.Errorf("GlobalPositions, GlobalNames = %+v, %q, want none", d.GlobalPositions, d.GlobalNames)
			}
			if d.LargestFile != tc.largest {
				t.Errorf("LargestFile = %q, want %q", d.LargestFile, tc.largest)
			}
			if !slices.Equal(d.SourceFiles, tc.files) {
				t.Errorf("SourceFiles = %q, want %q", d.SourceFiles, tc.files)
			}
		})
	}
	if _, err := e.Details(t.Context(), mod, "nope"); !errors.Is(err, metrics.ErrUnknownPackage) {
		t.Errorf("Details of an unknown package = %v, want metrics.ErrUnknownPackage", err)
	}
}

func TestExtractErrors(t *testing.T) {
	root := fixtureRoot(t)
	cases := []struct {
		name string
		ext  *Extractor
		pkg  string
		is   error
	}{
		{"unknown package", New(), "nope", metrics.ErrUnknownPackage},
		{"unknown tokenizer", New(WithTokenizer("bpe")), "trivial", metrics.ErrUnknownTokenizer},
		{"zero chars per token", New(WithCharsPerToken(0)), "trivial", nil},
		{"infinite chars per token", New(WithCharsPerToken(math.Inf(1))), "trivial", nil},
		{"zero minimum tokens", New(WithDuplication(0, true, true)), "trivial", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.ext.Extract(t.Context(), &metrics.ModuleContext{Root: root}, tc.pkg)
			if err == nil {
				t.Fatal("Extract returned no error")
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("Extract error = %v, want it to wrap %v", err, tc.is)
			}
		})
	}
	if _, err := New().Packages(t.TempDir() + "/missing"); err == nil {
		t.Error("Packages of a missing root returned no error")
	}
}

func TestOptions(t *testing.T) {
	root := fixtureRoot(t)
	extract := func(t *testing.T, e *Extractor, pkg string) metrics.RawMetrics {
		t.Helper()
		m, err := e.Extract(t.Context(), &metrics.ModuleContext{Root: root}, pkg)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	t.Run("chars per token", func(t *testing.T) {
		if m := extract(t, New(WithCharsPerToken(1)), "trivial"); m.TokensEst != 686 {
			t.Errorf("tokens_est at 1 char per token = %d, want the 686 bytes", m.TokensEst)
		}
	})
	t.Run("min tokens", func(t *testing.T) {
		if m := extract(t, New(WithDuplication(1000, true, true)), "dupes"); m.DupBlocks != 0 {
			t.Errorf("dup_blocks with a 1000-token minimum = %d, want 0", m.DupBlocks)
		}
	})
	t.Run("literal-only toggles", func(t *testing.T) {
		m := extract(t, New(WithDuplication(40, false, false)), "dupes")
		if m.DupBlocks != 1 {
			t.Errorf("dup_blocks = %d, want 1", m.DupBlocks)
		}
	})
	t.Run("o200k", func(t *testing.T) {
		m := extract(t, New(WithTokenizer(methodO200k)), "tested")
		est := extract(t, New(), "tested")
		if m.TokensEst <= 0 || m.TokensEstWithTests <= m.TokensEst || m.TokensEst == est.TokensEst {
			t.Errorf("o200k tokens = %d, %d; est %d", m.TokensEst, m.TokensEstWithTests, est.TokensEst)
		}
	})
}

func TestForget(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"package.json": "{}", "a/a.ts": "export const a = 1;\n"})
	e := New()
	if _, err := e.Packages(root); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"b/b.ts": "export const b = 1;\n"})
	got, _ := e.Packages(root)
	if !slices.Equal(got, []string{"a"}) {
		t.Fatalf("Packages before Forget = %q, want the cached [a]", got)
	}
	e.Forget(root + "/.")
	got, err := e.Packages(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Packages after Forget = %q, want [a b]", got)
	}
	e.Forget(t.TempDir()) // never loaded: no effect, no panic
}

func TestWithLogger(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json": "{}",
		"a/ok.ts":      "export const a = 1;\n",
		"a/broken.ts":  "export function f( {\n",
	})
	var buf bytes.Buffer
	e := New(WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	m, err := e.Extract(t.Context(), &metrics.ModuleContext{Root: root}, "a")
	if err != nil {
		t.Fatalf("Extract with a syntax error: %v", err)
	}
	if m.Files != 2 {
		t.Errorf("files = %d, want 2: a file with syntax errors is still measured", m.Files)
	}
	if out := buf.String(); !strings.Contains(out, "syntax errors") || !strings.Contains(out, "broken.ts") ||
		strings.Contains(out, "ok.ts") {
		t.Errorf("log = %q, want one syntax error record naming broken.ts", out)
	}
}

func TestConcurrentExtract(t *testing.T) {
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	ctx := context.Background()
	errs := make(chan error, len(fixturePackages()))
	for _, pkg := range fixturePackages() {
		go func() {
			_, err := e.Extract(ctx, mod, pkg)
			errs <- err
		}()
	}
	for range fixturePackages() {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
