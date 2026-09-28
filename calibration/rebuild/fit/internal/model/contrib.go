package model

import (
	"math"
	"slices"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Input names, as the report spells them. The first five are the terms of
// the 7.2 form; the rest are candidates the form leaves out.
const (
	InVolume         = "volume"
	InSpec           = "spec_tokens"
	InExports        = "exported_symbols"
	InUntested       = "untested_exports"
	InHidden         = "globals+init_funcs"
	InTestFuncs      = "test_funcs"
	InCognitiveTotal = "cognitive_total"
	InCognitiveP90   = "cognitive_p90"
	InMaxNesting     = "max_nesting"
	InFuncCount      = "func_count"
	InFanIn          = "fan_in"
)

// Input is one regressor: a 7.1 input or a candidate.
type Input struct {
	// Name is the input's name.
	Name string
	// InForm is true for the terms of the 7.2 form.
	InForm bool
	// Value reads the input from a package's metrics.
	Value func(m *metrics.RawMetrics) float64
}

// AllInputs returns the 7.1 inputs in the order of the form's terms, then
// test_funcs (a 7.1 input the formula does not read), the complexity
// candidates and fan_in.
func AllInputs() []Input {
	forms := [...]string{InVolume, InSpec, InExports, InUntested, InHidden}
	candidates := [...]string{InTestFuncs, InCognitiveTotal, InCognitiveP90, InMaxNesting, InFuncCount, InFanIn}
	out := make([]Input, 0, len(forms)+len(candidates))
	for k, name := range forms {
		out = append(out, Input{name, true, func(m *metrics.RawMetrics) float64 { return InputsOf(m).vector()[k] }})
	}
	for k, name := range candidates {
		out = append(out, Input{Name: name, Value: func(m *metrics.RawMetrics) float64 {
			return float64([...]int{m.TestFuncs, m.CognitiveTotal, m.CognitiveP90, m.MaxNesting, m.FuncCount, m.FanIn}[k])
		}})
	}
	return out
}

// distinct reports whether at takes more than one value over 0..n-1.
func distinct(n int, at func(i int) float64) bool {
	for i := 1; i < n; i++ {
		if at(i) != at(0) {
			return true
		}
	}
	return false
}

// valuesOf reads in from each item's metrics.
func valuesOf[T interface{ raw() *metrics.RawMetrics }](in Input, items []T) []float64 {
	return column(len(items), func(i int) float64 { return in.Value(items[i].raw()) })
}

// raw returns the package's metrics.
func (o Obs) raw() *metrics.RawMetrics { return &o.Metrics }

// raw returns the package's metrics.
func (o Outcome) raw() *metrics.RawMetrics { return &o.Metrics }

// column returns at(i) for i in 0..n-1.
func column(n int, at func(i int) float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = at(i)
	}
	return out
}

// Outcome is one run's verdict, for the correlation of each input with
// passing.
type Outcome struct {
	// Metrics are the package's raw metrics.
	Metrics metrics.RawMetrics
	// Passed is true when the oracle passed.
	Passed bool
}

// Verdict classifies what an input contributes.
type Verdict string

// Verdict values.
const (
	// VerdictCost: its coefficient on measured tokens is positive at about
	// the 95% level.
	VerdictCost Verdict = "adds cost"
	// VerdictHelp: its coefficient is negative at that level.
	VerdictHelp Verdict = "lowers cost"
	// VerdictPassOnly: no token signal, but it correlates with passing.
	VerdictPassOnly Verdict = "pass/fail only"
	// VerdictNone: neither test finds a signal.
	VerdictNone Verdict = "no measurable contribution"
	// VerdictNoVariation: the input takes one value in the data.
	VerdictNoVariation Verdict = "no variation in the data"
	// VerdictUntested: no coefficient could be fitted and passing shows no
	// signal, so nothing was measured either way.
	VerdictUntested Verdict = "not testable: no fit to test it in"
)

// Contribution is what the data says about one input.
type Contribution struct {
	// Input is the input's name; InForm is true for a 7.2 term.
	Input  string
	InForm bool
	// N is the packages with a measurement.
	N int
	// Pearson and Spearman correlate the input with measured tokens over
	// those packages; CorrOK is false when either side is constant.
	Pearson, Spearman float64
	CorrOK            bool
	// PassN is the runs with a verdict; PassCorr is the point-biserial
	// correlation of the input with passing over them, PassOK false when
	// it is undefined, and PassSignificant true when it differs from zero
	// at about the 95% level.
	PassN           int
	PassCorr        float64
	PassOK          bool
	PassSignificant bool
	// Coef, SE and T are the input's coefficient on measured tokens in the
	// linear fit (a form term) or the linear fit plus this input (a
	// candidate); CoefOK is false when that fit could not be made.
	Coef, SE, T     float64
	CoefOK          bool
	CoefSignificant bool
	// DeltaAdjR2 is, for a candidate, how much adding it moves the linear
	// fit's adjusted R2.
	DeltaAdjR2 float64
	// Verdict sums the tests up.
	Verdict Verdict
}

// LinearFit is the diagnostic fit with no knee and a free coefficient on
// every 7.2 term, volume and spec included, so the report can say whether
// the data agrees with 7.2's fixed coefficients.
type LinearFit struct {
	// Fit is the solution; Columns names its columns, intercept first.
	Fit     regress.Fit
	Columns []string
	// N is the packages it was fitted on.
	N int
	// Linearized is true when the measured tokens were first taken
	// through Linearize, so the fit has no knee to absorb.
	Linearized bool
}

// Col returns the index of the named column, or -1.
func (l *LinearFit) Col(name string) int {
	for i, c := range l.Columns {
		if c == name {
			return i
		}
	}
	return -1
}

// FitLinear fits measured tokens on an intercept and every form term that
// varies across obs, plus extra when it is not nil.
func FitLinear(obs []Obs, extra *Input) (LinearFit, error) {
	var cols []Input
	for _, in := range AllInputs() {
		if in.InForm && distinct(len(obs), func(i int) float64 { return in.Value(&obs[i].Metrics) }) {
			cols = append(cols, in)
		}
	}
	if extra != nil {
		cols = append(cols, *extra)
	}
	names := make([]string, 0, len(cols)+1)
	names = append(names, "intercept")
	for _, c := range cols {
		names = append(names, c.Name)
	}
	x := make([][]float64, len(obs))
	y := make([]float64, len(obs))
	for i := range obs {
		x[i] = make([]float64, 0, len(cols)+1)
		x[i] = append(x[i], 1)
		for _, c := range cols {
			x[i] = append(x[i], c.Value(&obs[i].Metrics))
		}
		y[i] = obs[i].Tokens
	}
	f, err := regress.OLS(x, y)
	if err != nil {
		return LinearFit{}, err
	}
	return LinearFit{Fit: f, Columns: names, N: len(obs)}, nil
}

// Contributions tests every input of AllInputs against the packages'
// measured tokens (obs) and the runs' verdicts (runs), and returns one
// Contribution per input with the linear fit the form terms were tested
// in, nil when that fit could not be made. The correlations read the
// measured tokens; the coefficients are fitted on link applied to them
// (Linearize of the fitted form, so the knee does not leak into the
// linear coefficients), or on the tokens themselves when link is nil.
func Contributions(obs []Obs, runs []Outcome, link func(float64) float64) ([]Contribution, *LinearFit) {
	tokens := column(len(obs), func(i int) float64 { return obs[i].Tokens })
	linear := slices.Clone(obs)
	for i := range linear {
		if link != nil {
			linear[i].Tokens = link(linear[i].Tokens)
		}
	}
	var lin *LinearFit
	if f, err := FitLinear(linear, nil); err == nil {
		f.Linearized = link != nil
		lin = &f
	}
	passed := column(len(runs), func(i int) float64 {
		if runs[i].Passed {
			return 1
		}
		return 0
	})
	all := AllInputs()
	out := make([]Contribution, 0, len(all))
	for _, in := range all {
		c := Contribution{Input: in.Name, InForm: in.InForm, N: len(obs), PassN: len(runs)}
		xs, rx := valuesOf(in, obs), valuesOf(in, runs)
		c.Pearson, c.CorrOK = regress.Pearson(xs, tokens)
		c.Spearman, _ = regress.Spearman(xs, tokens)
		c.PassCorr, c.PassOK = regress.Pearson(rx, passed)
		c.PassSignificant = c.PassOK && regress.CorrSignificant(c.PassCorr, len(runs))
		obsVary := distinct(len(xs), func(i int) float64 { return xs[i] })
		coefficient(&c, linear, in, lin, obsVary)
		c.Verdict = verdict(&c, obsVary || distinct(len(rx), func(i int) float64 { return rx[i] }))
		out = append(out, c)
	}
	return out, lin
}

// coefficient fills c's coefficient on measured tokens: from lin for a form
// term, from lin plus the input for a candidate, which needs the input to
// vary across obs.
func coefficient(c *Contribution, obs []Obs, in Input, lin *LinearFit, varied bool) {
	if lin == nil {
		return
	}
	f, j := lin, lin.Col(in.Name)
	if !in.InForm {
		if !varied {
			return
		}
		ext, err := FitLinear(obs, &in)
		if err != nil {
			return
		}
		f, j = &ext, len(ext.Columns)-1
		c.DeltaAdjR2 = ext.Fit.AdjR2 - lin.Fit.AdjR2
	}
	if j < 0 {
		return
	}
	c.Coef, c.SE, c.T = f.Fit.Coef[j], f.Fit.SE[j], f.Fit.T(j)
	c.CoefOK = !math.IsNaN(c.T)
	c.CoefSignificant = c.CoefOK && f.Fit.Significant(j)
}

// verdict sums up c's tests.
func verdict(c *Contribution, varied bool) Verdict {
	switch {
	case !varied:
		return VerdictNoVariation
	case c.CoefSignificant && c.Coef > 0:
		return VerdictCost
	case c.CoefSignificant:
		return VerdictHelp
	case c.PassSignificant:
		return VerdictPassOnly
	case !c.CoefOK:
		return VerdictUntested
	default:
		return VerdictNone
	}
}
