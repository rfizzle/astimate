package engine

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/report"
	"github.com/rfizzle/astimate/internal/report/rows"
)

// excludeRepo commits, as the tag base, a module whose package internal/gen
// imports base, and returns the module root.
func excludeRepo(t *testing.T) string {
	t.Helper()
	return gitRepoAt(t, map[string]string{
		"go.mod":              "module example.com/m\n\ngo 1.22\n",
		"base/base.go":        "package base\n\n// One is one.\nfunc One() int { return 1 }\n",
		"internal/gen/gen.go": "package gen\n\nimport \"example.com/m/base\"\n\n// Two is two.\nfunc Two() int { return base.One() + 1 }\n",
		"p/p.go":              "package p\n\n// Three is three.\nfunc Three() int { return 3 }\n",
	})
}

// rewriteGen rewrites internal/gen in the working tree of dir with far
// more, and more complex, code that still imports base.
func rewriteGen(t *testing.T, dir string) {
	t.Helper()
	writeFiles(t, dir, map[string]string{
		"internal/gen/gen.go":  strings.Replace(hardSource(), "package p\n", "package gen\n\nimport \"example.com/m/base\"\n\nvar _ = base.One\n", 1),
		"internal/gen/more.go": strings.Replace(trivialSource(), "package p\n", "package gen\n", 1),
	})
}

// excludeTarget loads the module at dir under the pinned default
// configuration with patterns as its exclude list, none when empty.
func excludeTarget(t *testing.T, dir string, patterns ...string) (*Target, *bytes.Buffer) {
	t.Helper()
	data := pinnedDefault()
	if len(patterns) > 0 {
		data = append(data, "\nexclude:\n"...)
		for _, p := range patterns {
			data = append(data, "  - \""+p+"\"\n"...)
		}
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	tg, err := LoadTarget(dir, TargetOptions{Config: cfg, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return tg, &logs
}

// wantExcludedGen returns the not-analysed entry of internal/gen under the
// pattern internal/gen/**.
func wantExcludedGen() report.Skipped {
	return report.Skipped{Package: "internal/gen", Reason: "excluded by config (internal/gen/**)"}
}

// TestCheckExcludeAdded checks an exclude pattern added in a change, the
// package it covers rewritten in the same change: against the git base,
// which is extracted under the same list, and against a baseline file
// written before the list existed, the check reports no package deleted,
// judges no row on the excluded path, and lists the package as not
// analysed with the pattern. Without the list the same change fails it.
func TestCheckExcludeAdded(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: runs git and loads Go packages")
	}
	t.Parallel()

	dir := excludeRepo(t)
	// A baseline file of the base commit, written with no exclude list.
	before, _ := excludeTarget(t, dir)
	file := filepath.Join(t.TempDir(), "baseline.json")
	if _, _, err := WriteBaseline(t.Context(), before, file); err != nil {
		t.Fatal(err)
	}
	rewriteGen(t, dir)

	plain, _ := excludeTarget(t, dir)
	c, failed, err := Check(t.Context(), plain, CheckOptions{Base: "base"})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Check without exclude = %v, %v", failed, err)
	}
	if findRow(c, "internal/gen") == nil {
		t.Fatal("without exclude the change does not check internal/gen")
	}

	for _, tc := range []struct {
		name string
		opts CheckOptions
	}{
		{"git base", CheckOptions{Base: "base"}},
		{"git base, all", CheckOptions{Base: "base", All: true}},
		{"baseline file", CheckOptions{BaselineFile: file}},
		{"baseline file, all", CheckOptions{BaselineFile: file, All: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tg, logs := excludeTarget(t, dir, "internal/gen/**")
			c, failed, err := Check(t.Context(), tg, tc.opts)
			if err != nil || len(failed) != 0 {
				t.Fatalf("Check = %v, %v; logs:\n%s", failed, err, logs)
			}
			if len(c.Deleted) != 0 {
				t.Errorf("Deleted = %v, want none", c.Deleted)
			}
			for i := range c.Packages {
				if p := c.Packages[i].Report.PackagePath; p == "internal/gen" {
					t.Errorf("checked %s, an excluded package", p)
				}
			}
			if !slices.Contains(c.Skipped, wantExcludedGen()) {
				t.Errorf("Skipped = %+v, want %+v", c.Skipped, wantExcludedGen())
			}
			if r := findRow(c, "base"); tc.opts.All && tc.opts.Base != "" {
				if r == nil || r.Baseline == nil || r.Metrics.FanIn != 0 || r.Baseline.Metrics.FanIn != 0 {
					t.Errorf("base row %+v, want fan_in 0 at both ends: the base is extracted under the same list", r)
				}
			}
		})
	}
}

// TestRankAndReportExclude checks that a package the exclude list covers
// is in no rank row and no report view, and is listed as not analysed
// with the pattern.
func TestRankAndReportExclude(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: loads Go packages")
	}
	t.Parallel()

	dir := excludeRepo(t)
	rewriteGen(t, dir)
	tg, _ := excludeTarget(t, dir, "internal/gen/**")
	ranked, failed, err := Rank(t.Context(), tg, RankOptions{Sort: rows.SortPasses})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Rank = %v, %v", failed, err)
	}
	var paths []string
	for _, r := range ranked {
		paths = append(paths, r.Path)
	}
	slices.Sort(paths)
	if want := []string{"base", "p"}; !slices.Equal(paths, want) {
		t.Errorf("ranked %v, want %v", paths, want)
	}
	m, failed, err := Report(t.Context(), tg, ReportOptions{})
	if err != nil || len(failed) != 0 {
		t.Fatalf("Report = %v, %v", failed, err)
	}
	for _, p := range m.Packages {
		if p.Path == "internal/gen" {
			t.Error("the report has a view of internal/gen, an excluded package")
		}
	}
	if len(m.Skipped) != 1 || m.Skipped[0].Path != "internal/gen" || m.Skipped[0].Reason != wantExcludedGen().Reason {
		t.Errorf("report Skipped = %+v, want internal/gen %s", m.Skipped, wantExcludedGen().Reason)
	}
}
