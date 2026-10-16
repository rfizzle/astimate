package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/narrate/render"
)

// notAnalysedDir is a module with one ordinary package and one directory
// whose only Go file build constraints exclude, relative to this package's
// directory.
const notAnalysedDir = "../../testdata/go/notanalysed"

// notAnalysedFixture copies the notanalysed module to a temporary
// directory outside any repository, so the report names no head, and
// returns its root.
func notAnalysedFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "notanalysed")
	copyTree(t, notAnalysedDir, root)
	return root
}

// TestReportNotAnalysedGoldens checks the report of the notanalysed
// module, which closes with its directory that is not analysed, as text
// at every layout, Markdown and JSON against
// testdata/report_notanalysed* (regenerate with UPDATE_GOLDEN=1); the
// page's golden is internal/narrate/html's, rendered from the JSON.
func TestReportNotAnalysedGoldens(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	root := notAnalysedFixture(t)
	cfg := pinnedConfigFile(t)
	tg, err := loadTarget(root, cfg, tokenizerEst, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	m, failed, err := engine.Report(context.Background(), tg, engine.ReportOptions{})
	if err != nil || len(failed) > 0 {
		t.Fatalf("Report = %v, %v", failed, err)
	}
	m.AstimateVersion = "test"
	checkTextGoldens(t, "report_notanalysed", m, render.Options{})

	for _, f := range []struct{ format, golden string }{{"md", "report_notanalysed.md"}, {"json", "report_notanalysed.json"}} {
		var stdout, stderr bytes.Buffer
		args := []string{"report", root, "--format", f.format, "--config", cfg}
		if got := run(args, &stdout, &stderr); got != exitOK || stderr.Len() != 0 {
			t.Fatalf("run(%q) = %d, stderr %q; want %d and no stderr", args, got, stderr.String(), exitOK)
		}
		got := strings.Replace(stdout.String(), `"astimate_version": "`+astimateVersion()+`"`, `"astimate_version": "test"`, 1)
		compareGolden(t, f.golden, got)
	}
}

// TestRankNotAnalysed checks that rank's table ends with a line counting
// the packages it left out, and that a module without any has none.
func TestRankNotAnalysed(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	for _, tc := range []struct {
		dir  string
		want bool
	}{{notAnalysedDir, true}, {fixtureDir, false}} {
		var stdout, stderr bytes.Buffer
		args := []string{"rank", tc.dir}
		if got := run(args, &stdout, &stderr); got != exitOK {
			t.Fatalf("run(%q) = %d, stderr %q", args, got, stderr.String())
		}
		const line = "\n1 package not analysed; --debug names it.\n"
		if got := strings.HasSuffix(stdout.String(), line); got != tc.want || (!tc.want && strings.Contains(stdout.String(), "not analysed")) {
			t.Errorf("%s: footer counts packages not analysed %v, want %v:\n%s", tc.dir, got, tc.want, stdout.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"rank", notAnalysedDir, "--json"}, &stdout, &stderr); got != exitOK || strings.Contains(stdout.String(), "not analysed") {
		t.Errorf("rank --json = %d with stdout\n%s\nwant no footer", got, stdout.String())
	}
}
