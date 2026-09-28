package report

import (
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
)

// writeProfile writes the budget profile.
func writeProfile(b *strings.Builder, in *Input) {
	if len(in.Profile) == 0 {
		return
	}
	b.WriteString("## Context budget\n\nThe budget is held fixed in the fit: it only matters through the knee, " +
		"where the exponent takes over, and the two trade off. Refitted at other budgets:\n\n")
	b.WriteString("| Budget | R² | SSE | Exponent | Past the knee |\n| ---: | ---: | ---: | ---: | ---: |\n")
	for _, p := range in.Profile {
		if p.Err != nil {
			fmt.Fprintf(b, "| %s | - | - | - | %v |\n", num(p.Budget), p.Err)
			continue
		}
		exp := num(p.Fit.Params.Exponent)
		if _, fixed := p.Fit.Fixed[model.NameExponent]; fixed {
			exp += " (held)"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %d |\n", num(p.Budget), num(p.Fit.Fit.R2), num(p.Fit.Fit.SSE), exp, p.Fit.PastKnee)
	}
	b.WriteString("\n")
}

// writePackages writes one row per package with a verdict.
func writePackages(b *strings.Builder, in *Input) {
	b.WriteString("## Packages\n\nTokens are the median of the passing runs, with the smallest and largest. " +
		"Predicted is the fitted form's measured tokens.\n\n")
	b.WriteString("| Package | Tier before | Runs | Passed | Tokens | Spread | Turns | Wall s | Predicted | Residual % |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |\n")
	for _, p := range in.Set.Packages {
		tokens, spread, turns, wall, pred, res := "-", "-", "-", "-", "-", "-"
		if p.Passes > 0 {
			med := regress.Median(p.Tokens)
			tokens = num(med)
			spread = num(slices.Min(p.Tokens)) + "–" + num(slices.Max(p.Tokens))
			if len(p.Turns) > 0 {
				turns = num(regress.Median(p.Turns))
			}
			wall = num(regress.Median(p.WallSeconds))
			if in.Form != nil {
				v := in.Form.Params.Measured(model.InputsOf(&p.Metrics))
				pred = num(v)
				if med != 0 {
					res = num((med - v) / med * 100)
				}
			}
		}
		fmt.Fprintf(b, "| `%s` | %s | %d | %d | %s | %s | %s | %s | %s | %s |\n",
			short(p.Package, p.Module), p.Estimate.Tier, p.Runs, p.Passes, tokens, spread, turns, wall, pred, res)
	}
	b.WriteString("\n")
}

// writeCensored writes the failed and excluded runs.
func writeCensored(b *strings.Builder, in *Input) {
	s := in.Set
	b.WriteString("## Censored runs\n\nA run that did not pass is a lower bound: the rebuild costs at least what it " +
		"spent. The least-squares fit cannot use a bound, so these runs are left out of it and listed here. " +
		"Where the spend exceeds the fitted cost of a pass, the fit underestimates that package.\n\n")
	if len(s.Censored) == 0 {
		b.WriteString("None: every valid run passed.\n\n")
	} else {
		b.WriteString("| Package | Run | Spent | Turns | Predicted pass | Spent > predicted | Reason |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | :---: | --- |\n")
		for _, c := range s.Censored {
			spent, turns, pred, over := "not reported", "-", "-", "-"
			if c.SpentOK {
				spent = num(c.Spent)
			}
			if c.Turns >= 0 {
				turns = strconv.Itoa(c.Turns)
			}
			if in.Form != nil {
				v := in.Form.Params.Measured(model.InputsOf(&c.Metrics))
				pred = num(v)
				if c.SpentOK && c.Spent > v {
					over = "yes"
				} else if c.SpentOK {
					over = "no"
				}
			}
			fmt.Fprintf(b, "| `%s` | %d | %s | %s | %s | %s | %s |\n", path.Base(c.Package), c.Run, spent, turns, pred, over, c.Reason)
		}
		b.WriteString("\n")
	}
	if un := s.Unmeasured(); len(un) > 0 {
		b.WriteString("Packages with no passing run, so no measurement at all: ")
		names := make([]string, len(un))
		for i, p := range un {
			names[i] = "`" + short(p.Package, p.Module) + "` (" + string(p.Estimate.Tier) + ")"
		}
		b.WriteString(strings.Join(names, ", ") + ".\n\n")
	}
	b.WriteString("## Excluded rows\n\n")
	if len(s.Excluded) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	b.WriteString("| Package | Run | Reason |\n| --- | ---: | --- |\n")
	for _, e := range s.Excluded {
		fmt.Fprintf(b, "| `%s` | %d | %s |\n", path.Base(e.Package), e.Run, e.Reason)
	}
	b.WriteString("\n")
}

// short returns pkg relative to its module, or pkg when it is the module.
func short(pkg, module string) string {
	if rest, ok := strings.CutPrefix(pkg, module+"/"); ok {
		return path.Base(module) + "/" + rest
	}
	return pkg
}

// sigMark returns "*" for a significant value.
func sigMark(sig bool) string {
	if sig {
		return "*"
	}
	return ""
}

// num formats v with at most four significant figures, or as an integer
// when it is at least 1000.
func num(v float64) string {
	switch {
	case math.IsNaN(v) || math.IsInf(v, 0):
		return "-"
	case math.Abs(v) >= 1000:
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', 4, 64)
}

// pct formats a fraction as a percentage.
func pct(v float64) string {
	return strconv.FormatFloat(v*100, 'f', 1, 64) + "%"
}

// ratePct formats a/b as a percentage, "-" when b is 0.
func ratePct(a, b int) string {
	if b == 0 {
		return "-"
	}
	return pct(float64(a) / float64(b))
}

// codeList formats paths as a comma-separated list of code spans.
func codeList(paths []string) string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = "`" + p + "`"
	}
	return strings.Join(out, ", ")
}

// wrap breaks s into lines of at most 100 columns at spaces.
func wrap(s string) string {
	var b strings.Builder
	col := 0
	for i, w := range strings.Fields(s) {
		if i > 0 {
			if col+1+len(w) > 100 {
				b.WriteByte('\n')
				col = 0
			} else {
				b.WriteByte(' ')
				col++
			}
		}
		b.WriteString(w)
		col += len(w)
	}
	return b.String()
}
