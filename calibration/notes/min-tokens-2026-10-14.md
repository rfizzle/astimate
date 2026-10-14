# Duplicate-block length: `min_tokens` and the `dup_blocks` delta

Date: 2026-10-14. Data: `calibration/data/replay-<name>-2026-10-14-min<N>/`
for N = 60, 80 and 120, beside the 40-token rows of
`calibration/data/replay-<name>-2026-10-14/`, for go-starter, ztp, shhh and
crush (Go) and ai-shell (TypeScript). Configurations:
`calibration/thresholds/astimate-min-tokens-<N>-2026-10-14.yaml`. Labels: the
hand labels `calibration/replay/labels/<name>-hand.yaml` (for shhh,
`shhh-hand-2026-10-14.yaml` at 40 and `shhh-hand-2026-10-14-6dfcbc1.yaml` at
the other settings) first, and the split-or-extraction labels
`<name>-split-extract-2026-10-14[-min<N>].yaml` second. Reports:
`calibration/reports/gate-validation-2026-10-14-replays.md` (40) and
`gate-validation-2026-10-14-min<N>.md`.

## Question

`dup_blocks` at `max_delta` 0 fails the most `allow` commits of any rule, and
the false hits read as short repeats near the 40-token floor: option
setters, error chains, per-command flag setup, registration calls.
golangci-lint's `dupl` starts at 80 to 120 tokens. Two levers lower those
hits: a longer minimum block (`duplication.min_tokens`, shared by every
duplication metric) or a tolerance on the count (`max_delta` on `dup_blocks`).
Which pair (`min_tokens`, `max_delta`) has the best pooled J (recall minus
false-failure share) on the hand labels while the rule fails at most 10% of
`allow` commits, the budget `calibration/validate` gives one rule? S-146
applies the answer.

## Replays

Each setting's configuration is `astimate config init` (the embedded default,
`rebuild-2026-09-28-claude-code-opus`) with `duplication.min_tokens` changed
and its own `config_version`, `min-tokens-<N>-2026-10-14`, so `validate`
refuses rows replayed at another setting (it does: the 80-token ztp rows
under the 60-token file, or under the default, stop with "replayed under
min-tokens-80-2026-10-14"). `min_tokens_cross` stays 80. Each repository was
replayed for its last 60 first-parent commits, one at a time, from a scratch
directory so no `astimate.yaml` was picked up:

    TMPDIR=<cache dir> GOPROXY=off ./astimate dryrun <repo> --last 60 --timeout 0 \
      --config calibration/thresholds/astimate-min-tokens-<N>-2026-10-14.yaml \
      --out calibration/data/replay-<name>-2026-10-14-min<N>

The binary is 2f4a136; the 40-token rows are bd13bc3's. Between the two only
the `duplicated_lines` rule support and calibration data landed, which
change no default rule's verdict, and re-evaluating the 40-token rows with
the current gate reproduces the recorded violations as before (23 module-row
`dup_blocks_cross_pkg` pairs, below).

| Repository | 40 failed | 60 failed | 80 failed | 120 failed | Loaded | 60-token wall time |
| --- | --- | --- | --- | --- | --- | --- |
| go-starter | 10 | 8 | 9 | 8 | 60 | 290 s |
| ztp | 29 | 28 | 26 | 23 | 58 | 76 s |
| shhh | 20 | 19 | 18 | 17 | 60 | 1,183 s |
| crush | 17 | 17 | 14 | 14 | 60 | 413 s |
| ai-shell | 14 | 15 | 14 | 14 | 60 | 5 s |

All three settings ran; 60 was run after 80 and 120 finished, with time to
spare. The four other ranges are those of 2026-10-14. shhh's head moved two
commits after the 40-token replay (73d0c97 to 6dfcbc1), so its range lost
e3ca575 and 50b9fa9 and gained af246d6 and 6dfcbc1: 58 of the 60 hand labels
join. `shhh-hand-2026-10-14-6dfcbc1.yaml` is those 58 and the two new commits
as allow by assumption (provenance proposed); both passed at every setting,
touch only a script and README images, carry no Co-Authored-By trailer and
read `agent: false`, so the agent-only views leave them out. The two lost
commits are agent `allow` labels, and e3ca575 was a `dup_blocks` false
failure at 40, so the 40-token pool has 140 `allow` commits and the others
138. The rows "40, matched" below restrict the 40-token shhh rows and labels
to the 58 shared commits (a filtered copy in scratch, not committed), so
every setting is compared on the same commits.

The split-or-extraction labeler reads the replay's own `dup_blocks` counts
(an extraction is a later fall in a count this commit raised), so it was
re-run per setting (`go run ./calibration/replay/label --rule split-extract
--data <the -min<N> dir>`, its capacity rules from the setting's file):
`block` labels per setting, all extraction, go-starter 2, 1, 2, ztp 7, 3, 2,
shhh 5, 0, 0, crush 2, 1, 1 and ai-shell 0 at 60, 80 and 120.

Validation, per setting:

    go run ./calibration/validate --date 2026-10-14 \
      --config calibration/thresholds/astimate-min-tokens-<N>-2026-10-14.yaml \
      --out calibration/reports/gate-validation-2026-10-14-min<N>.md \
      --corpus go-starter=calibration/data/replay-go-starter-2026-10-14-min<N>:calibration/replay/labels/go-starter-hand.yaml,calibration/replay/labels/go-starter-split-extract-2026-10-14-min<N>.yaml \
      ... (ztp, shhh with shhh-hand-2026-10-14-6dfcbc1.yaml, crush, ai-shell)

## Results on the hand labels, pooled, capacity-only set aside

All numbers are on all five corpora pooled with the two capacity-only `block`
labels set aside (ztp f430f2f, shhh ac1110f): 14 `block` commits, and 138
`allow` (140 for the 40-token rows as published).

### The gate

The gate as replayed at each setting, every rule at its shipped limit.

| `min_tokens` | Gate recall | Gate false failures | Gate J |
| --- | --- | --- | --- |
| 40, as published | 13 of 14 (92.9%) | 39 of 140 (27.9%) | +65.0 pts |
| 40, matched | 13 of 14 (92.9%) | 38 of 138 (27.5%) | +65.4 pts |
| 60 | 12 of 14 (85.7%) | 37 of 138 (26.8%) | +58.9 pts |
| 80 | 11 of 14 (78.6%) | 35 of 138 (25.4%) | +53.2 pts |
| 120 | 10 of 14 (71.4%) | 32 of 138 (23.2%) | +48.2 pts |

### `dup_blocks` per (`min_tokens`, `max_delta`)

Rule columns are `dup_blocks` alone, re-evaluated at that `max_delta` by the
sweep; gate columns are the gate with `dup_blocks` at that `max_delta` and
every other rule as replayed at the same `min_tokens`. Within budget means the
rule's own false-failure share is at most 10%.

| `min_tokens` | `max_delta` | Rule recall | Rule false failures | Rule J | Within budget | Gate recall | Gate false failures | Gate J |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 40, matched | 0 * | 85.7% | 13.0% | +72.7 pts | no | 92.9% | 27.5% | +65.4 pts |
| 40, matched | 1 | 50.0% | 4.3% | +45.7 pts | yes | 78.6% | 26.1% | +52.5 pts |
| 40, matched | 2 | 42.9% | 1.4% | +41.4 pts | yes | 71.4% | 26.1% | +45.3 pts |
| 40, matched | 3 | 28.6% | 1.4% | +27.1 pts | yes | 71.4% | 26.1% | +45.3 pts |
| 60 | 0 * | 64.3% | 6.5% | **+57.8 pts** | yes | 85.7% | 26.8% | +58.9 pts |
| 60 | 1 | 28.6% | 2.2% | +26.4 pts | yes | 71.4% | 26.1% | +45.3 pts |
| 60 | 2 | 7.1% | 0.7% | +6.4 pts | yes | 71.4% | 25.4% | +46.0 pts |
| 60 | 3 | 7.1% | 0.7% | +6.4 pts | yes | 71.4% | 25.4% | +46.0 pts |
| 80 | 0 * | 50.0% | 2.2% | +47.8 pts | yes | 78.6% | 25.4% | +53.2 pts |
| 80 | 1 | 21.4% | 0.7% | +20.7 pts | yes | 71.4% | 25.4% | +46.0 pts |
| 80 | 2 | 7.1% | 0.0% | +7.1 pts | yes | 71.4% | 25.4% | +46.0 pts |
| 80 | 3 | 0.0% | 0.0% | +0.0 pts | yes | 71.4% | 25.4% | +46.0 pts |
| 120 | 0 * | 28.6% | 2.9% | +25.7 pts | yes | 71.4% | 23.2% | +48.2 pts |
| 120 | 1 | 14.3% | 0.0% | +14.3 pts | yes | 71.4% | 23.2% | +48.2 pts |
| 120 | 2 | 0.0% | 0.0% | +0.0 pts | yes | 71.4% | 23.2% | +48.2 pts |
| 120 | 3 | 0.0% | 0.0% | +0.0 pts | yes | 71.4% | 23.2% | +48.2 pts |

`*` is the shipped `max_delta`. The 40-token rows as published differ only in
the false-failure column (13.6%, 5.0%, 1.4% and 1.4% at `max_delta` 0 to 3, J
+72.1, +45.0, +41.4 and +27.1). The full grid (5, 10, 20) is in each report;
from 2 up every setting's rule catches at most one `block` commit.

### Per-rule rows at the shipped limits

As recorded by the replay (a rule fired when one of the commit's rows records
its violation, its own or in a merged finding's `also`).

| `min_tokens` | Rule | Fired on `block` | Fired on `allow` | Precision | Recall | False-failure share | J |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 40, matched | `dup_blocks` | 12 | 18 | 40.0% | 85.7% | 13.0% | +72.7 pts |
| 40, matched | `duplication_pct` | 4 | 4 | 50.0% | 28.6% | 2.9% | +25.7 pts |
| 40, matched | `dup_blocks_cross_pkg` | 6 | 8 | 42.9% | 42.9% | 5.8% | +37.1 pts |
| 60 | `dup_blocks` | 9 | 9 | 50.0% | 64.3% | 6.5% | +57.8 pts |
| 60 | `duplication_pct` | 2 | 2 | 50.0% | 14.3% | 1.4% | +12.8 pts |
| 60 | `dup_blocks_cross_pkg` | 6 | 8 | 42.9% | 42.9% | 5.8% | +37.1 pts |
| 80 | `dup_blocks` | 7 | 3 | 70.0% | 50.0% | 2.2% | +47.8 pts |
| 80 | `duplication_pct` | 1 | 2 | 33.3% | 7.1% | 1.4% | +5.7 pts |
| 80 | `dup_blocks_cross_pkg` | 6 | 8 | 42.9% | 42.9% | 5.8% | +37.1 pts |
| 120 | `dup_blocks` | 4 | 4 | 50.0% | 28.6% | 2.9% | +25.7 pts |
| 120 | `duplication_pct` | 1 | 2 | 33.3% | 7.1% | 1.4% | +5.7 pts |
| 120 | `dup_blocks_cross_pkg` | 3 | 2 | 60.0% | 21.4% | 1.4% | +20.0 pts |

### `duplication_pct` and `dup_blocks_cross_pkg` beside

The same sweep for the two rules that share the setting (rule recall, rule
false failures, rule J).

| `min_tokens` | `duplication_pct` `max_delta` 0 | 1 | 2 | 3 | 6 * | `max` 20 | `max` 40 * |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 40, matched | 50.0%, 7.2%, +42.8 | 35.7%, 2.9%, +32.8 | 35.7%, 2.9%, +32.8 | 28.6%, 2.9%, +25.7 | 28.6%, 2.9%, +25.7 | 28.6%, 4.3%, +24.2 | 28.6%, 2.9%, +25.7 |
| 60 | 35.7%, 3.6%, +32.1 | 21.4%, 2.9%, +18.5 | 21.4%, 2.2%, +19.3 | 14.3%, 2.2%, +12.1 | 14.3%, 1.4%, +12.8 | 21.4%, 2.9%, +18.5 | 14.3%, 1.4%, +12.8 |
| 80 | 14.3%, 1.4%, +12.8 | 14.3%, 1.4%, +12.8 | 14.3%, 1.4%, +12.8 | 7.1%, 1.4%, +5.7 | 7.1%, 1.4%, +5.7 | 14.3%, 1.4%, +12.8 | 7.1%, 1.4%, +5.7 |
| 120 | 14.3%, 2.2%, +12.1 | 14.3%, 2.2%, +12.1 | 14.3%, 2.2%, +12.1 | 7.1%, 2.2%, +5.0 | 7.1%, 1.4%, +5.7 | 7.1%, 1.4%, +5.7 | 7.1%, 1.4%, +5.7 |

`duplication_pct` adds nothing
to the gate at any setting: dropping it leaves gate recall where it is
(92.9%, 85.7%, 78.6%, 71.4%) and lowers gate false failures by at most one
commit.

| `min_tokens` | `dup_blocks_cross_pkg` `max_delta` 0 * | 1 | 2 | 3 | Recheck disagreements |
| --- | --- | --- | --- | --- | --- |
| 40, matched | 85.7%, 13.0%, +72.7 | 64.3%, 9.4%, +54.9 | 50.0%, 5.1%, +44.9 | 42.9%, 5.1%, +37.8 | 23 of 4,898 |
| 60 | 64.3%, 6.5%, +57.8 | 50.0%, 2.2%, +47.8 | 14.3%, 1.4%, +12.8 | 7.1%, 1.4%, +5.7 | 11 of 4,898 |
| 80 | 42.9%, 4.3%, +38.5 | 28.6%, 1.4%, +27.1 | 7.1%, 1.4%, +5.7 | 0.0%, 1.4%, -1.4 | 4 of 4,898 |
| 120 | 21.4%, 1.4%, +20.0 | 7.1%, 1.4%, +5.7 | 7.1%, 0.7%, +6.4 | 7.1%, 0.7%, +6.4 | 2 of 4,898 |

The `dup_blocks_cross_pkg` sweep reads the module row's full count, while the
gate judges only the blocks a change added of at least `min_tokens_cross`
(80) tokens, which the stored counts cannot re-derive; every recheck
disagreement is that rule on the module row. Below 80 the sweep therefore
overstates the rule (85.7% and 64.3% recall at `max_delta` 0, against 42.9%
recorded), and at 80 it nearly matches. The recorded rows are the rule as it
gates: 42.9% and 5.8% at 40, 60 and 80, since the added-block floor stays 80,
and 21.4% and 1.4% at 120, where no block under 120 tokens is found at all.
Its `max` (450) judges only a module with no baseline; its `max` sweep is
flat at every setting, so these replays do not measure it.

### Split-or-extraction labels second

The same commits under the split-or-extraction labels, re-derived per
setting. Their `block` labels are extractions read from the same counts the
duplication rules read, so recall is near 100% by construction and too few to
compare across settings; the false-failure rate is the measure.

| `min_tokens` | `block` scored | Gate recall | Gate false failures |
| --- | --- | --- | --- |
| 40, as published | 9 | 8 of 9 (88.9%) | 44 of 145 (30.3%) |
| 40, matched | 9 | 8 of 9 (88.9%) | 43 of 143 (30.1%) |
| 60 | 11 | 11 of 11 (100.0%) | 38 of 141 (27.0%) |
| 80 | 2 | 2 of 2 (100.0%) | 44 of 150 (29.3%) |
| 120 | 1 | 1 of 1 (100.0%) | 41 of 151 (27.2%) |

## What moves between settings

At 60 the gate stops failing one hand-labelled `block` commit the 40-token
gate failed: shhh 0198952, which pastes a block's meta-building lines a
second time in the same file (`internal/ui/components/inspectorprocesses.go`
lines 72 and 81), a repeat under 60 tokens that `dup_blocks` alone caught.
At 80 it also stops failing ztp 0a8b69b (the HTTP RoundTripper copying the
filter package's redaction logic, caught at 40 and 60 by `dup_blocks` only);
at 120 it also passes shhh ce64275. shhh fe415c4, a 14-line paste between
packages that no rule catches at 40, stays missed at every setting.

`dup_blocks` at 60 stops failing ten `allow` commits it failed at 40:
go-starter ef5b99f (dependency-struct repeats after a deletion), ztp
b098ecc (a de-duplication refactor), 4d185e9, 5ded0c2, 33851d1 and 383d367
(fakes, I/O idioms, an HMAC wrapper) and shhh 228d31a, 759f9ad, de2261b and
3ffa8f4 (rail blocks, config rows), the boilerplate the hand notes name; it
starts failing one, shhh 3216a33 (`internal/cli` 111 to 113 blocks). Of the
nine `allow` commits it still fails at 60, four are new ztp packages counted
from zero under `ratchet_from_zero` (e9eac71, bb794c0, c4af29e, 130d2c8) and
one a restructure that moved blocks (51ef8c3).

The hand labels were drawn by reading the 40-token findings, so a `block`
whose only evidence is a short repeat is in them and one the 40-token gate
never showed is not; the labels favour 40 on recall. 60 has the higher J
within the budget all the same.

## Recommendation for S-146

**`duplication.min_tokens: 60` with `dup_blocks` `max_delta: 0`.**

- It has the best pooled J within the 10% budget: `dup_blocks` alone fails
  64.3% of `block` and 6.5% of `allow` commits, J +57.8 pts, against the best
  40-token setting within the budget, `max_delta` 1 at 50.0% and 4.3%
  (J +45.7 matched, +45.0 as published), and 80 with `max_delta` 0 at 50.0%
  and 2.2% (J +47.8). 120 is worse at every `max_delta` (J +25.7 at most).
- The whole gate agrees: 85.7% recall and 26.8% false failures (J +58.9),
  the only in-budget pair that keeps gate recall over the 80% target; the
  40-token `max_delta` 1 retune gives 78.6% and 26.1% (J +52.5) and the
  80-token pair 78.6% and 25.4% (J +53.2).
- It keeps zero tolerance on new duplicate blocks, the policy SPEC.md 11.1
  holds `max_delta` 0 to, rather than letting every change add one block.
- A delta does not help at 60 or above: the first step removes most of the
  rule's recall for a few `allow` commits (at 60, 64.3% to 28.6% of `block`
  for six `allow` commits, 6.5% to 2.2%, J +57.8 to +26.4; at 80, 50.0% to
  21.4% for two, J +47.8 to +20.7).

The gate stays far over its 10% false-failure target (26.8%); this pair is
the duplication lever, not the whole fix. S-180 (partial-overlap coalescing)
and the narrow-shape exemptions of `calibration/notes/dup-block-kinds-2026-10-14.md`
are measured separately; that note found block kinds explain about 11% of
duplication findings and named `min_tokens` the larger lever, which this
measurement confirms.

### The two dependent rules

`min_tokens` moves, so S-146 refits `duplication_pct` and
`dup_blocks_cross_pkg` from the corpus collection (SPEC.md 11.1) at
`min_tokens` 60; the 2026-09-28 fit was at 40.

- `duplication_pct` must be refitted: at 60 its J falls from +25.7 to +12.8
  (recall 28.6% to 14.3%) at its shipped `max` 40 and `max_delta` 6, since
  fewer lines count as duplicated and the 40-token percentiles are loose at
  60. It adds no gate recall at either setting.
- `dup_blocks_cross_pkg` keeps its J on these replays (+37.1 at 40, 60 and
  80, since its added-block floor `min_tokens_cross` is 80 either way), so its
  `max_delta` 0 stands. Its `max` 450 is the p90 of the module rows' count of
  blocks of at least `min_tokens` tokens, which falls as the setting rises,
  and these replays do not measure it (its sweep is flat), so it comes from
  the same collection.
- `min_tokens_cross` stays 80.

The embedded default is unchanged here; S-146 applies the pair and the refit.
