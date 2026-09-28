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

	"github.com/rfizzle/astimate/calibration/replay/labels"
)

// step is one commit of a synthetic history: files to write (empty content
// deletes) and the full message.
type step struct {
	files   map[string]string
	message string
}

const (
	barGo = "package a\n\ntype T struct{}\n\n// Bar is one.\nfunc Bar() int { return 1 }\n\nfunc (t *T) M() {}\n"
	fooGo = barGo + "\nfunc Foo(x int) int {\n\tif x > 0 {\n\t\treturn x\n\t}\n\treturn 0\n}\n"
	bazGo = "package a\n\nfunc Baz() {}\n"
)

// history is the synthetic history of the rule test: commit 1 adds Foo,
// which the fix in commit 5 changes; commit 2 adds Baz, which commit 4
// reverts.
func history() []step {
	return []step{
		{map[string]string{"go.mod": "module example.com/a\n\ngo 1.22\n", "a.go": barGo}, "feat: add bar"},
		{map[string]string{"a.go": fooGo}, "feat: add foo\n\nCo-Authored-By: Claude <noreply@anthropic.com>"},
		{map[string]string{"b.go": bazGo}, "feat: add baz"},
		{map[string]string{"README.md": "# a\n"}, "docs: add a readme"},
		{map[string]string{"b.go": ""}, "Revert \"feat: add baz\"\n\nThis reverts commit %s."},
		{map[string]string{"a.go": strings.Replace(fooGo, "return x", "return x + 1", 1), "a_test.go": "package a\n\nfunc TestFoo() {}\n"},
			"fix(a): correct foo"},
	}
}

// buildHistory commits steps into a new repository, writes the replay data
// directory the labeler reads (run.json and commits.jsonl), and returns the
// repository, the data directory and the hashes. A %s in a message is
// replaced by the hash of the commit two steps earlier.
func buildHistory(t *testing.T, steps []step) (repo, data string, hashes []string) {
	t.Helper()
	repo, data = t.TempDir(), t.TempDir()
	g := gitRepo{dir: repo}
	mustGit(t, g, "init", "-q", "-b", "master")
	var rows bytes.Buffer
	for i, s := range steps {
		for name, content := range s.files {
			path := filepath.Join(repo, name)
			var err error
			if content == "" {
				err = os.Remove(path)
			} else {
				err = os.WriteFile(path, []byte(content), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		msg := s.message
		if i >= 2 {
			msg = strings.Replace(msg, "%s", hashes[i-2], 1)
		}
		mustGit(t, g, "add", "-A")
		date := "2026-01-0" + strconv.Itoa(i+1) + "T12:00:00Z"
		cmd := exec.Command("git", "-C", repo, "-c", "user.name=Ann", "-c", "user.email=ann@example.com",
			"-c", "commit.gpgsign=false", "commit", "-q", "-m", msg)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v: %s", err, out)
		}
		hashes = append(hashes, strings.TrimSpace(mustGit(t, g, "rev-parse", "HEAD")))
		row := commit{Commit: hashes[i], Subject: strings.SplitN(msg, "\n", 2)[0], AuthorName: "Ann", AuthorEmail: "ann@example.com"}
		if i > 0 {
			row.Parent = hashes[i-1]
		}
		if strings.Contains(msg, "Co-Authored-By: ") {
			row.CoAuthoredBy = []string{"Claude <noreply@anthropic.com>"}
		}
		line, _ := json.Marshal(row)
		rows.Write(append(line, '\n'))
	}
	writeFile(t, filepath.Join(data, "commits.jsonl"), rows.String())
	writeFile(t, filepath.Join(data, "run.json"), `{"source":{"range":"master"}}`)
	return repo, data, hashes
}

func mustGit(t *testing.T, g gitRepo, args ...string) string {
	t.Helper()
	out, err := g.output(context.Background(), args...)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// parts returns the rule parts that fired on l, with who fired them.
func parts(l *labels.Label) []string {
	var out []string
	for _, e := range l.Fired {
		out = append(out, e.Part+"@"+e.By[:7]+strings.Join(e.Functions, ","))
	}
	return out
}

func TestLabelRevertAndFixup(t *testing.T) {
	repo, data, h := buildHistory(t, history())
	f, err := labelData(context.Background(), gitRepo{dir: repo}, data, defaultWindow,
		&agentRule{CoAuthoredBy: []string{"noreply@anthropic.com"}})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		nil,
		{"fixup@" + h[5][:7] + "a.go:Foo"},
		{"revert@" + h[4][:7], "fixup@" + h[4][:7] + "b.go:Baz"},
		nil, nil, nil,
	}
	for i := range f.Commits {
		l := &f.Commits[i]
		if got := parts(l); !slices.Equal(got, want[i]) {
			t.Errorf("commit %d fired %v, want %v (reason %q)", i, got, want[i], l.Reason)
		}
		if wantVerdict := map[bool]labels.Verdict{true: labels.Block, false: labels.Allow}[want[i] != nil]; l.Verdict != wantVerdict {
			t.Errorf("commit %d verdict %s, want %s", i, l.Verdict, wantVerdict)
		}
		if l.Provenance != labels.Rule || l.Agent == nil || *l.Agent != (i == 1) {
			t.Errorf("commit %d provenance %s agent %v", i, l.Provenance, l.Agent)
		}
	}
	if !strings.Contains(f.Commits[3].Reason, "changed no function") {
		t.Errorf("commit 3 reason %q", f.Commits[3].Reason)
	}
}

func TestLabelWindow(t *testing.T) {
	repo, data, _ := buildHistory(t, history())
	f, err := labelData(context.Background(), gitRepo{dir: repo}, data, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if l := f.Commits[1]; l.Verdict != labels.Allow || l.Agent != nil || !strings.Contains(l.Reason, "next 3 first-parent") {
		t.Errorf("commit 1 with a 3-commit window: %s %q, agent %v", l.Verdict, l.Reason, l.Agent)
	}
	if l := f.Commits[4]; !strings.Contains(l.Reason, "window cut short by the end of the range: 1 of 3") {
		t.Errorf("commit 4 reason %q, want the short window noted", l.Reason)
	}
}

func TestRun(t *testing.T) {
	repo, data, _ := buildHistory(t, history())
	corpus := filepath.Join(t.TempDir(), "corpus.yaml")
	writeFile(t, corpus, "repositories:\n  - name: a\n    repo: https://example.com/a.git\n    commit: "+
		strings.Repeat("a", 40)+"\n    range: master.."+strings.Repeat("a", 40)+
		"\n    history: linear\n    agent: {co_authored_by: [anthropic]}\n    data: d\n    labels: l\n")
	out := filepath.Join(t.TempDir(), "a.yaml")
	var stderr bytes.Buffer
	err := run(context.Background(), []string{"--repo", repo, "--data", data, "--out", out, "--corpus", corpus, "--name", "a"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stderr.String()); got != "commits=6 agent=1 block=2 revert=1 fixup=2 both=1" {
		t.Errorf("summary %q", got)
	}
	f, err := labels.Load(out)
	if err != nil || len(f.Commits) != 6 || f.Source.Range != "master" || !strings.Contains(f.Rule, "within the next 20") {
		t.Fatalf("labels file: %+v, %v", f, err)
	}
	if err := run(context.Background(), []string{"--repo", repo, "--data", data, "--out", out, "--corpus", corpus, "--name", "b"}, &stderr); err == nil {
		t.Error("run with an unknown corpus entry succeeded")
	}
	if err := run(context.Background(), []string{"--repo", repo, "--data", t.TempDir(), "--out", out}, &stderr); err == nil {
		t.Error("run without replay data succeeded")
	}
}

func TestParseFlags(t *testing.T) {
	base := []string{"--repo", "r", "--data", "d", "--out", "o"}
	tests := []struct {
		name string
		args []string
		ok   bool
	}{
		{"minimal", base, true},
		{"with corpus", append(slices.Clone(base), "--corpus", "c", "--name", "n"), true},
		{"corpus without name", append(slices.Clone(base), "--corpus", "c"), false},
		{"missing out", base[:4], false},
		{"zero window", append(slices.Clone(base), "--window", "0"), false},
		{"extra argument", append(slices.Clone(base), "x"), false},
		{"unknown flag", []string{"--nope"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseFlags(tc.args, &bytes.Buffer{})
			if (err == nil) != tc.ok {
				t.Fatalf("parseFlags(%v) = %+v, %v", tc.args, o, err)
			}
		})
	}
}

func TestIsFixSubject(t *testing.T) {
	tests := map[string]bool{
		"fix: nil map":           true,
		"Fix(gate)!: off by one": true,
		"fixes #12":              true,
		"fixup! feat: x":         true,
		`Revert "feat: x"`:       true,
		"revert(api): x":         true,
		"FIX":                    true,
		"fixture: add a case":    false,
		"prefix the path":        false,
		"feat: fix later":        false,
		"hotfix: x":              false,
		"":                       false,
	}
	for subject, want := range tests {
		if got := isFixSubject(subject); got != want {
			t.Errorf("isFixSubject(%q) = %v, want %v", subject, got, want)
		}
	}
}

func TestRevertedSubject(t *testing.T) {
	tests := []struct {
		subject, want string
		ok            bool
	}{
		{`Revert "feat: x"`, "feat: x", true},
		{`Revert "feat: x (#12)" (#15)`, "feat: x (#12)", true},
		{`Revert "Revert "feat: x""`, `Revert "feat: x"`, true},
		{`Revert "`, "", false},
		{"revert: x", "", false},
	}
	for _, tc := range tests {
		got, ok := revertedSubject(tc.subject)
		if got != tc.want || ok != tc.ok {
			t.Errorf("revertedSubject(%q) = %q, %v", tc.subject, got, ok)
		}
	}
	if got := revertedHashes("x\nThis reverts commit ABCDEF1234.\nthis reverts commit abc (too short)"); !slices.Equal(got, []string{"abcdef1234"}) {
		t.Errorf("revertedHashes = %v", got)
	}
}

func TestParseDiff(t *testing.T) {
	patch := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -3 +3,2 @@ func F() {\n-\ta := 1\n+\ta := 2\n+\tb := 3\n" +
		"@@ -9,2 +10,0 @@\n---- not a header\n-x\n" +
		"diff --git a/y.go b/y.go\nnew file mode 100644\n--- /dev/null\n+++ b/y.go\n@@ -0,0 +1 @@\n+package y\n"
	got := parseDiff([]byte(patch))
	want := []fileDiff{
		{oldPath: "x.go", newPath: "x.go", old: [][2]int{{3, 1}, {9, 2}}, new: [][2]int{{3, 2}}, inHunks: true},
		{oldPath: "", newPath: "y.go", new: [][2]int{{1, 1}}, inHunks: true},
	}
	if len(got) != len(want) {
		t.Fatalf("parseDiff = %+v", got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.oldPath != w.oldPath || g.newPath != w.newPath || !slices.Equal(g.old, w.old) || !slices.Equal(g.new, w.new) {
			t.Errorf("file %d = %+v, want %+v", i, g, w)
		}
	}
	if _, _, ok := parseHunk("@@ -x +1 @@"); ok {
		t.Error("parseHunk accepted a bad range")
	}
}

func TestFuncsAt(t *testing.T) {
	src := []byte("package a\n\ntype G[T any] struct{}\n\n// M doc.\nfunc (g *G[T]) M() {\n}\n\nfunc F() {}\n")
	tests := []struct {
		lines [][2]int
		want  []string
	}{
		{[][2]int{{6, 1}}, []string{"G.M"}},
		{[][2]int{{5, 1}}, nil}, // the doc comment only
		{[][2]int{{7, 3}}, []string{"G.M", "F"}},
	}
	for _, tc := range tests {
		if got := funcsAt(src, tc.lines); !slices.Equal(got, tc.want) {
			t.Errorf("funcsAt(%v) = %v, want %v", tc.lines, got, tc.want)
		}
	}
	cmd := []byte("package a\n\nvar cmd = &C{\n\tUse: \"x\",\n\tRunE: func() error {\n\t\treturn nil\n\t},\n}\n\nvar n, m = 1, 2\n")
	for lines, want := range map[[2]int][]string{{6, 1}: {"var cmd"}, {4, 1}: nil, {10, 1}: nil} {
		if got := funcsAt(cmd, [][2]int{lines}); !slices.Equal(got, want) {
			t.Errorf("funcsAt(cmd, %v) = %v, want %v", lines, got, want)
		}
	}
	if got := funcsAt([]byte("not go"), [][2]int{{1, 1}}); got != nil {
		t.Errorf("funcsAt of unparsable source = %v", got)
	}
}
