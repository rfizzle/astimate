package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
)

// handlerFiles is a module whose package api has Routes, which its test
// drives over HTTP, and Unused, which nothing runs; with hello, Routes
// serves Hello, which the test reaches only through the handler.
func handlerFiles(hello bool) map[string]string {
	route, handler := "", ""
	if hello {
		route = "\tmux.HandleFunc(\"/hello\", Hello)\n"
		handler = "\n// Hello writes a greeting.\nfunc Hello(w http.ResponseWriter, _ *http.Request) {\n\t_, _ = w.Write([]byte(\"hello\"))\n}\n"
	}
	return map[string]string{
		"go.mod": "module example.com/handler\n\ngo 1.27\n",
		"api/api.go": "package api\n\nimport \"net/http\"\n\n// Routes returns the handler of the API.\nfunc Routes() http.Handler {\n" +
			"\tmux := http.NewServeMux()\n" + route + "\treturn mux\n}\n" + handler +
			"\n// Unused is called by nothing.\nfunc Unused() int {\n\treturn 1\n}\n",
		"api/api_test.go": "package api_test\n\nimport (\n\t\"net/http/httptest\"\n\t\"testing\"\n\n\t\"example.com/handler/api\"\n)\n\n" +
			"func TestHello(t *testing.T) {\n\tsrv := httptest.NewServer(api.Routes())\n\tdefer srv.Close()\n" +
			"\tresp, err := srv.Client().Get(srv.URL + \"/hello\")\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\t_ = resp.Body.Close()\n}\n",
	}
}

// handlerWorkspace writes the module with Hello under mod and, as
// baseline.json, the baseline of the module without it, and returns the
// workspace.
func handlerWorkspace(t *testing.T) string {
	t.Helper()
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for dst, hello := range map[string]bool{"mod": true, "base": false} {
		for rel, data := range handlerFiles(hello) {
			p := filepath.Join(ws, dst, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	tg, err := engine.LoadTarget(filepath.Join(ws, "base"), engine.TargetOptions{Config: defaultConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.WriteBaseline(t.Context(), tg, filepath.Join(ws, "baseline.json")); err != nil {
		t.Fatalf("writing the baseline: %v", err)
	}
	return ws
}

// TestCheckPackageCoverageSource checks that check_package with coverage:
// true judges untested_exports_covered under an untested_exports rule with
// source: coverage, so an export a test reaches only through an HTTP
// handler passes, and that without coverage the same rule judges the
// static count and fails.
func TestCheckPackageCoverageSource(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	cfg, err := config.Parse([]byte(strings.Replace(string(config.Default()), "    # source: coverage\n", "    source: coverage\n", 1)))
	if err != nil {
		t.Fatalf("parsing the source: coverage config: %v", err)
	}
	for _, coverage := range []bool{true, false} {
		ws := handlerWorkspace(t)
		cs := newTestClient(t, Options{Config: cfg, WorkDir: ws, Version: "test"})
		res := callCheck(t, cs, map[string]any{"path": "mod/api", "baseline_file": "baseline.json", "coverage": coverage})
		if res.IsError {
			t.Fatalf("coverage %v: IsError = true; text:\n%s", coverage, resultText(res))
		}
		got := decodeCheck(t, res)
		if got.Passed == nil || *got.Passed != coverage {
			t.Errorf("coverage %v: passed = %v, violations %+v; want passed %v", coverage, got.Passed, got.Violations, coverage)
		}
		if c := got.Metrics.UntestedExportsCovered; coverage && (c == nil || *c != 1) {
			t.Errorf("untested_exports_covered = %v, want 1", c)
		}
	}
}
