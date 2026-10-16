package typescript

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/exclude"
	"github.com/rfizzle/astimate/internal/metrics"
)

// TestExclude checks the exclude list on a module: a package whose source
// files it all matches leaves Packages and is listed by Excluded with the
// pattern, and its import of base no longer counts in base's fan_in; a
// package with one file excluded is measured without it, its files, sloc
// and imports falling by that file's share; the excluded files move
// nothing to ClassifyFile and are logged at debug level.
func TestExclude(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":             `{"name": "x"}`,
		"base/base.ts":             "export function base(): number {\n  return 1;\n}\n",
		"internal/gen/gen.ts":      "import { base } from \"../../base/base\";\nexport const gen = base() + 1;\n",
		"internal/gen/gen.test.ts": "import { gen } from \"./gen\";\nexport const t = gen;\n",
		"internal/gen/sub/keep.ts": "export const keep = 1;\n",
		"p/p.ts":                   "import * as path from \"node:path\";\nexport function up(s: string): string {\n  return path.basename(s);\n}\n",
		"p/p_gen.ts":               "import * as fs from \"node:fs\";\nimport { base } from \"../base/base\";\nexport function show(): string {\n  return String(base()) + fs.constants;\n}\n",
		"p/p.test.ts":              "import { up } from \"./p\";\nexport const t = up(\"a\");\n",
	})
	before, err := New().Extract(t.Context(), &metrics.ModuleContext{Root: root}, "p")
	if err != nil {
		t.Fatal(err)
	}
	baseBefore, err := New().Extract(t.Context(), &metrics.ModuleContext{Root: root}, "base")
	if err != nil {
		t.Fatal(err)
	}

	list, err := exclude.New([]string{"internal/gen/*.ts", "**/*_gen.ts"})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	e := New(WithExclude(list), WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	pkgs, err := e.Packages(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"base", "internal/gen/sub", "p"}; !slices.Equal(pkgs, want) {
		t.Errorf("Packages = %v, want %v", pkgs, want)
	}
	excluded, err := e.Excluded(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []metrics.SkippedPackage{{Package: "internal/gen", Reason: "excluded by config (internal/gen/*.ts)"}}
	if !slices.Equal(excluded, want) {
		t.Errorf("Excluded = %+v, want %+v", excluded, want)
	}
	if na, err := metrics.NotAnalysed(e, root); err != nil || !slices.Equal(na, want) {
		t.Errorf("NotAnalysed = %+v, %v, want %+v", na, err, want)
	}

	mod := &metrics.ModuleContext{Root: root}
	after, err := e.Extract(t.Context(), mod, "p")
	if err != nil {
		t.Fatal(err)
	}
	if before.Files != 2 || after.Files != 1 {
		t.Errorf("p files %d -> %d, want 2 -> 1", before.Files, after.Files)
	}
	if after.SLOC != 4 || before.SLOC <= after.SLOC {
		t.Errorf("p sloc %d -> %d, want p.ts's 4 alone", before.SLOC, after.SLOC)
	}
	if before.StdlibImports != 2 || after.StdlibImports != 1 || before.InternalImports != 1 || after.InternalImports != 0 {
		t.Errorf("p stdlib, internal imports %d, %d -> %d, %d; want 2, 1 -> 1, 0",
			before.StdlibImports, before.InternalImports, after.StdlibImports, after.InternalImports)
	}
	baseAfter, err := e.Extract(t.Context(), mod, "base")
	if err != nil {
		t.Fatal(err)
	}
	if baseBefore.FanIn != 2 || baseAfter.FanIn != 0 {
		t.Errorf("base fan_in %d -> %d, want 2 -> 0: both importing files are excluded", baseBefore.FanIn, baseAfter.FanIn)
	}

	for rel, wantKind := range map[string]metrics.SourceKind{
		"p/p_gen.ts":               metrics.NotSource,
		"internal/gen/gen.ts":      metrics.NotSource,
		"internal/gen/gen.test.ts": metrics.NotSource,
		"internal/gen/sub/keep.ts": metrics.PackageSource,
		"p/p.ts":                   metrics.PackageSource,
	} {
		if got := e.ClassifyFile(rel).Kind; got != wantKind {
			t.Errorf("ClassifyFile(%s) = %v, want %v", rel, got, wantKind)
		}
	}
	text := logs.String()
	for _, line := range []string{
		"file=p/p_gen.ts pattern=**/*_gen.ts",
		"file=internal/gen/gen.ts pattern=internal/gen/*.ts",
		"file=internal/gen/gen.test.ts pattern=internal/gen/*.ts",
	} {
		if !strings.Contains(text, "msg=\"excluded file\" "+line) {
			t.Errorf("log lacks the excluded file %s:\n%s", line, text)
		}
	}
}
