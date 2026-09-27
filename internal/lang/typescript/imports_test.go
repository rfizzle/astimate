package typescript

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

func TestClassifyBaseURLAndExtensions(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"package.json":       "{}",
		"src/app/app.ts":     "export const a = 1;\n",
		"src/lib/index.ts":   "export const l = 1;\n",
		"src/esm/m.mts":      "export const m = 1;\n",
		"src/esm/d.d.mts":    "export declare const d: number;\n",
		"src/cjs/c.cts":      "export const c = 1;\n",
		"src/view/v.tsx":     "export const v = 1;\n",
		"src/path/p.ts":      "export const p = 1;\n",
		"src/esm.ts":         "export const e = 1;\n",
		"src/multi/index.ts": "export const x = 1;\n",
		"tsconfig.json": `{"compilerOptions": {"baseUrl": "src", "paths": {
			"@m/*": ["missing/*", "esm/*"],
		}}}`,
	})
	pkgs := map[string]*pkg{
		"src": {}, "src/app": {}, "src/lib": {}, "src/esm": {}, "src/cjs": {}, "src/view": {}, "src/path": {}, "src/multi": {},
	}
	cfg, err := readTSConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	r := newResolver(root, pkgs, cfg)
	cases := []struct {
		name, spec string
		want       classified
	}{
		{"bare directory under baseUrl", "lib", classified{importInternal, "src/lib"}},
		{"bare file under baseUrl", "lib/index", classified{importInternal, "src/lib"}},
		{"bare file wins over directory", "esm", classified{importInternal, "src"}},
		{"bare name under baseUrl shadows a built-in", "path", classified{importInternal, "src/path"}},
		{"node prefix is never under baseUrl", "node:path", classified{importStdlib, "path"}},
		{"bare name missing under baseUrl", "lodash", classified{importExternal, "lodash"}},
		{"built-in missing under baseUrl", "fs", classified{importStdlib, "fs"}},
		{"js extension to ts", "../lib/index.js", classified{importInternal, "src/lib"}},
		{"jsx extension to tsx", "../view/v.jsx", classified{importInternal, "src/view"}},
		{"mjs extension to mts", "../esm/m.mjs", classified{importInternal, "src/esm"}},
		{"mjs extension to d.mts", "../esm/d.mjs", classified{importInternal, "src/esm"}},
		{"cjs extension to cts", "../cjs/c.cjs", classified{importInternal, "src/cjs"}},
		{"alias target found through the mjs mapping", "@m/m.mjs", classified{importInternal, "src/esm"}},
		{"alias with no target found takes the first", "@m/none.mjs", classified{importNone, ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.classify("src/app", tc.spec); got != tc.want {
				t.Errorf("classify(%q) = %+v, want %+v", tc.spec, got, tc.want)
			}
		})
	}
}

func TestSourceCandidates(t *testing.T) {
	cases := map[string][]string{
		"a/x.js":  {"a/x.ts", "a/x.tsx", "a/x.d.ts"},
		"a/x.jsx": {"a/x.tsx"},
		"a/x.mjs": {"a/x.mts", "a/x.d.mts"},
		"a/x.cjs": {"a/x.cts", "a/x.d.cts"},
		"a/x.ts":  {},
		"a/x":     {},
	}
	for in, want := range cases {
		if got := sourceCandidates(in); !slices.Equal(got, want) {
			t.Errorf("sourceCandidates(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadTSConfigExtends(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		// root is the module root, relative to the directory files are
		// written to; "" for that directory.
		root string
		// base and baseURL are slash paths relative to the directory
		// files are written to; baseURL "" means none.
		base, baseURL string
		// match maps a specifier to its first target, "" for no match.
		match map[string]string
	}{
		{
			name: "relative chain inherits baseUrl relative to the parent",
			files: map[string]string{
				"tsconfig.json":            `{"extends": "./cfg/base.json", "compilerOptions": {"paths": {"@a/*": ["a/*"]}}}`,
				"cfg/base.json":            `{"extends": "./root", "compilerOptions": {"paths": {"@old/*": ["old/*"]}}}`,
				"cfg/root.json":            `{"compilerOptions": {"baseUrl": "../src"}}`,
				"cfg/unused/tsconfig.json": `{}`,
			},
			base: "src", baseURL: "src",
			match: map[string]string{"@a/x": "a/x", "@old/x": ""},
		},
		{
			name: "parent paths kept when the child sets none",
			files: map[string]string{
				"tsconfig.json": `{"extends": "./cfg/base.json"}`,
				"cfg/base.json": `{"compilerOptions": {"paths": {"@p/*": ["p/*"]}}}`,
			},
			// Without baseUrl, paths resolve against the file declaring them.
			base: "cfg", baseURL: "",
			match: map[string]string{"@p/x": "p/x"},
		},
		{
			name: "child baseUrl overrides the parent's",
			files: map[string]string{
				"tsconfig.json": `{"extends": "./base.json", "compilerOptions": {"baseUrl": "lib"}}`,
				"base.json":     `{"compilerOptions": {"baseUrl": "src", "paths": {"@p/*": ["p/*"]}}}`,
			},
			base: "lib", baseURL: "lib",
			match: map[string]string{"@p/x": "p/x"},
		},
		{
			name: "child empty paths clear the parent's",
			files: map[string]string{
				"tsconfig.json": `{"extends": "./base.json", "compilerOptions": {"paths": {}}}`,
				"base.json":     `{"compilerOptions": {"paths": {"@p/*": ["p/*"]}}}`,
			},
			base: ".", baseURL: "",
			match: map[string]string{"@p/x": ""},
		},
		{
			name: "bare package under node_modules above the root",
			root: "mod",
			files: map[string]string{
				"mod/tsconfig.json":                    `{"extends": "@org/tsconfig/base.json"}`,
				"node_modules/@org/tsconfig/base.json": `{"compilerOptions": {"baseUrl": "."}}`,
			},
			base: "node_modules/@org/tsconfig", baseURL: "node_modules/@org/tsconfig",
		},
		{
			name: "bare package directory reads its tsconfig.json",
			files: map[string]string{
				"tsconfig.json":                     `{"extends": "shared"}`,
				"node_modules/shared/tsconfig.json": `{"compilerOptions": {"paths": {"@s": ["s"]}}}`,
			},
			base: "node_modules/shared", baseURL: "",
			match: map[string]string{"@s": "s"},
		},
		{
			name: "bare name with json appended",
			files: map[string]string{
				"tsconfig.json":                   `{"extends": "shared/strict"}`,
				"node_modules/shared/strict.json": `{"compilerOptions": {"baseUrl": "."}}`,
			},
			base: "node_modules/shared", baseURL: "node_modules/shared",
		},
		{
			name: "array extends applies later entries last",
			files: map[string]string{
				"tsconfig.json": `{"extends": ["./one.json", "./two.json"]}`,
				"one.json":      `{"compilerOptions": {"baseUrl": "one", "paths": {"@x": ["one"]}}}`,
				"two.json":      `{"compilerOptions": {"baseUrl": "two"}}`,
			},
			base: "two", baseURL: "two",
			match: map[string]string{"@x": "one"},
		},
		{
			name: "missing parent ends the chain",
			files: map[string]string{
				"tsconfig.json": `{"extends": "not-installed", "compilerOptions": {"baseUrl": "."}}`,
			},
			base: ".", baseURL: ".",
		},
		{
			name: "cycle ends the chain",
			files: map[string]string{
				"tsconfig.json": `{"extends": "./a.json"}`,
				"a.json":        `{"extends": "./b.json", "compilerOptions": {"baseUrl": "a"}}`,
				"b.json":        `{"extends": "./a.json", "compilerOptions": {"baseUrl": "b"}}`,
			},
			base: "a", baseURL: "a",
		},
		{
			name: "self extension ends the chain",
			files: map[string]string{
				"tsconfig.json": `{"extends": "./tsconfig.json", "compilerOptions": {"baseUrl": "."}}`,
			},
			base: ".", baseURL: ".",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tc.files)
			cfg, err := readTSConfig(filepath.Join(dir, tc.root))
			if err != nil {
				t.Fatal(err)
			}
			abs := func(rel string) string { return filepath.Join(dir, filepath.FromSlash(rel)) }
			if want := abs(tc.base); cfg.base != want {
				t.Errorf("base = %s, want %s", cfg.base, want)
			}
			wantURL := ""
			if tc.baseURL != "" {
				wantURL = abs(tc.baseURL)
			}
			if cfg.baseURL != wantURL {
				t.Errorf("baseURL = %q, want %q", cfg.baseURL, wantURL)
			}
			for spec, want := range tc.match {
				got := cfg.match(spec)
				switch {
				case want == "" && got != nil:
					t.Errorf("match(%q) = %q, want no match", spec, got)
				case want != "" && (len(got) == 0 || got[0] != want):
					t.Errorf("match(%q) = %q, want first target %q", spec, got, want)
				}
			}
		})
	}
}

func TestReadTSConfigDepthBound(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{"tsconfig.json": `{"extends": "./c1.json"}`}
	// A chain one longer than the bound: the deepest file, which alone sets
	// baseUrl, is never read.
	for i := 1; i <= maxExtendsDepth+1; i++ {
		files["c"+strconv.Itoa(i)+".json"] = `{"extends": "./c` + strconv.Itoa(i+1) + `.json"}`
	}
	files["c"+strconv.Itoa(maxExtendsDepth+1)+".json"] = `{"compilerOptions": {"baseUrl": "deep"}}`
	files["c"+strconv.Itoa(maxExtendsDepth)+".json"] = `{"extends": "./c` + strconv.Itoa(maxExtendsDepth+1) + `.json", "compilerOptions": {"baseUrl": "last"}}`
	writeTree(t, root, files)
	cfg, err := readTSConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "last"); cfg.baseURL != want {
		t.Errorf("baseURL = %q, want %q from the deepest file within the bound", cfg.baseURL, want)
	}
}

func TestReadTSConfigExtendsErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"bad parent JSON":           {"tsconfig.json": `{"extends": "./base.json"}`, "base.json": "{ nope"},
		"extends of the wrong type": {"tsconfig.json": `{"extends": 3}`},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, files)
			if _, err := readTSConfig(root); err == nil {
				t.Error("readTSConfig = nil error, want one")
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
