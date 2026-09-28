// Command fit fits the rebuild estimate's parameters (SPEC.md 7.2) to the
// rebuild experiments' measurements (SPEC.md 11.2 steps 4 and 5). It reads
// the runs.jsonl rows the rebuild runner wrote, takes each package's median
// tokens over its passing runs, fits the 7.2 form by least squares, and
// writes a candidate configuration with config_version
// rebuild-<date>-<agent> and a Markdown report.
//
// Usage, from the repository root:
//
//	go run ./calibration/rebuild/fit --runs calibration/data/rebuild-<date>-<agent>/runs.jsonl \
//	    [--runs more.jsonl] [--base internal/config/default.yaml] [--date YYYY-MM-DD] [--agent name] \
//	    [--measure footprint|total|output] [--budget tokens] [--out file.yaml] [--report file.md]
//	    [--unit package|tree]
//
// Every row must be of one unit, the package rows of rebuild.yaml or the
// tree rows of rebuild-trees.yaml; the fit refuses to mix them, and --unit
// makes it refuse rows of the other. A tree fit regresses on each tree's
// aggregate metrics, so it fits the sum estimate method, and its
// config_version is rebuild-trees-<date>-<agent>.
//
// The candidate is the base configuration (the embedded default unless
// --base names a file) byte for byte, except config_version and the rebuild
// section's context_budget, tokens_per_export, tokens_per_untested_export,
// tokens_per_hidden_state and superlinear_exponent. It is parsed and
// validated before it is written. A parameter the data cannot fit keeps its
// base value, and the report and standard error say so. Check the candidate
// against the acceptance invariants with
//
//	ASTIMATE_CONFIG=$PWD/<candidate> go test ./internal/invariants
//
// and ship it by writing it over internal/config/default.yaml (--out
// internal/config/default.yaml --base internal/config/default.yaml).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/dataset"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/emit"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/model"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/regress"
	"github.com/rfizzle/astimate/calibration/rebuild/fit/internal/report"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/internal/config"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// runsFlag collects repeated --runs values.
type runsFlag []string

// String returns the values joined with commas.
func (r *runsFlag) String() string { return strings.Join(*r, ",") }

// Set appends one value.
func (r *runsFlag) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// options are the parsed command-line flags.
type options struct {
	runs                                              runsFlag
	base, date, agent, out, report, measureName, unit string
	measure                                           dataset.Measure
	budget                                            float64
	args                                              []string
}

// usageError is a bad command line: exit code 2. printed is true when the
// flag package has already reported it.
type usageError struct {
	err     error
	printed bool
}

// Error returns the cause's message.
func (u *usageError) Error() string { return u.err.Error() }

// run parses args, fits, writes the candidate and the report, and returns
// the exit code: 0 on success, 1 on a failure, 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	res, err := execute(args, stderr)
	var usage *usageError
	code := 1
	if errors.As(err, &usage) {
		code = 2
	}
	switch {
	case usage != nil && usage.printed:
		return code
	case err != nil:
		_, _ = fmt.Fprintln(stderr, "fit:", err)
		return code
	case !res.fitted:
		_, _ = fmt.Fprintln(stderr, "fit: warning: no parameter could be fitted; every one keeps its base value (see the report)")
	}
	_, _ = fmt.Fprintln(stdout, "wrote", res.out, "("+res.version+") and", res.report)
	return 0
}

// parseFlags parses the command line into options.
func parseFlags(args []string, stderr io.Writer) (*options, error) {
	o := &options{args: args}
	fs := flag.NewFlagSet("rebuild-fit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Var(&o.runs, "runs", "the runner's runs.jsonl (required; repeat for more files)")
	for _, s := range []struct {
		dst                *string
		name, def, purpose string
	}{
		{&o.base, "base", "", "base configuration file; empty means the embedded default"},
		{&o.date, "date", time.Now().Format(time.DateOnly), "calibration date, YYYY-MM-DD"},
		{&o.agent, "agent", "", "agent name in config_version; empty means the rows' agent.name"},
		{&o.out, "out", "", "candidate file; empty means calibration/rebuild/astimate-<config_version>.yaml"},
		{&o.report, "report", "", "report file; empty means calibration/reports/<config_version>.md"},
		{&o.measureName, "measure", string(dataset.Footprint), "token measure: footprint, total or output"},
		{&o.unit, "unit", "", "unit the rows must be, package or tree; empty means the rows' own, which must be one"},
	} {
		fs.StringVar(s.dst, s.name, s.def, s.purpose)
	}
	fs.Float64Var(&o.budget, "budget", 0, "context budget to fit at; 0 means the base's")
	if err := fs.Parse(args); err != nil {
		return nil, &usageError{err: err, printed: true}
	}
	_, dateErr := time.Parse(time.DateOnly, o.date)
	var measureErr error
	o.measure, measureErr = dataset.ParseMeasure(o.measureName)
	for _, c := range []struct {
		bad bool
		msg string
	}{
		{fs.NArg() > 0, "unexpected arguments: " + strings.Join(fs.Args(), " ")},
		{len(o.runs) == 0, "--runs is required"},
		{o.budget < 0, "--budget must be positive"},
		{o.agent != "" && !validName(o.agent), "--agent " + strconv.Quote(o.agent) + ": use lower-case letters, digits, '.', '_' and '-'"},
		{dateErr != nil, fmt.Sprint("--date: ", dateErr)},
		{measureErr != nil, fmt.Sprint("--measure: ", measureErr)},
		{o.unit != "" && o.unit != definition.UnitPackage && o.unit != definition.UnitTree,
			"--unit " + strconv.Quote(o.unit) + ": want package or tree"},
	} {
		if c.bad {
			return nil, &usageError{err: errors.New(c.msg)}
		}
	}
	return o, nil
}

// validName reports whether s is safe as a config_version part and a file
// name.
func validName(s string) bool {
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return s != ""
}

// result names what fit wrote.
type result struct {
	version, out, report string
	fitted               bool
}

// orDefault returns v, or def when v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// execute parses args, reads the runs and the base, fits, and writes the
// candidate and the report.
func execute(args []string, stderr io.Writer) (result, error) {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return result{}, err
	}
	set, err := loadSet(opts)
	if err != nil {
		return result{}, err
	}
	agentName := orDefault(opts.agent, set.Agent)
	if !validName(agentName) {
		return result{}, fmt.Errorf("agent name %q from the rows is not usable in config_version; pass --agent", agentName)
	}
	baseData := config.Default()
	if opts.base != "" {
		baseData, err = os.ReadFile(opts.base)
	}
	var base *config.Config
	if err == nil {
		base, err = config.Parse(baseData)
	}
	if err != nil {
		return result{}, fmt.Errorf("base config: %w", err)
	}
	version := "rebuild-" + opts.date + "-" + agentName
	if set.Unit == definition.UnitTree {
		version = "rebuild-trees-" + opts.date + "-" + agentName
	}
	res := result{
		version: version,
		out:     orDefault(opts.out, filepath.Join("calibration", "rebuild", "astimate-"+version+".yaml")),
		report:  orDefault(opts.report, filepath.Join("calibration", "reports", version+".md")),
	}
	in := analyze(set, base, opts.budget)
	in.Version, in.BaseVersion, in.Runs = version, base.Version, opts.runs
	in.Out, in.Command = filepath.ToSlash(res.out), command(opts.args)
	candidate, err := emit.Config(baseData, version, in.Emitted.Params)
	if err != nil {
		return result{}, err
	}
	res.fitted = in.Emitted.Fitted
	for _, f := range []struct {
		path string
		data []byte
	}{{res.out, candidate}, {res.report, []byte(report.Render(in))}} {
		if err := errors.Join(os.MkdirAll(filepath.Dir(f.path), 0o755), os.WriteFile(f.path, f.data, 0o644)); err != nil {
			return result{}, fmt.Errorf("writing the output: %w", err)
		}
	}
	return res, nil
}

// loadSet reads and classifies the runs opts names.
func loadSet(opts *options) (*dataset.Set, error) {
	rows, err := dataset.Load(opts.runs)
	if err != nil {
		return nil, err
	}
	set, err := dataset.Build(rows, opts.measure)
	if err == nil && opts.unit != "" && set.Unit != opts.unit {
		err = fmt.Errorf("--unit %s, but the rows are %s runs; a %s fit takes only %s rows", opts.unit, set.Unit, opts.unit, opts.unit)
	}
	return &set, err
}

// analyze runs every fit on set with base's parameters as the starting
// point and the budget held at budget (base's when 0). The form is also
// refitted at a spread of budgets around it for the report.
func analyze(set *dataset.Set, base *config.Config, budget float64) *report.Input {
	measured := set.Measured()
	obs := make([]model.Obs, len(measured))
	for i := range measured {
		obs[i] = model.Obs{Package: measured[i].Package, Metrics: measured[i].Metrics, Tokens: regress.Median(measured[i].Tokens)}
	}
	rp := base.Rebuild
	if budget == 0 {
		budget = rp.ContextBudget
	}
	start := model.Params{Budget: budget, PerExport: rp.TokensPerExport, PerUntested: rp.TokensPerUntestedExport,
		PerHidden: rp.TokensPerHiddenState, Exponent: rp.SuperlinearExponent}
	in := &report.Input{Set: set, Obs: obs, Base: rp}
	ff, err := model.FitForm(obs, start)
	if err != nil {
		in.FormErr = err
		in.Emitted = emit.FromFit(rp, nil, err.Error())
		in.Contributions, in.Linear = model.Contributions(obs, set.Outcomes, nil)
		return in
	}
	in.Form = &ff
	in.Emitted = emit.FromFit(rp, &ff, "")
	in.Contributions, in.Linear = model.Contributions(obs, set.Outcomes, model.Linearize(ff.Params))
	var budgets []float64
	for _, f := range [...]float64{0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4} {
		budgets = append(budgets, budget*f)
	}
	in.Profile = model.Profile(obs, start, budgets)
	return in
}

// command is the invocation that reproduces the fit, with each argument
// that the shell would split or expand single-quoted.
func command(args []string) string {
	var b strings.Builder
	b.WriteString("go run ./calibration/rebuild/fit")
	for _, a := range args {
		b.WriteByte(' ')
		if strings.ContainsAny(a, " '\"$") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		b.WriteString(a)
	}
	return b.String()
}
