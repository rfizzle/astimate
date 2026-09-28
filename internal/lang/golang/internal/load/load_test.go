package load

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// cgoRoot returns the absolute path of testdata/go/cgo, a module whose
// package native uses cgo and whose package user imports native.
func cgoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(filepath.Dir(fixtureRoot(t)), "cgo")
}

// cgoDependent writes a module to a temporary directory whose one package,
// example.com/dep/app, imports the cgo fixture's user package, so the cgo
// package is a dependency outside the loaded module. It returns the root.
func cgoDependent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/dep\n\ngo 1.27\n\n"+
		"require example.com/cgo v0.0.0\n\nreplace example.com/cgo => "+cgoRoot(t)+"\n")
	if err := os.Mkdir(filepath.Join(dir, "app"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "app", "app.go"), "// Package app sums through a cgo dependency.\npackage app\n\n"+
		"import \"example.com/cgo/user\"\n\n// Total sums 1, 2 and 3.\nfunc Total() int { return user.Sum(1, 2, 3) }\n")
	return dir
}

// cgoAlongsidePure writes a module example.com/mixed to a temporary
// directory with a copy of the cgo fixture's native package and its test,
// which no package imports, and a pure Go package pure, so the module loads
// with cgo disabled. It also holds a package ignored, without tests, whose
// only file has an ignore build constraint, and Go files the go command never loads: under
// testdata, vendor, _hidden and .hidden directories, in a nested module, and
// named _skip.go. It returns the root.
func cgoAlongsidePure(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"native", "pure", "ignored", "testdata", "vendor", "_hidden", ".hidden", "nested"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/mixed\n\ngo 1.27\n")
	for _, name := range []string{"native.go", "native_test.go"} {
		src, err := os.ReadFile(filepath.Join(cgoRoot(t), "native", name))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "native", name), string(src))
	}
	writeFile(t, filepath.Join(dir, "pure", "pure.go"), "// Package pure is plain Go.\npackage pure\n\n"+
		"// Double returns 2x.\nfunc Double(x int) int { return 2 * x }\n")
	writeFile(t, filepath.Join(dir, "pure", "_skip.go"), "package pure\n")
	writeFile(t, filepath.Join(dir, "ignored", "gen.go"), "//go:build ignore\n\npackage main\n")
	for _, sub := range []string{"testdata", "vendor", "_hidden", ".hidden", "nested"} {
		writeFile(t, filepath.Join(dir, sub, "x.go"), "//go:build ignore\n\npackage x\n")
	}
	writeFile(t, filepath.Join(dir, "nested", "go.mod"), "module example.com/nested\n\ngo 1.27\n")
	return dir
}

// haveCgo reports whether cgo is enabled and the C compiler go env names is
// on PATH.
func haveCgo(t *testing.T) bool {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "CGO_ENABLED", "CC").Output()
	if err != nil {
		t.Fatalf("go env: %v", err)
	}
	lines := strings.Fields(string(out))
	if len(lines) < 2 || lines[0] != "1" {
		return false
	}
	_, err = exec.LookPath(lines[1])
	return err == nil
}

func TestLoadCgo(t *testing.T) {
	noCC := filepath.Join(t.TempDir(), "no-cc")
	for _, tc := range []struct {
		name string
		// root returns the module root to load.
		root func(t *testing.T) string
		// cgo is CGO_ENABLED; cc, when set, is CC.
		cgo, cc string
		// toolchain skips the case when cgo cannot run on this machine.
		toolchain bool
		// want is the packages the load returns; when empty, the load must
		// fail with an error containing each of wantErr.
		want    []string
		wantErr []string
	}{
		{
			name: "C compiler", root: cgoRoot, cgo: "1", toolchain: true,
			want: []string{"example.com/cgo/native", "example.com/cgo/user"},
		},
		{
			name: "no C compiler", root: cgoRoot, cgo: "1", cc: noCC,
			wantErr: []string{
				"loading example.com/cgo/native: cgo package example.com/cgo/native needs a C compiler (CC=" + noCC + "): ",
				"could not import C",
			},
		},
		{
			name: "cgo disabled", root: cgoRoot, cgo: "0",
			wantErr: []string{
				"loading example.com/cgo/user: dependency example.com/cgo/native uses cgo, which is disabled (CGO_ENABLED=0",
				"undefined: native.Add",
			},
		},
		{
			name: "external dependency, no C compiler", root: cgoDependent, cgo: "1", cc: noCC,
			want: []string{"example.com/dep/app"},
		},
		{
			name: "external dependency, cgo disabled", root: cgoDependent, cgo: "0",
			want: []string{"example.com/dep/app"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.toolchain && !haveCgo(t) {
				t.Skip("cgo or its C compiler is unavailable")
			}
			root := tc.root(t)
			t.Setenv("CGO_ENABLED", tc.cgo)
			if tc.cc != "" {
				t.Setenv("CC", tc.cc)
			}
			l, err := Load(&packages.Config{Dir: root}, packages.Load)
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("loaded %v, want an error", l.Paths)
				}
				for _, want := range tc.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
				var pe packages.Error
				if !errors.As(err, &pe) {
					t.Errorf("error %v does not wrap the packages.Error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !slices.Equal(l.Paths, tc.want) {
				t.Errorf("paths = %v, want %v", l.Paths, tc.want)
			}
		})
	}
}

func TestLoadSkipped(t *testing.T) {
	const constraints = "build constraints exclude all Go files"
	ignored := SkippedDir{Dir: "ignored", ImportPath: "example.com/mixed/ignored", Reason: constraints}
	for _, tc := range []struct {
		name string
		// cgo is CGO_ENABLED.
		cgo string
		// toolchain skips the case when cgo cannot run on this machine.
		toolchain   bool
		wantPaths   []string
		wantSkipped []SkippedDir
	}{
		{
			name: "cgo disabled", cgo: "0",
			wantPaths: []string{"example.com/mixed/pure"},
			wantSkipped: []SkippedDir{ignored, {
				Dir: "native", ImportPath: "example.com/mixed/native",
				Reason: constraints + "; it " + usesDisabledCgo,
			}},
		},
		{
			name: "C compiler", cgo: "1", toolchain: true,
			wantPaths:   []string{"example.com/mixed/native", "example.com/mixed/pure"},
			wantSkipped: []SkippedDir{ignored},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.toolchain && !haveCgo(t) {
				t.Skip("cgo or its C compiler is unavailable")
			}
			root := cgoAlongsidePure(t)
			t.Setenv("CGO_ENABLED", tc.cgo)
			l, err := Load(&packages.Config{Dir: root}, packages.Load)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !slices.Equal(l.Paths, tc.wantPaths) {
				t.Errorf("paths = %v, want %v", l.Paths, tc.wantPaths)
			}
			if !slices.Equal(l.Skipped, tc.wantSkipped) {
				t.Errorf("skipped = %+v, want %+v", l.Skipped, tc.wantSkipped)
			}
		})
	}
}

// TestLoadSkippedTestOnly checks that a package made of tests beside
// "//go:build ignore" files is reported as test-only, and that one whose
// tests build constraints exclude too keeps the plain reason.
func TestLoadSkippedTestOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/tonly\n\ngo 1.27\n")
	for _, sub := range []string{"pure", "gen", "xgen", "allignored"} {
		if err := os.Mkdir(filepath.Join(root, sub), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "pure", "pure.go"), "// Package pure is plain Go.\npackage pure\n")
	const generator = "//go:build ignore\n\npackage main\n\nfunc main() {}\n"
	test := func(pkg string) string {
		return "package " + pkg + "\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n"
	}
	writeFile(t, filepath.Join(root, "gen", "gen.go"), generator)
	writeFile(t, filepath.Join(root, "gen", "gen_test.go"), test("gen"))
	writeFile(t, filepath.Join(root, "xgen", "gen.go"), generator)
	writeFile(t, filepath.Join(root, "xgen", "tool.go"), generator)
	writeFile(t, filepath.Join(root, "xgen", "x_test.go"), test("xgen_test"))
	writeFile(t, filepath.Join(root, "allignored", "gen.go"), generator)
	writeFile(t, filepath.Join(root, "allignored", "x_test.go"), "//go:build ignore\n\n"+test("allignored"))

	l, err := Load(&packages.Config{Dir: root}, packages.Load)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"example.com/tonly/pure"}; !slices.Equal(l.Paths, want) {
		t.Errorf("paths = %v, want %v", l.Paths, want)
	}
	want := []SkippedDir{
		{Dir: "allignored", ImportPath: "example.com/tonly/allignored", Reason: "build constraints exclude all Go files"},
		{Dir: "gen", ImportPath: "example.com/tonly/gen", Reason: "test-only package: build constraints exclude its 1 non-test Go file"},
		{Dir: "xgen", ImportPath: "example.com/tonly/xgen", Reason: "test-only package: build constraints exclude its 2 non-test Go files"},
	}
	if !slices.Equal(l.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", l.Skipped, want)
	}
	if len(l.Tests) != 0 || len(l.XTests) != 0 {
		t.Errorf("tests = %v, xtests = %v, want none", l.Tests, l.XTests)
	}
}

// readOnly makes dir and everything under it read-only, restoring write
// permission when the test ends so t.TempDir can remove it. It skips the
// test when the file system still lets the test write to dir, as it does
// for root.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	chmod := func(dirMode, fileMode fs.FileMode) {
		err := filepath.WalkDir(dir, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return os.Chmod(name, dirMode)
			}
			return os.Chmod(name, fileMode)
		})
		if err != nil {
			t.Fatalf("chmod %s: %v", dir, err)
		}
	}
	t.Cleanup(func() { chmod(0o700, 0o600) })
	chmod(0o500, 0o400)
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o600); err == nil {
		t.Skip("the file system does not honor read-only permissions")
	}
}

func TestLoadReadOnlyCache(t *testing.T) {
	for _, tc := range []struct {
		name string
		// populate loads the fixture into the cache before it is made
		// read-only, as a CI job restoring a cache would find it.
		populate bool
	}{
		{name: "empty"},
		{name: "populated", populate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := t.TempDir()
			t.Setenv("GOCACHE", cache)
			if tc.populate {
				loadRoot(t, fixtureRoot(t))
			}
			readOnly(t, cache)
			// A module the cache has never seen must be compiled for its
			// export data, which go list then cannot write.
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "go.mod"), "module example.com/fresh\n\ngo 1.27\n")
			writeFile(t, filepath.Join(root, "fresh.go"), "// Package fresh is new to the cache.\npackage fresh\n\n"+
				"import \"strings\"\n\n// Up upper-cases s.\nfunc Up(s string) string { return strings.ToUpper(s) }\n")
			_, err := Load(&packages.Config{Dir: root}, packages.Load)
			if err == nil {
				t.Fatal("load succeeded with a read-only build cache")
			}
			want := "the Go build cache must be writable (GOCACHE=" + cache + "): "
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not contain %q", err, want)
			}
		})
	}
}

func TestCacheCause(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	t.Setenv("GOCACHE", cache)
	for _, tc := range []struct {
		name string
		msg  string
		want bool
	}{
		{"initialize", "failed to initialize build cache at " + cache + ": permission denied", true},
		{"cache entry", "open " + cache + "/0f/0fd3-d: permission denied", true},
		{"off", "build cache is disabled by GOCACHE=off, but required", true},
		{"unrelated", "go: cannot find main module", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			orig := errors.New(tc.msg)
			err := cacheCause(orig)
			if !errors.Is(err, orig) {
				t.Fatalf("cacheCause(%q) = %v, does not wrap the original", tc.msg, err)
			}
			if got := strings.Contains(err.Error(), "GOCACHE="+cache); got != tc.want {
				t.Errorf("cacheCause(%q) = %q, names GOCACHE: %v, want %v", tc.msg, err, got, tc.want)
			}
		})
	}
}

func TestReadModulePath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gomod   string // empty means no go.mod file
		want    string
		wantErr string
	}{
		{name: "plain", gomod: "module example.com/m\n\ngo 1.27\n", want: "example.com/m"},
		{name: "quoted", gomod: "module \"example.com/q\"\n", want: "example.com/q"},
		{
			name:  "comments and requires",
			gomod: "// header\nmodule example.com/c // trailing\n\ngo 1.27\n\nrequire example.com/x v1.0.0\n",
			want:  "example.com/c",
		},
		{name: "no module directive", gomod: "go 1.27\n", wantErr: "no module directive"},
		{name: "missing go.mod", wantErr: "reading module path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.gomod != "" {
				writeFile(t, filepath.Join(dir, "go.mod"), tc.gomod)
			}
			got, err := ReadModulePath(dir)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ReadModulePath error = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadModulePath: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ReadModulePath = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInModule(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"example.com/m", true},
		{"example.com/m/sub", true},
		{"example.com/m_test", false},
		{"example.com/mod", false},
		{"example.com", false},
		{"fmt", false},
	} {
		if got := InModule("example.com/m", tc.path); got != tc.want {
			t.Errorf("InModule(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestFirstError(t *testing.T) {
	list := packages.Error{Msg: "# example.com/m\ncompiler output", Kind: packages.ListError}
	typ := packages.Error{Msg: "m.go:1:1: type error", Kind: packages.TypeError}
	parse := packages.Error{Msg: "m.go:1:1: parse error", Kind: packages.ParseError}
	for _, tc := range []struct {
		name string
		errs []packages.Error
		want packages.Error
	}{
		{"list error before type error", []packages.Error{list, typ}, typ},
		{"parse error first", []packages.Error{parse, list, typ}, parse},
		{"only list errors", []packages.Error{list}, list},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstError(tc.errs); got != tc.want {
				t.Errorf("firstError = %v, want %v", got, tc.want)
			}
		})
	}
}
