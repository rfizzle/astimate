package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rfizzle/astimate/calibration/replay/labels"
)

// corpus is calibration/corpus-commits.yaml: the external repositories
// whose agent-authored commits are replayed and rule-labeled.
type corpus struct {
	Note         string       `yaml:"note"`
	Repositories []repository `yaml:"repositories"`
}

// repository is one corpus entry. Name names the replay data directory
// and the labels file. Commit pins the newest commit replayed, and Range
// ends with it. Dir is the module root relative to the repository's top
// level. History says how changes reach the default branch: linear, squash
// or merge commits. AgentCommits counts the replayed commits the agent rule
// matches. Data and Labels are the replay data directory and the labels
// file of the revert-and-fix-up rule, and SplitExtractLabels the labels
// file of the split-or-extraction rule, relative to this repository's root.
type repository struct {
	Name, Repo, License, Commit, Range, Dir, History, Data, Labels, Reason string

	SplitExtractLabels string `yaml:"split_extract_labels"`

	Agent        agentRule
	AgentCommits int `yaml:"agent_commits"`
}

// agentRule identifies a repository's agent-authored commits: a commit is
// agent-authored when a Co-Authored-By value, or "<author name>
// <<author email>>", contains one of the rule's strings, ignoring case.
type agentRule struct {
	CoAuthoredBy []string `yaml:"co_authored_by"`
	Authors      []string `yaml:"authors"`
}

// matches reports whether c is agent-authored under r.
func (r *agentRule) matches(c *commit) bool {
	for _, v := range c.CoAuthoredBy {
		if containsAny(v, r.CoAuthoredBy) {
			return true
		}
	}
	return containsAny(c.AuthorName+" <"+c.AuthorEmail+">", r.Authors)
}

// containsAny reports whether s contains one of subs, ignoring case.
func containsAny(s string, subs []string) bool {
	s = strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(s, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// loadCorpus reads and validates the corpus file at path.
func loadCorpus(path string) (*corpus, error) {
	var c corpus
	if err := labels.Decode(path, &c); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// validate reports entries without a unique name, an https repository, a
// full-hash pin that ends the range, a history kind or an agent rule.
func (c *corpus) validate() error {
	var errs []error
	seen := map[string]bool{}
	for i := range c.Repositories {
		r := &c.Repositories[i]
		bad := func(msg string) { errs = append(errs, fmt.Errorf("repository %q: %s", r.Name, msg)) }
		if r.Name == "" || seen[r.Name] {
			bad("name missing or repeated")
		}
		seen[r.Name] = true
		if !strings.HasPrefix(r.Repo, "https://") {
			bad("repo is not an https URL")
		}
		if len(r.Commit) != 40 || strings.Trim(r.Commit, "0123456789abcdef") != "" || !strings.HasSuffix(r.Range, r.Commit) {
			bad("commit is not a full hash ending the range")
		}
		if r.History != "linear" && r.History != "squash" && r.History != "merge" {
			bad("history is not linear, squash or merge")
		}
		if len(r.Agent.CoAuthoredBy)+len(r.Agent.Authors) == 0 {
			bad("no agent rule")
		}
		if r.Data == "" || r.Labels == "" || r.SplitExtractLabels == "" {
			bad("data, labels or split_extract_labels path missing")
		}
	}
	return errors.Join(errs...)
}

// find returns the entry named name.
func (c *corpus) find(name string) (*repository, error) {
	for i := range c.Repositories {
		if c.Repositories[i].Name == name {
			return &c.Repositories[i], nil
		}
	}
	return nil, fmt.Errorf("no corpus entry named %q", name)
}
