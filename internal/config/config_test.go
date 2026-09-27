package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
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
	if cfg.Version != "default-uncalibrated-1" {
		t.Errorf("Version = %q, want default-uncalibrated-1", cfg.Version)
	}
	if cfg.CharsPerToken != 3.2 || cfg.DupMinTokens != 40 || !cfg.DupIgnoreLiteralOnly {
		t.Errorf("CharsPerToken, DupMinTokens, DupIgnoreLiteralOnly = %v, %d, %v; want 3.2, 40, true",
			cfg.CharsPerToken, cfg.DupMinTokens, cfg.DupIgnoreLiteralOnly)
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
		gate.Density:     "dup_blocks duplication_pct untested_exports globals init_funcs max_nesting cognitive_p90",
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
		if th.Metric == "cognitive_p90" && (th.Max == nil || *th.Max != 25) {
			t.Errorf("cognitive_p90 max = %v, want 25", th.Max)
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

	const capacityRule = "  - metric: sloc\n    kind: capacity\n    max: 6000\n    warn_at: 0.75\n"
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
		{name: "zero dup_min_tokens", old: "dup_min_tokens: 40", repl: "dup_min_tokens: 0", wantErr: "dup_min_tokens"},
		{name: "missing dup_ignore_literal_only", old: "dup_ignore_literal_only: true\n", repl: "", wantErr: "dup_ignore_literal_only is required"},
		{name: "dup_ignore_literal_only not a bool", old: "dup_ignore_literal_only: true", repl: "dup_ignore_literal_only: maybe", wantErr: "`maybe` into bool"},
		{name: "empty version", old: "config_version: default-uncalibrated-1", repl: "config_version: \"\"", wantErr: "config_version is required"},
		{name: "unknown key", old: "dup_min_tokens: 40", repl: "dup_min_tokens: 40\ndup_min_tokenz: 40", wantErr: "dup_min_tokenz"},
		{name: "threshold with no limit", old: "    kind: density\n    max_delta: 3\n", repl: "    kind: density\n", wantErr: `"cognitive_p90": density rule needs max_delta`},
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

func TestParseCapacityWarnAtDefault(t *testing.T) {
	t.Parallel()

	cfg, err := Parse([]byte(replace(t, "    max: 6000\n    warn_at: 0.75\n", "    max: 6000\n")))
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

func TestResolveOrder(t *testing.T) {
	t.Parallel()

	custom := replace(t, "config_version: default-uncalibrated-1", "config_version: custom")
	flagged := replace(t, "config_version: default-uncalibrated-1", "config_version: flagged")

	tests := []struct {
		name        string
		localFile   bool
		flag        bool
		wantSource  string
		wantVersion string
	}{
		{name: "embedded", wantSource: SourceEmbedded, wantVersion: "default-uncalibrated-1"},
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
	custom := replace(t, "config_version: default-uncalibrated-1", "config_version: cwd")
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
