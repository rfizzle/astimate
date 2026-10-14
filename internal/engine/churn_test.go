package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/baseline"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report/rows"
)

// churnRepo is a module with two packages and three commits: the first
// adds both, the second, by another author, changes a, and the third
// changes b and adds a test file to a, which counts for nothing.
func churnRepo(t *testing.T) string {
	t.Helper()
	dir := gitRepoAt(t, map[string]string{
		"go.mod": "module example.com/ch\n\ngo 1.22\n",
		"a/a.go": "package a\n\n// F is exported.\nfunc F() int { return 1 }\n",
		"b/b.go": "package b\n\n// G is exported.\nfunc G() int { return 2 }\n",
	})
	writeFiles(t, dir, map[string]string{"a/a.go": "package a\n\n// F is exported.\nfunc F() int { return 3 }\n"})
	gitIn(t, dir, "commit", "-q", "-a", "--no-verify", "--author", "U <u@example.com>", "-m", "a")
	writeFiles(t, dir, map[string]string{
		"b/b.go":      "package b\n\n// G is exported.\nfunc G() int { return 4 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { _ = F() }\n",
	})
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "b")
	return dir
}

// churnTarget loads the module at dir under the default configuration
// with churn.commits set to window.
func churnTarget(t *testing.T, dir string, window int) *Target {
	t.Helper()
	data := strings.Replace(string(config.Default()), "  commits: 200\n", "  commits: "+strconv.Itoa(window)+"\n", 1)
	cfg, err := config.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	tg, err := LoadTarget(dir, TargetOptions{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

// churnOf returns the churn of the row of rows at path as "commits/authors",
// "null" when it was not measured.
func churnOf(ranked []rows.Row, path string) string {
	for _, r := range ranked {
		if r.Path == path {
			if r.ChurnCommits == nil {
				return "null"
			}
			return strconv.Itoa(*r.ChurnCommits)
		}
	}
	return "missing"
}

// TestChurn checks churn_commits and churn_authors through rank, assess,
// check and baseline write on a three-commit repository, at windows of
// 200 and 2 commits, with churn off, and outside git.
func TestChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := churnRepo(t)
	t.Run("rank", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			window       int
			wantA, wantB string
			wantFirst    string
		}{
			{window: 200, wantA: "2", wantB: "2", wantFirst: "a"},
			{window: 2, wantA: "1", wantB: "1", wantFirst: "a"},
			{window: 1, wantA: "0", wantB: "1", wantFirst: "b"},
			{window: 0, wantA: "null", wantB: "null", wantFirst: "a"},
		}
		for _, tt := range tests {
			ranked, failed, err := Rank(t.Context(), churnTarget(t, dir, tt.window), RankOptions{Sort: rows.SortChurn})
			if err != nil || len(failed) > 0 {
				t.Fatalf("window %d: Rank: %v, failed %v", tt.window, err, failed)
			}
			if a, b := churnOf(ranked, "a"), churnOf(ranked, "b"); a != tt.wantA || b != tt.wantB || ranked[0].Path != tt.wantFirst {
				t.Errorf("window %d: churn a %s, b %s, first %s; want %s, %s, %s",
					tt.window, a, b, ranked[0].Path, tt.wantA, tt.wantB, tt.wantFirst)
			}
		}
	})
	t.Run("assess", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			window               int
			wantCommits, wantAut int
		}{
			{window: 200, wantCommits: 2, wantAut: 2},
			{window: 2, wantCommits: 1, wantAut: 1},
		}
		for _, tt := range tests {
			tg := churnTarget(t, filepath.Join(dir, "a"), tt.window)
			r, err := Assess(t.Context(), tg, AssessOptions{})
			if err != nil {
				t.Fatal(err)
			}
			m := r.Metrics
			if m.ChurnCommits == nil || *m.ChurnCommits != tt.wantCommits || m.ChurnAuthors == nil || *m.ChurnAuthors != tt.wantAut {
				t.Errorf("window %d: churn %v by %v, want %d by %d", tt.window, m.ChurnCommits, m.ChurnAuthors, tt.wantCommits, tt.wantAut)
			}
		}
	})
	t.Run("check and baseline write", func(t *testing.T) {
		t.Parallel()

		tg := churnTarget(t, dir, 200)
		c, failed, err := Check(t.Context(), tg, CheckOptions{Base: "base", All: true})
		if err != nil || len(failed) > 0 {
			t.Fatalf("Check: %v, failed %v", err, failed)
		}
		for _, p := range c.Packages {
			if m := p.Report.Metrics; m.ChurnCommits == nil || *m.ChurnCommits != 2 {
				t.Errorf("check row %s: churn %v, want 2", p.Report.PackagePath, m.ChurnCommits)
			}
			if b := p.Report.Baseline; b != nil && b.Metrics.ChurnCommits != nil {
				t.Errorf("check row %s: git baseline churn %v, want null", p.Report.PackagePath, *b.Metrics.ChurnCommits)
			}
		}
		path, _, err := WriteBaseline(t.Context(), tg, filepath.Join(t.TempDir(), "b.json"))
		if err != nil {
			t.Fatal(err)
		}
		b, err := baseline.FromFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if r, ok := b.Row("b"); !ok || r.Metrics.ChurnAuthors == nil || *r.Metrics.ChurnAuthors != 1 {
			t.Errorf("baseline file row b = %+v (present %v), want churn_authors 1", r.Metrics, ok)
		}
	})
	t.Run("fill by identifier", func(t *testing.T) {
		t.Parallel()

		tg := churnTarget(t, dir, 200)
		var m metrics.RawMetrics
		MeasureChurn(t.Context(), tg)("example.com/ch/a", &m)
		if m.ChurnCommits == nil || *m.ChurnCommits != 2 || m.ChurnAuthors == nil || *m.ChurnAuthors != 2 {
			t.Errorf("MeasureChurn on a: churn %v by %v, want 2 by 2", m.ChurnCommits, m.ChurnAuthors)
		}
	})
	t.Run("outside git", func(t *testing.T) {
		t.Parallel()

		plain := t.TempDir()
		if err := os.CopyFS(plain, os.DirFS(dir)); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(plain, ".git")); err != nil {
			t.Fatal(err)
		}
		r, err := Assess(t.Context(), churnTarget(t, filepath.Join(plain, "a"), 200), AssessOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Metrics.ChurnCommits != nil || r.Metrics.ChurnAuthors != nil {
			t.Errorf("churn %v by %v outside git, want null", r.Metrics.ChurnCommits, r.Metrics.ChurnAuthors)
		}
	})
}
