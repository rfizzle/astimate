package resolve

import (
	"path/filepath"
	"slices"
	"testing"
)

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
		if got := IsTestPath(rel); got != want {
			t.Errorf("IsTestPath(%q) = %v, want %v", rel, got, want)
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
		if got := PackageOf(rel); got != want {
			t.Errorf("PackageOf(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestFindFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":        "{}",
		"index.ts":            "",
		"a/a.ts":              "",
		"a/a.test.ts":         "",
		"a/__tests__/b.tsx":   "",
		"a/types.d.ts":        "",
		"a/notes.md":          "",
		"b/b.mts":             "",
		"node_modules/x/x.ts": "",
		"dist/d.ts":           "",
		"build/b.ts":          "",
		".cache/c.ts":         "",
		"nested/package.json": "{}",
		"nested/n.ts":         "",
	})
	files, err := FindFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		if f.Abs != filepath.Join(root, filepath.FromSlash(f.Rel)) {
			t.Errorf("%s: Abs = %s", f.Rel, f.Abs)
		}
		got = append(got, f.Rel+" "+f.Pkg+" "+map[bool]string{true: "test", false: "src"}[f.Test])
	}
	want := []string{
		"a/__tests__/b.tsx a test",
		"a/a.test.ts a test",
		"a/a.ts a src",
		"b/b.mts b src",
		"index.ts . src",
	}
	if !slices.Equal(got, want) {
		t.Errorf("FindFiles = %q, want %q", got, want)
	}
	if _, err := FindFiles(filepath.Join(root, "missing")); err == nil {
		t.Error("FindFiles on a missing root = nil error, want one")
	}
}

func TestFileNames(t *testing.T) {
	cases := []struct {
		name                         string
		typeScript, declaration, src bool
	}{
		{"x.ts", true, false, true},
		{"x.tsx", true, false, true},
		{"x.mts", true, false, true},
		{"x.cts", true, false, true},
		{"x.d.ts", true, true, false},
		{"x.d.mts", true, true, false},
		{"x.d.cts", true, true, false},
		{"x.js", false, false, false},
		{"x.json", false, false, false},
	}
	for _, tc := range cases {
		if got := IsTypeScriptName(tc.name); got != tc.typeScript {
			t.Errorf("IsTypeScriptName(%q) = %v, want %v", tc.name, got, tc.typeScript)
		}
		if got := IsDeclarationName(tc.name); got != tc.declaration {
			t.Errorf("IsDeclarationName(%q) = %v, want %v", tc.name, got, tc.declaration)
		}
		if got := IsSourceName(tc.name); got != tc.src {
			t.Errorf("IsSourceName(%q) = %v, want %v", tc.name, got, tc.src)
		}
	}
}

func TestSkipDir(t *testing.T) {
	for name, want := range map[string]bool{
		"node_modules": true, "dist": true, "build": true, ".git": true, ".cache": true,
		"src": false, "lib": false, "builds": false,
	} {
		if got := SkipDir(name); got != want {
			t.Errorf("SkipDir(%q) = %v, want %v", name, got, want)
		}
	}
}
