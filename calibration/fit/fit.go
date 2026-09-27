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

// Choice is one gate rule's pooled distribution, its base values and the
// candidate values fitted from the distribution.
type Choice struct {
	// Rule is the base rule.
	Rule gate.Threshold
	// Stats is the metric's pooled distribution.
	Stats Stats
	// Max and MaxDelta are the candidate values, nil where the rule has
	// none. A requirement rule keeps both nil.
	Max, MaxDelta *float64
	// OverBase and OverCandidate count rows a package new at head would
	// fail on under the base and the candidate rule: above max for a
	// density or capacity rule (and above max_delta from zero for a density
	// rule with ratchet_from_zero), or not meeting the requirement while
	// its when guard holds.
	OverBase, OverCandidate int
}

// fitThresholds fits every rule of base to the pooled rows. A capacity
// rule's max, and a density rule's max where the base has one, become the
// 90th percentile rounded with roundReadable (a capacity max never below
// one step, since it must be positive); a density rule's max_delta becomes
// deltaFromIQR. Kinds, warn_at, ratchet_from_zero and requirement rules
// are kept.
func fitThresholds(rows []Row, base *config.Config) []Choice {
	choices := make([]Choice, 0, len(base.Thresholds))
	for _, rule := range base.Thresholds {
		values := make([]float64, 0, len(rows))
		for i := range rows {
			if v, ok := rows[i].Metrics.Value(rule.Metric); ok {
				values = append(values, v)
			}
		}
		c := Choice{Rule: rule, Stats: computeStats(values)}
		pct := isPercent(rule.Metric)
		switch rule.Kind {
		case gate.Capacity:
			c.Max = ptr(max(roundReadable(c.Stats.P90, pct), minStep(pct)))
		case gate.Density:
			if rule.Max != nil {
				c.Max = ptr(roundReadable(c.Stats.P90, pct))
			}
			c.MaxDelta = ptr(deltaFromIQR(c.Stats.IQR, pct))
		}
		// A requirement rule has neither, so its candidate is the base.
		cand := rule
		cand.Max, cand.MaxDelta = c.Max, c.MaxDelta
		for i := range rows {
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

// failsNew reports whether m, as a package new at head, violates rule.
func failsNew(m metrics.RawMetrics, rule gate.Threshold) bool {
	return len(gate.Evaluate(m, nil, []gate.Threshold{rule}, nil).Violations) > 0
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
