package gate_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/metrics/role"
)

func TestThresholdJudges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		roles []string
		role  string
		want  bool
	}{
		{"no roles judges main", nil, role.Main, true},
		{"no roles judges an unknown role", nil, "", true},
		{"listed role", []string{role.Library, role.TestSupport}, role.TestSupport, true},
		{"unlisted role", []string{role.Library}, role.Main, false},
		{"empty role is a library", []string{role.Library}, "", true},
		{"empty role is not main", []string{role.Main}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := (gate.Threshold{Roles: tt.roles}).Judges(tt.role); got != tt.want {
				t.Errorf("Judges(%q) with roles %v = %v, want %v", tt.role, tt.roles, got, tt.want)
			}
		})
	}
}

func TestThresholdValidateRoles(t *testing.T) {
	t.Parallel()

	known := func(string) bool { return true }
	tests := []struct {
		name    string
		roles   []string
		wantErr string
	}{
		{"every role", role.Names(), ""},
		{"none", nil, ""},
		{"unknown", []string{"cmd"}, `roles: unknown role "cmd"; want one of main, library, test-support, generated-only`},
		{"twice", []string{role.Main, role.Library, role.Main}, `roles: "main" listed twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			th := gate.Threshold{Metric: "internal_imports", Kind: gate.Capacity, Max: ptr(10.0), WarnAt: 0.75, Roles: tt.roles}
			err := th.Validate(known)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestEvaluateRoles checks a rule whose roles leave out the row's role is
// not evaluated, under the default configuration's roles (SPEC.md 8.2):
// a composition root is not judged on internal_imports, exported_symbols
// or untested_exports, and needs tests only past 300 SLOC, while a
// library with the same metrics fails each.
func TestEvaluateRoles(t *testing.T) {
	t.Parallel()

	rules := defaultRules(t)
	// wired is a new package with twelve internal imports, seventy exports
	// of which five untested, and 250 SLOC without tests.
	wired := func(r string) metrics.RawMetrics {
		m := fresh()
		m.InternalImports, m.ExportedSymbols, m.UntestedExports = 12, 70, 5
		m.SLOC, m.HasTests, m.TestFiles, m.TestFuncs = 250, false, 0, 0
		m.Role = r
		return m
	}
	metricsOf := func(vs []gate.Violation) []string {
		out := make([]string, 0, len(vs))
		for _, v := range vs {
			out = append(out, v.Metric)
		}
		return out
	}
	tests := []struct {
		name string
		head metrics.RawMetrics
		want []string
	}{
		{"main", wired(role.Main), []string{}},
		{"library", wired(role.Library), []string{"exported_symbols", "has_tests", "internal_imports", "untested_exports"}},
		{"no role, as a library", wired(""), []string{"exported_symbols", "has_tests", "internal_imports", "untested_exports"}},
		{"test-support", wired(role.TestSupport), []string{"exported_symbols", "has_tests", "internal_imports"}},
		{"main past 300 SLOC", func() metrics.RawMetrics { m := wired(role.Main); m.SLOC = 301; return m }(), []string{"has_tests"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := metricsOf(gate.Evaluate(tt.head, nil, rules, nil).Violations)
			if !slices.Equal(got, tt.want) {
				t.Errorf("violations = %v, want %v", got, tt.want)
			}
		})
	}
}
