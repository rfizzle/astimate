// Package blocklog keeps the block log of `astimate check --format hook`
// (SPEC.md 8.5): an opt-in, local, append-only file of JSON lines. A check
// that blocks appends one block line per violation; a later check that
// passes appends one pass line per package and metric still blocked in the
// file that it judged or found deleted, recording what the working tree
// showed at that moment, which is gone afterwards. calibration/blocks reads
// the file and classifies how each block was resolved.
//
// The package writes the log and reads it back; it never changes a check's
// decision, output or exit code.
package blocklog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Version is the schema version every line carries in its "v" field.
const Version = 1

// The events a line records.
const (
	// EventBlock is a violation of a check that blocked.
	EventBlock = "block"
	// EventPass is the state, at a check that passed, of a package and
	// metric a block line left open.
	EventPass = "pass"
)

// maxLine bounds one line Read accepts; a line is a few hundred bytes.
const maxLine = 1 << 20

// Line is one line of the block log. Block is set on a block line and Pass
// on a pass line.
type Line struct {
	// V is the schema version, Version.
	V int `json:"v"`
	// Event is EventBlock or EventPass.
	Event string `json:"event"`
	// Time is when the check ran, RFC 3339 in UTC.
	Time string `json:"time"`
	// Commit is the full hash of the repository's HEAD; empty outside git
	// or before the first commit.
	Commit string `json:"commit,omitempty"`
	// Dirty reports whether the module's working tree differs from HEAD,
	// untracked files included and the log itself left out; nil when
	// Commit is empty.
	Dirty *bool `json:"dirty,omitempty"`
	// Module is the module path of the checked module.
	Module string `json:"module"`
	// Package is the row's package_path (SPEC.md 10.2): a module-relative
	// directory, "." for the root, or <module> for the module row.
	Package string `json:"package"`
	// Metric is the blocked rule's metric.
	Metric string `json:"metric"`
	// ConfigVersion is the row's config_version, suffixed with +<language>
	// under a languages override.
	ConfigVersion string `json:"config_version"`
	// AstimateVersion is the version of the astimate that wrote the line.
	AstimateVersion string `json:"astimate_version"`
	// Block holds the violation of a block line.
	Block *Block `json:"block,omitempty"`
	// Pass holds what a pass line observed.
	Pass *Pass `json:"pass,omitempty"`
}

// Block is a violation as the check reported it (SPEC.md 10.2).
type Block struct {
	// Kind is the kind of the rule that fired: capacity, density or
	// requirement; empty when the configuration has no rule on the metric
	// for the row, which a finding never lacks today.
	Kind string `json:"kind"`
	// Base is the baseline value; nil for a package with none.
	Base *float64 `json:"base"`
	// Head is the value judged at head.
	Head float64 `json:"head"`
	// Limit is the finding's limit as reported.
	Limit string `json:"limit"`
	// Also lists the metrics merged into the finding (SPEC.md 8.1).
	Also []string `json:"also,omitempty"`
}

// Pass is what a passing check observed for a package and metric a block
// line left open: what calibration/blocks needs and cannot recompute
// later.
type Pass struct {
	// Value is the package's value of the metric now; nil when the row
	// does not compute it or the package no longer exists. It is the row's
	// value, which for a module-wide metric is the full count rather than
	// the count a check judges (SPEC.md 8.1).
	Value *float64 `json:"value"`
	// Exists reports whether the package still exists: false when the
	// check found it deleted (SPEC.md 8.4).
	Exists bool `json:"exists"`
	// NewPackages are the check's packages new since the baseline, those
	// a rename seeded included, sorted by package.
	NewPackages []NewPackage `json:"new_packages"`
	// Exempted reports whether unexpired exemptions now cover the blocked
	// finding: its metric and every metric in its also (SPEC.md 8.6).
	Exempted bool `json:"exempted"`
	// Reshaped reports whether the reshaped-exports heads-up fired on the
	// package (SPEC.md 8.1).
	Reshaped bool `json:"reshaped"`
	// ReshapedNames are the names the heads-up listed, in full; empty when
	// it did not fire.
	ReshapedNames []string `json:"reshaped_names,omitempty"`
}

// NewPackage is a package new since the baseline in a passing check.
type NewPackage struct {
	// Package is its package_path.
	Package string `json:"package"`
	// BaselineFrom lists the packages a rename seeded its row from
	// (SPEC.md 8.4); empty for a package that is simply new.
	BaselineFrom []string `json:"baseline_from,omitempty"`
	// FromBlocked reports whether BaselineFrom holds the blocked package.
	FromBlocked bool `json:"from_blocked"`
}

// Read decodes the block log in r line by line. A line that is not a JSON
// object of the current or an earlier schema version, or whose event is
// neither EventBlock nor EventPass with its matching object, is counted in
// malformed and skipped, so a torn or hand-edited line costs only itself.
func Read(r io.Reader) (lines []Line, malformed int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var l Line
		if json.Unmarshal(sc.Bytes(), &l) != nil || !l.valid() {
			malformed++
			continue
		}
		lines = append(lines, l)
	}
	if err := sc.Err(); err != nil {
		return lines, malformed, fmt.Errorf("reading block log: %w", err)
	}
	return lines, malformed, nil
}

// valid reports whether l is a line Read keeps.
func (l *Line) valid() bool {
	if l.V < 1 || l.V > Version || l.Package == "" || l.Metric == "" {
		return false
	}
	switch l.Event {
	case EventBlock:
		return l.Block != nil && l.Pass == nil
	case EventPass:
		return l.Pass != nil && l.Block == nil
	}
	return false
}

// Key identifies the package and metric a line is about.
type Key struct {
	// Module is the line's module path.
	Module string
	// Package is the line's package_path.
	Package string
	// Metric is the line's metric.
	Metric string
}

// Key returns the package and metric l is about.
func (l *Line) Key() Key { return Key{Module: l.Module, Package: l.Package, Metric: l.Metric} }
