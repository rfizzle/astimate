// Package selection applies the deterministic rule described in
// calibration/rebuild/README.md that picks the corpus packages the
// rebuild experiment rebuilds: candidates are read from the collector's
// packages.jsonl, split into strata by tier and has_tests, and filled slot
// by slot from a caller-supplied check.
package selection

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

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

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
	// Estimate is the rebuild estimate under the selection's parameters;
	// for a tree, the members' summed and passed through the curve once.
	Estimate score.Rebuild
	// Tier is the estimate's tier.
	Tier score.Tier
	// Members are a tree's packages, the tree's own first; empty for a
	// package. A tree's Row carries its module, commit and import path,
	// the members' aggregate metrics, and the tree's rounded estimate.
	Members []Candidate
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
		case !ok || r.Module == definition.StdlibModule,
			m.UsesCgo == nil || *m.UsesCgo,
			m.GeneratedFiles == nil || *m.GeneratedFiles > 0,
			m.FuncCount == 0,
			r.AgentPasses > maxPasses,
			!m.HasTests && m.FanIn+m.FanInTests == 0:
			continue
		}
		c, err := estimated(r, repo, p)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// estimated returns the candidate of row r with its estimate recomputed
// under p, and fails when it does not round to the row's agent_passes.
func estimated(r *Row, repo string, p score.RebuildParams) (Candidate, error) {
	est := score.Estimate(r.Metrics, p)
	if got := est.AgentPassesRounded(); got != r.AgentPasses {
		return Candidate{}, fmt.Errorf("%s: agent_passes %v recomputes to %v; the data predates the current rebuild parameters",
			r.Package, r.AgentPasses, got)
	}
	return Candidate{Row: *r, Repo: repo, Estimate: est, Tier: score.TierOf(est.AgentPasses, p.Tiers)}, nil
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
	Experiment definition.Experiment
}

// checkFunc verifies one candidate at its pin and returns the experiment,
// or an error whose text is the rejection reason.
type checkFunc func(ctx context.Context, c Candidate) (definition.Experiment, error)

// Select applies the selection rule: candidates are split into the six
// strata (tier by has_tests), each sorted by rebuild_tokens then import
// path; slot k of a stratum's rule.Slots(s) slots starts at index
// floor(k*n/slots) of its n candidates and takes the first candidate from
// there on that is not yet taken, whose module has fewer than
// rule.MaxPerModule taken, and that check accepts. Strata are filled in order, slot by slot, so the
// result depends only on the candidates and check's verdicts. It returns
// every verdict in the order they were reached.
func Select(ctx context.Context, cands []Candidate, rule definition.SelectionRule, check checkFunc) ([]Verdict, error) {
	byStratum := groupByStratum(cands)
	perModule := make(map[string]int)
	tried := make(map[string]bool)
	var verdicts []Verdict
	for _, s := range definition.Strata() {
		list := byStratum[s]
		sortByTokens(list)
		slots := rule.Slots(s)
		for k := range slots {
			start := k * len(list) / slots
			mod, err := fillSlot(ctx, list, start, s, tried, perModule, rule.MaxPerModule, check, &verdicts)
			if err != nil {
				return verdicts, err
			}
			if mod != "" {
				perModule[mod]++
			}
		}
	}
	return verdicts, nil
}

// groupByStratum buckets cands by tier and has_tests.
func groupByStratum(cands []Candidate) map[definition.Stratum][]Candidate {
	byStratum := make(map[definition.Stratum][]Candidate)
	for _, c := range cands {
		s := definition.Stratum{Tier: c.Tier, Tested: c.Row.Metrics.HasTests}
		byStratum[s] = append(byStratum[s], c)
	}
	return byStratum
}

// sortByTokens sorts list by rebuild_tokens then import path, in place.
func sortByTokens(list []Candidate) {
	slices.SortFunc(list, func(a, b Candidate) int {
		return cmp.Or(
			cmp.Compare(math.Round(a.Estimate.RebuildTokens), math.Round(b.Estimate.RebuildTokens)),
			strings.Compare(a.Row.Package, b.Row.Package))
	})
}

// fillSlot tries the candidates of list from start on, in order, skipping
// one already tried or whose module is at maxPerModule, and appends a
// verdict to *verdicts for the first it checks. It returns the module of
// the candidate taken, or "" when the slot took none.
func fillSlot(ctx context.Context, list []Candidate, start int, s definition.Stratum, tried map[string]bool,
	perModule map[string]int, maxPerModule int, check checkFunc, verdicts *[]Verdict) (string, error) {
	for i := start; i < len(list); i++ {
		c := list[i]
		if tried[c.Row.Package] || perModule[c.Row.Module] >= maxPerModule {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		tried[c.Row.Package] = true
		exp, err := check(ctx, c)
		v := Verdict{Candidate: c, Stratum: s.String(), OK: err == nil, Experiment: exp}
		if err != nil {
			v.Reason = err.Error()
		}
		*verdicts = append(*verdicts, v)
		if err == nil {
			return c.Row.Module, nil
		}
	}
	return "", nil
}

// NewExperiment fills an experiment from a candidate, a package or a
// tree; the oracle and stub hash come from the checks at the pin.
func NewExperiment(c Candidate, oracle definition.Oracle, stubHash string, turnCap int) definition.Experiment {
	var members []definition.Member
	if len(c.Members) > 0 {
		members = MemberDefs(c.Members)
	}
	// The experiment's estimate reads as a member's would.
	self := MemberDefs([]Candidate{c})[0]
	return definition.Experiment{
		Module: c.Row.Module, Repo: c.Repo, Commit: c.Row.Commit, Package: self.Package, Dir: self.Dir,
		Stub: definition.StubSignatures, StubSHA256: stubHash, Oracle: oracle, TurnCap: turnCap,
		HasTests: self.HasTests, Tier: self.Tier, AgentPasses: self.AgentPasses, RebuildTokens: self.RebuildTokens,
		HumanDays: self.HumanDays, Metrics: self.Metrics, Members: members,
	}
}
