package pin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/calibration/rebuild/internal/selection"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/score"
)

// gitIn runs git in dir and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestTailDropsTimings(t *testing.T) {
	out := "--- FAIL: TestX\nFAIL\nFAIL\texample.com/m/p\t0.403s\nok  \texample.com/m/q\t(cached)\n"
	want := "FAIL / FAIL example.com/m/p / ok example.com/m/q"
	if got := Tail(out); got != want {
		t.Fatalf("Tail() = %q, want %q", got, want)
	}
}

func TestDefaultEnv(t *testing.T) {
	want := []string{"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off"}
	if got := DefaultEnv(); !slices.Equal(got, want) {
		t.Fatalf("DefaultEnv() = %v, want %v", got, want)
	}
}

func TestRunGo(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not found")
	}
	out, err := RunGo(context.Background(), t.TempDir(), DefaultEnv(), "version")
	if err != nil {
		t.Fatalf("RunGo: %v\n%s", err, out)
	}
	if !strings.Contains(out, "go version") {
		t.Fatalf("RunGo output = %q", out)
	}
}

func TestGoVersion(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not found")
	}
	v, err := GoVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "go") {
		t.Fatalf("GoVersion() = %q, want a go1.x version", v)
	}
}

// TestCloneAt clones a local one-commit repository and checks that the
// clone holds its tree, and that resetClone restores an edited file.
func TestCloneAt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "one")
	commit := gitIn(t, repo, "rev-parse", "HEAD")

	dst := filepath.Join(t.TempDir(), "clone")
	if err := CloneAt(context.Background(), "file://"+repo, commit, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "f.txt"))
	if err != nil || string(data) != "v1" {
		t.Fatalf("clone content = %q, %v, want v1", data, err)
	}

	if err := os.WriteFile(filepath.Join(dst, "f.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := resetClone(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "f.txt")); string(data) != "v1" {
		t.Fatalf("resetClone left %q, want v1", data)
	}
}

// fakeModule creates a one-package tested module in a git repository and
// returns a candidate for its only package, pinned at its only commit.
func fakeModule(t *testing.T) (repo string, cand selection.Candidate) {
	t.Helper()
	repo = filepath.Join(t.TempDir(), "repo")
	files := map[string]string{
		"go.mod":          "module example.com/fake\n\ngo 1.27\n",
		"lib/lib.go":      "// Package lib adds.\npackage lib\n\n// Add returns a+b.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"lib/lib_test.go": "package lib\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
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
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "fake")
	commit := gitIn(t, repo, "rev-parse", "HEAD")
	cand = selection.Candidate{
		Row: selection.Row{
			Module: "example.com/fake", Commit: commit, Package: "example.com/fake/lib",
			Metrics: metrics.RawMetrics{HasTests: true}, AgentPasses: 0.1, HumanDays: 0.1,
		},
		Repo:     "file://" + repo,
		Estimate: score.Rebuild{RebuildTokens: 100},
		Tier:     score.TierOnePass,
	}
	return repo, cand
}

// TestCheckSucceeds runs Checker.Check on a small tested module and checks
// that it verifies the package and leaves the clone at the pin.
func TestCheckSucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("clones a repository and runs go test")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not found")
	}
	_, cand := fakeModule(t)
	k := NewChecker(t.TempDir(), DefaultEnv(), 5)
	exp, err := k.Check(context.Background(), cand)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Package != "example.com/fake/lib" || exp.Dir != "lib" {
		t.Fatalf("Check() experiment = %+v", exp)
	}
	if len(exp.StubSHA256) != 64 {
		t.Fatalf("StubSHA256 = %q, want 64 hex characters", exp.StubSHA256)
	}
	if !slices.Equal(exp.Oracle.Test, []string{"./lib"}) {
		t.Fatalf("Oracle.Test = %v, want [./lib]", exp.Oracle.Test)
	}
	clone, err := k.clone(context.Background(), cand)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(clone, "lib", "lib.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "return a + b") {
		t.Fatalf("clone was not restored to the pin:\n%s", data)
	}
}
