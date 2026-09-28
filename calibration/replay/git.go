package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// commitInfo is what git says about one commit of the range.
type commitInfo struct {
	hash string
	// parent is the first parent; empty for a root commit.
	parent        string
	authorName    string
	authorEmail   string
	committerDate time.Time
	subject       string
	// trailers are the message's trailers, key as written to its values in
	// order.
	trailers map[string][]string
}

// gitCmd runs git with args in dir and returns its standard output. The
// error carries git's standard error.
func gitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// A hook's GIT_DIR or GIT_INDEX_FILE must not redirect git away from dir.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=")
	})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// defaultRange returns the revision whose first-parent history is replayed
// when no range is given: the first of master and main that exists, else
// HEAD.
func defaultRange(ctx context.Context, repo string) string {
	for _, ref := range []string{"master", "main"} {
		if _, err := gitCmd(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+ref); err == nil {
			return ref
		}
	}
	return "HEAD"
}

// Field and record separators of the log format listCommits parses.
const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
)

// listCommits returns the commits of revRange along first parents, oldest
// first.
func listCommits(ctx context.Context, repo, revRange string) ([]commitInfo, error) {
	format := strings.Join([]string{"%H", "%P", "%an", "%ae", "%cI", "%s", "%(trailers:only,unfold)"}, "%x1f") + "%x1e"
	out, err := gitCmd(ctx, repo, "log", "--first-parent", "--reverse", "--format="+format, revRange, "--")
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", revRange, err)
	}
	var commits []commitInfo
	for rec := range strings.SplitSeq(out, recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, fieldSep)
		if len(f) != 7 {
			return nil, fmt.Errorf("listing %s: unexpected log record %q", revRange, rec)
		}
		date, err := time.Parse(time.RFC3339, f[4])
		if err != nil {
			return nil, fmt.Errorf("commit %s: committer date: %w", f[0], err)
		}
		parent, _, _ := strings.Cut(f[1], " ")
		commits = append(commits, commitInfo{
			hash: f[0], parent: parent, authorName: f[2], authorEmail: f[3],
			committerDate: date, subject: f[5], trailers: parseTrailers(f[6]),
		})
	}
	return commits, nil
}

// parseTrailers parses git's "Key: value" trailer lines; an empty map when
// there are none.
func parseTrailers(s string) map[string][]string {
	out := map[string][]string{}
	for line := range strings.SplitSeq(s, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		key = strings.TrimSpace(key)
		out[key] = append(out[key], strings.TrimSpace(value))
	}
	return out
}

// coAuthors returns the values of the Co-Authored-By trailers, whatever
// their key's case.
func coAuthors(trailers map[string][]string) []string {
	var out []string
	for k, v := range trailers {
		if strings.EqualFold(k, "Co-Authored-By") {
			out = append(out, v...)
		}
	}
	slices.Sort(out)
	return out
}

// filesChanged returns the paths the commit changed against its first
// parent, or every path of a root commit, sorted.
func filesChanged(ctx context.Context, repo string, c *commitInfo) ([]string, error) {
	args := []string{"diff-tree", "-r", "--name-only", "--no-commit-id", "-z"}
	if c.parent == "" {
		args = append(args, "--root", c.hash)
	} else {
		args = append(args, c.parent, c.hash)
	}
	out, err := gitCmd(ctx, repo, args...)
	if err != nil {
		return nil, fmt.Errorf("listing the files %s changed: %w", c.hash, err)
	}
	files := slices.DeleteFunc(strings.Split(out, "\x00"), func(s string) bool { return s == "" })
	slices.Sort(files)
	return files, nil
}

// hasPath reports whether the tree of commit holds path, relative to the
// repository's top level.
func hasPath(ctx context.Context, repo, commit, path string) bool {
	_, err := gitCmd(ctx, repo, "cat-file", "-e", commit+":"+path)
	return err == nil
}

// pruneTimeout bounds the git commands that remove a worktree; they run on
// a fresh context, since the caller's may already be canceled.
const pruneTimeout = 30 * time.Second

// addWorktree checks commit out, detached, into a new directory under the
// system's temporary directory, never inside repo's checkout. The returned
// remove must be called exactly once, including after ctx is canceled.
func addWorktree(ctx context.Context, repo, commit string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "astimate-replay-")
	if err != nil {
		return "", nil, fmt.Errorf("creating a worktree directory: %w", err)
	}
	// A unique base name gives the worktree its own record in git, so
	// concurrent replays never reuse one another's.
	dir := filepath.Join(tmp, filepath.Base(tmp))
	_, err = gitCmd(ctx, repo, "worktree", "add", "--detach", dir, commit)
	if err != nil {
		removeWorktree(repo, tmp, dir)
		return "", nil, fmt.Errorf("checking %s out into a worktree: %w", commit, err)
	}
	return dir, func() { removeWorktree(repo, tmp, dir) }, nil
}

// removeWorktree removes the worktree of repo at dir and deletes tmp, which
// holds it. Only when git cannot remove it is git's record pruned, since a
// prune can race a concurrent worktree add in the same repository.
func removeWorktree(repo, tmp, dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), pruneTimeout)
	defer cancel()
	_, err := gitCmd(ctx, repo, "worktree", "remove", "--force", dir)
	_ = os.RemoveAll(tmp)
	if err != nil {
		_, _ = gitCmd(ctx, repo, "worktree", "prune")
	}
}

// repoName names the repository at repo for the default output directory:
// the directory holding its main git directory, so a linked worktree is
// named after the repository, not the worktree.
func repoName(ctx context.Context, repo string) string {
	out, err := gitCmd(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		abs, _ := filepath.Abs(repo)
		return filepath.Base(abs)
	}
	common := filepath.Clean(strings.TrimSpace(out))
	if filepath.Base(common) == ".git" {
		return filepath.Base(filepath.Dir(common))
	}
	return strings.TrimSuffix(filepath.Base(common), ".git")
}
