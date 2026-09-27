# Go loader mode: dependency types from export data

Date: 2026-09-27. Machine: darwin, 14 cores, Go 1.27, `golang.org/x/tools` v0.50.0.

## Question

The Go extractor loaded modules with `NeedDeps | NeedSyntax | NeedTypesInfo`,
which parses and type-checks every dependency, the standard library included,
from source. Does loading only the module's own packages from source, with
dependency types from compiler export data, keep every metric correct, and
how much does it save?

## Change measured

`loadMode` without `packages.NeedDeps`:

```
NeedName | NeedFiles | NeedSyntax | NeedTypes | NeedTypesInfo | NeedImports | NeedModule
```

with `Tests: true` as before. go/packages then type-checks the root packages
(the module's packages and their test variants) from source and every other
package from export data produced by `go list -export`. Imported packages
still carry `PkgPath`, `Module` and `Types`, which is all the metrics read
from a dependency:

- `imports.go`: `imp.Module` is still set for external modules (the fixture's
  replaced `example.com/extmod`) and nil for the standard library; the
  `hub` golden's `external_imports: 1` is unchanged.
- `fanin.go`: import paths only.
- `untested.go`: `types.Implements` against interfaces from other packages
  (`fmt.Stringer` dispatch) works on export-data types; `TypesInfo.Uses` and
  `Selections` are on module test packages, which are still roots.

One behaviour moved: `go list -export` compiles the module, so a package that
fails to type-check now carries the compiler's output as a `ListError` ahead
of the type checker's `TypeError`. `index` now reports the first non-list
error, keeping the precise `TypeError` the tests expect.

## Benchmarks

Interleaved runs, 8 each, of a baseline test binary built from a
`git archive` of master and the new one, `-benchmem`, medians with
[min..max]:

| Benchmark | Metric | Old | New | Delta |
|---|---|---|---|---|
| LoadFixture | B/op | 316.01 MiB [315.67..316.15] | 5.30 MiB [5.30..5.30] | -98.3% |
| LoadFixture | allocs/op | 3529.3k [3529.1k..3529.4k] | 42.8k [42.8k..42.8k] | -98.8% |
| LoadFixture | time/op | 343.1 ms [334.2..395.7] | 181.1 ms [174.4..201.2] | -47.2% |
| LoadSelf | B/op | 819.37 MiB [818.82..819.92] | 47.38 MiB [47.36..47.39] | -94.2% |
| LoadSelf | allocs/op | 9189.8k [9189.7k..9189.9k] | 480.2k [480.2k..480.3k] | -94.8% |
| LoadSelf | time/op | 588.7 ms [556.3..743.5] | 300.7 ms [289.8..448.6] | -48.9% |
| ExtractAll | B/op | 3.49 MiB | 3.49 MiB | 0.0% |
| ExtractAll | allocs/op | 17.9k | 17.9k | +0.2% |
| ExtractAll | time/op | 16.3 ms [13.3..18.3] | 15.6 ms [14.1..16.8] | -4.5% (noise) |

ExtractAll runs on an already loaded module, so it is unaffected, as expected.

`B/op` counts the extractor process only; the `go list -export` child is not
in it. End to end, `astimate rank` on a copy of this repository, 4 interleaved
runs each with a warm build cache:

| | Old | New |
|---|---|---|
| Peak RSS of the astimate process | 510-545 MiB | 52-54 MiB |
| Wall time, warm | 0.57-0.73 s (one 1.08 s outlier) | 0.32-0.34 s (one 0.65 s outlier) |
| Wall time after an edit that changes `internal/metrics`' export data | 0.60-0.74 s (one 1.49 s outlier) | 0.63-0.80 s (one 1.00 s outlier) |

The edited case is the worst one for the new mode: `go list -export` must
recompile the edited package and every dependent before the load, which eats
most of the saving, but it is no slower than before. The first run on a cold
build cache also compiles the standard library and module dependencies once.

## Correctness

- `TestConformance` (all fixture goldens) passes unchanged.
- `TestUntestedReferenceTable`, `TestUntestedNoTests`, `TestUntestedDirective`
  and `TestUntestedInPackageDispatch` (the `refs` fixture, including
  cross-package `fmt.Stringer` dispatch) pass unchanged.
- `astimate baseline write` on a copy of this repository produces identical
  metrics for all 10 packages with the old and new binaries; `rank --json`
  output is byte-identical.

## Decision

Ship it: drop `NeedDeps` from `loadMode`. Every metric is unchanged, and a
load allocates 94-98% less and takes about half the time on a warm cache.

## Follow-up: export data that cannot be built

Date: 2026-09-27, same machine.

`go list -export` needs a C compiler for cgo packages and a writable
`GOCACHE`. Observed with `testdata/go/cgo` (package `native` imports `"C"`,
package `user` imports `native`), in both the current mode and with
`NeedDeps` added back:

- No C compiler (`CGO_ENABLED=1`, `CC` a missing path): the cgo package in the
  module fails with `could not import C (no metadata for C)` in both modes, so
  falling back to `NeedDeps` cannot help; type-checking a cgo package from
  source needs cgo's output too. The load error now names the cause:
  `cgo package example.com/cgo/native needs a C compiler (CC=...)`, wrapped
  over the original error.
- A cgo package outside the module, with no C compiler or with cgo disabled:
  the load already works. go/packages type-checks any dependency whose export
  data `go list` could not build from source instead, and that dependency's
  own errors do not fail the load.
- `CGO_ENABLED=0`, which the go command also picks when no C compiler is on
  `PATH`: build constraints drop `native`, and `user` fails with
  `undefined: native.Add`. The error now names the cause:
  `dependency example.com/cgo/native uses cgo, which is disabled (CGO_ENABLED=0, ...)`.
- Read-only `GOCACHE`: every load fails in both modes. An empty cache fails
  in `go list` itself; a populated cache fails on the first cache miss, as a
  list error on a module package or on a `./...` pseudo-package, which used to
  surface as the misleading `no Go packages in module`. Both now read
  `the Go build cache must be writable (GOCACHE=...)`.

`BenchmarkLoadAfterEdit` appends an exported function to the fixture's `hub`
before each timed load, so `go list -export` recompiles `hub` and its
importers every time, the load an agent's edit loop hits. 6 runs each,
median [min..max]:

| Benchmark | time/op | B/op | allocs/op |
|---|---|---|---|
| LoadFixture | 203.9 ms [180.8..227.4] | 5.19 MiB | 42.6k |
| LoadAfterEdit | 231.2 ms [222.0..320.5] | 5.19 MiB | 42.7k |

The recompile adds about 13% (27 ms) on the fixture; the extractor's own
allocations do not change, since the recompile runs in the `go list` child.
