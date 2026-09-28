package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fixtureRepo builds a git repository in a temporary directory, one commit
// per element of commits, each writing its files (an empty content deletes
// the file), with fixed dates and the given message. It returns the
// repository path and the commit hashes, oldest first.
func fixtureRepo(t *testing.T, commits []fixtureCommit) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "master")
	hashes := make([]string, 0, len(commits))
	for i, c := range commits {
		for name, content := range c.files {
			path := filepath.Join(dir, name)
			if content == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gitT(t, dir, "add", "-A")
		date := "2026-01-0" + strconv.Itoa(i+1) + "T12:00:00Z"
		cmd := exec.Command("git", "-c", "user.name=Ann Author", "-c", "user.email=ann@example.com",
			"-c", "commit.gpgsign=false", "commit", "-q", "-m", c.message)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v: %s", err, out)
		}
		hashes = append(hashes, strings.TrimSpace(gitT(t, dir, "rev-parse", "HEAD")))
	}
	return dir, hashes
}

// fixtureCommit is one commit of a fixture repository.
type fixtureCommit struct {
	files   map[string]string
	message string
}

// gitT runs git in dir and fails t on error.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// goEnv keeps the fixture's module load from the network and from any
// go.work around the test.
func goEnv(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
}

const goMod = "module example.com/fix\n\ngo 1.22\n"

const pkgA = `package a

func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}
`

// dupFunc is a function long enough to be a duplicate block when repeated.
func dupFunc(name string) string {
	return `
func ` + name + `(a, b, c int) (int, int, int) {
	x := a*b + c - a/b + c*c - a
	y := b*c + a - b/c + a*a - b
	z := c*a + b - c/a + b*b - c
	x, y, z = x+y*z, y+z*x, z+x*y
	return x - y + z, y - z + x, z - x + y
}
`
}

// threeCommits is a small module, a commit adding a duplicate block to its
// package, and a commit changing no Go file.
func threeCommits() []fixtureCommit {
	return []fixtureCommit{
		{files: map[string]string{"go.mod": goMod, "a/a.go": pkgA}, message: "feat: add package a"},
		{files: map[string]string{"a/dup.go": "package a\n" + dupFunc("first") + dupFunc("second")},
			message: "feat: add two scorers\n\nCo-Authored-By: Bot <bot@example.com>\nRefs: 12"},
		{files: map[string]string{"README.md": "# fix\n"}, message: "docs: add a readme"},
	}
}

// replayT runs the command with args and returns its exit code and
// stderr.
func replayT(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stderr bytes.Buffer
	code := run(context.Background(), args, &stderr)
	return code, stderr.String()
}

func TestReplayRows(t *testing.T) {
	goEnv(t)
	repo, hashes := fixtureRepo(t, threeCommits())
	out := filepath.Join(t.TempDir(), "out")
	if code, log := replayT(t, "--repo", repo, "--out", out); code != exitOK {
		t.Fatalf("exit %d: %s", code, log)
	}
	commits, err := readRows[commitRow](filepath.Join(out, commitsFile))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := readRows[packageRow](filepath.Join(out, packagesFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := commitHashes(commits); !slices.Equal(got, hashes) {
		t.Fatalf("commit rows %v, want %v", got, hashes)
	}
	tests := []struct {
		name       string
		baseline   string
		packages   []string
		violations []string
		coAuthors  []string
	}{
		{name: "root", baseline: baselineEmpty, packages: []string{"a"}},
		// The copy is most of the package: its duplication_pct breaks both
		// the rule's max and its max_delta.
		{name: "duplicate", baseline: baselineParent, packages: []string{"a"},
			violations: []string{"dup_blocks", "duplication_pct", "duplication_pct"},
			coAuthors:  []string{"Bot <bot@example.com>"}},
		{name: "unrelated", baseline: baselineParent},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := commits[i]
			if !c.Loaded || c.Error != "" || c.Baseline != tt.baseline {
				t.Fatalf("loaded %v, error %q, baseline %q", c.Loaded, c.Error, c.Baseline)
			}
			if !slices.Equal(c.PackagesChanged, nonNil(tt.packages)) || !slices.Equal(c.CoAuthoredBy, nonNil(tt.coAuthors)) {
				t.Errorf("packages %v, co-authors %v", c.PackagesChanged, c.CoAuthoredBy)
			}
			var got []string
			for _, p := range pkgs {
				if p.Commit == c.Commit {
					for _, v := range p.Violations {
						got = append(got, v.Metric)
					}
				}
			}
			if !slices.Equal(got, tt.violations) || *c.Passed != (len(tt.violations) == 0) || c.Violations != len(got) {
				t.Errorf("violations %v (commit passed %v, %d), want %v", got, *c.Passed, c.Violations, tt.violations)
			}
		})
	}
	dup := packageRowOf(t, pkgs, hashes[1], "a")
	if dup.New || dup.Base == nil || dup.SLOCDelta != dup.Metrics.SLOC-dup.Base.SLOC || dup.SLOCDelta <= 0 {
		t.Errorf("duplicate commit row: new %v, sloc delta %d", dup.New, dup.SLOCDelta)
	}
	if v := dup.Violations[0]; v.Location == nil || !strings.HasPrefix(v.Location.File, "a/dup.go") {
		t.Errorf("dup_blocks location %+v, want a/dup.go", v.Location)
	}
	if root := packageRowOf(t, pkgs, hashes[0], "a"); !root.New || root.Base != nil || root.SLOCDelta != root.Metrics.SLOC {
		t.Errorf("root commit row: new %v, sloc delta %d", root.New, root.SLOCDelta)
	}
	if mod := packageRowOf(t, pkgs, hashes[0], "<module>"); !mod.New || mod.Base != nil {
		t.Errorf("root commit module row: new %v, base %v", mod.New, mod.Base)
	}
	if got := commits[1].Trailers["Refs"]; !slices.Equal(got, []string{"12"}) {
		t.Errorf("trailers %v", commits[1].Trailers)
	}
	assertNoWorktrees(t, repo)
}

func TestReplayResume(t *testing.T) {
	goEnv(t)
	repo, hashes := fixtureRepo(t, threeCommits())
	out := filepath.Join(t.TempDir(), "out")
	if code, log := replayT(t, "--repo", repo, "--out", out, "--parallel", "2"); code != exitOK {
		t.Fatalf("exit %d: %s", code, log)
	}
	before := snapshotFiles(t, out)
	code, log := replayT(t, "--repo", repo, "--out", out)
	if code != exitOK || !strings.Contains(log, "pending=0") || !strings.Contains(log, "skipped=3") {
		t.Fatalf("rerun exit %d: %s", code, log)
	}
	if after := snapshotFiles(t, out); !mapsEqual(before, after) {
		t.Error("a rerun over the same range changed the output")
	}

	// An interrupted write: the last commit's package rows are on disk,
	// its commit row is torn. The rerun drops both and records it once.
	commits := before[commitsFile]
	cut := bytes.LastIndexByte(commits[:len(commits)-1], '\n') + 1
	torn := append(slices.Clone(commits[:cut]), commits[cut:cut+20]...)
	orphan := `{"commit":"` + hashes[2] + `","package":"a"}` + "\n"
	writeFile(t, filepath.Join(out, commitsFile), torn)
	writeFile(t, filepath.Join(out, packagesFile), append(slices.Clone(before[packagesFile]), orphan...))
	if code, log := replayT(t, "--repo", repo, "--out", out); code != exitOK || !strings.Contains(log, "pending=1") {
		t.Fatalf("resume exit %d: %s", code, log)
	}
	after := snapshotFiles(t, out)
	if !bytes.Equal(after[packagesFile], before[packagesFile]) {
		t.Errorf("packages.jsonl after resume:\n%s\nwant:\n%s", after[packagesFile], before[packagesFile])
	}
	rows, err := readRows[commitRow](filepath.Join(out, commitsFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := commitHashes(rows); !slices.Equal(got, hashes) {
		t.Errorf("commit rows %v, want each of %v once", got, hashes)
	}

	// Rows gated under another configuration are not mixed with new ones.
	other := bytes.Replace(after[commitsFile], []byte(`"config_version":"`), []byte(`"config_version":"other-`), 1)
	writeFile(t, filepath.Join(out, commitsFile), other)
	if code, log := replayT(t, "--repo", repo, "--out", out); code == exitOK || !strings.Contains(log, "another --out") {
		t.Errorf("resume over another config's rows: exit %d: %s", code, log)
	}
}

func TestReplayNotLoading(t *testing.T) {
	goEnv(t)
	repo, hashes := fixtureRepo(t, []fixtureCommit{
		{files: map[string]string{"README.md": "# fix\n"}, message: "docs: start"},
		{files: map[string]string{"go.mod": "module example.com/fix\n\ngo 1.22\n\nrequire (\n", "a/a.go": pkgA},
			message: "feat: add a broken go.mod"},
		{files: map[string]string{"go.mod": goMod, "a/a.go": pkgA + "\nfunc one() int { return 1 }\n"},
			message: "fix: repair go.mod"},
	})
	out := filepath.Join(t.TempDir(), "out")
	if code, log := replayT(t, "--repo", repo, "--out", out); code != exitOK {
		t.Fatalf("exit %d: %s", code, log)
	}
	commits, err := readRows[commitRow](filepath.Join(out, commitsFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := commitHashes(commits); !slices.Equal(got, hashes) {
		t.Fatalf("commit rows %v, want %v", got, hashes)
	}
	for i, c := range commits[:2] {
		t.Logf("commit %d: %s", i, c.Error)
		if c.Loaded || c.Error == "" || c.Passed != nil || strings.Contains(c.Error, os.TempDir()) {
			t.Errorf("commit %d: loaded %v, passed %v, error %q; want recorded as not loaded", i, c.Loaded, c.Passed, c.Error)
		}
	}
	// The repair loads, but its parent's go.mod does not: the baseline
	// fails, and the commit is recorded with that error, not fatal.
	if c := commits[2]; c.Baseline != baselineParent || c.Loaded || !strings.Contains(c.Error, "extracting baseline at "+hashes[1]) {
		t.Errorf("repair commit: baseline %q, loaded %v, error %q", c.Baseline, c.Loaded, c.Error)
	}
	var info runInfo
	if err := json.Unmarshal(readFile(t, filepath.Join(out, runFile)), &info); err != nil {
		t.Fatal(err)
	}
	if info.Totals.Commits != 3 || info.Totals.NotLoaded != 3 || info.Source["first"] != hashes[0] || info.Source["last"] != hashes[2] {
		t.Errorf("run.json totals %+v, source %+v", info.Totals, info.Source)
	}
}

// readRows decodes every line of the JSON-lines file at path.
func readRows[R any](path string) ([]R, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []R
	for line := range bytes.Lines(data) {
		var r R
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func TestReplayDeterministic(t *testing.T) {
	goEnv(t)
	repo, _ := fixtureRepo(t, threeCommits())
	var got [2][]byte
	for i := range got {
		out := filepath.Join(t.TempDir(), "out")
		if code, log := replayT(t, "--repo", repo, "--out", out, "--parallel", strconv.Itoa(1+2*i)); code != exitOK {
			t.Fatalf("exit %d: %s", code, log)
		}
		rows, err := readRows[commitRow](filepath.Join(out, commitsFile))
		if err != nil {
			t.Fatal(err)
		}
		for j := range rows {
			rows[j].WallMS = 0
		}
		data, err := json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		got[i] = append(data, readFile(t, filepath.Join(out, packagesFile))...)
	}
	if !bytes.Equal(got[0], got[1]) {
		t.Errorf("two replays differ:\n%s\n%s", got[0], got[1])
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		ok   bool
	}{
		{name: "defaults", ok: true},
		{name: "extra argument", args: []string{"x"}},
		{name: "zero parallel", args: []string{"--parallel", "0"}},
		{name: "option as range", args: []string{"--range", "--all"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFlags(tt.args, &bytes.Buffer{})
			if (err == nil) != tt.ok {
				t.Errorf("err %v, want ok %v", err, tt.ok)
			}
		})
	}
}

func TestParseTrailers(t *testing.T) {
	got := parseTrailers("Co-authored-by: A <a@x>\nCo-Authored-By: B <b@x>\nSigned-off-by: C\n\n")
	if want := []string{"A <a@x>", "B <b@x>"}; !slices.Equal(coAuthors(got), want) {
		t.Errorf("co-authors %v, want %v", coAuthors(got), want)
	}
	if !slices.Equal(got["Signed-off-by"], []string{"C"}) || len(parseTrailers("")) != 0 {
		t.Errorf("trailers %v", got)
	}
}

func TestDefaultOut(t *testing.T) {
	repo, _ := fixtureRepo(t, threeCommits()[:1])
	name := repoName(context.Background(), repo)
	if name != filepath.Base(repo) {
		t.Errorf("repo name %q, want %q", name, filepath.Base(repo))
	}
	if got := defaultRange(context.Background(), repo); got != "master" {
		t.Errorf("default range %q", got)
	}
}

func commitHashes(rows []commitRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Commit)
	}
	return out
}

func packageRowOf(t *testing.T, rows []packageRow, commit, pkg string) packageRow {
	t.Helper()
	for _, r := range rows {
		if r.Commit == commit && r.Package == pkg {
			return r
		}
	}
	t.Fatalf("no row for %s at %s", pkg, commit)
	return packageRow{}
}

func assertNoWorktrees(t *testing.T, repo string) {
	t.Helper()
	if n := strings.Count(gitT(t, repo, "worktree", "list", "--porcelain"), "worktree "); n != 1 {
		t.Errorf("%d worktrees left, want only the main one", n)
	}
}

func snapshotFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, name := range []string{commitsFile, packagesFile, runFile} {
		out[name] = readFile(t, filepath.Join(dir, name))
	}
	return out
}

func mapsEqual(a, b map[string][]byte) bool {
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return len(a) == len(b)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
