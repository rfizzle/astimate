package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/report"
)

// handlerAPI is package api of a module whose test drives Hello over HTTP
// through Routes, naming only Routes, and never runs Unused. Without
// hello, Routes serves nothing and Hello does not exist: the baseline
// before the change that added it.
func handlerAPI(hello bool) map[string]string {
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

// TestCoverageUntestedExports checks the field case of untested_exports
// that static reference tracing cannot settle: Hello, reached only through
// an HTTP handler test, reads untested statically and tested with
// --coverage, and a change adding it passes an untested_exports rule with
// source: coverage under --coverage, while it fails the static rule and,
// without --coverage, the source: coverage rule, which falls back to the
// static count.
func TestCoverageUntestedExports(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs go test")
	}
	t.Parallel()

	head := writeTree(t, handlerAPI(true))
	t.Run("assess", func(t *testing.T) {
		t.Parallel()
		r, _ := assessJSON(t, "--coverage", filepath.Join(head, "api"))
		if r.Metrics.UntestedExports != 2 {
			t.Errorf("untested_exports = %d, want 2 (Hello, Unused)", r.Metrics.UntestedExports)
		}
		if c := r.Metrics.UntestedExportsCovered; c == nil || *c != 1 {
			t.Errorf("untested_exports_covered = %v, want 1 (Unused): the HTTP test runs Hello", c)
		}
		static, _ := assessJSON(t, filepath.Join(head, "api"))
		if static.Metrics.UntestedExportsCovered != nil || static.Metrics.UntestedExports != 2 {
			t.Errorf("without --coverage: untested_exports %d, covered %v; want 2 and null",
				static.Metrics.UntestedExports, static.Metrics.UntestedExportsCovered)
		}
	})

	t.Run("table golden", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		args := []string{"assess", "--coverage", filepath.Join(head, "api")}
		if got := run(args, &stdout, &stderr); got != exitOK {
			t.Fatalf("run(%q) = %d; stderr = %q", args, got, stderr.String())
		}
		golden := filepath.Join("testdata", "assess_handler_coverage.txt")
		if os.Getenv("UPDATE_GOLDEN") != "" {
			if err := os.WriteFile(golden, stdout.Bytes(), 0o644); err != nil {
				t.Fatalf("updating golden file: %v", err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("reading golden file (regenerate with UPDATE_GOLDEN=1): %v", err)
		}
		if got := stdout.String(); got != string(want) {
			t.Errorf("table output differs from %s (regenerate with UPDATE_GOLDEN=1)\ngot:\n%s\nwant:\n%s", golden, got, want)
		}
	})

	baseRoot := writeTree(t, handlerAPI(false))
	base := filepath.Join(t.TempDir(), "base.json")
	var stdout, stderr bytes.Buffer
	if got := run([]string{"baseline", "write", baseRoot, "--out", base}, &stdout, &stderr); got != exitOK {
		t.Fatalf("baseline write = %d; stderr = %q", got, stderr.String())
	}
	staticCfg := filepath.Join(t.TempDir(), "static.yaml")
	coverCfg := filepath.Join(t.TempDir(), "coverage.yaml")
	for path, data := range map[string]string{
		staticCfg: string(config.Default()),
		coverCfg:  strings.Replace(string(config.Default()), "    # source: coverage\n", "    source: coverage\n", 1),
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name     string
		cfg      string
		coverage bool
		want     int
		limit    string
	}{
		{"static rule fails the added handler", staticCfg, true, exitGateFailed, "max_delta +0"},
		{"source coverage passes it under --coverage", coverCfg, true, exitOK, ""},
		{"source coverage without --coverage falls back to static", coverCfg, false, exitGateFailed, "max_delta +0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := []string{"check", head, "--baseline", base, "--all", "--config", tt.cfg, "--format", "json"}
			if tt.coverage {
				args = append(args, "--coverage")
			}
			var stdout, stderr bytes.Buffer
			if got := run(args, &stdout, &stderr); got != tt.want {
				t.Fatalf("run(%q) = %d, want %d; stdout = %s; stderr = %q", args, got, tt.want, stdout.String(), stderr.String())
			}
			var reports []report.Report
			if err := json.Unmarshal(stdout.Bytes(), &reports); err != nil {
				t.Fatalf("decoding check: %v\n%s", err, stdout.String())
			}
			for _, r := range reports {
				if r.PackagePath != "api" {
					continue
				}
				if (r.Metrics.UntestedExportsCovered != nil) != tt.coverage {
					t.Errorf("untested_exports_covered = %v, want it measured only with --coverage", r.Metrics.UntestedExportsCovered)
				}
				for _, v := range r.Violations {
					if v.Metric == "untested_exports" && v.Limit != tt.limit {
						t.Errorf("violation limit = %q, want %q", v.Limit, tt.limit)
					}
				}
				if tt.limit != "" && (len(r.Violations) != 1 || r.Violations[0].Metric != "untested_exports") {
					t.Errorf("violations = %+v, want the one untested_exports finding", r.Violations)
				}
				if tt.limit != "" && !strings.Contains(r.Violations[0].Suggestion, "Hello") {
					t.Errorf("suggestion = %q, want it to name Hello, the export the change added", r.Violations[0].Suggestion)
				}
			}
		})
	}
}
