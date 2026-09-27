package score

import (
	"errors"
	"fmt"
)

// RebuildParams holds the rebuild-estimate parameters from SPEC.md sections
// 7.2 to 7.4. They are uncalibrated until section 11.2 has run.
type RebuildParams struct {
	// ContextBudget is B, the tokens one agent pass can hold.
	ContextBudget float64
	// TokensPerExport is the cost of keeping one exported symbol's contract.
	TokensPerExport float64
	// TokensPerUntestedExport is the cost of reverse-engineering one
	// untested export.
	TokensPerUntestedExport float64
	// TokensPerHiddenState is the cost of one global or init function.
	TokensPerHiddenState float64
	// SuperlinearExponent applies to the budget ratio once it exceeds 1.
	SuperlinearExponent float64
	// CocomoA is the COCOMO basic coefficient a.
	CocomoA float64
	// CocomoB is the COCOMO basic exponent b.
	CocomoB float64
	// DaysPerMonth converts person-months into working days.
	DaysPerMonth float64
	// Tiers bounds the agent_passes tiers.
	Tiers Tiers
}

// Tiers holds the inclusive upper bounds of the agent_passes tiers in SPEC.md
// 7.4.
type Tiers struct {
	// OnePassMax is the largest agent_passes still ONE_PASS.
	OnePassMax float64
	// FewPassesMax is the largest agent_passes still FEW_PASSES.
	FewPassesMax float64
}

// namedValue pairs a parameter's config name with its value for range checks.
type namedValue struct {
	name string
	v    float64
}

// Validate reports every out-of-range parameter, each error naming the
// parameter, joined with errors.Join. It returns nil when all are valid.
func (p RebuildParams) Validate() error {
	var errs []error
	if p.ContextBudget <= 0 {
		errs = append(errs, fmt.Errorf("context_budget must be > 0, got %v", p.ContextBudget))
	}
	for _, c := range []namedValue{
		{"tokens_per_export", p.TokensPerExport},
		{"tokens_per_untested_export", p.TokensPerUntestedExport},
		{"tokens_per_hidden_state", p.TokensPerHiddenState},
	} {
		if c.v < 0 {
			errs = append(errs, fmt.Errorf("%s must be >= 0, got %v", c.name, c.v))
		}
	}
	if p.SuperlinearExponent < 1 {
		errs = append(errs, fmt.Errorf("superlinear_exponent must be >= 1, got %v", p.SuperlinearExponent))
	}
	for _, c := range []namedValue{
		{"cocomo_a", p.CocomoA},
		{"cocomo_b", p.CocomoB},
		{"days_per_month", p.DaysPerMonth},
	} {
		if c.v <= 0 {
			errs = append(errs, fmt.Errorf("%s must be > 0, got %v", c.name, c.v))
		}
	}
	if p.Tiers.OnePassMax <= 0 {
		errs = append(errs, fmt.Errorf("tiers.one_pass_max must be > 0, got %v", p.Tiers.OnePassMax))
	}
	if p.Tiers.FewPassesMax <= p.Tiers.OnePassMax {
		errs = append(errs, fmt.Errorf("tiers.few_passes_max must be > tiers.one_pass_max (%v), got %v",
			p.Tiers.OnePassMax, p.Tiers.FewPassesMax))
	}
	return errors.Join(errs...)
}
