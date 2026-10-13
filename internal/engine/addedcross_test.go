package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// scoreSource is package pkg with one unexported function of about 100
// normalized tokens, named name.
func scoreSource(pkg, name string) string {
	return "package " + pkg + `

func ` + name + `(xs []int, limit int) int {
	total := 0
	for i, x := range xs {
		if i > limit {
			break
		}
		if x < 0 {
			total -= x * 2
			continue
		}
		total += x * 3
	}
	if total > 1000 {
		total = 1000 - total%7
	}
	if total == 0 {
		return -1
	}
	switch {
	case total < 10:
		return total + 1
	case total < 100:
		return total * 2
	case total < 500:
		return total / 2
	default:
		return total
	}
}
`
}

// preambleSource is package pkg with one unexported function whose first
// 45 or so normalized tokens, two argument checks, every preambleSource
// shares, followed by body.
func preambleSource(pkg, body string) string {
	return "package " + pkg + `

import "errors"

func open(name string, flags int) (string, error) {
	if name == "" {
		return "", errors.New("empty name")
	}
	if flags < 0 {
		return "", errors.New("negative flags")
	}
` + body + "}\n"
}

// moduleRow returns c's module row, failing t when it has none.
func moduleRow(t *testing.T, c *report.Check) *report.Report {
	t.Helper()
	if c.Module == nil {
		t.Fatal("check has no module row")
	}
	return &c.Module.Report
}

// TestCheckAddedCrossBlockFloor checks dup_blocks_cross_pkg against a git
// baseline under the default configuration: a change that copies a
// function of about 100 tokens into another package fails and the finding
// names the copy, while one that repeats a 45-token argument-checking
// preamble in another package passes, the row still counting the block.
func TestCheckAddedCrossBlockFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	tests := []struct {
		name     string
		base     map[string]string
		head     map[string]string
		wantFail bool
	}{
		{
			name:     "100-token function copied",
			base:     map[string]string{"p/p.go": scoreSource("p", "score"), "q/q.go": "package q\n\nfunc one() int { return 1 }\n"},
			head:     map[string]string{"q/copy.go": scoreSource("q", "rate")},
			wantFail: true,
		},
		{
			name: "45-token preamble repeated",
			base: map[string]string{
				"p/p.go": preambleSource("p", "\treturn name, nil\n"),
				"q/q.go": "package q\n\nfunc one() int { return 1 }\n",
			},
			head: map[string]string{"q/open.go": preambleSource("q", "\tn := len(name) + flags\n\treturn name[:n], nil\n")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"go.mod": "module example.com/m\n\ngo 1.22\n"}
			for k, v := range tt.base {
				files[k] = v
			}
			dir := gitRepoAt(t, files)
			writeFiles(t, dir, tt.head)
			gitIn(t, dir, "add", "-A")
			gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "change")

			c, logs := checkAgainstBase(t, dir, false)
			m := moduleRow(t, c)
			if n := m.Metrics.DupBlocksCrossPkg; n == nil || *n != 1 {
				t.Fatalf("module dup_blocks_cross_pkg = %v, want the full count 1; logs:\n%s", n, logs)
			}
			if !tt.wantFail {
				if len(m.Violations) != 0 {
					t.Errorf("module violations = %+v, want none under the 80-token floor", m.Violations)
				}
				return
			}
			if len(m.Violations) != 1 || m.Violations[0].Metric != "dup_blocks_cross_pkg" {
				t.Fatalf("module violations = %+v, want one on dup_blocks_cross_pkg", m.Violations)
			}
			v := m.Violations[0]
			if !strings.HasPrefix(v.Suggestion, "The change added 1 duplicate block of at least 80 tokens shared") ||
				!strings.Contains(v.Suggestion, "p/p.go:1-31 and q/copy.go:1-31") {
				t.Errorf("suggestion = %q, want it to name the added copy in p/p.go and q/copy.go", v.Suggestion)
			}
			if v.Location == nil || v.Location.File != "p/p.go" || v.Location.Line != 1 {
				t.Errorf("located at %+v, want p/p.go:1", v.Location)
			}
		})
	}
}

// TestCheckSplitIsNotAddedCrossBlock checks a change that splits a package
// by moving a file (a git rename) into a new package: the duplicate the two
// files shared within the package becomes a cross-package block between
// the halves, which the module row counts but the rule does not judge as
// added, since every package it touches came from the split.
func TestCheckSplitIsNotAddedCrossBlock(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := gitRepoAt(t, map[string]string{
		"go.mod":    "module example.com/m\n\ngo 1.22\n",
		"p/p.go":    scoreSource("p", "score"),
		"p/rate.go": scoreSource("p", "rate"),
	})
	if err := os.MkdirAll(filepath.Join(dir, "r"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "mv", "p/rate.go", "r/rate.go")
	writeFiles(t, dir, map[string]string{"r/rate.go": scoreSource("r", "rate")})
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--no-verify", "-m", "split")

	c, logs := checkAgainstBase(t, dir, false)
	if r := findRow(c, "r"); r == nil || len(r.BaselineFrom) != 1 || r.BaselineFrom[0] != "p" {
		t.Fatalf("r = %+v, want it seeded from p by the rename; logs:\n%s", r, logs)
	}
	m := moduleRow(t, c)
	if n := m.Metrics.DupBlocksCrossPkg; n == nil || *n != 1 {
		t.Fatalf("module dup_blocks_cross_pkg = %v, want the halves' block counted", n)
	}
	if len(m.Violations) != 0 || c.Failed() {
		t.Errorf("module violations = %+v, check failed %v; want the split to pass", m.Violations, c.Failed())
	}
}

// TestCheckDegradedFixtureCrossBlockFires checks that the degraded
// fixture's cross-package block, a.Checksum copied into b as Digest, clears
// the default duplication.min_tokens_cross floor: against a git baseline
// without Digest, the module row fails and names both copies.
func TestCheckDegradedFixtureCrossBlockFires(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range []string{"fixture-degraded", "extmod"} {
		if err := os.CopyFS(filepath.Join(repo, mod), os.DirFS(filepath.Join("..", "..", "testdata", "go", mod))); err != nil {
			t.Fatalf("copying %s: %v", mod, err)
		}
	}
	bPath := filepath.Join(repo, "fixture-degraded", "b", "b.go")
	full, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	before, _, ok := strings.Cut(string(full), "// Digest is a copy")
	if !ok {
		t.Fatal("b.go has no Digest to remove")
	}
	if err := os.WriteFile(bPath, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-q", "-b", "master")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "--no-verify", "-m", "base")
	gitIn(t, repo, "tag", "base")
	if err := os.WriteFile(bPath, full, 0o600); err != nil {
		t.Fatal(err)
	}

	c, logs := checkAgainstBase(t, filepath.Join(repo, "fixture-degraded"), false)
	m := moduleRow(t, c)
	if len(m.Violations) != 1 || m.Violations[0].Metric != metricCross {
		t.Fatalf("module violations = %+v, want one on %s; logs:\n%s", m.Violations, metricCross, logs)
	}
	const where = "a/a.go:7-23 and b/b.go:13-29"
	if s := m.Violations[0].Suggestion; !strings.Contains(s, where) {
		t.Errorf("suggestion = %q, want it to name %s", s, where)
	}
	if m.PackagePath != metrics.ModuleRowID {
		t.Errorf("module row path = %q, want %q", m.PackagePath, metrics.ModuleRowID)
	}
}

// metricCross is the module-wide metric these tests judge.
const metricCross = "dup_blocks_cross_pkg"
