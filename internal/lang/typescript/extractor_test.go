package typescript

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

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

func TestIsTestPath(t *testing.T) {
	cases := map[string]bool{
		"a/x.ts":                 false,
		"a/x.test.ts":            true,
		"a/x.spec.ts":            true,
		"a/x.test.tsx":           true,
		"a/x.spec.tsx":           true,
		"a/x.test.mts":           true,
		"a/x.spec.mts":           true,
		"a/x.test.cts":           true,
		"a/x.spec.cts":           true,
		"a/x.mts":                false,
		"a/x.test.mjs":           false,
		"a/__tests__/x.ts":       true,
		"a/__tests__/deep/x.tsx": true,
		"a/x.tests.ts":           false,
		"a/testsuite/x.ts":       false,
	}
	for rel, want := range cases {
		if got := isTestPath(rel); got != want {
			t.Errorf("isTestPath(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestPackageOf(t *testing.T) {
	cases := map[string]string{
		"x.ts":                   ".",
		"a/x.ts":                 "a",
		"a/b/x.ts":               "a/b",
		"a/__tests__/x.ts":       "a",
		"a/__tests__/d/x.ts":     "a",
		"__tests__/x.ts":         ".",
		"a/b/__tests__/__t/x.ts": "a/b",
	}
	for rel, want := range cases {
		if got := packageOf(rel); got != want {
			t.Errorf("packageOf(%q) = %q, want %q", rel, got, want)
		}
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

func TestDetails(t *testing.T) {
	e := New()
	mod := &metrics.ModuleContext{Root: fixtureRoot(t)}
	cases := []struct {
		pkg                string
		untested, excluded []string
		dupLocations       []string
	}{
		{"dupes", []string{"countVisits"}, nil, []string{"dupes.ts:6-23", "dupes.ts:27-44", "dupes.ts:48-65"}},
		{"hub", []string{"Counter.add", "clamp", "normalize", "twice"}, nil, nil},
		{"tested", nil, nil, nil},
		{"trivial", []string{"Box.close", "answer", "detached", "spaced"}, []string{"Box.open", "listed", "wrapped"}, nil},
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
		})
	}
	if _, err := e.Details(t.Context(), mod, "nope"); !errors.Is(err, ErrUnknownPackage) {
		t.Errorf("Details of an unknown package = %v, want ErrUnknownPackage", err)
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
		{"unknown package", New(), "nope", ErrUnknownPackage},
		{"unknown tokenizer", New(WithTokenizer("bpe")), "trivial", ErrUnknownTokenizer},
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
