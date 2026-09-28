// Command fit derives candidate gate thresholds from the pooled corpus
// data per SPEC.md 11.1. It reads the packages.jsonl the collector wrote,
// computes each gated metric's percentiles, IQR and histogram, and writes
// a candidate configuration and a Markdown report on the distribution and
// the chosen values.
//
// Usage, from the repository root:
//
//	go run ./calibration/fit --data calibration/data/<date>/packages.jsonl [--modules modules.jsonl] [--date YYYY-MM-DD] [--compare old.yaml]
//
// The candidate copies the base configuration (the embedded default unless
// --base names a file) with config_version thresholds-<date>, plus a
// -stdlib-provisional suffix while every row is from the standard library,
// and each rule's max and max_delta refitted; see methodText for the
// rules. It is validated with config.Parse before it is written.
// --compare adds a table of an earlier configuration's limits against the
// candidate's to the report. --modules adds the module rows (SPEC.md 8.1)
// the collector wrote to modules.jsonl, which a rule on a module-wide
// metric such as dup_blocks_cross_pkg is fitted from, and the report
// describes their distribution. Check the candidate against the acceptance
// invariants with
//
//	ASTIMATE_CONFIG=$PWD/<candidate> go test ./internal/invariants
//
// --language <id> fits rows collected for another language (SPEC.md 13)
// and writes, instead of a whole configuration, the languages.<id>
// override block to paste into the configuration: one rule per base rule
// the data fitted a statistic for, each replacing the top-level rule on
// its metric for that language, and nothing for the rebuild parameters.
// Every row must carry that language. The block is validated merged into
// the base, whose config_version it keeps; reports judged with it show
// <config_version>+<id>. --base-data names the rows the base was fitted
// from, and the report then sets the two languages' percentiles side by
// side.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	data, modules, base, out, report, date, suffix, compare string
	// language, when set, fits a languages.<language> override block;
	// baseData is the rows the base configuration was fitted from.
	language, baseData string
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
	fs.StringVar(&o.modules, "modules", "", "module rows (modules.jsonl) to pool with the data; empty means none")
	fs.StringVar(&o.base, "base", "", "base configuration file; empty means the embedded default")
	fs.StringVar(&o.out, "out", "", "candidate file; empty means calibration/thresholds/astimate-thresholds-<version suffix>.yaml")
	fs.StringVar(&o.report, "report", "", "report file; empty means calibration/reports/thresholds-<version suffix>.md")
	fs.StringVar(&o.date, "date", time.Now().Format(time.DateOnly), "calibration date, YYYY-MM-DD")
	fs.StringVar(&o.suffix, "suffix", suffixAuto, "config_version suffix; auto means "+provisionalSuffix+" for standard-library-only data")
	fs.StringVar(&o.compare, "compare", "", "earlier configuration file whose limits the report compares with the candidate's")
	fs.StringVar(&o.language, "language", "", "fit rows of this language and write its languages override block, not a whole configuration")
	fs.StringVar(&o.baseData, "base-data", "", "with --language: the rows the base was fitted from, for the report's side-by-side percentiles")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	var err error
	switch {
	case fs.NArg() > 0:
		err = fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	case o.data == "":
		err = errors.New("--data is required")
	case o.baseData != "" && o.language == "":
		err = errors.New("--base-data needs --language")
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
	if opts.modules != "" {
		modRows, err := loadRows(opts.modules)
		if err != nil {
			return result{}, err
		}
		for i := range modRows {
			if !isModuleRow(&modRows[i]) {
				return result{}, fmt.Errorf("reading %s: row %d is package %q, not a module row", opts.modules, i+1, modRows[i].Package)
			}
		}
		rows = append(rows, modRows...)
	}
	if err := checkLanguage(rows, opts.language); err != nil {
		return result{}, fmt.Errorf("reading %s: %w", opts.data, err)
	}
	var baseRows []Row
	if opts.baseData != "" {
		if baseRows, err = loadRows(opts.baseData); err != nil {
			return result{}, err
		}
		if err := checkLanguage(baseRows, ""); err != nil {
			return result{}, fmt.Errorf("reading %s: %w", opts.baseData, err)
		}
	}
	moduleRows := countModuleRows(rows)
	packageRows := len(rows) - moduleRows
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
	var previous *config.Config
	if opts.compare != "" {
		if previous, err = config.Load(opts.compare); err != nil {
			return result{}, fmt.Errorf("compare config: %w", err)
		}
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
	if opts.language != "" {
		// An override keeps the base's config_version; reports judged
		// with it show the language suffix.
		stem += "-" + opts.language
		version = base.Version + "+" + opts.language
	}
	res := result{version: version, out: opts.out, report: opts.report}
	if res.out == "" {
		res.out = filepath.Join("calibration", "thresholds", "astimate-thresholds-"+stem+".yaml")
	}
	if res.report == "" {
		res.report = filepath.Join("calibration", "reports", "thresholds-"+stem+".md")
	}

	choices := fitThresholds(rows, base)
	source := opts.data
	if opts.modules != "" {
		source = opts.data + " and " + strconv.Itoa(moduleRows) + " module rows in " + opts.modules
	}
	var out []byte
	if opts.language != "" {
		out, err = emitLanguage(baseData, opts.language, version, source, packageRows, choices)
	} else {
		out, err = emitCandidate(baseData, version, candidateHeader(version, source, packageRows, provisional), choices)
		if err == nil {
			if _, perr := config.Parse(out); perr != nil {
				err = fmt.Errorf("candidate does not validate: %w", perr)
			}
		}
	}
	if err != nil {
		return result{}, err
	}
	var run *runInfo
	if opts.language != "" {
		if run, err = loadRun(filepath.Join(filepath.Dir(opts.data), "run.json")); err != nil {
			return result{}, err
		}
	}
	var baseStats []Stats
	if baseRows != nil {
		baseStats = make([]Stats, len(choices))
		for i := range choices {
			baseStats[i] = computeStats(poolValues(baseRows, choices[i].Rule.Metric, choices[i].Pool))
		}
	}
	report := renderReport(&reportInput{
		Language:    opts.language,
		Run:         run,
		BaseData:    filepath.ToSlash(opts.baseData),
		BaseStats:   baseStats,
		Version:     version,
		BaseVersion: base.Version,
		Data:        filepath.ToSlash(opts.data),
		Candidate:   filepath.ToSlash(res.out),
		Rows:        packageRows,
		ModuleRows:  moduleRows,
		ModulesData: filepath.ToSlash(opts.modules),
		Modules:     mods,
		Provisional: provisional,
		Choices:     choices,
		CrossPkg:    crossPkgStats(rows),

		Previous:     previous,
		PreviousPath: filepath.ToSlash(opts.compare),
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

// loadRun reads the collector's run.json at path; nil, with no error, when
// there is none.
func loadRun(path string) (*runInfo, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading run info: %w", err)
	}
	var r runInfo
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return &r, nil
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
