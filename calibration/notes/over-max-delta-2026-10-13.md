# Over-max allowances for exported_symbols, largest_file_sloc and internal_imports

Date: 2026-10-13. Report: `calibration/reports/gate-validation-2026-10-13.md`.
Data: `calibration/data/replay-<name>-2026-10-13/` and the rule labels
`calibration/replay/labels/<name>-split-extract.yaml`, for go-starter, ztp,
shhh and crush (Go) and ai-shell (TypeScript).

## Replays

Each repository was replayed for its last 60 first-parent commits with the
extractor and the embedded default at c28a78f (every mechanics fix of the
run: per-function ratchet, immutable globals, module-wide `has_tests`,
untested-export exclusions). The metric values are unchanged by the later
head moves (citation order, density floors), so the rows stay valid.
The report is generated with `--config
calibration/thresholds/astimate-thresholds-2026-10-13.yaml`, the embedded
default at c28a78f that the replays ran under, because the new default
differs from it in the allowances this note fits. Re-checking every rule at
those limits reproduces 7216 of 7217 (row, rule) pairs; one pair disagrees
and was not investigated.

| Repository | Loaded | Not loaded | Why |
| --- | --- | --- | --- |
| go-starter | 60 | 0 | |
| ztp | 53 | 7 | the commit itself does not compile (a missing package, an undefined option) |
| shhh | 60 | 0 | |
| crush | 60 | 0 | |
| ai-shell | 60 | 0 | |

A first run of shhh and crush filled the 9.4 GB `/tmp` tmpfs (23 and 15
commits recorded as not loaded for "no space left on device"); both were
discarded and replayed again with `TMPDIR` on a larger disk, which is what
the data holds.

The rule labeler gave 4 `block` commits for go-starter, ztp and shhh, 3 for
crush and 2 for ai-shell: 17 in all, of 276 commits judged.

## Pooled per-commit view

The 5 corpora pooled (all labels are rule labels). `Recall` and `False` are
the rule's own contribution, the share of `block` and of `allow` commits it
fails; J is their difference. The budget is 10% of `allow` commits.

`largest_file_sloc` (shipped 5):

| over_max_delta | Recall | False | J |
| --- | --- | --- | --- |
| 0 | 23.5% | 6.2% | +17.4 |
| 1 | 23.5% | 5.4% | +18.1 |
| 2 | 23.5% | 4.0% | +19.5 |
| 3 | 23.5% | 3.6% | +19.9 |
| 5 | 17.6% | 3.3% | +14.4 |
| 10 | 17.6% | 2.2% | +15.5 |
| 20 | 11.8% | 1.8% | +10.0 |
| 50 | 11.8% | 1.4% | +10.3 |
| 100 | 0.0% | 0.7% | -0.7 |

`exported_symbols` (shipped 0):

| over_max_delta | Recall | False | J |
| --- | --- | --- | --- |
| 0 | 23.5% | 7.2% | +16.3 |
| 1 | 11.8% | 4.7% | +7.1 |
| 2 | 5.9% | 4.0% | +1.9 |
| 3 | 5.9% | 3.3% | +2.6 |
| 5 | 5.9% | 1.8% | +4.1 |
| 10 | 5.9% | 1.4% | +4.4 |
| 20 | 5.9% | 1.4% | +4.4 |
| 25 | 0.0% | 1.1% | -1.1 |

`internal_imports` (shipped 0; fired on 8 commits, under the 10 the report
needs to judge):

| over_max_delta | Recall | False | J |
| --- | --- | --- | --- |
| 0 | 5.9% | 2.5% | +3.3 |
| 1 | 5.9% | 0.0% | +5.9 |
| 2 to 5 | 0.0% | 0.0% | +0.0 |

The gate's recall stays 100% and its false failures fall from 27.9% to
27.2% at 1 for `internal_imports` (27.9% at every `exported_symbols`
setting), which is small because the gate's false failures are mostly
`dup_blocks`, `dup_blocks_cross_pkg`, `untested_exports` and `has_tests`.

## Shipped values and why

The rule is the one of SPEC.md 11.4: the setting with the best J inside the
10% budget, the largest on a tie.

- `largest_file_sloc` **3** (was 5). J +19.9 against +14.4: at 5 one more
  `block` commit is let through. The difference is one commit of 17, so 3
  and 5 are not separable, and the item's field cases (+6, +9, +17 and +24
  lines on a bug fix) fail at either; they are not a one-feature allowance.
- `exported_symbols` **0**, written out. Every allowance loses more `block`
  commits than it spares `allow` ones (J +16.3 at 0, +7.1 at 1, +4.4 at 10).
  The expected +5 was not found in the data: 4 of the 24 commits the rule
  fired on are `block` commits, a rate above the base rate (6%), and the
  allowance that spares most `allow` commits (5) keeps 1 of the 4. The
  field evidence of crush's config-schema package (150+ exports failing on
  each new option) is not in these 60 commits as a `allow` majority; see
  the follow-up in the report to the backlog.
- `internal_imports` **1**. It spares every false firing (2.5% to 0.0%) and
  keeps the one `block` commit; a composition root gains one import per
  wired component and now passes with a warning. Thin evidence (8 firings),
  but the setting is the best J and costs no recall.
- `sloc` 100 and `tokens_est` 1000 are not part of this item and are
  unchanged. The pooled view would favour smaller allowances for both (J
  +21.1 at `sloc` 20 and +30 at `tokens_est` 150 inside the budget); that
  is left for a separate sweep.

## TypeScript override

ai-shell yields no over-ceiling row: none of the five TypeScript capacity
rules fired on any of its 60 commits (0 of 0 already over), against the 10
rows the item asks for. The values are the Go ones scaled by the ceiling
ratio, rounded to a readable step:

| Rule | Go over_max_delta | Ceiling ratio (TypeScript / Go) | TypeScript |
| --- | --- | --- | --- |
| `sloc` | 100 | 2500 / 1000 | 250 |
| `tokens_est` | 1000 | 35000 / 16000 | 2200 |
| `largest_file_sloc` | 3 | 900 / 600 | 5 (4.5) |
| `exported_symbols` | 0 | 55 / 60 | 0 |
| `internal_imports` | 1 | 8 / 10 | 1 (0.8) |
| `changed_func_cognitive_max` | 5 | 55 / 50 | 5 (5.5) |

`changed_func_cognitive_max` was left out of the TypeScript override when the
per-function ratchet landed; it now carries the Go value, since the ceiling
differs by 10% only. The sweep over ai-shell reports no effect for any of
these settings, which is what a corpus with no over-ceiling row should show.
