package replay

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
	// BaselineFrom are the module-relative directories of the parent's
	// packages the row's baseline was seeded from because its files were
	// renamed from them (report.Report.BaselineFrom); omitted when the row
	// has a baseline row of its own or none. Such a row is not New, so
	// counting the rows that carry it counts the rows a rename kept from
	// reading as new.
	BaselineFrom []string `json:"baseline_from,omitempty"`
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
	// ChangedFunctions are the row's functions added or modified since the
	// parent with cognitive complexity over changedFuncFloor, each with
	// its complexity at the parent, in head order, so
	// changed_func_cognitive_max can be re-evaluated per function at any
	// max above the floor and any over_max_delta (SPEC.md 8.1). Empty when
	// none is over the floor; null when the row had no function-level diff
	// (changed_func_cognitive_max is null), as on the module row, and on
	// rows replayed before the field existed.
	ChangedFunctions []changedFunc `json:"changed_functions"`
}

// changedFuncFloor is the cognitive complexity a changed function must
// exceed to be recorded in a row's ChangedFunctions. It is under every
// max a sweep of changed_func_cognitive_max tries (half of the shipped 50
// at the least), and a function at or under a max is never judged, so the
// rows re-evaluate the rule exactly.
const changedFuncFloor = 20

// changedFunc is one changed function of a row.
type changedFunc struct {
	// Receiver is the method's receiver base type; omitted for a function.
	Receiver string `json:"receiver,omitempty"`
	Name     string `json:"name"`
	// File is relative to the package directory and Line its declaration.
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// Cognitive is the complexity at the commit, Base at the parent: that
	// of the parent's function with the same receiver and name; null for
	// a new or renamed function.
	Cognitive int  `json:"cognitive"`
	Base      *int `json:"base"`
}

// changedFuncs returns the functions of fns over changedFuncFloor as row
// entries, an empty list when none is; nil when listed is false.
func changedFuncs(fns []metrics.ChangedFunction, listed bool) []changedFunc {
	if !listed {
		return nil
	}
	out := []changedFunc{}
	for i := range fns {
		f := &fns[i]
		if f.Cognitive <= changedFuncFloor {
			continue
		}
		c := changedFunc{Receiver: f.Receiver, Name: f.Name, File: f.File, Line: f.Line, Cognitive: f.Cognitive}
		if f.HasBase {
			b := f.BaseCognitive
			c.Base = &b
		}
		out = append(out, c)
	}
	return out
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
	// Also lists the metrics of the rules whose findings of the same cause
	// the gate merged into this one (SPEC.md 8.1), so a merged rule's
	// firing can still be counted; omitted when nothing was merged.
	Also []string `json:"also,omitempty"`
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
	return withAlso(finding{Metric: f.Metric, Base: f.Base, Head: f.Head, Limit: f.Limit, Location: f.Location, Severity: f.Severity}, f.Also)
}

// withAlso returns g with also as its merged rules.
func withAlso(g finding, also []string) finding {
	g.Also = also
	return g
}

// newPackageRow returns the row of p, a row of a check of commit, with
// its changed functions over changedFuncFloor.
func newPackageRow(commit string, p *report.CheckedPackage) packageRow {
	r := &p.Report
	row := packageRow{
		Commit:       commit,
		Package:      r.PackagePath,
		Language:     r.Language,
		New:          r.Baseline == nil,
		BaselineFrom: r.BaselineFrom,
		Passed:       r.Passed == nil || *r.Passed,
		Violations:   findings(r.Violations),
		Warnings:     findings(r.Warnings),
		Exemptions:   make([]exempted, 0, len(r.Exemptions)),
		Metrics:      r.Metrics,
		SLOCDelta:    r.Metrics.SLOC,
		// The metric is non-null exactly when the check diffed the row's
		// functions.
		ChangedFunctions: changedFuncs(p.ChangedFunctions, r.Metrics.ChangedFuncCognitiveMax != nil),
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
		row := newPackageRow(commit, c.Module)
		if cr.Baseline == baselineEmpty {
			// The empty baseline's zero module row only lets the module-wide
			// rules run against zero; the row is as new as every package.
			row.New, row.Base, row.SLOCDelta = true, nil, row.Metrics.SLOC
		}
		rows = append(rows, row)
	}
	for i := range c.Packages {
		cr.PackagesChanged = append(cr.PackagesChanged, c.Packages[i].Report.PackagePath)
		rows = append(rows, newPackageRow(commit, &c.Packages[i]))
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
