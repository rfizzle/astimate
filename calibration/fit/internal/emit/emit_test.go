package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/fit/internal/pool"
	"github.com/rfizzle/astimate/internal/config"
)

// synthRows returns n rows with sloc, largest_file_sloc and
// duplication_pct running 1..n and has_tests set.
func synthRows(n int) []pool.Row {
	rows := make([]pool.Row, n)
	for i := range rows {
		v := i + 1
		rows[i].Module = "example.com/m"
		rows[i].Metrics.SLOC = v
		rows[i].Metrics.LargestFileSLOC = v
		rows[i].Metrics.DuplicationPct = float64(v)
		rows[i].Metrics.HasTests = true
	}
	return rows
}

// TestCandidate fits synthetic rows against the embedded default and
// checks Candidate produces a candidate that validates, carries the
// new version and header, and has the fitted sloc max.
func TestCandidate(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	choices := pool.FitThresholds(synthRows(100), base)
	out, err := Candidate(config.Default(), "thresholds-test", "# header\n", choices)
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}
	if !bytes.Contains(out, []byte("# header")) {
		t.Error("candidate does not carry the header")
	}
	cfg, err := config.Parse(out)
	if err != nil {
		t.Fatalf("candidate does not validate: %v", err)
	}
	if cfg.Version != "thresholds-test" {
		t.Errorf("config_version = %q, want thresholds-test", cfg.Version)
	}
	for _, r := range cfg.Thresholds {
		if r.Metric == "sloc" && (r.Max == nil || *r.Max != 90) {
			t.Errorf("sloc max = %v, want 90 (the fitted p90)", r.Max)
		}
	}
}

// TestCandidateMismatch checks Candidate rejects a choices slice
// that does not match the base thresholds.
func TestCandidateMismatch(t *testing.T) {
	_, err := Candidate(config.Default(), "v", "# h\n", nil)
	if err == nil {
		t.Error("Candidate with no choices: want an error")
	}
}

// TestLanguage fits synthetic typescript rows and checks Language
// writes a languages.typescript override block that merges and validates,
// keeping the base's config_version with the language suffix.
func TestLanguage(t *testing.T) {
	base, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rows := synthRows(100)
	for i := range rows {
		rows[i].Language = "typescript"
	}
	choices := pool.FitThresholds(rows, base)
	version := base.Version + "+typescript"
	block, err := Language(config.Default(), "typescript", version, "data.jsonl", 100, choices)
	if err != nil {
		t.Fatalf("Language: %v", err)
	}
	if bytes.Contains(block, []byte("config_version")) {
		t.Error("override block sets config_version")
	}
	if !bytes.Contains(block, []byte("languages:\n  typescript:")) {
		t.Error("override block does not carry languages.typescript")
	}
	merged, err := MergeLanguage(config.Default(), "typescript", block)
	if err != nil {
		t.Fatalf("MergeLanguage: %v", err)
	}
	cfg, err := config.Parse(merged)
	if err != nil {
		t.Fatalf("merged override does not validate: %v", err)
	}
	if got := cfg.ForLanguage("typescript").Version; got != version {
		t.Errorf("typescript version = %q, want %q", got, version)
	}
}

// TestMergeLanguageRejectsMissingBlock checks MergeLanguage rejects a
// block with no languages.<lang> entry.
func TestMergeLanguageRejectsMissingBlock(t *testing.T) {
	_, err := MergeLanguage(config.Default(), "typescript", []byte("foo: bar\n"))
	if err == nil || !strings.Contains(err.Error(), "no languages.typescript") {
		t.Errorf("MergeLanguage with no override: err = %v, want a missing-block error", err)
	}
}
