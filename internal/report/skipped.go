package report

import (
	"bufio"
	"strings"
)

// Skipped is a package a check left out because it does not load, such as
// a Go package importing a module that is not available (SPEC.md 6.5). It
// is neither extracted nor gated; the packages around it are.
type Skipped struct {
	// Package is the package directory relative to the module root, in
	// slash form, as package_path names a checked package.
	Package string `json:"package"`
	// Reason is the kind of failure, such as "missing module" or "type
	// error in test files".
	Reason string `json:"reason"`
	// Error is the package's first load error.
	Error string `json:"error"`
	// Selected reports that the check would have judged the package: it
	// changed since the baseline. Its verdict is then
	// incomplete, and check exits 2 unless a violation decides it.
	Selected bool `json:"selected,omitempty"`
}

// incomplete reports whether c left out a package it would have judged.
func (c *Check) incomplete() bool {
	for i := range c.Skipped {
		if c.Skipped[i].Selected {
			return true
		}
	}
	return false
}

// skippedText renders s on one line: "<package> skipped, it does not load
// (<reason>): <error>", the error's whitespace runs, line breaks included,
// collapsed to one space, followed by a note when the check selected it.
func skippedText(s *Skipped) string {
	text := s.Package + " skipped, it does not load (" + s.Reason + "): " + strings.Join(strings.Fields(s.Error), " ")
	if s.Selected {
		text += "; it is among the packages this check judges, so the verdict is incomplete"
	}
	return text
}

// writeSkippedLines writes one line per skipped package of c, each
// skippedText behind lead.
func writeSkippedLines(w *bufio.Writer, c *Check, lead string) {
	for i := range c.Skipped {
		_, _ = w.WriteString(lead + skippedText(&c.Skipped[i]) + "\n")
	}
}

// jsonRow is one entry of check --format json: a row's report, with the
// skipped packages on the module row only (SPEC.md 10.2).
type jsonRow struct {
	Report
	// Skipped lists the packages the check left out because they do not
	// load; omitted when there are none and on every package row.
	Skipped []Skipped `json:"skipped,omitempty"`
}
