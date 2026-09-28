// Package dataset turns the rebuild runner's runs.jsonl rows into what the
// fit regresses on: one measurement per package from its passing runs, the
// failed runs as censored observations, and the rows that cannot count,
// each with the reason.
package dataset

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/runner"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Measure names the combination of the session's token counts that stands
// for a rebuild's cost.
type Measure string

// Measure values.
const (
	// Footprint is input + cache writes + output: every token that entered
	// the session's context, each counted once. Cache reads are left out
	// because they re-read that same context on every turn, so they grow
	// with turns times context and would count one token many times.
	Footprint Measure = "footprint"
	// Total is input + output + cache reads + cache writes, what the
	// session consumed.
	Total Measure = "total"
	// Output is the generated tokens alone.
	Output Measure = "output"
)

// ParseMeasure returns the Measure named s.
func ParseMeasure(s string) (Measure, error) {
	switch m := Measure(s); m {
	case Footprint, Total, Output:
		return m, nil
	}
	return "", fmt.Errorf("unknown token measure %q (want %s, %s or %s)", s, Footprint, Total, Output)
}

// Formula describes how m combines the token counts.
func (m Measure) Formula() string {
	switch m {
	case Total:
		return "input + output + cache reads + cache writes"
	case Output:
		return "output"
	default:
		return "input + cache writes + output"
	}
}

// Tokens returns u's tokens under m, and false when a count m needs was not
// reported.
func (m Measure) Tokens(u *agent.Usage) (float64, bool) {
	parts := []*int64{u.InputTokens, u.CacheWriteTokens, u.OutputTokens}
	switch m {
	case Total:
		parts = append(parts, u.CacheReadTokens)
	case Output:
		parts = []*int64{u.OutputTokens}
	}
	var s int64
	for _, p := range parts {
		if p == nil {
			return 0, false
		}
		s += *p
	}
	return float64(s), true
}

// Load reads and concatenates the rows of the runs files at paths.
func Load(paths []string) ([]runner.RunRow, error) {
	var rows []runner.RunRow
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("reading runs: %w", err)
		}
		r, err := runner.ParseRuns(p, data)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	if len(rows) == 0 {
		return nil, errors.New("no rows in the runs files")
	}
	return rows, nil
}

// Package is one experiment's measurement.
type Package struct {
	// Package and Module identify the experiment.
	Package, Module string
	// Metrics are its raw metrics at the pin.
	Metrics metrics.RawMetrics
	// Estimate is the estimate before the runs.
	Estimate runner.Estimate
	// Runs counts the runs with a verdict; Passes those that passed.
	Runs, Passes int
	// Tokens are the passing runs' tokens, in run order.
	Tokens []float64
	// Turns and WallSeconds are the passing runs' turns (where reported)
	// and agent wall times.
	Turns, WallSeconds []float64
	// CapHitPasses counts passing runs that reached the turn cap.
	CapHitPasses int
}

// Censored is a run that did not pass: the rebuild costs at least what it
// spent, so it bounds the cost from below rather than measuring it.
type Censored struct {
	// Package and Run identify the run.
	Package string
	Run     int
	// Metrics are the package's raw metrics.
	Metrics metrics.RawMetrics
	// Spent is the tokens the run spent; SpentOK is false when they were
	// not reported.
	Spent   float64
	SpentOK bool
	// Turns is the turns taken, or -1 when not reported.
	Turns int
	// Reason says why it did not pass.
	Reason string
}

// Excluded is a row the fit cannot use at all, with the reason.
type Excluded struct {
	// Package and Run identify the run.
	Package string
	Run     int
	// Reason says why it was left out.
	Reason string
}

// Set is the fit's view of the runs.
type Set struct {
	// Agent and Model name the agent and the model asked for, the same on
	// every row; Models are the models the sessions reported.
	Agent, Model string
	Models       []string
	// Unit is what every row rebuilt: a package, or a tree of packages
	// whose metrics are the members' aggregate.
	Unit string
	// Measure is the token measure used.
	Measure Measure
	// Rows is the number of rows read.
	Rows int
	// Packages are the experiments with at least one run with a verdict,
	// in first-seen order.
	Packages []Package
	// Censored are the valid runs that did not pass.
	Censored []Censored
	// Excluded are the rows that count for nothing.
	Excluded []Excluded
	// Outcomes are the verdicts of the valid runs, for the correlation of
	// each input with passing, with each run's package metrics.
	Outcomes []model.Outcome
}

// Build classifies rows under measure. Every row must come from one agent
// name and model and one unit, and no (package, run) may repeat, since the
// fit is for one agent and one unit: a tree's cost is not a package's. A
// row counts as a pass when it is valid, its oracle completed and
// passed, and its tokens were reported; a valid completed row that did not
// pass is censored; every other row is excluded with the reason.
func Build(rows []runner.RunRow, measure Measure) (Set, error) {
	s := Set{Measure: measure, Rows: len(rows)}
	if len(rows) == 0 {
		return s, errors.New("no rows")
	}
	s.Agent, s.Model, s.Unit = rows[0].Agent.Name, rows[0].Agent.Model, runner.RowUnit(&rows[0])
	seen := make(map[string]bool, len(rows))
	index := map[string]int{}
	for i := range rows {
		r := &rows[i]
		if r.Agent.Name != s.Agent || r.Agent.Model != s.Model {
			return s, fmt.Errorf("rows from agent %s (model %s) and %s (model %s): fit one agent at a time",
				s.Agent, s.Model, r.Agent.Name, r.Agent.Model)
		}
		if u := runner.RowUnit(r); u != s.Unit {
			return s, fmt.Errorf("rows of unit %s and %s (%s run %d): fit one unit at a time, from runs of "+
				"one definition", s.Unit, u, r.Package, r.Run)
		}
		key := r.Package + "#" + strconv.Itoa(r.Run)
		if seen[key] {
			return s, fmt.Errorf("run %d of %s appears twice", r.Run, r.Package)
		}
		seen[key] = true
		for _, m := range r.Measured.Models {
			if !slices.Contains(s.Models, m) {
				s.Models = append(s.Models, m)
			}
		}
		s.add(r, index)
	}
	slices.Sort(s.Models)
	return s, nil
}

// add classifies one row into s; index maps a package to its place in
// s.Packages.
func (s *Set) add(r *runner.RunRow, index map[string]int) {
	switch {
	case !r.Oracle.Completed:
		s.Excluded = append(s.Excluded, Excluded{r.Package, r.Run, "the oracle did not complete"})
		return
	case !r.Valid:
		s.Excluded = append(s.Excluded, Excluded{r.Package, r.Run, fmt.Sprintf(
			"broke the prompt's rules (changed %d test files, %d paths outside the package)",
			len(r.Changes.TestFiles), len(r.Changes.OutsidePackage))})
		return
	}
	tokens, ok := s.Measure.Tokens(&r.Measured.Usage)
	if r.Oracle.Passed && !ok {
		s.Excluded = append(s.Excluded, Excluded{r.Package, r.Run, "passed, but the agent reported no token counts"})
		return
	}
	s.Outcomes = append(s.Outcomes, model.Outcome{Metrics: r.Metrics, Passed: r.Oracle.Passed})
	i, found := index[r.Package]
	if !found {
		i = len(s.Packages)
		index[r.Package] = i
		s.Packages = append(s.Packages, Package{Package: r.Package, Module: r.Module, Metrics: r.Metrics, Estimate: r.Estimate})
	}
	p := &s.Packages[i]
	p.Runs++
	capHit := r.Measured.TurnCapHit != nil && *r.Measured.TurnCapHit
	if !r.Oracle.Passed {
		c := Censored{Package: r.Package, Run: r.Run, Metrics: r.Metrics, Spent: tokens, SpentOK: ok, Turns: -1,
			Reason: failReason(r, capHit)}
		if r.Measured.Turns != nil {
			c.Turns = *r.Measured.Turns
		}
		s.Censored = append(s.Censored, c)
		return
	}
	p.Passes++
	p.Tokens = append(p.Tokens, tokens)
	if r.Measured.Turns != nil {
		p.Turns = append(p.Turns, float64(*r.Measured.Turns))
	}
	p.WallSeconds = append(p.WallSeconds, float64(r.Agent.WallMS)/1000)
	if capHit {
		p.CapHitPasses++
	}
}

// failReason says why a valid completed run did not pass.
func failReason(r *runner.RunRow, capHit bool) string {
	var why string
	switch {
	case !r.Oracle.TestsPass:
		why = "tests fail"
	default:
		why = "importers do not compile"
	}
	switch {
	case capHit:
		why += "; hit the turn cap of " + strconv.Itoa(r.TurnCap)
	case r.Agent.TimedOut:
		why += "; timed out"
	case r.Measured.ResultSubtype != nil && *r.Measured.ResultSubtype != "success":
		why += "; session ended " + *r.Measured.ResultSubtype
	}
	return why
}

// Measured returns the packages with at least one passing run.
func (s *Set) Measured() []Package {
	var out []Package
	for _, p := range s.Packages {
		if p.Passes > 0 {
			out = append(out, p)
		}
	}
	return out
}

// Unmeasured returns the packages none of whose runs passed.
func (s *Set) Unmeasured() []Package {
	var out []Package
	for _, p := range s.Packages {
		if p.Passes == 0 {
			out = append(out, p)
		}
	}
	return out
}

// CapHitPasses counts the passing runs that reached the turn cap.
func (s *Set) CapHitPasses() int {
	n := 0
	for _, p := range s.Packages {
		n += p.CapHitPasses
	}
	return n
}
