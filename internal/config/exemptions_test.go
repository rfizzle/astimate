package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// withExemptions returns the default config with section appended.
func withExemptions(section string) []byte {
	return []byte(string(Default()) + "\n" + section)
}

func TestParseExemptions(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(withExemptions(`exemptions:
  - package: internal/registry
    metric: globals
    reason: the registry is process-wide by design
    expires: 2026-12-31
  - package: <module>
    metric: dup_blocks_cross_pkg
    reason: two generated clients share their transport
  - package: .
    metric: tokens_est
    reason: "quoted: fine"
    expires: "2027-01-31"
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []gate.Exemption{
		{Package: "internal/registry", Metric: "globals", Reason: "the registry is process-wide by design", Expires: "2026-12-31"},
		{Package: metrics.ModuleRowID, Metric: "dup_blocks_cross_pkg", Reason: "two generated clients share their transport"},
		{Package: ".", Metric: "tokens_est", Reason: "quoted: fine", Expires: "2027-01-31"},
	}
	if !slices.Equal(cfg.Exemptions, want) {
		t.Errorf("Exemptions = %+v, want %+v", cfg.Exemptions, want)
	}
	// A languages override carries no exemptions: every language gets the
	// top-level list, overridden or not.
	for _, lang := range []string{"go", "typescript"} {
		if got := cfg.ForLanguage(lang).Exemptions; !slices.Equal(got, want) {
			t.Errorf("ForLanguage(%q).Exemptions = %+v, want the top-level list", lang, got)
		}
	}
}

func TestParseDefaultHasNoExemptions(t *testing.T) {
	t.Parallel()

	cfg, err := Parse(Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Exemptions) != 0 {
		t.Errorf("default Exemptions = %+v, want none", cfg.Exemptions)
	}
}

// TestDefaultExemptionExample uncomments the example `config init` writes
// and parses it, so the example stays a valid configuration.
func TestDefaultExemptionExample(t *testing.T) {
	t.Parallel()

	d := string(Default())
	_, example, ok := strings.Cut(d, "#   exemptions:\n")
	if !ok {
		t.Fatal("default.yaml has no commented exemptions example")
	}
	var b strings.Builder
	b.WriteString("exemptions:\n")
	for line := range strings.SplitSeq(example, "\n") {
		if !strings.HasPrefix(line, "#   ") {
			break
		}
		b.WriteString(strings.TrimPrefix(line, "#   ") + "\n")
	}
	cfg, err := Parse(withExemptions(b.String()))
	if err != nil {
		t.Fatalf("the uncommented example does not parse: %v\n%s", err, b.String())
	}
	if len(cfg.Exemptions) != 1 || cfg.Exemptions[0].Reason == "" || cfg.Exemptions[0].Expires == "" {
		t.Errorf("example Exemptions = %+v, want one with a reason and an expiry", cfg.Exemptions)
	}
}

func TestParseExemptionErrors(t *testing.T) {
	t.Parallel()

	const ok = "  - package: internal/x\n    metric: globals\n    reason: accepted\n"
	tests := []struct {
		name    string
		section string
		wantErr []string
	}{
		{name: "missing reason names its index",
			section: "exemptions:\n" + ok + ok + "  - package: internal/y\n    metric: globals\n",
			wantErr: []string{"exemptions[2]: reason is required"}},
		{name: "blank reason", section: "exemptions:\n  - package: internal/x\n    metric: globals\n    reason: \"  \"\n",
			wantErr: []string{"exemptions[0]: reason is required"}},
		{name: "missing package", section: "exemptions:\n  - metric: globals\n    reason: r\n",
			wantErr: []string{"exemptions[0]: package is required"}},
		{name: "absolute package", section: "exemptions:\n  - package: /src/x\n    metric: globals\n    reason: r\n",
			wantErr: []string{`exemptions[0]: package "/src/x"`}},
		{name: "unknown metric", section: "exemptions:\n  - package: x\n    metric: globalz\n    reason: r\n",
			wantErr: []string{`exemptions[0]: metric "globalz": no threshold gates it`}},
		{name: "known but ungated metric", section: "exemptions:\n  - package: x\n    metric: fan_in\n    reason: r\n",
			wantErr: []string{`exemptions[0]: metric "fan_in": no threshold gates it`}},
		{name: "rebuild output", section: "exemptions:\n  - package: x\n    metric: agent_passes\n    reason: r\n",
			wantErr: []string{`exemptions[0]: metric "agent_passes": no threshold gates it`}},
		{name: "module-wide metric on a package", section: "exemptions:\n  - package: x\n    metric: dup_blocks_cross_pkg\n    reason: r\n",
			wantErr: []string{"exemptions[0]: metric \"dup_blocks_cross_pkg\" is gated on the module row only"}},
		{name: "bad date", section: "exemptions:\n  - package: x\n    metric: globals\n    reason: r\n    expires: next year\n",
			wantErr: []string{`exemptions[0]: expires "next year"`}},
		{name: "unknown key", section: "exemptions:\n  - package: x\n    metric: globals\n    reason: r\n    until: 2026-12-31\n",
			wantErr: []string{"until"}},
		{name: "several at once", section: "exemptions:\n  - package: x\n  - metric: globals\n    reason: r\n",
			wantErr: []string{"exemptions[0]: metric is required", "exemptions[0]: reason is required", "exemptions[1]: package is required"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(withExemptions(tt.section))
			if err == nil {
				t.Fatalf("Parse() = nil error, want %q", tt.wantErr)
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("Parse() error = %v, want it to contain %q", err, w)
				}
			}
		})
	}
}

// TestParseExemptionLanguageRules checks that a metric gated only by a
// languages override may be exempted, and that an override cannot carry
// exemptions of its own.
func TestParseExemptionLanguageRules(t *testing.T) {
	t.Parallel()

	langRule := replace(t, "languages:\n  typescript:\n    thresholds:\n",
		"languages:\n  typescript:\n    thresholds:\n      - metric: fan_in\n        kind: capacity\n        max: 30\n")
	if _, err := Parse([]byte(langRule + "\nexemptions:\n  - package: x\n    metric: fan_in\n    reason: hub package\n")); err != nil {
		t.Errorf("exempting a metric a languages override gates: %v", err)
	}
	inOverride := replace(t, "languages:\n  typescript:\n",
		"languages:\n  typescript:\n    exemptions:\n      - package: x\n        metric: globals\n        reason: r\n")
	if _, err := Parse([]byte(inOverride)); err == nil || !strings.Contains(err.Error(), "exemptions") {
		t.Errorf("exemptions under a languages override: error = %v, want an unknown-field error", err)
	}
}
