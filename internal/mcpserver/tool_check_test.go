package mcpserver

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/report"
)

// Fixture modules relative to this package's directory; see testdata/go.
const (
	fixtureDir  = "../../testdata/go/fixture"
	degradedDir = "../../testdata/go/fixture-degraded"
	extmodDir   = "../../testdata/go/extmod"
)

// degradedMetrics are the rules the degraded fixture's tested package
// breaks by adding a duplicate function, an untested export, a global and
// one function of cognitive complexity 40.
func degradedMetrics() []string {
	return []string{"changed_func_cognitive_max", "dup_blocks", "globals", "untested_exports"}
}

// defaultConfig returns the embedded default configuration.
func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, _, err := config.Resolve("")
	if err != nil {
		t.Fatalf("resolving default config: %v", err)
	}
	return cfg
}

// workspace copies the fixture module src to <tmp>/mod, writes the pristine
// fixture's baseline to <tmp>/baseline.json, and returns tmp with symlinks
// resolved. The copy is outside any git repository, so only the baseline
// file can serve as the baseline.
func workspace(t *testing.T, src string) string {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The fixtures replace example.com/extmod with ../extmod.
	for dst, from := range map[string]string{"mod": src, "extmod": extmodDir} {
		if err := os.CopyFS(filepath.Join(tmp, dst), os.DirFS(from)); err != nil {
			t.Fatalf("copying %s: %v", from, err)
		}
	}
	tg, err := engine.LoadTarget(fixtureDir, engine.TargetOptions{Config: defaultConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.WriteBaseline(t.Context(), tg, filepath.Join(tmp, "baseline.json")); err != nil {
		t.Fatalf("writing the fixture baseline: %v", err)
	}
	return tmp
}

// callCheck calls check_package with in and returns the result.
func callCheck(t *testing.T, cs *mcp.ClientSession, in map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: checkToolName, Arguments: in})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

// resultText joins the text content of res.
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// decodeReport decodes the structured content of res as a report,
// rejecting unknown fields.
func decodeReport(t *testing.T, res *mcp.CallToolResult) report.Report {
	t.Helper()
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encoding structured content: %v", err)
	}
	var r report.Report
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("decoding structured content as a report: %v\n%s", err, data)
	}
	return r
}

func TestCheckPackageTool(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	tests := []struct {
		name       string
		src        string
		wantPassed bool
		wantMetric []string
		wantText   string
	}{
		{name: "degraded", src: degradedDir, wantPassed: false, wantMetric: degradedMetrics(), wantText: "FAILED"},
		{name: "unchanged", src: fixtureDir, wantPassed: true, wantText: "PASSED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ws := workspace(t, tt.src)
			cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: ws, Version: "test"})
			res := callCheck(t, cs, map[string]any{"path": "mod/tested", "baseline_file": "baseline.json"})
			text := resultText(res)
			if res.IsError {
				t.Fatalf("IsError = true, want a gate result; text:\n%s", text)
			}
			r := decodeReport(t, res)
			if r.Passed == nil || *r.Passed != tt.wantPassed {
				t.Fatalf("passed = %v, want %v; text:\n%s", r.Passed, tt.wantPassed, text)
			}
			if r.PackagePath != "tested" || r.Baseline == nil {
				t.Errorf("report = (%q, baseline %v), want tested with a baseline", r.PackagePath, r.Baseline)
			}
			got := make([]string, 0, len(r.Violations))
			for _, v := range r.Violations {
				got = append(got, v.Metric)
			}
			slices.Sort(got)
			got = slices.Compact(got)
			// The duplicate function also moves duplication_pct, so the
			// three rules the fixture breaks on purpose are checked as a
			// subset, as the CLI's fixture tests do.
			missing := slices.DeleteFunc(slices.Clone(tt.wantMetric), func(m string) bool {
				return slices.Contains(got, m)
			})
			if len(missing) > 0 || (len(tt.wantMetric) == 0 && len(got) > 0) {
				t.Errorf("violation metrics = %v, want %v among them and none when passing", got, tt.wantMetric)
			}
			if !strings.HasPrefix(text, tt.wantText) {
				t.Errorf("text = %q, want it to start with %q", text, tt.wantText)
			}
			for _, m := range tt.wantMetric {
				if !strings.Contains(text, m+": ") {
					t.Errorf("text does not name violation %s:\n%s", m, text)
				}
			}
		})
	}
}

func TestCheckPackageToolErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	ws := workspace(t, fixtureDir)
	outside := t.TempDir()
	link := filepath.Join(ws, "mod", "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig(t)
	inTested := Options{Config: cfg, WorkDir: filepath.Join(ws, "mod", "tested")}

	tests := []struct {
		name     string
		opts     Options
		in       map[string]any
		wantText string
	}{
		{name: "parent path refused", opts: inTested,
			in: map[string]any{"path": "../hub"}, wantText: "outside the server's working directory"},
		{name: "absolute path refused", opts: inTested,
			in: map[string]any{"path": outside}, wantText: "outside the server's working directory"},
		{name: "symlink out refused", opts: Options{Config: cfg, WorkDir: ws},
			in: map[string]any{"path": "mod/escape"}, wantText: "outside the server's working directory"},
		{name: "baseline file outside refused", opts: inTested,
			in: map[string]any{"path": ".", "baseline_file": "../../baseline.json"}, wantText: "baseline_file"},
		{name: "missing path", opts: Options{Config: cfg, WorkDir: ws},
			in: map[string]any{"path": "mod/nope"}, wantText: "mod/nope"},
		{name: "base and baseline file", opts: Options{Config: cfg, WorkDir: ws},
			in:       map[string]any{"path": "mod/tested", "base": "main", "baseline_file": "baseline.json"},
			wantText: "mutually exclusive"},
		{name: "no baseline", opts: Options{Config: cfg, WorkDir: ws},
			in: map[string]any{"path": "mod/tested"}, wantText: "pass base (a git ref) or baseline_file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := callCheck(t, newTestClient(t, tt.opts), tt.in)
			text := resultText(res)
			if !res.IsError {
				t.Fatalf("IsError = false, want true; text:\n%s", text)
			}
			if !strings.Contains(text, tt.wantText) {
				t.Errorf("text = %q, want it to contain %q", text, tt.wantText)
			}
			if strings.Contains(text, "--base") {
				t.Errorf("text = %q, want no CLI flag hint", text)
			}
			data, err := json.Marshal(res.StructuredContent)
			if err != nil || !bytes.Contains(data, []byte(`"error"`)) {
				t.Errorf("structured content = %s, want an error object", data)
			}
		})
	}

	t.Run("allow any path", func(t *testing.T) {
		t.Parallel()

		o := inTested
		o.AllowAnyPath = true
		res := callCheck(t, newTestClient(t, o), map[string]any{"path": "../hub", "baseline_file": "../../baseline.json"})
		if res.IsError {
			t.Fatalf("IsError = true with --allow-any-path; text:\n%s", resultText(res))
		}
		if r := decodeReport(t, res); r.PackagePath != "hub" || r.Passed == nil || !*r.Passed {
			t.Errorf("report = (%q, passed %v), want hub passing", r.PackagePath, r.Passed)
		}
	})
}
