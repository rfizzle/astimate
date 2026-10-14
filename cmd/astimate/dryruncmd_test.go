package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/replay/summary"
)

// scorerFunc returns the source of a function whose body is long enough
// for the duplication rules to see a copy of it.
func scorerFunc(name string) string {
	return "\nfunc " + name + "(a, b, c int) (int, int, int) {\n" +
		"\tx := a*b + c - a/b + c*c - a\n\ty := b*c + a - b/c + a*a - b\n\tz := c*a + b - c/a + b*b - c\n" +
		"\tx, y, z = x+y*z, y+z*x, z+x*y\n\treturn x - y + z, y - z + x, z - x + y\n}\n"
}

// dryrunRepo builds a git repository holding a Go module, four commits:
// the module with package a; a copy of a function in a, which the
// dup_blocks ratchet fails; a README; and a small function added to a.
func dryrunRepo(t *testing.T) string {
	t.Helper()
	scorer := scorerFunc
	return buildRepo(t, []repoCommit{
		{map[string]string{"go.mod": "module example.com/fix\n\ngo 1.22\n", "a/a.go": "package a\n\nfunc sum(xs []int) int {\n\tt := 0\n\tfor _, x := range xs {\n\t\tt += x\n\t}\n\treturn t\n}\n"}, "feat: add package a"},
		{map[string]string{"a/dup.go": "package a\n" + scorer("first") + scorer("second")}, "feat: add two scorers"},
		{map[string]string{"README.md": "# fix\n"}, "docs: add a readme"},
		{map[string]string{"a/one.go": "package a\n\nfunc one() int { return 1 }\n"}, "feat: add one"},
	})
}

// repoCommit is one commit of a built repository.
type repoCommit struct {
	files   map[string]string
	message string
}

// buildRepo builds a git repository with one commit per element of
// commits, dated a day apart, and returns its directory.
func buildRepo(t *testing.T, commits []repoCommit) string {
	t.Helper()
	dir := t.TempDir()
	gitRun := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", args[0], err, out)
		}
	}
	gitRun(nil, "init", "-q", "-b", "master")
	for i, c := range commits {
		for name, content := range c.files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		gitRun(nil, "add", "-A")
		date := "2026-01-0" + strconv.Itoa(i+1) + "T12:00:00Z"
		gitRun([]string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date},
			"-c", "user.name=Ann", "-c", "user.email=ann@example.com", "-c", "commit.gpgsign=false",
			"commit", "-q", "-m", c.message)
	}
	return dir
}

// TestDryrunLast checks dryrun --last 3 on a built repository: it replays
// the last three commits, reports the share of those that touched a
// package the gate would have failed and the per-rule table, and exits 0
// although the gate would have failed one.
func TestDryrunLast(t *testing.T) {
	repo := dryrunRepo(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dryrun", repo, "--last", "3"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{
		"replayed 3 of 3 commits",
		"3 checked, 0 not loaded, 2 touched a package",
		"would have failed 1 of 3 commits checked (33.3%), 1 of the 2 that touched a package (50.0%)",
		"rule", "dup_blocks", "over budget",
		"most-failed packages", "a  1 commit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	// The table's dup_blocks row: one commit, a third of those checked,
	// none by it alone (duplication_pct is merged into it).
	if !strings.Contains(got, "dup_blocks            1   33.3%      0  over budget") {
		t.Errorf("dup_blocks row not as expected:\n%s", got)
	}
}

// TestDryrunSuggest checks that --suggest writes a configuration that
// parses and sets warn on exactly the rules over --budget.
func TestDryrunSuggest(t *testing.T) {
	repo := dryrunRepo(t)
	file := filepath.Join(t.TempDir(), "suggested.yaml")
	var stdout, stderr bytes.Buffer
	args := []string{"dryrun", repo, "--last", "3", "--budget", "30", "--suggest", file}
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	cfg, err := config.Load(file)
	if err != nil {
		t.Fatalf("suggestion does not parse: %v", err)
	}
	over := []string{"dup_blocks", "duplication_pct"}
	for _, lang := range []string{"go", "typescript"} {
		for _, r := range cfg.ForLanguage(lang).Thresholds {
			if want := slices.Contains(over, r.Metric); r.Warns() != want {
				t.Errorf("%s rule %s: warns %v, want %v", lang, r.Metric, r.Warns(), want)
			}
		}
	}

	// Over a budget no rule exceeds, the suggestion is the configuration
	// unchanged.
	args = []string{"dryrun", repo, "--last", "3", "--budget", "60", "--suggest", file}
	if code := run(args, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	if data, err := os.ReadFile(file); err != nil || !bytes.Equal(data, config.Default()) {
		t.Errorf("suggestion with no rule over the budget differs from the embedded default (err %v)", err)
	}
}

// TestDryrunJSON checks --json prints the replay's rows, each commit's
// package rows before its commit row.
func TestDryrunJSON(t *testing.T) {
	repo := dryrunRepo(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dryrun", repo, "--last", "3", "--json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	var kinds []string
	for line := range strings.Lines(stdout.String()) {
		switch {
		case strings.Contains(line, `"package":`):
			kinds = append(kinds, "package")
		case strings.Contains(line, `"packages_changed":`):
			kinds = append(kinds, "commit")
		default:
			t.Errorf("line is neither a package nor a commit row: %s", line)
		}
	}
	// The scorers and one.go each check a and the module row; the README
	// checks nothing.
	want := []string{"package", "package", "commit", "commit", "package", "package", "commit"}
	if !slices.Equal(kinds, want) {
		t.Errorf("rows %v, want %v", kinds, want)
	}
}

func TestDryrunUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "zero last", args: []string{"--last", "0"}},
		{name: "zero parallel", args: []string{"--parallel", "0"}},
		{name: "budget over 100", args: []string{"--budget", "101"}},
		{name: "negative timeout", args: []string{"--timeout", "-1s"}},
		{name: "two roots", args: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := run(append([]string{"dryrun"}, tt.args...), &stdout, &stderr); code != exitUsage {
				t.Errorf("exit %d, want %d; stderr %q", code, exitUsage, stderr.String())
			}
		})
	}
}

// TestDryrunSummaryGolden renders a fixed summary and compares it with
// testdata/dryrun_summary.txt (regenerate with UPDATE_GOLDEN=1).
func TestDryrunSummaryGolden(t *testing.T) {
	t.Parallel()

	s := &summary.Summary{
		Commits: 50,
		NotLoaded: []summary.NotLoaded{{Commit: "0123456789abcdef0123456789abcdef01234567", Subject: "build: add a cgo package",
			Error: "internal/native: skipped, it does not load (missing module):\n  example.com/absent/client"}},
		Checked: 49,
		Touched: 40,
		Failed:  9,
		Rules: []summary.RuleCount{
			{Metric: "sloc", Failed: 5, Alone: 3},
			{Metric: "dup_blocks", Failed: 4, Alone: 2},
			{Metric: "duplication_pct", Failed: 1},
		},
		Packages: []summary.PackageCount{
			{Package: "internal/engine", Failed: 4}, {Package: "<module>", Failed: 2}, {Package: "cmd/astimate", Failed: 2},
			{Package: "internal/report", Failed: 1}, {Package: "internal/gate", Failed: 1},
		},
	}
	var buf bytes.Buffer
	if err := writeDryrun(&buf, s, dryrunHeader{Selected: 50, Budget: 0.1, Wall: 3*time.Minute + 12*time.Second}); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "dryrun_summary.txt")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file (regenerate with UPDATE_GOLDEN=1): %v", err)
	}
	if got := buf.String(); got != string(want) {
		t.Errorf("summary differs from %s (regenerate with UPDATE_GOLDEN=1)\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
}
