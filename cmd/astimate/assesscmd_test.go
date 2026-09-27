package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/score"
)

// fixtureDir is the fixture module, relative to this package's directory.
const fixtureDir = "../../testdata/go/fixture"

// fixturePackages are the package directories of the fixture module.
func fixturePackages() []string {
	return []string{"a", "b", "dupes", "hidden", "hub", "tested", "trivial"}
}

// assertKeys decodes the JSON object data and checks that its keys are
// exactly want, which must be sorted.
func assertKeys(t *testing.T, what string, data []byte, want []string) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("decoding %s object %s: %v", what, data, err)
	}
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("%s keys = %q, want %q", what, got, want)
	}
}

func sorted(s ...string) []string {
	slices.Sort(s)
	return s
}

func TestAssessJSONSchema(t *testing.T) {
	t.Parallel()

	// SPEC.md 10.2 keys an assess report carries; baseline, violations,
	// warnings and passed are omitted because no baseline or gate ran.
	// The Go extractor records details, at least the largest file, for
	// every package.
	wantTop := sorted("language", "package_path", "module_path", "rebuild", "suggestions",
		"metrics", "details", "astimate_version", "config_version")
	wantRebuild := sorted("agent_passes", "rebuild_tokens", "human_days", "tier", "calibrated", "drivers")
	wantDriver := sorted("term", "tokens", "detail")
	wantMetrics := sorted(metrics.MetricNames()...)

	for _, pkg := range fixturePackages() {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			args := []string{"assess", "--json", filepath.Join(fixtureDir, pkg)}
			if got := run(args, &stdout, &stderr); got != exitOK {
				t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
			}

			var r report.Report
			dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&r); err != nil {
				t.Fatalf("decoding report: %v\n%s", err, stdout.String())
			}
			if r.Language != "go" || r.PackagePath != pkg || r.ModulePath != "example.com/fixture" {
				t.Errorf("identity = (%q, %q, %q), want (go, %q, example.com/fixture)",
					r.Language, r.PackagePath, r.ModulePath, pkg)
			}
			if r.ConfigVersion == "" || r.AstimateVersion == "" {
				t.Errorf("config_version = %q, astimate_version = %q, want both set", r.ConfigVersion, r.AstimateVersion)
			}
			if r.Baseline != nil || r.Violations != nil || r.Warnings != nil || r.Passed != nil {
				t.Errorf("baseline, violations, warnings, passed = %v, %v, %v, %v; want all absent",
					r.Baseline, r.Violations, r.Warnings, r.Passed)
			}

			var top map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &top); err != nil {
				t.Fatalf("decoding report object: %v", err)
			}
			var rebuild struct {
				Drivers []json.RawMessage `json:"drivers"`
			}
			if err := json.Unmarshal(top["rebuild"], &rebuild); err != nil {
				t.Fatalf("decoding rebuild block: %v", err)
			}
			if len(rebuild.Drivers) == 0 {
				t.Error("rebuild.drivers is empty; every fixture package has non-zero terms")
			}
			assertKeys(t, "top-level", stdout.Bytes(), wantTop)
			assertKeys(t, "rebuild", top["rebuild"], wantRebuild)
			assertKeys(t, "metrics", top["metrics"], wantMetrics)
			for _, d := range rebuild.Drivers {
				assertKeys(t, "driver", d, wantDriver)
			}
			if !bytes.HasPrefix(bytes.TrimSpace(top["suggestions"]), []byte("[")) {
				t.Errorf("suggestions = %s, want an array", top["suggestions"])
			}
		})
	}
}

func TestAssessTrivialTier(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	args := []string{"assess", filepath.Join(fixtureDir, "trivial")}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "tier: "+string(score.TierOnePass)+"\n") {
		t.Errorf("table output does not show tier ONE_PASS:\n%s", stdout.String())
	}
}

func TestAssessTableGolden(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	args := []string{"assess", filepath.Join(fixtureDir, "trivial")}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}

	golden := filepath.Join("testdata", "assess_trivial.txt")
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
}

func TestAssessExitCodes(t *testing.T) {
	t.Parallel()

	notDir := filepath.Join(fixtureDir, "trivial", "trivial.go")
	tests := []struct {
		name       string
		args       []string
		want       int
		wantStderr string
	}{
		{name: "no package dir", args: []string{"assess"}, want: exitUsage},
		{name: "two package dirs", args: []string{"assess", "a", "b"}, want: exitUsage},
		{name: "unknown flag", args: []string{"assess", "--bogus", "."}, want: exitUsage},
		{name: "unknown tokenizer", args: []string{"assess", "--tokenizer", "cl100k", "."}, want: exitUsage, wantStderr: "cl100k"},
		{name: "non-module path", args: []string{"assess", t.TempDir()}, want: exitAnalysis, wantStderr: "go.mod"},
		{name: "missing dir", args: []string{"assess", filepath.Join(t.TempDir(), "absent")}, want: exitAnalysis},
		{name: "file not dir", args: []string{"assess", notDir}, want: exitAnalysis, wantStderr: "not a directory"},
		{name: "dir without package", args: []string{"assess", filepath.Join(fixtureDir, "golden")}, want: exitAnalysis, wantStderr: "unknown package"},
		{name: "missing config", args: []string{"assess", "--config", filepath.Join(t.TempDir(), "none.yaml"), filepath.Join(fixtureDir, "trivial")}, want: exitAnalysis, wantStderr: "config"},
		{name: "flags after dir", args: []string{"assess", filepath.Join(fixtureDir, "trivial"), "--json"}, want: exitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != tt.want {
				t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", tt.args, got, tt.want, stderr.String())
			}
			if tt.want != exitOK && stdout.Len() != 0 {
				t.Errorf("run(%q) stdout = %q, want empty on failure", tt.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("run(%q) stderr = %q, want it to mention %q", tt.args, stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestAssessNamesUntestedExports(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	args := []string{"assess", "--json", filepath.Join(fixtureDir, "dupes")}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	var r report.Report
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("decoding report: %v\n%s", err, stdout.String())
	}
	want := "3 exported functions have no test (CountVisits, SumOrders, TallyScores); " +
		"a rebuild would have to reverse-engineer their behavior."
	if !slices.Contains(r.Suggestions, want) {
		t.Errorf("suggestions = %q, want one to be %q", r.Suggestions, want)
	}
}

func TestAssessJSONDetails(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	args := []string{"assess", "--json", filepath.Join(fixtureDir, "dupes")}
	if got := run(args, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(%q) exit code = %d, want %d; stderr = %q", args, got, exitOK, stderr.String())
	}
	var r report.Report
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("decoding report: %v\n%s", err, stdout.String())
	}
	if r.Details == nil {
		t.Fatalf("report has no details:\n%s", stdout.String())
	}
	// The three copies of one block in dupes.go, files relative to the
	// package directory.
	wantDups := []report.Span{
		{File: "dupes.go", StartLine: 9, EndLine: 26},
		{File: "dupes.go", StartLine: 31, EndLine: 48},
		{File: "dupes.go", StartLine: 53, EndLine: 70},
	}
	if !slices.Equal(r.Details.Duplicates, wantDups) {
		t.Errorf("details.duplicates = %+v, want %+v", r.Details.Duplicates, wantDups)
	}
	if r.Metrics.DupBlocks == 0 {
		t.Error("dup_blocks = 0, want the fixture's duplicate block counted")
	}
	wantUntested := []report.Declaration{
		{Name: "CountVisits", File: "dupes.go", Line: 53},
		{Name: "SumOrders", File: "dupes.go", Line: 9},
		{Name: "TallyScores", File: "dupes.go", Line: 31},
	}
	if !slices.Equal(r.Details.UntestedExports, wantUntested) {
		t.Errorf("details.untested_exports = %+v, want %+v", r.Details.UntestedExports, wantUntested)
	}
	if r.Details.LargestFile != "dupes.go" {
		t.Errorf("details.largest_file = %q, want dupes.go", r.Details.LargestFile)
	}
}
