package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/engine"
)

// TestCheckSkippedPackage checks that check_package on a package that does
// not load returns a result, not an error, saying so with the reason and
// first error, and that a package beside it is still checked.
func TestCheckSkippedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(ws, os.DirFS("../../testdata/go/skipped")); err != nil {
		t.Fatal(err)
	}
	tg, err := engine.LoadTarget(ws, engine.TargetOptions{Config: defaultConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.WriteBaseline(t.Context(), tg, filepath.Join(ws, "baseline.json")); err != nil {
		t.Fatal(err)
	}
	cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: ws})

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: checkToolName,
		Arguments: map[string]any{"path": "remote", "baseline_file": "baseline.json"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError {
		t.Fatalf("isError set for a skipped package: %s", resultText(res))
	}
	got := decodeStrict[SkippedResult](t, res)
	if s := got.Skipped; s.Package != "remote" || s.Reason != "missing module" || !s.Selected ||
		!strings.Contains(s.Error, "example.com/absent/client") {
		t.Errorf("skipped = %+v, want remote, missing module, selected", s)
	}
	if text := resultText(res); !strings.HasPrefix(text, "SKIPPED: remote does not load (missing module)") {
		t.Errorf("text = %q, want it to open with SKIPPED and the reason", text)
	}

	res, err = cs.CallTool(t.Context(), &mcp.CallToolParams{Name: checkToolName,
		Arguments: map[string]any{"path": "app", "baseline_file": "baseline.json"}})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError || !strings.HasPrefix(resultText(res), "PASSED: app") {
		t.Errorf("app result = %s, want PASSED", resultText(res))
	}
}
