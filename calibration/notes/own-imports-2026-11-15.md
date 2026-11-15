# Own sub-packages in `internal_imports`

Date: 2026-11-15. Fields: `internal_imports_descendants` and
`internal_imports_private`, added the same day (SPEC.md 6 and 6.5). Data:
`calibration/data/2026-11-15-own-imports/packages.jsonl` (2,557 package
rows: corpus, standard library, this repository, replayed repositories)
and `carveouts.jsonl` (the 32 carve-outs of this repository's history).
Labels: the hand labels and split-or-extraction labels of SPEC.md 11.4,
read from the stored rows. No replay was run.

## Question

Two capacity ceilings pull against each other. `tokens_est` (max 16,000)
says to split a package that has grown too large, and the idiomatic Go
split puts the carved-out code in a child, `<pkg>/internal/<child>` or
`<pkg>/<child>`, that the parent imports. `internal_imports` (max 10,
`severity: warn` in the default, `fail` in at least one real
configuration) counts each such child as fan-out, so obeying the first
ceiling pushes the package towards the second. The rationale of
`internal_imports` (SPEC.md 4) is coupled facts absent from context: each
import is another package the next agent must know. Is a package's own
child foreign coupling, and does a rule that does not count it do better?

## Definitions

Both are counted in the Go extractor's `imports` pass, from the edges
`internal_imports` already counts (no second walk, no reload):

- **(b) `internal_imports_descendants`**: internal imports whose path lies
  below the package's own, `<pkg>/...`, private or public, any depth.
- **(a) `internal_imports_private`**: of those, the ones at or below
  `<pkg>/internal`, which the Go toolchain lets only `<pkg>` and its
  descendants import. `<pkg>/x/internal/y` is a descendant of `<pkg>` but
  not private to it.

Every package of a module lies below its root package, so for the root (b)
is every internal import and (a) the whole `<module>/internal` tree. The
TypeScript extractor counts (b) by package directory (every other package
for the root `.`) and reports (a) null: no TypeScript rule makes a
directory private to its parent. The candidate values are
`internal_imports` minus (a) and minus (b).

## Method

**Corpus.** The 36 cloned modules of `calibration/corpus.yaml` were
shallow-cloned at their pins (457 MB). Loading them with types, as the
collector does, needs every dependency built (gitea, goreleaser, opa and
helm alone pull several GB of modules), so the counts were recounted from
each corpus row's package with `go/build` instead: the import clauses of
its non-test files under the build constraints of the 2026-09-28
collection (darwin/arm64, cgo on), classified internal by the module path
of the clone's `go.mod`. The recount's `internal_imports` equals the
stored value on all 1,989 cloned-module rows. The new extractor, run on
five of the clones (go-cmp, toml, zerolog, bbolt, viper: 51 packages, 8
with own imports), agrees with the recount on all three counts for every
package. The rule does not judge `main` packages (`roles`), so the 86
packages named `main` are left out, leaving 1,903; the test-support role
cannot be told from the rows, so those packages stay in.

**Standard library.** `calibration/collect --stdlib` with the new
extractor (go1.27.0, linux/amd64): 360 packages, one load of `std` in
which every standard-library import is internal, as in 11.1. The rule's
`max` is fitted from cloned modules only, so these rows are context.

**This repository.** `astimate baseline write` with the new binary at
545124c plus this change, roles from `rank --json`: 88 packages, 80 not
`main`.

**Replays.** The stored rows carry only the total. For each Go replay the
packages over 10 at the end of its range were recounted: from shallow
clones of the last replayed commit for crush, beads, github-mcp-server and
roborev; from the local working trees for go-starter and shhh, whose HEAD
has moved past the replay, keeping only the packages whose recount equals
the replay's last `internal_imports`. ztp's remote is private and was not
read. That leaves 14 comparable packages over 10 that are not `main`.

**Splits.** The `split` parts of the split-or-extraction labels (11.3) of
the eight Go replays (1,274 commits) name the split commits. Since they
name only two, the first-parent history of this repository (348 commits)
was also scanned for every commit that creates a package below an existing
one, with the parent's imports read before and after from the tree, and
the internal imports of every other package compared across the commit.

## Distribution

| Population | Packages | Import a descendant (b) | Import an own `internal` (a) | Over 10 | Of those, 10 or under without (a) | Without (b) |
| --- | --- | --- | --- | --- | --- | --- |
| Corpus clones, not `main` | 1,903 | 164 (8.6%) | 48 (2.5%) | 208 | 5 (2.4%) | 20 (9.6%) |
| Standard library | 360 | 21 (5.8%) | 8 (2.2%) | 85 | 0 | 1 (1.2%) |
| This repository, not `main` | 80 | 10 (12.5%) | 6 (7.5%) | 3 | 2 | 2 |
| Replayed repositories, not `main`, over 10 | 14 | 3 | 0 | 14 | 0 | 1 |

Counting at or over 10 instead (the rule fails above 10): 250 corpus
packages, of which 7 (2.8%) fall below 10 without (a) and 20 (8.0%)
without (b).

Edges: of the 9,278 internal imports of the 1,903 corpus packages, 637
(6.9%) are descendants and 144 (1.6%) private. Of the 4,383 of the 208
packages over 10, 397 (9.1%) are descendants and 94 (2.1%) private; 58 of
the 208 import some descendant and 18 some private package.

Who (b) brings under the ceiling: the command groups of cli/cli
(`pkg/cmd/issue`, `pr`, `project`, `release`, `repo`, which import 10 to
19 of their own subcommands out of 11 to 20), gitea's `modelmigration` (24
of 27, one child per migration version) and `routers/api/packages` (23 of
33, one per package format), nats' `server` (12 of 15), eight grpc, opa
and gitea packages with one to four own children, and four module roots
(pebble 55 of 55, grpc 44 of 44, fiber 14 of 14, go-redis 13 of 13). (a)
brings two roots under (fiber 12 private of 14, go-redis 10 of 13) and
three grpc packages with one or two private children each.

Module roots carry much of (a): 14 of the 48 corpus packages with a private
import are a module's root package, whose `internal` tree is the whole
module's.

The fit is unchanged. Pooled over the 1,989 cloned-module rows as 11.1
pools them, the 90th percentile is 11 for `internal_imports`, 11 without
(a) and 10 without (b); each rounds to a `max` of 10 by the steps of 11.1.
A rule on either candidate would ship the same ceiling.

In this repository the own children are a large share where the ceiling
bites: `internal/engine` has 16 internal imports, 6 of them its private
children (`coverage`, `detect`, `judge`, `narration`, `ranking`,
`selection`; `surface` reaches it through `judge`, so it is not one of
engine's own imports), and `internal/lang/golang` 11, 6 private (`cover`,
`dup`, `imports`, `inspect`, `load`, `tests`). Without them engine is at
10, which passes, and golang at 5. `internal/engine/internal/judge` is at
11 with no children and stays there. 7.5% of this repository's packages
import a private child against 2.5% of the corpus: the gate itself drove
the carve-outs.

## Gate effect

The stored rows cannot evaluate a candidate: every row predates the
fields, which read null. Nor is there anything for one to move. The
`internal_imports` rule as shipped (`warn`, `roles`, `over_max_delta` 1)
fired on none of the 152 hand-labelled agent commits in any view
(`calibration/reports/gate-validation-2026-10-14.md`), under the hand
labels or the split-or-extraction labels. Its breaches in the 2026-10-14
rows are go-starter `internal/app` 11 to 13 (93137e7, not agent-authored,
so dropped) and ztp `internal/broker` new at 12 (130d2c8, whose baseline
did not load, so set aside). The "one `allow` commit" of SPEC.md 4 is ztp
fbb05c0, `cmd/broker` 20 to 21 in the 2026-10-13 rows, a `main` package the
roles have since taken out (`calibration/notes/package-roles-2026-10-14.md`);
SPEC.md 8.2 already reads "never fired".

`internal_imports` minus either count is never above `internal_imports`,
so at the same `max` the candidate is over the ceiling only where the total
is. It can gain no recall, and the false failure it could spare is already
zero. (The ratchet compares head with baseline, so a commit that drops an
own child while adding a foreign import could in principle cross on the
candidate and not on the total; none of the replayed packages over 10 is
in that position, since only three of the fourteen import a descendant at
all.) What can be concluded from the labels: nothing about the candidate's
precision or recall, and no measurement on these corpora can change that.
A re-replay would put the fields on the rows and still score 0 of 0, so it
was not run.

## The split evidence

**From the labels.** The split-or-extraction labels name a `split` 12
times, all in this repository's 2026-09-28 replay, for two split commits.
Every other label of the eight Go replays is an extraction.

| Split commit | Package | SLOC before, after | `internal_imports` before, after | Own private children added |
| --- | --- | --- | --- | --- |
| 9a10fd6 | `internal/lang/golang` | 3,094, 670 | 2, 8 | 6 (`cover`, `dup`, `imports`, `inspect`, `load`, `tests`) |
| 46ffb57 | `internal/lang/typescript` | 2,346, 611 | 2, 4 | 2 of the 3 created (`inspect`, `resolve`; `walk` is imported by `inspect`) |

Both splits added own-descendant imports to the parent, all private; neither
took it to 10. Two events from one repository is the whole labelled sample.

**From this repository's history.** 29 commits created a package below an
existing one, 32 carve-outs counting each parent once. 18 of the carve-outs
were size-driven: the parent was in the
`tokens_est` warning band (12,000 estimated tokens or more, bytes / 3.2) and
shrank. Of the 18:

- 15 added own-descendant imports to the parent, 31 in all, 28 of them
  private. One took the parent to 10 (037bace: engine 8 to 10, adding
  `coverage` and `selection`); none took a parent over 10. Engine was
  already at 16 when it added `ranking` (01bb6aa, 16 to 17), and the carve-out
  of `narration` and `detect` lowered it (2c2ddba, 17 to 15: two own
  children in, four foreign imports moved out with the code).
- 5 raised the `internal_imports` of other packages that import both
  halves, 23 times (24 imports in all): `report` into `report/rows` and
  `report/details`, `gate` into `gate/exempt`, `score` into
  `score/suggestion`, `metrics` into `metrics/explain` and
  `metrics/inventory`. The last (3b6862e) took `internal/lang/golang` and
  `internal/engine/internal/judge` from 10 to 11: the only two crossings a
  carve-out caused, both on the importer side.

How the three packages over the ceiling today got there:

- `internal/engine` reached 10 with its own children (037bace) and crossed
  with a feature, churn (0ac0198, 10 to 11); then the `report` split (+2,
  importer side), the module report (+2, a feature), its Markdown renderer
  (+1), `ranking` (+1, own), the `narration` and `detect` carve-out (net
  −2) and `metrics/inventory` (+1, importer side): 16.
- `internal/lang/golang` went from 2 to 8 with its own split, then +1
  `metrics/role`, +1 `exclude` and +1 `metrics/inventory` (importer side):
  11.
- `internal/engine/internal/judge` started at 6 and gained `report/details`,
  `gate/exempt`, `score/suggestion` and `metrics/inventory`, each the
  importer side of a split, and `engine/internal/surface`, a sibling: 11.

So the ceilings do fight, on two fronts. On the parent side, own children
are 6 of engine's 16 and 6 of golang's 11, which the candidate addresses.
On the importer side, a package that used one package now imports two or
three halves of it; that is what pushed golang and judge over, and neither
field counts it. The field report of a package split three ways was not
measured here.

## Recommendation

Keep counting own sub-packages in `internal_imports`, and report the split
(the two fields, shipped with this note). Do not adopt (b). (a) is the only
defensible variant, and the evidence for it is weak.

- **For not counting (a).** A private child is part of the same unit by the
  toolchain's own rule: nothing outside the parent's tree can import it, so
  it is not coupling another package can see. In this repository it would
  clear engine and golang, two of the three packages over the ceiling,
  and the agent instruction to split under `tokens_est` would stop costing
  `internal_imports`.
- **Against.** In the corpus it matters for 5 of 208 packages over the
  ceiling, in the standard library for none of 85, in the replayed
  repositories for none of 14; the fitted `max` does not move; the labels
  cannot measure it; and the crossings a split caused here came from the
  importer side, which it does not touch. A parent that imports six
  children must still be read with them to change behaviour across them.
  The rebuild tree experiments (SPEC.md 11.2) rebuilt a directory with its
  sub-packages as one unit and found its cost grows with the members'
  summed volume and no faster (the exponent clamps to 1 on the 14
  whole-tree runs): splitting moves volume into children without shrinking
  what an agent
  changing the family must hold. A rule that ignores children can be gamed
  by nesting: under (b), moving every sibling below one parent, or the
  module root, zeroes its count; under (a), moving siblings below
  `<pkg>/internal/` does the same, which at least narrows their visibility,
  but the module root gets the whole `internal` tree free (14 of the 48
  corpus packages with a private import are roots).
- **(b) specifically** exempts public children, which the rest of the
  module can and does import, and every import of a root package.

How strong: weak. The distribution is measured on 1,903 corpus packages,
360 standard-library packages and this repository, all three by the shipped
definition and cross-checked, but it describes code not written under these
ceilings. The gate effect is unmeasurable (0 firings on the hand labels).
The split evidence is two labelled events plus the history of one
repository, the one whose own gate created the pattern.

Missing evidence, in the order it would help: labelled commits where
`internal_imports` fires (this repository's history, or the configuration
that runs it at `fail`); replays carrying the new fields, so a candidate
can be scored; and a measurement of the importer side, for instance
counting the members of one split family (`report`, `report/rows`,
`report/details`) once, which is the larger cost here and is not what the
question proposed.

A smaller step that needs no rule change: the `internal_imports` finding
could name the own share ("16 internal imports, 6 of them its own internal
packages"), so the agent and the reviewer see that a split, not new
coupling, made the count. That is wording, not a verdict, and is left for
a later change.

## Candidate configuration

None is written under `calibration/candidates/`: the machinery cannot
express the rule. A gate rule judges one section 6 field (`gate.Evaluate`
reads `RawMetrics.Value(metric)`), no field holds `internal_imports` minus
a subset, and every replayed row holds the subsets null, so `validate
--candidate` would measure a rule on them over no row. Measuring one would
need, first, either a field equal to the candidate (for instance
`internal_imports_foreign`, `internal_imports` minus
`internal_imports_private`) or a rule option on `internal_imports` that
subtracts one of the new fields, read by the gate, `config` and
`calibration/validate` alike; second, replays with the new extractor (the
four public 2026-09-28 corpora and crush can be replayed from their remotes,
go-starter and shhh locally, ztp only by its owner); and third, labels on
commits where the rule fires, which none of the current hand-labelled
corpora have.

## Resources

Shallow clones of the 36 corpus modules (457 MB) and of four replayed
repositories at their last commit (46 MB), a build cache of 213 MB for the
five extractor cross-checks and the standard-library collector's temporary
cache, all under one directory in `$HOME`, removed afterwards. The module
cache was 8.7 GB before and after. Network: the 40 shallow fetches, about
500 MB.
