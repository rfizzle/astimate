package score

import (
	"strings"
	"testing"
)

func validParams() RebuildParams {
	p := RebuildParams{
		ContextBudget:           25000,
		TokensPerExport:         40,
		TokensPerUntestedExport: 800,
		TokensPerHiddenState:    400,
		SuperlinearExponent:     1.3,
		CocomoA:                 2.4,
		CocomoB:                 1.05,
		DaysPerMonth:            19,
	}
	p.Tiers.OnePassMax = 1
	p.Tiers.FewPassesMax = 3
	return p
}

func TestRebuildParamsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*RebuildParams)
		wantErr string
	}{
		{name: "defaults valid", mutate: func(*RebuildParams) {}},
		{name: "zero token costs valid", mutate: func(p *RebuildParams) {
			p.TokensPerExport, p.TokensPerUntestedExport, p.TokensPerHiddenState = 0, 0, 0
		}},
		{name: "exponent exactly 1 valid", mutate: func(p *RebuildParams) { p.SuperlinearExponent = 1 }},
		{name: "zero budget", mutate: func(p *RebuildParams) { p.ContextBudget = 0 }, wantErr: "context_budget"},
		{name: "negative export cost", mutate: func(p *RebuildParams) { p.TokensPerExport = -1 }, wantErr: "tokens_per_export"},
		{name: "negative untested cost", mutate: func(p *RebuildParams) { p.TokensPerUntestedExport = -1 }, wantErr: "tokens_per_untested_export"},
		{name: "negative hidden cost", mutate: func(p *RebuildParams) { p.TokensPerHiddenState = -1 }, wantErr: "tokens_per_hidden_state"},
		{name: "exponent below 1", mutate: func(p *RebuildParams) { p.SuperlinearExponent = 0.9 }, wantErr: "superlinear_exponent"},
		{name: "zero cocomo a", mutate: func(p *RebuildParams) { p.CocomoA = 0 }, wantErr: "cocomo_a"},
		{name: "negative cocomo b", mutate: func(p *RebuildParams) { p.CocomoB = -1 }, wantErr: "cocomo_b"},
		{name: "zero days per month", mutate: func(p *RebuildParams) { p.DaysPerMonth = 0 }, wantErr: "days_per_month"},
		{name: "zero one pass max", mutate: func(p *RebuildParams) { p.Tiers.OnePassMax = 0 }, wantErr: "tiers.one_pass_max"},
		{name: "tiers out of order", mutate: func(p *RebuildParams) { p.Tiers.FewPassesMax = 1 }, wantErr: "tiers.few_passes_max"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := validParams()
			tt.mutate(&p)
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error naming %q", err, tt.wantErr)
			}
		})
	}
}
