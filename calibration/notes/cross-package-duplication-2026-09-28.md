# Cross-package duplication on the corpus and the standard library

Date: 2026-09-28. Machine: darwin/arm64, 14 cores, Go 1.27.1, astimate
`7fd4a45` plus the collector change that records module rows. Default
duplication options (`min_tokens` 40, `ignore_literal_only`, `fold_signs`).

`dup_blocks_cross_pkg` (SPEC.md section 6) is found by one module pass: every
package's non-test, non-generated files go into one normalized token stream
and one suffix array, and a block whose occurrences lie in two or more
packages counts once on the module row (`<module>`, SPEC.md 8.1). The
standard-library loads skip the pass and report null, on the assumption
that a whole-library suffix array would be too expensive. This note measures
what the pass costs, whether that assumption holds, and the module-row
distribution the default rule is fitted from.

## Method

- **Corpus modules.** `go run ./calibration/collect --modules-only --out
  calibration/data/2026-09-28-modules` clones each of the 36 modules of
  `calibration/corpus.yaml` at its pin, loads it once, and calls the Go
  extractor's `ModuleRow` right after the load, so the pass is timed alone.
  `modules.jsonl` holds one row per module: the module row's metrics, the
  package count, and the cost (`load_ms`, `pass_ms`, `pass_alloc_bytes`,
  `pass_peak_heap_bytes`). The heap figures come from `runtime/metrics`: the
  bytes the pass allocated, and the highest heap-object size sampled every
  millisecond above the size after a collection just before the pass. No
  collection ran during any pass, so the peak equals the allocation on
  every module.
- **Peak RSS.** Each module was also collected in its own process, `collect
  --modules-only --only <module>` under `/usr/bin/time -l`, which reports the
  process's maximum resident set: the load and the pass together, without
  any per-package extraction. The data row's counts come from the combined
  run; the per-process runs found the same counts.
- **Standard library.** The extractor offers no way to run the pass on a
  standard-library load, and its non-test files were not to change for this
  measurement. A throwaway test in `internal/lang/golang` (not committed)
  called the unexported `loadStdlib` and `computeCrossDup` directly: the
  whole-library load that `ExtractStdlibAll` makes, with the `vendor/`
  packages dropped as the collector drops them, under a 20-minute context
  and a 25-minute test timeout, with the same heap sampling and `/usr/bin/time
  -l` around the process.

Wall times vary between runs on a shared machine (the three
combined runs differed by more than 10x on some small modules, which take
milliseconds); the counts do not vary.

## Cost

| Module | Packages | Load (ms) | Pass (ms) | Pass alloc (MiB) | Process peak RSS (MiB) | `dup_blocks_cross_pkg` |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `github.com/gin-gonic/gin` | 7 | 916 | 11 | 3 | 184 | 2 |
| `github.com/go-chi/chi/v5` | 2 | 596 | 5 | 1 | 125 | 0 |
| `github.com/labstack/echo/v5` | 3 | 1609 | 24 | 5 | 228 | 9 |
| `github.com/gofiber/fiber/v3` | 62 | 7729 | 56 | 25 | 642 | 104 |
| `github.com/gorilla/mux` | 1 | 440 | 1 | 0 | 125 | 0 |
| `github.com/spf13/cobra` | 2 | 505 | 6 | 2 | 143 | 1 |
| `github.com/urfave/cli/v3` | 4 | 1166 | 23 | 3 | 348 | 0 |
| `github.com/spf13/viper` | 7 | 561 | 3 | 1 | 104 | 2 |
| `charm.land/bubbletea/v2` | 1 | 500 | 6 | 1 | 105 | 0 |
| `github.com/junegunn/fzf` | 6 | 1175 | 28 | 15 | 294 | 11 |
| `github.com/cli/cli/v2` | 309 | 8398 | 174 | 66 | 426 | 1723 |
| `github.com/jackc/pgx/v5` | 25 | 2538 | 50 | 24 | 396 | 32 |
| `github.com/go-sql-driver/mysql` | 1 | 559 | 15 | 3 | 179 | 0 |
| `github.com/redis/go-redis/v9` | 20 | 6194 | 68 | 42 | 1369 | 33 |
| `go.etcd.io/bbolt` | 13 | 714 | 14 | 5 | 123 | 8 |
| `github.com/dgraph-io/badger/v4` | 12 | 1170 | 25 | 11 | 273 | 13 |
| `github.com/cockroachdb/pebble` | 103 | 8563 | 208 | 105 | 806 | 351 |
| `go.uber.org/zap` | 15 | 831 | 12 | 3 | 149 | 1 |
| `github.com/rs/zerolog` | 13 | 761 | 12 | 4 | 151 | 10 |
| `github.com/stretchr/testify` | 10 | 874 | 10 | 3 | 183 | 5 |
| `github.com/google/go-cmp` | 10 | 605 | 8 | 3 | 138 | 2 |
| `github.com/google/uuid` | 1 | 310 | 2 | 0 | 82 | 0 |
| `github.com/hashicorp/hcl/v2` | 25 | 1123 | 46 | 24 | 218 | 83 |
| `github.com/BurntSushi/toml` | 8 | 717 | 6 | 2 | 116 | 3 |
| `github.com/pelletier/go-toml/v2` | 17 | 848 | 13 | 6 | 130 | 8 |
| `github.com/prometheus/client_golang` | 27 | 1251 | 21 | 7 | 185 | 7 |
| `google.golang.org/grpc` | 261 | 7250 | 141 | 54 | 528 | 276 |
| `github.com/hashicorp/raft` | 2 | 669 | 13 | 4 | 181 | 0 |
| `github.com/nats-io/nats-server/v2` | 21 | 10276 | 153 | 102 | 2739 | 48 |
| `github.com/caddyserver/caddy/v2` | 49 | 4797 | 66 | 41 | 262 | 443 |
| `github.com/open-policy-agent/opa` | 257 | 10927 | 221 | 105 | 759 | 474 |
| `helm.sh/helm/v4` | 72 | 3251 | 71 | 26 | 288 | 177 |
| `gitea.dev` | 384 | 20030 | 513 | 214 | 1008 | 3394 |
| `github.com/goreleaser/goreleaser/v2` | 130 | 5418 | 49 | 21 | 268 | 437 |
| `github.com/aws/smithy-go` | 54 | 1436 | 37 | 12 | 107 | 129 |
| `golang.org/x/crypto` | 55 | 2500 | 52 | 31 | 210 | 75 |
| **std** (throwaway test, not in the data) | 358 | 2382 | 782 | 317 | 2107 | 1082 |

Over the 36 modules the pass took 2.2 s in all against 117 s of loading
(under 2%), at most 0.5 s (`gitea.dev`, 384 packages) and at most 214 MiB of
allocation, a fifth of that module's 1,008 MiB peak process RSS. The largest
resident sets (`nats-server` at 2.7 GiB, `go-redis` at 1.3 GiB) come from
their loads, whose passes allocate about 100 and 40 MiB.

The standard library, 358 packages without `vendor/`, took 0.78 s and 317
MiB of allocation on top of a loaded heap of 1.2 GiB; the whole process
peaked at 2.1 GiB resident and finished in 3.7 s including the load. It
touches 223 of the 358 packages; the most-touched are `runtime` (86 blocks),
`crypto/tls` (85), `net/http/internal/http2` (76), `net/http` (70) and
`debug/dwarf` (60), with the FIPS copies of the crypto packages and the
`strings`/`bytes` pair prominent, which is the library's deliberate
duplication. **The pass is affordable on the standard library**: it costs
about a quarter of the load's heap and a fraction of a second, so the
reason given for leaving `dup_blocks_cross_pkg` null there does not hold.
Turning it on is a change to the extractor (the `crossApplies` guard in
`internal/lang/golang/crossdup.go`), outside this measurement, so the
standard library has no row in `modules.jsonl` and does not feed the fit
below; with it the pooled p90 would be the same rounded 450 (the 37th value,
1,082, sits above the 90th percentile either way).

## Distribution

`dup_blocks_cross_pkg` on the 36 module rows (nearest rank):

| Modules | min | p25 | p50 | p75 | p90 | max |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 36 | 0 | 1 | 9 | 104 | 443 | 3394 |

The count is extensive: it grows with the module, from 0 in six of the
seven single- or two-package modules to 1,723 in `cli/cli` and 3,394 in
Gitea. Twenty-nine of the 36 modules have at least one cross-package block, so
counting a module with no baseline from zero would fail four in five of them.

## Default rule

The fitted default (`thresholds-2026-09-28`, `calibration/reports/thresholds-2026-09-28.md`):

```yaml
- metric: dup_blocks_cross_pkg
  kind: density
  max_delta: 0
  max: 450
```

- **`max_delta: 0`** by policy, as for `dup_blocks` (SPEC.md 11.1): no new
  cross-package copy. It is evaluated on the module row only, so one copy is
  one finding, and a baseline file written before the row existed skips it
  with a note (SPEC.md 8.1). On this repository it is 38 at `master`; a
  first draft of the collector change raised it to 40 (an error-handling
  loop and a run of struct fields and flag registrations that matched code
  in `internal/engine`, `internal/metrics` and `cmd/astimate`), the rule
  failed `check --base master --all`, and restructuring the change brought
  it back to 38.
- **`max: 450`**, the p90 443 rounded per 11.1. The max judges a module row
  with no baseline value, which with a git baseline means a module that did
  not exist at the base; a file baseline without the row skips the rule
  instead. Three of the 36 modules (`open-policy-agent/opa` 474, `cli/cli`
  1,723 and Gitea 3,394) would fail it as new modules.
- **No `ratchet_from_zero`**: 29 of 36 modules would fail a new-module check
  on the delta from zero, which judges a module's history rather than a
  change. The max does that job at the corpus's own 90th percentile.

The count is not normalized by module size, so as a ceiling for new modules
the max is loose for small modules and tight for large ones; a density per
package or per thousand lines would be the fix if the max ever matters in
practice. The delta, which is what fires on real changes, has no such
problem.
