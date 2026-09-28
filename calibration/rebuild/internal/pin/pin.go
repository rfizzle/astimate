// Package pin runs the go and git commands the rebuild experiment needs
// against a clone at a corpus pin: cloning and resetting a module, running
// go build and go test in it, and the Checker that verifies one candidate
// package is a valid experiment (SPEC.md 11.2, calibration/rebuild/README.md
// "The selection rule").
package pin

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
	"time"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/selection"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
)

// RejectedPrefix marks the text of an error Checker.Check returns: a
// candidate that failed a check, with the reason after the prefix.
const RejectedPrefix = "rejected: "

// rejected formats a rejection reason as the error Check returns.
func rejected(format string, args ...any) error {
	return fmt.Errorf(RejectedPrefix+format, args...)
}

// DefaultEnv is the environment every go command of the experiment runs
// with: no cgo (corpus criterion 5), no workspace, and the local toolchain
// only, so no toolchain is downloaded behind the run's back.
func DefaultEnv() []string {
	return []string{"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off"}
}

// commandTimeout bounds one go build or go test during the checks.
const commandTimeout = 20 * time.Minute

// RunGo runs the go command in dir with env added to the process
// environment and returns its combined output.
func RunGo(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runGit runs git with args and returns an error carrying its output.
func runGit(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CloneAt fetches repo at commit alone into the new directory dir, with no
// history, and checks it out detached, as calibration/collect does. It
// needs network unless repo is a local path or file:// URL.
func CloneAt(ctx context.Context, repo, commit, dir string) error {
	for _, args := range [][]string{
		{"init", "--quiet", dir},
		{"-C", dir, "fetch", "--quiet", "--depth", "1", repo, commit},
		{"-C", dir, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
	} {
		if err := runGit(ctx, args...); err != nil {
			return err
		}
	}
	return nil
}

// resetClone discards every change to the clone at dir, restoring the
// pinned tree.
func resetClone(ctx context.Context, dir string) error {
	if err := runGit(ctx, "-C", dir, "checkout", "--quiet", "--", "."); err != nil {
		return err
	}
	return runGit(ctx, "-C", dir, "clean", "--quiet", "-fd")
}

// importersOf returns, sorted, the patterns of the packages in the module
// at root that skip does not exclude, import one of targets from any
// file, and have test files: the tests that check an untested package's,
// or tree's, behavior.
func importersOf(ctx context.Context, root, module string, targets []string, skip func(importPath string) bool, env []string) ([]string, error) {
	const format = `{{.ImportPath}}|{{join .Imports ","}},{{join .TestImports ","}},{{join .XTestImports ","}}|{{len .TestGoFiles}}{{len .XTestGoFiles}}`
	out, err := RunGo(ctx, root, env, "list", "-e", "-f", format, "./...")
	if err != nil {
		return nil, rejected("go list: %s", Tail(out))
	}
	var pats []string
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimSpace(line), "|")
		if len(fields) != 3 || skip(fields[0]) || fields[2] == "00" {
			continue
		}
		imports := strings.Split(fields[1], ",")
		if slices.ContainsFunc(targets, func(t string) bool { return slices.Contains(imports, t) }) {
			pats = append(pats, definition.OwnPattern(definition.ModRelDir(module, fields[0])))
		}
	}
	slices.Sort(pats)
	return pats, nil
}

// nonGoExts are the extensions of a source a Go stub cannot replace:
// assembly, C and object files.
func nonGoExts() []string {
	return []string{".s", ".c", ".cc", ".cpp", ".h", ".m", ".syso"}
}

// nonGoSources returns the files in dir, by base name and sorted, whose
// extension is one of nonGoExts: a stub cannot replace their
// implementation.
func nonGoSources(dir string) ([]string, error) {
	var out []string
	for _, ext := range nonGoExts() {
		matches, err := filepath.Glob(filepath.Join(dir, "*"+ext))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", dir, err)
		}
		for _, m := range matches {
			out = append(out, filepath.Base(m))
		}
	}
	slices.Sort(out)
	return out, nil
}

// testBuildBroken reports whether go test output shows a package whose
// tests did not build, as opposed to tests that ran and failed.
func testBuildBroken(out string) bool {
	return strings.Contains(out, "[build failed]") || strings.Contains(out, "[setup failed]")
}

// Tail returns the last lines of out on one line, for a rejection reason,
// with go test's elapsed times and "(cached)" markers dropped and runs of
// white space collapsed, so the same failure reads the same in every run.
func Tail(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		kept := fields[:0]
		for _, f := range fields {
			if f == "(cached)" || isElapsed(f) {
				continue
			}
			kept = append(kept, f)
		}
		lines[i] = strings.Join(kept, " ")
	}
	s := strings.Join(lines, " / ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// isElapsed reports whether f is a go test elapsed time such as 0.403s.
func isElapsed(f string) bool {
	num, ok := strings.CutSuffix(f, "s")
	if !ok || num == "" {
		return false
	}
	_, err := strconv.ParseFloat(num, 64)
	return err == nil
}

// Checker verifies candidates in clones under work, one clone per module,
// reused across that module's candidates.
type Checker struct {
	work    string
	env     []string
	turnCap int
	// clones maps a module to its clone directory, or to the error that
	// made the module unusable.
	clones   map[string]string
	cloneErr map[string]error
}

// NewChecker returns a Checker that verifies candidates in clones under
// work with the given environment and experiment turn cap.
func NewChecker(work string, env []string, turnCap int) *Checker {
	return &Checker{work: work, env: env, turnCap: turnCap, clones: map[string]string{}, cloneErr: map[string]error{}}
}

// clone returns the checked clone of c's module: fetched at the pin, with
// go build ./... passing. The result is cached per module.
func (k *Checker) clone(ctx context.Context, c selection.Candidate) (string, error) {
	mod := c.Row.Module
	if dir, ok := k.clones[mod]; ok {
		return dir, nil
	}
	if err, ok := k.cloneErr[mod]; ok {
		return "", err
	}
	dir := filepath.Join(k.work, strings.NewReplacer("/", "_", ".", "_").Replace(mod))
	err := k.prepareClone(ctx, c.Repo, c.Row.Commit, dir)
	if err != nil {
		k.cloneErr[mod] = err
		return "", err
	}
	k.clones[mod] = dir
	return dir, nil
}

// prepareClone clones repo at commit into dir unless a clone at that
// commit is already there, and checks that the module builds.
func (k *Checker) prepareClone(ctx context.Context, repo, commit, dir string) error {
	head, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != commit {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("clearing %s: %w", dir, err)
		}
		if err := CloneAt(ctx, repo, commit, dir); err != nil {
			return rejected("cloning: %s", err)
		}
	} else if err := resetClone(ctx, dir); err != nil {
		return rejected("resetting clone: %s", err)
	}
	if out, err := RunGo(ctx, dir, k.env, "build", "./..."); err != nil {
		return rejected("module does not build at the pin: %s", Tail(out))
	}
	return nil
}

// Check verifies candidate c at its pin: the package has no non-Go
// sources; its oracle has tests to run; the oracle passes on the original
// tree; stubbing twice gives identical files; the stubbed module builds;
// the stubbed oracle's tests build, start, and fail with the stub's
// panic. The clone is restored to the pin afterwards.
func (k *Checker) Check(ctx context.Context, c selection.Candidate) (definition.Experiment, error) {
	dir := definition.ModRelDir(c.Row.Module, c.Row.Package)
	return k.check(ctx, c, func(root string) (plan, error) {
		pkgDir := filepath.Join(root, filepath.FromSlash(dir))
		return plan{
			dir: dir, dirs: []string{dir}, own: definition.OwnPattern(dir),
			skip:        func(p string) bool { return p == c.Row.Package },
			targets:     []string{c.Row.Package},
			noImporters: "no importer in the module has tests",
			stub:        func() ([]stub.File, error) { return stub.Package(ctx, pkgDir, k.env) },
		}, nil
	})
}

// plan is what check needs to verify one candidate, a package or a tree.
type plan struct {
	// dir is the candidate's directory and dirs every package directory
	// the stub replaces, module-relative.
	dir  string
	dirs []string
	// own is the oracle's test pattern when the candidate has tests.
	own string
	// Without tests, the oracle is the tests of the packages that import
	// one of targets and that skip does not exclude; noImporters is the
	// rejection when there is none.
	targets     []string
	skip        func(importPath string) bool
	noImporters string
	// stub returns the stubbed files, named relative to dir.
	stub func() ([]stub.File, error)
}

// check verifies candidate c in its module's clone, restored to the pin
// afterwards: prepare returns the plan for the clone at root; no
// directory of the plan has non-Go sources; the oracle has tests to run;
// and verify passes.
func (k *Checker) check(ctx context.Context, c selection.Candidate, prepare func(root string) (plan, error)) (exp definition.Experiment, err error) {
	root, err := k.clone(ctx, c)
	if err != nil {
		return definition.Experiment{}, err
	}
	defer func() {
		if rerr := resetClone(ctx, root); rerr != nil && err == nil {
			err = fmt.Errorf("restoring clone: %w", rerr)
		}
	}()
	p, err := prepare(root)
	if err != nil {
		return definition.Experiment{}, err
	}
	if err := checkSources(root, p.dirs); err != nil {
		return definition.Experiment{}, err
	}
	oracle := definition.Oracle{Test: []string{p.own}, Build: []string{"./..."}}
	if !c.Row.Metrics.HasTests {
		if oracle.Test, err = importersOf(ctx, root, c.Row.Module, p.targets, p.skip, k.env); err != nil {
			return definition.Experiment{}, err
		}
		if len(oracle.Test) == 0 {
			return definition.Experiment{}, rejected("%s", p.noImporters)
		}
	}
	hash, err := k.verify(ctx, root, filepath.Join(root, filepath.FromSlash(p.dir)), oracle, p.stub)
	if err != nil {
		// Paths in the reason are the clone's; keep them module-relative
		// so the report does not depend on --work.
		return definition.Experiment{}, errors.New(strings.ReplaceAll(err.Error(), root+string(filepath.Separator), ""))
	}
	return selection.NewExperiment(c, oracle, hash, k.turnCap), nil
}

// checkSources rejects a plan with non-Go sources in any of its package
// directories, naming the directory when there are several.
func checkSources(root string, dirs []string) error {
	for _, d := range dirs {
		other, err := nonGoSources(filepath.Join(root, filepath.FromSlash(d)))
		if err != nil {
			return rejected("%s", err)
		}
		where := ""
		if len(dirs) > 1 {
			where = " in " + d
		}
		if len(other) > 0 {
			return rejected("non-Go sources a stub cannot replace%s: %s", where, strings.Join(other, ", "))
		}
	}
	return nil
}

// verify runs the checks at the pin that follow the oracle's choice, in
// the clone at root: the oracle passes on the original tree; stubbing
// twice gives identical files, which are written under dir; the stubbed
// module builds; the oracle's tests build and start on the stub (no
// package initialization panics, which is what the runner checks before
// the agent starts); and they fail with the stub's panic. It returns the
// stub's tree hash.
func (k *Checker) verify(ctx context.Context, root, dir string, oracle definition.Oracle, stubFn func() ([]stub.File, error)) (string, error) {
	testArgs := append([]string{"test"}, oracle.Test...)
	if err := k.mustPass(ctx, root, "oracle tests fail before stubbing", testArgs...); err != nil {
		return "", err
	}
	hash, err := writeStub(dir, stubFn)
	if err != nil {
		return "", err
	}
	for _, step := range []struct {
		why  string
		args []string
	}{
		{"stubbed module does not build", []string{"build", "./..."}},
		{"oracle tests do not start on the stub", append([]string{"test", "-count=1", "-run", "^$"}, oracle.Test...)},
	} {
		if err := k.mustPass(ctx, root, step.why, step.args...); err != nil {
			return "", err
		}
	}
	out, err := RunGo(ctx, root, k.env, testArgs...)
	switch {
	case err == nil:
		return "", rejected("oracle tests still pass on the stub")
	case testBuildBroken(out):
		return "", rejected("stub breaks the test build: %s", Tail(out))
	case !strings.Contains(out, stub.Panic):
		return "", rejected("oracle tests fail on the stub without reaching it: %s", Tail(out))
	}
	return hash, nil
}

// writeStub stubs twice with stubFn, rejects a stub that differs between
// the two, writes the first under dir and returns its tree hash.
func writeStub(dir string, stubFn func() ([]stub.File, error)) (string, error) {
	var hashes [2]string
	var files []stub.File
	for i := range hashes {
		f, err := stubFn()
		if err != nil {
			return "", rejected("%s", err)
		}
		files, hashes[i] = f, stub.TreeHash(f)
	}
	if hashes[0] != hashes[1] {
		return "", rejected("stubbing is not deterministic")
	}
	if err := stub.Write(dir, files); err != nil {
		return "", rejected("%s", err)
	}
	return hashes[0], nil
}

// mustPass runs the go command with args in root and rejects the
// candidate with why and the end of the output when it fails.
func (k *Checker) mustPass(ctx context.Context, root, why string, args ...string) error {
	if out, err := RunGo(ctx, root, k.env, args...); err != nil {
		// Paths in a panic's stack are the clone's; keep them
		// module-relative so the reason does not depend on --work.
		return rejected("%s: %s", why, Tail(strings.ReplaceAll(out, root+string(filepath.Separator), "")))
	}
	return nil
}

// GoVersion returns the version of the go command on PATH.
func GoVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOVERSION").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOVERSION: %w", err)
	}
	return string(bytes.TrimSpace(out)), nil
}
