// Package ranking builds rank's rows (SPEC.md section 9, rank; 10.1,
// rank_packages): each extracted package's rebuild estimate and its room
// under the capacity rules that judge it (SPEC.md 8.1, Headroom), sorted
// and truncated. It does no I/O; engine.Rank extracts the packages once and
// hands their metrics here.
package ranking

import (
	"github.com/rfizzle/astimate/internal/engine/internal/judge"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report/rows"
	"github.com/rfizzle/astimate/internal/score"
)

// Input is what Rows ranks.
type Input struct {
	// ModulePath is the module's path, which names each row by its
	// directory relative to the module root (judge.Rel).
	ModulePath string
	// Packages are the extracted packages' metrics by identifier.
	Packages map[string]*metrics.RawMetrics
	// Order lists the identifiers of Packages to rank.
	Order []string
	// Rebuild estimates each row.
	Rebuild score.RebuildParams
	// Rules are the gate rules of the module's language, as check resolves
	// them; their capacity rules give each row its ceilings.
	Rules []gate.Threshold
	// Sort is the sort key, one of rows.SortKeys.
	Sort string
	// Top keeps only the first Top rows; 0 or less keeps every row.
	Top int
}

// Rows returns one row per package of in.Order, estimated under in.Rebuild
// and carrying the ceilings of the capacity rules of in.Rules that judge
// it on a package row (gate.ForRow, gate.Ceilings), sorted by in.Sort and
// truncated to in.Top. An unknown sort key returns no rows and an error
// wrapping rows.ErrUnknownSortKey.
func Rows(in *Input) ([]rows.Row, error) {
	rules := gate.ForRow(in.Rules, gate.PackageRow)
	ranked := make([]rows.Row, 0, len(in.Order))
	for _, pkg := range in.Order {
		m := in.Packages[pkg]
		r := rows.NewRow(judge.Rel(in.ModulePath, pkg), m, in.Rebuild)
		r.SetCeilings(ceilings(m, rules))
		ranked = append(ranked, r)
	}
	if err := rows.SortRows(ranked, in.Sort); err != nil {
		return nil, err
	}
	if in.Top > 0 && in.Top < len(ranked) {
		ranked = ranked[:in.Top]
	}
	return ranked, nil
}

// ceilings returns the room m has under each capacity rule of rules that
// judges it, in rule order; nil when none does.
func ceilings(m *metrics.RawMetrics, rules []gate.Threshold) []rows.Ceiling {
	cs := gate.Ceilings(*m, rules)
	if len(cs) == 0 {
		return nil
	}
	out := make([]rows.Ceiling, 0, len(cs))
	for _, c := range cs {
		out = append(out, rows.NewCeiling(c.Metric, c.Value, c.Max))
	}
	return out
}
