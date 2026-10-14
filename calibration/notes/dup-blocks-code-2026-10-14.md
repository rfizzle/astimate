# Gating code blocks: `dup_blocks_code` in place of `dup_blocks`

Date: 2026-10-14. Rows: `calibration/data/replay-<name>-2026-10-14-min60/`
for go-starter, ztp, shhh, crush (Go) and ai-shell (TypeScript), replayed
under `calibration/thresholds/astimate-min-tokens-60-2026-10-14.yaml`.
Labels: the hand labels `calibration/replay/labels/<name>-hand.yaml`
(`shhh-hand-2026-10-14-6dfcbc1.yaml` for shhh) after the relabel of five
ztp placeholders (`calibration/notes/hand-labels-2026-10-14.md`), and the
split-or-extraction labels `<name>-split-extract-2026-10-14-min60.yaml`
second. No replay was run: every number comes from the stored rows.

## Question

`dup_blocks_code` counts the blocks of `dup_blocks` that are code, not
declarative, idiom or self-overlap (SPEC.md 6.3), and every row carries
it. The reading of ztp's remaining failures
(`calibration/notes/ztp-remaining-2026-10-14.md`, F7a) scored gating it in
place of `dup_blocks` as passing three `allow` commits (4c07cb0, 51ef8c3,
e9eac71) and losing one `block` commit caught by accident (0a8b69b, failed
for `recordRequest` overlapping itself while the real copy was rewritten).
Does it raise the pooled J, with the rule inside the 10% false-failure
budget and the gate's recall at or above 80%? And would `duplication_pct`
and `duplicated_lines` counted over code blocks only do the same?

## Form

A `dup_blocks_code` rule, not a `kinds` option on `dup_blocks`. The metric
is already on every row and in `explain_metric`, so a finding names what
it judged (`dup_blocks_code: 1 -> 2`) and `config init` shows it as one
more metric; a `kinds` list would promise kind sets the rows cannot count
(they carry the total and the code count only), and a finding labelled
`dup_blocks` with values of code blocks reads as the wrong count. The rule
merges into `dup_blocks` when both are gated and otherwise carries
`duplication_pct` and `duplicated_lines`, its suggestion cites the first
code block (an added one first), and it is located there; the details keep
blocks of every kind.

## Method

`calibration/candidates/dup-blocks-code-2026-10-14.yaml` is the embedded
default with its `dup_blocks` rule on `dup_blocks_code` instead.
`calibration/validate` now accepts a rule on another metric of the same
kind in a replayed rule's place when no rule gates the replayed metric,
and re-evaluates it on the stored metrics, so the whole configuration is
validated on the rows a `dup_blocks` rule gated:

    go run ./calibration/validate --date 2026-10-14 \
      --config calibration/candidates/dup-blocks-code-2026-10-14.yaml \
      --out calibration/reports/gate-validation-2026-10-14-dup-blocks-code.md \
      --corpus go-starter=calibration/data/replay-go-starter-2026-10-14-min60:calibration/replay/labels/go-starter-hand.yaml,calibration/replay/labels/go-starter-split-extract-2026-10-14-min60.yaml \
      ... (ztp, shhh with shhh-hand-2026-10-14-6dfcbc1.yaml, crush, ai-shell)

The shipped default's numbers are those of
`calibration/reports/gate-validation-2026-10-14.md`, the same command
without `--config`. The view is all corpora pooled with the two
capacity-only `block` labels set aside: 15 `block` and 137 `allow`
agent-authored commits. Recall is the share of `block` commits a rule
fires on, false failures the share of `allow` commits, J their difference.

## Results

| Rule, pooled hand labels, capacity-only set aside | Recall | False failures | J |
| --- | --- | --- | --- |
| `dup_blocks`, `max_delta` 0 (shipped) | 10 of 15 (66.7%) | 8 of 137 (5.8%) | +60.8 pts |
| `dup_blocks_code`, `max_delta` 0 | 8 of 15 (53.3%) | 4 of 137 (2.9%) | +50.4 pts |

| Whole gate | Recall | False failures | J |
| --- | --- | --- | --- |
| shipped (`dup_blocks`) | 13 of 15 (86.7%) | 14 of 137 (10.2%) | +76.4 pts |
| `dup_blocks_code` in its place | 12 of 15 (80.0%) | 11 of 137 (8.0%) | +72.0 pts |
| shipped, split-or-extraction labels | 11 of 11 (100.0%) | 16 of 141 (11.3%) | +88.7 pts |
| `dup_blocks_code`, split-or-extraction labels | 9 of 11 (81.8%) | 14 of 141 (9.9%) | +71.9 pts |

The gate passes three more `allow` commits (ztp 4c07cb0 and 51ef8c3, shhh
6f84361), as the reading predicted for the two ztp ones, and loses ztp
0a8b69b, the catch by accident; e9eac71 still fails, on
`duplication_pct` (41% on 39 SLOC, a new package over the 40% max). The
rule loses two `block` commits; the second, c4af29e (its own new block is
a self-overlapping rules table), stays failed by `dup_blocks_cross_pkg`.
On the labels before the relabel (14 `block`, 138 `allow`) the same
comparison gave +57.8 against +54.2 for the rule and +74.8 against +69.9
for the gate, recall 85.7% against 78.6%.

`duplication_pct` and `duplicated_lines` over code blocks only cannot be
measured from the rows: they store neither code-block lines nor a share,
only the two block counts. Two of the four commits `duplication_pct` fires
on are exact: e9eac71 has no code block (0%, it would pass) and bb794c0's
only block is code (84.2%, unchanged). The two `block` commits it fires on
(f312542, a3a053e) mix kinds, and a commit whose code share rose more than
its whole share cannot be bounded. But the count cannot help while
`dup_blocks` is gated: `duplication_pct` fails no `allow` commit alone
under the shipped gate ("Each rule's part in the gate"), so no recount of
it can lower the gate's false failures. Beside a `dup_blocks_code` rule
it could pass e9eac71 too, at best 10 of 137 (7.3%) with recall 80.0%,
J +72.7, still under the shipped +76.4.

## Decision

Not shipped. The rule's J falls by 10.4 points and the gate's by 4.4,
and gate recall falls to exactly the 80% floor, so none of the three
criteria a change ships on (J rises, inside the budget, recall at or
above 80%) favours it although the rule is well inside the budget. The
default keeps `dup_blocks`, blocks of every kind; the `dup_blocks_code`
rule is available to a configuration that prefers fewer false failures
(8.0% against 10.2%, inside the gate's 10% target) to recall. Code-block
`duplication_pct` and `duplicated_lines` are recorded, not shipped; a
replay that stores code-block lines would measure them.

What the code-block gate removes is the self-overlap and accessor
scaffolding the reading named; what it loses is a catch that rested on a
self-overlap. The difference is one `block` label in fifteen, so the next
relabel or replay (the 2026-10-14 re-replay of the five repositories)
should re-run the command above before the question is closed.
