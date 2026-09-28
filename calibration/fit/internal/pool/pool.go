// Package pool reads the pooled corpus rows calibration/collect writes and
// fits each gated metric's candidate limits from their distribution
// (SPEC.md 11.1).
package pool

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"github.com/rfizzle/astimate/calibration/fit/internal/stats"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Row is the part of a pooled packages.jsonl line the fitter reads.
type Row struct {
	// Module is the module path, or "std".
	Module string `json:"module"`
	// Package is the import path.
	Package string `json:"package"`
	// Metrics are the package's raw metrics.
	Metrics metrics.RawMetrics `json:"metrics"`
	// FuncCognitive counts the package's functions by cognitive
	// complexity; absent in data collected before the collector recorded
	// it, and for a package with no functions.
	FuncCognitive map[int]int `json:"func_cognitive"`
	// Language is the row's language id; absent on Go rows.
	Language string `json:"language"`
}

// CheckLanguage checks that every row carries the language lang, where
// empty means Go, whose rows carry none: a fit pools one language's rows,
// and a whole configuration is fitted from Go rows alone.
func CheckLanguage(rows []Row, lang string) error {
	if lang == "go" {
		lang = ""
	}
	for i := range rows {
		got := rows[i].Language
		if got == "go" {
			got = ""
		}
		if got != lang {
			if lang == "" {
				return fmt.Errorf("row %d is %s; fit it with --language %s", i+1, got, got)
			}
			if got == "" {
				got = "go"
			}
			return fmt.Errorf("row %d is %s, not %s", i+1, got, lang)
		}
	}
	return nil
}

// readRows decodes packages.jsonl from r.
func readRows(r io.Reader) ([]Row, error) {
	var rows []Row
	dec := json.NewDecoder(bufio.NewReader(r))
	for {
		var row Row
		err := dec.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decoding row %d: %w", len(rows)+1, err)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, errors.New("no rows")
	}
	return rows, nil
}

// LoadRows reads the packages.jsonl file at path.
func LoadRows(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading data: %w", err)
	}
	defer func() { _ = f.Close() }()
	rows, err := readRows(f)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return rows, nil
}

// Modules returns the distinct modules of rows, sorted.
func Modules(rows []Row) []string {
	var mods []string
	for i := range rows {
		if !slices.Contains(mods, rows[i].Module) {
			mods = append(mods, rows[i].Module)
		}
	}
	slices.Sort(mods)
	return mods
}

// StdModule is the Module of standard-library rows.
const StdModule = "std"

// The pools a metric can be fitted from.
const (
	// PoolAll is every pooled row.
	PoolAll = "all rows"
	// PoolCloned is the cloned-module rows only, leaving out std.
	PoolCloned = "cloned-module rows only"
	// PoolFunctions is every function of every pooled row.
	PoolFunctions = "functions of all rows"
	// PoolModule is the module rows (package metrics.ModuleRowID) only.
	PoolModule = "module rows"
)

// FuncMetric is the per-function metric: it is a diff against a baseline,
// so no package row carries it, and it is fitted from the functions of
// every row counted as new, the way a package new at head is judged. Its
// max is fitted at the 99th percentile of the per-function values, not the
// 90th: a single function past the corpus's own worst percentile is the
// signal.
const FuncMetric = "changed_func_cognitive_max"

// poolFor names the rows metric is fitted from. internal_imports is pooled
// from cloned-module rows only: the standard library is loaded as one
// module, so every standard-library import on a std row counts as internal
// (calibration/corpus.md). FuncMetric is pooled over the functions of all
// rows. A module-wide metric (metrics.ModuleWide) is pooled from the module
// rows only, since the gate evaluates it there and nowhere else. Every
// other metric uses all package rows.
func poolFor(metric string) string {
	switch {
	case metric == "internal_imports":
		return PoolCloned
	case metric == FuncMetric:
		return PoolFunctions
	case metrics.ModuleWide(metric):
		return PoolModule
	}
	return PoolAll
}

// inPool reports whether row belongs to pool. A module row belongs to
// PoolModule only, and every package row to every other pool, except the
// standard library's to PoolCloned.
func inPool(row *Row, pool string) bool {
	if IsModuleRow(row) {
		return pool == PoolModule
	}
	switch pool {
	case PoolModule:
		return false
	case PoolCloned:
		return row.Module != StdModule
	}
	return true
}

// IsModuleRow reports whether row is a module-level row.
func IsModuleRow(row *Row) bool {
	return row.Package == metrics.ModuleRowID
}

// Values returns the values of metric over the rows in pool: one per
// row measuring it, or for PoolFunctions one per function, from the rows'
// FuncCognitive counts.
func Values(rows []Row, metric, pool string) []float64 {
	values := make([]float64, 0, len(rows))
	for i := range rows {
		if !inPool(&rows[i], pool) {
			continue
		}
		if pool == PoolFunctions {
			for v, n := range rows[i].FuncCognitive {
				for range n {
					values = append(values, float64(v))
				}
			}
			continue
		}
		if v, ok := rows[i].Metrics.Value(metric); ok {
			values = append(values, v)
		}
	}
	return values
}

// asNew returns row's metrics as a package new at head would carry them:
// for PoolFunctions every function is new, so changed_func_cognitive_max is
// the package's most complex function, or null with no functions.
func asNew(row *Row, pool string) metrics.RawMetrics {
	m := row.Metrics
	if pool == PoolFunctions && len(row.FuncCognitive) > 0 {
		worst := slices.Max(slices.Collect(maps.Keys(row.FuncCognitive)))
		m.ChangedFuncCognitiveMax = &worst
	}
	return m
}

// Choice is one gate rule's pooled distribution, its base values and the
// candidate values fitted from the distribution.
type Choice struct {
	// Rule is the base rule.
	Rule gate.Threshold
	// Pool names the rows the metric was fitted from: PoolAll, PoolCloned
	// or PoolFunctions.
	Pool string
	// Stats is the metric's pooled distribution.
	Stats stats.Stats
	// Max and MaxDelta are the candidate values, nil where the rule has
	// none. A requirement rule keeps both nil.
	Max, MaxDelta *float64
	// DeltaPinned says the base max_delta is 0 and was kept: zero
	// tolerance is a policy, not a statistic (SPEC.md 11.1).
	DeltaPinned bool
	// OverBase and OverCandidate count the pool's rows a package new at
	// head would fail on under the base and the candidate rule: above max
	// for a density or capacity rule (and above max_delta from zero for a
	// density rule with ratchet_from_zero), or not meeting the requirement
	// while its when guard holds. For PoolFunctions they count functions
	// above max.
	OverBase, OverCandidate int
	// Packages, PkgOverBase and PkgOverCandidate are set for
	// PoolFunctions only: the rows with functions, and those of them a
	// package new at head would fail on, holding a function above max.
	Packages, PkgOverBase, PkgOverCandidate int
}

// FitThresholds fits every rule of base to the pooled rows, each metric
// over the rows poolFor names. A capacity rule's max, and a density rule's
// max where the base has one, become the 90th percentile rounded with
// stats.RoundReadable (a capacity max never below one step, since it must
// be positive), except FuncMetric's, which is the 99th percentile of the
// per-function values; a density rule's max_delta, where the base has one,
// becomes stats.DeltaFromIQR, except that a base max_delta of 0 is kept. A
// rule whose metric no row measured, as FuncMetric in data without
// per-function counts, keeps its base values. Kinds, warn_at,
// ratchet_from_zero and requirement rules are kept.
func FitThresholds(rows []Row, base *config.Config) []Choice {
	choices := make([]Choice, 0, len(base.Thresholds))
	for _, rule := range base.Thresholds {
		p := poolFor(rule.Metric)
		values := Values(rows, rule.Metric, p)
		c := Choice{Rule: rule, Pool: p, Stats: stats.ComputeStats(values)}
		pct := stats.IsPercent(rule.Metric)
		at := c.Stats.P90
		if p == PoolFunctions {
			at = c.Stats.P99
		}
		switch {
		case len(values) == 0:
			// Nothing measured: keep the base values.
			c.Max, c.MaxDelta = rule.Max, rule.MaxDelta
		case rule.Kind == gate.Capacity:
			c.Max = ptr(max(stats.RoundReadable(at, pct), stats.MinStep(pct)))
		case rule.Kind == gate.Density:
			if rule.Max != nil {
				c.Max = ptr(stats.RoundReadable(at, pct))
			}
			switch {
			case rule.MaxDelta != nil && *rule.MaxDelta == 0:
				c.MaxDelta, c.DeltaPinned = ptr(0.0), true
			case rule.MaxDelta != nil:
				c.MaxDelta = ptr(stats.DeltaFromIQR(c.Stats.IQR, pct))
			}
		}
		// A requirement rule has neither, so its candidate is the base.
		cand := rule
		cand.Max, cand.MaxDelta = c.Max, c.MaxDelta
		if p == PoolFunctions {
			c.OverBase, c.OverCandidate = above(values, rule.Max), above(values, cand.Max)
		}
		for i := range rows {
			if !inPool(&rows[i], p) {
				continue
			}
			if p == PoolFunctions && len(rows[i].FuncCognitive) == 0 {
				continue
			}
			m := asNew(&rows[i], p)
			overBase, overCand := failsNew(m, rule), failsNew(m, cand)
			if p == PoolFunctions {
				c.Packages++
				c.PkgOverBase += b2i(overBase)
				c.PkgOverCandidate += b2i(overCand)
				continue
			}
			c.OverBase += b2i(overBase)
			c.OverCandidate += b2i(overCand)
		}
		choices = append(choices, c)
	}
	return choices
}

// above counts the values greater than limit; none when limit is nil.
func above(values []float64, limit *float64) int {
	n := 0
	if limit == nil {
		return n
	}
	for _, v := range values {
		if v > *limit {
			n++
		}
	}
	return n
}

// b2i is 1 for true and 0 for false.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CrossPkgStats is the distribution of dup_blocks_cross_pkg over the
// module rows (package metrics.ModuleRowID) of rows; empty when there are
// none.
func CrossPkgStats(rows []Row) stats.Stats {
	return stats.ComputeStats(Values(rows, "dup_blocks_cross_pkg", PoolModule))
}

// CountModuleRows returns how many of rows are module rows.
func CountModuleRows(rows []Row) int {
	n := 0
	for i := range rows {
		n += b2i(IsModuleRow(&rows[i]))
	}
	return n
}

// failsNew reports whether m, as a package new at head, violates rule.
func failsNew(m metrics.RawMetrics, rule gate.Threshold) bool {
	return len(gate.Evaluate(m, nil, []gate.Threshold{rule}, nil).Violations) > 0
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
