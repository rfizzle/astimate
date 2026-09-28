package pin

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/selection"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// treeFiles is a module with the tree t: t and t/sub tested, t/sub/deep
// untested, a main package t/cmd and a testdata package below it, both
// outside the tree's members, and a package use outside it that imports t.
func treeFiles() map[string]string {
	pkg := func(name, body string) string {
		return "// Package " + name + " is part of the tree.\npackage " + name + "\n\n" + body
	}
	test := func(name, call, want string) string {
		return "package " + name + "\n\nimport \"testing\"\n\nfunc TestIt(t *testing.T) {\n\tif " + call + " != " + want +
			" {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n"
	}
	return map[string]string{
		"go.mod":                 "module example.com/tree\n\ngo 1.27\n",
		"t/t.go":                 pkg("t", "import \"example.com/tree/t/sub\"\n\n// Inc returns n+1.\nfunc Inc(n int) int { return sub.Add(n, 1) }\n"),
		"t/t_test.go":            test("t", "Inc(1)", "2"),
		"t/sub/sub.go":           pkg("sub", "import \"example.com/tree/t/sub/deep\"\n\n// Add returns a+b.\nfunc Add(a, b int) int { return deep.Sum(a, b) }\n"),
		"t/sub/sub_test.go":      test("sub", "Add(1, 2)", "3"),
		"t/sub/deep/deep.go":     pkg("deep", "// Sum returns a+b.\nfunc Sum(a, b int) int { return a + b }\n"),
		"t/cmd/main.go":          "package main\n\nimport \"example.com/tree/t\"\n\nfunc main() { println(t.Inc(1)) }\n",
		"t/testdata/x/x.go":      "package x\n\nfunc X() int { return 1 }\n",
		"use/use.go":             pkg("use", "import \"example.com/tree/t\"\n\n// Two is 2.\nfunc Two() int { return t.Inc(1) }\n"),
		"use/use_test.go":        test("use", "Two()", "2"),
		"t/sub/deep/.keep/k.go":  "package k\n",
		"t/sub/deep/_skip/sk.go": "package sk\n",
	}
}

// TestCheckRejectsInitPanic checks that a package whose stub still
// panics during package initialization is rejected: here strings.Map
// calls the stubbed up at initialization, through a function value the
// stub cannot see.
func TestCheckRejectsInitPanic(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a repository and runs go test")
	}
	repo, commit := treeRepo(t, map[string]string{
		"go.mod":          "module example.com/fake\n\ngo 1.27\n",
		"lib/lib.go":      "// Package lib upper-cases.\npackage lib\n\nimport \"strings\"\n\n// Upper is AB.\nvar Upper = strings.Map(up, \"ab\")\n\nfunc up(r rune) rune { return r - 32 }\n",
		"lib/lib_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestUpper(t *testing.T) {\n\tif Upper != \"AB\" {\n\t\tt.Fatal(Upper)\n\t}\n}\n",
	})
	c := selection.Candidate{
		Row: selection.Row{Module: "example.com/fake", Commit: commit, Package: "example.com/fake/lib",
			Metrics: metrics.RawMetrics{HasTests: true}},
		Repo: "file://" + repo, Tier: score.TierOnePass,
	}
	_, err := NewChecker(t.TempDir(), DefaultEnv(), 5).Check(t.Context(), c)
	if err == nil || !strings.Contains(err.Error(), "do not start on the stub") {
		t.Fatalf("Check() = %v, want a rejection for an initialization panic", err)
	}
}

// treeRepo writes files into a new git repository and returns it and its
// commit.
func treeRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	for name, src := range files {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "tree")
	return repo, gitIn(t, repo, "rev-parse", "HEAD")
}

// treeCandidate returns the candidate of the tree t with members dirs.
func treeCandidate(repo, commit string, dirs []string, tested bool) selection.Candidate {
	var members []selection.Candidate
	for _, d := range dirs {
		m := metrics.RawMetrics{Files: 1, SLOC: 3, TokensEst: 20, TokensEstWithTests: 20, FuncCount: 1,
			UsesCgo: new(false), GeneratedFiles: new(0), HasTests: tested && d != "t/sub/deep"}
		members = append(members, selection.Candidate{
			Row:  selection.Row{Module: "example.com/tree", Commit: commit, Package: "example.com/tree/" + d, Metrics: m},
			Repo: "file://" + repo, Estimate: score.Rebuild{RebuildTokens: 20}, Tier: score.TierOnePass,
		})
	}
	return selection.Candidate{
		Row: selection.Row{Module: "example.com/tree", Commit: commit, Package: "example.com/tree/t",
			Metrics: definition.Aggregate(selection.MemberDefs(members))},
		Repo: "file://" + repo, Estimate: score.Rebuild{RebuildTokens: float64(20 * len(dirs))}, Tier: score.TierOnePass,
		Members: members,
	}
}

// TestTreeStubAndOracle stubs the tree t of a module with nested packages
// and checks that every package below it is stubbed and nothing else, that
// the tree builds and its tests start, and that the oracle's tests fail
// on the stub; then that CheckTree verifies the same tree and restores the
// clone.
func TestTreeStubAndOracle(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a repository and runs go test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not found")
	}
	repo, commit := treeRepo(t, treeFiles())
	env := DefaultEnv()
	dirs, err := TreeMembers(t.Context(), repo, "t", env)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"t", "t/sub", "t/sub/deep"}
	if !slices.Equal(dirs, want) {
		t.Fatalf("TreeMembers = %v, want %v (no main, testdata, _ or dot directories)", dirs, want)
	}
	files, err := stub.Tree(t.Context(), repo, "t", dirs, env)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
		if !strings.Contains(string(f.Data), `panic("`+stub.Panic+`")`) {
			t.Errorf("%s is not stubbed:\n%s", f.Name, f.Data)
		}
	}
	if w := []string{"sub/deep/deep.go", "sub/sub.go", "t.go"}; !slices.Equal(names, w) {
		t.Fatalf("stubbed files = %v, want %v", names, w)
	}
	if err := stub.Write(filepath.Join(repo, "t"), files); err != nil {
		t.Fatal(err)
	}
	for _, untouched := range []string{"t/cmd/main.go", "t/testdata/x/x.go", "use/use.go"} {
		if data, _ := os.ReadFile(filepath.Join(repo, untouched)); string(data) != treeFiles()[untouched] {
			t.Errorf("%s changed", untouched)
		}
	}
	if out, err := RunGo(t.Context(), repo, env, "build", "./..."); err != nil {
		t.Fatalf("stubbed tree does not build: %v\n%s", err, out)
	}
	if out, err := RunGo(t.Context(), repo, env, "test", "-count=1", "-run", "^$", "./t/..."); err != nil {
		t.Fatalf("stubbed tree's tests do not start: %v\n%s", err, out)
	}
	out, err := RunGo(t.Context(), repo, env, "test", "-count=1", "./t/...")
	if err == nil || testBuildBroken(out) || !strings.Contains(out, stub.Panic) ||
		!strings.Contains(out, "FAIL\texample.com/tree/t\t") || !strings.Contains(out, "FAIL\texample.com/tree/t/sub\t") {
		t.Fatalf("the tree's tests should fail on the panic: %v\n%s", err, out)
	}
	gitIn(t, repo, "checkout", "--quiet", "--", ".")

	k := NewChecker(t.TempDir(), env, 5)
	pkgs, err := ListPackages(t.Context(), repo, "./t/cmd", env)
	if err != nil || len(pkgs) != 1 || pkgs[0].Name != "main" || pkgs[0].Dir != "t/cmd" || pkgs[0].Err != "" {
		t.Fatalf("ListPackages(./t/cmd) = %+v, %v", pkgs, err)
	}
	mains, err := k.Mains(t.Context(), "example.com/tree", "file://"+repo, commit)
	if err != nil || len(mains) != 1 || !mains["example.com/tree/t/cmd"] {
		t.Fatalf("Mains() = %v, %v", mains, err)
	}
	exp, err := k.CheckTree(t.Context(), treeCandidate(repo, commit, want, true))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(exp.Oracle.Test, []string{"./t/..."}) || len(exp.Members) != 3 || exp.StubSHA256 != stub.TreeHash(files) {
		t.Fatalf("CheckTree experiment = %+v", exp)
	}

	// Untested, the tree is checked against its importers outside it.
	exp, err = k.CheckTree(t.Context(), treeCandidate(repo, commit, want, false))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(exp.Oracle.Test, []string{"./use"}) {
		t.Fatalf("untested tree oracle = %v, want [./use]", exp.Oracle.Test)
	}

	// A tree whose members at the pin are not the data's is rejected.
	if _, err := k.CheckTree(t.Context(), treeCandidate(repo, commit, want[:2], true)); err == nil ||
		!strings.Contains(err.Error(), "not the data's") {
		t.Fatalf("CheckTree with missing members = %v", err)
	}
}
