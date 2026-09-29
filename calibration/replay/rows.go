package main

import (
	"errors"

	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// packageRow is one checked row of one commit: a line of packages.jsonl.
// It carries the head and baseline metrics in full, so any rule can be
// re-evaluated at any threshold without replaying again.
type packageRow struct {
	// Commit is the full hash of the commit checked.
	Commit string `json:"commit"`
	// Package is the module-relative package directory, "." for the root
	// package, or metrics.ModuleRowID for the module row.
	Package string `json:"package"`
	// Language is the extractor's language id.
	Language string `json:"language"`
	// New is true when the baseline has no such row: the package is new at
	// the commit, or the commit is judged against an empty baseline.
	New bool `json:"new"`
	// Passed is the row's gate verdict under the replay's configuration.
	Passed bool `json:"passed"`
	// Violations, Warnings and Exemptions are the row's findings.
	Violations []finding  `json:"violations"`
	Warnings   []finding  `json:"warnings"`
	Exemptions []exempted `json:"exemptions"`
	// Metrics are the row's metrics at the commit.
	Metrics metrics.RawMetrics `json:"metrics"`
	// Base are the row's metrics at the first parent; null when New.
	Base *metrics.RawMetrics `json:"base"`
	// SLOCDelta is sloc at the commit minus sloc at the parent, the parent
	// counting 0 when New: the size of the change for a size-only baseline.
	SLOCDelta int `json:"sloc_delta"`
}

// finding is a gate finding without its suggestion text.
type finding struct {
	Metric   string           `json:"metric"`
	Base     *float64         `json:"base"`
	Head     float64          `json:"head"`
	Limit    string           `json:"limit"`
	Location *report.Location `json:"location"`
	// Severity is "warn" on the breach of a warn rule, which the check
	// reports among the warnings; omitted on every other finding.
	Severity string `json:"severity,omitempty"`
}

// exempted is a violation an exemption silenced, with its reason.
type exempted struct {
	finding
	Reason string `json:"reason"`
}

// commitRow is one replayed commit: a line of commits.jsonl. It is written
// after the commit's package rows, so its presence marks the commit done.
type commitRow struct {
	Commit        string              `json:"commit"`
	Parent        string              `json:"parent"`
	AuthorName    string              `json:"author_name"`
	AuthorEmail   string              `json:"author_email"`
	CommitterDate string              `json:"committer_date"`
	Subject       string              `json:"subject"`
	CoAuthoredBy  []string            `json:"co_authored_by"`
	Trailers      map[string][]string `json:"trailers"`
	FilesChanged  []string            `json:"files_changed"`
	// ConfigVersion is the config_version the commit was gated under.
	ConfigVersion string `json:"config_version"`
	// Baseline is how the commit was judged: "parent", against its first
	// parent; "empty", every package new, for a root commit or a parent
	// without the module.
	Baseline string `json:"baseline"`
	// Loaded is false when the module did not load or the check failed;
	// Error then says why and the commit has no package rows.
	Loaded bool   `json:"loaded"`
	Error  string `json:"error,omitempty"`
	// PackagesChanged are the packages the check selected, module-relative
	// in check order; PackagesDeleted the directories that lost their
	// package; PackagesFailed the rows that failed to extract.
	PackagesChanged []string        `json:"packages_changed"`
	PackagesDeleted []string        `json:"packages_deleted"`
	PackagesFailed  []packageFailed `json:"packages_failed"`
	// Passed is the commit's gate verdict: no row has a violation. Null
	// when not Loaded.
	Passed *bool `json:"passed"`
	// Violations counts the violations over every row.
	Violations int `json:"violations"`
	// WallMS is the wall time of the commit's replay, checkout included.
	WallMS int64 `json:"wall_ms"`
}

// packageFailed is a row that failed to extract.
type packageFailed struct {
	Package string `json:"package"`
	Error   string `json:"error"`
}

// findings copies fs without suggestions; an empty list, never null.
func findings(fs []report.Finding) []finding {
	out := make([]finding, len(fs))
	for i := range fs {
		out[i] = toFinding(&fs[i])
	}
	return out
}

// toFinding copies f without its suggestion.
func toFinding(f *report.Finding) finding {
	return finding{Metric: f.Metric, Base: f.Base, Head: f.Head, Limit: f.Limit, Location: f.Location, Severity: f.Severity}
}

// newPackageRow returns the row of r, a report of a check of commit.
func newPackageRow(commit string, r *report.Report) packageRow {
	row := packageRow{
		Commit:     commit,
		Package:    r.PackagePath,
		Language:   r.Language,
		New:        r.Baseline == nil,
		Passed:     r.Passed == nil || *r.Passed,
		Violations: findings(r.Violations),
		Warnings:   findings(r.Warnings),
		Exemptions: make([]exempted, 0, len(r.Exemptions)),
		Metrics:    r.Metrics,
		SLOCDelta:  r.Metrics.SLOC,
	}
	for _, e := range r.Exemptions {
		row.Exemptions = append(row.Exemptions, exempted{finding: toFinding(&e.Finding), Reason: e.Reason})
	}
	if r.Baseline != nil {
		base := r.Baseline.Metrics
		row.Base = &base
		row.SLOCDelta -= base.SLOC
	}
	return row
}

// checkRows returns the rows of c, a check of commit, module row first,
// and fills cr's package lists and verdict from it and from failed.
func checkRows(commit string, c *report.Check, failed []error, cr *commitRow) []packageRow {
	rows := make([]packageRow, 0, len(c.Packages)+1)
	if c.Module != nil {
		row := newPackageRow(commit, &c.Module.Report)
		if cr.Baseline == baselineEmpty {
			// The empty baseline's zero module row only lets the module-wide
			// rules run against zero; the row is as new as every package.
			row.New, row.Base, row.SLOCDelta = true, nil, row.Metrics.SLOC
		}
		rows = append(rows, row)
	}
	for i := range c.Packages {
		cr.PackagesChanged = append(cr.PackagesChanged, c.Packages[i].Report.PackagePath)
		rows = append(rows, newPackageRow(commit, &c.Packages[i].Report))
	}
	cr.PackagesDeleted = append(cr.PackagesDeleted, c.Deleted...)
	for _, err := range failed {
		pf := packageFailed{Error: err.Error()}
		var pe *engine.PackageError
		if errors.As(err, &pe) {
			pf = packageFailed{Package: pe.Path, Error: pe.Err.Error()}
		}
		cr.PackagesFailed = append(cr.PackagesFailed, pf)
	}
	passed := true
	for i := range rows {
		cr.Violations += len(rows[i].Violations)
		passed = passed && rows[i].Passed
	}
	cr.Passed = &passed
	return rows
}
