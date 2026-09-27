package golang

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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
			l, err := loadModule(&packages.Config{Dir: root}, packages.Load)
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("loaded %v, want an error", l.paths)
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
				t.Fatalf("loadModule: %v", err)
			}
			if !slices.Equal(l.paths, tc.want) {
				t.Errorf("paths = %v, want %v", l.paths, tc.want)
			}
		})
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
			_, err := loadModule(&packages.Config{Dir: root}, packages.Load)
			if err == nil {
				t.Fatal("load succeeded with a read-only build cache")
			}
			want := "the Go build cache must be writable (GOCACHE=" + cache + "): "
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not contain %q", err, want)
			}
			if errors.Is(err, ErrNoPackages) {
				t.Errorf("error %v reads as an empty module", err)
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

// BenchmarkLoadAfterEdit loads a copy of the fixture right after appending
// an exported function to hub, which changes hub's export data, so every
// load recompiles hub and the four fixture packages that import it, as the
// load after each edit in an agent's loop does. The edit is not timed.
func BenchmarkLoadAfterEdit(b *testing.B) {
	src := filepath.Dir(fixtureRoot(b))
	dir := b.TempDir()
	for _, mod := range []string{"fixture", "extmod"} {
		if err := os.CopyFS(filepath.Join(dir, mod), os.DirFS(filepath.Join(src, mod))); err != nil {
			b.Fatal(err)
		}
	}
	root := filepath.Join(dir, "fixture")
	hub := filepath.Join(root, "hub", "hub.go")
	// The copied files keep the fixture's read-only permissions in a
	// read-only checkout; the edit needs write permission.
	if err := os.Chmod(hub, 0o600); err != nil {
		b.Fatal(err)
	}
	n := 0
	for b.Loop() {
		b.StopTimer()
		n++
		f, err := os.OpenFile(hub, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			b.Fatal(err)
		}
		num := strconv.Itoa(n)
		_, err = f.WriteString("\n// Edit" + num + " returns " + num + ".\nfunc Edit" + num + "() int { return " + num + " }\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := New().Packages(root); err != nil {
			b.Fatalf("Packages(%s): %v", root, err)
		}
	}
}
