package mcpserver

import (
	"bytes"
	"encoding/json"
	"flag"
	stdhtml "html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rfizzle/astimate/internal/narrate/html"
)

// callModule calls report_module with in and returns the result.
func callModule(t *testing.T, cs *mcp.ClientSession, in map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: moduleToolName, Arguments: in})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

// compareGolden compares got with the file testdata/name, rewriting it
// first under -update.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if flag.Lookup(updateFlag).Value.String() == "true" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s; rerun with -update only after an intentional change\ngot:\n%s", path, got)
	}
}

// TestReportModuleTool calls report_module on the fixture and checks the
// verdict and the three packages to start with against the module report
// they come from, and the text and the first part of the structured
// content against testdata/module/fixture.golden.
func TestReportModuleTool(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: workspace(t, fixtureDir), Version: "test"})
	res := callModule(t, cs, map[string]any{"path": "mod"})
	if res.IsError {
		t.Fatalf("IsError = true; text:\n%s", resultText(res))
	}
	got := decodeStrict[moduleResult](t, res)
	if got.Headline == "" || got.Headline != got.Report.Headline || got.Lead != got.Report.Lead {
		t.Errorf("verdict = %q / %q, want the report's %q / %q", got.Headline, got.Lead, got.Report.Headline, got.Report.Lead)
	}
	var paths []string
	for _, h := range got.StartHere {
		paths = append(paths, h.Path)
		if h.Story == "" || h.Do == "" {
			t.Errorf("start here %s has story %q and do %q, want both", h.Path, h.Story, h.Do)
		}
	}
	if want := got.Report.Hotspots[:min(3, len(got.Report.Hotspots))]; len(paths) == 0 || !slices.Equal(paths, want) {
		t.Errorf("start here = %q, want the report's first hotspots %q", paths, want)
	}
	if !strings.HasPrefix(resultText(res), got.Headline+"\n"+got.Lead+"\n\nStart here:\n1. "+paths[0]) {
		t.Errorf("text does not lead with the verdict and the first package to start with:\n%s", resultText(res))
	}

	head := struct {
		Headline  string          `json:"headline"`
		Lead      string          `json:"lead"`
		StartHere []startHereItem `json:"start_here"`
	}{got.Headline, got.Lead, got.StartHere}
	data, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, filepath.Join("module", "fixture.golden"), "== text ==\n"+resultText(res)+"-- structured --\n"+string(data)+"\n")
}

// TestReportModuleToolErrors checks that a path outside the working
// directory and a base that does not exist are results with isError set.
func TestReportModuleToolErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: workspace(t, fixtureDir)})
	for name, in := range map[string]map[string]any{
		"missing path": {"path": "missing"},
		"outside":      {"path": "/"},
	} {
		t.Run(name, func(t *testing.T) {
			res := callModule(t, cs, in)
			if !res.IsError || !strings.HasPrefix(resultText(res), moduleToolName+": ") {
				t.Errorf("result = (isError %v, %q), want an error naming %s", res.IsError, resultText(res), moduleToolName)
			}
		})
	}
}

// termDiv matches one entry of the page's glossary.
var termDiv = regexp.MustCompile(`<div class="term"><b>([^<]*)</b>([^<]*)</div>`)

// TestExplainGlossaryMatchesPage checks that explain_metric answers each
// word of the glossary with the text the report page prints for it, by
// rendering the page of the report report_module returns.
func TestExplainGlossaryMatchesPage(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	cs := newTestClient(t, Options{Config: defaultConfig(t), WorkDir: workspace(t, fixtureDir), Version: "test"})
	mod := decodeStrict[moduleResult](t, callModule(t, cs, map[string]any{"path": "mod"}))
	var page bytes.Buffer
	if err := html.Write(&page, mod.Report); err != nil {
		t.Fatal(err)
	}
	onPage := map[string]string{}
	for _, m := range termDiv.FindAllStringSubmatch(page.String(), -1) {
		onPage[strings.ToLower(strings.ReplaceAll(stdhtml.UnescapeString(m[1]), " ", "_"))] = stdhtml.UnescapeString(m[2])
	}
	if len(onPage) != len(mod.Report.Glossary) || len(onPage) < len(glossaryNames())-1 {
		t.Fatalf("page glossary has %d terms, report %d, want the same and at least %d: %v", len(onPage), len(mod.Report.Glossary), len(glossaryNames())-1, onPage)
	}

	var golden strings.Builder
	for _, name := range glossaryNames() {
		res := callExplain(t, cs, name)
		if res.IsError {
			t.Fatalf("explain_metric %s: IsError = true; text:\n%s", name, resultText(res))
		}
		key := name
		if name == "paths" {
			key = "hard_to_follow"
		}
		want, ok := onPage[key]
		if !ok {
			t.Errorf("the page has no %q entry", key)
			continue
		}
		if got := decodeExplain(t, res).Definition; got != want {
			t.Errorf("explain_metric %s = %q, want the page's %q", name, got, want)
		}
		if got, wantText := resultText(res), onPage[key]+"\n"; !strings.HasSuffix(got, wantText) {
			t.Errorf("explain_metric %s text = %q, want it to end with the page's text", name, got)
		}
		golden.WriteString("== " + name + " ==\n" + resultText(res))
	}
	compareGolden(t, filepath.Join("explain", "glossary.golden"), golden.String())
}
