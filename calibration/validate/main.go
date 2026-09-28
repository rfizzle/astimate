// Command validate measures the gate on labeled commits (SPEC.md 11.3 and
// 14): it joins commit replays' rows with their labels, reports the share
// of block commits the gate failed and of allow commits it failed against
// the targets, each rule's precision and recall per corpus beside a
// size-only rule on sloc_delta, sweeps each rule's limits, recommends
// keep, retune or drop per rule, and lists the block commits it missed.
//
// Usage, from the repository root:
//
//	go run ./calibration/validate --corpus <name>=<replay dir>:<labels file> [--corpus ...] \
//	    [--config <file>] [--out <report.md>] [--agent-only=false] [--date YYYY-MM-DD]
//
// The configuration must be the one the replays gated with (the rows'
// config_version): which rules fired is read from the rows' recorded
// violations, and the sweeps re-evaluate each rule from the rows' stored
// metrics with internal/gate.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/rfizzle/astimate/calibration/validate/internal/corpus"
	"github.com/rfizzle/astimate/calibration/validate/internal/measure"
	"github.com/rfizzle/astimate/calibration/validate/internal/report"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate"
)

// options are the parsed command-line flags and the fixed criteria.
type options struct {
	sources     []corpus.Source
	config, out string
	// thresholdsDir holds the committed threshold candidates, by which a
	// replay made under thresholds-<date> is matched to the rules it used.
	thresholdsDir string
	date          string
	agentOnly     bool
	targets       report.Targets
	sizeGrid      []int
	criteria      measure.Criteria
}

// usageError is a command line the command cannot run.
type usageError struct{ error }

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr)
	var usage usageError
	switch {
	case errors.As(err, &usage):
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "validate:", err)
		os.Exit(1)
	}
}

// run parses args, measures, and writes the report. A usage error has
// already been reported on stderr.
func run(args []string, stdout, stderr io.Writer) error {
	o := newOptions()
	if err := o.parse(args, stderr); err != nil {
		return usageError{err}
	}
	in, err := measureAll(&o)
	if err != nil {
		return err
	}
	if err := os.WriteFile(o.out, []byte(report.Render(in)), 0o644); err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}
	last := in.Views[len(in.Views)-1]
	_, err = fmt.Fprintf(stdout, "wrote %s: %s recall %.1f%%, false failures %.1f%%\n",
		o.out, last.Name, 100*last.Gate.Recall(), 100*last.Gate.FalseFailure())
	return err
}

// newOptions returns the defaults: the SPEC.md 14 targets, the size-only
// thresholds swept and the recommendation criteria, whose false-failure
// budget per rule is the whole gate's target.
func newOptions() options {
	t := report.Targets{Recall: 0.8, FalseFailure: 0.1}
	return options{
		agentOnly:     true,
		thresholdsDir: filepath.Join("calibration", "thresholds"),
		date:          time.Now().Format(time.DateOnly),
		targets:       t,
		sizeGrid:      []int{0, 25, 50, 100, 150, 200, 300, 400, 500, 750, 1000, 1500, 2000},
		criteria:      measure.Criteria{MinFired: 10, Budget: t.FalseFailure, MinJ: 0.02},
	}
}

// parse reads the flags of args into o and checks them, reporting any
// problem on stderr.
func (o *options) parse(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Func("corpus", "a corpus as name=<replay dir>:<labels file>; repeat for each", func(s string) error {
		src, err := corpus.ParseSource(s)
		o.sources = append(o.sources, src)
		return err
	})
	fs.StringVar(&o.config, "config", "", "configuration the replays gated with; empty means the embedded default")
	fs.BoolVar(&o.agentOnly, "agent-only", o.agentOnly, "drop labels with agent: false")
	fs.StringVar(&o.out, "out", "", "report file; empty means calibration/reports/gate-validation-<date>.md")
	fs.StringVar(&o.date, "date", o.date, "report date, YYYY-MM-DD")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var err error
	if _, derr := time.Parse(time.DateOnly, o.date); derr != nil {
		err = fmt.Errorf("--date: %w", derr)
	}
	if len(o.sources) == 0 {
		err = errors.Join(err, errors.New("at least one --corpus is required"))
	}
	if fs.NArg() > 0 {
		err = errors.Join(err, fmt.Errorf("unexpected argument %q", fs.Arg(0)))
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "validate:", err)
		return err
	}
	if o.out == "" {
		o.out = "calibration/reports/gate-validation-" + o.date + ".md"
	}
	return nil
}

// loadConfig reads the configuration o names, or the embedded default,
// and says where it came from.
func loadConfig(o *options) (*config.Config, string, error) {
	if o.config != "" {
		cfg, err := config.Load(o.config)
		return cfg, "`" + o.config + "`", err
	}
	cfg, err := config.Parse(config.Default())
	return cfg, "the embedded default", err
}

// gatesAs reports whether cfg gates as the configuration a replay recorded
// as version did: the same config_version, or, for a thresholds-<date>
// version, the same rules as its candidate committed under dir. A rebuild
// fit (rebuild-<date>-<agent>) changes the estimate's parameters and never
// a rule, so replays made under the thresholds it was fitted on still hold.
func gatesAs(cfg *config.Config, version, dir string) bool {
	if cfg.Version == version {
		return true
	}
	if !strings.HasPrefix(version, "thresholds-") {
		return false
	}
	prev, err := config.Load(filepath.Join(dir, "astimate-"+version+".yaml"))
	return err == nil && reflect.DeepEqual(prev.Thresholds, cfg.Thresholds)
}

// measureAll loads every corpus and computes the report's input.
func measureAll(o *options) (*report.Input, error) {
	cfg, from, err := loadConfig(o)
	if err != nil {
		return nil, err
	}
	rules := measure.NewRules(cfg)
	capacity := func(m string) bool {
		for _, r := range rules.List {
			if r.Threshold.Metric == m && r.Threshold.Kind == gate.Capacity {
				return true
			}
		}
		return false
	}
	in := &report.Input{Date: o.date, Version: cfg.Version, ConfigSource: from}
	in.AgentOnly, in.Targets, in.Rules, in.Criteria = o.agentOnly, o.targets, rules.List, o.criteria
	loaded := make([]*corpus.Corpus, 0, len(o.sources))
	for _, src := range o.sources {
		c, err := corpus.Load(src, o.agentOnly)
		if err == nil && !gatesAs(cfg, c.ConfigVersion, o.thresholdsDir) {
			err = fmt.Errorf("corpus %s was replayed under %s, not %s, and their rules differ", src.Name, c.ConfigVersion, cfg.Version)
		}
		var s *measure.Scored
		if err == nil {
			s, err = measure.Score(c, rules)
		}
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, c)
		in.Corpora = append(in.Corpora, describe(src, s, capacity))
	}
	views := viewsOf(loaded, capacity)
	scored := make([]*measure.Scored, len(views))
	for i, v := range views {
		if scored[i], err = measure.Score(v, rules); err != nil {
			return nil, err
		}
	}
	fill(in, scored, o)
	return in, nil
}

// describe counts one scored corpus's capacity-only labels and rechecks
// its rows.
func describe(src corpus.Source, s *measure.Scored, capacity func(string) bool) report.Corpus {
	rc := report.Corpus{Source: src, Loaded: s.Corpus}
	for i := range s.Corpus.Commits {
		if s.Corpus.Commits[i].CapacityOnly(capacity) {
			rc.SetAside++
		}
	}
	rc.Checked, rc.Mismatched = s.Recheck()
	return rc
}

// viewsOf returns the views the report scores: each corpus, followed by
// itself without capacity-only block labels when it has any; the pool of
// the rule-labeled corpora when there are two or more; and, for more than
// one corpus, the pool of every corpus, followed, when any label was set
// aside, by that pool with them set aside. The last view is the basis for
// the size threshold, the sweeps and the advice.
func viewsOf(cs []*corpus.Corpus, capacity func(string) bool) []*corpus.Corpus {
	capOnly := func(c *corpus.Commit) bool { return c.CapacityOnly(capacity) }
	var views, honest, rule []*corpus.Corpus
	setAside := false
	for _, c := range cs {
		views = append(views, c)
		h := c.Without(c.Name+", capacity-only set aside", capOnly)
		if len(h.Commits) < len(c.Commits) {
			views = append(views, h)
			setAside = true
		}
		honest = append(honest, h)
		if c.RuleLabels {
			rule = append(rule, c)
		}
	}
	if len(rule) > 1 {
		views = append(views, corpus.Pool("rule-labeled corpora pooled", rule...))
	}
	if len(cs) > 1 {
		views = append(views, corpus.Pool("all corpora pooled", cs...))
		if setAside {
			views = append(views, corpus.Pool("all corpora pooled, capacity-only set aside", honest...))
		}
	}
	return views
}

// fill computes, on the last view, the size-only threshold, each rule's
// sweeps and advice; then every view's rates, with the advice applied
// too; and each corpus's missed block commits.
func fill(in *report.Input, scored []*measure.Scored, o *options) {
	basis := scored[len(scored)-1]
	in.SizeCurve = basis.SizeCurve(o.sizeGrid)
	best, met := measure.BestSize(in.SizeCurve, o.targets.FalseFailure)
	in.SizeT, in.SizeMet = best.T, met
	advised := make(map[int]*gate.Threshold)
	for i, r := range in.Rules {
		curves := basis.Sweep(i)
		_, dropped := basis.Swap(i, nil)
		a := measure.Advise(basis.Rule(i), basis.Judged(i), curves, o.criteria)
		in.Curves, in.Dropped, in.Advice = append(in.Curves, curves), append(in.Dropped, dropped), append(in.Advice, a)
		switch a.Action {
		case measure.Drop:
			advised[i] = nil
		case measure.Retune:
			t := measure.Apply(r.Threshold, a.To)
			advised[i] = &t
		}
		if r.Threshold.Kind == gate.Capacity && basis.Judged(i) > 0 {
			fired, over := basis.Legacy(i)
			in.Legacy = append(in.Legacy, report.Legacy{Rule: i, Fired: fired, Over: over})
		}
	}
	for _, s := range scored {
		v := report.View{Name: s.Corpus.Name, Gate: s.Gate(), Advised: s.With(advised), Size: s.Size(in.SizeT)}
		for i := range in.Rules {
			v.Rules, v.Judged = append(v.Rules, s.Rule(i)), append(v.Judged, s.Judged(i))
		}
		in.Views = append(in.Views, v)
	}
	for _, c := range in.Corpora {
		for _, s := range scored {
			if s.Corpus == c.Loaded {
				in.Missed = append(in.Missed, report.Missed{Corpus: c.Source.Name, Commits: s.Missed()})
			}
		}
	}
}
