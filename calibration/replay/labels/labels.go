// Package labels reads, checks and writes the labels files that the gate
// validation joins with a replay's commit rows (calibration/replay/README.md):
// calibration/replay/labels/<name>.yaml, one verdict per replayed commit
// with its reason and where the verdict came from. Hand labels and rule
// labels share the shape, so every labels file is loaded and validated by
// this package, and the join is on the full commit hash.
package labels

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Verdict is what the gate should have said about a commit.
type Verdict string

// Verdicts.
const (
	// Block: the gate should have failed the commit.
	Block Verdict = "block"
	// Allow: the gate should have passed the commit.
	Allow Verdict = "allow"
)

// Provenance says where a label came from.
type Provenance string

// Provenances.
const (
	// Proposed is a hand label drafted but not yet reviewed.
	Proposed Provenance = "proposed"
	// Confirmed is a hand label a reviewer confirmed or corrected.
	Confirmed Provenance = "confirmed"
	// Rule is a label a mechanical rule produced, described by File.Rule.
	Rule Provenance = "rule"
)

// Parts of a labeling rule that can mark a commit block: revert and fixup
// of the revert-and-fix-up rule, split and extract of the
// split-or-extraction rule.
const (
	// PartRevert: a later commit reverted the commit.
	PartRevert = "revert"
	// PartFixup: a later fix or revert commit changed a function the
	// commit added or changed.
	PartFixup = "fixup"
	// PartSplit: the commit took a package over a capacity max, or grew
	// one already over by 100 or more SLOC, and a later commit split it:
	// cut its sloc by a quarter or more while the module's total barely
	// fell.
	PartSplit = "split"
	// PartExtract: the commit added duplicate blocks to a package, or
	// cross-package ones to the module, and a later commit removed at
	// least as many there without deleting a quarter of the code.
	PartExtract = "extract"
)

// knownPart reports whether part is one of the rule parts above.
func knownPart(part string) bool {
	switch part {
	case PartRevert, PartFixup, PartSplit, PartExtract:
		return true
	}
	return false
}

// File is a labels file.
type File struct {
	// Source is the history the labels describe.
	Source Source `yaml:"source"`
	// Rule describes how rule labels were produced; empty for hand labels.
	Rule string `yaml:"rule,omitempty"`
	// Commits holds one label per replayed commit, in replay order.
	Commits []Label `yaml:"commits"`
}

// Source names the repository, the revision range and the replay data
// directory the labels were made for.
type Source struct {
	Repository string `yaml:"repository"`
	Range      string `yaml:"range"`
	Data       string `yaml:"data"`
}

// Label is one commit's verdict.
type Label struct {
	// Hash is the commit's full hash, commit in commits.jsonl.
	Hash    string  `yaml:"hash"`
	Verdict Verdict `yaml:"verdict"`
	// Reason says why, in a sentence.
	Reason string `yaml:"reason"`
	// Metrics optionally names the gate rules that should have fired.
	Metrics    []string   `yaml:"metrics,omitempty"`
	Provenance Provenance `yaml:"provenance"`
	// Agent reports whether the corpus's agent rule matched the commit's
	// author or trailers; nil when the corpus does not classify commits.
	Agent *bool `yaml:"agent,omitempty"`
	// Fired lists the parts of the labeling rule that fired, for a rule
	// label of a block commit.
	Fired []Evidence `yaml:"fired,omitempty"`
}

// Evidence is one part of a labeling rule that fired on a commit.
type Evidence struct {
	// Part is PartRevert, PartFixup, PartSplit or PartExtract.
	Part string `yaml:"part"`
	// By is the full hash of the reverting, fixing, splitting or
	// extracting commit. A revert may come from a merged branch, so By
	// need not be a replayed commit.
	By string `yaml:"by"`
	// Functions are the functions both commits changed, as
	// <file>:<receiver.name>, or <file>:var <name> for a package-level
	// variable holding a function literal, for PartFixup.
	Functions []string `yaml:"functions,omitempty"`
	// Package is the package the later commit split or extracted from, or
	// the module row's id for cross-package duplicates, for PartSplit and
	// PartExtract.
	Package string `yaml:"package,omitempty"`
}

// Load reads and decodes the labels file at path.
func Load(path string) (*File, error) {
	var f File
	if err := Decode(path, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Decode decodes the YAML file at path into v. Unknown keys are an error,
// so a misspelled field is not silently dropped. Labels files and the
// commit corpus file are read with it.
func Decode(path string, v any) error {
	data, err := os.ReadFile(path)
	if err == nil {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		err = dec.Decode(v)
	}
	if err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}
	return nil
}

// Write encodes f to path as YAML.
func Write(path string, f *File) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("encoding labels: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encoding labels: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing labels: %w", err)
	}
	return nil
}

// Validate reports every way f fails to label exactly the replayed
// commits: a replayed commit without a label, a label for a commit that
// was not replayed, a duplicate, a verdict, provenance or rule part outside
// its set, an empty reason, and fired evidence on an allow label.
func (f *File) Validate(replayed []string) error {
	want := make(map[string]bool, len(replayed))
	for _, h := range replayed {
		want[h] = true
	}
	var errs []error
	seen := make(map[string]bool, len(f.Commits))
	for i := range f.Commits {
		l := &f.Commits[i]
		switch {
		case seen[l.Hash]:
			errs = append(errs, fmt.Errorf("commit %s is labeled twice", l.Hash))
		case !want[l.Hash]:
			errs = append(errs, fmt.Errorf("commit %q was not replayed", l.Hash))
		}
		seen[l.Hash] = true
		errs = append(errs, l.check())
	}
	for _, h := range replayed {
		if !seen[h] {
			errs = append(errs, fmt.Errorf("commit %s has no label", h))
		}
	}
	return errors.Join(errs...)
}

// check reports what is wrong with l on its own, or nil.
func (l *Label) check() error {
	var problems []string
	if l.Verdict != Block && l.Verdict != Allow {
		problems = append(problems, fmt.Sprintf("verdict %q is not block or allow", l.Verdict))
	}
	if l.Reason == "" {
		problems = append(problems, "no reason")
	}
	if !slices.Contains([]Provenance{Proposed, Confirmed, Rule}, l.Provenance) {
		problems = append(problems, fmt.Sprintf("provenance %q is not proposed, confirmed or rule", l.Provenance))
	}
	if len(l.Fired) > 0 && l.Verdict != Block {
		problems = append(problems, "rule evidence on a "+string(l.Verdict)+" label")
	}
	for _, e := range l.Fired {
		if !knownPart(e.Part) || e.By == "" {
			problems = append(problems, fmt.Sprintf("evidence %q by %q is not a revert, fixup, split or extract with its commit", e.Part, e.By))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("commit %s: %s", l.Hash, strings.Join(problems, "; "))
}

// ByHash returns f's labels keyed by commit hash, for the join with the
// replay's rows.
func (f *File) ByHash() map[string]*Label {
	out := make(map[string]*Label, len(f.Commits))
	for i := range f.Commits {
		out[f.Commits[i].Hash] = &f.Commits[i]
	}
	return out
}

// Replayed returns the commit hashes of the replay data directory dir, as
// commits.jsonl lists them, in replay order.
func Replayed(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "commits.jsonl"))
	var hashes []string
	for dec := json.NewDecoder(bytes.NewReader(data)); err == nil && dec.More(); {
		var row struct {
			Commit string `json:"commit"`
		}
		if err = dec.Decode(&row); err == nil && row.Commit == "" {
			err = fmt.Errorf("row %d has no commit", len(hashes)+1)
		}
		hashes = append(hashes, row.Commit)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the replayed commits of %s: %w", dir, err)
	}
	return hashes, nil
}
