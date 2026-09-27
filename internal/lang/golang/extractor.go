package golang

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

// ErrUnknownPackage is returned by Extract when the requested import path is
// not a non-test package of the module.
var ErrUnknownPackage = errors.New("unknown package")

// Extractor computes RawMetrics for Go modules. It loads each module root at
// most once and shares the result across Packages and Extract. Construct it
// with New. It is safe for concurrent use.
type Extractor struct {
	load loadFunc

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

// New returns a Go extractor configured by opts.
func New(opts ...Option) *Extractor {
	e := &Extractor{
		load:    packages.Load,
		modules: make(map[string]*moduleLoad),
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
	return slices.Clone(l.paths), nil
}

// Extract computes the metrics of the package with import path pkg in the
// module at mod.Root. The module is loaded on the first call for its root
// and cached in mod.Cache. An import path that is not a non-test package of
// the module yields an error wrapping ErrUnknownPackage.
func (e *Extractor) Extract(ctx context.Context, mod *metrics.ModuleContext, pkg string) (metrics.RawMetrics, error) {
	l, err := e.cached(ctx, mod)
	if err != nil {
		return metrics.RawMetrics{}, err
	}
	if err := ctx.Err(); err != nil {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, err)
	}
	p, ok := l.pkgs[pkg]
	if !ok {
		return metrics.RawMetrics{}, fmt.Errorf("extracting %s: %w", pkg, ErrUnknownPackage)
	}
	return metrics.RawMetrics{
		Files: len(p.GoFiles),
	}, nil
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
