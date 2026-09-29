# `globals` counts mutable state: what the exclusions remove

Date: 2026-09-29. Machine: linux/arm64, 13 cores.
Toolchain: `go version go1.27.1-X:nodwarf5 linux/arm64`.
Targets: the standard library (`std`, the 358 packages outside `vendor/`,
loaded as the collector loads it) and this repository's module (55
packages) at the change. The 36 cloned corpus modules were not measured:
the collector clones them into temporary directories and none is on disk,
and this run made no network access. Measuring them is a follow-up.

## Question

SPEC.md 6 counted every name of a package-level `var` spec. In this
repository's replayed history every new package-level variable is a
sentinel error, ldflags build information or an embedded default
(`astimate-labels-2026-09-28.md`, "What the gate reports that the draft
allows"), so the `globals` rule, `max_delta: 0` with `ratchet_from_zero`,
failed the idiom AGENTS.md prescribes. SPEC.md 6.5 now leaves out three
idioms that hold no state, provided the package never writes them:

- (a) sentinel errors: every name of type `error`, every initializer a call
  to `errors.New` or `fmt.Errorf` with constant arguments;
- (b) `//go:embed` variables;
- (c) build information: boolean, numeric or string variables with a
  constant initializer or none.

How many globals does each remove, and where does the distribution go?

## Method

A throwaway test in `internal/lang/golang/internal/inspect`, not committed,
loaded both targets with the extractor's loader, ran the complexity walk
(which records the writes) and classified every non-blank name of every
package-level var spec in the authored non-test files, in the order the
extractor applies: written by the package (counts), else embed, else
sentinel, else build information, else counts. Names are counted, as the
metric counts them.

## Names removed

| Target | Names before | Written (count) | Other state (count) | (a) sentinel | (b) embed | (c) build info | Names after |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| std | 2304 | 391 | 1491 | 327 | 2 | 93 | 1882 |
| this repository | 23 | 0 | 0 | 19 | 1 | 3 | 0 |

In the standard library the exclusions remove 422 names (18.3%); sentinel
errors are three quarters of them. 108 of 358 packages change. In this
repository every counted global was one of the three idioms: the 19
sentinel errors across `internal/metrics`, `internal/engine`,
`internal/baseline`, `internal/lang/golang`, `internal/report`,
`internal/mcpserver`, `calibration/collect` and `calibration/rebuild`; the
embedded default configuration in `internal/config`; and `buildVersion`,
`buildCommit` and `buildDate` in `cmd/astimate`, set by `-ldflags`.

## Distribution per package

Nearest rank over packages.

| Target | | Sum | Zero | p50 | p75 | p90 | p99 | Max |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| std | before | 2304 | 136 | 1 | 5 | 12 | 83 | 333 |
| std | after | 1882 | 154 | 1 | 4 | 11 | 64 | 312 |
| this repository | before | 23 | 45 | 0 | 0 | 1 | 6 | 6 |
| this repository | after | 0 | 55 | 0 | 0 | 0 | 0 | 0 |

## Consequences

- No refit. The `globals` rule is `max_delta: 0` with no `max` (SPEC.md
  8.2); zero tolerance on new globals is a policy, not a statistic (11.1).
- The rebuild estimate is unchanged: `tokens_per_hidden_state` is 0 in the
  shipped parameters (11.2 clamped it), so `globals` enters the formula at
  zero weight.
- The replay's `globals` findings on this repository disappear, which is
  what the labels asked for.

## What the build-information rule also removes

Read over the 93 standard-library names excluded by (c): tuning constants
held in variables (`math/big` thresholds, `reflect` register counts),
platform flags (`runtime.isIntel`, `os/user.userImplemented`), test hooks
assigned only from `_test.go` files (`crypto/internal/sysrand.testingOnlyFailRead`,
`net/http/httputil.inOurTests`), and variables set by the linker or by
another package through `//go:linkname` (`runtime.iscgo`,
`internal/cpu.HWCap`). The first three are what the rule intends: nothing in
the package's own code changes them. The last kind is state written from
outside the package, which a per-package syntactic scan cannot see; an
exported variable set by an importer is the same case. Both are rare, and
counting them would need a module-wide write scan.
