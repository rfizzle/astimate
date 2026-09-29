package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/replay/labels"
)

// repoRoot is this repository's root, from the package directory.
const repoRoot = "../../.."

// TestCorpusCommits checks calibration/corpus-commits.yaml against what is
// committed: every entry's replay data exists, its two labels files label
// every replayed commit by each rule, agent_commits is the number of
// replayed commits its agent rule matches, and the labels agree on which
// commits those are. The corpus must hold at least three repositories and
// 300 agent commits.
func TestCorpusCommits(t *testing.T) {
	c, err := loadCorpus(filepath.Join(repoRoot, "calibration", "corpus-commits.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for i := range c.Repositories {
		r := &c.Repositories[i]
		t.Run(r.Name, func(t *testing.T) {
			rows, revRange, err := readCommits(filepath.Join(repoRoot, r.Data))
			if err != nil {
				t.Fatal(err)
			}
			if revRange != r.Range {
				t.Errorf("replayed range %q, corpus range %q", revRange, r.Range)
			}
			hashes := make([]string, len(rows))
			agents := 0
			for k := range rows {
				hashes[k] = rows[k].Commit
				agents += b2i(r.Agent.matches(&rows[k]))
			}
			for _, path := range []string{r.Labels, r.SplitExtractLabels} {
				f, err := labels.Load(filepath.Join(repoRoot, path))
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Validate(hashes); err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				for k := range f.Commits {
					l := &f.Commits[k]
					if l.Provenance != labels.Rule || l.Agent == nil || *l.Agent != r.Agent.matches(&rows[k]) {
						t.Errorf("%s label %s: provenance %s, agent %v, not the rule's", path, l.Hash, l.Provenance, l.Agent)
					}
				}
			}
			if agents != r.AgentCommits {
				t.Errorf("agent_commits %d, the rule matches %d replayed commits", r.AgentCommits, agents)
			}
			total += agents
		})
	}
	if len(c.Repositories) < 3 || total < 300 {
		t.Errorf("%d repositories and %d agent commits, want at least 3 and 300", len(c.Repositories), total)
	}
}

func TestCorpusValidate(t *testing.T) {
	good := repository{
		Name: "a", Repo: "https://example.com/a.git", Commit: strings.Repeat("b", 40), Range: "x.." + strings.Repeat("b", 40),
		History: "squash", Agent: agentRule{Authors: []string{"bot"}}, Data: "d", Labels: "l", SplitExtractLabels: "s",
	}
	tests := []struct {
		name string
		edit func(*repository)
		want string
	}{
		{"valid", func(*repository) {}, ""},
		{"ssh repo", func(r *repository) { r.Repo = "git@example.com:a.git" }, "https"},
		{"short pin", func(r *repository) { r.Commit = "bbbbbbb" }, "full hash"},
		{"range not ending with pin", func(r *repository) { r.Range = "main" }, "full hash"},
		{"history", func(r *repository) { r.History = "rebase" }, "history"},
		{"no agent rule", func(r *repository) { r.Agent = agentRule{} }, "agent rule"},
		{"no labels", func(r *repository) { r.Labels = "" }, "labels path"},
		{"no split-extract labels", func(r *repository) { r.SplitExtractLabels = "" }, "split_extract_labels path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := good
			tc.edit(&r)
			err := (&corpus{Repositories: []repository{r}}).validate()
			if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("validate = %v, want %q", err, tc.want)
			}
		})
	}
	if err := (&corpus{Repositories: []repository{good, good}}).validate(); err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Errorf("validate of a repeated name = %v", err)
	}
}

func TestAgentRule(t *testing.T) {
	r := agentRule{CoAuthoredBy: []string{"noreply@anthropic.com"}, Authors: []string{"copilot-swe-agent"}}
	tests := []struct {
		c    commit
		want bool
	}{
		{commit{CoAuthoredBy: []string{"Claude Opus 4.5 <NoReply@Anthropic.com>"}}, true},
		{commit{AuthorName: "Copilot", AuthorEmail: "198982749+Copilot@users.noreply.github.com"}, false},
		{commit{AuthorName: "copilot-swe-agent[bot]", AuthorEmail: "x@users.noreply.github.com"}, true},
		{commit{AuthorName: "dependabot[bot]", CoAuthoredBy: []string{"Ann <ann@example.com>"}}, false},
	}
	for _, tc := range tests {
		if got := r.matches(&tc.c); got != tc.want {
			t.Errorf("matches(%+v) = %v, want %v", tc.c, got, tc.want)
		}
	}
}
