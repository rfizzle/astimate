package report

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/rfizzle/astimate/internal/metrics"
)

// metricColumns is how many name/value pairs the metrics section puts on one
// line.
const metricColumns = 2

// WriteTable writes r for a human reader: the package, with its module path
// in parentheses when there is one, the estimate line labelled as an
// estimate from calibrated or uncalibrated rebuild parameters (the label says
// nothing about the gate thresholds), the tier, drivers, suggestions, then the metrics in two columns
// in metrics.MetricNames order, skipping v1 fields that were not computed.
func WriteTable(w io.Writer, r *Report) error {
	bw := bufio.NewWriter(w)
	label := "estimate from uncalibrated parameters"
	if r.Rebuild.Calibrated {
		label = "estimate from calibrated parameters"
	}
	if r.ModulePath == "" {
		_, _ = fmt.Fprintf(bw, "package: %s\n", r.PackagePath)
	} else {
		_, _ = fmt.Fprintf(bw, "package: %s (%s)\n", r.PackagePath, r.ModulePath)
	}
	_, _ = fmt.Fprintf(bw, "rebuild: %s agent passes, %s human days (%s)\n",
		strconv.FormatFloat(r.Rebuild.AgentPasses, 'f', 1, 64),
		strconv.FormatFloat(r.Rebuild.HumanDays, 'f', 1, 64), label)
	_, _ = fmt.Fprintf(bw, "tier: %s\n", r.Rebuild.Tier)

	_, _ = bw.WriteString("\ndrivers:\n")
	if len(r.Rebuild.Drivers) == 0 {
		_, _ = bw.WriteString("  none\n")
	}
	tw := tabwriter.NewWriter(bw, 0, 0, 2, ' ', 0)
	for _, d := range r.Rebuild.Drivers {
		_, _ = fmt.Fprintf(tw, "  %s\t%d tokens\t%s\n", d.Term, d.Tokens, d.Detail)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table report: %w", err)
	}

	_, _ = bw.WriteString("\nsuggestions:\n")
	if len(r.Suggestions) == 0 {
		_, _ = bw.WriteString("  none\n")
	}
	for _, s := range r.Suggestions {
		_, _ = bw.WriteString("  - " + s + "\n")
	}

	_, _ = bw.WriteString("\nmetrics:\n")
	if err := writeMetrics(bw, &r.Metrics); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("writing table report: %w", err)
	}
	return nil
}

// writeMetrics writes the computed metrics as aligned name/value pairs,
// metricColumns pairs per line.
func writeMetrics(w io.Writer, m *metrics.RawMetrics) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	cells := make([]string, 0, metricColumns*2)
	for _, name := range metrics.MetricNames() {
		v, ok := metricText(m, name)
		if !ok {
			continue
		}
		cells = append(cells, name, v)
		if len(cells) == cap(cells) {
			_, _ = io.WriteString(tw, "  "+strings.Join(cells, "\t")+"\n")
			cells = cells[:0]
		}
	}
	if len(cells) > 0 {
		_, _ = io.WriteString(tw, "  "+strings.Join(cells, "\t")+"\n")
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table report: %w", err)
	}
	return nil
}

// metricText formats one metric for the table: booleans as true or false,
// numbers to at most two decimals. It reports false for a v1 field that was
// not computed.
func metricText(m *metrics.RawMetrics, name string) (string, bool) {
	v, ok := m.Value(name)
	if !ok {
		return "", false
	}
	switch name {
	case "has_tests", "uses_cgo", "uses_reflect":
		return strconv.FormatBool(v != 0), true
	}
	return formatFloat(math.Round(v*100) / 100), true
}

// formatFloat prints v in the shortest form that round-trips.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
