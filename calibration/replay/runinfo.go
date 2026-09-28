package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/rfizzle/astimate/internal/engine"
)

// runInfo is run.json: what was replayed, with what, and totals over the
// whole output directory.
type runInfo struct {
	// Date is the day of the run that last wrote rows, YYYY-MM-DD.
	Date   string `json:"date"`
	Totals totals `json:"totals"`
	// Parallel is the last run's --parallel.
	Parallel int `json:"parallel"`
	// Source is what was replayed: repository (the name), remote (its
	// origin URL, when it has one), module_dir, range (as given, or the
	// default resolved), and first and last, the oldest and newest
	// commits selected.
	Source map[string]string `json:"source"`
	// Tool is what the rows were measured with: config (the file, or
	// "embedded"), config_version, astimate_commit, go (toolchain and
	// platform) and tokenizer.
	Tool map[string]string `json:"tool"`
}

// totals count the rows in the output directory, and what the last run
// that wrote rows did: Written commits recorded, Skipped already there.
type totals struct {
	Commits     int     `json:"commits"`
	Loaded      int     `json:"loaded"`
	NotLoaded   int     `json:"not_loaded"`
	Passed      int     `json:"passed"`
	Failed      int     `json:"failed"`
	PackageRows int     `json:"package_rows"`
	WallSeconds float64 `json:"wall_seconds"`
	Written     int     `json:"written"`
	Skipped     int     `json:"skipped"`
}

// newRunInfo describes the run rp is about to make.
func newRunInfo(ctx context.Context, rp *replayer) runInfo {
	src := map[string]string{"repository": repoName(ctx, rp.repo), "module_dir": filepath.ToSlash(rp.opts.dir), "range": rp.opts.revRange}
	if out, err := gitCmd(ctx, rp.repo, "remote", "get-url", "origin"); err == nil {
		src["remote"] = strings.TrimSpace(out)
	}
	if n := len(rp.commits); n > 0 {
		src["first"], src["last"] = rp.commits[0].hash, rp.commits[n-1].hash
	}
	tool := map[string]string{
		"config": "embedded", "config_version": rp.cfg.Version, "astimate_commit": astimateCommit(ctx),
		"go":        runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
		"tokenizer": engine.TokenizerEst,
	}
	if rp.opts.configPath != "" {
		tool["config"] = filepath.ToSlash(rp.opts.configPath)
	}
	return runInfo{Date: time.Now().Format(time.DateOnly), Parallel: rp.opts.parallel, Source: src, Tool: tool}
}

// astimateCommit returns the commit the running binary was built from, or,
// under go run, which stamps none, the commit checked out in the working
// directory.
func astimateCommit(ctx context.Context) string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		i := slices.IndexFunc(bi.Settings, func(s debug.BuildSetting) bool { return s.Key == "vcs.revision" })
		if i >= 0 {
			return bi.Settings[i].Value
		}
	}
	out, _ := gitCmd(ctx, ".", "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

// writeRunInfo counts s's rows into info's totals and writes info to dir's
// run.json.
func writeRunInfo(dir string, info *runInfo, s *store) error {
	t := &info.Totals
	t.Commits, t.PackageRows = len(s.rows), s.packageRows
	var wall int64
	for i := range s.rows {
		c := &s.rows[i]
		wall += c.WallMS
		if !c.Loaded {
			t.NotLoaded++
			continue
		}
		t.Loaded++
		if c.Passed != nil && *c.Passed {
			t.Passed++
		} else {
			t.Failed++
		}
	}
	t.WallSeconds = float64(wall) / 1000
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", runFile, err)
	}
	return os.WriteFile(filepath.Join(dir, runFile), append(data, '\n'), 0o644)
}
