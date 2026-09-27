# go/types data race under go/packages parallel type-checking

Date: 2026-09-27. Machine: darwin/arm64, 14 cores.
Toolchain: `go version go1.27.1 darwin/arm64` (Homebrew).
Modules from `go.mod`: `golang.org/x/tools` v0.50.0, `golang.org/x/sync` v0.23.0.

## Question

`go test -race` once reported a data race inside `go/types` while
`calibration/collect`'s `TestCollectModule` loaded this repository, and three
reruns did not reproduce it. Is it real, is it ours, and can it be worked
around?

## Runs

```
go test -race -run TestCollectModule -count=50 -timeout 20m ./calibration/collect/
```

Reproduced: one `WARNING: DATA RACE` in 50 iterations, failing
`TestCollectModule/this_repository`; every other iteration passed (86 s wall).

```
go test -race -count=20 -timeout 20m ./internal/lang/golang/
```

Not reproduced: 20 iterations of the whole extractor suite passed with no
race report (956 s). That run included the new test-only loader test.

## Report

Both goroutines are go/packages type-checking workers, started by the
errgroup in `(*loader).refine` (`go/packages/packages.go:954`) and running
`(*loader).loadPackage` (`packages.go:1273`) inside a single
`packages.Load` call. Each runs its own `types.Checker` over a different root
package, and both touch the same instantiated `*types.Named`:

```
WARNING: DATA RACE
Read at 0x00c001045130 by goroutine 34507:
  go/types.(*Checker).isComplete()       go/types/cycles.go:122
  go/types.(*Checker).callExpr()         go/types/call.go:331
  go/types.(*Checker).exprInternal()     go/types/expr.go:1124
  go/types.(*Checker).rawExpr()          go/types/expr.go:985
  go/types.(*Checker).genericExprList()  go/types/call.go:420
  go/types.(*Checker).callExpr()         go/types/call.go:311
  ...
  go/types.(*Checker).compositeLit()     go/types/literals.go:177
  ...
  go/types.(*Checker).builtin()          go/types/builtins.go:56
  ...
  go/types.(*Checker).funcLit.func1()    go/types/literals.go:100
  go/types.(*Checker).processDelayed()   go/types/check.go:512
  ...
  go/types.(*Checker).Files()            go/types/check.go:422
  golang.org/x/tools/go/packages.(*loader).loadPackage()  packages.go:1273
  golang.org/x/tools/go/packages.(*loader).refine.func2.1()  packages.go:956

Previous write at 0x00c001045130 by goroutine 34506:
  go/types.(*Named).unpack()             go/types/named.go:244
  go/types.(*Named).Underlying()         go/types/named.go:602
  go/types.asInterface()                 go/types/unify.go:278
  go/types.(*unifier).nify()             go/types/unify.go:464
  go/types.(*unifier).unify()            go/types/unify.go:145
  go/types.(*Checker).infer()            go/types/infer.go:180
  go/types.(*Checker).arguments()        go/types/call.go:626
  go/types.(*Checker).callExpr()         go/types/call.go:312
  ... (same statement path as the read)
  go/types.(*Checker).Files()            go/types/check.go:422
  golang.org/x/tools/go/packages.(*loader).loadPackage()  packages.go:1273
  golang.org/x/tools/go/packages.(*loader).refine.func2.1()  packages.go:956
```

The two stacks are the same statement path, so the two checkers are most
likely a package and its test variant, which go/packages type-checks
separately from the same source, both instantiating a generic type that
lives in a shared dependency.

## Attribution: upstream, in go/types

`(*Named).unpack` (named.go:244) writes `n.fromRHS` of an instance while
holding `n.mu`, which is how a `Named` shared between checkers is meant to be
made safe. `(*Checker).isComplete` (cycles.go:122) reads `t.fromRHS` of a
`*Named` directly, without `unpack` or the mutex, so a second checker can
read the field while the first is expanding the instance. No code of ours is
on either stack, and nothing in astimate shares `types` values between
loads: the race is between two goroutines of one `packages.Load`.

## Upstream status

Reported as golang/go#81122 ("go/types: data race between (*Named).unpack
and (*Checker).isComplete under concurrent go/packages export-data loading
on go1.27.0"), open and labelled NeedsInvestigation when checked on
2026-09-27. Its follow-up comment reproduces the same two frames with
source-mode loading, through the instantiation branch of `unpack`, which is
our case; it also notes that `GOMAXPROCS=1` masks the race. No fix has
landed.

## Bumps tried

Checked on 2026-09-27: the newest Go 1.27 release is go1.27.1 (tags
go1.27.0, go1.27.1; no go1.28 tag yet) and the newest `golang.org/x/tools`
is v0.50.0. Both are what this module already uses, so there was no newer
toolchain or x/tools to try and neither `go.mod` line changed.

| Go | x/tools | Setting | Command | Result |
| --- | --- | --- | --- | --- |
| go1.27.1 | v0.50.0 | default GOMAXPROCS (14) | 50-count soak | 1 race in 50 (first run above) |
| go1.27.1 | v0.50.0 | `GOMAXPROCS=1` | 50-count soak | clean: 50 of 50 passed, no race report (181.7 s) |
| go1.27.1 | v0.50.0 | `GOMAXPROCS=1` | `make race-soak` | clean: 50 of 50 passed, no race report (144.6 s) |

## Fix: GOMAXPROCS=1 for the collector package only

Serializing our own loads cannot help, because the racing checkers belong to
the same `packages.Load` call. `packages.Config` has no option that bounds
type-checking parallelism: the workers are gated by the package-level
`cpuLimit` semaphore, sized from `runtime.GOMAXPROCS(0)` when go/packages is
initialized, so the only lever is the process's `GOMAXPROCS`. Loading one
package at a time in `TestCollectModule/this_repository` was the other
candidate and was not taken: with tests enabled a single package's load
still type-checks the package and its test variant concurrently, the pair
most likely racing here, and the test would stop exercising the collector's
real whole-module load.

`make test` and the CI `check` job's `go test` step therefore run in two
commands: every package except `./calibration/collect/` with the default
`GOMAXPROCS`, then `GOMAXPROCS=1 go test -race ./calibration/collect/`.
`-race` stays on for both. The race is in go/types, not in astimate; the
production binary is untouched, and outside the race detector the racy read
sees either nil or the finished right-hand side, so no metric has been seen
to change.

Cost, `go test -race -count=1 ./calibration/collect/` on this host:

| Setting | Wall time (two runs) |
| --- | --- |
| default GOMAXPROCS (14) | 15.3 s (cold), 10.6 s |
| `GOMAXPROCS=1` | 41.1 s, 44.9 s |

About 30 s more for that package, and it now runs after the other packages
instead of alongside them, so `make check` grows by up to its full 41 s;
every other package keeps full parallelism.

## Re-verifying: make race-soak

`make race-soak` runs the soak under the setting `make test` uses:

```
GOMAXPROCS=1 go test -race -run TestCollectModule -count=50 -timeout 25m ./calibration/collect/
```

It is not part of `make check`. After each Go or x/tools bump, run it; to
test whether upstream fixed the race, run the same command without
`GOMAXPROCS=1`. When 50 iterations pass at the default setting, record that
here and drop the split from `make test` and the CI step.
