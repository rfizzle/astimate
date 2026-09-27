// Command collect gathers the reference corpus data SPEC.md 11.1 calibrates
// thresholds from. It clones each module of calibration/corpus.yaml at its
// pinned commit, ranks every package the way `astimate rank --json` does,
// measures the standard library in-process, and pools the rows into
// packages.jsonl with a run.json describing the environment.
//
// Usage, from the repository root:
//
//	go run ./calibration/collect --pin             # fill empty commits (network)
//	go run ./calibration/collect [--out dir]       # collect every module (network)
//	go run ./calibration/collect --stdlib          # standard library only (no network)
//	go run ./calibration/collect --only <module>   # one corpus module
//
// See calibration/corpus.md for the selection criteria.
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
	jobs   int
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
	// Packages is the number of rows in packages.jsonl.
	Packages int `json:"packages"`
	// Modules summarizes each selected module.
	Modules []ModuleRun `json:"modules"`
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
}

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
	fs.StringVar(&o.only, "only", "", "collect only this corpus module")
	fs.BoolVar(&o.stdlib, "stdlib", false, "collect only the standard library, in-process (no network)")
	fs.IntVar(&o.jobs, "jobs", max(runtime.NumCPU()/2, 1), "standard-library packages extracted in parallel")
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
	var all []Row
	code := exitOK
	for _, e := range entries {
		rows, mr := collectEntry(ctx, e, opts.jobs, cfg, logger)
		all = append(all, rows...)
		info.Modules = append(info.Modules, mr)
		if mr.Error != "" || len(mr.Failed) > 0 {
			code = exitPartial
		}
	}
	info.Packages = len(all)
	if err := writeRows(filepath.Join(opts.out, "packages.jsonl"), all); err != nil {
		logger.Error("collect failed", "err", err)
		return exitPartial
	}
	if err := writeJSON(filepath.Join(opts.out, "run.json"), info); err != nil {
		logger.Error("collect failed", "err", err)
		return exitPartial
	}
	logger.Info("collected", "packages", len(all), "modules", len(entries), "out", opts.out)
	return code
}

// collectEntry collects one corpus entry: the standard library in-process,
// any other module from a temporary shallow clone at its pin.
func collectEntry(ctx context.Context, e Entry, jobs int, cfg *config.Config, logger *slog.Logger) ([]Row, ModuleRun) {
	logger = logger.With("module", e.Module)
	logger.Info("collecting")
	if e.Local {
		mr := ModuleRun{Module: e.Module, Commit: runtime.Version()}
		pkgs, err := stdPackages(ctx)
		if err != nil {
			mr.Error = err.Error()
			return nil, mr
		}
		rows, failed := collectStdlib(ctx, pkgs, mr.Commit, cfg, jobs, logger)
		mr.Packages, mr.Failed = len(rows), failed
		return rows, mr
	}

	mr := ModuleRun{Module: e.Module, Commit: e.Commit}
	dir, err := os.MkdirTemp("", "astimate-corpus-*")
	if err != nil {
		mr.Error = err.Error()
		return nil, mr
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := cloneAt(ctx, e.Repo, e.Commit, dir); err != nil {
		logger.Error("clone failed", "err", err)
		mr.Error = err.Error()
		return nil, mr
	}
	rows, modPath, failed, err := collectModule(ctx, dir, e.Commit, cfg, logger)
	if modPath != "" && modPath != e.Module {
		logger.Warn("go.mod module path differs from the corpus", "go_mod", modPath)
		mr.GoModPath = modPath
	}
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
