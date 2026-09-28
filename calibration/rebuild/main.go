// Command rebuild defines the rebuild experiment of SPEC.md 11.2: which
// corpus packages are deleted and rebuilt, how each is stubbed, and the
// oracle a rebuild must pass. See calibration/rebuild/README.md.
//
// Usage, from the repository root:
//
//	go run ./calibration/rebuild select [flags]   # choose and verify the packages (network)
//	go run ./calibration/rebuild stub --root <module root> --dir <package dir> [--sha256 <hash>]
//	go run ./calibration/rebuild run --live [flags]  # run every experiment with Claude Code (live requests)
//	go run ./calibration/rebuild run --agent <template> [flags]  # run with another agent command
//
// select reads the corpus data, clones each candidate's module at its pin
// into a scratch directory, verifies the candidate and writes rebuild.yaml
// and selection.md. stub stubs one package of a module in place and prints
// the tree hash. run clones, stubs and hands each experiment to an agent,
// runs the oracle and appends one row per run to runs.jsonl; without
// --live it never starts the default agent, Claude Code.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/pin"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/selection"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
	"github.com/rfizzle/astimate/internal/config"
)

// Exit codes: 0 on success, 1 when the command ran and failed, 2 for a
// usage error.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// corpusFile is the part of calibration/corpus.yaml the selection reads.
type corpusFile struct {
	Modules []struct {
		Module string `yaml:"module"`
		Repo   string `yaml:"repo"`
		Commit string `yaml:"commit"`
	} `yaml:"modules"`
}

// runInfo is the part of the collector's run.json the selection reads.
type runInfo struct {
	ConfigVersion string `yaml:"config_version"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches the subcommand in args and returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: rebuild select|stub|run [flags]")
		return exitUsage
	}
	var err error
	switch args[0] {
	case "select":
		err = runSelect(ctx, args[1:], stdout, stderr)
	case "stub":
		err = runStub(args[1:], stdout, stderr)
	case "run":
		err = runRuns(ctx, args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "rebuild: unknown command %q; want select, stub or run\n", args[0])
		return exitUsage
	}
	switch {
	case errors.Is(err, flag.ErrHelp):
		return exitUsage
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintln(stderr, "rebuild:", err)
		return exitUsage
	case err != nil:
		_, _ = fmt.Fprintln(stderr, "rebuild:", err)
		return exitFail
	}
	return exitOK
}

// runStub stubs one package in place.
func runStub(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("stub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "module root")
	dir := fs.String("dir", "", "package directory relative to the module root")
	want := fs.String("sha256", "", "expected tree hash; the stub is not written when it differs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		fs.Usage()
		return flag.ErrHelp
	}
	pkgDir := filepath.Join(*root, filepath.FromSlash(*dir))
	files, err := stub.Package(pkgDir)
	if err != nil {
		return err
	}
	hash := stub.TreeHash(files)
	if *want != "" && hash != *want {
		return fmt.Errorf("tree hash %s, want %s", hash, *want)
	}
	if err := stub.Write(pkgDir, files); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, hash)
	return err
}

// runSelect chooses and verifies the experiment packages and writes the
// definition and the selection report.
func runSelect(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	fs.SetOutput(stderr)
	data := fs.String("data", "calibration/data/2026-09-28-corpus", "collector output directory with packages.jsonl and run.json")
	corpus := fs.String("corpus", "calibration/corpus.yaml", "corpus file with each module's repo and pin")
	work := fs.String("work", "", "scratch directory for the clones (default: a new temporary directory)")
	out := fs.String("out", "calibration/rebuild/rebuild.yaml", "definition to write")
	reportPath := fs.String("report", "calibration/rebuild/selection.md", "selection report to write")
	perStratum := fs.Int("per-stratum", 7, "packages per stratum (tier by has_tests)")
	maxPerModule := fs.Int("max-per-module", 3, "most packages taken from one module")
	maxPasses := fs.Float64("max-passes", 10, "largest agent_passes a candidate may have")
	turnCap := fs.Int("turn-cap", 100, "turn cap of every experiment")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *work == "" {
		dir, err := os.MkdirTemp("", "astimate-rebuild-")
		if err != nil {
			return fmt.Errorf("creating scratch directory: %w", err)
		}
		*work = dir
	}
	_, _ = fmt.Fprintln(stderr, "clones in", *work)

	cfg, err := config.Parse(config.Default())
	if err != nil {
		return fmt.Errorf("default config: %w", err)
	}
	var info runInfo
	if err := readYAML(filepath.Join(*data, "run.json"), &info); err != nil {
		return err
	}
	var cf corpusFile
	if err := readYAML(*corpus, &cf); err != nil {
		return err
	}
	repos := make(map[string]string, len(cf.Modules))
	for _, m := range cf.Modules {
		if m.Repo != "" {
			repos[m.Module] = m.Repo
		}
	}
	rows, err := selection.ReadRows(filepath.Join(*data, "packages.jsonl"))
	if err != nil {
		return err
	}
	cands, err := selection.Candidates(rows, repos, cfg.Rebuild, *maxPasses)
	if err != nil {
		return err
	}
	for _, c := range cands {
		if pinned := pinOf(&cf, c.Row.Module); pinned != c.Row.Commit {
			return fmt.Errorf("%s: data commit %s is not the corpus pin %s", c.Row.Module, c.Row.Commit, pinned)
		}
	}
	gover, err := pin.GoVersion(ctx)
	if err != nil {
		return err
	}
	env := pin.DefaultEnv()
	k := pin.NewChecker(*work, env, *turnCap)
	check := func(ctx context.Context, c selection.Candidate) (definition.Experiment, error) {
		start := time.Now()
		exp, err := k.Check(ctx, c)
		verdict := "ok"
		if err != nil {
			verdict = err.Error()
		}
		_, _ = fmt.Fprintf(stderr, "%s %s (%s): %s\n", c.Tier, c.Row.Package, time.Since(start).Round(time.Second), verdict)
		return exp, err
	}
	verdicts, err := selection.Select(ctx, cands, *perStratum, *maxPerModule, check)
	if err != nil {
		return err
	}
	def := &definition.Definition{
		Note:          definitionNote,
		Source:        filepath.ToSlash(filepath.Join(*data, "packages.jsonl")),
		ConfigVersion: info.ConfigVersion,
		GoVersion:     gover,
		Env:           env,
		Selection:     definition.SelectionRule{PerStratum: *perStratum, MaxPerModule: *maxPerModule, MaxAgentPasses: *maxPasses},
	}
	for _, v := range verdicts {
		if v.OK {
			def.Experiments = append(def.Experiments, v.Experiment)
		}
	}
	if err := writeReport(*reportPath, def, verdicts, time.Now().Format(time.DateOnly)); err != nil {
		return err
	}
	if err := def.Validate(); err != nil {
		return fmt.Errorf("selection does not validate (report written to %s): %w", *reportPath, err)
	}
	enc, err := def.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append([]byte(definitionHeader), enc...), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", *out, err)
	}
	_, err = fmt.Fprintf(stdout, "%d experiments written to %s; report in %s\n", len(def.Experiments), *out, *reportPath)
	return err
}

// definitionHeader opens rebuild.yaml.
const definitionHeader = `# Rebuild experiment definition (SPEC.md 11.2). Generated by
# go run ./calibration/rebuild select; do not edit by hand. The rule, the
# stub strategy and the oracle are described in calibration/rebuild/README.md.
`

// definitionNote is the note field of rebuild.yaml.
const definitionNote = "Each experiment deletes the package's implementation (stub: signatures), " +
	"has the agent rebuild it within turn_cap turns, and passes when the oracle does, " +
	"run from the module root with env set. metrics, agent_passes, rebuild_tokens, human_days " +
	"and tier are the pre-run estimate at the pin, copied from source."

// pinOf returns the corpus pin of module.
func pinOf(cf *corpusFile, module string) string {
	for _, m := range cf.Modules {
		if m.Module == module {
			return m.Commit
		}
	}
	return ""
}

// readYAML decodes the YAML (or JSON) file at path into v.
func readYAML(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	return nil
}

// writeReport writes selection.md: the rule's outcome per stratum and every
// candidate checked, taken or rejected with the reason.
func writeReport(path string, def *definition.Definition, verdicts []selection.Verdict, date string) error {
	var b strings.Builder
	b.WriteString("# Rebuild experiment selection\n\n")
	b.WriteString("Generated by `go run ./calibration/rebuild select` on " + date + " with " + def.GoVersion +
		" (" + strings.Join(def.Env, " ") + "); do not edit by hand. The rule is in README.md.\n\n")
	fmt.Fprintf(&b, "%d experiments from %s, estimated under `%s`.\n\n", len(def.Experiments), def.Source, def.ConfigVersion)
	b.WriteString("Every taken package was checked at its pin: the oracle passed on the original tree, " +
		"stubbing twice gave byte-identical files, the stubbed module built with `go build ./...`, " +
		"and the oracle's tests built and failed on the stub.\n\n")
	b.WriteString("## Taken\n\n| Stratum | Package | agent_passes | rebuild_tokens | Oracle tests | Compiles | Tests fail |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, v := range verdicts {
		if !v.OK {
			continue
		}
		e := v.Experiment
		fmt.Fprintf(&b, "| %s | `%s` | %.1f | %d | %s | yes | yes |\n",
			v.Stratum, e.Package, e.AgentPasses, e.RebuildTokens, testsCell(e.Oracle.Test))
	}
	b.WriteString("\n## Rejected\n\n")
	rejected := 0
	for _, v := range verdicts {
		if v.OK {
			continue
		}
		rejected++
		fmt.Fprintf(&b, "- %s `%s`: %s\n", v.Stratum, v.Candidate.Row.Package,
			strings.ReplaceAll(strings.TrimPrefix(v.Reason, pin.RejectedPrefix), "|", "\\|"))
	}
	if rejected == 0 {
		b.WriteString("None.\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// testsCell shortens a long oracle test list for the report table.
func testsCell(pats []string) string {
	if len(pats) <= 3 {
		return "`" + strings.Join(pats, " ") + "`"
	}
	return fmt.Sprintf("`%s` and %d more", strings.Join(pats[:3], " "), len(pats)-3)
}
