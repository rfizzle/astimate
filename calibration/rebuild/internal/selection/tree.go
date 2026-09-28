package selection

import (
	"math"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/internal/score"
)

// TreeCandidates returns the trees eligible for selection, sorted by
// import path. A tree is a package of a cloned corpus module (not the
// module root, not a main package) together with every package of the
// module below its directory that is not a main package (mains holds the
// import paths of the main packages). It is eligible when it has
// rule.MinPackages to rule.MaxPackages packages, none of them uses cgo or
// has generated files, and its estimate under p, the members'
// rebuild_tokens (each rounded) summed and passed through the SPEC.md 7.2
// curve once, is at most rule.MaxAgentPasses. It fails when a row's
// recomputed estimate does not round to the agent_passes it carries.
func TreeCandidates(rows []Row, repos map[string]string, mains map[string]bool, p score.RebuildParams,
	rule definition.SelectionRule) ([]Candidate, error) {
	byModule := map[string][]Candidate{}
	for i := range rows {
		r := &rows[i]
		repo, ok := repos[r.Module]
		if !ok || r.Module == definition.StdlibModule || mains[r.Package] {
			continue
		}
		c, err := estimated(r, repo, p)
		if err != nil {
			return nil, err
		}
		byModule[r.Module] = append(byModule[r.Module], c)
	}
	var out []Candidate
	for _, pkgs := range byModule {
		slices.SortFunc(pkgs, func(a, b Candidate) int { return strings.Compare(a.Row.Package, b.Row.Package) })
		for _, root := range pkgs {
			if root.Row.Package == root.Row.Module {
				continue
			}
			if c, ok := treeOf(root, pkgs, p, rule); ok {
				out = append(out, c)
			}
		}
	}
	slices.SortFunc(out, func(a, b Candidate) int { return strings.Compare(a.Row.Package, b.Row.Package) })
	return out, nil
}

// treeOf returns the tree rooted at root's directory from the module's
// packages pkgs, sorted by import path, and whether it is eligible.
func treeOf(root Candidate, pkgs []Candidate, p score.RebuildParams, rule definition.SelectionRule) (Candidate, bool) {
	dir := definition.ModRelDir(root.Row.Module, root.Row.Package)
	var members []Candidate
	for _, c := range pkgs {
		m := &c.Row.Metrics
		if !definition.InTree(dir, definition.ModRelDir(c.Row.Module, c.Row.Package)) {
			continue
		}
		if m.UsesCgo == nil || *m.UsesCgo || m.GeneratedFiles == nil || *m.GeneratedFiles > 0 {
			return Candidate{}, false
		}
		members = append(members, c)
	}
	if len(members) < rule.MinPackages || len(members) > rule.MaxPackages {
		return Candidate{}, false
	}
	slices.SortFunc(members, func(a, b Candidate) int {
		return strings.Compare(definition.ModRelDir(a.Row.Module, a.Row.Package), definition.ModRelDir(b.Row.Module, b.Row.Package))
	})
	defMembers := MemberDefs(members)
	tokens, days := 0, 0.0
	for _, m := range defMembers {
		tokens += m.RebuildTokens
		days += m.HumanDays
	}
	est := SumEstimate(tokens, p)
	if est.AgentPasses > rule.MaxAgentPasses {
		return Candidate{}, false
	}
	row := Row{Module: root.Row.Module, Commit: root.Row.Commit, Package: root.Row.Package,
		Metrics: definition.Aggregate(defMembers), AgentPasses: est.AgentPassesRounded(), HumanDays: math.Round(days*10) / 10}
	return Candidate{Row: row, Repo: root.Repo, Estimate: est, Tier: score.TierOf(est.AgentPasses, p.Tiers), Members: members}, true
}

// SumEstimate is the EstimateSum estimate of a tree whose members'
// rebuild_tokens sum to tokens: the sum passed through the SPEC.md 7.2
// curve once. HumanDays and Terms are left zero.
func SumEstimate(tokens int, p score.RebuildParams) score.Rebuild {
	r := float64(tokens) / p.ContextBudget
	passes := r
	if r > 1 {
		passes = math.Pow(r, p.SuperlinearExponent)
	}
	return score.Rebuild{RebuildTokens: float64(tokens), Ratio: r, AgentPasses: passes}
}

// MemberDefs returns the definition members of a tree's member
// candidates, in order.
func MemberDefs(members []Candidate) []definition.Member {
	out := make([]definition.Member, len(members))
	for i, c := range members {
		out[i] = definition.Member{
			Package:       c.Row.Package,
			Dir:           definition.ModRelDir(c.Row.Module, c.Row.Package),
			HasTests:      c.Row.Metrics.HasTests,
			Tier:          c.Tier,
			AgentPasses:   c.Row.AgentPasses,
			RebuildTokens: int(math.Round(c.Estimate.RebuildTokens)),
			HumanDays:     c.Row.HumanDays,
			Metrics:       definition.Metrics{RawMetrics: c.Row.Metrics},
		}
	}
	return out
}
