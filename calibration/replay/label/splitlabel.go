package main

import (
	"github.com/rfizzle/astimate/calibration/replay/label/internal/split"
	"github.com/rfizzle/astimate/calibration/replay/labels"
)

// labelSplit labels every commit of the replay data directory dir by the
// split-or-extraction rule, with the capacity rules of the configuration
// at cfg, or of the one the replay gated with when cfg is empty. window
// caps the later commits looked at; 0 means the rest of the range. agent,
// when not nil, sets each label's Agent.
func labelSplit(dir, cfg string, window int, agent *agentRule) (*labels.File, error) {
	r, run, err := split.Load(dir, cfg)
	var commits []commit
	if err == nil {
		commits, _, err = readCommits(dir)
	}
	if err != nil {
		return nil, err
	}
	repo := run.Remote
	if repo == "" {
		repo = run.Repository
	}
	f := &labels.File{
		Source:  labels.Source{Repository: repo, Range: run.Range, Data: dir},
		Rule:    split.RuleText(window),
		Commits: split.Decide(r, window),
	}
	return classify(f, commits, agent)
}
