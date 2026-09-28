# Reference corpus

Date: 2026-09-27. Machine: darwin/arm64, 14 cores, Go 1.27.1.

SPEC.md 11.1 calibrates the gate's default thresholds from the pooled
metrics of well-regarded Go code: the standard library plus 20 or more
widely used modules. `corpus.yaml` lists the modules; `collect/` gathers
their metrics into `data/<date>/`.

## Selection criteria

A module is in the corpus when all of these hold:

1. **Widely used.** Top of the ecosystem by GitHub stars or by dependents on
   pkg.go.dev: roughly 5k stars or more, or a dependent count that puts it
   among the most imported Go modules. The `stars_or_dependents` figures in
   `corpus.yaml` are the author's approximate recollection and must be
   re-verified before the first pin.
2. **Actively maintained.** At least one commit on the default branch within
   six months of the run date. Check this when pinning; the pin is the
   default branch's HEAD, so its commit date is the evidence.
3. **Permissively licensed.** MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0 or
   MPL-2.0. The collector's schema check rejects anything else.
4. **Not generated-heavy.** Repositories that are mostly protobuf or
   OpenAPI output, or that commit vendored trees, measure a generator rather
   than people. The standard library's `vendor/` packages are left out for
   the same reason.
5. **Builds with `CGO_ENABLED=0`.** A package whose substance is C measures
   poorly from its Go files.
6. **Spread across domains.** Web frameworks, CLI and terminal libraries,
   database drivers and storage engines, logging and testing, parsers,
   infrastructure servers and applications, cloud SDK pieces and
   cryptography, and sizes from single-package libraries to applications
   with hundreds of packages.

The root module of each repository is collected; nested modules in the same
repository are not.

### Considered and excluded

| Module | Reason |
| --- | --- |
| `github.com/golang/protobuf`, `google.golang.org/protobuf` | generated-heavy |
| `github.com/aws/aws-sdk-go-v2` | generated service clients; its runtime `smithy-go` is in instead |
| `k8s.io/kubernetes` | too large, generated-heavy, vendored |
| `github.com/mattn/go-sqlite3` | cgo |
| `github.com/minio/minio`, `github.com/grafana/grafana` | AGPL-3.0 |
| `github.com/hashicorp/terraform`, `vault`, `consul` | BUSL-1.1 |
| `github.com/sirupsen/logrus` | maintenance mode, little recent activity |
| `golang.org/x/text`, `golang.org/x/net` | large generated tables (`x/crypto` is in) |

## Schema

`corpus.yaml` has a `note` and a `modules` list. Each entry has `module`,
`repo`, `commit`, `license` (SPDX), `stars_or_dependents` and `reason`. The
standard library is the single entry with `local: true` and no repo or
commit; it is measured from the collector's own GOROOT, so its version is
the Go version in `run.json`. `go test ./calibration/collect` validates the
file: at least 20 cloned modules plus the standard library, unique modules,
permissive licenses, required fields, and each commit empty or a full
40-character hash.

## Pins and reproducibility

The commits ship empty because the corpus was assembled without network
access. The first full run pins them: `--pin` resolves each empty commit to
the repository's HEAD with `git ls-remote` and writes it into `corpus.yaml`
in place, touching nothing else. Commit the pinned file with the data it
produced. After that, a run fetches exactly the pinned commit of each
repository (`git fetch --depth 1 <repo> <commit>` into a temporary
directory), and the collector refuses to run a module without a pin. To
move a module forward, clear its commit and pin again.

## Running the collection

From the repository root, with network access (the clones and each module's
dependencies are downloaded):

```sh
go run ./calibration/collect --pin && go run ./calibration/collect --out calibration/data/$(date +%F)
```

Other forms:

- `go run ./calibration/collect --stdlib` collects only the standard
  library, in-process with `golang.ExtractStdlibAll`, one load of the
  pattern `std`, keeping the packages of `go list std`; no network.
- `go run ./calibration/collect --only <module>` collects one corpus entry.
- `go run ./calibration/collect --modules-only` collects only each cloned
  module's module row into `modules.jsonl`, with no package rows and
  nothing from the standard library; it needs the clones but skips the
  per-package extraction.

The collector ranks each module the way `astimate rank --json` does, with
one module load for all packages and the embedded default configuration (a
cloned repository's own `astimate.yaml` is ignored, and `GOWORK=off`). It
exits 1 when any module or package failed, after writing everything that
succeeded; `run.json` lists the failures.

## Output

`data/<date>/packages.jsonl` has one JSON object per package:

| Field | Meaning |
| --- | --- |
| `module` | module path from the collected `go.mod`, or `std` |
| `commit` | the pinned commit, or the Go version for `std` |
| `package` | import path |
| `metrics` | the package's `RawMetrics` |
| `agent_passes`, `human_days` | the rebuild estimate, rounded as `rank` rounds it |
| `func_cognitive` | the package's functions counted by cognitive complexity, value to count (`{"0": 12, "3": 4}`), absent for a package with no functions; data from 2026-09-28 on |

`data/<date>/modules.jsonl` has one JSON object per cloned module, its
module-level row (SPEC.md 8.1), written after the module's load and before
any package is extracted:

| Field | Meaning |
| --- | --- |
| `module`, `commit` | as in `packages.jsonl` |
| `package` | always `<module>` |
| `metrics` | the module row's `RawMetrics`: v0 fields 0, v1 fields null except `dup_blocks_cross_pkg`, the module's distinct cross-package blocks |
| `packages` | the module packages the module pass streamed |
| `cost` | `load_ms`, the module load's wall time; `pass_ms`, the module pass's alone; `pass_alloc_bytes`, what the pass allocated; `pass_peak_heap_bytes`, the highest heap sampled during the pass above the heap after a collection just before it |

The standard library has no module row: `dup_blocks_cross_pkg` is null for
its loads. `modules.jsonl` is written by every run that measured a module
row; data from before 2026-09-28 has none.

`data/<date>/run.json` records the date, Go version, platform and CPU count,
the astimate version and commit, the `config_version` the estimates were
made under, the tokenizer, the number of rows in each file, and per module the commit, package count, failed
packages and any error. A module whose `go.mod` path differs from the corpus
entry (a new major version on the default branch, for instance) is
collected under the `go.mod` path and flagged with `go_mod_path`; update
the corpus entry to match.

## Known limits

- **The standard library is one module.** It is loaded once, so `fan_in`
  and `fan_in_tests` count the standard-library packages importing each
  one, as a module rank counts module packages. Every standard-library
  import is therefore `internal_imports`, and `stdlib_imports` is 0 on
  every `std` row; the cloned modules' rows count the standard library in
  `stdlib_imports` instead. Pool `internal_imports` and `stdlib_imports`
  with that in mind. `fan_in` and `internal_imports` count the same edges,
  as in any module: blank and dot imports count on both sides, imports only
  cgo-generated files hold on neither, and a vendored `golang.org/x`
  package is keyed by its package path on both, so their sums over the
  library are equal (2,783 each on the 2026-09-27 toolchain). The data
  under `data/2026-09-27/` predates that rule: there `fan_in` counted 69
  edges `internal_imports` did not and missed 44 it did, of about 2,700.
- **Popularity figures are unverified.** See criterion 1.

## Current data

`data/2026-09-28-corpus/` is the current run: the same 2,347 packages from
the standard library and the 36 cloned modules at the same pins, Go 1.27.1,
astimate `8eea3d2`, with each row's `func_cognitive` counts added (80,280
functions in 2,158 packages; 189 packages have none). Every other field of
every row is identical to `data/2026-09-27-corpus/`; `run.json` differs only
in the date, the astimate commit, the `config_version` the estimates were
made under and the three module paths the corpus has since renamed.

`data/2026-09-28-modules/` holds the module rows of the same 36 cloned
modules at the same pins (`--modules-only`, so no package rows), with the
cost of each module pass; `calibration/notes/cross-package-duplication-2026-09-28.md`
reads it, with a one-off measurement of the pass on the standard library.

`data/2026-09-27-corpus/` is the first full run: 2,347 packages from the
standard library and all 36 cloned modules at the commits pinned in
`corpus.yaml` on 2026-09-27, no failures, Go 1.27.1, astimate `f64280f`
(after the equal fan-in/fan-out edges, typed abstractness, rounded ratios,
opacity flags and the generated-file rule). The largest modules are
`gitea.dev` (384 packages), `std` (358), `github.com/cli/cli/v2` (309),
`google.golang.org/grpc` (261) and `github.com/open-policy-agent/opa` (257).
Three default branches had moved to a new module path since the corpus was
written, and the entries now match their `go.mod`: `github.com/labstack/echo/v5`,
`charm.land/bubbletea/v2` and `gitea.dev`. The six-month activity rule was
not checked commit by commit; every pin is the default branch's HEAD on the
run date. Gitea also contains TypeScript; the collector took the Go
extractor, as its warning says.

`data/2026-09-27/` is the earlier standard-library-only run (358 packages)
under the older counting rules and is kept for comparison.

## Fitting thresholds

`fit/` derives candidate thresholds from the pooled rows per SPEC.md 11.1:

```sh
go run ./calibration/fit --data calibration/data/<date>/packages.jsonl --date <date>
ASTIMATE_CONFIG=$PWD/calibration/thresholds/astimate-thresholds-<version>.yaml go test ./internal/invariants
```

It writes `thresholds/astimate-thresholds-<version>.yaml`, the embedded
default with each rule's `max` and `max_delta` refitted, and
`reports/thresholds-<version>.md`, each gated metric's percentiles,
histogram and chosen limits, with the rows that fed each metric. The
fitting rules are stated in both files: a rule whose base `max_delta` is 0
keeps it, and `internal_imports` is pooled from the cloned modules' rows
only (see Known limits). While every row is from the standard library the
version carries a `-stdlib-provisional` suffix;
`thresholds-2026-09-27-stdlib-provisional` is such a candidate, fitted from
`data/2026-09-27/`, and is kept for comparison only: it predates the
zero-tolerance and `internal_imports` rules, so its `max_delta` values on
the zero-tolerance ratchets (1 to 3) are not what the fitter now produces.

`thresholds-2026-09-28` is the fit of `data/2026-09-28-corpus/` and the
shipped default: `internal/config/default.yaml` carries its rules and
`config_version` (a test in `fit/` keeps them equal), and
`configs/uncalibrated.yaml` keeps the placeholders it replaced. It was
fitted with that file as the base, so the report's before and after table
compares against the placeholders, and `--compare` adds a table against the
previous default, `thresholds-2026-09-27`:

```sh
go run ./calibration/fit --data calibration/data/2026-09-28-corpus/packages.jsonl \
  --modules calibration/data/2026-09-28-modules/modules.jsonl --date 2026-09-28 \
  --base configs/uncalibrated.yaml --compare calibration/thresholds/astimate-thresholds-2026-09-27.yaml
```

It reproduces every limit of `thresholds-2026-09-27`, the same fit of the
same rows without per-function counts, except `changed_func_cognitive_max`.
That metric is a diff against a baseline, so no row carries it; the fitter
pools the rows' `func_cognitive` counts, every function counted as new, and
sets the `max` at the 99th percentile (51, rounded to 50) instead of the
90th, since one function past the corpus's own worst percentile is the
signal. Fitting data without the counts keeps the base value, as
`thresholds-2026-09-27` kept the placeholder of 30.

`dup_blocks_cross_pkg` is module-wide, so `--modules` pools it from the
module rows alone, one per module, and not from the per-package counts the
package rows carry. Its rule keeps `max_delta` 0 by policy, like
`dup_blocks`, and its `max`, which judges a module with no baseline, is the
p90 of the 36 module rows (443, rounded to 450). The rule was added to the
default on 2026-09-28 after the other limits of `thresholds-2026-09-28`
shipped; the version's numbers now include it. `configs/uncalibrated.yaml`
carries it with a placeholder `max` of 0, so a fit without module rows keeps
0 and the report says the rows are missing.
