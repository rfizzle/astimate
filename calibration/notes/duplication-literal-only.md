# Duplication: literal-only blocks

Date: 2026-09-27. Machine: darwin, 14 cores, Go 1.27.1, `golang.org/x/tools` v0.50.0.

## Question

Literal tables normalize to runs of `LIT ,`, and a run that repeats inside
itself is a maximal repeat, so data scores as duplication. SPEC.md section 15
left open whether such blocks should count. `dup_ignore_literal_only` drops a
duplicate block, after merging, when every token in it is a literal or one of
the punctuation tokens `, { } : [ ] ( ) ;`. Any identifier, keyword or other
operator (a unary `-` included) keeps the block, and a block that holds a
table next to code is kept whole. How does the option move `dup_blocks` and
`duplication_pct` over the standard library, and which default should ship?

## Method

`TestDupMeasureStdlibLiteralOnly` in
`internal/lang/golang/duplication_test.go`, skipped unless
`ASTIMATE_MEASURE_STDLIB=1`:

```
ASTIMATE_MEASURE_STDLIB=1 go test ./internal/lang/golang/ -run TestDupMeasureStdlibLiteralOnly -v -count=1
```

It loads `std` once with `NeedName | NeedFiles | NeedSyntax`, keeps every
package without load errors and with at least 200 SLOC, and computes
duplication with the default options (40 tokens, identifiers and literals
normalized) with the option off and on. Quantiles are nearest-rank. Wall
time for load plus both passes: 2.4 s warm (7.7 s on the first, cold run).

## Distributions

225 packages of the 381 in `go list std` have at least 200 SLOC.

| | off | on |
|---|---|---|
| `duplication_pct` p50 | 19.7 | 19.6 |
| `duplication_pct` p90 | 52.6 | 52.5 |
| `duplication_pct` p99 | 82.5 | 82.1 |
| `dup_blocks` p50 | 9 | 9 |
| `dup_blocks` p90 | 55 | 55 |
| `dup_blocks` p99 | 199 | 198 |
| `dup_blocks` total | 4912 | 4855 |
| packages changed | | 35 |

## Largest drops

| package | SLOC | blocks off | blocks on | pct off | pct on |
|---|---|---|---|---|---|
| html | 2401 | 3 | 0 | 94.2 | 0 |
| crypto/internal/entropy/v1.0.0 | 294 | 7 | 6 | 39.5 | 12.2 |
| crypto/des | 370 | 20 | 19 | 71.6 | 58.9 |
| vendor/golang.org/x/net/internal/httpcommon | 454 | 2 | 1 | 14.1 | 1.5 |
| crypto/internal/fips140/sha256 | 353 | 11 | 9 | 40.2 | 30.6 |
| encoding/xml | 3341 | 47 | 45 | 31 | 22.1 |
| mime | 951 | 3 | 2 | 11.3 | 4.4 |
| net/textproto | 803 | 5 | 4 | 14.1 | 9.1 |
| crypto/internal/fips140/sha512 | 426 | 13 | 11 | 51.2 | 46.9 |
| vendor/golang.org/x/net/internal/http3 | 2291 | 24 | 23 | 25 | 20.7 |

The dropped occurrences were listed by file and line (57 blocks over the
35 packages) and eighteen of them read by hand, eight chosen
at random; every one read is a data table: the HTML entity map in
`html/entity.go`, CAST test vectors in `crypto/internal/fips140/*/cast.go`,
DER prefixes in `rsa/pkcs1v15.go`, the small-prime list in `rsa/keygen.go`,
curve orders, the `2/pi` bits in `math/trig_reduce.go`, power-of-five tables
in `math/big` and `internal/strconv`, Unicode space ranges in `fmt/scan.go`,
port and profile maps in `net` and `net/http/pprof`, and platform lists in
`internal/platform`.

## The two packages the question named

| package | SLOC | blocks off | blocks on | pct off | pct on |
|---|---|---|---|---|---|
| crypto/internal/fips140/nistec | 2091 | 45 | 42 | 33.3 | 33.3 |
| math/big | 5757 | 69 | 68 | 23.7 | 23.2 |

Neither score came from literal tables. `nistec`'s precomputed points in
`p256_table.go` are one `[...]byte{...}` on a single physical line, so they
weigh at most one covered line whatever the option does; three of its blocks
go (the `sqrs` list in `p256_ordinv.go` among them) and its 33.3% is the addition chains of `p256_ordinv.go` and
`p224_sqrt.go` and the repeated limb arithmetic of `p256_asm.go`, which is
real code and stays. `math/big`'s is the unrolled loops in `natmul.go`,
`natdiv.go`, `nat.go` and `rat.go`; only the `floatconv.go` power table goes.

## Top ten by `duplication_pct`

Off: html 94.2, debug/elf 82.8, runtime/metrics 82.5, hash/fnv 82.1,
internal/trace/tracev2 79, crypto/des 71.6, image 69,
crypto/internal/fips140/aes 67, internal/runtime/atomic 65.9, sync/atomic
62.5.

On: debug/elf 82.8, runtime/metrics 82.5, hash/fnv 82.1,
internal/trace/tracev2 79, image 69, crypto/internal/fips140/aes 67,
internal/runtime/atomic 65.9, sync/atomic 62.5,
vendor/golang.org/x/text/secure/bidirule 62.4, unicode/utf8 62.

## Decision

Default `dup_ignore_literal_only: true`.

The quantiles barely move (p90 52.6 to 52.5), so the option is not a
calibration lever. It is a correctness fix at the tail: the table-only
package `html` leaves the top ten (94.2% to 0), `crypto/des` falls out of it,
and every dropped block is data, not code. No code-shaped repeat is lost,
the `dupes` fixture keeps its three locations, and the other eight packages
in the top ten are unchanged. A gate that fails a change for appending rows
to a table would push agents to restructure data that is fine as it is.

Threshold calibration for `duplication_pct` should use the "on" distribution.
