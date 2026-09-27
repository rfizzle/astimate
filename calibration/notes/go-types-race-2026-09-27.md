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

## Workaround: none applied

Serializing our own loads cannot help, because the racing checkers belong to
the same `packages.Load` call. `packages.Config` has no option that bounds
type-checking parallelism: the workers are gated by the package-level
`cpuLimit` semaphore, sized from `runtime.GOMAXPROCS(0)` when go/packages is
initialized. The only lever is running the whole process with
`GOMAXPROCS=1`, which would slow every load and changes global state, so it
is not applied, and `-race` stays on.

Impact: about one run of `TestCollectModule/this_repository` in 50 under
`-race` may fail with this report, so `make check` can flake at that rate.
Outside the race detector the read sees either nil or the finished
right-hand side; a nil read would at worst make `isComplete` report a type
incomplete for one expression. No metric has been seen to change.

Action: report upstream to golang/go against go/types (`isComplete` reading
`Named.fromRHS` without `unpack`) with this stack, and rerun the command
above after each toolchain or x/tools bump; when 50 iterations pass, record
that here.
