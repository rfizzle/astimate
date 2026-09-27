package report

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// Sort keys accepted by SortRows (SPEC.md section 9, rank --sort).
const (
	SortPasses      = "passes"
	SortDays        = "days"
	SortFanIn       = "fan_in"
	SortTokens      = "tokens"
	SortDuplication = "duplication"
)

// ErrUnknownSortKey is returned by SortRows for a key not in SortKeys.
var ErrUnknownSortKey = errors.New("unknown sort key")

// Row is one package in a module ranking: the rank command's output and the
// rank_packages MCP result (SPEC.md 10.1).
type Row struct {
	// Path is the package directory relative to the module root, in slash
	// form; "." is the module root itself.
	Path string `json:"path"`
	// AgentPasses is score.Rebuild.AgentPassesRounded.
	AgentPasses float64 `json:"agent_passes"`
	// HumanDays is the human estimate rounded to one decimal.
	HumanDays float64 `json:"human_days"`
	// Tier is the tier of the unrounded agent_passes.
	Tier score.Tier `json:"tier"`
	// FanIn is the package's fan_in metric.
	FanIn int `json:"fan_in"`
	// TokensEst is the package's tokens_est metric.
	TokensEst int `json:"tokens_est"`
	// DuplicationPct is the package's duplication_pct metric.
	DuplicationPct float64 `json:"duplication_pct"`
}

// NewRow estimates the rebuild effort of m under p and returns the ranking
// row for the package at path, rounded as Build rounds the report.
func NewRow(path string, m *metrics.RawMetrics, p score.RebuildParams) Row {
	est := score.Estimate(*m, p)
	return Row{
		Path:           path,
		AgentPasses:    est.AgentPassesRounded(),
		HumanDays:      round1(est.HumanDays),
		Tier:           score.TierOf(est.AgentPasses, p.Tiers),
		FanIn:          m.FanIn,
		TokensEst:      m.TokensEst,
		DuplicationPct: m.DuplicationPct,
	}
}

// SortKeys returns the keys SortRows accepts; SortPasses is the default.
func SortKeys() []string {
	return []string{SortPasses, SortDays, SortFanIn, SortTokens, SortDuplication}
}

// SortRows sorts rows in place by key, descending, with Path ascending as the
// tiebreaker so the order is deterministic. An unknown key leaves rows
// unchanged and returns an error wrapping ErrUnknownSortKey.
func SortRows(rows []Row, key string) error {
	var value func(*Row) float64
	switch key {
	case SortPasses:
		value = func(r *Row) float64 { return r.AgentPasses }
	case SortDays:
		value = func(r *Row) float64 { return r.HumanDays }
	case SortFanIn:
		value = func(r *Row) float64 { return float64(r.FanIn) }
	case SortTokens:
		value = func(r *Row) float64 { return float64(r.TokensEst) }
	case SortDuplication:
		value = func(r *Row) float64 { return r.DuplicationPct }
	default:
		return fmt.Errorf("%w %q: want one of %s", ErrUnknownSortKey, key, strings.Join(SortKeys(), ", "))
	}
	slices.SortFunc(rows, func(a, b Row) int {
		if c := cmp.Compare(value(&b), value(&a)); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return nil
}

// WriteRowsJSON writes rows to w as an indented JSON array followed by a
// newline, without HTML escaping; no rows is an empty array, never null.
func WriteRowsJSON(w io.Writer, rows []Row) error {
	if rows == nil {
		rows = []Row{}
	}
	enc := newEncoder(w, "  ")
	if err := enc.Encode(rows); err != nil {
		return fmt.Errorf("writing json ranking: %w", err)
	}
	return nil
}

// rowColumns are the ranking table's column headers.
func rowColumns() []string {
	return []string{"PATH", "PASSES", "DAYS", "TIER", "FAN_IN", "TOKENS", "DUP%"}
}

// numericColumn reports whether column i of the ranking table holds numbers,
// which are right-aligned; PATH and TIER are text.
func numericColumn(i int) bool {
	return i != 0 && i != 3
}

// WriteRowsTable writes rows for a human reader: one header line, then one
// line per row, with numbers right-aligned in their columns.
func WriteRowsTable(w io.Writer, rows []Row) error {
	cols := rowColumns()
	cells := make([][]string, 0, len(rows)+1)
	cells = append(cells, cols)
	for i := range rows {
		r := &rows[i]
		cells = append(cells, []string{
			r.Path,
			strconv.FormatFloat(r.AgentPasses, 'f', 1, 64),
			strconv.FormatFloat(r.HumanDays, 'f', 1, 64),
			string(r.Tier),
			strconv.Itoa(r.FanIn),
			strconv.Itoa(r.TokensEst),
			strconv.FormatFloat(r.DuplicationPct, 'f', 1, 64),
		})
	}
	// Pad numeric cells on the left to their column's width so they
	// right-align; tabwriter then lays out the columns.
	widths := make([]int, len(cols))
	for _, line := range cells {
		for i, c := range line {
			widths[i] = max(widths[i], len(c))
		}
	}
	bw := bufio.NewWriter(w)
	tw := tabwriter.NewWriter(bw, 0, 0, 2, ' ', 0)
	var sb strings.Builder
	for _, line := range cells {
		sb.Reset()
		for i, c := range line {
			if i > 0 {
				sb.WriteByte('\t')
			}
			if numericColumn(i) {
				sb.WriteString(strings.Repeat(" ", widths[i]-len(c)))
			}
			sb.WriteString(c)
		}
		sb.WriteByte('\n')
		_, _ = io.WriteString(tw, sb.String())
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table ranking: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("writing table ranking: %w", err)
	}
	return nil
}
