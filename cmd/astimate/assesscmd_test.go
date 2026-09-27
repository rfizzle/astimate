package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
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
	wantTop := sorted("language", "package_path", "module_path", "rebuild", "suggestions",
		"metrics", "astimate_version", "config_version")
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

func TestPackagePaths(t *testing.T) {
	t.Parallel()

	root := filepath.FromSlash("/src/mod")
	tests := []struct {
		name           string
		dir            string
		wantPkgPath    string
		wantImportPath string
	}{
		{name: "module root", dir: root, wantPkgPath: ".", wantImportPath: "example.com/mod"},
		{name: "child", dir: filepath.Join(root, "hub"), wantPkgPath: "hub", wantImportPath: "example.com/mod/hub"},
		{name: "nested", dir: filepath.Join(root, "internal", "billing"),
			wantPkgPath: "internal/billing", wantImportPath: "example.com/mod/internal/billing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkgPath, importPath, err := packagePaths(root, tt.dir, "example.com/mod")
			if err != nil {
				t.Fatalf("packagePaths: %v", err)
			}
			if pkgPath != tt.wantPkgPath || importPath != tt.wantImportPath {
				t.Errorf("packagePaths(%q) = (%q, %q), want (%q, %q)",
					tt.dir, pkgPath, importPath, tt.wantPkgPath, tt.wantImportPath)
			}
		})
	}
}

func TestLoadTarget(t *testing.T) {
	t.Parallel()

	absFixture, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatalf("resolving fixture: %v", err)
	}
	tests := []struct {
		name           string
		dir            string
		wantPkgPath    string
		wantImportPath string
	}{
		{name: "relative package dir", dir: filepath.Join(fixtureDir, "hub"),
			wantPkgPath: "hub", wantImportPath: "example.com/fixture/hub"},
		{name: "module root", dir: fixtureDir, wantPkgPath: ".", wantImportPath: "example.com/fixture"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tg, err := loadTarget(tt.dir, targetFlags{tokenizer: tokenizerEst})
			if err != nil {
				t.Fatalf("loadTarget(%q): %v", tt.dir, err)
			}
			if tg.module.Root != absFixture || tg.module.ModulePath != "example.com/fixture" {
				t.Errorf("module = (%q, %q), want (%q, example.com/fixture)",
					tg.module.Root, tg.module.ModulePath, absFixture)
			}
			if tg.packagePath != tt.wantPkgPath || tg.importPath != tt.wantImportPath {
				t.Errorf("paths = (%q, %q), want (%q, %q)",
					tg.packagePath, tg.importPath, tt.wantPkgPath, tt.wantImportPath)
			}
		})
	}

	t.Run("non-module", func(t *testing.T) {
		t.Parallel()

		_, err := loadTarget(t.TempDir(), targetFlags{tokenizer: tokenizerEst})
		if !errors.Is(err, golang.ErrNoModule) {
			t.Errorf("loadTarget error = %v, want it to wrap golang.ErrNoModule", err)
		}
	})
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

// TestDupesDuplicationSuggestionNamesLocation checks the path check takes: the
// names assess resolves feed the duplication template, which cites the first
// occurrence of the first block.
func TestDupesDuplicationSuggestionNamesLocation(t *testing.T) {
	t.Parallel()

	tg, err := loadTarget(filepath.Join(fixtureDir, "dupes"), targetFlags{tokenizer: tokenizerEst})
	if err != nil {
		t.Fatalf("loadTarget: %v", err)
	}
	m, err := tg.extractor.Extract(t.Context(), tg.module, tg.importPath)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names, err := suggestionNames(t.Context(), tg.extractor, tg.module, tg.importPath)
	if err != nil {
		t.Fatalf("suggestionNames: %v", err)
	}
	got := score.MetricSuggestion("dup_blocks", float64(m.DupBlocks), m, names)
	want := "1 duplicate block covers 80.6% of lines; extract shared helpers, starting with dupes.go:9-26."
	if got != want {
		t.Errorf("dup_blocks suggestion = %q, want %q", got, want)
	}
}

func TestSuggestionNames(t *testing.T) {
	t.Parallel()

	const root = "/fake/module"
	pkgs := map[string]metrics.RawMetrics{"p": {UntestedExports: 1, DupBlocks: 1}}
	details := map[string]metrics.Details{"p": {
		UntestedExports:  []string{"Parse"},
		UntestedExcluded: []string{"Legacy"},
		DupLocations:     []string{"p.go:3-9", "q.go:1-7"},
	}}
	tests := []struct {
		name string
		ext  metrics.Extractor
		want score.Names
	}{
		{name: "detailer", ext: metricstest.NewFake("fake", root, pkgs, metricstest.WithDetails(details)),
			want: score.Names{UntestedExports: []string{"Parse"}, DupLocations: []string{"p.go:3-9", "q.go:1-7"}}},
		{name: "counts only", ext: metricstest.NewFake("fake", root, pkgs)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mod := &metrics.ModuleContext{Root: root}
			got, err := suggestionNames(t.Context(), tt.ext, mod, "p")
			if err != nil {
				t.Fatalf("suggestionNames: %v", err)
			}
			if !slices.Equal(got.UntestedExports, tt.want.UntestedExports) ||
				!slices.Equal(got.DupLocations, tt.want.DupLocations) {
				t.Errorf("suggestionNames = %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("unknown package", func(t *testing.T) {
		t.Parallel()

		ext := metricstest.NewFake("fake", root, pkgs, metricstest.WithDetails(details))
		if _, err := suggestionNames(t.Context(), ext, &metrics.ModuleContext{Root: root}, "absent"); err == nil {
			t.Error("suggestionNames on an unknown package returned no error")
		}
	})
}
