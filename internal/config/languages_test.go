package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
)

// withLanguages returns the embedded default with section appended.
func withLanguages(section string) []byte {
	return append(Default(), []byte("\n"+section)...)
}

// ruleMetrics lists the metric of each rule, in order.
func ruleMetrics(rules []gate.Threshold) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Metric)
	}
	return out
}

// ruleOn returns the first rule on metric, failing the test when none is.
func ruleOn(t *testing.T, rules []gate.Threshold, metric string) gate.Threshold {
	t.Helper()
	for _, r := range rules {
		if r.Metric == metric {
			return r
		}
	}
	t.Fatalf("no rule on %q in %v", metric, ruleMetrics(rules))
	return gate.Threshold{}
}

func TestDefaultHasNoLanguageOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(Default())
	if err != nil {
		t.Fatalf("Parse(Default()) error = %v", err)
	}
	for _, lang := range []string{"go", "typescript", "rust"} {
		eff := cfg.ForLanguage(lang)
		if eff.Version != cfg.Version || eff.Rebuild != cfg.Rebuild ||
			!slices.Equal(ruleMetrics(eff.Thresholds), ruleMetrics(cfg.Thresholds)) {
			t.Errorf("ForLanguage(%q) = %+v, want the top level unchanged", lang, eff)
		}
	}
}

// TestDefaultLanguagesExample checks that the commented languages example
// in default.yaml parses once uncommented and does what its comment says.
func TestDefaultLanguagesExample(t *testing.T) {
	t.Parallel()

	d := string(Default())
	i := strings.Index(d, "# languages:\n")
	if i < 0 {
		t.Fatal("default.yaml has no commented languages example")
	}
	var b strings.Builder
	b.WriteString(d[:i])
	for line := range strings.Lines(d[i:]) {
		line = strings.TrimPrefix(line, "#")
		b.WriteString(strings.TrimPrefix(line, " "))
	}
	cfg, err := Parse([]byte(b.String()))
	if err != nil {
		t.Fatalf("Parse(uncommented example) error = %v", err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", cfg.Warnings)
	}
	ts := cfg.ForLanguage("typescript")
	if ts.Version != "thresholds-2026-09-28+typescript" {
		t.Errorf("typescript Version = %q", ts.Version)
	}
	if ts.Rebuild.TokensPerExport != 30 || ts.Rebuild.ContextBudget != cfg.Rebuild.ContextBudget {
		t.Errorf("typescript Rebuild = %+v, want tokens_per_export 30 and the rest inherited", ts.Rebuild)
	}
	if got := *ruleOn(t, ts.Thresholds, "largest_file_sloc").Max; got != 1000 {
		t.Errorf("typescript largest_file_sloc max = %v, want 1000", got)
	}
	if slices.Contains(ruleMetrics(ts.Thresholds), "init_funcs") {
		t.Error("typescript still gates init_funcs, want it disabled")
	}
	goEff := cfg.ForLanguage("go")
	if goEff.Version != cfg.Version || goEff.Rebuild != cfg.Rebuild || len(goEff.Thresholds) != len(cfg.Thresholds) {
		t.Errorf("go = %+v, want the top level unchanged", goEff)
	}
}

func TestForLanguage(t *testing.T) {
	t.Parallel()

	const section = `languages:
  typescript:
    rebuild:
      context_budget: 50000
      tiers:
        few_passes_max: 4
    thresholds:
      - metric: tokens_est
        kind: capacity
        max: 50000
      - metric: cognitive_p90
        kind: density
        max_delta: 5
      - metric: cognitive_p90
        kind: density
        max: 40
      - metric: has_tests
        disabled: true
      - metric: dup_blocks_cross_pkg
        kind: density
        max_delta: 0
        ratchet_from_zero: true
  go: {}
`
	cfg, err := Parse(withLanguages(section))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", cfg.Warnings)
	}

	t.Run("override present", func(t *testing.T) {
		t.Parallel()

		eff := cfg.ForLanguage("typescript")
		if eff.Version != cfg.Version+"+typescript" {
			t.Errorf("Version = %q, want %q", eff.Version, cfg.Version+"+typescript")
		}
		want := cfg.Rebuild
		want.ContextBudget = 50000
		want.Tiers.FewPassesMax = 4
		if eff.Rebuild != want {
			t.Errorf("Rebuild = %+v, want %+v", eff.Rebuild, want)
		}
		// Replaced rules keep their position; cognitive_p90's two override
		// rules both replace its one top-level rule; has_tests is dropped;
		// the new metric is appended.
		var wantMetrics []string
		for _, m := range ruleMetrics(cfg.Thresholds) {
			switch m {
			case "has_tests":
			case "cognitive_p90":
				wantMetrics = append(wantMetrics, m, m)
			default:
				wantMetrics = append(wantMetrics, m)
			}
		}
		wantMetrics = append(wantMetrics, "dup_blocks_cross_pkg")
		if got := ruleMetrics(eff.Thresholds); !slices.Equal(got, wantMetrics) {
			t.Errorf("rule metrics = %v, want %v", got, wantMetrics)
		}
		if got := *ruleOn(t, eff.Thresholds, "tokens_est").Max; got != 50000 {
			t.Errorf("tokens_est max = %v, want 50000", got)
		}
		if got := ruleOn(t, eff.Thresholds, "tokens_est").WarnAt; got != defaultWarnAt {
			t.Errorf("tokens_est warn_at = %v, want default %v", got, defaultWarnAt)
		}
	})
	t.Run("top level untouched", func(t *testing.T) {
		t.Parallel()

		if got := *ruleOn(t, cfg.Thresholds, "tokens_est").Max; got != 16000 {
			t.Errorf("top-level tokens_est max = %v, want 16000", got)
		}
		if cfg.Rebuild.ContextBudget != 25000 {
			t.Errorf("top-level context_budget = %v, want 25000", cfg.Rebuild.ContextBudget)
		}
	})
	for _, lang := range []string{"go", "rust"} {
		t.Run("no override for "+lang, func(t *testing.T) {
			t.Parallel()

			eff := cfg.ForLanguage(lang)
			if eff.Version != cfg.Version || eff.Rebuild != cfg.Rebuild ||
				!slices.Equal(ruleMetrics(eff.Thresholds), ruleMetrics(cfg.Thresholds)) {
				t.Errorf("ForLanguage(%q) = %+v, want the top level unchanged", lang, eff)
			}
		})
	}
}

func TestParseLanguageWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		section string
		want    string
	}{
		{name: "disable ungated metric", section: "languages:\n  go:\n    thresholds:\n      - metric: fan_in\n        disabled: true\n",
			want: `languages.go.thresholds[0] "fan_in": disables a metric the top-level thresholds do not gate`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Parse(withLanguages(tt.section))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], tt.want) {
				t.Errorf("Warnings = %q, want one containing %q", cfg.Warnings, tt.want)
			}
		})
	}
}

// TestParseAcceptsAnyLanguage checks that Parse keeps an override for an id
// no shipped extractor reports without judging it, and lists every id.
func TestParseAcceptsAnyLanguage(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(withLanguages("languages:\n  rust:\n    rebuild:\n      cocomo_a: 3\n  go: {}\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", cfg.Warnings)
	}
	if got, want := cfg.Languages(), []string{"go", "rust"}; !slices.Equal(got, want) {
		t.Errorf("Languages() = %q, want %q", got, want)
	}
	if got := cfg.ForLanguage("rust").Rebuild.CocomoA; got != 3 {
		t.Errorf("ForLanguage(rust).Rebuild.CocomoA = %v, want 3", got)
	}
}

func TestParseLanguageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		section string
		wantErr string
	}{
		{name: "unknown key", section: "languages:\n  typescript:\n    rebuilds: {}\n", wantErr: "rebuilds"},
		{name: "unknown rebuild key", section: "languages:\n  typescript:\n    rebuild:\n      budget: 1\n", wantErr: "budget"},
		{name: "rebuild out of range", section: "languages:\n  typescript:\n    rebuild:\n      superlinear_exponent: 0.5\n",
			wantErr: "languages.typescript.rebuild: superlinear_exponent must be >= 1"},
		{name: "tiers out of order after merge", section: "languages:\n  go:\n    rebuild:\n      tiers:\n        few_passes_max: 0.5\n",
			wantErr: "languages.go.rebuild: tiers.few_passes_max must be > tiers.one_pass_max"},
		{name: "malformed rule", section: "languages:\n  typescript:\n    thresholds:\n      - metric: has_tests\n        disabled: true\n      - metric: sloc\n        kind: capacity\n",
			wantErr: `languages.typescript.thresholds[1]: threshold "sloc": capacity rule needs max`},
		{name: "unknown metric", section: "languages:\n  typescript:\n    thresholds:\n      - metric: slocs\n        kind: capacity\n        max: 1\n",
			wantErr: `languages.typescript.thresholds[0]: threshold "slocs": unknown metric`},
		{name: "rebuild output", section: "languages:\n  typescript:\n    thresholds:\n      - metric: agent_passes\n        kind: capacity\n        max: 1\n",
			wantErr: `languages.typescript.thresholds[0]: "agent_passes" is a rebuild output`},
		{name: "malformed when", section: "languages:\n  typescript:\n    thresholds:\n      - metric: has_tests\n        kind: requirement\n        require: true\n        when: sloc >= 1\n",
			wantErr: `languages.typescript.thresholds[0] "has_tests": when:`},
		{name: "disabled with limits", section: "languages:\n  typescript:\n    thresholds:\n      - metric: sloc\n        disabled: true\n        max: 1\n",
			wantErr: `languages.typescript.thresholds[0] "sloc": a disabled rule sets only metric and disabled`},
		{name: "disabled unknown metric", section: "languages:\n  typescript:\n    thresholds:\n      - metric: slocs\n        disabled: true\n",
			wantErr: `languages.typescript.thresholds: disabled "slocs": unknown metric`},
		{name: "disabled and overridden", section: "languages:\n  typescript:\n    thresholds:\n      - metric: sloc\n        disabled: true\n      - metric: sloc\n        kind: capacity\n        max: 1\n",
			wantErr: `languages.typescript.thresholds: "sloc" is both disabled and overridden`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(withLanguages(tt.section))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
	t.Run("disabled at top level", func(t *testing.T) {
		t.Parallel()

		_, err := Parse([]byte(replace(t, "    kind: requirement\n", "    kind: requirement\n    disabled: true\n")))
		const want = `thresholds[13] "has_tests": disabled applies only under languages`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Parse() error = %v, want error containing %q", err, want)
		}
	})
}
