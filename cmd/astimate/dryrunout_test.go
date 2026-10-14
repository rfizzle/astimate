package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/gocache"
	"github.com/rfizzle/astimate/internal/replay/summary"
)

// TestDryrunSuggestPasses checks that the commit the gate failed passes
// under the suggested configuration: a second dryrun under it fails none.
func TestDryrunSuggestPasses(t *testing.T) {
	repo := dryrunRepo(t)
	file := filepath.Join(t.TempDir(), "suggested.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dryrun", repo, "--last", "3", "--budget", "30", "--suggest", file}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"dryrun", repo, "--last", "3", "--config", file}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	if want := "would have failed 0 of 3 commits checked"; !strings.Contains(stdout.String(), want) {
		t.Errorf("under the suggested configuration the summary lacks %q:\n%s", want, stdout.String())
	}
}

// TestDryrunPackagesFailed checks that a commit the replay could not check
// because a package it changed does not load names that package in its
// row's packages_failed in --json.
func TestDryrunPackagesFailed(t *testing.T) {
	remote := "package remote\n\nimport \"example.com/absent/client\"\n\nfunc Call() int { return client.Call() }\n"
	repo := buildRepo(t, []repoCommit{
		{map[string]string{"go.mod": "module example.com/fix\n\ngo 1.22\n", "a/a.go": "package a\n\nfunc one() int { return 1 }\n"}, "feat: add package a"},
		{map[string]string{"remote/remote.go": remote}, "feat: add a package importing an absent module"},
	})
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dryrun", repo, "--last", "2", "--json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	var found bool
	for line := range strings.Lines(stdout.String()) {
		var row struct {
			Subject        string `json:"subject"`
			Loaded         bool   `json:"loaded"`
			PackagesFailed []struct {
				Package string `json:"package"`
				Error   string `json:"error"`
			} `json:"packages_failed"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(row.Subject, "absent module") {
			continue
		}
		found = true
		if row.Loaded || len(row.PackagesFailed) != 1 || row.PackagesFailed[0].Package != "remote" ||
			row.PackagesFailed[0].Error == "" {
			t.Errorf("commit row: loaded %v, packages_failed %+v; want not loaded naming package remote", row.Loaded, row.PackagesFailed)
		}
	}
	if !found {
		t.Errorf("no commit row for the unloadable commit:\n%s", stdout.String())
	}
}

// TestDryrunOut checks that --out keeps the replay's three files, and that
// the calibration commands read them with no manual step: the labeler
// writes split-extract labels from the directory and the gate validation
// scores them.
func TestDryrunOut(t *testing.T) {
	if testing.Short() {
		t.Skip("runs two calibration commands")
	}
	repo := dryrunRepo(t)
	out := filepath.Join(t.TempDir(), "replay")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dryrun", repo, "--last", "3", "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	for _, name := range []string{"commits.jsonl", "packages.jsonl", "run.json"} {
		if fi, err := os.Stat(filepath.Join(out, name)); err != nil || fi.Size() == 0 {
			t.Errorf("--out lacks %s: %v", name, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(out, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Source map[string]string `json:"source"`
		Tool   map[string]string `json:"tool"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	if info.Source["range"] != "HEAD" || info.Tool["config_version"] == "" {
		t.Errorf("run.json source %v, tool %v; want the range and the config version", info.Source, info.Tool)
	}
	if s, err := summary.Summarize(out); err != nil || s.Checked != 3 || s.Failed != 1 {
		t.Errorf("summary of --out: %+v, err %v; want 3 checked, 1 failed", s, err)
	}

	labels := filepath.Join(t.TempDir(), "labels.yaml")
	goRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("go", append([]string{"run"}, args...)...)
		cmd.Dir = filepath.Join("..", "..")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go run %s: %v\n%s", strings.Join(args, " "), err, b)
		}
	}
	goRun("./calibration/replay/label", "--rule", "split-extract", "--data", out, "--out", labels)
	goRun("./calibration/validate", "--corpus", "fix="+out+":"+labels, "--out", filepath.Join(t.TempDir(), "report.md"))
}

// TestDryrunOutUsage checks that --out refuses a directory that already
// holds files, which a replay would resume into.
func TestDryrunOutUsage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "old.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dryrun", "--out", dir}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "new or empty") {
		t.Errorf("exit %d, stderr %q; want a usage error naming a new or empty directory", code, stderr.String())
	}
}

// TestDryrunIsolatesGoCache checks that a dryrun leaves the user's Go build
// cache alone and the environment as it was, and removes its own.
func TestDryrunIsolatesGoCache(t *testing.T) {
	// t.TempDir honours TMPDIR and GOTMPDIR, so take the directories first.
	user, tmp := t.TempDir(), t.TempDir()
	repo := dryrunRepo(t)
	t.Setenv("GOCACHE", user)
	t.Setenv("GOTMPDIR", "")
	t.Setenv("TMPDIR", tmp)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dryrun", repo, "--last", "2"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr.String())
	}
	if entries, err := os.ReadDir(user); err != nil || len(entries) != 0 {
		t.Errorf("the user's GOCACHE holds %d entries after dryrun (err %v), want none", len(entries), err)
	}
	if got := os.Getenv("GOCACHE"); got != user {
		t.Errorf("GOCACHE = %q after dryrun, want %q", got, user)
	}
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Errorf("dryrun left %d entries in the temporary directory (err %v), want none", len(entries), err)
	}
}

// TestIsolateGoCache checks that the build cache is a directory of its own,
// which the go command is pointed at while it lives and which is removed
// with the previous environment restored.
func TestDryrunGoCacheRestored(t *testing.T) {
	t.Setenv("GOCACHE", "/user/cache")
	t.Setenv("GOTMPDIR", "/user/tmp")
	cleanup, err := gocache.Isolate("astimate-dryrun-gocache-")
	if err != nil {
		t.Fatal(err)
	}
	cache, tmp := os.Getenv("GOCACHE"), os.Getenv("GOTMPDIR")
	if cache == "/user/cache" || tmp == "/user/tmp" || filepath.Dir(cache) != filepath.Dir(tmp) {
		t.Errorf("GOCACHE %q and GOTMPDIR %q are not a cache of their own", cache, tmp)
	}
	for _, d := range []string{cache, tmp} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("%s not created: %v", d, err)
		}
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(cache)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the cache directory survived cleanup: %v", err)
	}
	if os.Getenv("GOCACHE") != "/user/cache" || os.Getenv("GOTMPDIR") != "/user/tmp" {
		t.Errorf("environment not restored: GOCACHE %q, GOTMPDIR %q", os.Getenv("GOCACHE"), os.Getenv("GOTMPDIR"))
	}
}
