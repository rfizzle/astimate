# Duplicate block kinds: what declarative, idiom and self-overlap blocks account for

Date: 2026-10-14. Machine: linux/arm64.
Toolchain: Go 1.27.1.
Targets: the standard library (`std`, 374 packages that parsed), this
repository at the change (60 packages), and the five repositories of the
2026-10-13 replays at their HEAD: go-starter `b2f5f20`, ztp `6fe1121`,
crush `6d14dd9` and ai-shell `8e4a29b` (each the replay's last commit) and
shhh `73d0c97` (past the replay's last commit `12b5c97`). For the failed
commits, every one of the 64 replay commits failed by `dup_blocks` or
`dup_blocks_cross_pkg`, at the commit and at its parent (105 distinct
trees), not a sample.

## Question

After the mechanics fixes, `dup_blocks` and `dup_blocks_cross_pkg` still
decide most per-commit failures of the replays, and the field adjudication
read many of the blocks as boilerplate: key maps and style structs, flag
rows, cobra preambles and `main.go` scaffolding, `if err != nil` chains,
`WithX` builders, call runs matching themselves a line down. SPEC.md 6.3 now
marks each block by what it is made of: declarative, idiom, self-overlap or
code, from a shape per token (6.5), and reports `dup_blocks_code`, the code
blocks of `dup_blocks`. How many blocks and how many failed commits does
each kind account for, and should the gate judge `dup_blocks_code`?

## Method

`TestMeasureBlockKinds` in `internal/lang/golang/internal/dup` (and its
TypeScript twin in `internal/lang/typescript`) runs only with
`ASTIMATE_DUPKINDS_DIR` and `ASTIMATE_DUPKINDS_OUT` set. The Go one loads
the module with syntax only, which is all the duplication pass reads, runs
`dup.Package` on every package and the cross-package pass on the module,
and writes every block with its kind and anchors; the TypeScript one runs
the extractor. Each commit was measured in a detached worktree of the
sibling repository under the session scratchpad, removed afterwards. Every
`dup_blocks` of a violated package and every module row's
`dup_blocks_cross_pkg` measured this way equals the replay's recorded value
(0 mismatches over the 105 trees), so the kinds are those of the blocks the
gate counted.

A commit's `dup_blocks` finding on a package is explained by a set of kinds
when the count of blocks of the other kinds did not rise from the parent;
the module row's `dup_blocks_cross_pkg` finding likewise over the
cross-package blocks. Labels are the hand labels of
`calibration/replay/labels/<name>-hand.yaml` (S-198's drafts, 23 `block`
of 94 failed commits).

## Blocks by kind

| Target | Blocks | Declarative | Idiom | Self-overlap | Code | Packages with a block | of them with no code block | Cross blocks (decl / idiom / self / code) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| std | 4,966 | 575 (11.6%) | 380 (7.7%) | 1,054 (21.2%) | 2,957 (59.5%) | 257 | 51 | n/a |
| this repository | 73 | 12 | 8 | 14 | 39 (53%) | 17 | 4 | 66: 11 / 17 / 1 / 37 |
| go-starter | 48 | 12 | 15 | 10 | 11 (23%) | 12 | 7 | 2: 1 / 0 / 0 / 1 |
| ztp | 141 | 14 | 19 | 29 | 79 (56%) | 28 | 5 | 117: 7 / 7 / 0 / 103 |
| shhh | 2,246 | 546 | 160 | 216 | 1,324 (59%) | 59 | 8 | 860: 220 / 30 / 10 / 600 |
| crush | 1,407 | 145 | 164 | 181 | 917 (65%) | 45 | 5 | 270: 78 / 27 / 6 / 159 |
| ai-shell | 11 | 5 | 1 | 2 | 3 (27%) | 3 | 2 | n/a |

About two blocks in five are not code, in std and in the field repositories
alike; the share is highest in the small go-starter (scaffolding) and
lowest in crush.

## Failed commits by kind

Each new block a failed commit added (by anchors, package and cross blocks
together) has a kind; a commit counts once per kind it added.

| Replay | Commits failed by a dup rule | Added code | Added declarative | Added idiom | Added self-overlap | Added only declarative or idiom |
| --- | --- | --- | --- | --- | --- | --- |
| go-starter | 6 | 5 | 4 | 4 | 1 | 1 |
| ztp | 26 | 23 | 5 | 6 | 13 | 1 |
| shhh | 19 | 16 | 9 | 5 | 6 | 2 |
| crush | 11 | 9 | 4 | 5 | 3 | 1 |
| ai-shell | 2 | 0 | 1 | 1 | 0 | 2 |
| Total | 64 | 53 | 23 | 21 | 23 | 7 |

The gate's view, by counts (which also absorbs anchor churn, a block whose
end moved), and the share of each replay's failed commits:

| Replay | Failed commits | Dup findings only declarative or idiom | Share of failed | Of them passing with no other finding | Dup findings only declarative, idiom or self-overlap | Of them passing |
| --- | --- | --- | --- | --- | --- | --- |
| go-starter | 13 | 2 | 15% | 2 | 2 | 2 |
| ztp | 26 | 3 | 12% | 2 | 4 | 2 |
| shhh | 24 | 2 | 8% | 1 | 3 | 2 |
| crush | 17 | 1 | 6% | 0 | 3 | 0 |
| ai-shell | 14 | 2 | 14% | 0 | 2 | 0 |
| Total | 94 | 10 | 11% | 5 | 14 | 6 |

Every one of the 10 commits whose dup findings were only declarative or
idiom is labelled `allow`. Of the four that self-overlap adds, one is
`block`: shhh `0198952` pastes seven lines of a heading builder a second
time a few lines down in the same function
(`internal/ui/components/inspectorprocesses.go:71-81` and `78-88`), a real
copy whose two occurrences share lines.

## What `dup_blocks_code` at `max_delta 0` would fail

With the hand labels, before and after replacing `dup_blocks` by
`dup_blocks_code` and judging `dup_blocks_cross_pkg` on the code
cross-package blocks only (all other rules unchanged):

| Rule set | `allow` commits failing | `block` commits failing | Dup findings on `allow` | Dup findings on `block` |
| --- | --- | --- | --- | --- |
| As shipped (`dup_blocks`) | 71 | 23 | 43 | 21 |
| Declarative and idiom exempt (self-overlap counted) | 66 | 23 | 33 | 21 |
| `dup_blocks_code` (self-overlap exempt too) | 66 | 22 | 30 | 20 |
| `dup_blocks_code` on packages, cross rule unchanged | 68 | 22 | 34 | 20 |

## Sample read

Twenty of the commits were read at the cited blocks (go-starter `74f7726`,
`f818863`, `ef5b99f`, `96d2cff`; ztp `8fa49ec`, `e9eac71`, `bb794c0`,
`33851d1`, `0a8b69b`; shhh `e3ca575`, `6f84361`, `fe415c4`, `0198952`,
`228d31a`; crush `ed15da4`, `90bc466`, `8661a1c`, `6abd61e`; ai-shell
`064db6e`, `6fac82b`). The kinds read as named where they apply: crush's
command allow-list rows (`internal/agent/tools/safe.go:88-112`), shhh's
inspector struct literal of section calls (`internal/ui/chat/inspector.go:219-231`)
and go-starter's `const` block and dependency struct (`cmd/migrate.go:22-31`)
are declarative; go-starter's cobra command preamble (`cmd/migrate.go:30-45`
against `cmd/seed.go:23-40`) and ai-shell's runs of awaited calls and
`console.log` (`src/prompt.ts:113-141`) are idiom; ztp's option funcs
(`internal/broker/options.go:22-50`, each `return func(s *T) { s.x = v }`)
are self-overlap. The
boilerplate the adjudication called false is mostly still code, because
the definitions are narrow: a module preamble of imports, an `Option` type,
a `WithLogger` option func (`return func(...)`, a return statement) and a
constructor looping over options (ztp `internal/recording/filter/filter.go:1-31`
against `pty/asciicast_writer.go:1-30`); `mu.Lock(); defer mu.Unlock(); if
f.closed { return ... }` (a `defer` and a guard that is not `!= nil`);
repository methods whose error branch nests `if errors.Is(err, ...)`
(go-starter `internal/modules/user/repository.go:52-77`, two statements in
the `if`); and crush's dialog `Update` default branch and `Draw` header,
repeated in five dialogs (`internal/ui/dialog/notifications.go:155-182`),
which is real copied logic.

## Recommendation for S-146

Keep the gate on `dup_blocks`; report `dup_blocks_code` beside it and do
not move the rule yet. The kinds are right where they apply, but they
account for little of the problem: declarative and idiom blocks alone
explain the dup findings of 10 of 94 failed commits (11%; 6% to 15% per
replay) and would let 5 `allow` commits pass, without losing a `block`
commit; the false failures from `dup_blocks` drop from 43 to 33 `allow`
commits. Exempting self-overlap too, as `dup_blocks_code` does, clears the dup
findings of 3 more `allow` commits (33 to 30) but lets no further `allow`
commit pass, and it lets a real `block` pass (shhh `0198952`), so if
S-146 or S-158 does move the rule, it should judge a count that keeps
self-overlap blocks in (declarative and idiom exempt), not
`dup_blocks_code` as defined. The larger share of false dup failures is
boilerplate the narrow shapes call code (option funcs, constructors,
mutex-and-guard preambles, nested error branches), which `min_tokens`
(S-158) and partial-overlap coalescing (S-180) address better than a wider
idiom list; widening the idioms to returns of literals, `defer` and any
guard clause is the next variant to measure if those fall short.
