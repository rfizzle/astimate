package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
)

// TestReplaySkippedSelected checks that a commit whose check selected a
// package that does not load is recorded as not loaded, with the skip's
// reason as its error and no package rows, as a module that does not load
// is, while a commit that only changes a loading package beside it is
// checked as usual.
func TestReplaySkippedSelected(t *testing.T) {
	goEnv(t)
	remote := "package remote\n\nimport \"example.com/absent/client\"\n\nfunc Call() int { return client.Call() }\n"
	repo, hashes := fixtureRepo(t, []fixtureCommit{
		{files: map[string]string{"go.mod": goMod, "a/a.go": pkgA}, message: "feat: add package a"},
		{files: map[string]string{"remote/remote.go": remote}, message: "feat: add a package importing an absent module"},
		{files: map[string]string{"a/a.go": pkgA + "\nfunc one() int { return 1 }\n"}, message: "feat: grow a"},
	})
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
	if len(commits) != 3 {
		t.Fatalf("%d commit rows, want 3", len(commits))
	}
	skipped := commits[1]
	if skipped.Loaded || skipped.Passed != nil || !strings.Contains(skipped.Error, "skipped, it does not load") ||
		!strings.Contains(skipped.Error, "remote") {
		t.Errorf("skipped commit: loaded %v, passed %v, error %q; want not loaded with the skip's reason",
			skipped.Loaded, skipped.Passed, skipped.Error)
	}
	for _, p := range pkgs {
		if p.Commit == hashes[1] {
			t.Errorf("skipped commit has a package row for %s", p.Package)
		}
	}
	if c := commits[2]; !c.Loaded || c.Error != "" {
		t.Errorf("commit beside the skipped package: loaded %v, error %q; want checked", c.Loaded, c.Error)
	}
}

// TestLocate checks that a directory is located as its repository's top
// level and its slash path below it, "." at the top level.
func TestLocate(t *testing.T) {
	repo, _ := fixtureRepo(t, threeCommits()[:1])
	top := strings.TrimSpace(gitT(t, repo, "rev-parse", "--show-toplevel"))
	for _, tt := range []struct{ dir, rel string }{{repo, "."}, {filepath.Join(repo, "a"), "a"}} {
		gotTop, rel, err := Locate(t.Context(), tt.dir)
		if err != nil || gotTop != top || rel != tt.rel {
			t.Errorf("Locate(%s) = %q, %q, %v; want %q, %q", tt.dir, gotTop, rel, err, top, tt.rel)
		}
	}
	if _, _, err := Locate(t.Context(), t.TempDir()); err == nil {
		t.Error("Locate outside a repository succeeded")
	}
}

// TestCommitsLast checks that Last keeps the newest commits of the range,
// oldest first, and that Commits counts them.
func TestCommitsLast(t *testing.T) {
	repo, hashes := fixtureRepo(t, threeCommits())
	cfg, err := config.Parse(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rp, err := New(t.Context(), Options{Repo: repo, Last: 2, Out: t.TempDir(), Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if rp.Commits() != 2 || rp.commits[0].hash != hashes[1] || rp.commits[1].hash != hashes[2] {
		t.Errorf("Commits() = %d, first %s; want the last two of %v", rp.Commits(), rp.commits[0].hash, hashes)
	}
}

// TestReplayRunsNoHooks checks that checking a commit out into the
// replay's worktree runs none of the repository's hooks.
func TestReplayRunsNoHooks(t *testing.T) {
	goEnv(t)
	repo, _ := fixtureRepo(t, threeCommits()[:1])
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, log := replayT(t, "--repo", repo, "--out", filepath.Join(t.TempDir(), "out")); code != exitOK {
		t.Fatalf("exit %d: %s", code, log)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the post-checkout hook ran on the replay's checkout")
	}
}
