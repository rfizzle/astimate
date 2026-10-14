package summary

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Header is what the text summary says about the run itself.
type Header struct {
	// Selected is the number of commits the replay selected.
	Selected int
	// Budget is dryrun's --budget as a share.
	Budget float64
	// Wall is the run's wall time.
	Wall time.Duration
}

// WriteText renders s as dryrun's text summary: the failed share of the
// commits checked and of those that touched a package, a per-rule table,
// each rule's share of the commits checked, marking the rules over the
// budget, the most-failed packages and the commits that could not be
// checked.
func WriteText(w io.Writer, s *Summary, h Header) error {
	var b strings.Builder
	fmt.Fprintf(&b, "replayed %d of %d commits in %s: %d checked, %d not loaded, %d touched a package\n",
		s.Commits, h.Selected, h.Wall.Round(time.Second), s.Checked, len(s.NotLoaded), s.Touched)
	fmt.Fprintf(&b, "would have failed %d of %d commits checked (%s), %d of the %d that touched a package (%s)\n",
		s.Failed, s.Checked, pct(s.Share(s.Failed)), s.Failed, s.Touched, pct(s.TouchedShare(s.Failed)))
	if len(s.Rules) > 0 {
		width := len("rule")
		for _, r := range s.Rules {
			width = max(width, len(r.Metric))
		}
		fmt.Fprintf(&b, "\n%-*s  %6s  %6s  %5s\n", width, "rule", "failed", "share", "alone")
		for _, r := range s.Rules {
			mark := ""
			if s.Share(r.Failed) > h.Budget {
				mark = "  over budget"
			}
			fmt.Fprintf(&b, "%-*s  %6d  %6s  %5d%s\n", width, r.Metric, r.Failed, pct(s.Share(r.Failed)), r.Alone, mark)
		}
		fmt.Fprintf(&b, "budget %s of the commits checked: --suggest sets severity warn on the rules over it\n", pct(h.Budget))
	}
	if len(s.Packages) > 0 {
		b.WriteString("\nmost-failed packages\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, p := range s.Packages {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\n", p.Package, commitCount(p.Failed))
		}
		_ = tw.Flush()
	}
	if len(s.NotLoaded) > 0 {
		b.WriteString("\nnot loaded\n")
		for _, c := range s.NotLoaded {
			fmt.Fprintf(&b, "  %s %s: %s\n", shortHash(c.Commit), c.Subject, strings.Join(strings.Fields(c.Error), " "))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// pct formats a share as a percentage with one decimal.
func pct(share float64) string {
	return strconv.FormatFloat(100*share, 'f', 1, 64) + "%"
}

// commitCount formats n commits, "1 commit" or "n commits".
func commitCount(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return strconv.Itoa(n) + " commits"
}

// shortHash abbreviates a commit hash to 12 characters.
func shortHash(h string) string {
	return h[:min(len(h), 12)]
}
