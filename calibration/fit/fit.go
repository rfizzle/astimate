package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

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

// loadRows reads the packages.jsonl file at path.
func loadRows(path string) ([]Row, error) {
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

// modules returns the distinct modules of rows, sorted.
func modules(rows []Row) []string {
	var mods []string
	for i := range rows {
		if !slices.Contains(mods, rows[i].Module) {
			mods = append(mods, rows[i].Module)
		}
	}
	slices.Sort(mods)
	return mods
}

// stdModule is the Module of standard-library rows.
const stdModule = "std"

// The pools a metric can be fitted from.
const (
	// poolAll is every pooled row.
	poolAll = "all rows"
	// poolCloned is the cloned-module rows only, leaving out std.
	poolCloned = "cloned-module rows only"
	// poolFunctions is every function of every pooled row.
	poolFunctions = "functions of all rows"
)

// funcMetric is the per-function metric: it is a diff against a baseline,
// so no package row carries it, and it is fitted from the functions of
// every row counted as new, the way a package new at head is judged. Its
// max is fitted at the 99th percentile of the per-function values, not the
// 90th: a single function past the corpus's own worst percentile is the
// signal.
const funcMetric = "changed_func_cognitive_max"

// poolFor names the rows metric is fitted from. internal_imports is pooled
// from cloned-module rows only: the standard library is loaded as one
// module, so every standard-library import on a std row counts as internal
// (calibration/corpus.md). funcMetric is pooled over the functions of all
// rows. Every other metric uses all rows.
func poolFor(metric string) string {
	switch metric {
	case "internal_imports":
		return poolCloned
	case funcMetric:
		return poolFunctions
	}
	return poolAll
}

// inPool reports whether row belongs to pool.
func inPool(row *Row, pool string) bool {
	return pool != poolCloned || row.Module != stdModule
}

// poolValues returns the values of metric over the rows in pool: one per
// row measuring it, or for poolFunctions one per function, from the rows'
// FuncCognitive counts.
func poolValues(rows []Row, metric, pool string) []float64 {
	values := make([]float64, 0, len(rows))
	for i := range rows {
		if !inPool(&rows[i], pool) {
			continue
		}
		if pool == poolFunctions {
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
// for poolFunctions every function is new, so changed_func_cognitive_max is
// the package's most complex function, or null with no functions.
func asNew(row *Row, pool string) metrics.RawMetrics {
	m := row.Metrics
	if pool == poolFunctions && len(row.FuncCognitive) > 0 {
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
	// Pool names the rows the metric was fitted from: poolAll, poolCloned
	// or poolFunctions.
	Pool string
	// Stats is the metric's pooled distribution.
	Stats Stats
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
	// while its when guard holds. For poolFunctions they count functions
	// above max.
	OverBase, OverCandidate int
	// Packages, PkgOverBase and PkgOverCandidate are set for
	// poolFunctions only: the rows with functions, and those of them a
	// package new at head would fail on, holding a function above max.
	Packages, PkgOverBase, PkgOverCandidate int
}

// fitThresholds fits every rule of base to the pooled rows, each metric
// over the rows poolFor names. A capacity rule's max, and a density rule's
// max where the base has one, become the 90th percentile rounded with
// roundReadable (a capacity max never below one step, since it must be
// positive), except funcMetric's, which is the 99th percentile of the
// per-function values; a density rule's max_delta, where the base has one,
// becomes deltaFromIQR, except that a base max_delta of 0 is kept. A rule whose metric no row measured, as
// funcMetric in data without per-function counts, keeps its base values.
// Kinds, warn_at, ratchet_from_zero and requirement rules are kept.
func fitThresholds(rows []Row, base *config.Config) []Choice {
	choices := make([]Choice, 0, len(base.Thresholds))
	for _, rule := range base.Thresholds {
		pool := poolFor(rule.Metric)
		values := poolValues(rows, rule.Metric, pool)
		c := Choice{Rule: rule, Pool: pool, Stats: computeStats(values)}
		pct := isPercent(rule.Metric)
		at := c.Stats.P90
		if pool == poolFunctions {
			at = c.Stats.P99
		}
		switch {
		case len(values) == 0:
			// Nothing measured: keep the base values.
			c.Max, c.MaxDelta = rule.Max, rule.MaxDelta
		case rule.Kind == gate.Capacity:
			c.Max = ptr(max(roundReadable(at, pct), minStep(pct)))
		case rule.Kind == gate.Density:
			if rule.Max != nil {
				c.Max = ptr(roundReadable(at, pct))
			}
			switch {
			case rule.MaxDelta != nil && *rule.MaxDelta == 0:
				c.MaxDelta, c.DeltaPinned = ptr(0.0), true
			case rule.MaxDelta != nil:
				c.MaxDelta = ptr(deltaFromIQR(c.Stats.IQR, pct))
			}
		}
		// A requirement rule has neither, so its candidate is the base.
		cand := rule
		cand.Max, cand.MaxDelta = c.Max, c.MaxDelta
		if pool == poolFunctions {
			c.OverBase, c.OverCandidate = above(values, rule.Max), above(values, cand.Max)
		}
		for i := range rows {
			if !inPool(&rows[i], pool) {
				continue
			}
			if pool == poolFunctions && len(rows[i].FuncCognitive) == 0 {
				continue
			}
			m := asNew(&rows[i], pool)
			overBase, overCand := failsNew(m, rule), failsNew(m, cand)
			if pool == poolFunctions {
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

// crossPkgStats is the distribution of dup_blocks_cross_pkg over the
// module rows (package metrics.ModuleRowID) of rows; empty when there are
// none.
func crossPkgStats(rows []Row) Stats {
	var values []float64
	for i := range rows {
		if rows[i].Package != metrics.ModuleRowID {
			continue
		}
		if v, ok := rows[i].Metrics.Value("dup_blocks_cross_pkg"); ok {
			values = append(values, v)
		}
	}
	return computeStats(values)
}

// failsNew reports whether m, as a package new at head, violates rule.
func failsNew(m metrics.RawMetrics, rule gate.Threshold) bool {
	return len(gate.Evaluate(m, nil, []gate.Threshold{rule}, nil).Violations) > 0
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
