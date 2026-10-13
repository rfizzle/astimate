package golang

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
)

var _ metrics.CoverageMeasurer = (*Extractor)(nil)

func TestCoverageFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	root := fixtureRoot(t)
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/fixture"}
	pkgs := []string{"example.com/fixture/tested", "example.com/fixture/trivial"}
	got, err := New().Coverage(t.Context(), mod, pkgs)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	tested := got["example.com/fixture/tested"]
	if tested.Pct == nil || *tested.Pct < 0 || *tested.Pct > 100 {
		t.Errorf("tested = %+v, want a percentage in [0, 100]", tested)
	}
	if trivial := got["example.com/fixture/trivial"]; trivial.Pct != nil || trivial.Reason != "" {
		t.Errorf("trivial, which has no test files, = %+v, want nil with no reason", trivial)
	}
	if trivial := got["example.com/fixture/trivial"]; trivial.UntestedExports != nil {
		t.Errorf("trivial untested_exports_covered = %d, want nil: no test binary of the run links it", *trivial.UntestedExports)
	}
	if tested.UntestedExports == nil {
		t.Error("tested untested_exports_covered = nil, want a count")
	}
}

// handlerModule is a module whose package api serves Hello through a mux
// that its test drives over HTTP, naming only Routes, and has an export,
// Unused, that nothing runs.
func handlerModule() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/handler\n\ngo 1.27\n",
		"api/api.go": `package api

import "net/http"

// Routes returns the handler of the API.
func Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", Hello)
	return mux
}

// Hello writes a greeting.
func Hello(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("hello"))
}

// Unused is called by nothing.
func Unused() int {
	return 1
}
`,
		"api/api_test.go": `package api_test

import (
	"io"
	"net/http/httptest"
	"testing"

	"example.com/handler/api"
)

func TestHello(t *testing.T) {
	srv := httptest.NewServer(api.Routes())
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/hello")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("body = %q", body)
	}
}
`,
	}
}

// writeModule writes files, keyed by slash path, under a new temporary
// directory and returns it.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, data)
	}
	return dir
}

func TestCoverageUntestedExports(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	root := writeModule(t, handlerModule())
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/handler"}
	e := New()
	const pkg = "example.com/handler/api"
	m, err := e.Extract(t.Context(), mod, pkg)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if m.UntestedExports != 2 || m.UntestedExportsCovered != nil {
		t.Fatalf("static untested_exports = %d, covered = %v; want 2 (Hello, Unused) and nil before a run", m.UntestedExports, m.UntestedExportsCovered)
	}
	got, err := e.Coverage(t.Context(), mod, []string{pkg})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	c := got[pkg]
	if c.UntestedExports == nil || *c.UntestedExports != 1 {
		t.Errorf("untested_exports_covered = %v, want 1: the HTTP test runs Hello, not Unused", c.UntestedExports)
	}
	if c.Pct == nil || *c.Pct <= 0 || *c.Pct >= 100 {
		t.Errorf("coverage_pct = %v, want a share strictly between 0 and 100", c.Pct)
	}
	d, err := e.Details(t.Context(), mod, pkg)
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	if !slices.Equal(d.UntestedCovered, []string{"Hello"}) || !slices.Equal(d.UntestedExports, []string{"Hello", "Unused"}) {
		t.Errorf("details untested = %v, covered = %v; want [Hello Unused] and [Hello]", d.UntestedExports, d.UntestedCovered)
	}
}

// TestCoverageTestOnlyDirectory checks that a package without test files
// of its own, driven by the tests of a directory holding only test files,
// gets untested_exports_covered from that directory's run and no
// coverage_pct, and that a package nothing tests gets neither.
func TestCoverageTestOnlyDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	files := map[string]string{
		"go.mod":           handlerModule()["go.mod"],
		"api/api.go":       handlerModule()["api/api.go"],
		"test/e2e_test.go": strings.Replace(handlerModule()["api/api_test.go"], "package api_test", "package test", 1),
		"other/other.go":   "package other\n\n// One returns 1.\nfunc One() int { return 1 }\n",
	}
	root := writeModule(t, files)
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/handler"}
	e := New()
	const api, other = "example.com/handler/api", "example.com/handler/other"
	for _, pkg := range []string{api, other} {
		if _, err := e.Extract(t.Context(), mod, pkg); err != nil {
			t.Fatalf("Extract(%s): %v", pkg, err)
		}
	}
	got, err := e.Coverage(t.Context(), mod, []string{api, other})
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if c := got[api]; c.Pct != nil || c.Reason != "" || c.UntestedExports == nil || *c.UntestedExports != 1 {
		t.Errorf("api = %+v, want no coverage_pct and untested_exports_covered 1 from test/'s run", c)
	}
	if c := got[other]; c.Pct != nil || c.UntestedExports != nil {
		t.Errorf("other = %+v, want nothing measured: no test binary links it", c)
	}
}
