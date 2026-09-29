# Split-or-extraction labels

Date: 2026-09-29. Labels: `calibration/replay/labels/<corpus>-split-extract.yaml`
for `astimate`, `roborev`, `github-mcp-server` and `beads`. Report:
`calibration/reports/gate-validation-2026-09-29.md`.

## Why a second rule

The fix-up rule (`calibration/notes/agent-commits-corpus-2026-09-28.md`)
marks a commit `block` when a later fix changed a function it changed.
All 123 external `block` labels scored in the 2026-09-28 validation are
fix-ups, none a revert: they measure defects, which the gate does not
claim to catch. The hand labels of this repository used the definition the
gate does claim: a later commit had to split the package this commit grew,
or extract the code it copied. `--rule split-extract` automates that
definition from the replay rows alone (SPEC.md 11.3,
`calibration/replay/README.md`, "Split-or-extraction labels"). The fix-up
labels stay, unchanged, as the first label set.

```sh
go run ./calibration/replay/label --rule split-extract \
  --data calibration/data/replay-astimate-2026-09-28 \
  --out calibration/replay/labels/astimate-split-extract.yaml
go run ./calibration/replay/label --rule split-extract \
  --data calibration/data/replay-<name>-2026-09-28 \
  --out calibration/replay/labels/<name>-split-extract.yaml \
  --corpus calibration/corpus-commits.yaml --name <name>
```

The replays ran under `thresholds-2026-09-28`, so the capacity rules come
from `calibration/thresholds/astimate-thresholds-2026-09-28.yaml`
(`sloc` 1000, `tokens_est` 16000, `largest_file_sloc` 600,
`exported_symbols` 60, `internal_imports` 10).

## Choices made while building it

- **No window.** A split or an extraction lands far later than a fix-up:
  on this repository the median `block` label's later commit is 96 commits
  on, and the Go extractor split (`9a10fd6`) comes 182 commits after the
  first commit that took `internal/lang/golang` over a ceiling (`a13cfa2`).
  A first draft with a 40-commit window reproduced 5 of the 25 hand
  labels below. Both parts look
  through the rest of the range; `--window` remains for experiments.
- **Touch requirement on extraction.** The later commit must change a file
  directly in the package's directory (for the module row's
  `dup_blocks_cross_pkg`, in a package the labeled commit changed).
  Without it a duplicate count that drifts down after an edit elsewhere
  counted as an extraction. Its effect on the external `block` counts:

  | Corpus | Window 40, no touch | Window 40, touch | Rest of range, touch (shipped) |
  | --- | --- | --- | --- |
  | roborev | 40 | 40 | 52 |
  | github-mcp-server | 40 | 38 | 51 |
  | beads | 56 | 49 | 58 |

  The touch rule removes few labels. What it cannot remove is a later
  commit that edits the package and happens to remove one or two duplicate
  blocks; see "What the rule gets wrong".
- **Censoring.** Every reason ends with `(lookahead N commits)`. The last
  commits of a range see few later commits, so an `allow` there says
  nothing was undone yet, not that nothing will be.

## Counts

All commits of each range; the agent-scored columns are what the
validation scores (agent-authored and loaded).

| Corpus | Commits | `block` | part `split` | part `extract` | both | `allow` | `block` scored | `allow` scored |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| astimate | 222 | 30 | 12 | 23 | 5 | 192 | 30 | 184 |
| roborev | 272 | 52 | 0 | 52 | 0 | 220 | 51 | 176 |
| github-mcp-server | 270 | 51 | 0 | 51 | 0 | 219 | 30 | 104 |
| beads | 270 | 58 | 0 | 58 | 0 | 212 | 43 | 84 |

How far away the undoing commit is, for `block` labels (the nearest when
both parts fired):

| Corpus | Median distance | Largest | Beyond 20 commits | Beyond 40 commits |
| --- | --- | --- | --- | --- |
| astimate | 96 | 182 | 23 of 30 | 19 of 30 |
| roborev | 24 | 179 | 27 of 52 | 12 of 52 |
| github-mcp-server | 11 | 249 | 19 of 51 | 13 of 51 |
| beads | 15 | 114 | 19 of 58 | 9 of 58 |

No external repository split a package in its range: every external
`block` label is an extraction. Two of the three are squash-merged, and a
split that happens inside a pull request lands as one commit that adds
the new packages and shrinks the old one; the rule sees that as a split
only if the old package was over a ceiling before it.

## Agreement with the fix-up rule

Rows are the fix-up labels (hand labels for `astimate`), columns the
split-or-extraction labels, over every commit of each range.

| Corpus | both `block` | fix-up only | split-or-extraction only | both `allow` |
| --- | --- | --- | --- | --- |
| astimate (hand) | 16 | 14 | 14 | 178 |
| roborev | 28 | 48 | 24 | 172 |
| github-mcp-server | 17 | 28 | 34 | 191 |
| beads | 16 | 36 | 42 | 176 |

The two rules mostly disagree on the external corpora: of 173 fix-up
`block` labels, 61 are also split-or-extraction `block`. They measure
different things, which is why both label sets are reported.

## Agreement with the hand labels

The hand labels (`calibration/notes/astimate-labels-2026-09-28.md`) have
30 `block` labels: 25 commits labeled for capacity or copy-paste (18
capacity and 8 copy-paste, `66b5a08` both), and 5 for a fix-up alone.

### Capacity and copy-paste labels whose cited later commit is in the range

12 labels cite a split or extraction inside the replayed range. The rule
labels all 12 `block` (12 of 12, 100%), and for each its `split` evidence
names the commit the hand label cites:

| Commit | Hand label cites | Rule |
| --- | --- | --- |
| `a13cfa2` | `internal/lang/golang` split by `9a10fd6` | split by `9a10fd6` |
| `2fca91e` | the same | split by `9a10fd6` |
| `be9ac98` | the same (and a fix-up) | split by `9a10fd6`; extract by `4132fa0` |
| `5e351a8` | the same (and a fix-up) | split by `9a10fd6`; extract by `4132fa0` |
| `2509490` | the same | split by `9a10fd6` |
| `2f4de4c` | the same | split by `9a10fd6`; extract by `66b5a08` |
| `6e8312f` | the same | split by `9a10fd6` |
| `ab2d630` | the same, and `internal/engine` split by `9843e5c` | split by `9a10fd6`; extract by `9a10fd6` |
| `66b5a08` | `internal/lang/typescript` split by `46ffb57`; duptok unified by `4132fa0` | split by `46ffb57` |
| `2b0a320` | `internal/lang/typescript` split by `46ffb57` (and a fix-up) | split by `46ffb57`; extract by `46ffb57` |
| `3f01734` | the same | split by `46ffb57` |
| `7c90b60` | the same | split by `46ffb57` |

One part of one label is not reproduced: `66b5a08` wrote `duptok` as a
second copy of the Go duplicate finder, which `4132fa0` unified. `66b5a08`
raised the module's `dup_blocks_cross_pkg` by 25 (13 to 38), most of it
the new TypeScript extractor's own blocks; `4132fa0` removed 7 (40 to 33).
The extraction part asks one later commit to remove at least what the
commit added, so it does not fire, and the label is `block` by its split
alone.

### Labels whose cited later commit is past the range end

The replay ends at `5e8c777`. 13 labels cite only `9843e5c` ("refactor:
bring every package under its own ceilings"), five first-parent commits
later, so no rule reading these rows can see the commit they cite.
Extending the replay is a separate change. Four of them are `block` anyway,
by an unrelated in-range removal of duplicate blocks, which is not the
extraction the hand label means:

| Commit | Hand label cites | Rule |
| --- | --- | --- |
| `c5564d8` | `internal/metrics` per-field code, replaced by `9843e5c` | `block`: extract by `6e8312f` (one cross-package block, 109 commits later) |
| `688b5ea` | `cmd/astimate` flag and logger setup, extracted by `9843e5c` | `block`: extract by `2251316` (one cross-package block) |
| `e10a0f5` | the same | `block`: extract by `e25b497` (`internal/report`, 130 commits later) |
| `49adff2` | the same | `block`: extract by `2251316` (one cross-package block) |
| `399af8c` | the same | `allow`: 3 duplicate blocks added to `cmd/astimate`, none removed in range |
| `8915b89` | the same | `allow`: the same, 3 blocks |
| `b69ea5f` | `internal/metrics` per-field code, replaced by `9843e5c` | `allow`: 11 blocks added to `internal/metrics`, none removed in range |
| `e1623a4` | `internal/engine` split by `9843e5c` | `allow`: took `internal/engine` over `sloc` and `tokens_est`, not split in range |
| `3f6294f` | the same | `allow`: took it over `largest_file_sloc`, not split in range |
| `6052227` | `internal/metrics` replaced by `9843e5c` | `allow`: took it over `tokens_est`, not split in range |
| `d345888` | `calibration/rebuild` split by `9843e5c` | `allow`: took it over `sloc`; lookahead 17 |
| `b8fd067` | `calibration/fit` and `calibration/collect` split by `9843e5c` | `allow`: took both over ceilings; lookahead 15 |
| `9da995a` | `calibration/rebuild` split by `9843e5c` | `allow`: doubled it; lookahead 13 |

Each of the nine `allow` labels records the growth the hand label cites,
and would be `block` if the range reached `9843e5c`.

### The other disagreements

The 5 hand `block` labels resting on a fix-up alone (`faccda1`, `635a95a`,
`b241edc`, `4ac37dc`, `4f5b236`) are defects, not growth or copies; the
rule labels them `allow`, as it should.

The rule labels 14 commits `block` that the hand labels `allow`. Every one
is an extraction of one to three duplicate blocks, removed by a later
commit that edited the same package for other reasons:

| Commit | Rule | Hand label |
| --- | --- | --- |
| `f3bd7df` | 1 block in `internal/score`, removed by `ab2d630` 164 commits later | legitimate growth; 3 new blocks not judged copy-paste |
| `3349b44` | 2 blocks in `internal/score`, removed by `ab2d630` | the same, 2 blocks |
| `8a725f9` | 1 block in `internal/lang/golang`, removed by `4132fa0` | the same, 1 block |
| `52ac174` | the same | the same |
| `a932f2c` | the same | the same |
| `3c19af6` | the same | the same; growth under 100 SLOC over a ceiling |
| `8cd4c35` | the same | a targeted fix |
| `cc7a0d1` | the same | a targeted fix |
| `1a42ce0` | 1 block in `internal/engine`, removed by `66b5a08` | legitimate growth |
| `e420e4c` | 2 blocks in `internal/report`, removed by `e25b497` | legitimate growth |
| `8eaa6f0` | 1 block in `internal/score`, removed by `ab2d630` | legitimate growth |
| `80cf58a` | 2 cross-package blocks, removed by `09146a4` | legitimate growth |
| `176ca48` | 3 cross-package blocks, removed by `9a10fd6` | tests and test support |
| `b1612df` | 2 cross-package blocks, removed by `9a10fd6` | legitimate growth |

`4132fa0` rewrote Go duplicate detection, so it removed duplicate blocks
across `internal/lang/golang` by changing the counting, not by extracting
the copies; the six labels it undoes are artifacts of that. `9a10fd6` split
the package and moved code, and cross-package counts fall when a package
is split. These are the rule's main source of false `block` labels here.

## Validation under both label sets

```sh
go run ./calibration/validate --date 2026-09-29 \
  --corpus astimate=calibration/data/replay-astimate-2026-09-28:calibration/replay/labels/astimate.yaml,calibration/replay/labels/astimate-split-extract.yaml \
  --corpus roborev=calibration/data/replay-roborev-2026-09-28:calibration/replay/labels/roborev.yaml,calibration/replay/labels/roborev-split-extract.yaml \
  --corpus github-mcp-server=calibration/data/replay-github-mcp-server-2026-09-28:calibration/replay/labels/github-mcp-server.yaml,calibration/replay/labels/github-mcp-server-split-extract.yaml \
  --corpus beads=calibration/data/replay-beads-2026-09-28:calibration/replay/labels/beads.yaml,calibration/replay/labels/beads-split-extract.yaml
```

From `calibration/reports/gate-validation-2026-09-29.md`, "Label sets", on
the all corpora pooled, capacity-only set aside view:

| Labels | Gate recall | Gate false failures | Size-only recall | Size-only false failures |
| --- | --- | --- | --- | --- |
| first (hand, fix-up) | 122 of 139 (87.8%) | 283 of 549 (51.5%) | 28.1% | 8.0% |
| second (split or extraction) | 146 of 146 (100.0%) | 259 of 542 (47.8%) | 27.4% | 7.9% |

Recall under the second set is 100% by construction: every
split-or-extraction `block` commit crossed a capacity `max`, grew a
package already over one, or raised a duplicate-block count, and the
shipped capacity and `dup_blocks` rules fail exactly those rises. The
number the second set measures is the false-failure rate: the gate fails
48% of the agent commits no later split or extraction undid. That is the
gate's real over-blocking on the definition it claims, and it is not much
lower than under the fix-up labels.

The replay rows were recorded before `globals` stopped counting sentinel
errors, `go:embed` variables and never-written basic-typed variables, so
the gate columns count `globals` violations under the old definition. The
split-or-extraction rule itself reads no `globals` value, and the rows
were not replayed again.

## What the rule gets wrong

- **Small duplicate counts undone by unrelated edits.** A later commit that
  rewrites the duplicate finder, splits a package, or refactors nearby code
  can lower a count by one or two without extracting the copy the labeled
  commit made. The touch requirement cannot tell these apart. See the 14
  rule-only labels above.
- **The module total misses untouched packages.** The total is rebuilt
  from rows, and a package no replayed commit changed has no row, so it is
  missing. That shrinks the denominator of the 5% (split) and 25%
  (cross-package extraction) tests, which makes them stricter than the
  definition. Recording the module's total `sloc` on the replay's module
  row would fix this; the replay format is unchanged here.
- **Deleting a package counts as a split** when the module total falls by
  less than 5%. A package moved to a new directory is a split by that
  test; a package deleted in a large module is too.
- **Censoring.** A commit near the end of a range, or one whose split came
  after the range (the 9 `9843e5c` labels above), is `allow`.
- **Cross-package duplicates have no location** in the rows, so the touch
  requirement for the module row accepts any change to any package the
  labeled commit changed.
