package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestTypeScriptCorpusFile checks that the shipped TypeScript corpus parses
// as one, with at least minTypeScriptRepos pinned repositories, each naming
// its module roots, and that pinning it again changes nothing.
func TestTypeScriptCorpusFile(t *testing.T) {
	const path = "../corpus-typescript.yaml"
	c, err := LoadCorpus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsTypeScript() {
		t.Fatalf("language = %q, want %s", c.Language, languageTypeScript)
	}
	if len(c.Modules) < minTypeScriptRepos {
		t.Errorf("%d repositories, want at least %d", len(c.Modules), minTypeScriptRepos)
	}
	for _, e := range c.Modules {
		if e.Local || !isCommitHash(e.Commit) || len(e.Modules) == 0 {
			t.Errorf("%s: want a pinned clone with module roots: %+v", e.Module, e)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fail := func(context.Context, string) (string, error) {
		t.Error("pinning resolved a commit of a fully pinned corpus")
		return "", nil
	}
	if out, n, err := PinCorpus(t.Context(), data, fail); err != nil || n != 0 || string(out) != string(data) {
		t.Errorf("PinCorpus = %d pinned, %v; want the file unchanged", n, err)
	}
}

// TestTypeScriptModulesOnly checks that module rows alone cannot be
// collected from a TypeScript corpus, whose extractor measures none; the
// run stops before cloning anything.
func TestTypeScriptModulesOnly(t *testing.T) {
	var stderr strings.Builder
	args := []string{"--corpus", "../corpus-typescript.yaml", "--modules-only", "--out", t.TempDir()}
	if got := run(t.Context(), args, &strings.Builder{}, &stderr); got != exitUsage {
		t.Errorf("run(%q) = %d, want %d", args, got, exitUsage)
	}
	if !strings.Contains(stderr.String(), "measures no module row") {
		t.Errorf("stderr = %q, want the reason", stderr.String())
	}
}

// validTSCorpus returns a TypeScript corpus document with n unpinned
// repositories, each with the module root ".".
func validTSCorpus(n int) string {
	var b strings.Builder
	b.WriteString("note: test\nlanguage: typescript\nmodules:\n")
	for i := range n {
		b.WriteString("  - module: example.com/r")
		b.WriteByte(byte('a' + i))
		b.WriteString("\n    repo: https://example.com/r.git\n    commit: \"\"\n    license: MIT\n")
		b.WriteString("    stars_or_dependents: many\n    reason: r\n    modules: [\".\"]\n")
	}
	return b.String()
}

func TestParseTypeScriptCorpus(t *testing.T) {
	valid := validTSCorpus(minTypeScriptRepos)
	root := func(p string) string { return strings.Replace(valid, `modules: ["."]`, `modules: ["`+p+`"]`, 1) }
	tests := []struct {
		name, doc, wantErr string
	}{
		{"valid", valid, ""},
		{"glob root", root("packages/*"), ""},
		{"too few", validTSCorpus(minTypeScriptRepos - 1), "at least 15 cloned repositories"},
		{"unknown language", strings.Replace(valid, "language: typescript", "language: rust", 1), `language "rust"`},
		{"no module roots", strings.Replace(valid, "    modules: [\".\"]\n", "", 1), "needs at least one module root"},
		{"parent root", root("../x"), "inside the repository"},
		{"absolute root", root("/x"), "inside the repository"},
		{"unclean root", root("a/../b"), "inside the repository"},
		{"bad pattern", root("a/["), "bad pattern"},
		{"local entry", strings.Replace(valid, "    repo: https://example.com/r.git\n", "    local: true\n", 1), "no local entry"},
		{"duplicate", valid + "  - module: example.com/ra\n    repo: r\n    commit: \"\"\n    license: MIT\n    stars_or_dependents: s\n    reason: r\n    modules: [\".\"]\n", "duplicate module"},
		{"go corpus with roots", strings.Replace(validCorpus(minExternalModules), "reason: r\n  - module: example.com/ma", "reason: r\n    modules: [\".\"]\n  - module: example.com/ma", 1), "TypeScript corpus only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCorpus([]byte(tt.doc))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestExcludedPackage(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{".", false},
		{"src", false},
		{"src/testing", false},
		{"src/latest", false},
		{"src/test-utils", true},
		{"test", true},
		{"src/__tests__/util", true},
		{"runtime-tests/node", true},
		{"__performance_tests__", true},
		{"packages-private/dts-test", true},
		{"src/fixtures", true},
		{"examples/basic", true},
		{"website/src", true},
		{"src/e2e", true},
		{"src/button.spec", true},
	}
	for _, tt := range tests {
		if got := excludedPackage(tt.id); got != tt.want {
			t.Errorf("excludedPackage(%q) = %t, want %t", tt.id, got, tt.want)
		}
	}
}

// writeTree writes files, slash paths relative to root with their content.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCollectTSTree collects a small TypeScript monorepo with no network:
// each module root the entry's patterns match is ranked on its own, a
// directory with no package.json matches nothing, test and fixture
// packages are counted as excluded, a root that also holds a go.mod fails
// alone, and every row carries the module's name, the commit and the
// language.
func TestCollectTSTree(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"package.json":                     "{}\n",
		"root.ts":                          "export const r = 1;\n",
		"packages/lib/package.json":        "{}\n",
		"packages/lib/src/a.ts":            "import { u } from './util/u';\nexport function a(): number { return u(); }\n",
		"packages/lib/src/util/u.ts":       "export function u(): number { return 1; }\n",
		"packages/lib/src/a.test.ts":       "it('a', () => {});\n",
		"packages/lib/test/helpers.ts":     "export const h = 1;\n",
		"packages/lib/runtime-tests/rt.ts": "export const rt = 1;\n",
		"packages/app/package.json":        "{}\n",
		"packages/app/index.ts":            "export const app = 1;\n",
		"packages/docs/readme.md":          "no package.json\n",
		"packages/gomod/package.json":      "{}\n",
		"packages/gomod/go.mod":            "module example.com/gomod\n",
		"packages/gomod/x.ts":              "export const x = 1;\n",
	})
	const commit = "0123456789abcdef0123456789abcdef01234567"
	e := Entry{Module: "example.com/r", Commit: commit, Modules: []string{"packages/*"}}
	rows, mr := collectTSTree(t.Context(), dir, e, defaultConfig(t), slog.New(slog.DiscardHandler), ModuleRun{Module: e.Module})
	if mr.Error != "" {
		t.Fatalf("error = %s", mr.Error)
	}
	if want := []string{"packages/app", "packages/gomod", "packages/lib"}; !slices.Equal(mr.Roots, want) {
		t.Errorf("roots = %q, want %q", mr.Roots, want)
	}
	if len(mr.Failed) != 1 || mr.Failed[0].Package != "example.com/r/packages/gomod" || !strings.Contains(mr.Failed[0].Err, "go extractor") {
		t.Errorf("failed = %+v, want the gomod root alone, resolved to Go", mr.Failed)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Module+" "+r.Package)
		if r.Commit != commit || r.Language != languageTypeScript {
			t.Errorf("%s %s: commit %q language %q", r.Module, r.Package, r.Commit, r.Language)
		}
	}
	want := []string{"example.com/r/packages/app .", "example.com/r/packages/lib src", "example.com/r/packages/lib src/util"}
	if !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
	if mr.Excluded != 2 || mr.Packages != len(rows) {
		t.Errorf("excluded %d packages, counted %d rows; want 2 and %d", mr.Excluded, mr.Packages, len(rows))
	}
	for _, r := range rows {
		if r.Package == "src" && (r.Metrics.InternalImports != 1 || r.Metrics.HasTests != true) {
			t.Errorf("lib src: internal_imports %d has_tests %t, want 1 and true", r.Metrics.InternalImports, r.Metrics.HasTests)
		}
	}

	if _, mr := collectTSTree(t.Context(), dir, Entry{Module: "example.com/r", Modules: []string{"missing/*"}},
		defaultConfig(t), slog.New(slog.DiscardHandler), ModuleRun{}); !strings.Contains(mr.Error, "matches no directory") {
		t.Errorf("unmatched root error = %q, want one naming the pattern", mr.Error)
	}
}
