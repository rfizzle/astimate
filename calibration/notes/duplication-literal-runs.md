# Duplication: literal-only runs inside mixed duplicate blocks

Date: 2026-09-27. Machine: darwin/arm64, 14 cores, Go 1.27.1, `golang.org/x/tools` v0.50.0.

## Question

`duplication-refinements.md` found that the coefficient tables in `math`
(`j0.go`, `j1.go`, `erf.go`, `lgamma.go`) sit in duplicate blocks that also
cover the functions next to them. The literal-only rule looks at a whole
block after merging, so it keeps such a block and the tables count as
duplication. Can the rule reach a literal table inside a mixed block,
either by trimming the block's literal-only ends or by splitting it at long
literal-only runs, without touching code?

## Variants

The baseline is the shipped defaults: 40 tokens, identifiers and literals
normalized, `duplication.ignore_literal_only` and `duplication.fold_signs`
on. "Literal-only" below is the rule's own token class: a literal, one of
`, { } : [ ] ( )`, an explicit `;`, or a unary sign before a numeric
literal. Both variants run after merging and after the literal-only rule,
on each block's token sequence (the same for every occurrence), and move
all occurrences together.

- **T, trim.** Remove the literal-only prefix and suffix of each block. If
  fewer than `duplication.min_tokens` tokens remain, drop the block.
- **S, split.** Cut each block at every literal-only run of at least
  `duplication.min_tokens` tokens. Keep the code parts of at least
  `duplication.min_tokens` tokens as blocks; drop the rest.
- **S10, split at shorter runs.** S with a run length of 10 instead of 40.
  Not one of the two proposed variants. It is included because S does not
  reach the `math` tables at all (see below), and 10 is short enough to
  cut at them.

Parts that end up with the same first occurrence and length are counted
once. A part can lie inside another block's occurrences; coverage is a
union so that does not change `duplication_pct`, only `dup_blocks`.

## Method

`TestDupMeasureStdlibLiteralRuns` in
`internal/lang/golang/duplication_test.go`, skipped unless
`ASTIMATE_MEASURE_STDLIB=1`:

```
ASTIMATE_MEASURE_STDLIB=1 go test ./internal/lang/golang/ -run TestDupMeasureStdlibLiteralRuns -v -count=1
```

It loads `std` once with `NeedName | NeedFiles | NeedSyntax` and keeps the
225 packages that have no load errors and at least 200 SLOC, the same set
as the previous notes. It then applies each variant to the baseline blocks
of every package. For each variant it logs the quantiles, the largest drops
and rises, the named packages, every block that changed (as `-` for a
removed block and `+` for a new trimmed or split part), and the covered
lines of the four `math` files. Quantiles are nearest-rank. Load plus all
passes took 2.6 s warm.

## Distributions

| | baseline | T | S | S10 |
|---|---|---|---|---|
| `duplication_pct` p50 | 19.6 | 17.1 | 17.4 | 16.9 |
| `duplication_pct` p90 | 52.5 | 49.6 | 51.6 | 46.2 |
| `duplication_pct` p99 | 82.1 | 76.3 | 79 | 73.5 |
| `dup_blocks` p50 | 9 | 9 | 9 | 8 |
| `dup_blocks` p90 | 55 | 50 | 55 | 55 |
| `dup_blocks` p99 | 198 | 184 | 198 | 195 |
| `dup_blocks` total | 4854 | 4447 | 4760 | 4680 |
| packages changed | | 186 | 30 | 46 |
| blocks removed / parts added | | 3442 / 3035 | 146 / 52 | 246 / 72 |

## The math packages

Covered source lines in the four files the question named:

| file | baseline | T | S | S10 |
|---|---|---|---|---|
| `math/j0.go` | 241 | 230 | 241 | 118 |
| `math/j1.go` | 252 | 241 | 252 | 129 |
| `math/erf.go` | 109 | 108 | 109 | 109 |
| `math/lgamma.go` | 52 | 31 | 52 | 31 |

Package rows, `dup_blocks / duplication_pct`:

| package | SLOC | baseline | T | S | S10 |
|---|---|---|---|---|---|
| math | 3322 | 81 / 37.7 | 69 / 33.5 | 80 / 37 | 73 / 28.7 |
| math/big | 5757 | 68 / 23.2 | 67 / 22.2 | 68 / 23.2 | 68 / 23.2 |
| math/cmplx | 516 | 13 / 24.2 | 13 / 24 | 13 / 24.2 | 13 / 24.2 |
| math/rand | 894 | 9 / 38.1 | 6 / 26.6 | 6 / 7.4 | 6 / 7.4 |
| net/http | 11045 | 55 / 8.5 | 50 / 7.7 | 55 / 8.5 | 55 / 8.5 |

The `math` tables are short. Each `j0.go` and `j1.go` table has five or six
entries, which is 12 to 20 tokens. The tables are separated by
`var p0S8 = [5]float64{`, which holds a keyword and identifiers. So the
longest literal-only run in these blocks is well under 40 tokens, and S
never cuts them. T only removes whichever table happens to sit at a
block's edge. `lgamma.go` is the same shape. The `erf.go` coefficients are
not tables at all: they are named constants (`pp0 = 1.28e-01`), where every
value comes after an identifier. No rule based on literal runs can reach
them, and S10 leaves `erf.go` unchanged too.

## T: trimming removes code

T changes 186 of 225 packages and 3442 of 4854 blocks. It does this because
the rule's punctuation includes `}` and `)`, so almost every block that
begins or ends at a statement boundary loses its closing braces. When a
brace sits on its own line, that line stops being covered. Ten changed
blocks, read by hand:

| block | reading |
|---|---|
| `bufio/bufio.go:59-84` becomes `62-84` | code: the `}` that closes `NewReaderSize` |
| `bufio/scan.go:291-300` becomes `291-299` | code: the `}` that closes an `if` in `ScanBytes` |
| `context/context.go:182-196` becomes `184-196` | code: the `{}` of `type emptyCtx struct{}` |
| `math/dim.go:24-50` becomes `26-50` | code: the `}` that closes an `if` in `Dim` |
| `math/exp.go:135-152` with `sin.go:110-129`, becomes `140-152` | code: the `) }` that ends `Exp` |
| `math/j0.go:112-129` with `j1.go:111-128` | code: the asymptotic branch of `J0`/`J1`; with its closing braces trimmed it matches another block and is counted once |
| `math/erf.go:141-163` becomes `142-163` | data: one constant's value at the head of the block |
| `math/erfinv.go:17-65`, 141 tokens become 140 | data: the `)` that closes a constant block; no line changes |
| `math/lgamma.go:92-112` with `pow10.go:11-17` | data: table rows at both ends; what is left is under 40 tokens and is dropped |
| `math/j0.go:399-428` with `j1.go:394-423` | mixed: the tail of `q0R2` at the head is trimmed; the `q0S2` table and `qzero` stay |

Six of the ten are code. The four that touch data remove a single value, a
closing parenthesis, or table rows at a block's edge; none reaches a table
in the middle of a block. T is rejected. Trimming only affixes
that contain a literal would still cut `0 }` off every code block that ends
in `return 0 }`.

## S: data only, but not the math tables

S removes 146 blocks and adds 52 parts in 30 packages. It does not touch
`net/http`, `math/big`, or any of the four `math` files. Ten changed
blocks, read by hand:

| block | reading |
|---|---|
| `crypto/des/const.go:14-33` and `:27-46` | data: permutation tables joined by `var x = [64]byte{` headers |
| `crypto/internal/fips140/aes/const.go:20-322` and `:55-356` | data: S-box tables, 4722 tokens |
| `crypto/internal/fips140/ecdh/ecdh.go:70-84` becomes `70-80` | data: the `p224Order` bytes are cut out; the `P224()`/`P256()` constructors stay as a block |
| `math/big/internal/asmgen/arm64.go:5-21` with `loong64.go:5-24` | data: register-name string lists; the struct fields after them stay |
| `crypto/tls/auth.go:111-115` with `common.go:232-236` | data: `signaturePadding` bytes |
| `internal/strconv/atof.go:421-423` with `uscale.go:82-84` | data: `float64pow10` table |
| `math/bits/bits.go:37-46` and `:47-53` | data: de Bruijn tables |
| `crypto/internal/fips140/edwards25519/edwards25519.go:64-74` and `:74-87` | data: identity and generator point bytes; the one-line constructors between them are under 40 tokens on their own |
| `math/rand/exp.go:45-166` and `:100-221` | data: the `ke`/`we`/`fe` tables |
| `debug/elf/elf.go:123-165` becomes `123-145` | data: `osabiStrings` rows are cut out; the `OSABI` constant block stays |

All ten are data. The only code that loses coverage is a short fragment
that shares a block with a table and is under `duplication.min_tokens` once
the table is gone: the constructors in `edwards25519.go`, the
`return b.String() }` tail before `cssReplacementTable` in
`html/template/css.go`, and the `return true } return false` after the
case list in `internal/platform/supported.go`. By the SPEC definition these
fragments are too short to be duplicates on their own. S moves p50 from
19.6 to 17.4 and p99 from 82.1 to 79, mostly through `debug/elf` (82.8 to
58.3), `crypto/internal/fips140/aes` (67 to 35.1), `math/rand` (38.1 to 7.4)
and `math/rand/v2` (50.1 to 9.7).

## S10: reaches the tables, and code

S10 halves the covered lines of `j0.go` and `j1.go` (241 to 118, 252 to 129)
and removes the `lgamma.go` tables (52 to 31). But a run of 10 tokens is
common in code:

| block | reading |
|---|---|
| `crypto/tls/handshake_messages.go:245-259` and `:291-305` | code: `}) } }) }) } }` closing nested builder closures is a 10-token run |
| `image/color/color.go:198-207` and `:216-225` | code: `NRGBA{0, 0, 0, 0} }` splits `nrgbaModel` |
| `database/sql/convert.go:569-573` and `:571-575` | code: `, 'g', -1, 64)` splits a `FormatFloat` switch |
| `runtime/print.go:126-134` and `:142-150` | code: the `AppendFloat` argument run splits `printfloat64` |

S10 is rejected on those four alone.

## Decision

Nothing ships. The shipping condition was a variant that removes the
`math` tables without touching code blocks, and none meets it:

- **T** reaches only the table at a block's edge and trims closing braces
  off code in 186 packages.
- **S** is data-only in every block read, but it leaves all four `math`
  files unchanged, because their tables are shorter than
  `duplication.min_tokens` and are separated by declaration headers.
- **S10** reaches `j0.go`, `j1.go` and `lgamma.go` but also splits code at
  runs of closing brackets and argument lists.
- `erf.go` is out of reach of any literal-run rule: its coefficients are
  named constants.

The `math` coefficient tables stay counted. Like `nistec` in the previous
note, `math` is duplicated by construction (parallel Bessel
implementations with parallel tables), and tolerance for it belongs in
thresholds, not in the block rule.

S is worth deciding on its own merits, separately from `math`. It removes
data that the literal-only rule misses only because table headers join
the tables into one block (S-boxes, permutation tables, `debug/elf` name
tables, `math/rand` ziggurat tables). In the blocks read here it cost no
code block, and it lowers p50 by 2.2 points. That is a calibration lever
rather than a correctness fix, so it needs its own story with a test
corpus beyond std before it becomes a default.

Threshold calibration for `duplication_pct` should keep using the baseline
distribution in this note, which matches the distribution in
`duplication-refinements.md`.
