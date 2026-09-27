package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
)

// poolFor names the rows metric is fitted from. internal_imports is pooled
// from cloned-module rows only: the standard library is loaded as one
// module, so every standard-library import on a std row counts as internal
// (calibration/corpus.md). Every other metric uses all rows.
func poolFor(metric string) string {
	if metric == "internal_imports" {
		return poolCloned
	}
	return poolAll
}

// inPool reports whether row belongs to pool.
func inPool(row *Row, pool string) bool {
	return pool == poolAll || row.Module != stdModule
}

// Choice is one gate rule's pooled distribution, its base values and the
// candidate values fitted from the distribution.
type Choice struct {
	// Rule is the base rule.
	Rule gate.Threshold
	// Pool names the rows the metric was fitted from: poolAll or
	// poolCloned.
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
	// while its when guard holds.
	OverBase, OverCandidate int
}

// fitThresholds fits every rule of base to the pooled rows, each metric
// over the rows poolFor names. A capacity rule's max, and a density rule's
// max where the base has one, become the 90th percentile rounded with
// roundReadable (a capacity max never below one step, since it must be
// positive); a density rule's max_delta, where the base has one, becomes
// deltaFromIQR, except that a base max_delta of 0 is kept. A rule whose
// metric no row measured keeps its base values. Kinds, warn_at,
// ratchet_from_zero and requirement rules are kept.
func fitThresholds(rows []Row, base *config.Config) []Choice {
	choices := make([]Choice, 0, len(base.Thresholds))
	for _, rule := range base.Thresholds {
		pool := poolFor(rule.Metric)
		values := make([]float64, 0, len(rows))
		for i := range rows {
			if !inPool(&rows[i], pool) {
				continue
			}
			if v, ok := rows[i].Metrics.Value(rule.Metric); ok {
				values = append(values, v)
			}
		}
		c := Choice{Rule: rule, Pool: pool, Stats: computeStats(values)}
		pct := isPercent(rule.Metric)
		switch {
		case len(values) == 0:
			// Nothing measured, as for changed_func_cognitive_max, which
			// needs a baseline diff: keep the base values.
			c.Max, c.MaxDelta = rule.Max, rule.MaxDelta
		case rule.Kind == gate.Capacity:
			c.Max = ptr(max(roundReadable(c.Stats.P90, pct), minStep(pct)))
		case rule.Kind == gate.Density:
			if rule.Max != nil {
				c.Max = ptr(roundReadable(c.Stats.P90, pct))
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
		for i := range rows {
			if !inPool(&rows[i], pool) {
				continue
			}
			if failsNew(rows[i].Metrics, rule) {
				c.OverBase++
			}
			if failsNew(rows[i].Metrics, cand) {
				c.OverCandidate++
			}
		}
		choices = append(choices, c)
	}
	return choices
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
