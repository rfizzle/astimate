package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/rfizzle/astimate/calibration/internal/gocache"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/pin"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/selection"
	"github.com/rfizzle/astimate/internal/config"
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

// selectOptions are the select command's flags.
type selectOptions struct {
	unit, data, corpus, work, config, out, report string
	rule                                          definition.SelectionRule
	turnCap                                       int
}

// parseSelectFlags parses the select command's flags and fills the
// defaults that depend on the unit.
func parseSelectFlags(stderr io.Writer, args []string) (*selectOptions, error) {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	fs.SetOutput(stderr)
	unit := fs.String("unit", definition.UnitPackage, "what an experiment rebuilds: package or tree")
	data := fs.String("data", "calibration/data/2026-09-28-corpus", "collector output directory with packages.jsonl and run.json")
	corpus := fs.String("corpus", "calibration/corpus.yaml", "corpus file with each module's repo and pin")
	work := fs.String("work", "", "scratch directory for the clones (default: a new temporary directory)")
	cfg := fs.String("config", "", "configuration the data was collected under "+
		"(default calibration/thresholds/astimate-<config_version of the data>.yaml)")
	out := fs.String("out", "", "definition to write (default calibration/rebuild/rebuild.yaml, or rebuild-trees.yaml for trees)")
	report := fs.String("report", "", "selection report to write (default calibration/rebuild/selection.md, or selection-trees.md)")
	perStratum := fs.Int("per-stratum", 0, "experiments per stratum (tier by has_tests); default 7, or 3 for trees")
	untested := fs.Int("per-stratum-untested", 0, "experiments per untested stratum; default per-stratum, or 2 for trees")
	perModule := fs.Int("max-per-module", 0, "most experiments taken from one module; default 3, or 2 for trees")
	passes := fs.Float64("max-passes", 0, "largest agent_passes a candidate may have; default 10, or 12 for trees")
	minPkgs := fs.Int("min-packages", 2, "fewest packages in a tree")
	maxPkgs := fs.Int("max-packages", 8, "most packages in a tree")
	turnCap := fs.Int("turn-cap", 100, "turn cap of every experiment")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *unit != definition.UnitPackage && *unit != definition.UnitTree {
		return nil, fmt.Errorf("%w: --unit %q is not package or tree", errUsage, *unit)
	}
	tree := *unit == definition.UnitTree
	o := &selectOptions{unit: *unit, data: *data, corpus: *corpus, work: *work, config: *cfg, turnCap: *turnCap,
		out:    orUnit(*out, "calibration/rebuild/rebuild.yaml", "calibration/rebuild/rebuild-trees.yaml", tree),
		report: orUnit(*report, "calibration/rebuild/selection.md", "calibration/rebuild/selection-trees.md", tree),
		rule: definition.SelectionRule{
			PerStratum: orUnit(*perStratum, 7, 3, tree), PerStratumUntested: orUnit(*untested, 0, 2, tree),
			MaxPerModule: orUnit(*perModule, 3, 2, tree), MaxAgentPasses: orUnit(*passes, 10, 12, tree),
		},
	}
	if tree {
		o.rule.MinPackages, o.rule.MaxPackages = *minPkgs, *maxPkgs
	}
	if o.work != "" {
		return o, nil
	}
	var err error
	if o.work, err = os.MkdirTemp("", "astimate-rebuild-select-"); err != nil {
		return o, fmt.Errorf("creating scratch directory: %w", err)
	}
	return o, nil
}

// orUnit returns v when it is set, else the default of the unit: pkg, or
// tree when isTree.
func orUnit[T comparable](v, pkg, tree T, isTree bool) T {
	var zero T
	switch {
	case v != zero:
		return v
	case isTree:
		return tree
	}
	return pkg
}

// cmpOr returns v, or def when v is empty.
func cmpOr(v, def string) string {
	return orUnit(v, def, def, false)
}

// runSelect chooses and verifies the experiments and writes the
// definition and the selection report.
func runSelect(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseSelectFlags(stderr, args)
	if err == nil {
		_, _ = fmt.Fprintln(stderr, "clones in", o.work)
		err = selectAndWrite(ctx, o, stdout, stderr)
	}
	return err
}

// selectAndWrite runs the selection o asks for and writes its definition
// and report.
func selectAndWrite(ctx context.Context, o *selectOptions, stdout, stderr io.Writer) error {
	var info runInfo
	if err := readYAML(filepath.Join(o.data, "run.json"), &info); err != nil {
		return err
	}
	cfgPath := cmpOr(o.config, filepath.Join("calibration", "thresholds", "astimate-"+info.ConfigVersion+".yaml"))
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("reading the data's configuration: %w", err)
	}
	cfg, err := config.Parse(cfgData)
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	if cfg.Version != info.ConfigVersion {
		return fmt.Errorf("%s is %s, but the data was collected under %s", cfgPath, cfg.Version, info.ConfigVersion)
	}
	var cf corpusFile
	if err := readYAML(o.corpus, &cf); err != nil {
		return err
	}
	rows, err := selection.ReadRows(filepath.Join(o.data, "packages.jsonl"))
	if err != nil {
		return err
	}
	gover, err := pin.GoVersion(ctx)
	if err != nil {
		return err
	}
	env := pin.DefaultEnv()
	// The verification builds every candidate module and its tests; its
	// build cache lives in the scratch directory with the clones, not in
	// the shared one. The definition records env alone.
	cache, err := gocache.Env(o.work)
	if err != nil {
		return err
	}
	k := pin.NewChecker(o.work, append(slices.Clip(env), cache...), o.turnCap)
	cands, check, skipped, err := candidates(ctx, o, &cf, rows, cfg, k, stderr)
	if err != nil {
		return err
	}
	logged := func(ctx context.Context, c selection.Candidate) (definition.Experiment, error) {
		start := time.Now()
		exp, err := check(ctx, c)
		verdict := "ok"
		if err != nil {
			verdict = err.Error()
		}
		_, _ = fmt.Fprintf(stderr, "%s %s (%s): %s\n", c.Tier, c.Row.Package, time.Since(start).Round(time.Second), verdict)
		return exp, err
	}
	verdicts, err := selection.Select(ctx, cands, o.rule, logged)
	if err != nil {
		return err
	}
	def := &definition.Definition{
		Note:          definitionNote,
		Source:        filepath.ToSlash(filepath.Join(o.data, "packages.jsonl")),
		ConfigVersion: info.ConfigVersion,
		GoVersion:     gover,
		Env:           env,
		Selection:     o.rule,
	}
	header := definitionHeader
	if o.unit == definition.UnitTree {
		def.Unit, def.EstimateMethod, def.Note, header = definition.UnitTree, definition.EstimateSum, treeNote, treeHeader
	}
	for _, v := range verdicts {
		if v.OK {
			def.Experiments = append(def.Experiments, v.Experiment)
		}
	}
	if err := writeReport(o.report, def, verdicts, skipped, time.Now().Format(time.DateOnly)); err != nil {
		return err
	}
	if err := def.Validate(); err != nil {
		return fmt.Errorf("selection does not validate (report written to %s): %w", o.report, err)
	}
	enc, err := def.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(o.out, append([]byte(header), enc...), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", o.out, err)
	}
	_, err = fmt.Fprintf(stdout, "%d experiments written to %s; report in %s\n", len(def.Experiments), o.out, o.report)
	return err
}

// checkFunc verifies one candidate at its pin.
type checkFunc = func(ctx context.Context, c selection.Candidate) (definition.Experiment, error)

// candidates returns the unit's candidates under cfg's rebuild parameters,
// each at its corpus pin, and the check that verifies one. For trees it
// clones every module a tree may come from, to leave its main packages
// out; skipped lists the modules that could not be, with the reason.
func candidates(ctx context.Context, o *selectOptions, cf *corpusFile, rows []selection.Row, cfg *config.Config,
	k *pin.Checker, stderr io.Writer) (cands []selection.Candidate, check checkFunc, skipped []string, err error) {
	repos := make(map[string]string, len(cf.Modules))
	for _, m := range cf.Modules {
		if m.Repo != "" {
			repos[m.Module] = m.Repo
		}
	}
	if o.unit == definition.UnitPackage {
		cands, err = selection.Candidates(rows, repos, cfg.Rebuild, o.rule.MaxAgentPasses)
		check = k.Check
	} else {
		cands, skipped, err = treeCandidates(ctx, o, cf, rows, repos, cfg, k, stderr)
		check = k.CheckTree
	}
	if err != nil {
		return nil, nil, nil, err
	}
	for _, c := range cands {
		if pinned := pinOf(cf, c.Row.Module); pinned != c.Row.Commit {
			return nil, nil, nil, fmt.Errorf("%s: data commit %s is not the corpus pin %s", c.Row.Module, c.Row.Commit, pinned)
		}
	}
	return cands, check, skipped, nil
}

// treeCandidates returns the tree candidates of rows. It first finds the
// modules a tree may come from, then clones each to find its main
// packages, which trees leave out; a module that cannot be cloned or does
// not build is dropped, and skipped says why.
func treeCandidates(ctx context.Context, o *selectOptions, cf *corpusFile, rows []selection.Row, repos map[string]string,
	cfg *config.Config, k *pin.Checker, stderr io.Writer) (cands []selection.Candidate, skipped []string, err error) {
	cands, err = selection.TreeCandidates(rows, repos, nil, cfg.Rebuild, o.rule)
	if err != nil {
		return nil, nil, err
	}
	mods := map[string]bool{}
	for _, c := range cands {
		mods[c.Row.Module] = true
	}
	mains := map[string]bool{}
	for _, mod := range slices.Sorted(maps.Keys(mods)) {
		found, merr := k.Mains(ctx, mod, repos[mod], pinOf(cf, mod))
		if merr != nil {
			_, _ = fmt.Fprintf(stderr, "%s: %v\n", mod, merr)
			skipped = append(skipped, "module `"+mod+"`: "+strings.TrimPrefix(merr.Error(), pin.RejectedPrefix))
			delete(repos, mod)
		}
		maps.Copy(mains, found)
	}
	cands, err = selection.TreeCandidates(rows, repos, mains, cfg.Rebuild, o.rule)
	return cands, skipped, err
}

// definitionHeader opens rebuild.yaml.
const definitionHeader = `# Rebuild experiment definition (SPEC.md 11.2). Generated by
# go run ./calibration/rebuild select; do not edit by hand. The rule, the
# stub strategy and the oracle are described in calibration/rebuild/README.md.
`

// treeHeader opens rebuild-trees.yaml.
const treeHeader = `# Rebuild experiment definition of directory trees (SPEC.md 11.2).
# Generated by go run ./calibration/rebuild select --unit tree; do not edit
# by hand. The rule, the stub strategy and the oracle are described in
# calibration/rebuild/README.md.
`

// definitionNote is the note field of rebuild.yaml.
const definitionNote = "Each experiment deletes the package's implementation (stub: signatures), " +
	"has the agent rebuild it within turn_cap turns, and passes when the oracle does, " +
	"run from the module root with env set. metrics, agent_passes, rebuild_tokens, human_days " +
	"and tier are the pre-run estimate at the pin, copied from source."

// treeNote is the note field of rebuild-trees.yaml.
const treeNote = "Each experiment deletes the implementation of a directory's package and of every " +
	"non-main package below it (stub: signatures), has the agent rebuild them together within turn_cap " +
	"turns, and passes when the oracle does, run from the module root with env set. Each member carries " +
	"its own metrics and pre-run estimate, copied from source; the tree's rebuild_tokens and human_days are " +
	"the members' sums, its agent_passes and tier the sum passed through the SPEC.md 7.2 curve once " +
	"(estimate_method: sum), and its metrics the members' aggregate."

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
