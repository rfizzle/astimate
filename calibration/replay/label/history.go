package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rfizzle/astimate/calibration/replay/label/internal/split"
)

// commit is the part of a commits.jsonl row the labeler reads.
type commit struct {
	Commit       string   `json:"commit"`
	Parent       string   `json:"parent"`
	AuthorName   string   `json:"author_name"`
	AuthorEmail  string   `json:"author_email"`
	Subject      string   `json:"subject"`
	CoAuthoredBy []string `json:"co_authored_by"`
}

// message is one commit's message, for revert detection.
type message struct {
	hash, subject, body string
}

// gitRepo runs git in a clone.
type gitRepo struct{ dir string }

// output runs git with args and returns its standard output; the error
// carries git's standard error. Variables a git hook exports, which would
// point git at another repository, are dropped.
func (g gitRepo) output(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.dir, "-c", "core.quotePath=false"}, args...)...)
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); name != "GIT_DIR" && name != "GIT_WORK_TREE" && name != "GIT_INDEX_FILE" {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, bytes.TrimSpace(exit.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

// messages returns every commit of revRange, merged branches included,
// parents before children.
func (g gitRepo) messages(ctx context.Context, revRange string) ([]message, error) {
	out, err := g.output(ctx, "log", "--topo-order", "--reverse", "--format=%H%x1f%s%x1f%B%x1e", revRange, "--")
	if err != nil {
		return nil, fmt.Errorf("reading the messages of %s: %w", revRange, err)
	}
	var msgs []message
	for rec := range strings.SplitSeq(string(out), "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x1f", 3)
		if len(f) == 3 {
			msgs = append(msgs, message{hash: f[0], subject: f[1], body: f[2]})
		}
	}
	return msgs, nil
}

// touched returns the Go functions c added, changed or deleted, as
// <file>:<receiver.name>: every function of a non-test .go file whose
// declaration, on the parent's side or the commit's, holds a line the
// commit removed or added. A file that does not parse on a side
// contributes nothing from that side.
func (g gitRepo) touched(ctx context.Context, c *commit) (map[string]bool, error) {
	out, err := g.output(ctx, "diff-tree", "-p", "-U0", "--no-renames", "--no-commit-id", "--no-color",
		"--root", "--diff-merges=first-parent", c.Commit, "--", "*.go")
	if err != nil {
		return nil, fmt.Errorf("diffing %s: %w", c.Commit, err)
	}
	fns := map[string]bool{}
	for _, fd := range parseDiff(out) {
		sides := []struct {
			rev, path string
			lines     [][2]int
		}{{c.Parent, fd.oldPath, fd.old}, {c.Commit, fd.newPath, fd.new}}
		for _, s := range sides {
			if s.path == "" || len(s.lines) == 0 || strings.HasSuffix(s.path, "_test.go") {
				continue
			}
			src, err := g.output(ctx, "cat-file", "blob", s.rev+":"+s.path)
			if err != nil {
				return nil, fmt.Errorf("reading %s at %s: %w", s.path, s.rev, err)
			}
			for _, name := range funcsAt(src, s.lines) {
				fns[s.path+":"+name] = true
			}
		}
	}
	return fns, nil
}

// readCommits returns the rows of dir's commits.jsonl, in replay order, and
// the range run.json records.
func readCommits(dir string) ([]commit, string, error) {
	run, err := split.ReadRun(dir)
	if err == nil && run.Range == "" {
		err = fmt.Errorf("%s/run.json has no source range", dir)
	}
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "commits.jsonl"))
	if err != nil {
		return nil, "", fmt.Errorf("reading the replayed commits: %w", err)
	}
	var rows []commit
	for line := range bytes.Lines(data) {
		rows = append(rows, commit{})
		if err := json.Unmarshal(line, &rows[len(rows)-1]); err != nil {
			return nil, "", fmt.Errorf("decoding %s/commits.jsonl row %d: %w", dir, len(rows), err)
		}
	}
	return rows, run.Range, nil
}
