package typescript

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rfizzle/astimate/internal/lang/duptok"
	"github.com/rfizzle/astimate/internal/metrics"
)

// Token counting methods. They match the values of the --tokenizer flag.
const (
	methodEst   = "est"
	methodO200k = "o200k"
)

// defaultCharsPerToken is the byte-to-token ratio SPEC.md 6.1 sets.
const defaultCharsPerToken = 3.2

// Extractor computes RawMetrics for TypeScript modules: a directory with a
// package.json and the .ts, .tsx, .mts and .cts files below it. It parses
// each module root at most once and shares the result across Packages and
// Extract.
// Construct it with New. It is safe for concurrent use.
type Extractor struct {
	charsPerToken float64
	tokenizer     string
	dup           duptok.Options
	logger        *slog.Logger

	mu      sync.Mutex
	modules map[string]*moduleLoad
}

// moduleLoad is one module root's load, shared by concurrent callers. done
// is closed once m and err are set.
type moduleLoad struct {
	done chan struct{}
	m    *module
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

// WithDuplication sets the duplication settings of SPEC.md 6.3:
// duplication.min_tokens, the shortest normalized token sequence counted as
// a duplicate block (default 40; a value below 1 makes Extract fail);
// duplication.ignore_literal_only, which drops a block made only of
// literals and the punctuation of a literal table (, { } : [ ] ( ) ;)
// (default true); and duplication.fold_signs, under which that rule counts
// a unary + or - applied to a numeric literal as part of the literal
// (default true). It leaves duplication.split_literal_runs as it is.
func WithDuplication(minTokens int, ignoreLiteralOnly, foldSigns bool) Option {
	return func(e *Extractor) {
		e.dup.MinTokens, e.dup.IgnoreLiteralOnly, e.dup.FoldSigns = minTokens, ignoreLiteralOnly, foldSigns
	}
}

// WithDupSplitLiteralRuns sets duplication.split_literal_runs: when on,
// under duplication.ignore_literal_only, each duplicate block is cut at
// every run of at least duplication.min_tokens literal-only tokens and only
// the parts of at least that length are kept (SPEC.md 6.3; default false).
func WithDupSplitLiteralRuns(on bool) Option {
	return func(e *Extractor) { e.dup.SplitLiteralRuns = on }
}

// WithTokenizer selects how tokens_est is counted: "est" (the default)
// divides file bytes by the chars-per-token ratio, "o200k" counts exactly
// with the o200k_base encoding, offline. Any other name makes Extract return
// an error wrapping metrics.ErrUnknownTokenizer.
func WithTokenizer(name string) Option {
	return func(e *Extractor) { e.tokenizer = name }
}

// WithLogger sets the logger that receives, at info level, one record per
// file whose parse tree holds syntax errors. Such files are still measured.
// Nil, the default, discards them.
func WithLogger(logger *slog.Logger) Option {
	return func(e *Extractor) { e.logger = logger }
}

// New returns a TypeScript extractor configured by opts.
func New(opts ...Option) *Extractor {
	e := &Extractor{
		charsPerToken: defaultCharsPerToken,
		tokenizer:     methodEst,
		dup:           duptok.DefaultOptions(),
		modules:       make(map[string]*moduleLoad),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Language returns "typescript".
func (e *Extractor) Language() string { return "typescript" }

// Detect reports whether root contains a package.json file.
func (e *Extractor) Detect(root string) bool {
	fi, err := os.Stat(filepath.Join(root, manifestName))
	return err == nil && fi.Mode().IsRegular()
}

// Packages returns the sorted identifiers of the packages of the module at
// root: the slash-separated directories, relative to root and "." for root
// itself, that hold at least one non-test, non-declaration .ts, .tsx, .mts
// or .cts file. It returns an error when a file cannot be read or parsed.
func (e *Extractor) Packages(root string) ([]string, error) {
	m, err := e.module(context.Background(), root)
	if err != nil {
		return nil, err
	}
	return slices.Clone(m.pkgIDs), nil
}

// Extract computes the v0 metrics of package pkg in the module at mod.Root,
// with the v1 fields instability, abstractness and main_sequence_distance;
// the other v1 fields, which need type information or a toolchain, are
// left nil. The module is parsed on the first call for its root and cached
// in mod.Cache. An identifier Packages does not list yields an error
// wrapping metrics.ErrUnknownPackage, and an unknown tokenizer one wrapping
// metrics.ErrUnknownTokenizer.
func (e *Extractor) Extract(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.RawMetrics, error) {
	m, err := e.cached(ctx, mod)
	if err != nil {
		return metrics.RawMetrics{}, err
	}
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, err)
	}
	p, ok := m.pkgs[pkg]
	if !ok {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, metrics.ErrUnknownPackage)
	}
	if err := e.checkTokenizer(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, err)
	}
	return assemble(m, p, assembleOptions{
		charsPerToken: e.charsPerToken,
		o200k:         e.tokenizer == methodO200k,
		dup:           e.dup,
	})
}

// checkTokenizer returns an error for a tokenizer other than est and o200k.
func (e *Extractor) checkTokenizer() error {
	switch e.tokenizer {
	case methodEst, methodO200k:
		return nil
	default:
		return fmt.Errorf("%w %q: want %q or %q", metrics.ErrUnknownTokenizer, e.tokenizer, methodEst, methodO200k)
	}
}

// Details returns the names behind untested_exports, the names the
// //astimate:untested directive left out of it, the duplicate block
// locations, the declarations of the untested exports and globals, the
// largest file and the source files of package pkg, from the details its
// most recent Extract on mod recorded. When nothing is recorded it runs Extract first, so it fails
// exactly when Extract would.
func (e *Extractor) Details(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.Details, error) {
	if err := ctx.Err(); err != nil {
		return metrics.Details{}, fmt.Errorf("details of %s: %w", pkg, err)
	}
	m, err := e.cached(ctx, mod)
	if err != nil {
		return metrics.Details{}, err
	}
	d, ok := m.detailsOf(pkg)
	if !ok {
		if _, err := e.Extract(ctx, mod, pkg); err != nil {
			return metrics.Details{}, err
		}
		d, _ = m.detailsOf(pkg)
	}
	return metrics.Details{
		UntestedExports:   slices.Clone(d.untested),
		UntestedExcluded:  slices.Clone(d.excluded),
		DupLocations:      slices.Clone(d.dupLocations),
		UntestedPositions: slices.Clone(d.untestedPos),
		GlobalPositions:   slices.Clone(d.globalPos),
		GlobalNames:       slices.Clone(d.globalNames),
		LargestFile:       d.largestFile,
		SourceFiles:       slices.Clone(d.sourceFiles),
	}, nil
}

// Functions returns the functions of package pkg's non-test files, in file
// and declaration order, for the changed-function rule (SPEC.md 6.5 and
// 13.1): top-level functions, top-level variables initialized with a
// function or arrow function, and the methods and function-valued fields of
// top-level classes, with the class name as Receiver. Overload signatures
// have no body and are not listed. File is relative to the package
// directory. The records come from the module's parse, the one Extract
// reads, so they reflect the most recent Extract on mod; an identifier
// Packages does not list yields an error wrapping metrics.ErrUnknownPackage.
func (e *Extractor) Functions(ctx context.Context, mod *metrics.ModuleContext, pkg string) ([]metrics.FunctionInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("functions of %s: %w", pkg, err)
	}
	m, err := e.cached(ctx, mod)
	if err != nil {
		return nil, err
	}
	p, ok := m.pkgs[pkg]
	if !ok {
		return nil, fmt.Errorf("functions of %s: %w", pkg, metrics.ErrUnknownPackage)
	}
	n := 0
	for _, f := range p.src {
		n += len(f.funcs)
	}
	fns := make([]metrics.FunctionInfo, 0, n)
	for _, f := range p.src {
		file := filepath.Base(f.abs)
		if rel, err := filepath.Rel(p.dir, f.abs); err == nil {
			file = rel
		}
		file = filepath.ToSlash(file)
		for _, fn := range f.funcs {
			fns = append(fns, metrics.FunctionInfo{
				Receiver:    fn.receiver,
				Name:        fn.ident,
				Fingerprint: fn.fingerprint,
				Cognitive:   fn.cognitive,
				File:        file,
				Line:        fn.line,
			})
		}
	}
	return fns, nil
}

// Forget drops the cached parse of the module at root, spelled in any form
// that resolves to the same absolute path, with the details and function
// records it holds, so the memory can be reclaimed
// once no ModuleContext still holds it. A load still in flight is left
// alone. Forget of a root never loaded does nothing.
func (e *Extractor) Forget(root string) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if l, ok := e.modules[abs]; ok {
		select {
		case <-l.done:
			delete(e.modules, abs)
		default:
		}
	}
}

// cached returns the module held in mod.Cache, loading it and filling the
// slot on first use.
func (e *Extractor) cached(ctx context.Context, mod *metrics.ModuleContext) (*module, error) {
	e.mu.Lock()
	m, ok := mod.Cache.(*module)
	e.mu.Unlock()
	if ok {
		return m, nil
	}
	m, err := e.module(ctx, mod.Root)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	mod.Cache = m
	e.mu.Unlock()
	return m, nil
}

// module returns the parse of the module at root, performing it on the
// first call for that root. Concurrent callers for the same root wait for a
// single load. A failed load is not kept, so a later call retries it.
func (e *Extractor) module(ctx context.Context, root string) (*module, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving module root %s: %w", root, err)
	}
	for {
		e.mu.Lock()
		l, joined := e.modules[abs]
		if !joined {
			l = &moduleLoad{done: make(chan struct{})}
			e.modules[abs] = l
		}
		e.mu.Unlock()

		if !joined {
			l.m, l.err = loadModule(ctx, abs, loadOptions{o200k: e.tokenizer == methodO200k, logger: e.logger})
			if l.err != nil {
				e.mu.Lock()
				delete(e.modules, abs)
				e.mu.Unlock()
			}
			close(l.done)
			return l.m, l.err
		}
		select {
		case <-l.done:
		case <-ctx.Done():
			return nil, fmt.Errorf("loading %s: %w", abs, ctx.Err())
		}
		if l.err != nil && ctx.Err() == nil &&
			(errors.Is(l.err, context.Canceled) || errors.Is(l.err, context.DeadlineExceeded)) {
			continue
		}
		return l.m, l.err
	}
}
