// Package model fits the SPEC.md 7.2 agent estimate to measured rebuilds.
//
// A rebuild's measured tokens are modelled as a fixed per-session overhead
// plus a scale times the estimate's own cost in tokens:
//
//	measured = overhead + scale * budget * passes(rebuild_tokens / budget)
//	rebuild_tokens = volume + spec + exports * per_export
//	               + untested * per_untested + hidden * per_hidden
//	passes(r) = r below the knee (r <= 1), r ^ exponent past it
//
// so below the knee measured tokens are linear in the 7.1 inputs, with the
// coefficients on volume and spec fixed at one rebuild token each as 7.2
// fixes them, and past it they grow as agent_passes does. The overhead and
// the scale are not 7.2 parameters: the overhead is what a session costs
// before it reads the package (the system prompt, the tool list, the
// prompt), and the scale is how many measured tokens one token of the
// estimate costs (context is re-read, tests are rerun, code is rewritten).
// The per-item costs and the exponent are 7.2's, in rebuild tokens, and map
// back to the configuration unchanged. The budget is held fixed: it is
// only identified through the knee, and Profile reports how the fit moves
// with it.
package model

import (
	"fmt"
	"math"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
	"github.com/rfizzle/astimate/internal/metrics"
)

// TooFewError is returned when there are not enough packages to fit the
// parameters with at least two residual degrees of freedom.
type TooFewError struct {
	// Packages and Params are what the fit had and had to fit.
	Packages, Params int
}

// Error says how many packages the fit needs.
func (e *TooFewError) Error() string {
	return fmt.Sprintf("too few packages: %d for %d parameters; the fit needs at least %d",
		e.Packages, e.Params, e.Params+2)
}

// Parameter names, as the configuration and the report spell them. The
// overhead and the scale are the fit's own and are not in the
// configuration.
const (
	NameOverhead    = "overhead"
	NameScale       = "scale"
	NameBudget      = "context_budget"
	NamePerExport   = "tokens_per_export"
	NamePerUntested = "tokens_per_untested_export"
	NamePerHidden   = "tokens_per_hidden_state"
	NameExponent    = "superlinear_exponent"
)

// kneeMin is the smallest budget ratio a package must reach to count as
// past the knee when deciding whether the exponent can be fitted: just
// past 1, ln r is near zero and says nothing about the exponent.
// minPastKnee is how many such packages the exponent needs.
const (
	kneeMin     = 1.1
	minPastKnee = 3
)

// Inputs are the SPEC.md 7.1 inputs of one package, as the 7.2 terms read
// them.
type Inputs struct {
	// Volume is tokens_est * (1 - duplication_pct / 100).
	Volume float64
	// Spec is tokens_est_with_tests - tokens_est, at least 0.
	Spec float64
	// Exports is exported_symbols.
	Exports float64
	// Untested is untested_exports, scaled by 1 - coverage_pct / 100 when
	// coverage was measured.
	Untested float64
	// Hidden is globals + init_funcs.
	Hidden float64
}

// InputsOf returns the 7.1 inputs of m, as score.Estimate computes them.
func InputsOf(m *metrics.RawMetrics) Inputs {
	untested := float64(m.UntestedExports)
	if m.CoveragePct != nil {
		untested *= 1 - min(max(*m.CoveragePct, 0), 100)/100
	}
	return Inputs{
		Volume:   float64(m.TokensEst) * (1 - m.DuplicationPct/100),
		Spec:     math.Max(float64(m.TokensEstWithTests-m.TokensEst), 0),
		Exports:  float64(m.ExportedSymbols),
		Untested: untested,
		Hidden:   float64(m.Globals + m.InitFuncs),
	}
}

// vector returns in's terms in the order of the form: volume, spec,
// exports, untested, hidden.
func (in Inputs) vector() [5]float64 {
	return [5]float64{in.Volume, in.Spec, in.Exports, in.Untested, in.Hidden}
}

// Params are the model's parameters: the fit's overhead and scale, and the
// 7.2 parameters.
type Params struct {
	// Overhead is the tokens a session spends whatever the package.
	Overhead float64
	// Scale is the measured tokens per rebuild token.
	Scale float64
	// Budget, PerExport, PerUntested, PerHidden and Exponent are 7.2's
	// context_budget, tokens_per_export, tokens_per_untested_export,
	// tokens_per_hidden_state and superlinear_exponent.
	Budget, PerExport, PerUntested, PerHidden, Exponent float64
}

// RebuildTokens returns 7.2's rebuild_tokens for in under p.
func (p *Params) RebuildTokens(in Inputs) float64 {
	return in.Volume + in.Spec + in.Exports*p.PerExport + in.Untested*p.PerUntested + in.Hidden*p.PerHidden
}

// Ratio returns r, rebuild_tokens over the budget.
func (p *Params) Ratio(in Inputs) float64 {
	return p.RebuildTokens(in) / p.Budget
}

// Passes returns 7.2's agent_passes for in under p.
func (p *Params) Passes(in Inputs) float64 {
	r := p.Ratio(in)
	if r > 1 {
		return math.Pow(r, p.Exponent)
	}
	return r
}

// Measured returns the measured tokens the model predicts for in.
func (p *Params) Measured(in Inputs) float64 {
	return p.Overhead + p.Scale*p.Budget*p.Passes(in)
}

// Obs is one package's measurement: its metrics and the median tokens of
// its passing runs.
type Obs struct {
	// Package is the import path.
	Package string
	// Metrics are the package's raw metrics.
	Metrics metrics.RawMetrics
	// Tokens is the measured tokens.
	Tokens float64
}

// FormFit is the least-squares fit of the 7.2 form.
type FormFit struct {
	// Params are the fitted parameters; those in Fixed hold their base
	// values.
	Params Params
	// SE holds the standard error of each fitted parameter, 0 for a fixed
	// one.
	SE Params
	// Fixed names each parameter held at its base value, with the reason.
	Fixed map[string]string
	// Fit is the underlying solution: R2, residuals, degrees of freedom.
	Fit regress.Fit
	// PastKnee counts the packages whose r exceeds 1 at the solution, and
	// FarPastKnee those whose r is at least 1.1.
	PastKnee, FarPastKnee int
}

// freeParam is one parameter the Levenberg-Marquardt solve moves: its
// name, how to read and write it, and its column of the Jacobian.
type freeParam struct {
	name string
	ptr  func(p *Params) *float64
	grad func(p *Params, in Inputs) float64
}

// params lists every parameter the form can fit, in solve order.
func params() []freeParam {
	slope := func(p *Params, in Inputs) float64 {
		if p.Ratio(in) > 1 {
			return p.Exponent * math.Pow(p.Ratio(in), p.Exponent-1)
		}
		return 1
	}
	out := []freeParam{
		{NameOverhead, func(p *Params) *float64 { return &p.Overhead }, func(*Params, Inputs) float64 { return 1 }},
		{NameScale, func(p *Params) *float64 { return &p.Scale }, func(p *Params, in Inputs) float64 { return p.Budget * p.Passes(in) }},
	}
	for k, name := range costNames() {
		out = append(out, freeParam{name, func(p *Params) *float64 { return p.costs()[k] },
			func(p *Params, in Inputs) float64 { return p.Scale * slope(p, in) * in.vector()[costTerm+k] }})
	}
	return append(out, freeParam{NameExponent, func(p *Params) *float64 { return &p.Exponent }, func(p *Params, in Inputs) float64 {
		r := p.Ratio(in)
		if r <= 1 {
			return 0
		}
		return p.Scale * p.Budget * math.Pow(r, p.Exponent) * math.Log(r)
	}})
}

// costTerm is the index in Inputs.vector of the first per-item term,
// exports; untested and hidden follow, in the order of costNames.
const costTerm = 2

// costNames names the per-item costs in the order of their terms.
func costNames() [3]string {
	return [3]string{NamePerExport, NamePerUntested, NamePerHidden}
}

// costs returns pointers to p's per-item costs, in the order of
// costNames.
func (p *Params) costs() [3]*float64 {
	return [3]*float64{&p.PerExport, &p.PerUntested, &p.PerHidden}
}

// FitForm fits the 7.2 form to obs by nonlinear least squares, with the
// budget held at base.Budget. A per-item cost whose input does not vary
// across obs keeps its base value, and so does the exponent unless at
// least three packages reach r >= 1.1 once the rest is fitted; Fixed
// records each with the reason.
func FitForm(obs []Obs, base Params) (FormFit, error) {
	ins := make([]Inputs, len(obs))
	y := make([]float64, len(obs))
	for i := range obs {
		ins[i] = InputsOf(&obs[i].Metrics)
		y[i] = obs[i].Tokens
	}
	fixed := map[string]string{NameExponent: "fitted only past the knee"}
	for k, name := range costNames() {
		if !distinct(len(ins), func(i int) float64 { return ins[i].vector()[costTerm+k] }) {
			fixed[name] = "its input does not vary across the packages"
		}
	}
	start := linearStart(ins, y, base, fixed)
	first, err := solve(ins, y, start, fixed)
	if err != nil {
		return FormFit{}, err
	}
	if n := countPast(ins, &first.Params, kneeMin); n < minPastKnee {
		first.Fixed[NameExponent] = fmt.Sprintf("%d packages reach r >= %.1f; the exponent needs %d", n, kneeMin, minPastKnee)
		return first, nil
	}
	delete(fixed, NameExponent)
	best := FormFit{}
	var bestErr error
	// The kink at r = 1 can leave a local minimum; start the exponent
	// from a few places and keep the best.
	for _, a := range []float64{1, base.Exponent, 2} {
		s := first.Params
		s.Exponent = a
		ff, err := solve(ins, y, s, fixed)
		if err != nil {
			bestErr = err
			continue
		}
		if best.Fit.Coef == nil || ff.Fit.SSE < best.Fit.SSE {
			best = ff
		}
	}
	if best.Fit.Coef == nil {
		// Keep the fit with the exponent held rather than lose it all.
		first.Fixed[NameExponent] = "the fit with a free exponent failed: " + bestErr.Error()
		return first, nil
	}
	return best, nil
}

// countPast counts the packages whose r under p is at least lo, or above 1
// when lo is 1.
func countPast(ins []Inputs, p *Params, lo float64) int {
	n := 0
	for _, in := range ins {
		r := p.Ratio(in)
		if r > 1 && r >= lo {
			n++
		}
	}
	return n
}

// linearStart is the starting point for the solve: the linear fit of y on
// volume plus spec and the per-item inputs, with no knee, mapped to the
// form's parameters, or base with a scale matched to the means when that
// fit is degenerate.
func linearStart(ins []Inputs, y []float64, base Params, fixed map[string]string) Params {
	start := base
	start.Overhead = 0
	var sumR float64
	for _, in := range ins {
		sumR += base.RebuildTokens(in)
	}
	start.Scale = regress.Mean(y) / math.Max(sumR/float64(len(ins)), 1)
	var free []int
	for k, name := range costNames() {
		if _, ok := fixed[name]; !ok {
			free = append(free, k)
		}
	}
	x := make([][]float64, len(ins))
	for i, in := range ins {
		v := in.vector()
		x[i] = []float64{1, in.Volume + in.Spec}
		for _, k := range free {
			x[i] = append(x[i], v[costTerm+k])
		}
	}
	f, err := regress.OLS(x, y)
	if err != nil || f.Coef[1] <= 0 {
		return start
	}
	start.Overhead, start.Scale = f.Coef[0], f.Coef[1]
	for j, k := range free {
		*start.costs()[k] = f.Coef[2+j] / f.Coef[1]
	}
	return start
}

// solve runs Levenberg-Marquardt on the parameters not in fixed, from
// start.
func solve(ins []Inputs, y []float64, start Params, fixed map[string]string) (FormFit, error) {
	var free []freeParam
	for _, fp := range params() {
		if _, ok := fixed[fp.name]; !ok {
			free = append(free, fp)
		}
	}
	if len(ins) < len(free)+2 {
		return FormFit{}, &TooFewError{Packages: len(ins), Params: len(free)}
	}
	at := func(theta []float64) Params {
		p := start
		for j, fp := range free {
			*fp.ptr(&p) = theta[j]
		}
		return p
	}
	mdl := func(theta []float64) ([]float64, [][]float64) {
		p := at(theta)
		f := make([]float64, len(ins))
		jac := make([][]float64, len(ins))
		for i, in := range ins {
			f[i] = p.Measured(in)
			jac[i] = make([]float64, len(free))
			for j, fp := range free {
				jac[i][j] = fp.grad(&p, in)
			}
		}
		return f, jac
	}
	theta0 := make([]float64, len(free))
	for j, fp := range free {
		theta0[j] = *fp.ptr(&start)
	}
	fit, err := regress.LM(mdl, y, theta0)
	if err != nil {
		return FormFit{}, fmt.Errorf("fitting the 7.2 form: %w", err)
	}
	ff := FormFit{Params: at(fit.Coef), Fit: fit, Fixed: map[string]string{}}
	for k, v := range fixed {
		ff.Fixed[k] = v
	}
	for j, fp := range free {
		*fp.ptr(&ff.SE) = fit.SE[j]
	}
	ff.PastKnee = countPast(ins, &ff.Params, 1)
	ff.FarPastKnee = countPast(ins, &ff.Params, kneeMin)
	return ff, nil
}

// Linearize returns the function that undoes the knee of p on a measured
// value: it maps y to what the form would predict with no knee for the
// same rebuild_tokens, overhead + scale * rebuild_tokens. Below the knee
// that is y itself; past it, the passes (y - overhead) / (scale * budget)
// are taken to the power 1 / exponent. A y at or below the overhead is
// returned unchanged. Regressing the result linearly on the 7.1 inputs
// tests each one free of the superlinear growth the form already fits.
func Linearize(p Params) func(float64) float64 {
	return func(y float64) float64 {
		q := (y - p.Overhead) / (p.Scale * p.Budget)
		if q <= 1 || p.Scale <= 0 {
			return y
		}
		return p.Overhead + p.Scale*p.Budget*math.Pow(q, 1/p.Exponent)
	}
}

// ProfilePoint is the form's fit at one budget.
type ProfilePoint struct {
	// Budget is the context budget the fit held fixed.
	Budget float64
	// Fit is the fit at that budget; Err is set instead when it failed.
	Fit FormFit
	Err error
}

// BestBudget returns the budget of pts with the smallest residual sum of
// squares, and whether its improvement over the fit at budget at is
// significant at about the 95% level: the drop in SSE, over the best
// fit's residual variance, is at least the squared Student t critical
// value (an F test on the one parameter the budget adds). It returns 0
// when no point was fitted.
func BestBudget(pts []ProfilePoint, at float64) (best float64, significant bool) {
	var bestFit, atFit *FormFit
	for i := range pts {
		if pts[i].Err != nil {
			continue
		}
		f := &pts[i].Fit
		if bestFit == nil || f.Fit.SSE < bestFit.Fit.SSE {
			best, bestFit = pts[i].Budget, f
		}
		if pts[i].Budget == at {
			atFit = f
		}
	}
	if bestFit == nil || atFit == nil || bestFit == atFit || bestFit.Fit.DF < 1 {
		return best, false
	}
	df := bestFit.Fit.DF - 1
	if df < 1 {
		return best, false
	}
	f := (atFit.Fit.SSE - bestFit.Fit.SSE) / (bestFit.Fit.SSE / float64(df))
	t := regress.TCrit95(df)
	return best, f >= t*t
}

// Profile refits the form at each budget, so the report can show whether
// the data prefers a knee other than the one the estimate ships with.
func Profile(obs []Obs, base Params, budgets []float64) []ProfilePoint {
	out := make([]ProfilePoint, 0, len(budgets))
	for _, b := range budgets {
		p := base
		p.Budget = b
		ff, err := FitForm(obs, p)
		out = append(out, ProfilePoint{Budget: b, Fit: ff, Err: err})
	}
	return out
}
