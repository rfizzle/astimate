// Package corpus joins a commit replay's rows (calibration/replay/README.md)
// with the commit labels of calibration/replay/labels on the full commit
// hash, keeping what the gate validation scores: each labeled, loaded
// commit with its verdict, its package and module rows, their head and
// baseline metrics and the rules that fired on them.
package corpus

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/calibration/replay/labels"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Source names one corpus: its replay data directory and its labels file.
type Source struct {
	// Name is how the report names the corpus.
	Name string
	// Data is the replay's output directory, holding commits.jsonl and
	// packages.jsonl.
	Data string
	// Labels is the labels file for that directory: the first label set,
	// which every view, sweep and recommendation is scored with.
	Labels string
	// Second is an optional second labels file for the same directory,
	// the second label set, which each view is also scored with (Relabel).
	Second string
}

// ParseSource parses the --corpus form name=<data dir>:<labels
// file>[,<second labels file>]. The labels files are everything after the
// last colon.
func ParseSource(s string) (Source, error) {
	name, rest, ok := strings.Cut(s, "=")
	i := strings.LastIndexByte(rest, ':')
	if !ok || name == "" || i <= 0 || i == len(rest)-1 {
		return Source{}, fmt.Errorf("corpus %q is not name=<data dir>:<labels file>[,<second labels file>]", s)
	}
	first, second, two := strings.Cut(rest[i+1:], ",")
	if first == "" || (two && (second == "" || strings.Contains(second, ","))) {
		return Source{}, fmt.Errorf("corpus %q does not name one or two labels files", s)
	}
	return Source{Name: name, Data: rest[:i], Labels: first, Second: second}, nil
}

// LoadSecond reads src's second labels file and validates it against the
// replayed commits; nil when src has no second file.
func LoadSecond(src Source) (*labels.File, error) {
	if src.Second == "" {
		return nil, nil
	}
	return loadLabels(src.Second, src.Data)
}

// loadLabels reads the labels file at path and validates it against the
// commits the replay data directory data replayed.
func loadLabels(path, data string) (*labels.File, error) {
	lf, err := labels.Load(path)
	if err != nil {
		return nil, err
	}
	replayed, err := labels.Replayed(data)
	if err != nil {
		return nil, err
	}
	if err := lf.Validate(replayed); err != nil {
		return nil, fmt.Errorf("labels %s: %w", path, err)
	}
	return lf, nil
}

// Relabel returns a copy of c with every commit's label replaced by its
// label in byHash, so a view keeps its commits and is scored under
// another label set. RuleLabels holds when every new label has provenance
// rule. It fails when byHash has no label for one of c's commits.
func (c *Corpus) Relabel(byHash map[string]*labels.Label) (*Corpus, error) {
	out := *c
	out.Commits = make([]Commit, len(c.Commits))
	out.RuleLabels = true
	for i := range c.Commits {
		l, ok := byHash[c.Commits[i].Hash]
		if !ok {
			return nil, fmt.Errorf("view %s: commit %s has no label in the second label set", c.Name, c.Commits[i].Hash)
		}
		out.Commits[i] = c.Commits[i]
		out.Commits[i].Label = *l
		out.RuleLabels = out.RuleLabels && l.Provenance == labels.Rule
	}
	return &out, nil
}

// Row is one checked package, or the module row, of one commit.
type Row struct {
	// Package is the module-relative directory, or metrics.ModuleRowID.
	Package string
	// Language is the extractor's language id.
	Language string
	// Head and Base are the metrics at the commit and at its first parent;
	// Base is nil when the row is new.
	Head metrics.RawMetrics
	Base *metrics.RawMetrics
	// SLOCDelta is sloc at the commit minus at the parent.
	SLOCDelta int
	// Violated holds the metric of every violation the replay recorded on
	// the row, in the order recorded.
	Violated []string
	// Warned holds the metric of every breach of a warn rule the replay
	// recorded on the row, a warning with severity warn, in the order
	// recorded: what the rule would have failed at severity fail.
	Warned []string
}

// Module reports whether r is the module row.
func (r *Row) Module() bool { return r.Package == metrics.ModuleRowID }

// Breached reports whether the replay recorded a breach of the rule on
// metric on r, whatever its severity: a violation or a warn rule's
// warning.
func (r *Row) Breached(metric string) bool {
	return slices.Contains(r.Violated, metric) || slices.Contains(r.Warned, metric)
}

// Commit is one labeled commit the replay loaded.
type Commit struct {
	// Hash and Subject identify the commit.
	Hash, Subject string
	// Label is the commit's label.
	Label labels.Label
	// Passed is the replay's verdict on the commit.
	Passed bool
	// Rows are the commit's checked rows in replay order.
	Rows []Row
}

// Block reports whether the commit is labeled block.
func (c *Commit) Block() bool { return c.Label.Verdict == labels.Block }

// Failed reports whether any row of the commit has a recorded violation.
func (c *Commit) Failed() bool {
	for i := range c.Rows {
		if len(c.Rows[i].Violated) > 0 {
			return true
		}
	}
	return false
}

// CapacityOnly reports whether c is a block label resting on capacity
// alone: it names rules, every one of them capacity (capacity reports
// which metrics are), and it cites no revert or fix-up, neither as rule
// evidence nor as a reason starting "fixed up by" or "reverted by". Such a
// label reads the sizes the capacity rules read, so the gate is bound to
// agree with it (calibration/notes/astimate-labels-2026-09-28.md).
func (c *Commit) CapacityOnly(capacity func(metric string) bool) bool {
	l := &c.Label
	if l.Verdict != labels.Block || len(l.Metrics) == 0 || len(l.Fired) > 0 {
		return false
	}
	if strings.HasPrefix(l.Reason, "fixed up by") || strings.HasPrefix(l.Reason, "reverted by") {
		return false
	}
	return !slices.ContainsFunc(l.Metrics, func(m string) bool { return !capacity(m) })
}

// Corpus is the labeled, loaded commits of one or more replays.
type Corpus struct {
	// Name is how the report names the corpus.
	Name string
	// Commits are the commits scored, in replay order.
	Commits []Commit
	// Labeled is the number of labels in the file; NotLoaded counts the
	// labeled commits the replay did not load and NotAgent those dropped
	// for agent: false.
	Labeled, NotLoaded, NotAgent int
	// ConfigVersion is the configuration the replay gated with.
	ConfigVersion string
	// RuleLabels is true when every label has provenance rule.
	RuleLabels bool
	// Rule is the labels file's rule text, empty for hand labels; a pool
	// keeps its first part's.
	Rule string
}

// Counts returns the numbers of block and allow commits in c.
func (c *Corpus) Counts() (block, allow int) {
	for i := range c.Commits {
		if c.Commits[i].Block() {
			block++
		} else {
			allow++
		}
	}
	return block, allow
}

// Without returns a copy of c named name without the commits drop
// reports true for.
func (c *Corpus) Without(name string, drop func(*Commit) bool) *Corpus {
	out := *c
	out.Name = name
	out.Commits = make([]Commit, 0, len(c.Commits))
	for i := range c.Commits {
		if !drop(&c.Commits[i]) {
			out.Commits = append(out.Commits, c.Commits[i])
		}
	}
	return &out
}

// Pool returns the commits of every corpus of cs as one corpus named
// name. Its RuleLabels holds when it does for every part.
func Pool(name string, cs ...*Corpus) *Corpus {
	out := &Corpus{Name: name, RuleLabels: len(cs) > 0}
	for _, c := range cs {
		out.Commits = append(out.Commits, c.Commits...)
		out.Labeled += c.Labeled
		out.NotLoaded += c.NotLoaded
		out.NotAgent += c.NotAgent
		out.RuleLabels = out.RuleLabels && c.RuleLabels
		if out.ConfigVersion == "" {
			out.ConfigVersion, out.Rule = c.ConfigVersion, c.Rule
		}
	}
	return out
}

// Load reads src's replay rows and labels, validates the labels against
// the replayed commits, and joins them. With agentOnly, a label with
// agent: false is dropped; a label that does not classify is kept. A
// commit the replay did not load has no verdict and is left out.
func Load(src Source, agentOnly bool) (*Corpus, error) {
	lf, err := loadLabels(src.Labels, src.Data)
	if err != nil {
		return nil, err
	}
	rows, err := readRows(filepath.Join(src.Data, "packages.jsonl"))
	if err != nil {
		return nil, err
	}
	c := &Corpus{Name: src.Name, Labeled: len(lf.Commits), RuleLabels: true, Rule: lf.Rule}
	for i := range lf.Commits {
		c.RuleLabels = c.RuleLabels && lf.Commits[i].Provenance == labels.Rule
	}
	byHash := lf.ByHash()
	err = eachLine(filepath.Join(src.Data, "commits.jsonl"), func(line []byte) error {
		var cr commitRow
		if err := json.Unmarshal(line, &cr); err != nil {
			return err
		}
		return c.add(&cr, byHash[cr.Commit], rows[cr.Commit], agentOnly)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// add scores one commit row with its label and package rows.
func (c *Corpus) add(cr *commitRow, l *labels.Label, rows []Row, agentOnly bool) error {
	if c.ConfigVersion == "" {
		c.ConfigVersion = cr.ConfigVersion
	}
	switch {
	case cr.ConfigVersion != c.ConfigVersion:
		return fmt.Errorf("commit %s was gated under %s, not %s", cr.Commit, cr.ConfigVersion, c.ConfigVersion)
	case !cr.Loaded:
		c.NotLoaded++
		return nil
	case agentOnly && l.Agent != nil && !*l.Agent:
		c.NotAgent++
		return nil
	}
	if cr.Baseline == "empty" {
		zeroModuleBase(rows)
	}
	cm := Commit{Hash: cr.Commit, Subject: cr.Subject, Label: *l, Rows: rows}
	if cr.Passed != nil {
		cm.Passed = *cr.Passed
	}
	if cm.Passed == cm.Failed() {
		return fmt.Errorf("commit %s: passed is %t but its rows record %t violations", cr.Commit, cm.Passed, cm.Failed())
	}
	c.Commits = append(c.Commits, cm)
	return nil
}

// zeroModuleBase gives the module row of a commit judged against an empty
// baseline the zero module row the replay's empty baseline file holds, so
// its module-wide rules are re-evaluated against zero as the replay gated
// them; the row records it as new, with no base.
func zeroModuleBase(rows []Row) {
	for i := range rows {
		if rows[i].Module() && rows[i].Base == nil {
			zero := 0
			rows[i].Base = &metrics.RawMetrics{DupBlocksCrossPkg: &zero}
		}
	}
}

// commitRow is the part of a commits.jsonl line the join reads.
type commitRow struct {
	Commit        string `json:"commit"`
	Subject       string `json:"subject"`
	ConfigVersion string `json:"config_version"`
	Baseline      string `json:"baseline"`
	Loaded        bool   `json:"loaded"`
	Passed        *bool  `json:"passed"`
}

// packageRow is the part of a packages.jsonl line the join reads.
type packageRow struct {
	Commit     string              `json:"commit"`
	Package    string              `json:"package"`
	Language   string              `json:"language"`
	Metrics    metrics.RawMetrics  `json:"metrics"`
	Base       *metrics.RawMetrics `json:"base"`
	SLOCDelta  int                 `json:"sloc_delta"`
	Violations []struct {
		Metric string `json:"metric"`
	} `json:"violations"`
	Warnings []struct {
		Metric   string `json:"metric"`
		Severity string `json:"severity"`
	} `json:"warnings"`
}

// readRows reads packages.jsonl at path, keyed by commit hash.
func readRows(path string) (map[string][]Row, error) {
	out := make(map[string][]Row)
	err := eachLine(path, func(line []byte) error {
		var pr packageRow
		if err := json.Unmarshal(line, &pr); err != nil {
			return err
		}
		r := Row{Package: pr.Package, Language: pr.Language, Head: pr.Metrics, Base: pr.Base, SLOCDelta: pr.SLOCDelta}
		for _, v := range pr.Violations {
			r.Violated = append(r.Violated, v.Metric)
		}
		r.Warned = pr.warned()
		out[pr.Commit] = append(out[pr.Commit], r)
		return nil
	})
	return out, err
}

// warned returns the metric of each of pr's warnings that is a warn
// rule's breach, one with severity warn, in the order recorded.
func (pr *packageRow) warned() []string {
	var out []string
	for i := range pr.Warnings {
		if pr.Warnings[i].Severity == "warn" {
			out = append(out, pr.Warnings[i].Metric)
		}
	}
	return out
}

// eachLine calls fn with every non-blank line of the file at path,
// wrapping an error with the path and line number.
func eachLine(path string, fn func([]byte) error) (err error) {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for n := 1; sc.Scan(); n++ {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		if err := fn(sc.Bytes()); err != nil {
			return fmt.Errorf("reading %s line %d: %w", path, n, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	return nil
}
