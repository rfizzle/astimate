package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/narrate"
)

// reportFixture copies the fixture and the module its replace directive
// points at into a temporary directory outside git, so the report has no
// head and no churn, and returns the fixture's copy.
func reportFixture(t *testing.T) (repo, fixture string) {
	t.Helper()
	repo = t.TempDir()
	copyTree(t, fixtureDir, filepath.Join(repo, "fixture"))
	copyTree(t, extmodDir, filepath.Join(repo, "extmod"))
	return repo, filepath.Join(repo, "fixture")
}

// TestReportJSONGolden checks report --format json on the fixture against
// testdata/report_fixture.json (regenerate with UPDATE_GOLDEN=1). The
// astimate version is replaced, so the golden does not move with the
// build.
func TestReportJSONGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	_, fixture := reportFixture(t)
	var stdout, stderr bytes.Buffer
	args := []string{"report", fixture, "--format", "json", "--config", pinnedConfigFile(t)}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	if strings.Contains(stderr.String(), "text output is coming") {
		t.Errorf("stderr = %q, want no notice with --format json", stderr.String())
	}
	got := strings.Replace(stdout.String(), `"astimate_version": "`+astimateVersion()+`"`, `"astimate_version": "test"`, 1)

	golden := filepath.Join("testdata", "report_fixture.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file (regenerate with UPDATE_GOLDEN=1): %v", err)
	}
	if got != string(want) {
		t.Errorf("report differs from %s (regenerate with UPDATE_GOLDEN=1)\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
	var m narrate.Module
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	if m.Change != nil || m.Head != "" || len(m.Packages) == 0 || len(m.Glossary) == 0 {
		t.Errorf("report = %+v, want packages and a glossary, no head and no change", m)
	}
}

// TestReportBase checks the change section of report --base: the fixture
// committed, then one package degraded in the working tree.
func TestReportBase(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages and runs git")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	repo, fixture := reportFixture(t)
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "pristine fixture")
	copyTree(t, filepath.Join(degradedDir, "tested"), filepath.Join(fixture, "tested"))

	var stdout, stderr bytes.Buffer
	args := []string{"report", fixture, "--base", "master", "--config", pinnedConfigFile(t)}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d: a failing change is reported, not gated; stderr = %q",
			args, got, exitOK, stderr.String())
	}
	if !strings.Contains(stderr.String(), "text output is coming") {
		t.Errorf("stderr = %q, want the notice that text output is coming", stderr.String())
	}
	var m narrate.Module
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	c := m.Change
	if c == nil || m.Head == "" {
		t.Fatalf("change = %v, head = %q, want both", c, m.Head)
	}
	if c.Base != "master" || c.BaseCommit == "" || c.Verdict != "FAIL" || c.Changed != 1 || c.Failed == 0 ||
		c.Failing[len(c.Failing)-1].Path != "tested" {
		t.Errorf("change = %+v, want tested failing against master", *c)
	}
	for _, f := range c.Failing {
		for _, v := range f.Findings {
			if v.Location == nil && f.Path == "tested" {
				t.Errorf("finding %s on tested has no location", v.Metric)
			}
		}
	}
}

func TestReportUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown format", []string{"report", "--format", "pdf"}, `unknown format "pdf"`},
		{"two roots", []string{"report", "a", "b"}, "want at most one module root"},
		{"bad tokenizer", []string{"report", "--tokenizer", "nope"}, "unknown tokenizer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != exitUsage {
				t.Errorf("exit code = %d, want %d", got, exitUsage)
			}
			if !strings.Contains(stderr.String(), tt.want) || stdout.Len() != 0 {
				t.Errorf("stderr = %q, want %q and no stdout", stderr.String(), tt.want)
			}
		})
	}
}

// TestReportHTML checks report --format html --out on the fixture: the page
// is written where --out says, its path is printed, and nothing else is.
func TestReportHTML(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	_, fixture := reportFixture(t)
	out := filepath.Join(t.TempDir(), "page.html")
	var stdout, stderr bytes.Buffer
	args := []string{"report", fixture, "--format", "html", "--out", out, "--config", pinnedConfigFile(t)}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	if stdout.String() != out+"\n" {
		t.Errorf("stdout = %q, want the page's path", stdout.String())
	}
	page, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(page, []byte("<!DOCTYPE html>")) || !bytes.Contains(page, []byte("Fixture has no package too big")) {
		t.Errorf("page = %.200q, want the fixture's report", page)
	}
}
