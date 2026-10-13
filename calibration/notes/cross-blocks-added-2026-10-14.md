# Cross-package blocks: judging the blocks a change added, over a size floor

Date: 2026-10-14. Machine: linux/arm64. Toolchain: Go 1.27.1.
Targets: a **sample** of 20 of the 64 replay commits of 2026-10-13 that
failed by `dup_blocks` or `dup_blocks_cross_pkg`
(`calibration/notes/dup-block-kinds-2026-10-14.md`), drawn from the 37 of
them whose module row failed `dup_blocks_cross_pkg`, since only that rule
moves with the floor. This is not the full replay.

## Question

The module row failed any rise of the cross-package count, which decided
21 of ztp's 26 failed replay commits and 10 of shhh's 24. SPEC.md 8.1 now
judges the baseline's count plus the blocks the change *added*: new by the
set of packages they touch, at least `duplication.min_tokens_cross`
normalized tokens long, and not moved by a rename. Which floor separates
the hand labels best: 40 (the `min_tokens` minimum, so no floor), 60, 80 or
120?

## Method

The replay rows record only the module row's count, not its blocks, so
the sweep re-runs `check`. Each sampled commit was checked out in a
detached worktree of its sibling repository under the session scratchpad
(removed afterwards) and checked with the binary of this change,
`astimate check <worktree> --base <parent> --config <cfg> --format json`
with `GOPROXY=off`, once per floor, the configuration being the embedded
default with only `min_tokens_cross` changed (so every other rule is
today's default, not the 2026-10-13 replay's configuration). Floor 0 is
the added-block rule with no floor; at `min_tokens: 40` it equals floor
40. Labels are the hand labels of
`calibration/replay/labels/<name>-hand.yaml`. J is the share of `block`
commits failing minus the share of `allow` commits failing.

The sample, 10 `block` and 10 `allow`:

| Replay | Commits |
| --- | --- |
| ztp | `ac00641` allow, `8fa49ec` allow, `e9eac71` allow, `0a8b69b` block, `f312542` block, `6b9cbe6` block, `f430f2f` block, `2579fee` allow, `3feff9a` allow, `9fbbe74` block |
| shhh | `fe415c4` block, `ff3ef76` block, `0b74b67` allow, `a1d84df` allow, `13e5c70` block |
| crush | `b82d7d0` block, `804324f` allow, `ec2fe43` block, `25bf6a2` allow |
| go-starter | `96d2cff` allow |

The sample over-weights `block` (10 of 20, against 16 of the 37
cross-failed commits), so the rates below are of the sample and J is not
the replay's J.

## Results

| Floor | `dup_blocks_cross_pkg` fires: block / allow | J (cross rule) | Gate fails: block / allow | J (gate) |
| --- | --- | --- | --- | --- |
| count rule (as replayed) | 10/10, 10/10 | 0.0 | 10/10, 10/10 | 0.0 |
| none (0) | 10/10, 10/10 | 0.0 | 10/10, 10/10 | 0.0 |
| 40 | 10/10, 10/10 | 0.0 | 10/10, 10/10 | 0.0 |
| 60 | 8/10, 7/10 | 0.1 | 9/10, 10/10 | -0.1 |
| **80** | **5/10, 2/10** | **0.3** | **9/10, 8/10** | **0.1** |
| 120 | 2/10, 0/10 | 0.2 | 9/10, 7/10 | 0.2 |

At 80 the cross rule stops failing 8 of the 10 `allow` commits and keeps
failing 5 of the 10 `block` ones: ztp `f312542`, `6b9cbe6`
(`internal/protocol/ssh/channel_relay.go:494-544` at 120), `f430f2f`
(`pkg/recording/http/handler.go:294-312`), shhh `ff3ef76`
(`internal/lsp/manager.go:253-269`) and crush `b82d7d0`
(`internal/cmd/run.go:564-580`). Of the `allow` commits it still fails ztp
`8fa49ec` (`internal/recording/filter/filter.go:45-60`) and crush `25bf6a2`
(`internal/ui/dialog/sessions.go:112-131`). Most failed commits have other
findings, so the gate's J moves less than the rule's: at 80, 8 `allow`
commits still fail the gate, 7 of them on other rules. 120 lets ztp
`f312542`, shhh `ff3ef76` and crush `b82d7d0` through the cross rule,
three `block` commits it caught at 80, to spare two more `allow` commits,
one of which another rule fails anyway; 60 barely moves from no floor. The default is 80,
the best J of the cross rule on the sample.

## Moved blocks

The moved exclusion did not change any sampled outcome at floors 0 to 40:
shhh `a1d84df` ("move the side jobs into a sub-package") still adds 22
blocks at no floor and none at 80, so its new blocks do not lie within
one rename group (a rename seeds only a package without a row of its own;
which case applies here was not traced), and the floor is what clears it. The split case is covered by
`TestCheckSplitIsNotAddedCrossBlock` in `internal/engine`.

## Limits

20 commits, chosen by hand to cover all four Go replays and both labels,
not at random; a full re-measure needs the module rows' blocks recorded by
the replay, which S-196's `dup.Cross` details could supply.
