package config

import (
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
	if cfg.Version != "thresholds-2026-09-28" {
		t.Errorf("Version = %q, want thresholds-2026-09-28", cfg.Version)
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
		{"context_budget", r.ContextBudget, 25000},
		{"tokens_per_export", r.TokensPerExport, 40},
		{"tokens_per_untested_export", r.TokensPerUntestedExport, 800},
		{"tokens_per_hidden_state", r.TokensPerHiddenState, 400},
		{"superlinear_exponent", r.SuperlinearExponent, 1.3},
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
		gate.Density:     "dup_blocks duplication_pct untested_exports globals init_funcs max_nesting cognitive_p90 changed_func_cognitive_max",
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
	if got, w := strings.Join(ratchet, " "), "dup_blocks untested_exports globals init_funcs"; got != w {
		t.Errorf("ratchet_from_zero metrics = %q, want %q", got, w)
	}
	last := cfg.Thresholds[len(cfg.Thresholds)-1]
	if last.When == nil || *last.When != (gate.Condition{Metric: "sloc", Value: 100}) {
		t.Errorf("has_tests when = %+v, want sloc > 100", last.When)
	}
}

// TestDefaultVersionPrefix checks the embedded default ships calibrated
// thresholds: SPEC.md 11.1 names a corpus fit thresholds-<date>.
func TestDefaultVersionPrefix(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(Default())
	if err != nil {
		t.Fatalf("Parse(Default()) error = %v", err)
	}
	if !strings.HasPrefix(cfg.Version, "thresholds-") {
		t.Errorf("Version = %q, want a thresholds-<date> version", cfg.Version)
	}
}

// TestUncalibratedStillParses checks the pre-calibration defaults kept for
// comparison under configs/ still load, with the default's rules in the same
// order and its rebuild parameters.
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
	if len(cfg.Thresholds) != len(def.Thresholds) || cfg.Rebuild != def.Rebuild {
		t.Fatalf("uncalibrated file has %d rules and rebuild %+v, want %d rules and the default rebuild parameters",
			len(cfg.Thresholds), cfg.Rebuild, len(def.Thresholds))
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
		{name: "exponent below 1", old: "superlinear_exponent: 1.3", repl: "superlinear_exponent: 0.9", wantErr: "superlinear_exponent must be >= 1"},
		{name: "zero budget", old: "context_budget: 25000", repl: "context_budget: 0", wantErr: "context_budget"},
		{name: "negative token cost", old: "tokens_per_export: 40", repl: "tokens_per_export: -1", wantErr: "tokens_per_export"},
		{name: "zero cocomo", old: "cocomo_a: 2.4", repl: "cocomo_a: 0", wantErr: "cocomo_a"},
		{name: "tiers unordered", old: "few_passes_max: 3.0", repl: "few_passes_max: 0.5", wantErr: "tiers.few_passes_max"},
		{name: "missing rebuild field", old: "  days_per_month: 19\n", repl: "", wantErr: "rebuild.days_per_month is required"},
		{name: "missing chars_per_token", old: "chars_per_token: 3.2\n", repl: "", wantErr: "chars_per_token is required"},
		{name: "zero chars_per_token", old: "chars_per_token: 3.2", repl: "chars_per_token: 0", wantErr: "chars_per_token must be > 0"},
		{name: "zero min_tokens", old: "min_tokens: 40", repl: "min_tokens: 0", wantErr: "duplication.min_tokens must be > 0"},
		{name: "ignore_literal_only not a bool", old: "ignore_literal_only: true", repl: "ignore_literal_only: maybe", wantErr: "`maybe` into bool"},
		{name: "unknown duplication key", old: "  min_tokens: 40\n", repl: "  min_tokens: 40\n  min_tokenz: 40\n", wantErr: "min_tokenz"},
		{name: "empty version", old: "config_version: thresholds-2026-09-28", repl: "config_version: \"\"", wantErr: "config_version is required"},
		{name: "unknown key", old: "chars_per_token: 3.2", repl: "chars_per_token: 3.2\nchars_per_tokenz: 3.2", wantErr: "chars_per_tokenz"},
		{name: "threshold with no limit", old: "    kind: density\n    max_delta: 6\n    max: 40\n", repl: "    kind: density\n", wantErr: `"duplication_pct": density rule needs max_delta or max`},
		{name: "capacity with max_delta", old: capacityRule, repl: capacityRule + "    max_delta: 0\n", wantErr: `"sloc": capacity rule must not set max_delta`},
		{name: "ratchet_from_zero on capacity", old: capacityRule, repl: capacityRule + "    ratchet_from_zero: true\n", wantErr: `"sloc": ratchet_from_zero applies only to density rules`},
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
	gated := map[string]bool{}
	for _, th := range cfg.Thresholds {
		gated[th.Metric] = true
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
			if _, err := Parse(append(Default(), rule(name)...)); err != nil {
				t.Errorf("Parse() with a %s threshold error = %v, want nil", name, err)
			}
		})
	}
	if _, err := Parse(append(Default(), rule("concrete_param_ratio")...)); err == nil {
		t.Error("Parse() accepted a threshold on the removed concrete_param_ratio")
	}
}

func TestResolveOrder(t *testing.T) {
	t.Parallel()

	custom := replace(t, "config_version: thresholds-2026-09-28", "config_version: custom")
	flagged := replace(t, "config_version: thresholds-2026-09-28", "config_version: flagged")

	tests := []struct {
		name        string
		localFile   bool
		flag        bool
		wantSource  string
		wantVersion string
	}{
		{name: "embedded", wantSource: SourceEmbedded, wantVersion: "thresholds-2026-09-28"},
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
	custom := replace(t, "config_version: thresholds-2026-09-28", "config_version: cwd")
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
