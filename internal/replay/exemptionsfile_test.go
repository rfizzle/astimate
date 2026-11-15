package replay

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/gate/exempt"
)

// TestReplayExemptionsFile checks that a replay under the embedded default,
// as dryrun runs it, reads the exemptions file of the working tree's module
// once, and never a replayed commit's: here the committed file is invalid,
// and every commit still replays, judged under the working tree's file.
func TestReplayExemptionsFile(t *testing.T) {
	goEnv(t)
	commits := threeCommits()[:2]
	commits[1].files[config.ExemptionsFileName] = "thresholds: []\n"
	repo, _ := fixtureRepo(t, commits)
	if err := os.WriteFile(filepath.Join(repo, config.ExemptionsFileName),
		[]byte("exemptions:\n  - package: a\n    metric: dup_blocks\n    reason: kept on purpose\n"+
			"  - package: a\n    metric: duplication_pct\n    reason: kept on purpose\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.FileName); err == nil {
		t.Fatalf("%s exists in the test's working directory", config.FileName)
	}
	cfg, source, err := config.Resolve("")
	if err != nil || source != config.SourceEmbedded {
		t.Fatalf("config.Resolve(\"\") = %q, %v; want the embedded default", source, err)
	}
	out := filepath.Join(t.TempDir(), "out")
	rp, err := New(t.Context(), Options{Repo: repo, Out: out, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	want := []exempt.Entry{
		{Package: "a", Metric: "dup_blocks", Reason: "kept on purpose", Source: config.ExemptionsFileName},
		{Package: "a", Metric: "duplication_pct", Reason: "kept on purpose", Source: config.ExemptionsFileName},
	}
	if !slices.Equal(rp.cfg.Exemptions, want) {
		t.Errorf("replay exemptions = %+v, want the working tree's %+v", rp.cfg.Exemptions, want)
	}
	if err := rp.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := os.ReadFile(filepath.Join(out, "commits.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// The second commit adds a duplicate block, which the working tree's
	// file exempts; its own file, which does not parse, is never read.
	if n := strings.Count(string(rows), `"loaded":true`); n != len(commits) || strings.Contains(string(rows), `"passed":false`) {
		t.Errorf("commits.jsonl has %d loaded commits, want %d, all passing:\n%s", n, len(commits), rows)
	}
}
