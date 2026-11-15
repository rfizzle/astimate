package rows

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
	SortChurn       = "churn"
	SortHotspot     = "hotspot"
	SortHeadroom    = "headroom"
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
	// HumanDays is the human estimate under the configured model, rounded
	// to one decimal.
	HumanDays float64 `json:"human_days"`
	// HumanDaysLow and HumanDaysHigh are the lower and the higher of the
	// two models' estimates, each rounded to one decimal.
	HumanDaysLow  float64 `json:"human_days_low"`
	HumanDaysHigh float64 `json:"human_days_high"`
	// Tier is the tier of the unrounded agent_passes.
	Tier score.Tier `json:"tier"`
	// FanIn is the package's fan_in metric.
	FanIn int `json:"fan_in"`
	// TokensEst is the package's tokens_est metric.
	TokensEst int `json:"tokens_est"`
	// DuplicationPct is the package's duplication_pct metric.
	DuplicationPct float64 `json:"duplication_pct"`
	// ChurnCommits is churn_commits, nil when not measured.
	ChurnCommits *int `json:"churn_commits"`
	// Role is the package's role (metrics.RawMetrics.Role, SPEC.md 6.5);
	// omitted when the extractor records none.
	Role string `json:"role,omitempty"`
	// Headroom is the tightest of Ceilings, the one with the highest Used
	// (the first on ties); nil when no capacity rule judges the package.
	// SetCeilings sets it.
	Headroom *Ceiling `json:"headroom"`
	// Ceilings are the capacity rules that judge the package, in the
	// configuration's rule order; omitted when none does. SetCeilings
	// sets them.
	Ceilings []Ceiling `json:"ceilings,omitempty"`
}

// Ceiling is one capacity rule's ceiling on a package and the room left
// under it (SPEC.md 8.1, Headroom).
type Ceiling struct {
	// Metric is the rule's metric.
	Metric string `json:"metric"`
	// Value is the package's value of the metric.
	Value float64 `json:"value"`
	// Max is the rule's ceiling.
	Max float64 `json:"max"`
	// Remaining is Max less Value, negative when the package is over the
	// ceiling, rounded to three decimals.
	Remaining float64 `json:"remaining"`
	// Used is Value over Max, the share of the ceiling used, above 1 when
	// the package is over it, rounded to three decimals.
	Used float64 `json:"used"`
}

// NewCeiling returns the ceiling limit of metric on a package whose value
// of it is value; limit must be greater than zero.
func NewCeiling(metric string, value, limit float64) Ceiling {
	return Ceiling{
		Metric:    metric,
		Value:     value,
		Max:       limit,
		Remaining: round3(limit - value),
		Used:      round3(value / limit),
	}
}

// round3 rounds v to three decimals.
func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// SetCeilings sets r's Ceilings to cs, which r keeps, and its Headroom to
// a copy of the one with the highest Used, the first of those on ties, or
// nil when cs is empty.
func (r *Row) SetCeilings(cs []Ceiling) {
	r.Ceilings, r.Headroom = cs, nil
	for i := range cs {
		if r.Headroom == nil || cs[i].Used > r.Headroom.Used {
			c := cs[i]
			r.Headroom = &c
		}
	}
}

// NewRow estimates the rebuild effort of m under p and returns the ranking
// row for the package at path, rounded as Build rounds the report.
func NewRow(path string, m *metrics.RawMetrics, p score.RebuildParams) Row {
	est := score.Estimate(*m, p)
	return Row{
		Path:           path,
		AgentPasses:    est.AgentPassesRounded(),
		HumanDays:      math.Round(est.HumanDays*10) / 10,
		HumanDaysLow:   math.Round(est.HumanDaysLow*10) / 10,
		HumanDaysHigh:  math.Round(est.HumanDaysHigh*10) / 10,
		Tier:           score.TierOf(est.AgentPasses, p.Tiers),
		FanIn:          m.FanIn,
		TokensEst:      m.TokensEst,
		DuplicationPct: m.DuplicationPct,
		ChurnCommits:   m.ChurnCommits,
		Role:           m.Role,
	}
}

// SortKeys returns the keys SortRows accepts; SortPasses is the default.
func SortKeys() []string {
	return []string{SortPasses, SortDays, SortFanIn, SortTokens, SortDuplication, SortChurn, SortHotspot, SortHeadroom}
}

// Hotspot returns churn_commits times agent_passes, to one decimal: how
// much of the measured rebuild effort sits in code that keeps changing. It
// is 0 when churn was not measured; the report's "Start here" order and
// SortHotspot rank by it, then by human_days_low.
func Hotspot(churn *int, passes float64) float64 {
	if churn == nil {
		return 0
	}
	return math.Round(float64(*churn)*passes*10) / 10
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
	case SortChurn:
		// Unmeasured churn sorts last.
		value = func(r *Row) float64 {
			if r.ChurnCommits == nil {
				return -1
			}
			return float64(*r.ChurnCommits)
		}
	case SortHotspot:
		value = func(r *Row) float64 { return Hotspot(r.ChurnCommits, r.AgentPasses) }
	case SortHeadroom:
		// The tightest ceiling first, packages over one at the top; a
		// package no capacity rule judges sorts last.
		value = func(r *Row) float64 {
			if r.Headroom == nil {
				return -1
			}
			return r.Headroom.Used
		}
	default:
		return fmt.Errorf("%w %q: want one of %s", ErrUnknownSortKey, key, strings.Join(SortKeys(), ", "))
	}
	slices.SortFunc(rows, func(a, b Row) int {
		if c := cmp.Compare(value(&b), value(&a)); c != 0 {
			return c
		}
		if key == SortHotspot {
			// A package without churn, or in no pass, has no hotspot
			// score: the human time's low end orders those, so the order
			// is total.
			if c := cmp.Compare(b.HumanDaysLow, a.HumanDaysLow); c != 0 {
				return c
			}
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
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		return fmt.Errorf("writing json ranking: %w", err)
	}
	return nil
}

// TableOptions shape WriteRowsTable's columns.
type TableOptions struct {
	// Headroom adds the HEADROOM column, the package's tightest capacity
	// ceiling (headroomText); rank shows it when sorting by SortHeadroom.
	Headroom bool
}

// rowColumns are the ranking table's column headers: the measured agent
// estimate and its tier before the textbook DAYS, and HEADROOM last when
// opts asks for it.
func rowColumns(opts TableOptions) []string {
	cols := []string{"PATH", "PASSES", "TIER", "DAYS", "FAN_IN", "TOKENS", "DUP%", "CHURN", "ROLE"}
	if opts.Headroom {
		cols = append(cols, "HEADROOM")
	}
	return cols
}

// numericColumn reports whether column i of the ranking table holds numbers,
// which are right-aligned; PATH, TIER, ROLE and HEADROOM are text.
func numericColumn(i int) bool {
	return i != 0 && i != 2 && i < 8
}

// WriteRowsTable writes rows for a human reader: one header line, then one
// line per row, with numbers right-aligned in their columns, and the
// HEADROOM column last when opts asks for it.
func WriteRowsTable(w io.Writer, rows []Row, opts TableOptions) error {
	cols := rowColumns(opts)
	cells := make([][]string, 0, len(rows)+1)
	cells = append(cells, cols)
	for i := range rows {
		r := &rows[i]
		line := make([]string, 0, len(cols))
		line = append(line,
			r.Path,
			strconv.FormatFloat(r.AgentPasses, 'f', 1, 64),
			string(r.Tier),
			daysText(r),
			strconv.Itoa(r.FanIn),
			strconv.Itoa(r.TokensEst),
			strconv.FormatFloat(r.DuplicationPct, 'f', 1, 64),
			churnText(r.ChurnCommits),
			cmp.Or(r.Role, "-"),
		)
		if opts.Headroom {
			line = append(line, headroomText(r.Headroom))
		}
		cells = append(cells, line)
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

// headroomText is the HEADROOM cell, "tokens_est 14,100/16,000 (1,900
// left)", "(300 over)" for a package over the ceiling, and "-" for one no
// capacity rule judges.
func headroomText(c *Ceiling) string {
	if c == nil {
		return "-"
	}
	room := grouped(c.Remaining) + " left"
	if c.Remaining < 0 {
		room = grouped(-c.Remaining) + " over"
	}
	return c.Metric + " " + grouped(c.Value) + "/" + grouped(c.Max) + " (" + room + ")"
}

// grouped writes v without trailing zeros, its integer part in groups of
// three digits separated by commas.
func grouped(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	b.Grow(len(s) + len(s)/3 + 1)
	b.WriteString(sign)
	for i := range len(whole) {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(whole[i])
	}
	if hasFrac {
		b.WriteByte('.')
		b.WriteString(frac)
	}
	return b.String()
}

// daysText is the DAYS cell, the human range to one decimal, "3.1-4.2",
// once when both ends round the same.
func daysText(r *Row) string {
	ends := []string{strconv.FormatFloat(r.HumanDaysLow, 'f', 1, 64), strconv.FormatFloat(r.HumanDaysHigh, 'f', 1, 64)}
	return strings.Join(slices.Compact(ends), "-")
}

// churnText is the CHURN cell, "-" when not measured.
func churnText(n *int) string {
	if n == nil {
		return "-"
	}
	return strconv.Itoa(*n)
}
