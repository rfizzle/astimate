package golang

import (
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rfizzle/astimate/internal/metrics"
	"golang.org/x/tools/go/packages"
)

func TestExtractorIsForgetter(t *testing.T) {
	var ext metrics.Extractor = New()
	if _, ok := ext.(metrics.Forgetter); !ok {
		t.Fatal("*Extractor does not implement metrics.Forgetter")
	}
}

// holds reports whether e holds a load, finished or in flight, for root.
func holds(e *Extractor, root string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.modules[root]
	return ok
}

func TestForgetDropsLoadAndExtractReloads(t *testing.T) {
	var loads atomic.Int32
	e := New(withLoadCounter(&loads))
	root := fixtureRoot(t)
	mod := &metrics.ModuleContext{Root: root, ModulePath: "example.com/fixture"}
	if _, err := e.Extract(t.Context(), mod, "example.com/fixture/a"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	l, ok := mod.Cache.(*loaded)
	if !ok {
		t.Fatalf("mod.Cache = %T, want *loaded", mod.Cache)
	}
	if _, ok := l.detailsOf("example.com/fixture/a"); !ok {
		t.Fatal("Extract recorded no details")
	}

	// A differently spelled root that resolves to the same path is forgotten.
	sep := string(filepath.Separator)
	e.Forget(root + sep + "a" + sep + "..")

	if holds(e, root) {
		t.Fatal("extractor still holds a load for the root after Forget")
	}
	if _, ok := l.detailsOf("example.com/fixture/a"); ok {
		t.Error("Forget kept the load's per-package details")
	}

	fresh := &metrics.ModuleContext{Root: root, ModulePath: "example.com/fixture"}
	if _, err := e.Extract(t.Context(), fresh, "example.com/fixture/a"); err != nil {
		t.Fatalf("Extract after Forget: %v", err)
	}
	if n := loads.Load(); n != 2 {
		t.Fatalf("packages.Load called %d times, want 2 (one reload after Forget)", n)
	}
	if fresh.Cache == mod.Cache {
		t.Error("Extract after Forget reused the forgotten load")
	}
}

func TestForgetUnknownRoot(t *testing.T) {
	e := New()
	e.Forget(t.TempDir())
	if n := len(e.modules); n != 0 {
		t.Fatalf("modules has %d entries, want 0", n)
	}
}

// TestForgetLeavesInFlightLoad checks that Forget returns without waiting
// for, or removing, a load still running, and that its caller still gets it.
func TestForgetLeavesInFlightLoad(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	e := New()
	e.load = func(cfg *packages.Config, patterns ...string) ([]*packages.Package, error) {
		close(started)
		<-release
		return packages.Load(cfg, patterns...)
	}
	root := fixtureRoot(t)
	errc := make(chan error, 1)
	go func() {
		_, err := e.Packages(root)
		errc <- err
	}()
	<-started
	e.Forget(root)
	if !holds(e, root) {
		t.Error("Forget removed a load still in flight")
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("Packages: %v", err)
	}
}
