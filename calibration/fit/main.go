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
// and each rule's max and max_delta refitted; see report.MethodText for
// the rules. It is validated with config.Parse before it is written.
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

	"github.com/rfizzle/astimate/calibration/fit/internal/emit"
	"github.com/rfizzle/astimate/calibration/fit/internal/pool"
	"github.com/rfizzle/astimate/calibration/fit/internal/report"
	"github.com/rfizzle/astimate/calibration/fit/internal/stats"
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
	rows, baseRows, moduleRows, packageRows, err := loadFitRows(opts)
	if err != nil {
		return result{}, err
	}
	base, baseData, err := loadFitBase(opts)
	if err != nil {
		return result{}, err
	}
	previous, err := loadFitPrevious(opts)
	if err != nil {
		return result{}, err
	}

	mods := pool.Modules(rows)
	version, stem, provisional := planVersion(opts, base, mods)
	res := defaultResult(opts, version, stem)

	choices := pool.FitThresholds(rows, base)
	out, err := emitFitOutput(opts, baseData, version, fitSource(opts, moduleRows), packageRows, provisional, choices)
	if err != nil {
		return result{}, err
	}
	run, err := loadFitRun(opts)
	if err != nil {
		return result{}, err
	}
	rendered := report.RenderReport(&report.Input{
		Language:    opts.language,
		Run:         run,
		BaseData:    filepath.ToSlash(opts.baseData),
		BaseStats:   fitBaseStats(baseRows, choices),
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
		CrossPkg:    pool.CrossPkgStats(rows),

		Previous:     previous,
		PreviousPath: filepath.ToSlash(opts.compare),
	})
	if err := writeFile(res.out, out); err != nil {
		return result{}, err
	}
	if err := writeFile(res.report, []byte(rendered)); err != nil {
		return result{}, err
	}
	return res, nil
}

// loadFitRows loads and validates the rows opts names: the pooled data,
// any module rows to combine with it from --modules, and the rows
// --base-data names. It returns the combined rows, the base rows (nil when
// --base-data is empty), and how many of the combined rows are module and
// package rows.
func loadFitRows(opts options) (rows, baseRows []pool.Row, moduleRows, packageRows int, err error) {
	if rows, err = pool.LoadRows(opts.data); err != nil {
		return nil, nil, 0, 0, err
	}
	if opts.modules != "" {
		if rows, err = addModuleRows(rows, opts.modules); err != nil {
			return nil, nil, 0, 0, err
		}
	}
	if err = pool.CheckLanguage(rows, opts.language); err != nil {
		return nil, nil, 0, 0, fmt.Errorf("reading %s: %w", opts.data, err)
	}
	if opts.baseData != "" {
		if baseRows, err = pool.LoadRows(opts.baseData); err != nil {
			return nil, nil, 0, 0, err
		}
		if err = pool.CheckLanguage(baseRows, ""); err != nil {
			return nil, nil, 0, 0, fmt.Errorf("reading %s: %w", opts.baseData, err)
		}
	}
	moduleRows = pool.CountModuleRows(rows)
	return rows, baseRows, moduleRows, len(rows) - moduleRows, nil
}

// addModuleRows loads the module rows at path, checks that every one is a
// module row, and appends them to rows.
func addModuleRows(rows []pool.Row, path string) ([]pool.Row, error) {
	modRows, err := pool.LoadRows(path)
	if err != nil {
		return nil, err
	}
	for i := range modRows {
		if !pool.IsModuleRow(&modRows[i]) {
			return nil, fmt.Errorf("reading %s: row %d is package %q, not a module row", path, i+1, modRows[i].Package)
		}
	}
	return append(rows, modRows...), nil
}

// loadFitBase reads the base configuration opts names, or the embedded
// default, and parses it.
func loadFitBase(opts options) (base *config.Config, baseData []byte, err error) {
	baseData = config.Default()
	if opts.base != "" {
		if baseData, err = os.ReadFile(opts.base); err != nil {
			return nil, nil, fmt.Errorf("reading base config: %w", err)
		}
	}
	if base, err = config.Parse(baseData); err != nil {
		return nil, nil, fmt.Errorf("base config: %w", err)
	}
	return base, baseData, nil
}

// loadFitPrevious loads the configuration --compare names, or nil when it
// is empty.
func loadFitPrevious(opts options) (*config.Config, error) {
	if opts.compare == "" {
		return nil, nil
	}
	previous, err := config.Load(opts.compare)
	if err != nil {
		return nil, fmt.Errorf("compare config: %w", err)
	}
	return previous, nil
}

// planVersion picks the candidate's config_version, or with --language the
// override's version (the base's config_version kept, per SPEC.md 13), the
// file stem the output paths are named from, and whether the data is
// standard-library rows alone.
func planVersion(opts options, base *config.Config, mods []string) (version, stem string, provisional bool) {
	provisional = len(mods) == 1 && mods[0] == pool.StdModule
	suffix := opts.suffix
	if suffix == suffixAuto {
		suffix = ""
		if provisional {
			suffix = provisionalSuffix
		}
	}
	stem = opts.date
	if suffix != "" {
		stem += "-" + suffix
	}
	version = "thresholds-" + stem
	if opts.language != "" {
		// An override keeps the base's config_version; reports judged
		// with it show the language suffix.
		stem += "-" + opts.language
		version = base.Version + "+" + opts.language
	}
	return version, stem, provisional
}

// defaultResult fills opts.out and opts.report with their default paths,
// named from stem, when they are empty.
func defaultResult(opts options, version, stem string) result {
	res := result{version: version, out: opts.out, report: opts.report}
	if res.out == "" {
		res.out = filepath.Join("calibration", "thresholds", "astimate-thresholds-"+stem+".yaml")
	}
	if res.report == "" {
		res.report = filepath.Join("calibration", "reports", "thresholds-"+stem+".md")
	}
	return res
}

// fitSource describes the rows a fit was made from, for the candidate's
// header and the report: the data file alone, or with the module rows
// pooled from --modules.
func fitSource(opts options, moduleRows int) string {
	if opts.modules == "" {
		return opts.data
	}
	return opts.data + " and " + strconv.Itoa(moduleRows) + " module rows in " + opts.modules
}

// emitFitOutput writes the fitted YAML: a languages.<id> override block
// with --language, else a whole candidate configuration, validated with
// config.Parse.
func emitFitOutput(opts options, baseData []byte, version, source string, packageRows int, provisional bool, choices []pool.Choice) ([]byte, error) {
	if opts.language != "" {
		return emit.Language(baseData, opts.language, version, source, packageRows, choices)
	}
	out, err := emit.Candidate(baseData, version, candidateHeader(version, source, packageRows, provisional), choices)
	if err != nil {
		return nil, err
	}
	if _, err := config.Parse(out); err != nil {
		return nil, fmt.Errorf("candidate does not validate: %w", err)
	}
	return out, nil
}

// loadFitRun reads the collector's run.json beside the data, for a
// --language fit's report; nil for a whole-configuration fit, which does
// not describe the corpus.
func loadFitRun(opts options) (*report.RunInfo, error) {
	if opts.language == "" {
		return nil, nil
	}
	return loadRun(filepath.Join(filepath.Dir(opts.data), "run.json"))
}

// fitBaseStats computes each choice's distribution over baseRows, index
// for index; nil when baseRows is nil.
func fitBaseStats(baseRows []pool.Row, choices []pool.Choice) []stats.Stats {
	if baseRows == nil {
		return nil
	}
	baseStats := make([]stats.Stats, len(choices))
	for i := range choices {
		baseStats[i] = stats.ComputeStats(pool.Values(baseRows, choices[i].Rule.Metric, choices[i].Pool))
	}
	return baseStats
}

// candidateHeader is the candidate file's leading comment.
func candidateHeader(version, data string, rows int, provisional bool) string {
	lines := []string{"Astimate configuration: rebuild parameters and gate thresholds in one file.", ""}
	lines = append(lines, report.Wrap(fmt.Sprintf("Candidate %s, fitted by calibration/fit (SPEC.md 11.1) from %d packages in %s.",
		version, rows, filepath.ToSlash(data)), 74)...)
	if provisional {
		lines = append(lines, report.Wrap("PROVISIONAL: every row is from the Go standard library; refit after "+
			"the corpus run, before this replaces the embedded default.", 74)...)
	}
	for _, m := range report.MethodText() {
		lines = append(lines, "")
		lines = append(lines, report.Wrap(m, 74)...)
	}
	for i, l := range lines {
		lines[i] = strings.TrimRight("# "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// loadRun reads the collector's run.json at path; nil, with no error, when
// there is none.
func loadRun(path string) (*report.RunInfo, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading run info: %w", err)
	}
	var r report.RunInfo
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
