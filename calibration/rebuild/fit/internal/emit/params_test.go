package emit_test

import (
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/emit"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/internal/score"
)

func TestFromFit(t *testing.T) {
	def := score.RebuildParams{ContextBudget: 25000, TokensPerExport: 40, TokensPerUntestedExport: 800,
		TokensPerHiddenState: 400, SuperlinearExponent: 1.3, CocomoA: 2.4, CocomoB: 1.05, DaysPerMonth: 19,
		Tiers: score.Tiers{OnePassMax: 1, FewPassesMax: 3}}
	fitted := func(mut func(*model.FormFit)) *model.FormFit {
		ff := &model.FormFit{Params: model.Params{Overhead: 1, Scale: 2, Budget: 30000, PerExport: 61.4,
			PerUntested: 523.7, PerHidden: 0.347, Exponent: 1.456}, Fixed: map[string]string{}}
		if mut != nil {
			mut(ff)
		}
		return ff
	}
	tests := []struct {
		name   string
		ff     *model.FormFit
		want   score.RebuildParams
		fitted bool
		notes  []string
	}{
		{"rounded", fitted(nil), with(def, 30000, 61, 520, 0.35, 1.46), true, nil},
		{"no fit", nil, def, false, []string{"tokens_per_export kept at 40: why", "superlinear_exponent kept at 1.3: why"}},
		{"clamped", fitted(func(f *model.FormFit) { f.Params.PerHidden, f.Params.Exponent = -50, 0.8 }),
			with(def, 30000, 61, 520, 0, 1), true,
			[]string{"tokens_per_hidden_state fitted -50, clamped to 0", "superlinear_exponent fitted 0.8, clamped to 1"}},
		{"held", fitted(func(f *model.FormFit) {
			f.Fixed[model.NameExponent] = "too few past the knee"
			f.Fixed[model.NamePerHidden] = "does not vary"
		}), with(def, 30000, 61, 520, 400, 1.3), true,
			[]string{"superlinear_exponent kept at 1.3: too few past the knee", "tokens_per_hidden_state kept at 400: does not vary"}},
		{"bad scale", fitted(func(f *model.FormFit) { f.Params.Scale = -1 }), def, false,
			[]string{"the fitted scale is not positive"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := emit.FromFit(def, tt.ff, "why")
			if e.Params != tt.want || e.Fitted != tt.fitted {
				t.Errorf("FromFit = %+v fitted %v, want %+v fitted %v", e.Params, e.Fitted, tt.want, tt.fitted)
			}
			var all []string
			for _, n := range e.Notes {
				all = append(all, n.Param+" "+n.Text)
			}
			joined := strings.Join(all, "\n")
			for _, n := range tt.notes {
				if !strings.Contains(joined, n) {
					t.Errorf("notes %q lack %q", joined, n)
				}
			}
			if err := e.Params.Validate(); err != nil {
				t.Errorf("emitted parameters do not validate: %v", err)
			}
		})
	}
	vals := emit.Values(&def)
	if len(vals) != 5 || vals[0].Param != model.NameBudget || vals[0].Text != "25000" || vals[4].Text != "1.3" {
		t.Errorf("Values = %+v", vals)
	}
}

// with returns p with the fitted parameters replaced.
func with(p score.RebuildParams, budget, export, untested, hidden, exponent float64) score.RebuildParams {
	p.ContextBudget, p.TokensPerExport, p.TokensPerUntestedExport = budget, export, untested
	p.TokensPerHiddenState, p.SuperlinearExponent = hidden, exponent
	return p
}
