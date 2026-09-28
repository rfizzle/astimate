package emit

import (
	"math"
	"strconv"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/internal/score"
)

// Note is one remark on how a fitted value became the emitted one.
type Note struct {
	// Param names the configuration parameter.
	Param string
	// Text is the remark.
	Text string
}

// Emitted is the rebuild section a fit produces: base with the fitted
// parameters, rounded to readable values, and the notes on each that was
// kept or clamped away from the fit.
type Emitted struct {
	// Params is the rebuild section to write.
	Params score.RebuildParams
	// Notes explain each parameter kept at its base value or clamped.
	Notes []Note
	// Fitted is true when at least one parameter took a fitted value.
	Fitted bool
}

// FromFit maps a form fit onto base's rebuild parameters. The budget is
// the fit's; each per-item cost the fit moved is rounded to two
// significant figures, and the exponent to two decimals. A negative cost
// is clamped to 0 and an exponent below 1 to 1, the ranges Validate
// allows, with a note, since the form cannot express what the data says.
// With no fit (ff nil), or a fit whose scale is not positive, every
// parameter keeps its base value, and reason (or the scale) says why.
func FromFit(base score.RebuildParams, ff *model.FormFit, reason string) Emitted {
	e := Emitted{Params: base}
	if ff != nil && ff.Params.Scale <= 0 {
		reason = "the fitted scale is not positive, so the form does not describe the data"
	}
	if ff == nil || ff.Params.Scale <= 0 {
		for _, n := range []string{model.NamePerExport, model.NamePerUntested, model.NamePerHidden, model.NameExponent} {
			e.Notes = append(e.Notes, Note{n, "kept at " + num(paramOf(&base, n)) + ": " + reason})
		}
		return e
	}
	e.Params.ContextBudget = ff.Params.Budget
	for _, c := range []struct {
		name   string
		fitted float64
		dst    *float64
	}{
		{model.NamePerExport, ff.Params.PerExport, &e.Params.TokensPerExport},
		{model.NamePerUntested, ff.Params.PerUntested, &e.Params.TokensPerUntestedExport},
		{model.NamePerHidden, ff.Params.PerHidden, &e.Params.TokensPerHiddenState},
	} {
		if why, ok := ff.Fixed[c.name]; ok {
			e.Notes = append(e.Notes, Note{c.name, "kept at " + num(*c.dst) + ": " + why})
			continue
		}
		e.Fitted = true
		if c.fitted < 0 {
			*c.dst = 0
			e.Notes = append(e.Notes, Note{c.name, "fitted " + num(c.fitted) +
				", clamped to 0: Validate requires >= 0, and the form cannot say that this input lowers the cost"})
			continue
		}
		*c.dst = sigFig2(c.fitted)
	}
	if why, ok := ff.Fixed[model.NameExponent]; ok {
		e.Notes = append(e.Notes, Note{model.NameExponent, "kept at " + num(base.SuperlinearExponent) + ": " + why})
		return e
	}
	e.Fitted = true
	e.Params.SuperlinearExponent = math.Round(ff.Params.Exponent*100) / 100
	if e.Params.SuperlinearExponent < 1 {
		e.Notes = append(e.Notes, Note{model.NameExponent, "fitted " + num(ff.Params.Exponent) +
			", clamped to 1: Validate requires >= 1; past the knee the measured cost grows no faster than r"})
		e.Params.SuperlinearExponent = 1
	}
	return e
}

// paramOf returns the named per-item cost or exponent of p.
func paramOf(p *score.RebuildParams, name string) float64 {
	switch name {
	case model.NamePerExport:
		return p.TokensPerExport
	case model.NamePerUntested:
		return p.TokensPerUntestedExport
	case model.NamePerHidden:
		return p.TokensPerHiddenState
	default:
		return p.SuperlinearExponent
	}
}

// sigFig2 rounds v to two significant figures.
func sigFig2(v float64) float64 {
	if v == 0 {
		return 0
	}
	k := math.Floor(math.Log10(math.Abs(v))) - 1
	if k < 0 {
		// Divide by a whole power of ten, so 0.3 stays 0.3.
		m := math.Pow(10, -k)
		return math.Round(v*m) / m
	}
	e := math.Pow(10, k)
	return math.Round(v/e) * e
}

// num formats a whole number as an integer and anything else with at most
// four significant figures.
func num(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', 4, 64)
}

// Values returns p's fitted parameters as the configuration names them,
// for the report's tables.
func Values(p *score.RebuildParams) []Note {
	names := [...]string{model.NameBudget, model.NamePerExport, model.NamePerUntested, model.NamePerHidden, model.NameExponent}
	vals := [...]float64{p.ContextBudget, p.TokensPerExport, p.TokensPerUntestedExport, p.TokensPerHiddenState, p.SuperlinearExponent}
	out := make([]Note, len(names))
	for i, n := range names {
		out[i] = Note{n, num(vals[i])}
	}
	return out
}
