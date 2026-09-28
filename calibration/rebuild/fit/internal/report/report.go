// Package report renders the rebuild fit's Markdown report: what the data
// was, how well the SPEC.md 7.2 form fits each measured output, what each
// input contributes, the censored and excluded runs, the parameters before
// and after, and a paragraph a reader can act on.
package report

import (
	"fmt"
	"path"
	"strings"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/dataset"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/emit"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
	"github.com/rfizzle/astimate/internal/score"
)

// Input is everything the report describes.
type Input struct {
	// Version is the emitted config_version; BaseVersion the base's.
	Version, BaseVersion string
	// Runs are the runs files read; Out is the emitted configuration's
	// path; Command reproduces the fit.
	Runs    []string
	Out     string
	Command string
	// Set is the classified runs.
	Set *dataset.Set
	// Obs are the measured packages the fits were made on, in Set order.
	Obs []model.Obs
	// Base is the base configuration's rebuild section; Emitted what was
	// written.
	Base    score.RebuildParams
	Emitted emit.Emitted
	// Form is the fit of the 7.2 form, nil when FormErr says why not.
	Form    *model.FormFit
	FormErr error
	// Linear is the diagnostic fit with free coefficients, nil when it
	// could not be made.
	Linear *model.LinearFit
	// Contributions are the per-input tests.
	Contributions []model.Contribution
	// Profile is the form refitted at other budgets.
	Profile []model.ProfilePoint
}

// Render returns the report.
func Render(in *Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Rebuild parameters %s\n\n", in.Version)
	fmt.Fprintf(&b, "Fitted by `calibration/rebuild/fit` (SPEC.md 7.2 and 11.2) from %s. Candidate configuration: `%s`, "+
		"the base `%s` with only the rebuild parameters and `config_version` changed.\n\n",
		codeList(in.Runs), in.Out, in.BaseVersion)
	b.WriteString("## Summary\n\n")
	b.WriteString(wrap(summary(in)) + "\n\n")
	b.WriteString("Before this configuration ships, check it against the acceptance invariants (SPEC.md 7.5):\n\n")
	cfgPath := "$PWD/" + in.Out
	if path.IsAbs(in.Out) {
		cfgPath = in.Out
	}
	fmt.Fprintf(&b, "```sh\nASTIMATE_CONFIG=%s go test ./internal/invariants\n```\n\n", cfgPath)
	for _, section := range []func(*strings.Builder, *Input){
		writeParams, writeData, writeQuality, writeContributions, writeForm, writeProfile, writePackages, writeCensored,
	} {
		section(&b, in)
	}
	b.WriteString("## Reproduce\n\n```sh\n" + in.Command + "\n```\n")
	return b.String()
}

// summary is the plain-English paragraph at the top.
func summary(in *Input) string {
	s := in.Set
	var parts []string
	parts = append(parts, fmt.Sprintf("%d runs of %d packages by %s (model %s): %d packages have a passing run, "+
		"%d runs failed and are censored, %d rows were excluded.",
		s.Rows, len(s.Packages), s.Agent, s.Model, len(in.Obs), len(s.Censored), len(s.Excluded)))
	if in.Form == nil {
		parts = append(parts, fmt.Sprintf("The 7.2 form could not be fitted (%v), so every parameter keeps its base "+
			"value: this configuration is calibrated in name only and must not ship.", in.FormErr))
		return strings.Join(parts, " ")
	}
	f := in.Form
	parts = append(parts, fmt.Sprintf("The 7.2 form explains %s of the variance in measured tokens (R², %d packages). "+
		"A session spends about %s tokens before it reads the package, and each token of the estimate costs "+
		"about %s measured tokens.", pct(f.Fit.R2), len(in.Obs), num(f.Params.Overhead), num(f.Params.Scale)))
	if f.Fit.R2 < 0.5 {
		parts = append(parts, "That is a poor fit: the form does not describe what a rebuild costs this agent, "+
			"and the parameters below should not be trusted.")
	}
	var changed []string
	before, after := emit.Values(&in.Base), emit.Values(&in.Emitted.Params)
	for i := range before {
		if before[i].Text != after[i].Text {
			changed = append(changed, before[i].Param+" "+before[i].Text+" → "+after[i].Text)
		}
	}
	if len(changed) > 0 {
		parts = append(parts, "Changed: "+strings.Join(changed, ", ")+".")
	} else {
		parts = append(parts, "No parameter changed.")
	}
	for _, n := range in.Emitted.Notes {
		parts = append(parts, n.Param+" "+n.Text+".")
	}
	parts = append(parts, specSentence(in.Linear))
	if none := silent(in.Contributions); len(none) > 0 {
		parts = append(parts, "No measurable contribution, to tokens or to passing: "+strings.Join(none, ", ")+".")
	}
	if len(s.Censored) > 0 {
		above := 0
		for _, c := range s.Censored {
			if c.SpentOK && c.Spent > f.Params.Measured(model.InputsOf(&c.Metrics)) {
				above++
			}
		}
		parts = append(parts, fmt.Sprintf("The fit uses passing runs only. Of the %d failed runs, %d had already "+
			"spent more than the fit predicts a pass of their package costs, so the fit underestimates at least "+
			"those packages.", len(s.Censored), above))
	}
	if un := s.Unmeasured(); len(un) > 0 {
		parts = append(parts, fmt.Sprintf("%d packages never passed and are not in the fit at all; if they are the "+
			"hard ones, every fitted cost is biased low.", len(un)))
	}
	parts = append(parts, budgetSentence(in.Profile, f.Params.Budget))
	return strings.Join(parts, " ")
}

// budgetSentence says whether the data prefers another context budget.
func budgetSentence(pts []model.ProfilePoint, at float64) string {
	best, sig := model.BestBudget(pts, at)
	switch {
	case best == 0:
		// BestBudget found no fitted point: there is no profile to read.
		return "The form could not be refitted at other budgets, so the budget stays."
	case best == at:
		return fmt.Sprintf("Of the budgets tried, %s fits best.", num(at))
	case sig:
		return fmt.Sprintf("The data prefers a context budget of %s to the %s the estimate uses, beyond noise; "+
			"rerun with --budget %s to adopt it.", num(best), num(at), num(best))
	default:
		return fmt.Sprintf("The residuals are smallest at a context budget of %s, but not significantly below "+
			"those at %s: the data cannot locate the knee, so the budget stays.", num(best), num(at))
	}
}

// specSentence says whether tests count as cost or as help, from the
// linear fit's free spec coefficient relative to the volume one.
func specSentence(lin *model.LinearFit) string {
	if lin == nil {
		return "The data cannot say whether tests count as cost or as help."
	}
	sp, vol := lin.Col(model.InSpec), lin.Col(model.InVolume)
	if sp < 0 || vol < 0 {
		return "The data cannot say whether tests count as cost or as help: spec or volume does not vary."
	}
	ratio, se := lin.Fit.Coef[sp]/lin.Fit.Coef[vol], lin.Fit.RatioSE(sp, vol)
	lo, hi := ratio-regress.TCrit95(lin.Fit.DF)*se, ratio+regress.TCrit95(lin.Fit.DF)*se
	head := fmt.Sprintf("Fitted freely, one test token costs %s volume tokens (95%% interval %s to %s; 7.2 fixes it at 1)",
		num(ratio), num(lo), num(hi))
	switch {
	case !lin.Fit.Significant(sp):
		return head + ": tests carry no measurable cost, and the data cannot tell cost from help."
	case lin.Fit.Coef[sp] < 0:
		return head + ": tests help, lowering the cost; the 7.2 form, which charges for them, does not fit, " +
			"and SPEC.md 7.5's rule that adding tests raises only the spec term is the thing to revisit."
	case lo > 1 || hi < 1:
		return head + ": tests count as cost, but not at the rate 7.2 charges."
	default:
		return head + ": tests count as cost, consistent with 7.2."
	}
}

// silent names the inputs with no measurable contribution.
func silent(cs []model.Contribution) []string {
	var out []string
	for _, c := range cs {
		if c.Verdict == model.VerdictNone || c.Verdict == model.VerdictNoVariation || c.Verdict == model.VerdictUntested {
			out = append(out, c.Input)
		}
	}
	return out
}

// writeParams writes the parameter table, before and after.
func writeParams(b *strings.Builder, in *Input) {
	b.WriteString("## Parameters\n\n| Parameter | Base | Fitted | SE | Emitted |\n| --- | ---: | ---: | ---: | ---: |\n")
	before, after := emit.Values(&in.Base), emit.Values(&in.Emitted.Params)
	for i := range before {
		fitted, se := "-", "-"
		if in.Form != nil {
			v, e := formParam(in.Form, before[i].Param)
			fitted = num(v)
			if _, fixed := in.Form.Fixed[before[i].Param]; fixed || before[i].Param == model.NameBudget {
				fitted = "held"
			} else {
				se = num(e)
			}
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n", before[i].Param, before[i].Text, fitted, se, after[i].Text)
	}
	if in.Form != nil {
		f := in.Form
		fmt.Fprintf(b, "| overhead (fit only) | - | %s | %s | - |\n", num(f.Params.Overhead), num(f.SE.Overhead))
		fmt.Fprintf(b, "| scale (fit only) | - | %s | %s | - |\n", num(f.Params.Scale), num(f.SE.Scale))
	}
	b.WriteString("\nThe overhead (tokens a session spends whatever the package) and the scale (measured tokens per " +
		"token of the estimate) belong to the fit, not the configuration: the estimate counts what a rebuild must " +
		"hold in context, and the scale converts it to what the agent spent. The per-item costs are rounded to two " +
		"significant figures and the exponent to two decimals.\n\n")
	for _, n := range in.Emitted.Notes {
		fmt.Fprintf(b, "- `%s`: %s.\n", n.Param, n.Text)
	}
	if len(in.Emitted.Notes) > 0 {
		b.WriteString("\n")
	}
}

// formParam returns the fitted value and standard error of the named
// configuration parameter.
func formParam(f *model.FormFit, name string) (v, se float64) {
	switch name {
	case model.NameBudget:
		return f.Params.Budget, 0
	case model.NamePerExport:
		return f.Params.PerExport, f.SE.PerExport
	case model.NamePerUntested:
		return f.Params.PerUntested, f.SE.PerUntested
	case model.NamePerHidden:
		return f.Params.PerHidden, f.SE.PerHidden
	default:
		return f.Params.Exponent, f.SE.Exponent
	}
}

// writeData describes the rows and the token measure.
func writeData(b *strings.Builder, in *Input) {
	s := in.Set
	b.WriteString("## Data\n\n")
	fmt.Fprintf(b, "| | |\n| --- | --- |\n| Agent | %s |\n| Model asked for | %s |\n| Models reported | %s |\n",
		s.Agent, s.Model, strings.Join(s.Models, ", "))
	fmt.Fprintf(b, "| Rows | %d |\n| Packages with a verdict | %d |\n| Packages with a passing run | %d |\n",
		s.Rows, len(s.Packages), len(in.Obs))
	fmt.Fprintf(b, "| Censored runs (failed) | %d |\n| Excluded rows | %d |\n| Passing runs that hit the turn cap | %d |\n\n",
		len(s.Censored), len(s.Excluded), s.CapHitPasses())
	fmt.Fprintf(b, "Measured tokens are the `%s` measure, %s, summed over the session. ", s.Measure, s.Measure.Formula())
	b.WriteString("The footprint counts every token that entered the agent's context once: what it read, " +
		"what tools returned and what it wrote. That is the quantity 7.2's rebuild_tokens models, what a rebuild " +
		"must hold in context. Cache reads are left out of it because each turn re-reads the whole context, so " +
		"they grow with turns times context size and count the same token many times; `--measure total` " +
		"regresses on everything the session consumed instead. A package's measurement is the median of its " +
		"passing runs; the spread is in the package table.\n\n")
}

// writeQuality writes the fit quality per output and the residuals by
// tier.
func writeQuality(b *strings.Builder, in *Input) {
	b.WriteString("## Fit quality per output\n\n")
	if in.Form == nil {
		b.WriteString("No fit: " + fmt.Sprint(in.FormErr) + ".\n\n")
		return
	}
	f := in.Form
	b.WriteString("| Output | N | Against | R² | Note |\n| --- | ---: | --- | ---: | --- |\n")
	fmt.Fprintf(b, "| Measured tokens | %d | the 7.2 form | %s | %d packages past the knee (r > 1), %d at r ≥ 1.1 |\n",
		len(in.Obs), num(f.Fit.R2), f.PastKnee, f.FarPastKnee)
	if in.Linear != nil {
		against := "free linear fit, no knee"
		if in.Linear.Linearized {
			against = "free linear fit, on tokens with the knee undone"
		}
		fmt.Fprintf(b, "| Measured tokens | %d | %s | %s | adjusted R² %s |\n",
			in.Linear.N, against, num(in.Linear.Fit.R2), num(in.Linear.Fit.AdjR2))
	}
	est, turns, wall := outputSeries(in)
	for _, o := range []struct {
		name string
		x, y []float64
	}{{"Turns (median of passes)", est.turnsX, turns}, {"Agent wall seconds (median of passes)", est.wallX, wall}} {
		of := fitOutput(o.x, o.y)
		if !of.ok {
			fmt.Fprintf(b, "| %s | %d | fitted rebuild_tokens | - | too few to fit |\n", o.name, of.n)
			continue
		}
		fmt.Fprintf(b, "| %s | %d | fitted rebuild_tokens | %s | %s + %s per 1,000 rebuild tokens |\n",
			o.name, of.n, num(of.r2), num(of.intercept), num(of.slope*1000))
	}
	pb, n, ok := passCorrelation(in)
	if ok {
		fmt.Fprintf(b, "| Passing (per run) | %d | emitted agent_passes | - | point-biserial r = %s%s |\n",
			n, num(pb), sigMark(regress.CorrSignificant(pb, n)))
	}
	b.WriteString("\n### Residuals by tier\n\nTiers under the fitted parameters; residual = measured − predicted.\n\n")
	b.WriteString("| Tier | Packages | Mean residual | Mean abs % | Max abs % |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, r := range residualsByTier(in.Obs, &f.Params, in.Base.Tiers) {
		fmt.Fprintf(b, "| %s | %d | %s | %s | %s |\n", r.tier, r.n, num(r.meanResidual), num(r.meanAbsPct), num(r.maxAbsPct))
	}
	b.WriteString("\n### Pass rate by tier\n\nTiers under the emitted parameters, over every run with a verdict.\n\n")
	b.WriteString("| Tier | Runs | Passed | Pass rate |\n| --- | ---: | ---: | ---: |\n")
	for _, t := range passByTier(in) {
		fmt.Fprintf(b, "| %s | %d | %d | %s |\n", t.tier, t.runs, t.passed, ratePct(t.passed, t.runs))
	}
	b.WriteString("\n")
}

// series pairs each package's fitted rebuild_tokens with its median turns
// and wall time, for the packages that report them.
type series struct{ turnsX, wallX []float64 }

// outputSeries returns the fitted rebuild_tokens against median turns and
// median wall seconds.
func outputSeries(in *Input) (x series, turns, wall []float64) {
	for _, p := range in.Set.Measured() {
		rt := in.Form.Params.RebuildTokens(model.InputsOf(&p.Metrics))
		if len(p.Turns) > 0 {
			x.turnsX = append(x.turnsX, rt)
			turns = append(turns, regress.Median(p.Turns))
		}
		x.wallX = append(x.wallX, rt)
		wall = append(wall, regress.Median(p.WallSeconds))
	}
	return x, turns, wall
}

// passCorrelation correlates each run's agent_passes under the emitted
// parameters with passing.
func passCorrelation(in *Input) (r float64, n int, ok bool) {
	xs := make([]float64, len(in.Set.Outcomes))
	ys := make([]float64, len(in.Set.Outcomes))
	for i, o := range in.Set.Outcomes {
		xs[i] = score.Estimate(o.Metrics, in.Emitted.Params).AgentPasses
		if o.Passed {
			ys[i] = 1
		}
	}
	r, ok = regress.Pearson(xs, ys)
	return r, len(xs), ok
}

// tierPass counts runs and passes in one tier.
type tierPass struct {
	tier         score.Tier
	runs, passed int
}

// passByTier counts runs and passes per tier under the emitted parameters.
func passByTier(in *Input) []tierPass {
	out := []tierPass{{tier: score.TierOnePass}, {tier: score.TierFewPasses}, {tier: score.TierPartition}}
	for _, o := range in.Set.Outcomes {
		t := score.TierOf(score.Estimate(o.Metrics, in.Emitted.Params).AgentPasses, in.Emitted.Params.Tiers)
		for i := range out {
			if out[i].tier == t {
				out[i].runs++
				if o.Passed {
					out[i].passed++
				}
			}
		}
	}
	return out
}

// writeContributions writes the per-input correlation and contribution
// table.
func writeContributions(b *strings.Builder, in *Input) {
	b.WriteString("## What each input contributes\n\n")
	b.WriteString("Correlations of each input with the packages' measured tokens (Pearson and Spearman) and with " +
		"passing over every run with a verdict (point-biserial). The coefficient is the input's measured tokens " +
		"per unit in a linear fit with an intercept and every 7.2 term: for a 7.2 term that fit itself, for a " +
		"candidate that fit plus the candidate alone, with the change in adjusted R² it brings. `*` marks a value " +
		"that differs from zero at about the 95% level. An input with no marked coefficient and no marked pass " +
		"correlation has no measurable contribution.\n\n")
	b.WriteString("| Input | In 7.2 | Pearson | Spearman | Pass r | Coefficient | SE | t | ΔR²adj | Verdict |\n")
	b.WriteString("| --- | :---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n")
	for _, c := range in.Contributions {
		form := ""
		if c.InForm {
			form = "yes"
		}
		pear, spear, pass := "-", "-", "-"
		if c.CorrOK {
			pear, spear = num(c.Pearson), num(c.Spearman)
		}
		if c.PassOK {
			pass = num(c.PassCorr) + sigMark(c.PassSignificant)
		}
		coef, se, t, d := "-", "-", "-", "-"
		if c.CoefOK {
			coef, se, t = num(c.Coef)+sigMark(c.CoefSignificant), num(c.SE), num(c.T)
			if !c.InForm {
				d = num(c.DeltaAdjR2)
			}
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			c.Input, form, pear, spear, pass, coef, se, t, d, c.Verdict)
	}
	b.WriteString("\n" + wrap(specSentence(in.Linear)) + "\n\n")
}

// writeForm writes the free linear fit's coefficients next to the form's
// fixed ones.
func writeForm(b *strings.Builder, in *Input) {
	b.WriteString("## Does the data fit the 7.2 form?\n\n")
	if in.Linear == nil {
		b.WriteString("The free linear fit could not be made (too few packages, or inputs that do not vary).\n\n")
		return
	}
	lin := in.Linear
	vol := lin.Col(model.InVolume)
	b.WriteString("The free linear fit gives every term its own coefficient. Dividing by the volume coefficient " +
		"puts each in rebuild tokens, the units 7.2 uses, where volume and spec are fixed at 1.")
	if lin.Linearized {
		b.WriteString(" It is fitted on the measured tokens with the fitted knee undone (each past-the-knee " +
			"measurement taken back through the exponent), so the superlinear growth the form already models " +
			"does not load onto the terms that grow with size.")
	}
	b.WriteString("\n\n")
	// Compare with the form's fitted values, or the base's without a fit.
	col, e, u, h := "7.2 base", in.Base.TokensPerExport, in.Base.TokensPerUntestedExport, in.Base.TokensPerHiddenState
	if in.Form != nil {
		col, e, u, h = "Form fit", in.Form.Params.PerExport, in.Form.Params.PerUntested, in.Form.Params.PerHidden
	}
	b.WriteString("| Term | Coefficient | SE | Per volume token | " + col + " |\n| --- | ---: | ---: | ---: | ---: |\n")
	fixed := map[string]string{
		model.InVolume: "1", model.InSpec: "1", model.InExports: num(e), model.InUntested: num(u), model.InHidden: num(h),
	}
	for j, name := range lin.Columns {
		per := "-"
		if vol >= 0 && j > 0 {
			per = num(lin.Fit.Coef[j]/lin.Fit.Coef[vol]) + " ± " + num(lin.Fit.RatioSE(j, vol))
		}
		want := fixed[name]
		if want == "" {
			want = "-"
		}
		fmt.Fprintf(b, "| `%s` | %s%s | %s | %s | %s |\n", name, num(lin.Fit.Coef[j]), sigMark(lin.Fit.Significant(j)),
			num(lin.Fit.SE[j]), per, want)
	}
	b.WriteString("\nThe intercept is the per-session overhead; 7.2 has none, since it counts only what the package " +
		"costs. A term whose per-volume-token value is far from the 7.2 column, measured in its standard errors, " +
		"is one the form gets wrong, or one the knee absorbs differently.\n\n")
}
