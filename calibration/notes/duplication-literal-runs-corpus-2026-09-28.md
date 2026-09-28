# Duplication: literal-run split on the calibration corpus

Date: 2026-09-28. Machine: darwin/arm64, 14 cores, Go 1.27.1,
`golang.org/x/tools` v0.50.0. Follows `duplication-literal-runs.md`, which
measured the split on the standard library only.

## Question

Variant S of `duplication-literal-runs.md` cuts each duplicate block at
every literal-only run of at least `duplication.min_tokens` tokens and
keeps the parts of at least that length. On std it removed only data:
tables that the literal-only rule keeps because their declaration headers
(`var x = [N]T{`) join them into one block. Does that still hold on the 36
cloned modules of `calibration/corpus.yaml`? If it does, S ships as
`duplication.split_literal_runs`.

## Method

Each cloned entry of `calibration/corpus.yaml` was fetched at its pinned
commit the way `calibration/collect` does it (`git init`, `git fetch
--depth 1 <repo> <commit>`, `git checkout --detach FETCH_HEAD`), into a
scratch directory outside the repository. `TestDupMeasureModuleLiteralRuns`
then ran on each clone, with the module cache already on the host and no
further network:

```
GOWORK=off GOPROXY=off GOFLAGS=-mod=mod ASTIMATE_MEASURE_ROWS=1 \
ASTIMATE_MEASURE_MODULE=/path/to/clone \
  go test ./internal/lang/golang/ -run 'TestDupMeasureModuleLiteralRuns$' -v -count=1 -timeout 15m
```

`ASTIMATE_MEASURE_ROWS=1`, added for this note, logs every package row of
every variant so quantiles can be pooled across modules. The test loads
`./...` of the module with `NeedName | NeedFiles | NeedSyntax` and keeps
the packages with no load errors and at least 200 authored SLOC. No package
of any module failed to load. The test now also logs the loaded and skipped
counts.

std was rerun the same way with `ASTIMATE_MEASURE_STDLIB=1
ASTIMATE_MEASURE_SLOC=authored`, so all 1039 packages share the
extractor's current denominator. Its numbers match the "Denominator"
paragraph of the earlier note: p50 21.7 at baseline and 19.8 under S, and
146 blocks removed with 52 parts added.

Every module finished well under the ten-minute budget. `gitea.dev` was
the slowest at 17 s, and all 37 runs took about 2 minutes in total. No
module was skipped.

The test also checks the shipped option against the measurement. For each
package, `dupCheckSplit` runs `duplication` with `splitLiteralRuns` on and
fails if the blocks, `duplication_pct` or the locations differ from
variant S counted over the recorder. It passed on all 1039 packages.

## Per module (variant S)

`p50` is the median `duplication_pct` over the module's measured packages.
Blocks are counted as `-` (removed) and `+` (split parts added).

| module | packages | changed | blocks - / + | p50 before | p50 after |
|---|---|---|---|---|---|
| std | 215 | 30 | 146 / 52 | 21.7 | 19.8 |
| github.com/gin-gonic/gin | 3 | 0 | 0 / 0 | 28.1 | 28.1 |
| github.com/go-chi/chi/v5 | 2 | 0 | 0 / 0 | 16.7 | 16.7 |
| github.com/labstack/echo/v5 | 2 | 0 | 0 / 0 | 25.2 | 25.2 |
| github.com/gofiber/fiber/v3 | 22 | 0 | 0 / 0 | 9.5 | 9.5 |
| github.com/gorilla/mux | 1 | 0 | 0 / 0 | 17.0 | 17.0 |
| github.com/spf13/cobra | 2 | 0 | 0 / 0 | 36.5 | 36.5 |
| github.com/urfave/cli/v3 | 2 | 0 | 0 / 0 | 21.4 | 21.4 |
| github.com/spf13/viper | 1 | 0 | 0 / 0 | 31.7 | 31.7 |
| charm.land/bubbletea/v2 | 1 | 0 | 0 / 0 | 24.5 | 24.5 |
| github.com/junegunn/fzf | 4 | 0 | 0 / 0 | 12.6 | 12.6 |
| github.com/cli/cli/v2 | 114 | 0 | 0 / 0 | 8.7 | 8.7 |
| github.com/jackc/pgx/v5 | 10 | 0 | 0 / 0 | 30.6 | 30.6 |
| github.com/go-sql-driver/mysql | 1 | 0 | 0 / 0 | 20.3 | 20.3 |
| github.com/redis/go-redis/v9 | 11 | 1 | 5 / 0 | 37.6 | 37.6 |
| go.etcd.io/bbolt | 5 | 0 | 0 / 0 | 19.3 | 19.3 |
| github.com/dgraph-io/badger/v4 | 6 | 0 | 0 / 0 | 6.7 | 6.7 |
| github.com/cockroachdb/pebble | 57 | 0 | 0 / 0 | 17.1 | 17.1 |
| go.uber.org/zap | 2 | 0 | 0 / 0 | 22.1 | 22.1 |
| github.com/rs/zerolog | 6 | 1 | 2 / 0 | 40.0 | 37.9 |
| github.com/stretchr/testify | 5 | 0 | 0 / 0 | 11.1 | 11.1 |
| github.com/google/go-cmp | 5 | 0 | 0 / 0 | 27.2 | 27.2 |
| github.com/google/uuid | 1 | 0 | 0 / 0 | 15.8 | 15.8 |
| github.com/hashicorp/hcl/v2 | 11 | 1 | 14 / 0 | 33.7 | 27.0 |
| github.com/BurntSushi/toml | 2 | 0 | 0 / 0 | 19.2 | 19.2 |
| github.com/pelletier/go-toml/v2 | 5 | 0 | 0 / 0 | 20.1 | 20.1 |
| github.com/prometheus/client_golang | 8 | 0 | 0 / 0 | 18.0 | 18.0 |
| google.golang.org/grpc | 78 | 0 | 0 / 0 | 14.7 | 14.7 |
| github.com/hashicorp/raft | 1 | 0 | 0 / 0 | 18.9 | 18.9 |
| github.com/nats-io/nats-server/v2 | 11 | 0 | 0 / 0 | 25.0 | 25.0 |
| github.com/caddyserver/caddy/v2 | 22 | 1 | 1 / 0 | 13.5 | 13.5 |
| github.com/open-policy-agent/opa | 75 | 0 | 0 / 0 | 18.0 | 18.0 |
| helm.sh/helm/v4 | 34 | 2 | 2 / 0 | 27.1 | 21.7 |
| gitea.dev | 223 | 4 | 32 / 20 | 19.7 | 19.7 |
| github.com/goreleaser/goreleaser/v2 | 42 | 0 | 0 / 0 | 12.1 | 12.1 |
| github.com/aws/smithy-go | 21 | 0 | 0 / 0 | 31.6 | 31.6 |
| golang.org/x/crypto | 28 | 3 | 3 / 2 | 26.3 | 24.8 |

The helm row's p50 moves because the module has two identical
`release/util` packages (`pkg/release/v1/util` and
`internal/release/v2/util`), and they sit at its median. In zerolog and hcl
the median package is the one that changed.

The largest single drops are `x/crypto/blowfish` (85.2 to 37.7),
`hcl/v2/hclsyntax` (66.4 to 24.5), `zerolog/internal` (40.0 to 18.8), the
two helm `release/util` packages (27.1 to 6.5), `x/crypto/ripemd160`
(46.2 to 32.7) and `x/crypto/cast5` (26.3 to 23.7). Every other change is
under 2.3 points.

## Pooled

All 1039 packages (std plus 824 from the 36 modules), then the 824 alone.
Quantiles are nearest-rank.

| | baseline | S | T | S10 |
|---|---|---|---|---|
| `duplication_pct` p50, all | 18.8 | 18.5 | 16.8 | 18.2 |
| `duplication_pct` p90, all | 46.2 | 45.1 | 42.3 | 44.7 |
| `duplication_pct` p99, all | 81.7 | 78.6 | 78.1 | 78.3 |
| `dup_blocks` total, all | 25379 | 25248 | 23412 | 25086 |
| packages changed, all | | 43 | 862 | 118 |
| `duplication_pct` p50, corpus modules | 18.1 | 18.0 | 16.3 | 17.7 |
| `duplication_pct` p90, corpus modules | 44.0 | 43.5 | 41.0 | 43.3 |
| `duplication_pct` p99, corpus modules | 78.6 | 78.3 | 78.1 | 78.3 |
| packages changed, corpus modules | | 13 | 677 | 72 |
| blocks - / +, corpus modules | | 59 / 22 | | |

Outside std, S touches 13 of 824 packages. It is a narrow rule: most Go
modules do not keep large literal tables next to each other. T and S10
are shown for completeness only. Both were rejected on std because they
cut code, and the corpus does not change that.

## Twenty changed blocks, read

These are all from the 36 modules. The ten std blocks read in
`duplication-literal-runs.md` are not repeated here. Each row gives the
first two occurrences, the occurrence count and the length in tokens. It
also names any code that loses coverage: a fragment on the far side of a
cut that is shorter than `duplication.min_tokens` on its own.

| # | block | reading |
|---|---|---|
| 1 | go-redis `autopipeline.go:1727-1737`, `command.go:26-48` x3 n121 | data: `map[string]struct{}` command-name sets with their `var … = map[string]struct{}{` header |
| 2 | go-redis `command.go:18-73`, `csc_commands.go:4-36` x2 n251 | data: the tail of the import list, a map header and a command-name set |
| 3 | go-redis `autopipeline.go:1728-1740`, `csc_commands.go:38-57` x2 n119 | data: command-name set; the `} func runsOutsidePipeline(name string) bool {` / `func isCacheable(cmd Cmder) bool {` signature after it (9 tokens) is dropped |
| 4 | go-redis `autopipeline.go:1760-1780`, `csc_commands.go:48-61` x2 n71 | data: command-name set; the next function's signature and `if` (about 12 tokens) are dropped |
| 5 | zerolog `internal/testcases.go:29-68`, `:251-280` x2 n152 | data: CBOR/JSON test-case tables `{value, "bytes"}` and their `[]struct{…}{` headers |
| 6 | zerolog `internal/testcases.go:140-151`, `:158-169` x2 n59 | data: the `Float32TestCases` and `Float64TestCases` rows |
| 7 | hcl `hclsyntax/scan_tokens.go:16-1508`, `:2264-3757` x2 n23792 | data: Ragel state-machine tables |
| 8 | hcl `hclsyntax/scan_string_lit.go:10-40`, `:54-85` x2 n396 | data: `_hclstrtok_*` byte tables joined by `var … []byte = []byte{` headers |
| 9 | hcl `hclsyntax/scan_string_lit.go:105-116`, `scan_tokens.go:4972-4983` x2 n76 | data: a table tail and the generated state constants (`const hclstrtok_start int = 4`) |
| 10 | caddy `httpcaddyfile/directives.go:47-98`, `shorthands.go:54-74` x2 n82 | data: the directive-order `[]string` against the placeholder-pair `[]string` |
| 11 | helm `pkg/release/v1/util/kind_sorter.go:31-70`, `:75-115` x2 n85 | data: the `InstallOrder` and `UninstallOrder` kind lists (the same in `internal/release/v2/util`) |
| 12 | gitea `modules/charset/ambiguous_gen.go:23-46`, `:410-433` x2 n386 | data: confusable-rune tables; the split cuts out the `[]rune{…}` lists and keeps the keyed `{Lo:, Hi:, Stride:}` range entries as a 299-token part |
| 13 | gitea `modules/charset/ambiguous_gen.go:22-23`, `:23-24` x2 n3085 | data: adjacent `Confusable` and `With` rune lists |
| 14 | gitea `modules/charset/ambiguous_gen.go:538-586`, `:691-739` x2 n742 | data: range-table entries; kept as parts `538-563` and `565-586` around a cut rune list |
| 15 | gitea `modules/git/object_format.go:25-32`, `:54-62` x2 n58 | data: the empty-tree hash bytes; the `} type …ObjectFormatImpl struct{} var ( … = &…Hash{} … = &…Hash{` declarations before them (about 18 tokens) are dropped |
| 16 | gitea `modules/markup/sanitizer_default.go:67-82`, `:103-114` x2 n69 | data: the MathML element list and the safe-attribute list; the `) mathMLElements := []string{` header (7 tokens) is dropped |
| 17 | gitea `modules/markup/sanitizer_default.go:106-123`, `:127-133` x2 n140 | data: `generalSafeAttrs` and `generalSafeElements` lists |
| 18 | gitea `modules/setting/i18n.go:7-22`, `repository.go:267-296` x2 n62 | data: language-name pairs against the charset detection order |
| 19 | x/crypto `blowfish/const.go:11-152`, `:57-198` x2 n1607 | data: S-boxes `s0` to `s3` and `p`; a 44-token part (the `var p = [18]uint32{` header and a short row tail) stays |
| 20 | x/crypto `ripemd160/ripemd160block.go:16-39`, `:24-47` x2 n507 | data: the `_n`, `_r`, `n_` and `r_` index and rotation tables |

The one remaining x/crypto change is `cast5/cast5.go:140-167` with
`:168-195`, which becomes `140-161` with `168-189`. It is the key-schedule
table. Its `16 + 0` sums are code-class tokens, so most of it stays
counted.

All twenty are data. As on std, the code that loses coverage is only short
fragments next to a table: a function's signature (3, 4), a declaration
header (15, 16) or a function's closing `ID }` before a map header
(go-redis `autopipeline.go:1713`). Each is under 40
tokens once the table is cut away, so by SPEC 6.3 it is too short to be a
duplicate on its own.

Two of the modules' largest tables are generator output that the
generated-file rule does not catch. `hclsyntax/scan_*.go` (Ragel) starts
with `//line` directives. `charset/ambiguous_gen.go` says "This file is
generated by … DO NOT EDIT" without the `Code generated` prefix. Both
therefore count as authored. The split is what removes them from
`duplication_pct`.

## Decision

S ships as `duplication.split_literal_runs`. Every block read is data, on
std (ten) and on the corpus (twenty), and the only code it uncovers is
fragments below `duplication.min_tokens`. The option is wired into the Go
and TypeScript extractors through `duptok`, like `duplication.fold_signs`.
It works under `duplication.ignore_literal_only` and counts a sign as a
literal exactly when `fold_signs` does. It also applies to the Go
cross-package count, which uses the same finder. It was not measured on
TypeScript.

**The default stays off**, although the measurement would support on. On
lowers the pooled p50 by 0.3 points and p90 by 1.1. That is a result
change: `dup_blocks` and `duplication_pct` move in 43 of 1039 corpus
packages. The `duplication_pct` thresholds of 2026-09-28 were also fitted
on data collected with it off. Turning it on needs its own
change: set the default, recollect and refit or show that the thresholds
hold, and record both. The `dupes`, `net/http` and fixture goldens do not
move either way. They were checked with the option off, and with it forced
on in every default (config, Go extractor, `duptok`): only the unit tests
that pin the old default changed.
