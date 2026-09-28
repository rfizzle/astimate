package engine

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/metricstest"
	"github.com/rfizzle/astimate/internal/report"
)

// tsOverride is a languages section that relaxes the tokens_est ceiling,
// drops has_tests and doubles the context budget for TypeScript only.
const tsOverride = `
languages:
  typescript:
    rebuild:
      context_budget: 50000
    thresholds:
      - metric: tokens_est
        kind: capacity
        max: 50000
      - metric: has_tests
        disabled: true
`

// defaultTop returns the embedded default up to its languages section, the
// last key, so a test can append a languages section of its own in place
// of the shipped one; the whole default when it has none.
func defaultTop() []byte {
	top, _, _ := strings.Cut(string(config.Default()), "\nlanguages:\n")
	return []byte(top + "\n")
}

// overrideConfig parses the embedded default with its languages section
// replaced by tsOverride.
func overrideConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse(append(defaultTop(), tsOverride...))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	return cfg
}

// findingMetrics lists the metric of each finding.
func findingMetrics(fs []report.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Metric)
	}
	return out
}

// TestCheckLanguageOverride checks the same metrics under a Go and a
// TypeScript extractor with one config: only the TypeScript row takes the
// override.
func TestCheckLanguageOverride(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	pkg := modPath + "/big"
	pkgs := map[string]metrics.RawMetrics{
		pkg: {TokensEst: 40000, SLOC: 700, ExportedSymbols: 10},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	// The baseline lacks big, so it is judged as new.
	other := map[string]metrics.RawMetrics{modPath + "/other": {}}
	if err := baseline.Write(path, "", modPath, TokenizerEst, other); err != nil {
		t.Fatalf("writing baseline: %v", err)
	}
	cfg := overrideConfig(t)

	tests := []struct {
		lang           string
		wantViolations []string
		wantWarnings   []string
		wantVersion    string
		wantPasses     float64
	}{
		{lang: "go", wantViolations: []string{"has_tests", "tokens_est"},
			wantVersion: "thresholds-2026-09-28", wantPasses: 1.9},
		{lang: "typescript", wantWarnings: []string{"tokens_est"},
			wantVersion: "thresholds-2026-09-28+typescript", wantPasses: 0.8},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			t.Parallel()

			tg := &Target{
				Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
				Ext: metricstest.NewFake(tt.lang, root, pkgs),
				Cfg: cfg,
			}
			c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, Packages: []string{pkg}})
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = (%v, %v), want no error", failed, err)
			}
			if len(c.Packages) != 1 {
				t.Fatalf("checked %d packages, want 1", len(c.Packages))
			}
			r := c.Packages[0].Report
			if got := findingMetrics(r.Violations); !slices.Equal(got, tt.wantViolations) {
				t.Errorf("violations = %v, want %v", got, tt.wantViolations)
			}
			if got := findingMetrics(r.Warnings); !slices.Equal(got, tt.wantWarnings) {
				t.Errorf("warnings = %v, want %v", got, tt.wantWarnings)
			}
			if r.ConfigVersion != tt.wantVersion {
				t.Errorf("config_version = %q, want %q", r.ConfigVersion, tt.wantVersion)
			}
			if r.Rebuild.AgentPasses != tt.wantPasses {
				t.Errorf("agent_passes = %v, want %v", r.Rebuild.AgentPasses, tt.wantPasses)
			}
		})
	}
}

// TestDefaultTypeScriptOverride checks the same new package under a Go and
// a TypeScript extractor with the embedded default: the TypeScript row is
// judged by the shipped typescript override's larger size limits, the Go
// row by the top level.
func TestDefaultTypeScriptOverride(t *testing.T) {
	t.Parallel()

	const root, modPath = "/mod", "example.com/m"
	pkg := modPath + "/big"
	pkgs := map[string]metrics.RawMetrics{
		pkg: {TokensEst: 20000, SLOC: 2000, LargestFileSLOC: 500, ExportedSymbols: 10},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	other := map[string]metrics.RawMetrics{modPath + "/other": {}}
	if err := baseline.Write(path, "", modPath, TokenizerEst, other); err != nil {
		t.Fatalf("writing baseline: %v", err)
	}
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}

	tests := []struct {
		lang           string
		wantViolations []string
		wantWarnings   []string
		wantVersion    string
	}{
		{lang: "go", wantViolations: []string{"has_tests", "sloc", "tokens_est"}, wantWarnings: []string{"largest_file_sloc"},
			wantVersion: "thresholds-2026-09-28"},
		{lang: "typescript", wantViolations: []string{"has_tests"}, wantWarnings: []string{"sloc"},
			wantVersion: "thresholds-2026-09-28+typescript"},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			t.Parallel()

			tg := &Target{
				Mod: &metrics.ModuleContext{Root: root, ModulePath: modPath},
				Ext: metricstest.NewFake(tt.lang, root, pkgs),
				Cfg: cfg,
			}
			c, failed, err := Check(t.Context(), tg, CheckOptions{BaselineFile: path, Packages: []string{pkg}})
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = (%v, %v), want no error", failed, err)
			}
			if len(c.Packages) != 1 {
				t.Fatalf("checked %d packages, want 1", len(c.Packages))
			}
			r := c.Packages[0].Report
			if got := findingMetrics(r.Violations); !slices.Equal(got, tt.wantViolations) {
				t.Errorf("violations = %v, want %v", got, tt.wantViolations)
			}
			if got := findingMetrics(r.Warnings); !slices.Equal(got, tt.wantWarnings) {
				t.Errorf("warnings = %v, want %v", got, tt.wantWarnings)
			}
			if r.ConfigVersion != tt.wantVersion {
				t.Errorf("config_version = %q, want %q", r.ConfigVersion, tt.wantVersion)
			}
		})
	}
}

// TestLanguageOverrideFixtures assesses the Go and TypeScript fixtures with
// one config carrying a TypeScript override: the TypeScript report shows
// the override and its rebuild budget, the Go report neither.
func TestLanguageOverrideFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go and TypeScript packages")
	}
	t.Parallel()

	cfg := overrideConfig(t)
	tests := []struct {
		name        string
		dir         string
		wantVersion string
		wantBudget  float64
	}{
		{name: "go", dir: filepath.Join(fixtureDir, "hub"), wantVersion: "thresholds-2026-09-28", wantBudget: 25000},
		{name: "typescript", dir: "../../testdata/ts/fixture/hub", wantVersion: "thresholds-2026-09-28+typescript", wantBudget: 50000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tg, err := LoadTarget(tt.dir, TargetOptions{Config: cfg})
			if err != nil {
				t.Fatalf("LoadTarget: %v", err)
			}
			if got := tg.Ext.Language(); got != tt.name {
				t.Fatalf("language = %q, want %q", got, tt.name)
			}
			r, err := Assess(t.Context(), tg, AssessOptions{})
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if r.ConfigVersion != tt.wantVersion {
				t.Errorf("config_version = %q, want %q", r.ConfigVersion, tt.wantVersion)
			}
			if got := tg.langConfig().Rebuild.ContextBudget; got != tt.wantBudget {
				t.Errorf("context_budget = %v, want %v", got, tt.wantBudget)
			}
		})
	}
}
