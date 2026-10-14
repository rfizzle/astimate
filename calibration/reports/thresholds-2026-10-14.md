# Gate retune thresholds-2026-10-14

- Configuration: `calibration/thresholds/astimate-thresholds-2026-10-14.yaml`, the embedded default (`internal/config/default.yaml`) and this repository's `astimate.yaml`
- Validation: `calibration/reports/gate-validation-2026-10-14.md`, the shipped default on the 60-token rows
- Rows: `calibration/data/replay-<name>-2026-10-14-min60/` for go-starter, ztp, shhh, crush (Go) and ai-shell (TypeScript), replayed under `calibration/thresholds/astimate-min-tokens-60-2026-10-14.yaml`; the 40-token rows `calibration/data/replay-<name>-2026-10-14/` only for the before view
- Labels: the hand labels `calibration/replay/labels/<name>-hand.yaml` (`shhh-hand-2026-10-14-6dfcbc1.yaml` for shhh's moved range at 60, `shhh-hand-2026-10-14.yaml` at 40) first, the split-or-extraction labels `<name>-split-extract-2026-10-14[-min60].yaml` second
- Inputs decided before this retune: `duplication.min_tokens` 60 with `dup_blocks` `max_delta` 0 (`calibration/notes/min-tokens-2026-10-14.md`), `duplicated_lines` left ungated (`calibration/notes/duplicated-lines-rule-2026-10-14.md`)
- No replay was run for this retune: every number below comes from the stored rows, through `calibration/validate`

## Relabel 2026-10-14

Five ztp hand labels were placeholders, `allow` with the reason "not loaded", written when the 2026-10-13 replay could not load their commits; the 2026-10-14 replays load them, so they were scored as `allow` without having been read. They have now been read (`calibration/notes/hand-labels-2026-10-14.md`): c4af29e is `block` (a verbatim copy of the policy matcher, extracted twelve commits later), and bb7f0cd, c0373bb, 9a006d6 and 130d2c8 stay `allow` with a cause. The pool is now 15 `block` and 137 `allow` commits. The two tables "The whole gate" and "Per rule" are re-run on the relabelled labels, with the same commands; the prose of the steps, the warn-rule list and the Not taken table keep the numbers each decision was made on (14 and 138).

Every decision holds on the relabelled labels. c4af29e fires only `dup_blocks` and `dup_blocks_cross_pkg`, so every other rule keeps its counts and only its denominators move, and no rule's "only it" count changes:

- `min_tokens` 60: `dup_blocks` at 60 fails 66.7% of `block` and 5.8% of `allow` (J +60.8), against 86.7% and 12.4% at 40 on the matched commits (over the budget), 46.7% and 2.2% at 80 (+44.5) and 26.7% and 2.9% at 120 (+23.7).
- `sloc` `over_max_delta` 100 to 150: J +31.9 to +33.3, false failures 1.5% to 0.0%, recall 33.3% kept; 200 lowers recall (26.7%).
- `tokens_est` 1,000 to 2,000: J +17.8 to +20.0, 2.2% to 0.0%, recall 20.0% kept; 3,000 lowers recall (13.3%).
- `largest_file_sloc` 5 to 50: 20, 25 and 50 tie at J +13.3 against +11.1 at 5; 100 lowers recall (6.7%).
- The four warn rules still fail no `block` commit another rule misses, and gate recall stays 86.7%.
- The settings under Not taken are still tightenings, which criterion 4 rules out whatever their numbers.

The shipped gate's false failures fall from 10.9% to 10.2% (14 of 137), still one commit over the target.

## Method

Every number is on the pooled hand-label view with the two capacity-only `block` labels set aside (ztp f430f2f, shhh ac1110f): 14 `block` and 138 `allow` agent-authored commits, as in the min-tokens note, when the retune was decided, and 15 and 137 in the two tables re-run after the relabel (above). A rule's recall is the share of `block` commits it fires on, its false-failure share the share of `allow` commits, and J their difference. A rule fires whatever its severity, so a warn rule keeps its own rates.

A change to a rule ships only when all of these hold:

1. the rule's J rises;
2. the rule fails at most 10% of `allow` commits, the per-rule budget of `calibration/validate`;
3. the whole gate still fails at least 80% of `block` commits;
4. it does not make the rule stricter. The gate failed 26.8% of `allow` commits at 60 tokens against a 10% target, and the hand labels were drawn by reading the 40-token gate's findings, so a stricter setting's extra catch is one or two of 14 `block` commits while its added false failures fall on every repository. This is the standing advice rule of `calibration/validate` ("while the gate fails far more `allow` commits than its target, no rule is made stricter"); every tightening the data favoured is listed under Not taken with its numbers.

A rule that fails no `block` commit the other failing rules miss, while failing `allow` commits, may ship as `severity: warn`, provided gate recall stays at or above 80% (the "Each rule's part in the gate" table of the validation report). It keeps reporting its findings to an agent's self-check loop and stops failing commits.

On a tie in J the largest allowance is taken, the rule of SPEC.md 8.2 and 11.4.

The validation of the shipped default reads the rows replayed under the min-tokens-60 file: `calibration/validate` now accepts rows replayed under another committed configuration when its `chars_per_token`, duplication settings and exemptions are the same, and re-evaluates every rule whose limits differ on the rows' stored metrics (here `sloc`, `tokens_est` and `largest_file_sloc`, with their TypeScript override rules) instead of reading the replay's recorded breaches. Re-evaluating the other rules at their limits reproduces the recorded violations but for 11 of 4,898 (row, rule) pairs, all `dup_blocks_cross_pkg` on the module row (below).

## The whole gate

| Rows, configuration | Gate recall | Gate false failures | Gate J | Split-or-extraction labels: recall, false failures | Every author: recall, false failures |
| --- | --- | --- | --- | --- | --- |
| 40 tokens, `astimate-thresholds-2026-10-13.yaml` (the replay's own gate) | 14 of 15 (93.3%) | 38 of 139 (27.3%) | +66.0 pts | 8 of 9 (88.9%), 44 of 145 (30.3%) | 20 of 21 (95.2%), 68 of 275 (24.7%) |
| 60 tokens, `astimate-min-tokens-60-2026-10-14.yaml` (only `min_tokens` changed) | 13 of 15 (86.7%) | 36 of 137 (26.3%) | +60.4 pts | 11 of 11 (100.0%), 38 of 141 (27.0%) | 19 of 21 (90.5%), 66 of 275 (24.0%) |
| 60 tokens, the four warn rules only | 13 of 15 (86.7%) | 20 of 137 (14.6%) | +72.1 pts | | |
| 60 tokens, the three allowances only | 13 of 15 (86.7%) | 31 of 137 (22.6%) | +64.0 pts | | |
| 60 tokens, `thresholds-2026-10-14` (shipped) | 13 of 15 (86.7%) | 14 of 137 (10.2%) | +76.4 pts | 11 of 11 (100.0%), 16 of 141 (11.3%) | 18 of 21 (85.7%), 38 of 275 (13.8%) |

The 40-token row has 139 `allow` commits because shhh's range moved two commits between the replays (min-tokens note); on the 137 shared commits it fails 37 (27.0%). The every-author view is `--agent-only=false`, adding go-starter's first 20 commits, crush's 58 human commits and all of ai-shell; it was run to check the retune does not rest on the agent-only filter, and it falls from 24.0% to 13.8% too.

The false-failure target (10%) is missed by one commit: 13 of 137 would meet it. Every remaining false failure carries a duplication finding: `dup_blocks` fails 8 of the 14 `allow` commits (5 alone), `dup_blocks_cross_pkg` 7 (5 alone), `duplication_pct` 2 and `max_nesting` 1 (neither alone). That is what the partial-overlap coalescing and narrow-shape work of `calibration/notes/dup-block-kinds-2026-10-14.md` addresses, not a limit.

## Per rule

Rule columns are the rule alone, as recorded or re-evaluated at that setting; "only it" counts the commits no other failing rule fails, from the validation report.

| Rule | Shipped before | Decision | Shipped now | Before: recall, false failures, J | After: recall, false failures, J | Only it before: `block`, `allow` |
| --- | --- | --- | --- | --- | --- | --- |
| `dup_blocks` | `max_delta` 0 at `min_tokens` 40 | kept, at `min_tokens` 60 | `max_delta` 0 | 86.7%, 12.4%, +74.3 (40, over the budget) | 66.7%, 5.8%, +60.8 | 2, 2 |
| `duplication_pct` | `max_delta` 6, `max` 40, `floor` 10 | kept | the same | 13.3%, 1.5%, +11.9 | the same | 0, 0 |
| `untested_exports` | fail, `max_delta` 0 | warn | warn, `max_delta` 0 | 46.7%, 12.4%, +34.3 (over the budget) | the same, never failing | 0, 10 |
| `globals` | fail, `max_delta` 0 | warn | warn, `max_delta` 0 | 6.7%, 3.6%, +3.0 | the same, never failing | 0, 2 |
| `init_funcs` | `max_delta` 0 | kept | the same | never fired | never fired | 0, 0 |
| `max_nesting` | `max_delta` 0, `max` 5, `floor` 4 | kept | the same | 6.7%, 0.7%, +5.9 | the same | 0, 0 |
| `cognitive_p90` | fail, `max_delta` 3, `max` 20, `floor` 10 | warn | warn, same limits | 0.0%, 2.9%, -2.9 | the same, never failing | 0, 3 |
| `changed_func_cognitive_max` | `max` 50, `over_max_delta` 5 | kept | the same | 6.7%, 0.0%, +6.7 | the same | 0, 0 |
| `dup_blocks_cross_pkg` | `max_delta` 0, `max` 450 | kept | the same | 46.7%, 5.1%, +41.6 | the same | 0, 3; after the demotions 1, 5 |
| `tokens_est` | `max` 16,000, `over_max_delta` 1,000 | retuned | `over_max_delta` 2,000 | 20.0%, 2.2%, +17.8 | 20.0%, 0.0%, +20.0 | 0, 1 |
| `largest_file_sloc` | `max` 600, `over_max_delta` 5 | retuned | `over_max_delta` 50 | 13.3%, 2.2%, +11.1 | 13.3%, 0.0%, +13.3 | 0, 3 |
| `exported_symbols` | `max` 60, `over_max_delta` 5 | kept | the same | 26.7%, 0.0%, +26.7 | the same | 0, 0 |
| `internal_imports` | fail, `max` 10, `over_max_delta` 1 | warn | warn, same limits | 0.0%, 0.7%, -0.7 | the same, never failing | 0, 0 |
| `sloc` | `max` 1,000, `over_max_delta` 100 | retuned | `over_max_delta` 150 | 33.3%, 1.5%, +31.9 | 33.3%, 0.0%, +33.3 | 0, 0 |
| `has_tests`, past 100 SLOC, not `main` | require | kept | the same | 6.7%, 0.0%, +6.7 | the same | 0, 0 |
| `has_tests`, past 300 SLOC, `main` | require | kept | the same | never fired | never fired | 0, 0 |

### `min_tokens` and `dup_blocks` (step 1)

Applied as the min-tokens note recommends: 60 has the best pooled J of the four settings with `dup_blocks` inside the budget (+57.8 at `max_delta` 0, against +45.7 for 40 at `max_delta` 1, +47.8 for 80 and +25.7 for 120), and is the only pair inside the budget that keeps gate recall over 80%. `min_tokens_cross` stays 80.

### `duplication_pct` and `dup_blocks_cross_pkg` (step 2)

Neither can be refitted from the corpus collection, whose 36 modules would need cloning. Both are swept on the 60-token rows instead.

- `duplication_pct` keeps `max_delta` 6, `max` 40 and `floor` 10. Every looser setting lowers its J (`max_delta` 10: 7.1%, 1.4%, +5.7; `max` 120: 7.1%, 0.0%, +7.1); every `floor` from 0 to 15 gives the same +12.8. The stricter `max_delta` 0 (35.7%, 3.6%, +32.1) adds two gate false failures (28.3%) and no gate recall, and is not taken. It fails no `block` commit `dup_blocks` does not.
- `dup_blocks_cross_pkg` keeps `max_delta` 0. Its sweep reads the module row's whole count, while the gate judges only the blocks a change added of at least `min_tokens_cross` (80) tokens, which the stored counts cannot re-derive; that is the source of every recheck disagreement, and it overstates the rule below 80 (sweep at `max_delta` 0: 64.3%, 6.5%, against 42.9%, 5.8% recorded). On the sweep's own terms `max_delta` 1 lowers J (+57.8 to +47.8). Its `max` sweep is flat (450 judges only a module with no baseline). After the four demotions it is the only rule failing one `block` commit, which keeps gate recall at 85.7% (78.6% without it).
- Both `max` values were fitted at `min_tokens` 40 (SPEC.md 11.1). A longer minimum only lowers both metrics, so at 60 the same limits are looser, not stricter; a refit needs the collection re-run at 60.

### `sloc`, `tokens_est`, `largest_file_sloc` and `changed_func_cognitive_max` (step 3)

- `sloc` `over_max_delta` 100 to 150: J +34.3 to +35.7, false failures 1.4% to 0.0%, recall unchanged at 35.7%. 200 lowers recall (28.6%, +28.6).
- `tokens_est` `over_max_delta` 1,000 to 2,000: J +19.3 to +21.4, false failures 2.2% to 0.0%, recall unchanged at 21.4%; the gate alone falls from 26.8% to 26.1%. 3,000 lowers recall (14.3%).
- `largest_file_sloc` `over_max_delta` 5 to 50: the hand-set 5 was to stay "until the hand labels allow a refit" (SPEC.md 8.2). 20, 25 and 50 tie at J +14.3 (14.3%, 0.0%) against +12.1 at 5; the largest is taken. The gate alone falls from 26.8% to 24.6%. 100 lowers recall (7.1%). It also covers the field adjudication's bug fixes of +6 to +24 lines in files over 600.
- `changed_func_cognitive_max` stays at `max` 50 and `over_max_delta` 5. The 2026-09-29 report advised `max` 63 from the rule-labeled corpora; on the hand labels 63 has the same J as 50 (+7.1), so it is not a rise. Every `over_max_delta` from 0 to 5 gives +7.1 and 10 loses the one `block` commit.
- `exported_symbols` keeps its hand-set `over_max_delta` 5 (28.6%, 0.0%, +28.6): 2 and 3 give +35.7, a tightening that adds no gate recall, against the crush config-schema evidence for 5.

### Warn rules (step 4)

On the 60-token rows, with every rule at fail, only `dup_blocks` fails a `block` commit no other rule fails (2 of 14). Four rules that fail no `block` commit another rule misses, and do fail `allow` commits, ship as `severity: warn`:

- `untested_exports`: over the per-rule budget (12.3%), 10 of the gate's 37 false failures alone, and every one of its 7 `block` commits is failed by another rule as well. Its retune to `max_delta` 1 lowers its J (+37.7 to +36.3) and is not taken.
- `cognitive_p90`: fires on no `block` commit at its shipped limits (J -2.9); 3 false failures alone.
- `globals`: J +3.5 at precision 16.7%; 2 false failures alone, and its one `block` commit is failed by another rule.
- `internal_imports`: fires on no `block` commit (J -0.7); its one false failure is shared.

Together they leave gate recall at 85.7% and take its false failures from 26.8% to 15.2%; with the three allowances, 10.9%. The TypeScript override's `cognitive_p90` and `internal_imports` rules warn as well, since an override rule replaces the top-level rule whole.

Not demoted although they fail `allow` commits alone: `dup_blocks_cross_pkg` (J +37.1 inside the budget, and after the demotions it alone fails one `block` commit), `largest_file_sloc` and `tokens_est` (their false failures are removed by the allowances instead, keeping their recall).

### Not taken: tightenings the data favoured

| Rule | Setting | Rule recall, false failures, J | Gate recall, false failures (others as replayed) |
| --- | --- | --- | --- |
| `sloc` | `over_max_delta` 25 | 50.0%, 6.5%, +43.5 | 92.9%, 27.5% |
| `cognitive_p90` | `max_delta` 0 | 21.4%, 5.8%, +15.6 | 92.9%, 28.3% |
| `duplication_pct` | `max_delta` 0 | 35.7%, 3.6%, +32.1 | 85.7%, 28.3% |
| `changed_func_cognitive_max` | `max` 25 | 21.4%, 2.2%, +19.3 | 85.7%, 27.5% |
| `max_nesting` | `floor` 1 | 21.4%, 1.4%, +20.0 | 85.7%, 27.5% |
| `exported_symbols` | `over_max_delta` 3 | 35.7%, 0.0%, +35.7 | 85.7%, 26.8% |
| `has_tests`, past 100 SLOC | `when > 50` | 14.3%, 1.4%, +12.8 | 85.7%, 26.8% |
| `internal_imports` | `max` 8 | 14.3%, 0.7%, +13.6 | 85.7%, 26.8% |

`sloc` at 25 and `cognitive_p90` at 0 each add one `block` commit to the gate for one and two false failures; they are candidates once the gate is inside its false-failure target.

### TypeScript override

ai-shell is all `agent: false`, so no TypeScript row is in the agent-only views and no TypeScript limit is measured. The override's allowances stay the top level's scaled by the ratio of the ceilings, as SPEC.md 8.2 defines them: `sloc` 150 x 2,500/1,000 = 375, `tokens_est` 2,000 x 35,000/16,000 = 4,375, rounded to 4,400, `largest_file_sloc` 50 x 900/600 = 75.

## Missed `block` commits and the blind-spot stories (step 7)

| Commit | What it does | Missed at | Why | What would catch it | Stories S-147 to S-152 |
| --- | --- | --- | --- | --- | --- |
| shhh fe415c4 | pastes a 14-line temp-and-rename write from `internal/pricing` into `internal/update` (`internal/update/check.go:209`) | 40 and 60 | a copy between packages shorter than the 80-token `min_tokens_cross` floor, so `dup_blocks_cross_pkg` does not count it as added, and no package's own count or `duplicated_lines` sees a cross-package copy | `min_tokens_cross` 60: on the 20-commit sample of `calibration/notes/cross-blocks-added-2026-10-14.md` the cross rule then fails 8 of 10 `block` and 7 of 10 `allow` commits, against 5 and 2 at 80, and whether fe415c4's block clears 60 is not recorded in the rows | none: it touches 5 packages, the p90 of the 186 replayed commits' changed packages, so a `changed_packages` max at p90 (S-151) passes it |
| shhh 0198952 | repeats seven heading-builder lines a few lines down in the same function (`internal/ui/components/inspectorprocesses.go:72` and `:81`) | 60 | a repeat under 60 tokens; `dup_blocks` caught it at 40 | `min_tokens` 40, which the min-tokens note rejected: `dup_blocks` then fails 13.0% of `allow` commits, over the budget | none |

None of S-147 to S-152 would catch a missed hand-labelled `block` commit, so the count orders nothing: every story catches 0. Both misses are duplication below a length floor, a setting rather than a missing metric. The order is therefore by the evidence each story has outside these labels, and by cost:

1. S-150 (dead exports): the only story with field evidence, four cases in the 2026-10-13 adjudication (its own reconciliation note).
2. S-151 (change shape): fe415c4 sits at the p90 of changed packages, the nearest any story comes to a miss, and the replay rows already carry the count.
3. S-152 (swallowed errors and library panics): cheap, in the single inspector pass, and enforces this project's own bans, with no field case yet.
4. S-147 (new external dependencies): plausible, no case in the labels or the field adjudication.
5. S-148 (assertion-free tests): its value rides on `untested_exports`, which now warns rather than fails, so the bypass it closes no longer fails a commit.
6. S-149 (producer interfaces): least evidence, and Go's consumer-defined interfaces make the count noisy.

## What was not redone

- The four 2026-09-28 corpora were not re-replayed: their repositories need clones, which this retune does not make. Their rows and reports stay as they are.
- `duplication_pct`'s and `dup_blocks_cross_pkg`'s `max` were not refitted at 60 (step 2).
- Nothing here re-measures the rebuild estimate.

## config_version

The default ships as `config_version: thresholds-2026-10-14`, the name of this retune. Its rebuild parameters are still those of `rebuild-2026-09-28-claude-code-opus`, so `score.Calibrated` now also reads a `thresholds-<date>` version dated after that fit as carrying the fitted parameters (`rebuild.calibrated` stays true and the text summary keeps its passes); a `thresholds-<date>` file dated on or before 2026-09-28 still reads as uncalibrated, as its parameters are the placeholders.
