package main

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/rfizzle/astimate/calibration/replay/labels"
)

// ruleText describes the rule in the labels file; %d is the window.
const ruleText = "block when a later commit of the range reverts the commit (its message says " +
	"\"This reverts commit <hash>\", or its subject is Revert \"<the commit's subject>\"), or when a " +
	"function of a non-test .go file that the commit added, changed or deleted (a declaration holding " +
	"a line of its -U0 diff, on either side, matched by file and receiver.name; a function literal in a " +
	"package-level variable counts as that variable) is changed again " +
	"within the next %d first-parent commits by a commit whose subject starts with fix or revert " +
	"(fix, fixes, fixed, fixup, revert, reverts, ...: case-insensitive, a Conventional Commits scope " +
	"allowed); otherwise allow. Weak and objective: a fix may be unrelated to the change, and a bad " +
	"change nobody fixed reads as allow."

// labelData labels every commit of the replay data directory dir, with
// the history read from repo. rule, when not nil, sets each label's Agent.
func labelData(ctx context.Context, repo gitRepo, dir string, window int, rule *agentRule) (*labels.File, error) {
	commits, revRange, err := readCommits(dir)
	if err != nil {
		return nil, err
	}
	msgs, err := repo.messages(ctx, revRange)
	if err != nil {
		return nil, err
	}
	touched, err := touchedAll(ctx, repo, commits)
	if err != nil {
		return nil, err
	}
	remote, _ := repo.output(ctx, "remote", "get-url", "origin")
	f := &labels.File{
		Source:  labels.Source{Repository: strings.TrimSpace(string(remote)), Range: revRange, Data: dir},
		Rule:    fmt.Sprintf(ruleText, window),
		Commits: decide(commits, msgs, touched, window),
	}
	return classify(f, commits, rule)
}

// classify sets each label of f's Agent from rule, when rule is not nil,
// and checks that f labels exactly commits.
func classify(f *labels.File, commits []commit, rule *agentRule) (*labels.File, error) {
	hashes := make([]string, len(commits))
	for i := range commits {
		hashes[i] = commits[i].Commit
		if rule != nil && i < len(f.Commits) {
			agent := rule.matches(&commits[i])
			f.Commits[i].Agent = &agent
		}
	}
	if err := f.Validate(hashes); err != nil {
		return nil, fmt.Errorf("labels do not cover the replay: %w", err)
	}
	return f, nil
}

// touchedAll returns touched for every commit, computed in parallel.
func touchedAll(ctx context.Context, repo gitRepo, commits []commit) ([]map[string]bool, error) {
	out := make([]map[string]bool, len(commits))
	errs := make([]error, len(commits))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i := range commits {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			out[i], errs[i] = repo.touched(ctx, &commits[i])
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// decide applies the rule: commits are the first-parent commits in order,
// msgs every commit of the range parents first, touched each commit's
// functions.
func decide(commits []commit, msgs []message, touched []map[string]bool, window int) []labels.Label {
	reverts := findReverts(commits, msgs)
	out := make([]labels.Label, len(commits))
	for i := range commits {
		l := labels.Label{Hash: commits[i].Commit, Provenance: labels.Rule}
		var why []string
		if r, ok := reverts[i]; ok {
			l.Fired = append(l.Fired, labels.Evidence{Part: labels.PartRevert, By: r.hash})
			why = append(why, fmt.Sprintf("reverted by %.12s (%q)", r.hash, r.subject))
		}
		if j, fns := fixup(commits, touched, i, window); j >= 0 {
			l.Fired = append(l.Fired, labels.Evidence{Part: labels.PartFixup, By: commits[j].Commit, Functions: fns})
			why = append(why, fmt.Sprintf("%s changed again %d commits later by %.12s (%q)",
				strings.Join(fns, ", "), j-i, commits[j].Commit, commits[j].Subject))
		}
		l.Verdict, l.Reason = labels.Block, strings.Join(why, "; ")
		if len(why) == 0 {
			l.Verdict, l.Reason = labels.Allow, allowReason(len(touched[i]), len(commits)-1-i, window)
		}
		out[i] = l
	}
	return out
}

// allowReason explains an allow label for a commit that touched n
// functions and has later commits after it in the range.
func allowReason(n, later, window int) string {
	if n == 0 {
		return "not reverted in the range; it changed no function of a non-test .go file, so the fix-up part cannot fire"
	}
	reason := fmt.Sprintf("not reverted in the range; no fix or revert commit within the next %d first-parent commits "+
		"changed a function it changed (%d)", window, n)
	if later < window {
		reason += fmt.Sprintf(" (window cut short by the end of the range: %d of %d commits)", later, window)
	}
	return reason
}

// fixup returns the first commit within window commits after i whose
// subject is a fix or a revert and which touched a function commit i
// touched, with the shared functions sorted; -1 when there is none.
func fixup(commits []commit, touched []map[string]bool, i, window int) (int, []string) {
	if len(touched[i]) == 0 {
		return -1, nil
	}
	for j := i + 1; j < len(commits) && j <= i+window; j++ {
		if !isFixSubject(commits[j].Subject) {
			continue
		}
		var shared []string
		for fn := range touched[j] {
			if touched[i][fn] {
				shared = append(shared, fn)
			}
		}
		if len(shared) > 0 {
			slices.Sort(shared)
			return j, shared
		}
	}
	return -1, nil
}

// isFixSubject reports whether subject starts with a fix or revert word,
// ignoring case: "fix: x", "Fix(scope)!: x", "Fixes #12", "fixup! x",
// "Revert \"x\"", but not "fixture: x" or "prefix x".
func isFixSubject(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	end := strings.IndexFunc(s, func(r rune) bool { return r < 'a' || r > 'z' })
	if end < 0 {
		end = len(s)
	}
	word := s[:end]
	for _, stem := range []string{"fix", "revert"} {
		if rest, ok := strings.CutPrefix(word, stem); ok && slices.Contains([]string{"", "s", "es", "ed", "ing", "up"}, rest) {
			return true
		}
	}
	return false
}

// findReverts maps the index in commits of each reverted commit to the
// first commit of msgs that reverted it: one whose message says "This
// reverts commit <hash>" (a full or abbreviated hash of an earlier commit),
// or whose subject is Revert "<subject>" for the latest earlier commit
// with that subject.
func findReverts(commits []commit, msgs []message) map[int]message {
	index := make(map[string]int, len(commits))
	for i := range commits {
		index[commits[i].Commit] = i
	}
	seen := map[string]int{} // subject to the latest first-parent index seen
	out := map[int]message{}
	record := func(i int, m message) {
		if _, done := out[i]; !done && commits[i].Commit != m.hash {
			out[i] = m
		}
	}
	for _, m := range msgs {
		for _, h := range revertedHashes(m.body) {
			if i, ok := resolve(index, commits, h); ok {
				record(i, m)
			}
		}
		if inner, ok := revertedSubject(m.subject); ok {
			if i, ok := seen[inner]; ok {
				record(i, m)
			}
		}
		if i, ok := index[m.hash]; ok {
			seen[commits[i].Subject] = i
		}
	}
	return out
}

// revertedHashes returns the hashes named by "This reverts commit <hash>"
// in body, ignoring case.
func revertedHashes(body string) []string {
	const marker = "this reverts commit "
	var out []string
	rest := strings.ToLower(body)
	for {
		k := strings.Index(rest, marker)
		if k < 0 {
			return out
		}
		rest = rest[k+len(marker):]
		end := strings.IndexFunc(rest, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef", r) })
		if end < 0 {
			end = len(rest)
		}
		if end >= 7 {
			out = append(out, rest[:end])
		}
	}
}

// resolve finds the commit a full or abbreviated hash names.
func resolve(index map[string]int, commits []commit, h string) (int, bool) {
	if i, ok := index[h]; ok {
		return i, true
	}
	for i := range commits {
		if strings.HasPrefix(commits[i].Commit, h) {
			return i, true
		}
	}
	return 0, false
}

// revertedSubject returns x from a subject Revert "x", allowing text such
// as a pull request number after the closing quote.
func revertedSubject(subject string) (string, bool) {
	const prefix = `Revert "`
	last := strings.LastIndex(subject, `"`)
	if !strings.HasPrefix(subject, prefix) || last < len(prefix) {
		return "", false
	}
	return subject[len(prefix):last], true
}
