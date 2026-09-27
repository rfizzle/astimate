package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// params are the SPEC.md 7.2 and 7.3 default rebuild parameters.
func params() score.RebuildParams {
	return score.RebuildParams{
		ContextBudget:           25000,
		TokensPerExport:         40,
		TokensPerUntestedExport: 800,
		TokensPerHiddenState:    400,
		SuperlinearExponent:     1.3,
		CocomoA:                 2.4,
		CocomoB:                 1.05,
		DaysPerMonth:            19,
		Tiers:                   score.Tiers{OnePassMax: 1, FewPassesMax: 3},
	}
}

// workedExample is the SPEC.md 7.2 worked example.
func workedExample() metrics.RawMetrics {
	return metrics.RawMetrics{
		SLOC: 1200, TokensEst: 10000, TokensEstWithTests: 16000, DuplicationPct: 20,
		ExportedSymbols: 30, UntestedExports: 15, Globals: 2, InitFuncs: 1,
	}
}

func TestBuildWorkedExample(t *testing.T) {
	t.Parallel()

	r := Build(&Input{Language: "go", Metrics: workedExample(), Params: params(), ConfigVersion: "default-uncalibrated-1"})
	got := r.Rebuild
	if got.AgentPasses != 1.2 || got.RebuildTokens != 28400 || got.HumanDays != 55.2 ||
		got.Tier != score.TierFewPasses || got.Calibrated {
		t.Errorf("rebuild = %+v, want agent_passes 1.2, rebuild_tokens 28400, human_days 55.2, FEW_PASSES, uncalibrated", got)
	}
	if len(got.Drivers) != 2 || got.Drivers[0].Term != score.TermUnspecified || got.Drivers[0].Tokens != 12000 {
		t.Errorf("drivers = %+v, want unspecified (12000) first of two", got.Drivers)
	}
}

func TestReportOptionalKeys(t *testing.T) {
	t.Parallel()

	passed := false
	tests := []struct {
		name     string
		mutate   func(*Report)
		wantKeys []string
		noKeys   []string
	}{
		{
			name:   "no gate",
			mutate: func(*Report) {},
			noKeys: []string{`"baseline"`, `"violations"`, `"warnings"`, `"passed"`},
		},
		{
			name: "gate ran",
			mutate: func(r *Report) {
				r.Baseline = &Baseline{Ref: "a1b2c3d"}
				r.Violations = []Finding{{Metric: "dup_blocks", Head: 4, Limit: "max_delta +0"}}
				r.Warnings = []Finding{{Metric: "tokens_est", Head: 24100, Limit: "max 30000"}}
				r.Passed = &passed
			},
			wantKeys: []string{`"baseline"`, `"violations"`, `"warnings"`, `"passed": false`},
		},
		{
			name: "gate ran clean",
			mutate: func(r *Report) {
				r.Violations = []Finding{}
				r.Warnings = []Finding{}
				ok := true
				r.Passed = &ok
			},
			wantKeys: []string{`"violations": []`, `"warnings": []`, `"passed": true`},
			noKeys:   []string{`"baseline"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := Build(&Input{Language: "go", Metrics: workedExample(), Params: params()})
			tt.mutate(&r)
			var buf bytes.Buffer
			if err := WriteJSON(&buf, &r); err != nil {
				t.Fatalf("WriteJSON: %v", err)
			}
			if !json.Valid(buf.Bytes()) {
				t.Fatalf("WriteJSON produced invalid JSON:\n%s", buf.String())
			}
			for _, k := range tt.wantKeys {
				if !strings.Contains(buf.String(), k) {
					t.Errorf("JSON lacks %s:\n%s", k, buf.String())
				}
			}
			for _, k := range tt.noKeys {
				if strings.Contains(buf.String(), k) {
					t.Errorf("JSON has %s, want it omitted:\n%s", k, buf.String())
				}
			}
		})
	}
}

func TestWriteTableCalibratedLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    string
	}{
		{version: "default-uncalibrated-1", want: "rebuild: 1.2 agent passes, 55.2 human days (estimate, uncalibrated)\n"},
		{version: "rebuild-2026-10", want: "rebuild: 1.2 agent passes, 55.2 human days (estimate, calibrated)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()

			r := Build(&Input{Metrics: workedExample(), Params: params(), ConfigVersion: tt.version})
			var buf bytes.Buffer
			if err := WriteTable(&buf, &r); err != nil {
				t.Fatalf("WriteTable: %v", err)
			}
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("table lacks %q:\n%s", tt.want, buf.String())
			}
			if strings.Contains(buf.String(), "coverage_pct") {
				t.Errorf("table shows uncomputed v1 field coverage_pct:\n%s", buf.String())
			}
		})
	}
}

// TestTextEmptyModulePath checks that no text renderer prints an empty
// parenthetical for a module without an import path, as for TypeScript, and
// that the table header keeps the module path when there is one.
func TestTextEmptyModulePath(t *testing.T) {
	t.Parallel()

	table := func(b *bytes.Buffer, r Report) error { return WriteTable(b, &r) }
	tests := []struct {
		name       string
		modulePath string
		write      func(*bytes.Buffer, Report) error
		want       string
	}{
		{name: "table empty", write: table, want: "package: hub\n"},
		{name: "table set", modulePath: "example.com/app", write: table, want: "package: hub (example.com/app)\n"},
		{name: "check text empty", write: func(b *bytes.Buffer, r Report) error {
			ApplyGate(&r, "a1b2c3d", nil, &gate.Result{Passed: true})
			return WriteCheckText(b, &Check{Packages: []CheckedPackage{{Report: r}}})
		}},
		{name: "rows table empty", write: func(b *bytes.Buffer, r Report) error {
			m := workedExample()
			return WriteRowsTable(b, []Row{NewRow(r.PackagePath, &m, params())})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := Build(&Input{Language: "typescript", PackagePath: "hub", ModulePath: tt.modulePath,
				Metrics: workedExample(), Params: params()})
			var buf bytes.Buffer
			if err := tt.write(&buf, r); err != nil {
				t.Fatalf("write: %v", err)
			}
			if strings.Contains(buf.String(), "()") {
				t.Errorf("output has empty parentheses:\n%s", buf.String())
			}
			if !strings.HasPrefix(buf.String(), tt.want) {
				t.Errorf("output does not start with %q:\n%s", tt.want, buf.String())
			}
		})
	}
}
