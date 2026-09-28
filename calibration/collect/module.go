package main

import (
	"context"
	"fmt"
	"runtime"
	"runtime/metrics"
	"time"

	astmetrics "github.com/rfizzle/astimate/internal/metrics"
)

// ModuleRow is one module's module-level row (SPEC.md 8.1): a line of
// modules.jsonl. Its module, package and metrics fields match a
// packages.jsonl row, so the fitter reads both files alike.
type ModuleRow struct {
	// Module is the module path from the module's go.mod.
	Module string `json:"module"`
	// Commit is the pinned commit the module was collected at.
	Commit string `json:"commit"`
	// Package is always metrics.ModuleRowID.
	Package string `json:"package"`
	// Metrics are the module row's metrics: v0 fields zero and v1 fields
	// null except the module-wide dup_blocks_cross_pkg.
	Metrics astmetrics.RawMetrics `json:"metrics"`
	// Packages is the number of module packages the pass streamed.
	Packages int `json:"packages"`
	// Cost is what the module pass cost on the collecting machine.
	Cost PassCost `json:"cost"`
}

// PassCost is the measured cost of loading a module and of its
// module-wide pass. Wall times depend on the machine run.json describes;
// the heap figures are the Go runtime's, not the process's resident set.
type PassCost struct {
	// LoadMS is the wall time of the module load (go/packages and parsing)
	// in milliseconds.
	LoadMS int64 `json:"load_ms"`
	// PassMS is the wall time of the module pass alone, the whole-module
	// token stream and suffix array behind dup_blocks_cross_pkg, run on the
	// loaded module, in milliseconds.
	PassMS int64 `json:"pass_ms"`
	// PassAllocBytes is the heap the pass allocated in total.
	PassAllocBytes uint64 `json:"pass_alloc_bytes"`
	// PassPeakHeapBytes is the highest heap-object size seen during the
	// pass, sampled every millisecond, above the size after a collection
	// just before it: roughly the extra memory the pass holds at its peak,
	// garbage not yet collected included.
	PassPeakHeapBytes uint64 `json:"pass_peak_heap_bytes"`
}

// Runtime metric names measurePass reads.
const (
	heapObjectsMetric = "/memory/classes/heap/objects:bytes"
	heapAllocsMetric  = "/gc/heap/allocs:bytes"
)

// measurePass runs f and returns its wall time, the heap it allocated and
// the peak heap it held above the heap after a collection just before it.
func measurePass(f func() error) (ms int64, alloc, peak uint64, err error) {
	runtime.GC()
	samples := []metrics.Sample{{Name: heapObjectsMetric}, {Name: heapAllocsMetric}}
	metrics.Read(samples)
	before, allocBefore := samples[0].Value.Uint64(), samples[1].Value.Uint64()

	stop := sampleHeap(before)
	start := time.Now()
	err = f()
	ms = time.Since(start).Milliseconds()
	high := stop()
	metrics.Read(samples)
	high = max(high, samples[0].Value.Uint64())
	return ms, samples[1].Value.Uint64() - allocBefore, high - before, err
}

// sampleHeap reads the heap-object size every millisecond from now on,
// starting from floor, until the returned stop is called, which returns
// the highest size seen.
func sampleHeap(floor uint64) (stop func() uint64) {
	quit, done := make(chan struct{}), make(chan uint64)
	go func() {
		s := []metrics.Sample{{Name: heapObjectsMetric}}
		high := floor
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			metrics.Read(s)
			high = max(high, s[0].Value.Uint64())
			select {
			case <-quit:
				done <- high
				return
			case <-tick.C:
			}
		}
	}()
	return func() uint64 {
		close(quit)
		return <-done
	}
}

// moduleRow measures the module row of the module in t through mm, which
// shares the load already made, and returns it tagged with commit.
func moduleRow(ctx context.Context, mm astmetrics.ModuleMetrics, mod *astmetrics.ModuleContext,
	modPath, commit string, pkgs int, loadMS int64,
) (ModuleRow, error) {
	var m astmetrics.RawMetrics
	ms, alloc, peak, err := measurePass(func() error {
		var err error
		m, err = mm.ModuleRow(ctx, mod)
		return err
	})
	if err != nil {
		return ModuleRow{}, fmt.Errorf("measuring the module row of %s: %w", modPath, err)
	}
	return ModuleRow{
		Module:   modPath,
		Commit:   commit,
		Package:  astmetrics.ModuleRowID,
		Metrics:  m,
		Packages: pkgs,
		Cost:     PassCost{LoadMS: loadMS, PassMS: ms, PassAllocBytes: alloc, PassPeakHeapBytes: peak},
	}, nil
}
