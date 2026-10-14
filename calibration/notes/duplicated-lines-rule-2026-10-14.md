# A duplicated-lines rule

Date: 2026-10-14. Data: `calibration/data/replay-<name>-2026-10-14/` for
go-starter, ztp, shhh and crush (Go) and ai-shell (TypeScript). Labels: the
hand labels `calibration/replay/labels/<name>-hand.yaml` (for shhh,
`shhh-hand-2026-10-14.yaml`) and the split-or-extraction labels
`<name>-split-extract-2026-10-14.yaml`. Candidate:
`calibration/candidates/duplicated-lines-2026-10-14.yaml`, the embedded
default with one rule added. Gate report for the same replays:
`calibration/reports/gate-validation-2026-10-14-replays.md`.

## Question

The 2026-10-13 adjudication found a copy the gate passed: kubesentry's
`internal/output` copied its Text and CSV methods a third time while
`dup_blocks` fell 6 to 5 (two blocks merged into one longer one) and
`duplication_pct` fell 83.3 to 73.7 (the package grew). `duplicated_lines`,
the SLOC a duplicate block covers, rose. Does a density rule on it, with
`max_delta` 0 (no new duplicated line) and a `floor` of 0, 10 or 20 lines,
raise the gate's J on the hand labels, and does it fail the kubesentry
change? The rule ships in the default only if both hold.

## Replays

The 2026-10-13 replays have no `duplicated_lines`, so each repository was
replayed again for its last 60 first-parent commits with `astimate dryrun
--out` at bd13bc3 under the embedded default, one at a time with `TMPDIR`
off `/tmp`, and labelled with `go run ./calibration/replay/label --rule
split-extract`.

| Repository | Loaded | Failed | Hand labels that join | Split-or-extraction `block` |
| --- | --- | --- | --- | --- |
| go-starter | 60 | 10 | 60 of 60 | 4 |
| ztp | 58 | 29 | 60 of 60 | 6 |
| shhh | 60 | 20 | 59 of 60 | 4 |
| crush | 60 | 17 | 60 of 60 | 3 |
| ai-shell | 60 | 14 | 60 of 60 | 2 |

shhh's head moved by one commit after 2026-10-13, so its range lost
c470d9e (a sampled pass, allow) and gained 73d0c97, a test clean-up the gate
passed. `shhh-hand-2026-10-14.yaml` is the 59 hand labels that join and
73d0c97 as allow by assumption (provenance proposed), as the hand labels
treat every passed commit outside their sample. The other four ranges are
unchanged, so their hand labels apply as they are. The labels' agent rule
drops crush's and ai-shell's commits by other authors, as in
`calibration/reports/gate-validation-2026-10-14-hand.md`.

## Measurement

`calibration/validate` gained `--candidate <file>` (SPEC.md 11.4): the
top-level rules of that configuration on metrics `--config` does not gate are
re-evaluated on every row they would judge and scored beside the gate as
replayed, without becoming part of it. Floors 0 and 20 are the same file with
the floor changed. Commands, hand labels first, then split-or-extraction first:

    go run ./calibration/validate --candidate calibration/candidates/duplicated-lines-2026-10-14.yaml \
      --corpus go-starter=calibration/data/replay-go-starter-2026-10-14:calibration/replay/labels/go-starter-hand.yaml,calibration/replay/labels/go-starter-split-extract-2026-10-14.yaml \
      ... (ztp, shhh with shhh-hand-2026-10-14.yaml, crush, ai-shell)

The configuration is the embedded default, the one the replays ran under.

## Results on the hand labels, pooled

All five corpora pooled, 16 `block` and 140 `allow` agent commits; and the
same pool with the two capacity-only `block` labels set aside, the view the
validation's sweeps and advice use. Rule columns are the rule alone; gate
columns are the gate as replayed, with the rule added where a floor is named.

| View | Setting | Rule recall | Rule false failures | Gate recall | Gate false failures | Gate J |
| --- | --- | --- | --- | --- | --- | --- |
| pooled | without the rule | | | 15 of 16 (93.8%) | 39 of 140 (27.9%) | +65.9 pts |
| pooled | floor 0 | | | 15 of 16 (93.8%) | 41 of 140 (29.3%) | +64.5 pts |
| pooled | floor 10 | | | 15 of 16 (93.8%) | 41 of 140 (29.3%) | +64.5 pts |
| pooled | floor 20 | | | 15 of 16 (93.8%) | 41 of 140 (29.3%) | +64.5 pts |
| pooled, capacity-only set aside | without the rule | | | 13 of 14 (92.9%) | 39 of 140 (27.9%) | +65.0 pts |
| pooled, capacity-only set aside | floor 0 | 78.6% | 12.9% | 13 of 14 (92.9%) | 41 of 140 (29.3%) | +63.6 pts |
| pooled, capacity-only set aside | floor 10 | 78.6% | 12.9% | 13 of 14 (92.9%) | 41 of 140 (29.3%) | +63.6 pts |
| pooled, capacity-only set aside | floor 20 | 78.6% | 12.1% | 13 of 14 (92.9%) | 41 of 140 (29.3%) | +63.6 pts |

At every floor the rule lowers the pooled J by 1.4 points: it adds no
`block` commit and fails two more `allow` commits. Per corpus at floor 10,
ztp goes from 20 to 21 of 49 `allow` commits failed and shhh from 14 to 15 of
53; go-starter (3 of 36) and crush (2 of 2) do not move, and ai-shell has no
agent commit. The rule alone fires on 11 of the 14 `block` commits, every
one of them already failed by another rule, mostly `dup_blocks`.

The two false failures are small additions to packages far past any floor:

- ztp 2910d8d ("Fix TCP proxy producing session.har"),
  `pkg/recording/protocol/kubernetes` 36 to 40 duplicated lines, the
  duplicated share flat at 81.8% to 81.6%; the gate passed it.
- shhh 9c2bea4 ("carry built-in skills for the quality gate"),
  `internal/cli` 4,210 to 4,212 duplicated lines in a package of over 400
  blocks; the gate passed it.

A floor cannot spare them, since both packages start well over 20 lines.
The `max_delta` sweep can: at 10 (ten duplicated lines a change) the rule
alone fails 64.3% of `block` and 5.7% of `allow`, and the gate stays at 92.9%
and 27.9%, which adds nothing either. The one `block` commit the gate misses,
shhh fe415c4, pastes a temp-and-rename write from `internal/pricing` into
`internal/update`: a copy between packages, which no package's
`duplicated_lines` sees.

## Results on the split-or-extraction labels, pooled

With those labels first every commit is kept (they carry no agent field):
19 `block` and 279 `allow` commits.

| Setting | Rule recall | Rule false failures | Gate recall | Gate false failures | Gate J |
| --- | --- | --- | --- | --- | --- |
| without the rule | | | 17 of 19 (89.5%) | 73 of 279 (26.2%) | +63.3 pts |
| floor 0 | 73.7% | 12.5% | 17 of 19 (89.5%) | 76 of 279 (27.2%) | +62.2 pts |
| floor 10 | 73.7% | 12.5% | 17 of 19 (89.5%) | 76 of 279 (27.2%) | +62.2 pts |
| floor 20 | 73.7% | 12.2% | 17 of 19 (89.5%) | 76 of 279 (27.2%) | +62.2 pts |

The same picture: no `block` commit added, three `allow` commits more, J
down 1.1 points at each floor. With the hand labels as the second set on the
same 298 commits, 21 of 22 and 69 of 276 without the rule against 21 of 22 and
72 of 276 with it at floor 10.

## The kubesentry case

Checked with the binary built from this change, under the candidate at each
floor:

    astimate check /Users/copierson/Projects/kubesentry --all \
      --base af04825184884f41200a0e16cc528a0e2bb395f2 \
      --config <candidate with floor 0, 10 or 20>

At floors 0, 10 and 20 alike, `internal/output` fails the rule:

    duplicated_lines: 135 -> 157, max_delta +0. 157 source lines are duplicated.
    5 duplicate blocks cover 73.7% of lines; extract shared helpers, starting
    with entity.go:1-64, added in this change.

Under the embedded default the package's only findings are `has_tests` and
`untested_exports` (5 to 9), so the check already fails, but nothing names
the copy. The rule also fires on `pkg/config` (51 to 63) and `internal/app`
(106 to 121), which the default passes on duplication, and on `cmd` it
merges into the `dup_blocks` finding (238 to 326).

## Decision

Not shipped. The kubesentry change fails under the rule at every floor, but
the rule lowers the pooled J on the hand labels at every floor (+65.0 to
+63.6 points on the pool with capacity-only labels set aside, +65.9 to +64.5
on the full pool) and adds no `block` commit the gate missed; the
split-or-extraction labels agree (+63.3 to +62.2). The embedded default
carries no `duplicated_lines` rule. What ships is the rule's support: a
configuration may add it, its findings are located and suggested like the
other duplication rules' and merge with them into one finding (SPEC.md 8.1),
and `calibration/validate --candidate` measures it, or any rule the default
lacks, before it ships. A rule with a minimum rise rather than a floor
(`max_delta` 10) costs no false failure here but adds nothing either; it
would need a replay where the block count misses a copy to be worth
measuring again.

## Recheck

Re-evaluating every rule of the default at its shipped limits disagrees with
the recorded violations on 23 of 4,913 (row, rule) pairs, all of them
`dup_blocks_cross_pkg` on the module row: the replay judged it on the
baseline count plus the added cross-package blocks (SPEC.md 8.1), which the
stored counts cannot reproduce. The validate command at bd13bc3, before this
change, reports the same 23, and the candidate is not rechecked.
