# Package roles: composition roots judged as such

Date: 2026-10-14. Machine: linux/arm64. Toolchain: Go 1.27.1.
Targets: the five repositories of the 2026-10-13 replays (go-starter, ztp,
shhh, crush, ai-shell) at their HEAD, and the 94 failed commits of those
replays (`calibration/data/replay-*-2026-10-13/`) with the hand labels of
`calibration/notes/hand-labels-2026-10-14.md`. No commit was replayed.

## Question

The gate judged `main` packages like libraries: ztp's `cmd/broker` failed
`internal_imports` 20 -> 21 on a change that removed a fake from its
wiring, and `untested_exports` counted exports of packages nothing can
import. Each package row now carries a role (SPEC.md 6.5): `main`,
`library`, `test-support` (rule d of SPEC.md 6.4) or `generated-only`, and
a threshold may list the `roles` it judges. The default (SPEC.md 8.2)
leaves `main` out of `internal_imports`, `exported_symbols` and
`untested_exports`, `test-support` out of `untested_exports`, requires
tests of a `main` package only past 300 SLOC, and leaves out of the module
rule's added blocks a declarative cross-package block every occurrence of
which is in a `main` package. What share of the failed commits do these
rules change?

## Method

Roles at HEAD come from this change's binary, `astimate rank <repo> --json`
with `GOPROXY=off`. The replay rows record the package directory of each
finding but no role, so each row's role is the role of the package at that
path at HEAD. A path missing at HEAD would be `main` when it is `.` or
under `cmd/`; none of the missing paths was. A finding is removed when the
rule that produced it no longer judges the row's role (for `has_tests` on
`main`, when the row's `sloc` is at most 300); a commit passes when every
finding it had is removed. Each removed finding's cause is the one the
hand notes (`calibration/replay/labels/<name>-hand-notes.md`) give it.
The script is a one-off in the session scratchpad, not committed.

The rows record the module row's cross-package count, not its blocks or
their kinds, so the cross-package exemption cannot be re-evaluated from
them, and the location they record is the first block in the replay
binary's order, a legacy block, not the one the change added. It was
measured instead on each repository at HEAD: every cross-package block of
the module with its kind (SPEC.md 6.3) and the roles of the packages it
touches, from the Go extractor's module details. goKMS and kubesentry,
named by the field reports, were measured the same way, read-only.

## Roles at HEAD

| Repository | Packages | `main` | `test-support` | `generated-only` | `library` |
| --- | --- | --- | --- | --- | --- |
| go-starter | 23 | 2 (`.`, `tools/initialize`) | 0 | 2 (`internal/api/v1`, `internal/platform/database/postgres/db`) | 19 |
| ztp | 48 | 5 (`.`, `cmd/benchmark`, `cmd/broker`, `cmd/compliance-ui`, `cmd/ztctl`) | 2 (`pkg/recording/recordingtest`, `pkg/recording/storage/storagetest`) | 1 (`pkg/api`) | 40 |
| shhh | 82 | 2 (`cmd/shhh`, `scripts/gotest`) | 3 (`internal/testhttp`, `internal/ui/golden`, `internal/hostgit/hostgittest`) | 0 | 77 |
| crush | 75 | 2 (`.`, `internal/ui/logo/example`) | 1 (`internal/agent/agenttest`) | 0 | 72 |
| ai-shell | 3 | 0 | 0 | 0 | 3 |

ai-shell is TypeScript: its `bin` entries name `./dist/cli.mjs`, build
output that is no package, and each of its three packages exports
something, so none is `main`. TypeScript has no `test-support` or
`generated-only` rule.

## The failed commits

| Repository | Failed commits | Lose a finding | Pass |
| --- | --- | --- | --- |
| go-starter | 13 | 0 | 0 |
| ztp | 26 | 1 | 0 |
| shhh | 24 | 0 | 0 |
| crush | 17 | 0 | 0 |
| ai-shell | 14 | 0 | 0 |
| total | 94 | 1 (1.1%) | 0 |

One finding of 287 is removed: ztp fbb05c060, `cmd/broker`
`internal_imports`, cause `over-ceiling-growth`, a commit labelled `allow`.
The commit still fails, on `dup_blocks_cross_pkg` and on
`pkg/recording/protocol/http`'s `untested_exports`. Only five findings of
the failed commits fall on a `main` row at all, all in ztp: that one, two
`dup_blocks`, one `cognitive_p90` and one `max_nesting`, which no role
rule touches. No finding falls on a `test-support` or `generated-only`
row, and no `has_tests` finding falls on a `main` row. No real finding is
removed, and no `block` commit changes.

Ten ztp paths of the failed commits are not packages at HEAD
(`internal/recording/policy`, `internal/web`, and eight under
`pkg/recording/` that were moved or renamed since); none is `.` or under
`cmd/`, so they were judged as libraries. Their roles at the commits
could not be decided from HEAD; by their names and findings they are
libraries.

## The cross-package exemption

| Repository | Cross-package blocks | Every occurrence in `main` | of which declarative | of which at least 80 tokens |
| --- | --- | --- | --- | --- |
| go-starter | 2 | 0 | 0 | 0 |
| ztp | 117 | 5 | 0 | 1 (code, 122 tokens) |
| shhh | 860 | 0 | 0 | 0 |
| crush | 270 | 0 | 0 | 0 |
| goKMS | 32 | 6 | 0 | 1 (idiom, 94 tokens) |
| kubesentry | 4 | 0 | 0 | 0 |

ai-shell has no module row (SPEC.md 13.1). The exemption changes nothing
on these repositories: the blocks the commands share are flag parsing,
logger and server set-up statements, which SPEC.md 6.3 marks code or
idiom, not declarative. goKMS's `cmd/*/main.go` duplicates are idiom (the
import block and the run of set-up calls), so they stay judged; most are
under the 80-token floor already, and the one over it, `cmd/encryptiond`
and `cmd/kms`, 94 tokens, would fail a change that added it.

## Decision

Ship the roles and the defaults as specified. On these replays they change
one finding of 94 failed commits, the `cmd/broker` case that motivated
them, and remove no real finding; ztp's `cmd/broker` checked
`--all --base 130d2c8` against its adjudicated window no longer reports
`internal_imports` (it failed 20 -> 21 before). The cross-package
exemption is kept as specified, declarative blocks only; whether idiom
blocks shared only by composition roots should be exempt too is a separate
question the goKMS blocks raise, and nothing here measures its cost.
