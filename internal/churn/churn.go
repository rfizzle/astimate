// Package churn counts how often each package of a module changes: the
// first-parent commits of a window that touched a non-test, non-generated
// file of the package, and the distinct authors of those commits (SPEC.md
// 6.6), read from one git log per measurement. It imports metrics and the
// standard library only; the engine calls it and fills the metrics.
package churn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rfizzle/astimate/internal/metrics"
)

// ErrUnavailable reports that the module has no history to read: its root
// is not inside a git work tree, HEAD names no commit yet, the clone is
// shallow, or git cannot run. The churn metrics are then null rather than 0.
var ErrUnavailable = errors.New("no git history")

// Counts is the churn of one package over a window.
type Counts struct {
	// Commits is churn_commits: the commits of the window that touched a
	// counted file of the package.
	Commits int
	// Authors is churn_authors: the distinct author emails among those
	// commits, compared case-insensitively.
	Authors int
}

// Options shape a Measure call.
type Options struct {
	// Window is how many first-parent commits from HEAD are read.
	Window int
	// Classifier says which files are package source, which are tests and
	// which package each belongs to, by module-relative path, as
	// changed-package detection reads them (SPEC.md 8.4). A file counts
	// when it is metrics.PackageSource and not a test.
	Classifier metrics.SourceClassifier
	// Generated reports whether the file at an absolute path is generated,
	// which leaves it out; it is asked once per distinct file, and a file
	// gone from the tree must read as not generated. Nil counts every
	// file, for a language with no generated-file convention.
	Generated func(path string) bool
}

// Index is the churn of a module's packages over one window, keyed by
// module-relative package directory.
type Index struct {
	commits map[string]int
	authors map[string]map[string]struct{}
}

// Of returns the churn of the package in the module-relative, slash
// directory dir ("." for the module root): zero when no commit of the
// window touched it.
func (ix *Index) Of(dir string) Counts {
	return Counts{Commits: ix.commits[dir], Authors: len(ix.authors[dir])}
}

// Measure reads the last opts.Window first-parent commits from HEAD of the
// repository holding the module at root with one git log and attributes
// each to the packages whose counted files it touched. Paths are taken
// relative to root, so files outside the module are not read. A merge
// commit counts the files it changed against its first parent. Renames
// are not followed: a commit that moved a file counts for the package it
// left and the one it entered, and an earlier commit for the package the
// file was in then. It returns an error wrapping ErrUnavailable when root
// has no history or a shallow one, and any other git failure as is.
func Measure(ctx context.Context, root string, opts Options) (*Index, error) {
	if opts.Window <= 0 {
		return nil, fmt.Errorf("measuring churn: window %d: %w", opts.Window, ErrUnavailable)
	}
	if _, err := run(ctx, root, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return nil, fmt.Errorf("measuring churn in %s: %w: %w", root, ErrUnavailable, err)
	}
	// A shallow clone's oldest commit lists every file as added, so its
	// counts would describe the clone, not the history.
	shallow, err := run(ctx, root, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, fmt.Errorf("measuring churn in %s: %w", root, err)
	}
	if strings.TrimSpace(string(shallow)) == "true" {
		return nil, fmt.Errorf("measuring churn in %s: shallow clone: %w", root, ErrUnavailable)
	}
	out, err := run(ctx, root, "-c", "core.quotePath=false", "log", "--first-parent", "--diff-merges=first-parent",
		"--no-renames", "--relative", "--name-only", "--no-color", "--no-show-signature",
		"--format=%x00%ae", "-n", strconv.Itoa(opts.Window), "HEAD", "--")
	if err != nil {
		return nil, fmt.Errorf("measuring churn in %s: %w", root, err)
	}
	return attribute(out, root, opts), nil
}

// attribute builds the Index from git log output: per commit a NUL, the
// author email on the first line and the touched paths, module-relative,
// one per line after it.
func attribute(out []byte, root string, opts Options) *Index {
	ix := &Index{commits: make(map[string]int), authors: make(map[string]map[string]struct{})}
	pkgOf := make(map[string]string)
	touched := make(map[string]struct{})
	for commit := range bytes.SplitSeq(out, []byte{0}) {
		lines := strings.Split(string(commit), "\n")
		if len(lines) < 2 {
			// The empty text before the first NUL.
			continue
		}
		clear(touched)
		for _, p := range lines[1:] {
			if p == "" {
				continue
			}
			pkg, seen := pkgOf[p]
			if !seen {
				pkg = counted(p, root, opts)
				pkgOf[p] = pkg
			}
			if pkg != "" {
				touched[pkg] = struct{}{}
			}
		}
		author := strings.ToLower(strings.TrimSpace(lines[0]))
		for pkg := range touched {
			ix.commits[pkg]++
			if ix.authors[pkg] == nil {
				ix.authors[pkg] = make(map[string]struct{})
			}
			ix.authors[pkg][author] = struct{}{}
		}
	}
	return ix
}

// counted returns the package the module-relative path p counts for, or
// "" when it is no package's source, a test or generated.
func counted(p, root string, opts Options) string {
	sf := opts.Classifier.ClassifyFile(p)
	if sf.Kind != metrics.PackageSource || sf.Test {
		return ""
	}
	if opts.Generated != nil && opts.Generated(filepath.Join(root, filepath.FromSlash(p))) {
		return ""
	}
	return sf.Package
}

// run runs git with args in dir, never through a shell, and returns its
// standard output. The variables a git hook sets to point git at another
// repository or index are dropped, so the history read is dir's.
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return name == "GIT_DIR" || name == "GIT_WORK_TREE" || name == "GIT_INDEX_FILE"
	})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
