// Command fit derives candidate gate thresholds from the pooled corpus
// data per SPEC.md 11.1. It reads the packages.jsonl the collector wrote,
// computes each gated metric's percentiles, IQR and histogram, and writes
// a candidate configuration and a Markdown report on the distribution and
// the chosen values.
//
// Usage, from the repository root:
//
//	go run ./calibration/fit --data calibration/data/<date>/packages.jsonl [--date YYYY-MM-DD]
//
// The candidate copies the base configuration (the embedded default unless
// --base names a file) with config_version thresholds-<date>, plus a
// -stdlib-provisional suffix while every row is from the standard library,
// and each rule's max and max_delta refitted; see methodText for the
// rules. It is validated with config.Parse before it is written. Check it
// against the acceptance invariants with
//
//	ASTIMATE_CONFIG=$PWD/<candidate> go test ./internal/invariants
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rfizzle/astimate/internal/config"
)

// provisionalSuffix marks a candidate fitted from standard-library rows
// alone.
const provisionalSuffix = "stdlib-provisional"

// suffixAuto picks provisionalSuffix when every row is from the standard
// library and no suffix otherwise.
const suffixAuto = "auto"

// options are the parsed command-line flags.
type options struct {
	data, base, out, report, date, suffix string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args, fits and writes the candidate and report, and returns
// the exit code: 0 on success, 1 on a failure, 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return 2
	}
	res, err := fit(opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "fit:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s (%s) and %s\n", res.out, res.version, res.report)
	return 0
}

// parseFlags parses the command line; the flag package reports errors on
// stderr.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("fit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.StringVar(&o.data, "data", "", "pooled packages.jsonl (required)")
	fs.StringVar(&o.base, "base", "", "base configuration file; empty means the embedded default")
	fs.StringVar(&o.out, "out", "", "candidate file; empty means calibration/thresholds/astimate-thresholds-<version suffix>.yaml")
	fs.StringVar(&o.report, "report", "", "report file; empty means calibration/reports/thresholds-<version suffix>.md")
	fs.StringVar(&o.date, "date", time.Now().Format(time.DateOnly), "calibration date, YYYY-MM-DD")
	fs.StringVar(&o.suffix, "suffix", suffixAuto, "config_version suffix; auto means "+provisionalSuffix+" for standard-library-only data")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	var err error
	switch {
	case fs.NArg() > 0:
		err = fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	case o.data == "":
		err = errors.New("--data is required")
	default:
		if _, perr := time.Parse(time.DateOnly, o.date); perr != nil {
			err = fmt.Errorf("--date: %w", perr)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "fit:", err)
	}
	return o, err
}

// result names what fit wrote.
type result struct {
	version, out, report string
}

// fit reads the data and base, fits the thresholds, validates the
// candidate and writes it and the report.
func fit(opts options) (result, error) {
	rows, err := loadRows(opts.data)
	if err != nil {
		return result{}, err
	}
	baseData := config.Default()
	if opts.base != "" {
		if baseData, err = os.ReadFile(opts.base); err != nil {
			return result{}, fmt.Errorf("reading base config: %w", err)
		}
	}
	base, err := config.Parse(baseData)
	if err != nil {
		return result{}, fmt.Errorf("base config: %w", err)
	}

	mods := modules(rows)
	provisional := len(mods) == 1 && mods[0] == stdModule
	suffix := opts.suffix
	if suffix == suffixAuto {
		suffix = ""
		if provisional {
			suffix = provisionalSuffix
		}
	}
	stem := opts.date
	if suffix != "" {
		stem += "-" + suffix
	}
	version := "thresholds-" + stem
	res := result{version: version, out: opts.out, report: opts.report}
	if res.out == "" {
		res.out = filepath.Join("calibration", "thresholds", "astimate-thresholds-"+stem+".yaml")
	}
	if res.report == "" {
		res.report = filepath.Join("calibration", "reports", "thresholds-"+stem+".md")
	}

	choices := fitThresholds(rows, base)
	header := candidateHeader(version, opts.data, len(rows), provisional)
	out, err := emitCandidate(baseData, version, header, choices)
	if err != nil {
		return result{}, err
	}
	if _, err := config.Parse(out); err != nil {
		return result{}, fmt.Errorf("candidate does not validate: %w", err)
	}
	report := renderReport(&reportInput{
		Version:     version,
		BaseVersion: base.Version,
		Data:        filepath.ToSlash(opts.data),
		Candidate:   filepath.ToSlash(res.out),
		Rows:        len(rows),
		Modules:     mods,
		Provisional: provisional,
		Choices:     choices,
		CrossPkg:    crossPkgStats(rows),
	})
	if err := writeFile(res.out, out); err != nil {
		return result{}, err
	}
	if err := writeFile(res.report, []byte(report)); err != nil {
		return result{}, err
	}
	return res, nil
}

// candidateHeader is the candidate file's leading comment.
func candidateHeader(version, data string, rows int, provisional bool) string {
	lines := []string{"Astimate configuration: rebuild parameters and gate thresholds in one file.", ""}
	lines = append(lines, wrap(fmt.Sprintf("Candidate %s, fitted by calibration/fit (SPEC.md 11.1) from %d packages in %s.",
		version, rows, filepath.ToSlash(data)), 74)...)
	if provisional {
		lines = append(lines, wrap("PROVISIONAL: every row is from the Go standard library; refit after "+
			"the corpus run, before this replaces the embedded default.", 74)...)
	}
	for _, m := range methodText() {
		lines = append(lines, "")
		lines = append(lines, wrap(m, 74)...)
	}
	for i, l := range lines {
		lines[i] = strings.TrimRight("# "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// wrap breaks s into lines of at most width bytes at spaces.
func wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) > width:
			lines = append(lines, line)
			line = word
		default:
			line += " " + word
		}
	}
	return append(lines, line)
}

// writeFile writes data to path, creating its directory.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
