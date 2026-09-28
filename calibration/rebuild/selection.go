package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// stdlibModule is the corpus name of the standard library, which the
// selection leaves out (see README.md).
const stdlibModule = "std"

// Row is one line of the collector's packages.jsonl.
type Row struct {
	// Module is the module path, or "std".
	Module string `json:"module"`
	// Commit is the pinned commit, or the Go version for std.
	Commit string `json:"commit"`
	// Package is the import path.
	Package string `json:"package"`
	// Metrics are the package's raw metrics.
	Metrics metrics.RawMetrics `json:"metrics"`
	// AgentPasses is the rounded rebuild estimate.
	AgentPasses float64 `json:"agent_passes"`
	// HumanDays is the rounded human estimate.
	HumanDays float64 `json:"human_days"`
}

// ReadRows reads packages.jsonl.
func ReadRows(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading rows: %w", err)
	}
	defer func() { _ = f.Close() }()
	var rows []Row
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		var r Row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("reading %s:%d: %w", path, line, err)
		}
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return rows, nil
}

// Candidate is a package the selection may take, with its estimate
// recomputed from its metrics.
type Candidate struct {
	// Row is the packages.jsonl row.
	Row Row
	// Repo is the module's clone URL from corpus.yaml.
	Repo string
	// Estimate is the rebuild estimate under the default parameters.
	Estimate score.Rebuild
	// Tier is the estimate's tier.
	Tier score.Tier
}

// stratum is one cell of the selection: a tier and whether the package has
// tests of its own.
type stratum struct {
	tier   score.Tier
	tested bool
}

// String names the stratum for reports.
func (s stratum) String() string {
	if s.tested {
		return string(s.tier) + " tested"
	}
	return string(s.tier) + " untested"
}

// strata returns the six strata in selection order.
func strata() []stratum {
	out := make([]stratum, 0, 6)
	for _, t := range tiers() {
		out = append(out, stratum{t, true}, stratum{t, false})
	}
	return out
}

// Candidates returns the rows eligible for selection with their estimates
// under p: rows of a cloned corpus module (repos maps module path to clone
// URL; std and modules missing from it are left out) known not to use cgo
// and to have no generated files, that declare at least one function, are estimated at
// no more than maxPasses agent passes, and, when they have no tests, are
// imported by at least one other package of the module. It fails when a
// row's recomputed estimate does not round to the agent_passes it carries,
// which means the data was collected under other rebuild parameters.
func Candidates(rows []Row, repos map[string]string, p score.RebuildParams, maxPasses float64) ([]Candidate, error) {
	var out []Candidate
	for i := range rows {
		r := &rows[i]
		repo, ok := repos[r.Module]
		m := &r.Metrics
		switch {
		case !ok || r.Module == stdlibModule,
			m.UsesCgo == nil || *m.UsesCgo,
			m.GeneratedFiles == nil || *m.GeneratedFiles > 0,
			m.FuncCount == 0,
			r.AgentPasses > maxPasses,
			!m.HasTests && m.FanIn+m.FanInTests == 0:
			continue
		}
		est := score.Estimate(*m, p)
		if got := est.AgentPassesRounded(); got != r.AgentPasses {
			return nil, fmt.Errorf("%s: agent_passes %v recomputes to %v; the data predates the current rebuild parameters",
				r.Package, r.AgentPasses, got)
		}
		out = append(out, Candidate{Row: *r, Repo: repo, Estimate: est, Tier: score.TierOf(est.AgentPasses, p.Tiers)})
	}
	return out, nil
}

// Verdict is the outcome of checking one candidate.
type Verdict struct {
	// Candidate is the package checked.
	Candidate Candidate
	// Stratum names the candidate's stratum.
	Stratum string
	// OK is true when the candidate was taken.
	OK bool
	// Reason says why a candidate was rejected; empty when taken.
	Reason string
	// Experiment is the verified experiment when OK.
	Experiment Experiment
}

// checkFunc verifies one candidate at its pin and returns the experiment,
// or an error whose text is the rejection reason.
type checkFunc func(ctx context.Context, c Candidate) (Experiment, error)

// Select applies the selection rule: candidates are split into the six
// strata (tier by has_tests), each sorted by rebuild_tokens then import
// path; stratum slot k of perStratum starts at index floor(k*n/perStratum)
// of its n candidates and takes the first candidate from there on that is
// not yet taken, whose module has fewer than maxPerModule packages taken,
// and that check accepts. Strata are filled in order, slot by slot, so the
// result depends only on the candidates and check's verdicts. It returns
// every verdict in the order they were reached.
func Select(ctx context.Context, cands []Candidate, perStratum, maxPerModule int, check checkFunc) ([]Verdict, error) {
	byStratum := make(map[stratum][]Candidate)
	for _, c := range cands {
		s := stratum{c.Tier, c.Row.Metrics.HasTests}
		byStratum[s] = append(byStratum[s], c)
	}
	perModule := make(map[string]int)
	tried := make(map[string]bool)
	var verdicts []Verdict
	for _, s := range strata() {
		list := byStratum[s]
		slices.SortFunc(list, func(a, b Candidate) int {
			return cmp.Or(
				cmp.Compare(math.Round(a.Estimate.RebuildTokens), math.Round(b.Estimate.RebuildTokens)),
				strings.Compare(a.Row.Package, b.Row.Package))
		})
		for k := range perStratum {
			for i := k * len(list) / perStratum; i < len(list); i++ {
				c := list[i]
				if tried[c.Row.Package] || perModule[c.Row.Module] >= maxPerModule {
					continue
				}
				if err := ctx.Err(); err != nil {
					return verdicts, err
				}
				tried[c.Row.Package] = true
				exp, err := check(ctx, c)
				v := Verdict{Candidate: c, Stratum: s.String(), OK: err == nil, Experiment: exp}
				if err != nil {
					v.Reason = err.Error()
				}
				verdicts = append(verdicts, v)
				if err == nil {
					perModule[c.Row.Module]++
					break
				}
			}
		}
	}
	return verdicts, nil
}

// newExperiment fills an experiment from a candidate; the oracle and stub
// hash come from the checks at the pin.
func newExperiment(c Candidate, oracle Oracle, stubHash string, turnCap int) Experiment {
	return Experiment{
		Module:        c.Row.Module,
		Repo:          c.Repo,
		Commit:        c.Row.Commit,
		Package:       c.Row.Package,
		Dir:           modRelDir(c.Row.Module, c.Row.Package),
		Stub:          StubSignatures,
		StubSHA256:    stubHash,
		Oracle:        oracle,
		TurnCap:       turnCap,
		HasTests:      c.Row.Metrics.HasTests,
		Tier:          c.Tier,
		AgentPasses:   c.Row.AgentPasses,
		RebuildTokens: int(math.Round(c.Estimate.RebuildTokens)),
		HumanDays:     c.Row.HumanDays,
		Metrics:       Metrics{c.Row.Metrics},
	}
}
