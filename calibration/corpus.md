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

`data/<date>/run.json` records the date, Go version, platform and CPU count,
the astimate version and commit, the `config_version` the estimates were
made under, the tokenizer, and per module the commit, package count, failed
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

`data/2026-09-27/` holds the standard-library portion only: 358 packages
(the 381 packages of `go list std` less 23 under `vendor/`), no failures,
Go 1.27.1, measured as one load, so `fan_in` is real: p50 2, p90 16,
maximum 164 (`errors`). As in a module rank, cgo packages are measured
from their Go source files, not the files cgo generates. The cloned
modules are still to be collected with the command above; the pooled data should reach at least 2,000 packages before the
SPEC.md 11.1 percentiles are derived from it.

## Fitting thresholds

`fit/` derives candidate thresholds from the pooled rows per SPEC.md 11.1:

```sh
go run ./calibration/fit --data calibration/data/<date>/packages.jsonl --date <date>
ASTIMATE_CONFIG=$PWD/calibration/thresholds/astimate-thresholds-<version>.yaml go test ./internal/invariants
```

It writes `thresholds/astimate-thresholds-<version>.yaml`, the embedded
default with each rule's `max` and `max_delta` refitted, and
`reports/thresholds-<version>.md`, each gated metric's percentiles,
histogram and chosen limits. The fitting rules are stated in both files.
While every row is from the standard library the version carries a
`-stdlib-provisional` suffix; `thresholds-2026-09-27-stdlib-provisional`
is such a candidate and is not the shipped default.
