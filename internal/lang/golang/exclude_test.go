package golang

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/exclude"
	"github.com/rfizzle/astimate/internal/metrics"
)

// excludeModule writes a module in which internal/gen and p's p_gen.go
// both import base, so each exclusion moves base's fan_in, and p_gen.go
// adds an import of fmt to p's own strings.
func excludeModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                        "module example.com/x\n\ngo 1.22\n",
		"base/base.go":                  "package base\n\n// Base is one.\nfunc Base() int { return 1 }\n",
		"internal/gen/gen.go":           "package gen\n\nimport \"example.com/x/base\"\n\n// Gen is two.\nfunc Gen() int { return base.Base() + 1 }\n",
		"internal/gen/gen_test.go":      "package gen\n\nimport \"testing\"\n\nfunc TestGen(t *testing.T) { _ = Gen() }\n",
		"p/p.go":                        "package p\n\nimport \"strings\"\n\n// Up shouts.\nfunc Up(s string) string { return strings.ToUpper(s) }\n",
		"p/p_gen.go":                    "package p\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/x/base\"\n)\n\n// Show prints.\nfunc Show() string {\n\treturn fmt.Sprint(base.Base())\n}\n",
		"p/p_test.go":                   "package p\n\nimport \"testing\"\n\nfunc TestUp(t *testing.T) { _ = Up(\"a\") }\n",
		"internal/gen/sub/keep/keep.go": "package keep\n\n// Keep stays.\nconst Keep = 1\n",
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestExclude checks the exclude list on a module: a package whose files
// it all matches leaves Packages and is listed by Excluded with the
// pattern, and its import of base no longer counts in base's fan_in; a
// package with one file excluded is measured without it, its files, sloc
// and imports falling by that file's share; the excluded files move
// nothing to ClassifyFile and are logged at debug level.
func TestExclude(t *testing.T) {
	t.Parallel()

	root := excludeModule(t)
	mod := func() *metrics.ModuleContext { return &metrics.ModuleContext{Root: root, ModulePath: "example.com/x"} }

	plain := New()
	before, err := plain.Extract(t.Context(), mod(), "example.com/x/p")
	if err != nil {
		t.Fatal(err)
	}
	baseBefore, err := plain.Extract(t.Context(), mod(), "example.com/x/base")
	if err != nil {
		t.Fatal(err)
	}

	list, err := exclude.New([]string{"internal/gen/*.go", "**/*_gen.go"})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	e := New(WithExclude(list), WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	paths, err := e.Packages(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"example.com/x/base", "example.com/x/internal/gen/sub/keep", "example.com/x/p"}; !slices.Equal(paths, want) {
		t.Errorf("Packages = %v, want %v", paths, want)
	}
	excluded, err := e.Excluded(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []metrics.SkippedPackage{{Package: "example.com/x/internal/gen", Reason: "excluded by config (internal/gen/*.go)"}}
	if !slices.Equal(excluded, want) {
		t.Errorf("Excluded = %+v, want %+v", excluded, want)
	}

	after, err := e.Extract(t.Context(), mod(), "example.com/x/p")
	if err != nil {
		t.Fatal(err)
	}
	if before.Files != 2 || after.Files != 1 {
		t.Errorf("p files %d -> %d, want 2 -> 1", before.Files, after.Files)
	}
	if after.SLOC >= before.SLOC || after.SLOC != 3 {
		t.Errorf("p sloc %d -> %d, want 3, p.go's alone", before.SLOC, after.SLOC)
	}
	if before.StdlibImports != 2 || after.StdlibImports != 1 || before.InternalImports != 1 || after.InternalImports != 0 {
		t.Errorf("p stdlib, internal imports %d, %d -> %d, %d; want 2, 1 -> 1, 0",
			before.StdlibImports, before.InternalImports, after.StdlibImports, after.InternalImports)
	}
	if !after.HasTests {
		t.Error("p has_tests = false, want true: p_test.go is not excluded")
	}
	baseAfter, err := e.Extract(t.Context(), mod(), "example.com/x/base")
	if err != nil {
		t.Fatal(err)
	}
	if baseBefore.FanIn != 2 || baseAfter.FanIn != 0 {
		t.Errorf("base fan_in %d -> %d, want 2 -> 0: both importers are excluded", baseBefore.FanIn, baseAfter.FanIn)
	}

	for rel, wantKind := range map[string]metrics.SourceKind{
		"p/p_gen.go":                    metrics.NotSource,
		"internal/gen/gen.go":           metrics.NotSource,
		"internal/gen/gen_test.go":      metrics.NotSource,
		"internal/gen/sub/keep/keep.go": metrics.PackageSource,
		"p/p.go":                        metrics.PackageSource,
	} {
		if got := e.ClassifyFile(rel).Kind; got != wantKind {
			t.Errorf("ClassifyFile(%s) = %v, want %v", rel, got, wantKind)
		}
	}
	text := logs.String()
	for _, line := range []string{
		"file=p/p_gen.go pattern=**/*_gen.go",
		"file=internal/gen/gen.go pattern=internal/gen/*.go",
		"file=internal/gen/gen_test.go pattern=internal/gen/*.go",
	} {
		if !strings.Contains(text, "msg=\"excluded file\" "+line) {
			t.Errorf("log lacks the excluded file %s:\n%s", line, text)
		}
	}
}
