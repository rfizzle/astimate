package main

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
)

// defaultEnv is the environment every go command of the experiment runs
// with: no cgo (corpus criterion 5), no workspace, and the local toolchain
// only, so no toolchain is downloaded behind the run's back.
func defaultEnv() []string {
	return []string{"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off"}
}

// commandTimeout bounds one go build or go test during the checks.
const commandTimeout = 20 * time.Minute

// errCheck marks a candidate that failed a check; its text is the reason.
var errCheck = errors.New("rejected")

// runGo runs the go command in dir with env added to the process
// environment and returns its combined output.
func runGo(ctx context.Context, dir string, env []string, args ...string) (string, error) {
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
// needs network.
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

// ownPattern is the go package pattern of the module-relative dir.
func ownPattern(dir string) string {
	if dir == "." {
		return "."
	}
	return "./" + dir
}

// importerTests returns, sorted, the patterns of the packages in the module
// at root, other than pkg itself, that import pkg from any file and have
// test files: the tests that check an untested package's behavior.
func importerTests(ctx context.Context, root, module, pkg string, env []string) ([]string, error) {
	const format = `{{.ImportPath}}|{{join .Imports ","}},{{join .TestImports ","}},{{join .XTestImports ","}}|{{len .TestGoFiles}}{{len .XTestGoFiles}}`
	out, err := runGo(ctx, root, env, "list", "-e", "-f", format, "./...")
	if err != nil {
		return nil, fmt.Errorf("%w: go list: %s", errCheck, tail(out))
	}
	var pats []string
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimSpace(line), "|")
		if len(fields) != 3 || fields[0] == pkg || fields[2] == "00" {
			continue
		}
		if slices.Contains(strings.Split(fields[1], ","), pkg) {
			pats = append(pats, ownPattern(modRelDir(module, fields[0])))
		}
	}
	slices.Sort(pats)
	return pats, nil
}

// nonGoSources returns the files in dir whose implementation a Go stub
// cannot replace: assembly, C and object files.
func nonGoSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		switch filepath.Ext(e.Name()) {
		case ".s", ".c", ".cc", ".cpp", ".h", ".m", ".syso":
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// testBuildBroken reports whether go test output shows a package whose
// tests did not build, as opposed to tests that ran and failed.
func testBuildBroken(out string) bool {
	return strings.Contains(out, "[build failed]") || strings.Contains(out, "[setup failed]")
}

// tail returns the last lines of out on one line, for a rejection reason,
// with go test's elapsed times and "(cached)" markers dropped and runs of
// white space collapsed, so the same failure reads the same in every run.
func tail(out string) string {
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

// checker verifies candidates in clones under work, one clone per module,
// reused across that module's candidates.
type checker struct {
	work    string
	env     []string
	turnCap int
	// clones maps a module to its clone directory, or to the error that
	// made the module unusable.
	clones   map[string]string
	cloneErr map[string]error
}

// clone returns the checked clone of c's module: fetched at the pin, with
// go build ./... passing. The result is cached per module.
func (k *checker) clone(ctx context.Context, c Candidate) (string, error) {
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
func (k *checker) prepareClone(ctx context.Context, repo, commit, dir string) error {
	head, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != commit {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("clearing %s: %w", dir, err)
		}
		if err := CloneAt(ctx, repo, commit, dir); err != nil {
			return fmt.Errorf("%w: cloning: %w", errCheck, err)
		}
	} else if err := resetClone(ctx, dir); err != nil {
		return fmt.Errorf("%w: resetting clone: %w", errCheck, err)
	}
	if out, err := runGo(ctx, dir, k.env, "build", "./..."); err != nil {
		return fmt.Errorf("%w: module does not build at the pin: %s", errCheck, tail(out))
	}
	return nil
}

// check verifies candidate c at its pin: the package has no non-Go
// sources; its oracle has tests to run; the oracle passes on the original
// tree; stubbing twice gives identical files; the stubbed module builds;
// the stubbed oracle's tests build and fail with the stub's panic. The clone is restored to the
// pin afterwards.
func (k *checker) check(ctx context.Context, c Candidate) (exp Experiment, err error) {
	root, err := k.clone(ctx, c)
	if err != nil {
		return Experiment{}, err
	}
	defer func() {
		if rerr := resetClone(ctx, root); rerr != nil && err == nil {
			err = fmt.Errorf("restoring clone: %w", rerr)
		}
	}()
	dir := modRelDir(c.Row.Module, c.Row.Package)
	pkgDir := filepath.Join(root, filepath.FromSlash(dir))
	other, err := nonGoSources(pkgDir)
	if err != nil {
		return Experiment{}, fmt.Errorf("%w: %w", errCheck, err)
	}
	if len(other) > 0 {
		return Experiment{}, fmt.Errorf("%w: non-Go sources a stub cannot replace: %s", errCheck, strings.Join(other, ", "))
	}
	oracle := Oracle{Test: []string{ownPattern(dir)}, Build: []string{"./..."}}
	if !c.Row.Metrics.HasTests {
		oracle.Test, err = importerTests(ctx, root, c.Row.Module, c.Row.Package, k.env)
		if err != nil {
			return Experiment{}, err
		}
		if len(oracle.Test) == 0 {
			return Experiment{}, fmt.Errorf("%w: no importer in the module has tests", errCheck)
		}
	}
	testArgs := append([]string{"test"}, oracle.Test...)
	if out, err := runGo(ctx, root, k.env, testArgs...); err != nil {
		return Experiment{}, fmt.Errorf("%w: oracle tests fail before stubbing: %s", errCheck, tail(out))
	}
	first, err := StubPackage(pkgDir)
	if err != nil {
		return Experiment{}, fmt.Errorf("%w: %w", errCheck, err)
	}
	second, err := StubPackage(pkgDir)
	if err != nil {
		return Experiment{}, fmt.Errorf("%w: %w", errCheck, err)
	}
	hash := TreeHash(first)
	if TreeHash(second) != hash {
		return Experiment{}, fmt.Errorf("%w: stubbing is not deterministic", errCheck)
	}
	if err := WriteStub(pkgDir, first); err != nil {
		return Experiment{}, fmt.Errorf("%w: %w", errCheck, err)
	}
	if out, err := runGo(ctx, root, k.env, "build", "./..."); err != nil {
		return Experiment{}, fmt.Errorf("%w: stubbed module does not build: %s", errCheck, tail(out))
	}
	out, err := runGo(ctx, root, k.env, testArgs...)
	switch {
	case err == nil:
		return Experiment{}, fmt.Errorf("%w: oracle tests still pass on the stub", errCheck)
	case testBuildBroken(out):
		return Experiment{}, fmt.Errorf("%w: stub breaks the test build: %s", errCheck, tail(out))
	case !strings.Contains(out, stubPanic):
		return Experiment{}, fmt.Errorf("%w: oracle tests fail on the stub without reaching it: %s", errCheck, tail(out))
	}
	return newExperiment(c, oracle, hash, k.turnCap), nil
}

// ApplyStub stubs the package of e in the clone at root and checks the
// result against e.StubSHA256, so a runner starts from the tree the
// selection verified.
func ApplyStub(root string, e *Experiment) error {
	pkgDir := filepath.Join(root, filepath.FromSlash(e.Dir))
	files, err := StubPackage(pkgDir)
	if err != nil {
		return err
	}
	if got := TreeHash(files); got != e.StubSHA256 {
		return fmt.Errorf("stubbing %s: tree hash %s, want %s (different toolchain or tree?)", e.Package, got, e.StubSHA256)
	}
	return WriteStub(pkgDir, files)
}

// goVersion returns the version of the go command on PATH.
func goVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOVERSION").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOVERSION: %w", err)
	}
	return string(bytes.TrimSpace(out)), nil
}
