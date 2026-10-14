package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compareGolden checks got against testdata/name, which UPDATE_GOLDEN=1
// rewrites.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", name)
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
		t.Errorf("output differs from %s (regenerate with UPDATE_GOLDEN=1)\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
}

// TestReportMarkdownGolden checks report --format md on the fixture against
// testdata/report_fixture.md.
func TestReportMarkdownGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	_, fixture := reportFixture(t)
	var stdout, stderr bytes.Buffer
	args := []string{"report", fixture, "--format", "md", "--config", pinnedConfigFile(t)}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want none", stderr.String())
	}
	compareGolden(t, "report_fixture.md", stdout.String())
}

// TestCheckMarkdownGolden checks check --format md on the degraded fixture
// against testdata/check_degraded.md and that the exit code is the gate's.
func TestCheckMarkdownGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	var stdout, stderr bytes.Buffer
	args := []string{"check", degradedDir, "--all", "--baseline", fixtureBaseline(t), "--config", pinnedConfigFile(t), "--format", formatMD}
	if got := run(args, &stdout, &stderr); got != exitGateFailed {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitGateFailed, stderr.String())
	}
	compareGolden(t, "check_degraded.md", stdout.String())
}

// TestCheckGitHubSummary checks that check --format github appends the
// Markdown to GITHUB_STEP_SUMMARY and leaves the annotations on stdout as
// they are without it.
func TestCheckGitHubSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}

	base, cfg := fixtureBaseline(t), pinnedConfigFile(t)
	args := []string{"check", degradedDir, "--all", "--baseline", base, "--config", cfg, "--format", formatGitHub}
	var plain, stderr bytes.Buffer
	if got := run(args, &plain, &stderr); got != exitGateFailed {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitGateFailed, stderr.String())
	}
	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	var withSummary bytes.Buffer
	if got := run(args, &withSummary, &stderr); got != exitGateFailed {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitGateFailed, stderr.String())
	}
	if withSummary.String() != plain.String() {
		t.Errorf("stdout with a summary =\n%s\nwant it unchanged:\n%s", withSummary.String(), plain.String())
	}
	data, err := os.ReadFile(summary)
	if err != nil {
		t.Fatalf("reading the step summary: %v", err)
	}
	if !strings.HasPrefix(string(data), "## astimate: FAIL — ") || !strings.Contains(string(data), "| `tested` |") {
		t.Errorf("step summary =\n%s\nwant the FAIL heading and a table row for tested", data)
	}
}
