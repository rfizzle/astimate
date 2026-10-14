# The cross-package floor: `min_tokens_cross` at 60, 80, 100 and 120

Date: 2026-10-14. Rows: `calibration/data/replay-<name>-2026-10-14-min60/`
for go-starter, ztp, shhh, crush (Go) and ai-shell (TypeScript), replayed
again under `calibration/thresholds/astimate-min-tokens-60-2026-10-14.yaml`
so their module rows carry `cross_added`. Labels: the hand labels
`calibration/replay/labels/<name>-hand.yaml` (`shhh-hand-2026-10-14-6dfcbc1.yaml`
for shhh) as relabelled on 2026-10-14, and the split-or-extraction labels
`<name>-split-extract-2026-10-14-min60.yaml` second.

## Question

`dup_blocks_cross_pkg` judges the baseline count plus the cross-package
blocks a change added of at least `duplication.min_tokens_cross` tokens
(SPEC.md 8.1). The rows recorded only the counts, so the floor could not be
swept from them: `calibration/validate` read the module row's full count,
its recheck disagreed with the recorded verdicts on 11 of 3,863 (row, rule)
pairs on the 60-token rows (23 of 4,898 on the 40-token rows), and the floor
was parked three times. The ztp reading
(`calibration/notes/ztp-remaining-2026-10-14.md`, F4) scored 120 as passing
four `allow` commits there and losing no catch, while shhh fe415c4, the
paste the gate misses, was thought to sit under 80. Which floor has the best
pooled J, with the rule inside the 10% false-failure budget and the gate's
recall at or above 80%?

## What the rows now record

Each module row records `cross_added` (SPEC.md 11.3,
`calibration/replay/README.md`): one entry per set of packages, as the rule
matches blocks (each package a rename joined read as its group), on which
some floor adds a block, with the token lengths of the parent's blocks on
the set and the commit's blocks on it that are neither exempt nor moved,
each with its length, kind, packages and every occurrence's file and lines.
Per set the rule adds the commit's blocks of at least the floor beyond the
parent's blocks that clear the same floor, so the record re-derives the
rule exactly at any floor at or above the replay's `min_tokens` (60): the
count is not monotone in the floor (ztp 5015ee3 adds 6 blocks at 80 and 7
at 100, where a 97-token legacy block stops matching a 137-token one).
`calibration/validate` re-derives the rule from it at `--config`'s floor,
and accepts rows replayed at another floor when they carry the field.

## Replays

Built from 0d71c22 with the row change (the binary reports
`v0.4.1-6-g0d71c22-dirty`), one repository at a time from a scratch
directory, `TMPDIR` off `/tmp`:

    TMPDIR=<cache dir> GOPROXY=off astimate dryrun <repo> --last 60 --timeout 0 \
      --config calibration/thresholds/astimate-min-tokens-60-2026-10-14.yaml \
      --out calibration/data/replay-<name>-2026-10-14-min60

| Repository | Head | Loaded | Failed | Wall time |
| --- | --- | --- | --- | --- |
| go-starter | b2f5f20 | 60 | 8 | 293 s |
| ztp | 6fe1121 | 58 | 28 | 75 s |
| shhh | 6dfcbc1 | 60 | 19 | 1,010 s |
| crush | 6d14dd9 | 60 | 17 | 423 s |
| ai-shell | 8e4a29b | 60 | 15 | 4 s |

Every head is where the previous min60 replays stopped, so all 60 hand labels
of each repository join by hash (shhh's 60 through
`shhh-hand-2026-10-14-6dfcbc1.yaml`; `shhh-hand-2026-10-14.yaml` is for the
range before its head moved). Compared with the committed rows, every
metric of every row is the same; the only other change is ztp 130d2c8, whose
six packages that failed to load at the parent are now marked
`"baseline": "unavailable"` and judged by their maxes only (the gate change
that landed after the first replay), which drops three of its seven
violations. The split-or-extraction labels read only the unchanged counts,
so they were not re-derived. ai-shell has no module row.

## Method

`calibration/validate` now sets aside the package rows marked
`"baseline": "unavailable"` with the violations recorded on them (6 rows, 1
violation, all ztp 130d2c8) and judges a commit by its other rows. That alone
moves three rules on the pooled view: `dup_blocks` 8 to 7 `allow` commits,
`globals` 5 to 4, `internal_imports` 1 to 0; the gate is unchanged.

Each floor is validated as the configuration it would ship: the default at
60, 80 and 100 as committed configurations, and the shipped default at 120.

    go run ./calibration/validate --date 2026-10-14 --config <file> --out <report> \
      --corpus go-starter=calibration/data/replay-go-starter-2026-10-14-min60:calibration/replay/labels/go-starter-hand.yaml,calibration/replay/labels/go-starter-split-extract-2026-10-14-min60.yaml \
      ... (ztp, shhh with shhh-hand-2026-10-14-6dfcbc1.yaml, crush, ai-shell)

| Floor | Configuration | Report |
| --- | --- | --- |
| 60 | `calibration/candidates/cross-floor-60-2026-10-14.yaml` | `calibration/reports/gate-validation-2026-10-14-cross-floor-60.md` |
| 80 | `calibration/thresholds/astimate-thresholds-2026-10-14.yaml` | `calibration/reports/gate-validation-2026-10-14-cross-floor-80.md` |
| 100 | `calibration/candidates/cross-floor-100-2026-10-14.yaml` | `calibration/reports/gate-validation-2026-10-14-cross-floor-100.md` |
| 120 | the embedded default, `thresholds-2026-10-14b` | `calibration/reports/gate-validation-2026-10-14.md` |

The view is all corpora pooled with the two capacity-only `block` labels set
aside: 15 `block` and 137 `allow` agent-authored commits. Recall is the share
of `block` commits failed, false failures the share of `allow` commits, J
their difference.

## Results

Recheck: re-evaluating every rule at its shipped limits on the rows now
reproduces every recorded verdict. At the replay's own floor (80) 0 of 3,800
(row, rule) pairs disagree, the cross rule included; at 60, 100 and 120,
where the cross rule is re-derived at another floor and so left out of the
recheck, 0 of 3,691.

| Floor | Rule recall | Rule false failures | Rule J | Gate recall | Gate false failures | Gate J |
| --- | --- | --- | --- | --- | --- | --- |
| 60 | 10 of 15 (66.7%) | 12 of 137 (8.8%) | +57.9 pts | 13 of 15 (86.7%) | 17 of 137 (12.4%) | +74.3 pts |
| 80 (was shipped) | 7 of 15 (46.7%) | 7 of 137 (5.1%) | +41.6 pts | 13 of 15 (86.7%) | 14 of 137 (10.2%) | +76.4 pts |
| 100 | 6 of 15 (40.0%) | 4 of 137 (2.9%) | +37.1 pts | 13 of 15 (86.7%) | 11 of 137 (8.0%) | +78.6 pts |
| 120 (shipped) | 4 of 15 (26.7%) | 1 of 137 (0.7%) | +25.9 pts | 13 of 15 (86.7%) | 8 of 137 (5.8%) | +80.8 pts |

| Floor | Split-or-extraction labels: gate recall, false failures | Every author: gate recall, false failures | Cross rule alone: `block`, `allow` |
| --- | --- | --- | --- |
| 60 | 11 of 11 (100.0%), 19 of 141 (13.5%) | | 1, 9 |
| 80 | 11 of 11 (100.0%), 16 of 141 (11.3%) | 18 of 21 (85.7%), 38 of 275 (13.8%) | 1, 6 |
| 100 | 11 of 11 (100.0%), 13 of 141 (9.2%) | | 1, 3 |
| 120 | 11 of 11 (100.0%), 10 of 141 (7.1%) | 18 of 21 (85.7%), 32 of 275 (11.6%) | 1, 0 |

"Cross rule alone" counts the commits no other failing rule fails (the
validation report's "Each rule's part in the gate"): at every floor the rule
alone catches one `block` commit, ztp 6b9cbe6 (a 204-token copy of the SSH
peeker), so gate recall never moves.

What moves, read from `cross_added`:

- 60 over 80: the rule adds three `block` commits, ztp 0a8b69b (three
  constructor blocks of 68 to 74 tokens), ztp 9fbbe74 and shhh 13e5c70, each
  already failed by `dup_blocks` or a capacity rule, and five `allow`
  commits: ztp e9eac71 and bb794c0 (blocks of 63 and 75 tokens), which
  `dup_blocks` fails anyway, and three the gate did not fail before, ztp
  2579fee (constructor scaffolding) and 33851d1, and shhh fdb5f71. shhh fe415c4,
  the 14-line paste between `internal/pricing` and `internal/update`, records
  no cross-package block even at 60: its repeat is under the 60-token
  `min_tokens`, so no floor of this rule catches it.
- 120 over 80: the gate passes six `allow` commits that only the cross rule
  failed: ztp 8fa49ec (83 tokens) and 7aa1539 (110), the functional-options
  scaffolds; 878d5f0, where two struct fields split a 285-token block into
  107 and 129 tokens on the same pair of packages (at 120 the 129-token half
  matches the 285-token parent block); c0373bb, where an import alias grew
  an existing block to 107 tokens; 130d2c8 (blocks of 67 to 87 tokens around
  a de-duplication); shhh dfee87d. The rule stops firing on three `block`
  commits, ztp f312542 (blocks of 89 and 105 tokens) and a3a053e (90 to 116)
  and shhh ff3ef76, all three still failed by `dup_blocks`, `max_nesting` or
  a capacity rule. Its one remaining `allow` commit, ztp bb7f0cd, fails
  `max_nesting` as well.
- 100 sits between: it passes 8fa49ec, 130d2c8 and dfee87d and keeps 7aa1539,
  c0373bb and 878d5f0.

## Content matching

The ztp reading named three ways a count-based match invents an added block.
With `cross_added` each can be read off the rows:

| Commit | Artifact | Added at 80 | At 120 | Would content matching remove it |
| --- | --- | --- | --- | --- |
| 878d5f0 | two struct fields split a 285-token block into 107 + 129 | 1 | 0 | yes: the halves keep the parent block's first and last tokens |
| c0373bb | an import alias grows a 101-token block to 107, into the import list | 1 | 0 | yes: its last tokens are unchanged |
| bb7f0cd | deleting `directtcpip/logger.go` leaves a 154-token block on two packages instead of three | 1 | 1 | yes: same content, a smaller package set |

Matching an added block by content (the anchors `duptok` already hashes for
`dup_blocks`, the first and last `min_tokens` tokens of a block) rather than
by count per package set would remove all three. At 120 the floor already
removes the first two, and bb7f0cd also fails `max_nesting`, so content
matching would pass no further `allow` commit on these labels: the rule's
own false failures would fall from 1 to 0 (rule J +25.9 to +26.7) and the
gate's not at all. It is not built: cross-package blocks carry no anchors
today (`metrics.CrossBlock` has kind, length and occurrences;
`duptok.Block` for the module stream has none), so it needs anchors from
both extractors' module streams, carried through the baseline, and a match
in `score.AddedCrossBlocks`, well over the 100 lines the item allows, and a
copy that extends a legacy shared block keeps one of its anchors and would
read as old. Parked with these numbers.

## Decision

**`min_tokens_cross` 120, shipped as `config_version: thresholds-2026-10-14b`.**

- The gate's J is best at 120 (+80.8 against +76.4 at 80, +78.6 at 100 and
  +74.3 at 60), its recall is 86.7% at every floor, over the 80% target, and
  its false failures fall from 10.2% to 5.8%, inside the 10% target the gate
  missed by one commit. The split-or-extraction labels agree (11.3% to 7.1%,
  recall 100% kept), and so does the every-author view (13.8% to 11.6%).
- The rule stays inside its budget at every floor (0.7% at 120).
- The rule's own J falls as the floor rises (+41.6 at 80, +25.9 at 120)
  because it stops firing on three `block` commits that other rules fail;
  the one `block` commit only it catches stays caught at every floor. The
  thresholds report's first criterion reads the rule's J; on that reading
  the best J is 60 (+57.9), a tightening that adds three gate false failures
  (12.4%) for no gate recall, which its fourth criterion rules out while the
  gate is over its target. The floor is a duplication setting that decides
  which commits the gate fails, so it is judged by the gate's J here, as
  the four warn demotions were judged by the gate's recall: every `allow`
  commit 120 passes was failed by this rule alone.

`config_version` moves to `thresholds-2026-10-14b`: the thresholds report
names one version per retune and does not cover a later single-value
change, so the new value gets its own name, and
`calibration/thresholds/astimate-thresholds-2026-10-14b.yaml` beside the
2026-10-14 file. Rows replayed under `thresholds-2026-10-14` stay
validatable with that file.

## Code blocks, re-run

The `dup_blocks_code` comparison of `calibration/notes/dup-blocks-code-2026-10-14.md`
was re-run on these rows (its own note has the numbers): the decision there
stands.
