// Package summary reads a replay output directory (package replay) for
// `astimate dryrun`: it summarizes how often the gate would have failed,
// writes the rows back out as JSON lines, and suggests a configuration
// that warns on the rules over a failure budget. It reads only the row
// fields it needs, as calibration/validate does.
package summary

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/rfizzle/astimate/internal/replay"
)

// TopPackages is the number of most-failed packages a Summary lists.
const TopPackages = 5

// Names of the replay's row files inside its output directory.
const (
	commitsFile  = replay.CommitsFile
	packagesFile = replay.PackagesFile
)

// commitRow is the part of a commits.jsonl line a summary reads.
type commitRow struct {
	Commit          string   `json:"commit"`
	Subject         string   `json:"subject"`
	Loaded          bool     `json:"loaded"`
	Error           string   `json:"error"`
	PackagesChanged []string `json:"packages_changed"`
	PackagesDeleted []string `json:"packages_deleted"`
	Passed          *bool    `json:"passed"`
}

// packageRow is the part of a packages.jsonl line a summary reads.
type packageRow struct {
	Commit     string `json:"commit"`
	Package    string `json:"package"`
	Violations []struct {
		Metric string   `json:"metric"`
		Also   []string `json:"also"`
	} `json:"violations"`
}

// Summary is what a replay's rows say the gate would have done over the
// history replayed: how many commits it would have failed, by which
// rules, in which packages, and which commits it could not judge.
type Summary struct {
	// Commits counts the commits recorded.
	Commits int
	// NotLoaded are the recorded commits the replay could not check, in
	// range order: the module did not load, the baseline could not be
	// extracted, or the check selected a package that does not load.
	NotLoaded []NotLoaded
	// Checked counts the commits the replay checked: every recorded commit
	// but those NotLoaded. Touched counts those of them that changed or
	// deleted at least one package, and Failed those the gate failed,
	// every one of which touched a package. Failed over Checked is the
	// share calibration/validate reports for the same rows: it scores
	// every loaded commit.
	Checked, Touched, Failed int
	// Rules are the rules that failed at least one commit, the most
	// commits first, then by metric.
	Rules []RuleCount
	// Packages are the TopPackages packages (the module row included)
	// with a violation in the most commits, the most first, then by
	// package.
	Packages []PackageCount

	// merged records, per rule, the also lists of its own violations.
	merged map[string]*mergedUse
}

// mergedUse is how a rule's own violations were reported: whether one
// stood alone, and the distinct also lists of the rest.
type mergedUse struct {
	bare  bool
	alsos [][]string
}

// add records a violation of the rule with the also list also.
func (m *mergedUse) add(also []string) {
	if len(also) == 0 {
		m.bare = true
		return
	}
	for _, a := range m.alsos {
		if slices.Equal(a, also) {
			return
		}
	}
	m.alsos = append(m.alsos, also)
}

// NotLoaded is one commit the replay could not check.
type NotLoaded struct {
	Commit, Subject, Error string
}

// RuleCount is one rule's failures. A rule is named by its metric: every
// threshold on the metric, at the top level and in a languages override.
type RuleCount struct {
	Metric string
	// Failed counts the commits with a violation of the rule, its own or
	// one the gate merged into another rule's finding of the same cause
	// (its also list, SPEC.md 8.1); Alone those whose violations are of
	// this rule only, which the rule at severity warn would have passed.
	Failed, Alone int
}

// PackageCount is one package's failures: the commits in which its row
// had a violation.
type PackageCount struct {
	Package string
	Failed  int
}

// Share returns n as a share of the commits checked, 0 when none was.
func (s *Summary) Share(n int) float64 {
	return share(n, s.Checked)
}

// TouchedShare returns n as a share of the commits that touched a
// package, 0 when none did.
func (s *Summary) TouchedShare(n int) float64 {
	return share(n, s.Touched)
}

// share is n over d, or 0 when d is 0.
func share(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// OverBudget returns the metrics of the rules whose failed share is over
// budget, a share between 0 and 1, sorted.
func (s *Summary) OverBudget(budget float64) []string {
	var out []string
	for _, r := range s.Rules {
		if s.Share(r.Failed) > budget {
			out = append(out, r.Metric)
		}
	}
	slices.Sort(out)
	return out
}

// Warn returns the metrics a suggestion sets to warn for budget, a share
// between 0 and 1, sorted: those of OverBudget, and each other rule whose
// every violation was merged into by a rule over budget (SPEC.md 8.1,
// the over-budget metric is in its also list). A merge attributes the
// finding to the primary rule, so the over-budget rule at warn would
// leave the primary failing the commit; the primary has to warn too.
func (s *Summary) Warn(budget float64) []string {
	over := s.OverBudget(budget)
	out := slices.Clone(over)
	for metric, use := range s.merged {
		if slices.Contains(over, metric) || use.bare || len(use.alsos) == 0 {
			continue
		}
		if slices.ContainsFunc(use.alsos, func(also []string) bool {
			return !slices.ContainsFunc(also, func(m string) bool { return slices.Contains(over, m) })
		}) {
			continue
		}
		out = append(out, metric)
	}
	slices.Sort(out)
	return out
}

// Summarize reads the rows of the replay output directory dir and
// summarizes them.
func Summarize(dir string) (*Summary, error) {
	commits, err := readLines[commitRow](filepath.Join(dir, commitsFile))
	if err != nil {
		return nil, err
	}
	pkgs, err := readLines[packageRow](filepath.Join(dir, packagesFile))
	if err != nil {
		return nil, err
	}
	byCommit := make(map[string][]*packageRow, len(commits))
	for i := range pkgs {
		byCommit[pkgs[i].Commit] = append(byCommit[pkgs[i].Commit], &pkgs[i])
	}
	s := &Summary{Commits: len(commits), merged: map[string]*mergedUse{}}
	failed, alone, packages := map[string]int{}, map[string]int{}, map[string]int{}
	for i := range commits {
		c := &commits[i]
		if !c.Loaded {
			s.NotLoaded = append(s.NotLoaded, NotLoaded{Commit: c.Commit, Subject: c.Subject, Error: c.Error})
			continue
		}
		s.Checked++
		if len(c.PackagesChanged) > 0 || len(c.PackagesDeleted) > 0 {
			s.Touched++
		}
		if c.Passed != nil && !*c.Passed {
			s.Failed++
		}
		broken := countPackages(byCommit[c.Commit], packages, s.merged)
		for _, m := range broken {
			failed[m]++
		}
		if len(broken) == 1 {
			alone[broken[0]]++
		}
	}
	for _, m := range mostFirst(failed) {
		s.Rules = append(s.Rules, RuleCount{Metric: m, Failed: failed[m], Alone: alone[m]})
	}
	top := mostFirst(packages)
	for _, p := range top[:min(len(top), TopPackages)] {
		s.Packages = append(s.Packages, PackageCount{Package: p, Failed: packages[p]})
	}
	return s, nil
}

// mostFirst returns the keys of counts, the largest count first, equal
// counts by key.
func mostFirst(counts map[string]int) []string {
	keys := slices.Sorted(maps.Keys(counts))
	slices.SortStableFunc(keys, func(a, b string) int { return cmp.Compare(counts[b], counts[a]) })
	return keys
}

// countPackages counts each of rows, one commit's rows, that has a
// violation into packages, records each violation's also list in merged,
// and returns the metrics of the rules the
// commit's violations broke, merged rules included, sorted.
func countPackages(rows []*packageRow, packages map[string]int, merged map[string]*mergedUse) []string {
	var metrics []string
	for _, r := range rows {
		if len(r.Violations) == 0 {
			continue
		}
		packages[r.Package]++
		for _, v := range r.Violations {
			if merged[v.Metric] == nil {
				merged[v.Metric] = &mergedUse{}
			}
			merged[v.Metric].add(v.Also)
			metrics = append(metrics, v.Metric)
			metrics = append(metrics, v.Also...)
		}
	}
	slices.Sort(metrics)
	return slices.Compact(metrics)
}

// WriteRows writes the rows of the replay output directory dir to w as
// JSON lines in the order the replay recorded them: for each commit in
// range order, its packages.jsonl rows, then its commits.jsonl row. A
// package row is the line with a top-level "package" key.
func WriteRows(w io.Writer, dir string) error {
	commits, err := rawLines(filepath.Join(dir, commitsFile))
	if err != nil {
		return err
	}
	pkgs, err := rawLines(filepath.Join(dir, packagesFile))
	if err != nil {
		return err
	}
	byCommit := map[string][][]byte{}
	for _, line := range pkgs {
		hash, err := commitOf(line)
		if err != nil {
			return fmt.Errorf("%s: %w", packagesFile, err)
		}
		byCommit[hash] = append(byCommit[hash], line)
	}
	bw := bufio.NewWriter(w)
	for _, line := range commits {
		hash, err := commitOf(line)
		if err != nil {
			return fmt.Errorf("%s: %w", commitsFile, err)
		}
		for _, p := range byCommit[hash] {
			_, _ = bw.Write(p)
		}
		_, _ = bw.Write(line)
	}
	return bw.Flush()
}

// commitOf returns the commit hash of a row line.
func commitOf(line []byte) (string, error) {
	var r struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(line, &r); err != nil {
		return "", err
	}
	return r.Commit, nil
}

// rawLines returns the non-empty lines of the file at path, each with its
// newline.
func rawLines(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading rows: %w", err)
	}
	var out [][]byte
	for line := range bytes.Lines(data) {
		if len(bytes.TrimSpace(line)) > 0 {
			out = append(out, line)
		}
	}
	return out, nil
}

// readLines decodes every line of the JSON-lines file at path.
func readLines[R any](path string) ([]R, error) {
	lines, err := rawLines(path)
	if err != nil {
		return nil, err
	}
	out := make([]R, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal(line, &out[i]); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", filepath.Base(path), i+1, err)
		}
	}
	return out, nil
}
