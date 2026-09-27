package typescript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestClassify(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":         "{}",
		"src/app/app.ts":       "export const a = 1;\n",
		"src/app/util.ts":      "export const u = 1;\n",
		"src/lib/index.ts":     "export const l = 1;\n",
		"src/lib/deep/deep.ts": "export const d = 1;\n",
		"assets/logo.svg":      "<svg/>",
		"sub.ts":               "export const s = 1;\n",
		"sub/sub.ts":           "export const t = 1;\n",
		"tsconfig.json": `{
  "compilerOptions": {
    "baseUrl": "src",
    "paths": {
      "@lib": ["lib/index"],
      "@lib/*": ["lib/*"],
      "@deep/*": ["missing/*", "lib/deep/*"],
      "@out/*": ["../../elsewhere/*"],
    }
  }
}`,
	})
	pkgs := map[string]*pkg{
		".": {}, "src/app": {}, "src/lib": {}, "src/lib/deep": {}, "sub": {},
	}
	cfg, err := readTSConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	r := newResolver(root, pkgs, cfg)

	cases := []struct {
		name, dir, spec string
		want            classified
	}{
		{"relative directory", "src/app", "../lib", classified{importInternal, "src/lib"}},
		{"relative file", "src/app", "../lib/index", classified{importInternal, "src/lib"}},
		{"relative file with js extension", "src/app", "../lib/index.js", classified{importInternal, "src/lib"}},
		{"relative same directory", "src/app", "./util", classified{importInternal, "src/app"}},
		{"relative dot", "src/app", ".", classified{importInternal, "src/app"}},
		{"relative nested directory", "src/app", "../lib/deep", classified{importInternal, "src/lib/deep"}},
		{"relative file wins over directory", ".", "./sub", classified{importInternal, "."}},
		{"relative into a non-package directory", "src/app", "../../assets/logo.svg", classified{importNone, ""}},
		{"relative escaping the module", "src/app", "../../../other/x", classified{importExternal, "../../../other/x"}},
		{"exact alias", "src/app", "@lib", classified{importInternal, "src/lib"}},
		{"wildcard alias", "src/app", "@lib/deep/deep", classified{importInternal, "src/lib/deep"}},
		{"alias falls through to the target that exists", "src/app", "@deep/deep", classified{importInternal, "src/lib/deep"}},
		{"alias outside the module", "src/app", "@out/x", classified{importExternal, "@out/x"}},
		{"bare", "src/app", "lodash", classified{importExternal, "lodash"}},
		{"bare subpath", "src/app", "lodash/map", classified{importExternal, "lodash"}},
		{"scoped bare", "src/app", "@scope/pkg/sub", classified{importExternal, "@scope/pkg"}},
		{"scoped bare not matching an alias", "src/app", "@libx/y", classified{importExternal, "@libx/y"}},
		{"node prefix", "src/app", "node:fs/promises", classified{importStdlib, "fs"}},
		{"node prefix only built-in", "src/app", "node:test", classified{importStdlib, "test"}},
		{"built-in without prefix", "src/app", "path", classified{importStdlib, "path"}},
		{"built-in subpath", "src/app", "fs/promises", classified{importStdlib, "fs"}},
		{"not a built-in", "src/app", "pathlib", classified{importExternal, "pathlib"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.classify(tc.dir, tc.spec); got != tc.want {
				t.Errorf("classify(%q, %q) = %+v, want %+v", tc.dir, tc.spec, got, tc.want)
			}
		})
	}
}

func TestClassifyWithoutTSConfig(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"package.json": "{}", "a/a.ts": "export {};\n"})
	cfg, err := readTSConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	r := newResolver(root, map[string]*pkg{"a": {}}, cfg)
	if got, want := r.classify(".", "@app/a"), (classified{importExternal, "@app/a"}); got != want {
		t.Errorf("classify without aliases = %+v, want %+v", got, want)
	}
}

func TestReadTSConfigErrors(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"tsconfig.json": "{ not json"})
	if _, err := readTSConfig(root); err == nil || !strings.Contains(err.Error(), "tsconfig.json") {
		t.Errorf("readTSConfig on bad JSON = %v, want an error naming tsconfig.json", err)
	}
}

func TestStripJSONC(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"line comment", "{\"a\": 1 // one\n}", "{\"a\": 1 \n}"},
		{"block comment", "{/* x */\"a\": 1}", "{ \"a\": 1}"},
		{"comment markers in strings", `{"a": "//not", "b": "/*no*/"}`, `{"a": "//not", "b": "/*no*/"}`},
		{"escaped quote", `{"a": "\"//"}`, `{"a": "\"//"}`},
		{"trailing commas", "{\"a\": [1, 2,],\n}", "{\"a\": [1, 2]\n}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(stripJSONC([]byte(tc.in))); got != tc.want {
				t.Errorf("stripJSONC(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAliasOrder(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"tsconfig.json": `{"compilerOptions": {"paths": {
		"*": ["any/*"], "@a/*": ["short/*"], "@a/b/*": ["long/*"], "@a/b/c": ["exact"]}}}`})
	cfg, err := readTSConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"@a/b/c": "exact",
		"@a/b/d": "long/d",
		"@a/x":   "short/x",
		"zzz":    "any/zzz",
	}
	for spec, want := range cases {
		if got := cfg.match(spec); len(got) != 1 || got[0] != want {
			t.Errorf("match(%q) = %q, want [%q]", spec, got, want)
		}
	}
}
