package golang

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
)

// cgoRoot returns the absolute path of testdata/go/cgo, a module whose
// package native uses cgo and whose package user imports native.
func cgoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(filepath.Dir(fixtureRoot(t)), "cgo")
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

// TestLogSkipped checks that the extractor logs each skipped package once,
// at info level, however many calls share the load.
func TestLogSkipped(t *testing.T) {
	root := cgoAlongsidePure(t)
	t.Setenv("CGO_ENABLED", "0")
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	e := New(WithLogger(logger))
	for range 2 {
		if _, err := e.Packages(root); err != nil {
			t.Fatalf("Packages: %v", err)
		}
	}
	want := `level=INFO msg="skipped package" package=example.com/mixed/ignored dir=ignored reason="build constraints exclude all Go files"` + "\n" +
		`level=INFO msg="skipped package" package=example.com/mixed/native dir=native reason="build constraints exclude all Go files; it ` +
		`uses cgo, which is disabled (CGO_ENABLED=0, the default when no C compiler is on PATH)"` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("log =\n%s\nwant\n%s", got, want)
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

// newO200kForTest returns an o200k counter with proxies pointed at an
// unroutable address and an empty tiktoken cache directory, so a download of
// the BPE file would fail instead of succeeding.
func newO200kForTest(t *testing.T) inspect.Counter {
	t.Helper()
	t.Setenv("TIKTOKEN_CACHE_DIR", t.TempDir())
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(k, "http://127.0.0.1:9")
	}
	for _, k := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	c, err := inspect.NewO200kCounter()
	if err != nil {
		t.Fatalf("NewO200kCounter: %v", err)
	}
	return c
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
