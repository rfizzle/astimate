# Rename seeding: copies, carried cross blocks and importers of a carved-out package

Date: 2026-10-14. Machine: linux/arm64. Toolchain: Go 1.27.1.
Targets: the shhh window (`--all --base bad6327`, head `12b5c97`, the
commit whose `HEAD~30` is `bad6327`), the mark window (`--all --base
9a26f65`, head `25137be`) and shhh `a1d84df` against its parent. Each was
checked in a detached worktree of the sibling repository under the session
scratchpad (removed afterwards) with the embedded default configuration,
once with the binary of master before this change and once with the
binary of this change. No replay, no live run.

## Question

Three false failures the landing check of rename seeding left on shhh:

1. `internal/observe/record` read "new since baseline" with three
   violations, although its files came out of `internal/observe`.
2. On `a1d84df` ("move the side jobs into a sub-package") the moved-block
   exclusion did not clear the cross-package blocks the move created; only
   the 80-token floor did.
3. `internal_imports` rose on packages that newly import
   `internal/agent/sidejobs` without being its parent, although each of
   them imported `internal/agent` before.

## Trace 1: a copy, not a rename

`git diff -M --name-status bad6327 HEAD -- internal/observe` lists
`internal/observe/record/record.go` as `A` and only its test file as a
rename (`R086 eventkinds_test.go`), which does not count. The commit that
made the package (`0d137dc`) moved 700 lines out of `observe.go`, and
`observe.go` kept its path, so git has no deleted file to pair the new one
with: none of the candidates (a rename edited after the move, a directory
move, similarity under 50%) applies. `git diff -M -C` lists it as
`C063 internal/observe/observe.go internal/observe/record/record.go`.
Rename detection now runs `-M -C` and counts a copy as a rename. Git lists
a copy only from a file the same diff modified (no
`--find-copies-harder`), so a copy of a file the change did not touch is
not followed; the rename tests cover both, and a file renamed in one
commit and edited in a later one.

## Trace 2: the blocks a move carried to a third package

Comparing the module's cross blocks at `a1d84df` with those at its parent
by package set: 21 of the 22 new sets were blocks between
`internal/agent/sidejobs` and a third package (`persona`, `todo`,
`ui/chat`, `ui/components`, `cli`, `tools`, `resolve`, `web`) or among
`agent`, `sidejobs` and a third, and the same number of
`internal/agent`-and-third-package sets were gone. The moved exclusion
excuses a block only when every package it touches is in one rename group,
so a legacy block whose `agent` copy moved to `sidejobs` while its other
copy stayed put changed its package set and read as new. The matching of
`score.AddedCrossBlocks` now reads each package of a rename group as the
whole group, on both sides, so `{sidejobs, persona}` matches the
baseline's `{agent, persona}`. `Seeded` also read a source without
recorded anchors as an empty list; a seeded row now records duplicate
blocks and untested exports only when every source does.

| `a1d84df`, floor 1 (`min_tokens_cross: 1`) | Before | After |
| --- | --- | --- |
| Added cross blocks judged | 22 | 1 |

The one left is a 4-line block between `internal/agent` and `internal/lsp`
that the change did create; the default floor of 80 clears it, so the
commit passes the module rule either way at the default.

## Trace 3: importers of the carved-out package

Import diff of the shhh window (`go list`, base against head): `cli`,
`eval`, `observe`, `subagent` and `ui/chat` each add
`internal/agent/sidejobs` and keep `internal/agent`; `rpc` and `storage`
swap `internal/observe` for `internal/observe/record`. In the change that
carves B out of A, a package that imported A at the baseline (the git
baseline's import graph) and imports both A and B at head now has B
counted in its baseline `internal_imports`. One that swapped A for B has
no rise to exempt and gets none; one that imports B without ever having
imported A still counts it (engine test `TestCheckCarvedImporters`).
Against a baseline file, which records no import graph, only the parent's
own edge is exempt.

## Results

"New since baseline" counts the summary rows that say so; `internal_imports`
counts the findings, violations and warnings, whose value rose.

| Window | Rows new since baseline, before | After | Rising `internal_imports` findings, before | After |
| --- | --- | --- | --- | --- |
| shhh `bad6327..12b5c97` | 2 (`internal/calltier`, `internal/observe/record`) | 1 (`internal/calltier`) | 6 (`agent`, `cli` +2, `eval`, `observe` +2, `subagent`, `ui/chat`) | 2 (`agent`, `cli`) |
| mark `9a26f65..25137be` | 3 (`ascii`, `comment`, `confluence/confluencetest`) | 3 | 2 | 2 |

On shhh `internal/observe/record` is now seeded from `internal/observe`
(three violations to none) and `internal/cli` drops its one violation
(59 -> 61 becomes 60 -> 61, within `over_max_delta: 1`). The two rises
left are real: `internal/agent` newly imports `internal/calltier`, a new
package, and `internal/cli` newly imports `internal/receipt`, which it did
not import before. `internal/calltier` is a new package whose files are
all new. mark has no renames or copies in its window, so nothing moved;
its three new rows are packages of new files only.
