package runner

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/agent"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/definition"
	"github.com/rfizzle/astimate/calibration/rebuild/internal/stub"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// fakeTree creates a module with a tree t (t and t/sub, both tested, and
// the main package t/cmd) in a git repository and returns the tree
// experiment, pinned at its only commit.
func fakeTree(t *testing.T) definition.Experiment {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	files := map[string]string{
		"go.mod":            "module example.com/fake\n\ngo 1.27\n",
		"t/t.go":            "// Package t adds one.\npackage t\n\nimport \"example.com/fake/t/sub\"\n\n// Inc returns n+1.\nfunc Inc(n int) int { return sub.Add(n, 1) }\n",
		"t/t_test.go":       "package t\n\nimport \"testing\"\n\nfunc TestInc(t *testing.T) {\n\tif Inc(1) != 2 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		"t/sub/sub.go":      "// Package sub adds.\npackage sub\n\n// Add returns a+b.\nfunc Add(a, b int) int { return a + b }\n",
		"t/sub/sub_test.go": "package sub\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		"t/cmd/main.go":     "package main\n\nimport \"example.com/fake/t\"\n\nfunc main() { println(t.Inc(1)) }\n",
	}
	for name, src := range files {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stubbed, err := stub.Tree(t.Context(), repo, "t", []string{"t", "t/sub"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "fake")
	m := metrics.RawMetrics{Files: 1, SLOC: 3, TokensEst: 20, TokensEstWithTests: 50, FuncCount: 1, TestFuncs: 1,
		HasTests: true, UsesCgo: new(false), GeneratedFiles: new(0)}
	members := []definition.Member{
		{Package: "example.com/fake/t", Dir: "t", HasTests: true, Tier: score.TierOnePass, AgentPasses: 0, RebuildTokens: 50,
			HumanDays: 0.1, Metrics: definition.Metrics{RawMetrics: m}},
		{Package: "example.com/fake/t/sub", Dir: "t/sub", HasTests: true, Tier: score.TierOnePass, AgentPasses: 0,
			RebuildTokens: 50, HumanDays: 0.1, Metrics: definition.Metrics{RawMetrics: m}},
	}
	return definition.Experiment{
		Module: "example.com/fake", Repo: "file://" + repo, Commit: gitIn(t, repo, "rev-parse", "HEAD"),
		Package: "example.com/fake/t", Dir: "t", Stub: definition.StubSignatures, StubSHA256: stub.TreeHash(stubbed),
		Oracle: definition.Oracle{Test: []string{"./t/..."}, Build: []string{"./..."}}, TurnCap: 5, HasTests: true,
		Tier: score.TierOnePass, AgentPasses: 0, RebuildTokens: 100, HumanDays: 0.2,
		Metrics: definition.Metrics{RawMetrics: definition.Aggregate(members)}, Members: members,
	}
}

// TestRunTree runs a tree experiment with the dry-run agent: the whole
// tree is stubbed and restored, the row records the unit and the members,
// and a change to the main package below the tree, which is not a
// member, breaks the rules while a change to a member does not.
func TestRunTree(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a local repository and runs go test")
	}
	exps := []definition.Experiment{fakeTree(t)}
	script, err := filepath.Abs(filepath.Join("..", "..", "dryrun-agent.sh"))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := "sh " + agent.ShellQuote(script) + " {dir} && echo '// y' >> t/sub/sub.go && echo '// z' >> t/cmd/main.go"
	rn := newTestRunner(t, t.TempDir(), tmpl)
	rn.Def.Unit = definition.UnitTree
	res, err := rn.Resume(t.Context(), exps, 1, 1)
	if err != nil || res.Written != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	row := res.Rows[0]
	if row.Unit != definition.UnitTree || len(row.Members) != 2 || row.Members[1].Dir != "t/sub" ||
		row.Members[1].Estimate.RebuildTokens != 50 {
		t.Fatalf("row unit %q members %+v", row.Unit, row.Members)
	}
	if !row.Oracle.Passed || !slices.Equal(row.Oracle.Test, []string{"./t/..."}) {
		t.Fatalf("oracle %+v", row.Oracle)
	}
	if row.Valid || !slices.Equal(row.Changes.OutsidePackage, []string{"t/cmd/main.go"}) || len(row.Changes.TestFiles) != 0 {
		t.Fatalf("changes %+v valid %v", row.Changes, row.Valid)
	}
}
