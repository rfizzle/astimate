package report

import "bufio"

// unavailableText renders the notice of a package whose baseline is
// unavailable (Report.BaselineUnavailable): "baseline unavailable for
// <package>: <reason>; ratchets skipped".
func unavailableText(r *Report) string {
	return "baseline unavailable for " + r.PackagePath + ": " + r.BaselineUnavailable + "; ratchets skipped"
}

// writeUnavailableLines writes one line per package row of c whose
// baseline is unavailable, each unavailableText behind lead.
func writeUnavailableLines(w *bufio.Writer, c *Check, lead string) {
	for i := range c.Packages {
		if r := &c.Packages[i].Report; r.BaselineUnavailable != "" {
			_, _ = w.WriteString(lead + unavailableText(r) + "\n")
		}
	}
}
