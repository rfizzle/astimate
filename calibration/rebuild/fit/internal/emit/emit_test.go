package emit_test

import (
	"os"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/emit"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/score"
)

// base returns the pre-calibration placeholder configuration, a fixed base
// whatever the shipped default holds.
func base(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../../../configs/uncalibrated.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fitted(t *testing.T) score.RebuildParams {
	t.Helper()
	def, err := config.Parse(base(t))
	if err != nil {
		t.Fatal(err)
	}
	p := def.Rebuild
	p.ContextBudget, p.TokensPerExport, p.TokensPerUntestedExport = 30000, 61, 520
	p.TokensPerHiddenState, p.SuperlinearExponent = 0.35, 1.46
	return p
}

func TestConfig(t *testing.T) {
	p := fitted(t)
	out, err := emit.Config(base(t), "rebuild-2026-09-28-claude-code", p)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Rebuild != p || cfg.Version != "rebuild-2026-09-28-claude-code" || !score.Calibrated(cfg.Version) {
		t.Errorf("read back %s %+v", cfg.Version, cfg.Rebuild)
	}
	// Only the six lines changed, and their comments stayed.
	before, got := strings.Split(string(base(t)), "\n"), strings.Split(string(out), "\n")
	if len(before) != len(got) {
		t.Fatalf("%d lines, base has %d", len(got), len(before))
	}
	var changed []string
	for i := range before {
		if before[i] != got[i] {
			changed = append(changed, got[i])
		}
	}
	want := []string{
		"config_version: rebuild-2026-09-28-claude-code",
		"  context_budget: 30000",
		"  tokens_per_export: 61",
		"  tokens_per_untested_export: 520",
		"  tokens_per_hidden_state: 0.35",
		"  superlinear_exponent: 1.46",
	}
	if strings.Join(changed, "\n") != strings.Join(want, "\n") {
		t.Errorf("changed lines:\n%s\nwant:\n%s", strings.Join(changed, "\n"), strings.Join(want, "\n"))
	}
}

func TestConfigKeepsTrailingComments(t *testing.T) {
	commented := strings.Replace(string(base(t)), "  tokens_per_export: 40\n", "  tokens_per_export: 40 # per symbol\n", 1)
	out, err := emit.Config([]byte(commented), "rebuild-x", fitted(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "  tokens_per_export: 61 # per symbol\n") {
		t.Error("trailing comment lost")
	}
}

func TestConfigRefuses(t *testing.T) {
	def := string(base(t))
	bad := fitted(t)
	bad.SuperlinearExponent = 0.5
	tests := []struct {
		name    string
		base    string
		version string
		p       score.RebuildParams
		want    string
	}{
		{"not calibrated", def, "thresholds-2026-09-28", fitted(t), "not a rebuild calibration"},
		{"invalid", def, "rebuild-x", bad, "does not validate"},
		{"no rebuild section", strings.Replace(def, "\nrebuild:\n", "\nrebuilt:\n", 1), "rebuild-x", fitted(t), "no top-level rebuild"},
		{"missing key", strings.Replace(def, "  tokens_per_hidden_state: 400\n", "", 1), "rebuild-x", fitted(t), "tokens_per_hidden_state"},
		{"no version", strings.Replace(def, "config_version: ", "# config_version: ", 1), "rebuild-x", fitted(t), "config_version"},
		{"duplicate key", strings.Replace(def, "  context_budget: 25000\n", "  context_budget: 25000\n  context_budget: 1\n", 1),
			"rebuild-x", fitted(t), "more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := emit.Config([]byte(tt.base), tt.version, tt.p); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err %v, want %q", err, tt.want)
			}
		})
	}
}
