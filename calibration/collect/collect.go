package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
	"github.com/rfizzle/astimate/internal/lang/golang"
	"github.com/rfizzle/astimate/internal/metrics"
	"github.com/rfizzle/astimate/internal/report"
)

// Row is one pooled package: a line of packages.jsonl.
type Row struct {
	// Module is the module path from the module's go.mod, or "std".
	Module string `json:"module"`
	// Commit is the pinned commit the module was collected at; for the
	// standard library it is the Go version whose GOROOT was measured.
	Commit string `json:"commit"`
	// Package is the package's import path.
	Package string `json:"package"`
	// Metrics are the package's raw metrics.
	Metrics metrics.RawMetrics `json:"metrics"`
	// AgentPasses is the rounded rebuild estimate, as rank reports it.
	AgentPasses float64 `json:"agent_passes"`
	// HumanDays is the human estimate, as rank reports it.
	HumanDays float64 `json:"human_days"`
}

// newRow estimates m under cfg and returns its pooled row, rounded the way
// report.NewRow rounds the rank output.
func newRow(module, commit, pkg string, m *metrics.RawMetrics, cfg *config.Config) Row {
	r := report.NewRow(pkg, m, cfg.Rebuild)
	return Row{
		Module:      module,
		Commit:      commit,
		Package:     pkg,
		Metrics:     *m,
		AgentPasses: r.AgentPasses,
		HumanDays:   r.HumanDays,
	}
}

// packageFailure is a package that failed to extract.
type packageFailure struct {
	// Package is the import path.
	Package string `json:"package"`
	// Err is the extraction error's text.
	Err string `json:"error"`
}

// collectModule ranks the module rooted at dir the way `astimate rank
// --json` does, with one module load shared by every package, and returns
// one row per package sorted by import path, tagged with commit. It also
// returns the module path from go.mod and the packages that failed to
// extract; an error means the module could not be loaded or listed at all.
func collectModule(ctx context.Context, dir, commit string, cfg *config.Config, logger *slog.Logger,
) (rows []Row, modPath string, failed []packageFailure, err error) {
	t, err := engine.LoadTarget(dir, engine.TargetOptions{Config: cfg, Logger: logger})
	if err != nil {
		return nil, "", nil, err
	}
	modPath = t.Mod.ModulePath
	pkgs, err := t.Ext.Packages(t.Mod.Root)
	if err != nil {
		return nil, modPath, nil, fmt.Errorf("listing packages of %s: %w", modPath, err)
	}
	rows = make([]Row, 0, len(pkgs))
	for _, pkg := range pkgs {
		m, err := t.Ext.Extract(ctx, t.Mod, pkg)
		if err != nil {
			logger.Error("extracting package failed", "package", pkg, "err", err)
			failed = append(failed, packageFailure{Package: pkg, Err: err.Error()})
			continue
		}
		rows = append(rows, newRow(modPath, commit, pkg, &m, cfg))
	}
	sortRows(rows)
	return rows, modPath, failed, nil
}

// stdPackages lists the standard library with `go list std`, leaving out
// the vendored golang.org/x trees under vendor/, which the selection
// criteria exclude.
func stdPackages(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "go", "list", "std").Output()
	if err != nil {
		return nil, fmt.Errorf("go list std: %w", err)
	}
	var pkgs []string
	for _, p := range strings.Fields(string(out)) {
		if !strings.HasPrefix(p, "vendor/") {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs, nil
}

// stdlibOptions carries the configuration's extraction settings to the Go
// extractor, as engine.LoadTarget does for a module.
func stdlibOptions(cfg *config.Config) []golang.Option {
	return []golang.Option{
		golang.WithCharsPerToken(cfg.CharsPerToken),
		golang.WithDupMinTokens(cfg.Duplication.MinTokens),
		golang.WithDupIgnoreLiteralOnly(cfg.Duplication.IgnoreLiteralOnly),
		golang.WithDupFoldSigns(cfg.Duplication.FoldSigns),
		golang.WithTokenizer(engine.TokenizerEst),
	}
}

// collectStdlib measures the whole standard library in one load with
// golang.ExtractStdlibAll, so fan_in and fan_in_tests count the
// standard-library packages importing each one, and returns the rows of
// the packages in pkgs sorted by import path, tagged with goVersion as the
// commit, and the packages of pkgs that failed or were not in the load. An
// error means the standard library could not be loaded at all.
func collectStdlib(ctx context.Context, pkgs []string, goVersion string, cfg *config.Config, logger *slog.Logger,
) (rows []Row, failed []packageFailure, err error) {
	all, errs, err := golang.ExtractStdlibAll(ctx, stdlibOptions(cfg)...)
	if err != nil {
		return nil, nil, err
	}
	rows = make([]Row, 0, len(pkgs))
	for _, pkg := range pkgs {
		m, ok := all[pkg]
		if !ok {
			err := errs[pkg]
			if err == nil {
				err = fmt.Errorf("extracting %s: %w", pkg, metrics.ErrUnknownPackage)
			}
			logger.Error("extracting package failed", "package", pkg, "err", err)
			failed = append(failed, packageFailure{Package: pkg, Err: err.Error()})
			continue
		}
		rows = append(rows, newRow(stdlibModule, goVersion, pkg, &m, cfg))
	}
	sortRows(rows)
	return rows, failed, nil
}

// sortRows orders rows by module, then import path.
func sortRows(rows []Row) {
	slices.SortFunc(rows, func(a, b Row) int {
		return cmp.Or(strings.Compare(a.Module, b.Module), strings.Compare(a.Package, b.Package))
	})
}

// cloneAt fetches repo at commit alone into the new directory dir, with no
// history, and checks it out detached. It needs network.
func cloneAt(ctx context.Context, repo, commit, dir string) error {
	steps := [][]string{
		{"init", "--quiet", dir},
		{"-C", dir, "fetch", "--quiet", "--depth", "1", repo, commit},
		{"-C", dir, "checkout", "--quiet", "--detach", "FETCH_HEAD"},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// writeRows writes rows to path as JSON lines, through a temporary file in
// the same directory so a failed run never leaves a truncated file.
func writeRows(path string, rows []Row) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".packages-*.jsonl")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	for i := range rows {
		if err := enc.Encode(&rows[i]); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	// CreateTemp opens the file 0600; the data is meant to be committed.
	if err := tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// writeJSON writes v to path as indented JSON followed by a newline.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
