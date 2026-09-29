package config

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

func TestDefault(t *testing.T) {
	t.Parallel()

	got := Default()
	got[0] = 'X'
	if Default()[0] == 'X' {
		t.Error("Default() returned shared storage; mutation leaked")
	}
}

func TestParseDefault(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(Default())
	if err != nil {
		t.Fatalf("Parse(Default()) error = %v", err)
	}
	if cfg.Version != "rebuild-2026-09-28-claude-code-opus" {
		t.Errorf("Version = %q, want rebuild-2026-09-28-claude-code-opus", cfg.Version)
	}
	if cfg.CharsPerToken != 3.2 {
		t.Errorf("CharsPerToken = %v, want 3.2", cfg.CharsPerToken)
	}
	if want := (Duplication{MinTokens: 40, IgnoreLiteralOnly: true, FoldSigns: true}); cfg.Duplication != want {
		t.Errorf("Duplication = %+v, want %+v", cfg.Duplication, want)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", cfg.Warnings)
	}

	r := cfg.Rebuild
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"context_budget", r.ContextBudget, 37500},
		{"tokens_per_export", r.TokensPerExport, 0},
		{"tokens_per_untested_export", r.TokensPerUntestedExport, 300},
		{"tokens_per_hidden_state", r.TokensPerHiddenState, 0},
		{"superlinear_exponent", r.SuperlinearExponent, 1},
		{"cocomo_a", r.CocomoA, 2.4},
		{"cocomo_b", r.CocomoB, 1.05},
		{"days_per_month", r.DaysPerMonth, 19},
		{"tiers.one_pass_max", r.Tiers.OnePassMax, 1},
		{"tiers.few_passes_max", r.Tiers.FewPassesMax, 3},
	} {
		if c.got != c.want {
			t.Errorf("rebuild.%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	kinds := map[gate.Kind][]string{}
	for _, th := range cfg.Thresholds {
		kinds[th.Kind] = append(kinds[th.Kind], th.Metric)
	}
	want := map[gate.Kind]string{
		gate.Density:     "dup_blocks duplication_pct untested_exports globals init_funcs max_nesting cognitive_p90 changed_func_cognitive_max dup_blocks_cross_pkg",
		gate.Capacity:    "tokens_est largest_file_sloc exported_symbols internal_imports sloc",
		gate.Requirement: "has_tests",
	}
	for k, w := range want {
		if got := strings.Join(kinds[k], " "); got != w {
			t.Errorf("%s metrics = %q, want %q", k, got, w)
		}
	}
	var ratchet []string
	for _, th := range cfg.Thresholds {
		if th.RatchetFromZero {
			ratchet = append(ratchet, th.Metric)
		}
		if th.Metric == "cognitive_p90" && (th.Max == nil || *th.Max != 20) {
			t.Errorf("cognitive_p90 max = %v, want 20", th.Max)
		}
		if th.Metric == "changed_func_cognitive_max" && (th.Max == nil || *th.Max != 50 || th.MaxDelta != nil) {
			t.Errorf("changed_func_cognitive_max max = %v, max_delta = %v, want max 50 and no max_delta", th.Max, th.MaxDelta)
		}
	}
	// over_max_delta is fitted for three capacity rules (SPEC.md 8.2); the
	// other two keep the default of 0 and no rule of another kind has one.
	overMax := map[string]float64{}
	for _, th := range cfg.Thresholds {
		if th.OverMaxDelta != nil {
			overMax[th.Metric] = *th.OverMaxDelta
		}
	}
	if w := map[string]float64{"tokens_est": 1000, "largest_file_sloc": 5, "sloc": 100}; !maps.Equal(overMax, w) {
		t.Errorf("over_max_delta = %v, want %v", overMax, w)
	}
	if got, w := strings.Join(ratchet, " "), "dup_blocks untested_exports globals init_funcs"; got != w {
		t.Errorf("ratchet_from_zero metrics = %q, want %q", got, w)
	}
	last := cfg.Thresholds[len(cfg.Thresholds)-1]
	if last.When == nil || *last.When != (gate.Condition{Metric: "sloc", Value: 100}) {
		t.Errorf("has_tests when = %+v, want sloc > 100", last.When)
	}
	// The default sets no severity, so every rule it ships fails the gate.
	for _, lang := range append([]string{""}, cfg.Languages()...) {
		for _, th := range cfg.ForLanguage(lang).Thresholds {
			if th.Severity != "" {
				t.Errorf("%s %s: severity = %q, want none (fail)", lang, th.Metric, th.Severity)
			}
		}
	}
}

// TestParseSeverity checks that a rule's severity is read as written, at
// the top level and in a language override, and that an absent one is
// empty, which the gate treats as fail.
func TestParseSeverity(t *testing.T) {
	t.Parallel()

	data := replace(t, "  - metric: dup_blocks\n    kind: density\n", "  - metric: dup_blocks\n    kind: density\n    severity: warn\n")
	data = strings.Replace(data, "  - metric: globals\n    kind: density\n", "  - metric: globals\n    kind: density\n    severity: fail\n", 1)
	data = strings.Replace(data, "languages:\n  typescript:\n    thresholds:\n",
		"languages:\n  typescript:\n    thresholds:\n      - metric: init_funcs\n        kind: density\n        max_delta: 0\n        severity: warn\n", 1)
	cfg, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := map[string]gate.Severity{}
	for _, th := range cfg.Thresholds {
		got[th.Metric] = th.Severity
	}
	if got["dup_blocks"] != gate.SeverityWarn || got["globals"] != gate.SeverityFail || got["init_funcs"] != "" {
		t.Errorf("severities = %v, want dup_blocks warn, globals fail, init_funcs unset", got)
	}
	for _, th := range cfg.ForLanguage("typescript").Thresholds {
		if th.Metric == "init_funcs" && !th.Warns() {
			t.Errorf("typescript init_funcs severity = %q, want warn", th.Severity)
		}
	}
}

// TestDefaultVersionPrefix checks the embedded default ships calibrated
// rebuild parameters: SPEC.md 11.2 names a rebuild fit rebuild-<date>-<agent>,
// which score.Calibrated reads. The thresholds it carries are thresholds-<date>'s.
func TestDefaultVersionPrefix(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(Default())
	if err != nil {
		t.Fatalf("Parse(Default()) error = %v", err)
	}
	if !strings.HasPrefix(cfg.Version, "rebuild-") {
		t.Errorf("Version = %q, want a rebuild-<date>-<agent> version", cfg.Version)
	}
}

// TestUncalibratedStillParses checks the pre-calibration defaults kept for
// comparison under configs/ still load, with the default's rules in the same
// order and the placeholder rebuild parameters the rebuild fit replaced.
func TestUncalibratedStillParses(t *testing.T) {
	t.Parallel()

	cfg, err := Load(filepath.Join("..", "..", "configs", "uncalibrated.yaml"))
	if err != nil {
		t.Fatalf("Load(configs/uncalibrated.yaml) error = %v", err)
	}
	if cfg.Version != "default-uncalibrated-1" {
		t.Errorf("Version = %q, want default-uncalibrated-1", cfg.Version)
	}
	def, err := Parse(Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Thresholds) != len(def.Thresholds) {
		t.Fatalf("uncalibrated file has %d rules, want the default's %d", len(cfg.Thresholds), len(def.Thresholds))
	}
	// The rebuild parameters are the pre-calibration placeholders; only the
	// ones the rebuild fit does not touch match the default.
	placeholders := def.Rebuild
	placeholders.ContextBudget = 25000
	placeholders.TokensPerExport, placeholders.TokensPerUntestedExport = 40, 800
	placeholders.TokensPerHiddenState, placeholders.SuperlinearExponent = 400, 1.3
	if cfg.Rebuild != placeholders {
		t.Errorf("uncalibrated rebuild = %+v, want the placeholders %+v", cfg.Rebuild, placeholders)
	}
	for i, r := range cfg.Thresholds {
		if d := def.Thresholds[i]; r.Metric != d.Metric || r.Kind != d.Kind {
			t.Errorf("thresholds[%d] = %s %s, default has %s %s", i, r.Metric, r.Kind, d.Metric, d.Kind)
		}
	}
}

// replace returns the default config with old replaced by repl, failing the
// test when old is absent so edits to default.yaml cannot silently void a case.
func replace(t *testing.T, old, repl string) string {
	t.Helper()
	d := string(Default())
	if !strings.Contains(d, old) {
		t.Fatalf("default.yaml does not contain %q", old)
	}
	return strings.Replace(d, old, repl, 1)
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	const capacityRule = "  - metric: sloc\n    kind: capacity\n    max: 1000\n    warn_at: 0.75\n"
	tests := []struct {
		name    string
		old     string
		repl    string
		wantErr string
	}{
		{name: "exponent below 1", old: "superlinear_exponent: 1\n", repl: "superlinear_exponent: 0.9\n", wantErr: "superlinear_exponent must be >= 1"},
		{name: "zero budget", old: "context_budget: 37500", repl: "context_budget: 0", wantErr: "context_budget"},
		{name: "negative token cost", old: "tokens_per_export: 0", repl: "tokens_per_export: -1", wantErr: "tokens_per_export"},
		{name: "zero cocomo", old: "cocomo_a: 2.4", repl: "cocomo_a: 0", wantErr: "cocomo_a"},
		{name: "tiers unordered", old: "few_passes_max: 3.0", repl: "few_passes_max: 0.5", wantErr: "tiers.few_passes_max"},
		{name: "missing rebuild field", old: "  days_per_month: 19\n", repl: "", wantErr: "rebuild.days_per_month is required"},
		{name: "missing chars_per_token", old: "chars_per_token: 3.2\n", repl: "", wantErr: "chars_per_token is required"},
		{name: "zero chars_per_token", old: "chars_per_token: 3.2", repl: "chars_per_token: 0", wantErr: "chars_per_token must be > 0"},
		{name: "zero min_tokens", old: "min_tokens: 40", repl: "min_tokens: 0", wantErr: "duplication.min_tokens must be > 0"},
		{name: "ignore_literal_only not a bool", old: "ignore_literal_only: true", repl: "ignore_literal_only: maybe", wantErr: "`maybe` into bool"},
		{name: "unknown duplication key", old: "  min_tokens: 40\n", repl: "  min_tokens: 40\n  min_tokenz: 40\n", wantErr: "min_tokenz"},
		{name: "empty version", old: "config_version: rebuild-2026-09-28-claude-code-opus", repl: "config_version: \"\"", wantErr: "config_version is required"},
		{name: "unknown key", old: "chars_per_token: 3.2", repl: "chars_per_token: 3.2\nchars_per_tokenz: 3.2", wantErr: "chars_per_tokenz"},
		{name: "threshold with no limit", old: "    kind: density\n    max_delta: 6\n    max: 40\n", repl: "    kind: density\n", wantErr: `"duplication_pct": density rule needs max_delta or max`},
		{name: "capacity with max_delta", old: capacityRule, repl: capacityRule + "    max_delta: 0\n", wantErr: `"sloc": capacity rule must not set max_delta`},
		{name: "ratchet_from_zero on capacity", old: capacityRule, repl: capacityRule + "    ratchet_from_zero: true\n", wantErr: `"sloc": ratchet_from_zero applies only to density rules`},
		{name: "negative over_max_delta", old: "    over_max_delta: 100\n", repl: "    over_max_delta: -1\n", wantErr: `"sloc": over_max_delta must be a finite number >= 0, got -1`},
		{name: "over_max_delta on density", old: "    kind: density\n    max_delta: 6\n", repl: "    kind: density\n    max_delta: 6\n    over_max_delta: 0\n", wantErr: `"duplication_pct": over_max_delta applies only to capacity rules`},
		{name: "over_max_delta on requirement", old: "    require: true\n", repl: "    require: true\n    over_max_delta: 10\n", wantErr: `"has_tests": over_max_delta applies only to capacity rules`},
		{name: "over_max_delta on a disabled rule", old: "languages:\n  typescript:\n    thresholds:\n", repl: "languages:\n  typescript:\n    thresholds:\n      - metric: sloc\n        disabled: true\n        over_max_delta: 10\n", wantErr: "a disabled rule sets only metric and disabled"},
		{name: "unknown severity", old: capacityRule, repl: capacityRule + "    severity: error\n", wantErr: `"sloc": unknown severity "error"; want fail or warn`},
		{name: "severity on a disabled rule", old: "languages:\n  typescript:\n    thresholds:\n", repl: "languages:\n  typescript:\n    thresholds:\n      - metric: sloc\n        disabled: true\n        severity: warn\n", wantErr: "a disabled rule sets only metric and disabled"},
		{name: "ratchet_from_zero not a bool", old: "    ratchet_from_zero: true\n", repl: "    ratchet_from_zero: sometimes\n", wantErr: "`sometimes` into bool"},
		{name: "warn_at out of range", old: capacityRule, repl: strings.Replace(capacityRule, "0.75", "1.5", 1), wantErr: "warn_at must be in (0, 1)"},
		{name: "unknown metric", old: "metric: dup_blocks", repl: "metric: dupe_blocks", wantErr: `"dupe_blocks": unknown metric`},
		{name: "missing kind", old: "metric: dup_blocks\n    kind: density\n", repl: "metric: dup_blocks\n", wantErr: "kind is required"},
		{name: "requirement without require", old: "    require: true\n", repl: "", wantErr: "requirement rule needs require"},
		{name: "malformed when", old: "when: sloc > 100", repl: "when: sloc >= 100", wantErr: `"has_tests": when:`},
		{name: "when unknown metric", old: "when: sloc > 100", repl: "when: slock > 100", wantErr: `when: unknown metric "slock"`},
		{name: "rebuild output as metric", old: "metric: dup_blocks", repl: "metric: agent_passes", wantErr: `"agent_passes" is a rebuild output`},
		{name: "human_days as metric", old: "metric: sloc\n", repl: "metric: human_days\n", wantErr: `"human_days" is a rebuild output`},
		{name: "rebuild_tokens as metric", old: "metric: tokens_est\n", repl: "metric: rebuild_tokens\n", wantErr: `"rebuild_tokens" is a rebuild output`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(replace(t, tt.old, tt.repl)))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// defaultDupSection is the duplication section of default.yaml, comments
// included, so tests can drop or rewrite it.
const defaultDupSection = `duplication:
  # Minimum normalized token run that counts as a duplicate block.
  min_tokens: 40
  # Drop a duplicate block made only of literals and the punctuation of a
  # literal table, so repeated runs of data tables are not duplication.
  ignore_literal_only: true
  # Under ignore_literal_only, count a unary + or - directly before a
  # numeric literal as part of the literal, so tables of negative numbers
  # are data too. Matching is unchanged.
  fold_signs: true
  # Under ignore_literal_only, also cut each block at every run of at least
  # min_tokens literal-only tokens and keep the parts of at least min_tokens,
  # so tables that declaration headers join into one block are data too.
  split_literal_runs: false
`

func TestParseDuplication(t *testing.T) {
	t.Parallel()

	defaults := Duplication{MinTokens: 40, IgnoreLiteralOnly: true, FoldSigns: true}
	tests := []struct {
		name         string
		section      string
		want         Duplication
		wantWarnings int
		wantErr      []string
	}{
		{name: "absent section", section: "", want: defaults},
		{name: "empty section", section: "duplication: {}\n", want: defaults},
		{name: "only min_tokens", section: "duplication:\n  min_tokens: 25\n",
			want: Duplication{MinTokens: 25, IgnoreLiteralOnly: true, FoldSigns: true}},
		{name: "only fold_signs", section: "duplication:\n  fold_signs: false\n",
			want: Duplication{MinTokens: 40, IgnoreLiteralOnly: true, FoldSigns: false}},
		{name: "split_literal_runs on", section: "duplication:\n  split_literal_runs: true\n",
			want: Duplication{MinTokens: 40, IgnoreLiteralOnly: true, FoldSigns: true, SplitLiteralRuns: true}},
		{name: "split_literal_runs off", section: "duplication:\n  split_literal_runs: false\n", want: defaults},
		{name: "full section with split_literal_runs", section: "duplication:\n  min_tokens: 30\n  ignore_literal_only: true\n" +
			"  fold_signs: false\n  split_literal_runs: true\n",
			want: Duplication{MinTokens: 30, IgnoreLiteralOnly: true, SplitLiteralRuns: true}},
		{name: "old keys", section: "dup_min_tokens: 30\ndup_ignore_literal_only: false\ndup_fold_signs: false\n",
			want: Duplication{MinTokens: 30}, wantWarnings: 1},
		{name: "one old key", section: "dup_min_tokens: 30\n",
			want: Duplication{MinTokens: 30, IgnoreLiteralOnly: true, FoldSigns: true}, wantWarnings: 1},
		{name: "both forms", section: "dup_min_tokens: 30\ndup_fold_signs: false\nduplication:\n  min_tokens: 25\n",
			wantErr: []string{"dup_min_tokens and duplication.min_tokens", "dup_fold_signs and duplication.fold_signs"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := Parse([]byte(replace(t, defaultDupSection, tt.section)))
			if tt.wantErr != nil {
				for _, w := range tt.wantErr {
					if err == nil || !strings.Contains(err.Error(), w) {
						t.Errorf("Parse() error = %v, want error containing %q", err, w)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if cfg.Duplication != tt.want {
				t.Errorf("Duplication = %+v, want %+v", cfg.Duplication, tt.want)
			}
			if len(cfg.Warnings) != tt.wantWarnings {
				t.Fatalf("Warnings = %q, want %d", cfg.Warnings, tt.wantWarnings)
			}
			for _, w := range cfg.Warnings {
				if !strings.Contains(w, "dup_min_tokens") || !strings.Contains(w, "deprecated") {
					t.Errorf("warning %q does not name dup_min_tokens as deprecated", w)
				}
			}
		})
	}
}

func TestParseCapacityWarnAtDefault(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(replace(t, "    max: 1000\n    warn_at: 0.75\n", "    max: 1000\n")))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for _, th := range cfg.Thresholds {
		if th.Metric == "sloc" && th.WarnAt != 0.75 {
			t.Errorf("sloc warn_at = %v, want default 0.75", th.WarnAt)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	t.Parallel()

	if _, err := Parse(nil); err == nil {
		t.Fatal("Parse(nil) error = nil, want error")
	}
}

// TestDefaultGatesMatchExplanations checks that metrics.Explain's Gated flag
// names exactly the metrics the embedded default thresholds, so the reported
// but ungated coupling metrics stay out of the default.
func TestDefaultGatesMatchExplanations(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(Default())
	if err != nil {
		t.Fatalf("Parse(Default()) error = %v", err)
	}
	gated, capacity := map[string]bool{}, map[string]bool{}
	for _, th := range cfg.Thresholds {
		gated[th.Metric] = true
		capacity[th.Metric] = th.Kind == gate.Capacity
	}
	for _, name := range metrics.MetricNames() {
		e, ok := metrics.Explain(name)
		if !ok {
			t.Errorf("Explain(%q) has no entry", name)
			continue
		}
		if e.Gated != gated[name] {
			t.Errorf("Explain(%q).Gated = %v, default thresholds gate it: %v", name, e.Gated, gated[name])
		}
		// A capacity metric's explanation says what happens over its
		// ceiling, over_max_delta included (SPEC.md 8.1); no other does.
		if got := strings.Contains(e.Evidence, "over_max_delta"); got != capacity[name] {
			t.Errorf("Explain(%q) mentions over_max_delta: %v, default capacity rule: %v", name, got, capacity[name])
		}
	}
	for _, name := range []string{"instability", "abstractness", "main_sequence_distance"} {
		if gated[name] || strings.Contains(string(Default()), name) {
			t.Errorf("default configuration references %s, which is reported only", name)
		}
	}
}

// TestParseAcceptsCouplingMetrics checks that the coupling metrics are known
// metric names, so a user config may still gate them, and that the removed
// concrete_param_ratio is not.
func TestParseAcceptsCouplingMetrics(t *testing.T) {
	t.Parallel()

	rule := func(name string) string {
		return "  - metric: " + name + "\n    kind: capacity\n    max: 0.9\n"
	}
	for _, name := range []string{"instability", "abstractness", "main_sequence_distance"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// defaultTop ends with the thresholds list.
			if _, err := Parse(append(defaultTop(), rule(name)...)); err != nil {
				t.Errorf("Parse() with a %s threshold error = %v, want nil", name, err)
			}
		})
	}
	if _, err := Parse(append(defaultTop(), rule("concrete_param_ratio")...)); err == nil {
		t.Error("Parse() accepted a threshold on the removed concrete_param_ratio")
	}
}

func TestResolveOrder(t *testing.T) {
	t.Parallel()

	custom := replace(t, "config_version: rebuild-2026-09-28-claude-code-opus", "config_version: custom")
	flagged := replace(t, "config_version: rebuild-2026-09-28-claude-code-opus", "config_version: flagged")

	tests := []struct {
		name        string
		localFile   bool
		flag        bool
		wantSource  string
		wantVersion string
	}{
		{name: "embedded", wantSource: SourceEmbedded, wantVersion: "rebuild-2026-09-28-claude-code-opus"},
		{name: "working dir", localFile: true, wantSource: SourceWorkDir, wantVersion: "custom"},
		{name: "flag over nothing", flag: true, wantSource: SourceFlag, wantVersion: "flagged"},
		{name: "flag over working dir", localFile: true, flag: true, wantSource: SourceFlag, wantVersion: "flagged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.localFile {
				if err := os.WriteFile(filepath.Join(dir, FileName), []byte(custom), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var flagPath string
			if tt.flag {
				flagPath = filepath.Join(t.TempDir(), "other.yaml")
				if err := os.WriteFile(flagPath, []byte(flagged), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, source, err := resolveIn(flagPath, dir)
			if err != nil {
				t.Fatalf("resolveIn() error = %v", err)
			}
			if source != tt.wantSource {
				t.Errorf("source = %q, want %q", source, tt.wantSource)
			}
			if cfg.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", cfg.Version, tt.wantVersion)
			}
		})
	}
}

func TestResolveErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing flag file", func(t *testing.T) {
		t.Parallel()

		if _, _, err := resolveIn(filepath.Join(t.TempDir(), "absent.yaml"), t.TempDir()); err == nil {
			t.Fatal("resolveIn() error = nil, want error")
		}
	})
	t.Run("invalid working dir file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("config_version: x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, source, err := resolveIn("", dir)
		if err == nil || !strings.Contains(err.Error(), FileName) {
			t.Fatalf("resolveIn() error = %v, want error naming %s", err, FileName)
		}
		if source != SourceWorkDir {
			t.Errorf("source = %q, want %q", source, SourceWorkDir)
		}
	})
}

// TestResolveUsesWorkingDir checks the exported entry point reads ./astimate.yaml.
func TestResolveUsesWorkingDir(t *testing.T) {
	dir := t.TempDir()
	custom := replace(t, "config_version: rebuild-2026-09-28-claude-code-opus", "config_version: cwd")
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg, source, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if source != SourceWorkDir || cfg.Version != "cwd" {
		t.Errorf("Resolve() = (%q, %q), want (%q, %q)", cfg.Version, source, "cwd", SourceWorkDir)
	}
}
