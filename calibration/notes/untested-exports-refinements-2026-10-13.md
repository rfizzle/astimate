# untested_exports: option funcs, interface methods and test-support packages

Date: 2026-10-13. Machine: linux/arm64 VM (OrbStack), Go 1.27, cgo enabled.
Code: the change that adds rules a to d to SPEC.md 6.4. Follows
`untested-exports-refinements-2026-09-29.md`.

## Question

The 2026-10-13 field adjudication found about 20 of 148 covered-export false
findings in three shapes: functional options (`WithLogger`), exported methods
of unexported types that exist to satisfy an interface
(`pcapngStreamRecording.Write`, `rewriteHost.RoundTrip`), and `pflag.Value`
methods; and a fourth, a test-support package's own helpers calling its
fakes. How much of the count does each rule remove?

- **option**: rule a, functional-option constructors.
- **held**: rule b, an exported method of an unexported type that
  implements an in-module interface some test holds a value of.
- **support**: rule d, references from a test-support package's own non-test
  code to its own funcs and methods.
- Rule c (`pflag.Value`, `pflag.SliceValue` on the closed list) is a change
  to the closed list, not a column: it is part of `prev` here, since the
  closed list is matched by method set and the corpus modules measured here
  declare no such method.

## Method

`TestMeasureUntestedRefinements` (`internal/lang/golang/internal/tests/measure_test.go`)
counts each package under: `prev` (module-wide tests plus the closed list,
the metric before this change), `prev` plus one rule, and all rules.

```
ASTIMATE_MEASURE_UNTESTED=std go test ./internal/lang/golang/internal/tests/ \
  -run TestMeasureUntestedRefinements -v -count=1
```

Offline, `GOPROXY=off`. The corpus clones are not on this host; four of the
nine corpus modules in the module cache load (as in the 2026-09-29 note),
each copied to a scratch directory: `BurntSushi/toml` v1.6.0,
`google/go-cmp` v0.7.0, `google/uuid` v1.3.0, `pelletier/go-toml/v2` v2.4.3.
The full 36-module measurement at the pinned commits is still a follow-up.

## Counts

Counted exports as in the earlier note. Each rule column is the count with
`prev` plus that rule alone.

| Module | Exports | prev | +option | +held | +support | all | Removed by all |
| --- | --- | --- | --- | --- | --- | --- | --- |
| std | 8,216 | 2,247 | 2,246 | 2,142 | 2,178 | 2,065 | 182 (2.2%) |
| this repository | 441 | 12 | 11 | 8 | 9 | 7 | 5 (1.1%) |
| BurntSushi/toml | 68 | 25 | 25 | 25 | 15 | 15 | 10 (14.7%) |
| google/go-cmp | 224 | 109 | 109 | 109 | 103 | 103 | 6 (2.7%) |
| google/uuid | 55 | 4 | 4 | 4 | 4 | 4 | 0 |
| pelletier/go-toml/v2 | 88 | 24 | 24 | 24 | 24 | 24 | 0 |

Share of counted exports each rule removes on its own: on std option 0.01%,
held 1.3%, support 0.8%; on this repository option 0.2%, held 0.9%, support
0.7%.

What they removed here: in `toml`, all ten are `internal/toml-test`, a
vendored harness only toml's tests import (`Test.Run`, `Runner.List`, ...);
in this repository the three `Details` methods and `fake.Detect` of
`metricstest` (the item's example), `snapshot.CrossBlocks` through a held
interface and `WithDupSplitLiteralRuns`. Option funcs are rare in the
standard library and the measured modules, which is why rule a moves little
here; its evidence is the field adjudication (ztp, mark, ai-orchestration),
where it covers `WithLogger`, `WithTempDir` and `WithFlushInterval`.

## Caveats

- The ztp acceptance case. On the current ztp checkout
  (`pkg/recording/protocol/http`, head 6fe1121) every `With*` of
  `orchestrator.go` is a pure setter, `WithFrameFilter` included, so rule a
  excludes all four; the adjudicated head, where `WithFrameFilter` had a
  branch, is not on this host. `orchResponseWriter.Flush` stays counted: the
  interface it satisfies is `net/http.Flusher`, outside the module and not
  on the closed list, and no in-module interface declares `Flush`. Adding
  `http.Flusher` to the closed list would cover it.
- Rule d treats a package nothing in the module imports from non-test code
  and that another package's tests use as test support, including a
  library's own public root package when only an end-to-end test package
  uses it. Its own calls (a function calling another of its exports) then
  mark the callee tested. None of the measured modules shows that case.
