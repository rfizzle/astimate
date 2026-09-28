// Command collect gathers the reference corpus data SPEC.md 11.1 calibrates
// thresholds from. It clones each module of calibration/corpus.yaml at its
// pinned commit, ranks every package the way `astimate rank --json` does,
// measures the standard library in-process, and pools the rows into
// packages.jsonl with a run.json describing the environment. Each row also
// counts its package's functions by cognitive complexity, the per-function
// distribution changed_func_cognitive_max is fitted from. Each cloned
// module's module-level row (SPEC.md 8.1), with dup_blocks_cross_pkg and
// what the module pass cost, goes to modules.jsonl.
//
// A corpus with language: typescript (calibration/corpus-typescript.yaml)
// is collected with the TypeScript extractor instead: each entry names the
// module roots of its repository, every root is ranked on its own, rows of
// test, fixture, example and documentation directories are left out, and
// every row carries language: typescript. The TypeScript extractor
// measures no module row, so such a run writes no modules.jsonl.
//
// Usage, from the repository root:
//
//	go run ./calibration/collect --pin             # fill empty commits (network)
//	go run ./calibration/collect [--out dir]       # collect every module (network)
//	go run ./calibration/collect --stdlib          # standard library only (no network)
//	go run ./calibration/collect --only <module>   # one corpus module
//	go run ./calibration/collect --modules-only    # module rows only (network)
//	go run ./calibration/collect --corpus calibration/corpus-typescript.yaml [--pin] [--out dir]
//
// See calibration/corpus.md for the selection criteria, and
// calibration/notes/typescript-corpus-2026-09-28.md for the TypeScript
// corpus's.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/rfizzle/astimate/calibration/collect/internal/modpass"
	"github.com/rfizzle/astimate/calibration/internal/gocache"
	"github.com/rfizzle/astimate/internal/config"
	"github.com/rfizzle/astimate/internal/engine"
)

// Exit codes: 0 when every selected module and package was collected, 1
// when the run finished with failures (the output is still written), 2 for
// a usage or setup error that stopped the run before collecting.
const (
	exitOK      = 0
	exitPartial = 1
	exitUsage   = 2
)

// defaultCorpus is the corpus file, relative to the repository root.
const defaultCorpus = "calibration/corpus.yaml"

// options are the parsed command-line flags.
type options struct {
	corpus string
	out    string
	pin    bool
	only   string
	stdlib bool
	// modulesOnly skips the package rows and writes only modules.jsonl
	// and run.json.
	modulesOnly bool
}

// RunInfo is run.json: the environment and tool version a collection ran
// under, and what it collected per module.
type RunInfo struct {
	// Date is the collection date, YYYY-MM-DD.
	Date string `json:"date"`
	// GoVersion is the Go toolchain the collector ran with; the standard
	// library measured is this version's GOROOT.
	GoVersion string `json:"go_version"`
	// GOOS and GOARCH are the host platform.
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	// CPUs is the host's logical CPU count.
	CPUs int `json:"cpus"`
	// AstimateVersion is the collector's main-module version from its build
	// information; "(devel)" under go run.
	AstimateVersion string `json:"astimate_version"`
	// AstimateCommit is the astimate commit the collector was built from.
	AstimateCommit string `json:"astimate_commit"`
	// ConfigVersion is the config_version of the embedded default
	// configuration the rows were estimated under.
	ConfigVersion string `json:"config_version"`
	// Tokenizer is the token counting method.
	Tokenizer string `json:"tokenizer"`
	// Corpus is the corpus file the run read.
	Corpus string `json:"corpus"`
	// Language is the corpus's language when it is not Go: typescript.
	Language string `json:"language,omitempty"`
	// Packages is the number of rows in packages.jsonl; 0 for a run of
	// module rows only.
	Packages int `json:"packages"`
	// Modules summarizes each selected module.
	Modules []ModuleRun `json:"modules"`
	// ModuleRows is the number of rows in modules.jsonl.
	ModuleRows int `json:"module_rows"`
}

// ModuleRun is one module's outcome in run.json.
type ModuleRun struct {
	// Module is the corpus module path.
	Module string `json:"module"`
	// GoModPath is the module path in the collected go.mod, when it
	// differs from Module.
	GoModPath string `json:"go_mod_path,omitempty"`
	// Commit is the pinned commit, or the Go version for the standard
	// library.
	Commit string `json:"commit"`
	// Packages is the number of rows collected.
	Packages int `json:"packages"`
	// Failed lists the packages that failed to extract.
	Failed []packageFailure `json:"failed_packages,omitempty"`
	// Error is why the module could not be collected at all.
	Error string `json:"error,omitempty"`
	// Note says how the module's rows were measured, where that differs
	// from a module rank.
	Note string `json:"note,omitempty"`
	// CommitDate is the pinned commit's committer date, YYYY-MM-DD, the
	// evidence for the activity criterion; recorded for a TypeScript
	// corpus only.
	CommitDate string `json:"commit_date,omitempty"`
	// Roots are the TypeScript module roots collected from the
	// repository, relative to its root.
	Roots []string `json:"roots,omitempty"`
	// Excluded counts the TypeScript packages left out as tests,
	// fixtures, examples, benchmarks or documentation.
	Excluded int `json:"excluded_packages,omitempty"`
}

// stdlibNote is the run.json note on the standard library's rows.
const stdlibNote = "measured as one load of the pattern std, so fan_in and fan_in_tests count " +
	"standard-library importers and every standard-library import is internal_imports, not stdlib_imports"

// stdlibModulesNote is the run.json note on the standard library in a run
// of module rows only.
const stdlibModulesNote = "no module row: dup_blocks_cross_pkg is null for standard-library loads, " +
	"which are not modules (see calibration/notes/cross-package-duplication-2026-09-28.md)"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run parses args, then pins the corpus or collects it, and returns the
// exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return exitUsage
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if opts.pin {
		if err := pin(ctx, opts.corpus, lsRemoteHead, stdout); err != nil {
			logger.Error("pin failed", "err", err)
			return exitUsage
		}
		return exitOK
	}
	return collect(ctx, opts, logger)
}

// parseFlags parses the command line; the flag package reports errors on
// stderr.
func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.StringVar(&o.corpus, "corpus", defaultCorpus, "corpus file")
	fs.StringVar(&o.out, "out", filepath.Join("calibration", "data", time.Now().Format(time.DateOnly)),
		"output directory for packages.jsonl and run.json")
	fs.BoolVar(&o.pin, "pin", false, "fill empty commits in the corpus with each repo's HEAD via git ls-remote (network), then exit")
	fs.BoolVar(&o.modulesOnly, "modules-only", false, "collect only each cloned module's module row into modules.jsonl, no package rows")
	fs.StringVar(&o.only, "only", "", "collect only this corpus module")
	fs.BoolVar(&o.stdlib, "stdlib", false, "collect only the standard library, in-process (no network)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	switch {
	case fs.NArg() > 0:
		err := fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
		_, _ = fmt.Fprintln(stderr, "collect:", err)
		return o, err
	case o.stdlib && o.only != "":
		err := errors.New("--stdlib and --only are mutually exclusive")
		_, _ = fmt.Fprintln(stderr, "collect:", err)
		return o, err
	}
	if o.stdlib {
		o.only = stdlibModule
	}
	return o, nil
}

// pin fills the corpus file's empty commits in place.
func pin(ctx context.Context, path string, resolve resolveFunc, stdout io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading corpus: %w", err)
	}
	out, n, err := PinCorpus(ctx, data, resolve)
	if err != nil {
		return fmt.Errorf("pinning %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	_, err = fmt.Fprintf(stdout, "pinned %d modules in %s\n", n, path)
	return err
}

// selectEntries returns the corpus entries to collect: every entry, or the
// one named by only. Every cloned entry selected must be pinned, so a run
// is reproducible from the file alone.
func selectEntries(c *Corpus, only string) ([]Entry, error) {
	entries := c.Modules
	if only != "" {
		entries = nil
		for _, e := range c.Modules {
			if e.Module == only {
				entries = append(entries, e)
			}
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("module %s is not in the corpus", only)
		}
	}
	for _, e := range entries {
		if !e.Local && e.Commit == "" {
			return nil, fmt.Errorf("module %s has no pinned commit: run with --pin first", e.Module)
		}
	}
	return entries, nil
}

// collect runs the selected entries and writes packages.jsonl and run.json.
func collect(ctx context.Context, opts options, logger *slog.Logger) int {
	c, err := LoadCorpus(opts.corpus)
	if err != nil {
		logger.Error("collect failed", "err", err)
		return exitUsage
	}
	if c.IsTypeScript() && opts.modulesOnly {
		logger.Error("collect failed", "err", errors.New("--modules-only: the TypeScript extractor measures no module row"))
		return exitUsage
	}
	entries, err := selectEntries(c, opts.only)
	if err != nil {
		logger.Error("collect failed", "err", err)
		return exitUsage
	}
	cfg, err := config.Parse(config.Default())
	if err != nil {
		logger.Error("collect failed", "err", err)
		return exitUsage
	}
	if err := os.MkdirAll(opts.out, 0o755); err != nil {
		logger.Error("collect failed", "err", err)
		return exitUsage
	}
	// A cloned repository's go.work must not pull in modules outside the
	// pinned one.
	if err := os.Setenv("GOWORK", "off"); err != nil {
		logger.Error("collect failed", "err", err)
		return exitUsage
	}

	info := newRunInfo(ctx, opts.corpus, cfg)
	if c.IsTypeScript() {
		info.Language = c.Language
	}
	var all []Row
	var modRows []modpass.Row
	code := exitOK
	for _, e := range entries {
		var (
			rows   []Row
			modRow *modpass.Row
			mr     ModuleRun
		)
		if c.IsTypeScript() {
			rows, mr = collectTSEntry(ctx, e, cfg, logger)
		} else {
			rows, modRow, mr = collectEntry(ctx, e, cfg, logger, opts.modulesOnly)
		}
		all = append(all, rows...)
		if modRow != nil {
			modRows = append(modRows, *modRow)
		}
		info.Modules = append(info.Modules, mr)
		if mr.Error != "" || len(mr.Failed) > 0 {
			code = exitPartial
		}
	}
	info.Packages, info.ModuleRows = len(all), len(modRows)
	if !opts.modulesOnly {
		if err := writeRows(filepath.Join(opts.out, "packages.jsonl"), all); err != nil {
			logger.Error("collect failed", "err", err)
			return exitPartial
		}
	}
	if opts.modulesOnly || len(modRows) > 0 {
		if err := writeRows(filepath.Join(opts.out, "modules.jsonl"), modRows); err != nil {
			logger.Error("collect failed", "err", err)
			return exitPartial
		}
	}
	if err := writeJSON(filepath.Join(opts.out, "run.json"), info); err != nil {
		logger.Error("collect failed", "err", err)
		return exitPartial
	}
	logger.Info("collected", "packages", len(all), "module_rows", len(modRows), "modules", len(entries), "out", opts.out)
	return code
}

// collectEntry collects one corpus entry: the standard library in-process,
// any other module from a temporary shallow clone at its pin, with its
// module row. With modulesOnly it collects the module row alone, and
// nothing from the standard library, which has none. Loading a module
// compiles its dependencies' export data, so each entry builds into a
// cache in its own temporary directory, beside its clone, removed with it
// rather than left in the shared Go build cache.
func collectEntry(ctx context.Context, e Entry, cfg *config.Config, logger *slog.Logger, modulesOnly bool,
) ([]Row, *modpass.Row, ModuleRun) {
	logger = logger.With("module", e.Module)
	logger.Info("collecting")
	if e.Local && modulesOnly {
		return nil, nil, ModuleRun{Module: e.Module, Commit: runtime.Version(), Note: stdlibModulesNote}
	}

	mr := ModuleRun{Module: e.Module, Commit: e.Commit}
	work, err := os.MkdirTemp("", "astimate-collect-*")
	if err != nil {
		mr.Error = err.Error()
		return nil, nil, mr
	}
	defer func() { _ = os.RemoveAll(work) }()
	restore, err := gocache.Setenv(work)
	if err != nil {
		mr.Error = err.Error()
		return nil, nil, mr
	}
	defer restore()
	if e.Local {
		rows, mr := collectLocal(ctx, e, cfg, logger)
		return rows, nil, mr
	}
	dir := filepath.Join(work, "module")
	if err := cloneAt(ctx, e.Repo, e.Commit, dir); err != nil {
		logger.Error("clone failed", "err", err)
		mr.Error = err.Error()
		return nil, nil, mr
	}
	res, err := collectModule(ctx, dir, e.Commit, cfg, logger, modulesOnly)
	if res.ModPath != "" && res.ModPath != e.Module {
		logger.Warn("go.mod module path differs from the corpus", "go_mod", res.ModPath)
		mr.GoModPath = res.ModPath
	}
	if err != nil {
		logger.Error("collect failed", "err", err)
		mr.Error = err.Error()
		return nil, nil, mr
	}
	mr.Packages, mr.Failed = len(res.Rows), res.Failed
	return res.Rows, res.ModuleRow, mr
}

// collectLocal collects the standard library in-process.
func collectLocal(ctx context.Context, e Entry, cfg *config.Config, logger *slog.Logger) ([]Row, ModuleRun) {
	mr := ModuleRun{Module: e.Module, Commit: runtime.Version(), Note: stdlibNote}
	pkgs, err := stdPackages(ctx)
	if err != nil {
		mr.Error = err.Error()
		return nil, mr
	}
	rows, failed, err := collectStdlib(ctx, pkgs, mr.Commit, cfg, logger)
	if err != nil {
		logger.Error("collect failed", "err", err)
		mr.Error = err.Error()
		return nil, mr
	}
	mr.Packages, mr.Failed = len(rows), failed
	return rows, mr
}

// newRunInfo records the run environment.
func newRunInfo(ctx context.Context, corpus string, cfg *config.Config) RunInfo {
	info := RunInfo{
		Date:          time.Now().Format(time.DateOnly),
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		CPUs:          runtime.NumCPU(),
		ConfigVersion: cfg.Version,
		Tokenizer:     engine.TokenizerEst,
		Corpus:        filepath.ToSlash(corpus),
		Modules:       []ModuleRun{},
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.AstimateVersion = bi.Main.Version
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				info.AstimateCommit = s.Value
			}
		}
	}
	// go run does not stamp VCS information; read the checkout instead.
	if info.AstimateCommit == "" {
		if out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output(); err == nil {
			info.AstimateCommit = strings.TrimSpace(string(out))
		}
	}
	return info
}
