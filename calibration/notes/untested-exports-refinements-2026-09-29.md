# untested_exports: module-wide test references and the closed interface list

Date: 2026-09-29. Machine: linux/arm64 VM, 13 cores, 5.9 GiB, Go 1.27.1,
cgo enabled. Code: the change that adds both refinements to SPEC.md 6.4.

## Question

`astimate-labels-2026-09-28.md` found that every `untested_exports`
finding the draft labels allow is one of three kinds: `Error`/`Unwrap`
methods, constructors exercised only by other packages' tests, and exports
of test-support packages such as `metricstest`. SPEC.md 6.4 counted a
reference only from the package's own test files, and only by name, so a
method the runtime or the standard library calls was never seen. How much
of the count do two refinements remove, and what do they cost?

- **other**: a reference from a test file of any package of the module
  counts, direct or through interface dispatch (the dispatch needs the
  receiver's package in the calling test binary).
- **iface**: a method whose receiver type, or a pointer to it, implements an
  interface of the closed list in SPEC.md 6.4 is covered.

A test-support package gets no rule of its own; with **other** it reads as
tested when other packages' tests use its exports.

## Method

`TestMeasureUntestedRefinements` in
`internal/lang/golang/internal/tests/measure_test.go` loads a module once,
builds the index once and counts every package under four rule sets: own
tests only (the previous metric), **other**, **iface**, and both (the
shipped metric). It skips unless `ASTIMATE_MEASURE_UNTESTED` is set:

```
ASTIMATE_MEASURE_UNTESTED=std go test ./internal/lang/golang/internal/tests/ \
  -run TestMeasureUntestedRefinements -v -count=1
ASTIMATE_MEASURE_UNTESTED=/path/to/module go test ...   # same
```

The "own" column equals the previous extractor's count: the per-module sums
match a run of the unmodified code on the same inputs (std 3,427, this
repository 24).

**Corpus coverage.** The 36 corpus clones were not on this host, and the
run was offline (no cloning, `GOPROXY=off`). Nine corpus modules are in the
module cache at unpinned versions; each was copied to a scratch directory
and loaded as a main module. Four loaded (`BurntSushi/toml` v1.6.0,
`google/go-cmp` v0.7.0, `google/uuid` v1.3.0, `pelletier/go-toml/v2`
v2.4.3); five failed for test dependencies absent from the cache
(`cobra`, `viper`, `zap`, `testify`, `prometheus/client_golang`). The full
36-module measurement at the pinned commits is a follow-up.

## Counts

Counted exports are exported funcs and methods outside generated files and
without the `//astimate:untested` directive.

| Module | Packages | Changed | Exports | own | other | iface | both | Covered by both |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| std | 381 | 200 | 8,216 | 3,427 | 2,681 | 2,853 | 2,247 | 1,180 (14.4%) |
| this repository | 55 | 5 | 423 | 24 | 15 | 21 | 12 | 12 (2.8%) |
| BurntSushi/toml | 8 | 3 | 68 | 39 | 32 | 31 | 25 | 14 (20.6%) |
| google/go-cmp | 10 | 4 | 224 | 153 | 112 | 149 | 109 | 44 (19.6%) |
| google/uuid | 1 | 1 | 55 | 10 | 10 | 4 | 4 | 6 (10.9%) |
| pelletier/go-toml/v2 | 16 | 1 | 88 | 27 | 24 | 27 | 24 | 3 (3.4%) |

Share of counted exports each refinement covers on its own: on std,
**other** 746 (9.1%) and **iface** 574 (7.0%), overlapping on 140; on this
repository **other** 9 and **iface** 3, no overlap.

Largest std changes (own -> both): `vendor/golang.org/x/net/quic` 96 -> 32
and `debug/elf` 76 -> 13, almost all `String` methods that `fmt` calls;
`vendor/golang.org/x/net/dns/dnsmessage` 97 -> 47, both rules;
`internal/testenv` 31 -> 5, a test-support package that only other
packages' tests call; `crypto/internal/fips140/sha3` 29 -> 4 and
`crypto/internal/fips140/aes/gcm` 27 -> 7, exercised from the public
packages' tests.

This repository, what is left: `internal/baseline` `CollectCrossBlocks`,
`HeadCommit`, `snapshot.CrossBlocks`; `internal/engine` `ValidTokenizer`;
`internal/metrics/metricstest` the `Details` methods of three fakes, which
the suite calls from non-test code. The labels' three false-positive
kinds are gone: the test-support packages `metricstest` 9 -> 3 and
`duptoktest` 1 -> 0 and `report` 1 -> 0 through other packages' tests,
`engine` 3 -> 1 and `baseline` 5 -> 3 through the closed list and other
packages' tests.

## Cost

The index is built once per module load, serially, in one pass over the
`types.Info.Uses` and `Selections` of every test package. Measured by the
measurement test: 48.8 ms on std, 3.1 ms on this repository, 0.1 to 5.6 ms
on the four corpus modules. `BenchmarkRefsBuild`: about 34 us on the
fixture, 2.7 ms on this repository (it replaces per-package walks of the
package's own test variants, which the parallel std extraction spread over
its workers).

Whole extraction, load included (`TestMeasureExtractTime` in
`internal/lang/golang/timing_test.go`, run on the unmodified code in a
pristine copy and on the change, medians):

| Target | Before | After |
| --- | --- | --- |
| std, `ExtractStdlibAll`, four alternating rounds of 11 to 15 runs | 1.452, 1.525, 1.734, 1.442 s (mean 1.538) | 1.491, 1.702, 1.568, 1.433 s (mean 1.549) |
| this repository, 15 runs | 356 ms | 350 ms |

The host's run-to-run noise is about 10%; the mean change on std is +0.7%,
and the index build is at most 3% of the std extraction even if nothing
overlapped it. The four small corpus modules (100 to 400 ms each) were
too noisy to compare beyond "no consistent direction". The change stays
under the 10% bound.

## Conclusion

Both refinements ship (SPEC.md 6.4, and the module-wide rule for
TypeScript in 13.1). The gate rule is `max_delta: 0` with no `max`, so a
lower count moves no threshold and nothing is refitted.
