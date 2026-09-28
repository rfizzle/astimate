package golang

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/golang/internal/dup"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/inspect"
	"github.com/rfizzle/astimate/internal/lang/golang/internal/load"
	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// Extractor computes RawMetrics for Go modules. It loads each module root at
// most once and shares the result across Packages and Extract. Construct it
// with New. It is safe for concurrent use.
type Extractor struct {
	load load.Func

	// charsPerToken is the ratio the "est" tokenizer divides bytes by.
	charsPerToken float64
	// tokenizer is the token counting method: inspect.MethodEst or
	// inspect.MethodO200k.
	tokenizer string
	// dup configures duplicate detection.
	dup dup.Options
	// logger receives an info record per module directory a load skipped;
	// nil discards them.
	logger *slog.Logger

	// o200kOnce guards the one-time build of o200k, because
	// tiktoken.SetBpeLoader writes an unguarded library global.
	o200kOnce sync.Once
	o200k     inspect.Counter
	o200kErr  error

	mu      sync.Mutex
	modules map[string]*moduleLoad
}

// moduleLoad is one module root's load, shared by concurrent callers. done is
// closed once l and err are set.
type moduleLoad struct {
	done chan struct{}
	l    *loaded
	err  error
}

// Option configures an Extractor.
type Option func(*Extractor)

// WithCharsPerToken sets the bytes-per-token ratio of the "est" tokenizer
// (SPEC.md 6.1; default 3.2). A ratio that is not positive and finite makes
// Extract fail.
func WithCharsPerToken(ratio float64) Option {
	return func(e *Extractor) { e.charsPerToken = ratio }
}

// WithTokenizer selects how tokens_est is counted: "est" (the default)
// divides file bytes by the chars-per-token ratio, "o200k" counts exactly
// with the o200k_base encoding, offline. Any other name makes Extract return
// an error wrapping metrics.ErrUnknownTokenizer.
func WithTokenizer(name string) Option {
	return func(e *Extractor) { e.tokenizer = name }
}

// WithDupMinTokens sets duplication.min_tokens, the shortest normalized
// token sequence counted as a duplicate block (SPEC.md 6.3; default 40). A
// value below 1 makes Extract fail.
func WithDupMinTokens(n int) Option {
	return func(e *Extractor) { e.dup.MinTokens = n }
}

// WithDupIgnoreLiteralOnly sets duplication.ignore_literal_only: when on,
// a duplicate block made only of literals and the punctuation of a literal
// table (, { } : [ ] ( ) ;) is dropped, so repeated runs of data tables do
// not count as duplication (SPEC.md 6.3; default true).
func WithDupIgnoreLiteralOnly(on bool) Option {
	return func(e *Extractor) { e.dup.IgnoreLiteralOnly = on }
}

// WithDupFoldSigns sets duplication.fold_signs: when on, the literal-only
// rule of WithDupIgnoreLiteralOnly counts a unary + or - directly before a
// numeric literal as part of the literal, so a table of negative numbers is
// dropped like any other. Matching is unchanged (SPEC.md 6.3; default true).
func WithDupFoldSigns(on bool) Option {
	return func(e *Extractor) { e.dup.FoldSigns = on }
}

// WithDupSplitLiteralRuns sets duplication.split_literal_runs: when on,
// under WithDupIgnoreLiteralOnly, each duplicate block is cut at every run
// of at least duplication.min_tokens literal-only tokens and only the parts
// of at least that length are kept, so literal tables that declaration
// headers join into one block are dropped too. It applies to the
// cross-package count as well (SPEC.md 6.3; default false).
func WithDupSplitLiteralRuns(on bool) Option {
	return func(e *Extractor) { e.dup.SplitLiteralRuns = on }
}

// WithLogger sets the logger that receives, at info level, one record per
// module directory whose Go files build constraints exclude entirely, such
// as a package made only of cgo files when cgo is disabled. Each is logged
// once per module load. Nil, the default, discards them.
func WithLogger(logger *slog.Logger) Option {
	return func(e *Extractor) { e.logger = logger }
}

// New returns a Go extractor configured by opts.
func New(opts ...Option) *Extractor {
	e := &Extractor{
		load:          packages.Load,
		charsPerToken: inspect.DefaultCharsPerToken,
		tokenizer:     inspect.MethodEst,
		dup:           dup.DefaultOptions(),
		modules:       make(map[string]*moduleLoad),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Language returns "go".
func (e *Extractor) Language() string { return "go" }

// Detect reports whether root contains a go.mod file.
func (e *Extractor) Detect(root string) bool {
	fi, err := os.Stat(filepath.Join(root, "go.mod"))
	return err == nil && fi.Mode().IsRegular()
}

// Packages returns the sorted import paths of the non-test packages of the
// module at root. Dependencies and test packages are not included. It
// returns an error naming the first package that failed to load or
// type-check.
func (e *Extractor) Packages(root string) ([]string, error) {
	l, err := e.module(context.Background(), root)
	if err != nil {
		return nil, err
	}
	return slices.Clone(l.Paths), nil
}

// Extract computes the v0 metrics of the package with import path pkg in the
// module at mod.Root, and the v1 fields assemble lists; coverage_pct and
// changed_func_cognitive_max are left nil. The module is loaded on the
// first call for its root and cached in mod.Cache. An import path that is not
// a non-test package of the module yields an error wrapping
// metrics.ErrUnknownPackage, and an unknown tokenizer one wrapping
// metrics.ErrUnknownTokenizer.
func (e *Extractor) Extract(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.RawMetrics, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return metrics.RawMetrics{}, err
	}
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, err)
	}
	p, ok := l.Pkgs[pkg]
	if !ok {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, metrics.ErrUnknownPackage)
	}
	counter, err := e.counter()
	if err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, err)
	}
	return assemble(ctx, l, p, assembleOptions{counter: counter, dup: e.dup})
}

// ModuleRow returns the module-level row of the module at mod.Root
// (metrics.ModuleMetrics): v0 fields zero, v1 fields null except
// dup_blocks_cross_pkg, the number of distinct duplicate blocks whose
// occurrences lie in two or more of the module's packages. It shares the
// load and the memoized cross-package pass with Extract.
func (e *Extractor) ModuleRow(ctx context.Context, mod *metrics.ModuleContext) (metrics.RawMetrics, error) {
	cross, err := e.moduleCross(ctx, mod)
	if err != nil {
		return metrics.RawMetrics{}, err
	}
	n := len(cross.Blocks)
	return metrics.RawMetrics{DupBlocksCrossPkg: &n}, nil
}

// ModuleDetails returns the details of the module row of the module at
// mod.Root (metrics.ModuleDetailer): every cross-package duplicate block
// counted by the row's dup_blocks_cross_pkg, with all its occurrences. It
// shares the memoized cross-package pass with ModuleRow and Extract, so it
// reads no file once either has run, and fails when ModuleRow would.
func (e *Extractor) ModuleDetails(ctx context.Context, mod *metrics.ModuleContext) (metrics.Details, error) {
	cross, err := e.moduleCross(ctx, mod)
	if err != nil {
		return metrics.Details{}, err
	}
	return metrics.Details{CrossBlocks: cross.BlocksOf("")}, nil
}

// moduleCross returns the memoized cross-package duplication of the module
// at mod.Root, loading the module and running the pass on first use.
func (e *Extractor) moduleCross(ctx context.Context, mod *metrics.ModuleContext) (dup.Cross, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return dup.Cross{}, err
	}
	var cross dup.Cross
	if err = ctx.Err(); err == nil {
		cross, err = l.cross.Cross(l.Module, load.OSFiles{}, e.dup)
	}
	if err != nil {
		return dup.Cross{}, fmt.Errorf("extracting %s: %w", metrics.ModuleRowID, err)
	}
	return cross, nil
}

// counter returns the token counter the tokenizer option selects. The o200k
// counter is built on first use and shared by every later call.
func (e *Extractor) counter() (inspect.Counter, error) {
	switch e.tokenizer {
	case inspect.MethodEst:
		return inspect.NewRatioCounter(e.charsPerToken), nil
	case inspect.MethodO200k:
		e.o200kOnce.Do(func() { e.o200k, e.o200kErr = inspect.NewO200kCounter() })
		return e.o200k, e.o200kErr
	default:
		return nil, fmt.Errorf("%w %q: want %q or %q", metrics.ErrUnknownTokenizer, e.tokenizer, inspect.MethodEst, inspect.MethodO200k)
	}
}

// cached returns the load held in mod.Cache, loading the module and filling
// the slot on first use.
func (e *Extractor) cached(ctx context.Context, mod *metrics.ModuleContext) (*loaded, error) {
	e.mu.Lock()
	l, ok := mod.Cache.(*loaded)
	e.mu.Unlock()
	if ok {
		return l, nil
	}
	l, err := e.module(ctx, mod.Root)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	mod.Cache = l
	e.mu.Unlock()
	return l, nil
}

// Forget drops the cached load of the module at root, spelled in any form
// that resolves to the same absolute path, together with the per-package
// debug details that load recorded, so the memory can be reclaimed once no
// ModuleContext still holds it. A later Packages or Extract for root, through
// a ModuleContext whose Cache is empty, loads the module again. Forget does
// not wait for or disturb a load of root still in flight: it leaves that
// entry and returns. Forget of a root never loaded does nothing.
func (e *Extractor) Forget(root string) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	e.mu.Lock()
	m, ok := e.modules[abs]
	if ok {
		select {
		case <-m.done:
			delete(e.modules, abs)
		default:
			ok = false
		}
	}
	e.mu.Unlock()
	if !ok || m.l == nil {
		return
	}
	m.l.detailsMu.Lock()
	m.l.details = nil
	m.l.detailsMu.Unlock()
}

// logSkipped logs each module directory l skipped at info level, when e has
// a logger.
func (e *Extractor) logSkipped(l *loaded) {
	if e.logger == nil {
		return
	}
	for _, s := range l.Skipped {
		e.logger.Info("skipped package", "package", s.ImportPath, "dir", s.Dir, "reason", s.Reason)
	}
}

// module returns the load of the module at root, performing it on the first
// call for that root. Concurrent callers for the same root wait for a single
// load. A failed load is not kept, so a later call retries it; a waiter whose
// own context is still live retries a load that another caller's context
// cancelled.
func (e *Extractor) module(ctx context.Context, root string) (*loaded, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving module root %s: %w", root, err)
	}
	for {
		e.mu.Lock()
		m, joined := e.modules[abs]
		if !joined {
			m = &moduleLoad{done: make(chan struct{})}
			e.modules[abs] = m
		}
		e.mu.Unlock()

		if !joined {
			m.l, m.err = loadModule(&packages.Config{Dir: abs, Context: ctx}, e.load)
			if m.err != nil {
				e.mu.Lock()
				delete(e.modules, abs)
				e.mu.Unlock()
			} else {
				e.logSkipped(m.l)
			}
			close(m.done)
			return m.l, m.err
		}
		select {
		case <-m.done:
		case <-ctx.Done():
			return nil, fmt.Errorf("loading %s: %w", abs, ctx.Err())
		}
		if m.err != nil && ctx.Err() == nil &&
			(errors.Is(m.err, context.Canceled) || errors.Is(m.err, context.DeadlineExceeded)) {
			continue
		}
		return m.l, m.err
	}
}
