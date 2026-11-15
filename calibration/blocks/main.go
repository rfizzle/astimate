// Command blocks reads the block log that `astimate check --format hook`
// keeps when the configuration names one (hook.block_log, SPEC.md 8.5 and
// 11.5) and reports, per block, how it was resolved: exempted, split,
// reshaped, reduced, unexplained or still open, by the first rule that
// matches, with the evidence each rule read and counts per metric and
// resolution.
//
// Usage, from the module root:
//
//	go run ./calibration/blocks [--json] [<log file> ...]
//
// The default log is .astimate/blocks.jsonl. Each file is classified on
// its own: a pass in one log never resolves a block in another. The
// classification is mechanical, from what the gate could see at each
// check: reduced is a residual that cannot tell a smaller change from an
// extraction or a workaround, and every row is evidence for a reviewer,
// not a verdict.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/rfizzle/astimate/internal/blocklog"
)

// defaultLog is the log read when no file is named, the path the
// configuration's commented example uses.
const defaultLog = ".astimate/blocks.jsonl"

// result is the reader's output: every episode and the counts.
type result struct {
	Episodes []episode `json:"episodes"`
	// Counts holds, per metric, the episodes per resolution.
	Counts map[string]map[string]int `json:"counts"`
	// Malformed counts the lines skipped because they could not be read.
	Malformed int `json:"malformed"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "blocks:", err)
		if errors.Is(err, errUsage) {
			os.Exit(1)
		}
		os.Exit(2)
	}
}

// errUsage reports a command line the command cannot run.
var errUsage = errors.New("usage: blocks [--json] [<log file> ...]")

// run parses args, reads each log and writes the report to stdout, a
// warning per log with malformed lines to stderr.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("blocks", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "write the episodes and counts as JSON")
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	files := fs.Args()
	if len(files) == 0 {
		files = []string{defaultLog}
	}
	res := result{Episodes: []episode{}, Counts: map[string]map[string]int{}}
	for _, name := range files {
		lines, malformed, err := readFile(name)
		if err != nil {
			return err
		}
		if malformed > 0 {
			_, _ = fmt.Fprintf(stderr, "blocks: %s: %d malformed lines skipped\n", name, malformed)
		}
		res.Malformed += malformed
		res.Episodes = append(res.Episodes, episodes(lines)...)
	}
	for _, e := range res.Episodes {
		if res.Counts[e.Metric] == nil {
			res.Counts[e.Metric] = map[string]int{}
		}
		res.Counts[e.Metric][e.Resolution]++
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return fmt.Errorf("writing JSON: %w", err)
		}
		return nil
	}
	return writeText(stdout, &res)
}

// readFile reads the block log name.
func readFile(name string) ([]blocklog.Line, int, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, 0, fmt.Errorf("opening block log: %w", err)
	}
	defer func() { _ = f.Close() }()
	lines, malformed, err := blocklog.Read(f)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", name, err)
	}
	return lines, malformed, nil
}

// writeText writes the episodes as a table, then the counts per metric and
// resolution, then what the classification can and cannot say.
func writeText(w io.Writer, res *result) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PACKAGE\tMETRIC\tKIND\tBLOCKS\tHEAD\tNOW\tRESOLUTION\tEVIDENCE")
	for _, e := range res.Episodes {
		now := "-"
		switch {
		case e.Deleted:
			now = "deleted"
		case e.ResolvedAt != "":
			now = valueText(e.Value)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n", e.Package, e.Metric, orDash(e.Kind), e.Blocks,
			num(e.Head), now, e.Resolution, e.Evidence)
	}
	_, _ = fmt.Fprintln(tw)
	_, _ = fmt.Fprintln(tw, "METRIC\t"+strings.ToUpper(strings.Join(resolutions(), "\t"))+"\tTOTAL")
	for _, m := range slices.Sorted(maps.Keys(res.Counts)) {
		var row strings.Builder
		row.WriteString(m)
		total := 0
		for _, r := range resolutions() {
			n := res.Counts[m][r]
			total += n
			row.WriteString("\t" + strconv.Itoa(n))
		}
		_, _ = fmt.Fprintln(tw, row.String()+"\t"+strconv.Itoa(total))
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	_, err := fmt.Fprintln(w, "\nEach block is classified by the first rule that matches what the gate saw at the next passing check "+
		"(SPEC.md 11.5).\nreduced is a residual: the log cannot tell a smaller change from an extraction or a workaround. "+
		"Every row is evidence for a reviewer, not a verdict.")
	if err != nil {
		return fmt.Errorf("writing notes: %w", err)
	}
	return nil
}

// orDash returns s, or "-" when it is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
