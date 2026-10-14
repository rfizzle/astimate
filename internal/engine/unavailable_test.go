package engine

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/report"
)

// unavailableFixture is a module whose package blocks holds one duplicate
// block, and fresh, which repeats it.
const unavailableFixture = "../../testdata/go/unavailable"

// brokenFile does not type-check, so a baseline holding it skips the
// package it is in.
const brokenFile = "package blocks\n\nvar Broken int = \"not an int\"\n"

// unavailableRepo commits unavailableFixture on master with blocks/broken.go
// beside it, so blocks does not load there, then commits the fixture again
// without broken.go, so blocks loads with the same block. The head commit
// adds the fresh package only when withFresh is set; the baseline never
// has it. It returns the module root.
func unavailableRepo(t *testing.T, withFresh bool) string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(unavailableFixture, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(unavailableFixture, path)
		files[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	fresh := map[string]string{}
	for name, body := range files {
		if strings.HasPrefix(name, "fresh/") {
			fresh[name] = body
			delete(files, name)
		}
	}
	files["blocks/broken.go"] = brokenFile
	writeFiles(t, root, files)
	gitIn(t, root, "init", "-q", "-b", "master")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-q", "-m", "base")
	if err := os.Remove(filepath.Join(root, "blocks", "broken.go")); err != nil {
		t.Fatal(err)
	}
	if withFresh {
		writeFiles(t, root, fresh)
	}
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "head")
	return root
}

// unavailableRow returns the report row of dir in c.
func unavailableRow(t *testing.T, c *report.Check, dir string) *report.Report {
	t.Helper()
	for i := range c.Packages {
		if r := &c.Packages[i].Report; r.PackagePath == dir {
			return r
		}
	}
	t.Fatalf("check has no row for %s: %+v", dir, c.Packages)
	return nil
}

// TestCheckBaselineUnavailable commits a package that does not load at the
// baseline and loads at head with the same duplicate block: it is judged
// against the maxes only, so the block it had does not ratchet from zero,
// its report says why, and the text and JSON carry the notice. A package
// the baseline never had still ratchets dup_blocks from zero.
func TestCheckBaselineUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	tg, err := LoadTarget(unavailableRepo(t, true), TargetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{Base: "HEAD~1"})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check: %v, failed %v", err, failed)
	}

	blocks := unavailableRow(t, c, "blocks")
	if blocks.Baseline != nil || !strings.HasPrefix(blocks.BaselineUnavailable, "type error: ") ||
		strings.Contains(blocks.BaselineUnavailable, tg.Mod.Root) {
		t.Errorf("blocks baseline %+v, unavailable %q, want no baseline block and a type error", blocks.Baseline, blocks.BaselineUnavailable)
	}
	if blocks.Metrics.DupBlocks == 0 {
		t.Fatalf("blocks has no duplicate block: %+v", blocks.Metrics)
	}
	if blocks.Passed == nil || !*blocks.Passed || len(blocks.Violations) != 0 {
		t.Errorf("blocks passed %v, violations %+v, want a pass: it faces the maxes only", blocks.Passed, blocks.Violations)
	}

	fresh := unavailableRow(t, c, "fresh")
	if fresh.BaselineUnavailable != "" || fresh.Baseline != nil {
		t.Errorf("fresh baseline %+v, unavailable %q, want a new package", fresh.Baseline, fresh.BaselineUnavailable)
	}
	if !slices.ContainsFunc(fresh.Violations, func(f report.Finding) bool { return f.Metric == "dup_blocks" }) {
		t.Errorf("fresh violations %+v, want dup_blocks from zero", fresh.Violations)
	}

	var text bytes.Buffer
	if err := report.WriteCheckText(&text, c); err != nil {
		t.Fatal(err)
	}
	notice := "baseline unavailable for blocks: type error: "
	if got := strings.Count(text.String(), "baseline unavailable for "); got != 1 || !strings.Contains(text.String(), notice) ||
		!strings.Contains(text.String(), "; ratchets skipped\n") {
		t.Errorf("text report wants one %q... line ending in %q:\n%s", notice, "; ratchets skipped", text.String())
	}

	var js bytes.Buffer
	if err := report.WriteCheckJSON(&js, c); err != nil {
		t.Fatal(err)
	}
	var rows []report.Report
	if err := json.Unmarshal(js.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for i := range rows {
		states[rows[i].PackagePath] = rows[i].BaselineUnavailable
	}
	if !strings.HasPrefix(states["blocks"], "type error: ") || states["fresh"] != "" {
		t.Errorf("JSON baseline_unavailable by package = %q, want a type error for blocks alone", states)
	}
}

// TestCheckBaselineUnavailablePasses is the fixture without the new
// package: the gate passes, with the notice.
func TestCheckBaselineUnavailablePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	tg, err := LoadTarget(unavailableRepo(t, false), TargetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c, failed, err := Check(t.Context(), tg, CheckOptions{Base: "HEAD~1"})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check: %v, failed %v", err, failed)
	}
	if c.Failed() {
		var text bytes.Buffer
		_ = report.WriteCheckText(&text, c)
		t.Fatalf("the gate failed, want a pass:\n%s", text.String())
	}
	var text bytes.Buffer
	if err := report.WriteCheckText(&text, c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "baseline unavailable for blocks: type error: ") {
		t.Errorf("text report lacks the notice:\n%s", text.String())
	}
}
