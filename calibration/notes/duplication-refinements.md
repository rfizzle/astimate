# Duplication: signed literals and call chains

Date: 2026-09-27. Machine: darwin, 14 cores, Go 1.27.1, `golang.org/x/tools` v0.50.0.

## Question

`duplication-literal-only.md` left two findings. Tables of negative numbers
(`{-1, -2, ...}`) survive `dup_ignore_literal_only` because a unary `-` is an
operator token. And `crypto/internal/fips140/nistec` (33.3%) and `math/big`
(23.2%) score from straight-line call sequences and unrolled loops, not from
tables. Should a unary sign before a literal count as part of the literal,
and should a repeat made only of calls to one or two functions count as
data?

## Variants

The baseline is the shipped defaults before this change: 40 tokens,
identifiers and literals normalized, `dup_ignore_literal_only` on.

- **A1, stream fold.** A `+` or `-` directly before an int, float, imaginary
  or char literal, and after a token that cannot end an operand (anything
  but an identifier, a literal, `)`, `]` or `}`; the start of a file and an
  automatic semicolon count as not ending one), is removed from the stream,
  so `-1` normalizes to `LIT` like `1`.
- **A2, sign-aware literal-only rule.** The same unary signs are recorded
  but stay in the stream as their own codes. When the literal-only rule
  checks a block, such a sign counts as part of its literal. Matching is
  unchanged; A2 can only drop blocks the literal-only rule already
  inspects.
- **B strict, call chain.** After merging, drop a block whose every token is
  a literal, table punctuation or an identifier, where every identifier is
  directly followed by `(` and the identifiers have at most two distinct
  texts (read from a parallel stream with identifiers interned).
- **B loose.** As B strict, but an identifier not followed by `(` is allowed
  when it is a whole argument: after `(` or `,` and before `,` or `)`. The
  callees are still at most two distinct texts. This is the form that
  reaches `p256OrdSqr(x, _1, 1)`, whose arguments are identifiers, not
  literals.

## Method

`TestDupMeasureStdlibRefinements` in
`internal/lang/golang/duplication_test.go`, skipped unless
`ASTIMATE_MEASURE_STDLIB=1`:

```
ASTIMATE_MEASURE_STDLIB=1 go test ./internal/lang/golang/ -run TestDupMeasureStdlibRefinements -v -count=1
```

It loads `std` once with `NeedName | NeedFiles | NeedSyntax`, keeps the 225
packages without load errors and with at least 200 SLOC (the same set as
the previous note), and computes the baseline and the four variants. A1 is
built in the test from the baseline stream; A2 is the `foldSigns` option;
B is applied to the baseline's blocks. For each variant it logs the
quantiles, the largest drops and rises, the top ten, and every block
occurrence that differs from the baseline. Quantiles are nearest-rank. Wall
time for load plus all passes: 3.9 s warm.

## Distributions

| | baseline | A1 | A2 | B strict | B loose |
|---|---|---|---|---|---|
| `duplication_pct` p50 | 19.6 | 19.6 | 19.6 | 19.5 | 19.5 |
| `duplication_pct` p90 | 52.5 | 52.5 | 52.5 | 52.5 | 52.5 |
| `duplication_pct` p99 | 82.1 | 82.1 | 82.1 | 82.1 | 82.1 |
| `dup_blocks` p50 | 9 | 9 | 9 | 9 | 9 |
| `dup_blocks` p90 | 55 | 55 | 55 | 55 | 55 |
| `dup_blocks` p99 | 198 | 198 | 198 | 198 | 198 |
| `dup_blocks` total | 4855 | 4861 | 4854 | 4853 | 4849 |
| packages changed | | 13 | 1 | 2 | 6 |

The top ten by `duplication_pct` is the same in every column: debug/elf
82.8, runtime/metrics 82.5, hash/fnv 82.1, internal/trace/tracev2 79, image
69, crypto/internal/fips140/aes 67, internal/runtime/atomic 65.9,
sync/atomic 62.5, vendor/golang.org/x/text/secure/bidirule 62.4,
unicode/utf8 62.

## The packages the question named

| package | SLOC | baseline | A1 | A2 | B strict | B loose |
|---|---|---|---|---|---|---|
| crypto/internal/fips140/nistec | 2091 | 42 / 33.3 | 42 / 33.3 | 42 / 33.3 | 42 / 33.3 | 41 / 32.9 |
| math/big | 5757 | 68 / 23.2 | 68 / 23.2 | 68 / 23.2 | 68 / 23.2 | 67 / 23.2 |
| net/http | 11045 | 55 / 8.5 | 55 / 8.5 | 55 / 8.5 | 55 / 8.5 | 55 / 8.5 |

Cells are `dup_blocks / duplication_pct`.

## A1: folding the sign into the stream changes what matches

| package | SLOC | blocks base | blocks A1 | pct base | pct A1 |
|---|---|---|---|---|---|
| math | 3322 | 81 | 86 | 37.7 | 44.1 |
| math/rand | 894 | 10 | 10 | 39 | 45.4 |
| debug/gosym | 1080 | 7 | 8 | 10.1 | 12.5 |
| text/scanner | 556 | 2 | 3 | 2.9 | 4.7 |
| internal/poll | 1416 | 29 | 30 | 40.7 | 41.7 |
| crypto/internal/fips140/sha256 | 353 | 9 | 8 | 30.6 | 29.2 |
| crypto/internal/entropy/v1.0.0 | 294 | 6 | 5 | 12.2 | 10.9 |
| crypto/elliptic | 573 | 20 | 19 | 39.1 | 38 |
| crypto/internal/fips140/sha512 | 426 | 11 | 10 | 46.9 | 46.5 |

A1 raises more than it lowers, and neither direction is about tables.

- Rises. With `-x` and `x` equal, `f(-1)` matches `f(1)` and a signed
  coefficient table matches an unsigned one, so repeats grow across the
  table boundary into the code around it and the literal-only rule no
  longer applies. `math/j0.go` and `math/j1.go` merge into one block of 200
  lines (227-429 against 222-424); in `math/rand` the signed `rngCooked`
  table now matches the unsigned `ke` table in `exp.go` and the block runs
  into the `var we = ...` declarations; `debug/gosym` and `text/scanner`
  gain code blocks that differed only in a sign.
- Drops. Every block A1 removes is code that fell below 40 tokens once its
  signs were gone: `bits.RotateLeft32(v1, -17)` in `sha256block.go`,
  `bits.RotateLeft64(e, -14)` in `sha512block.go` and `entropy/sha384.go`,
  and `if z3.Sign() == -1` in `crypto/elliptic/params.go`.

A1 is rejected.

## A2: signed tables only

A2 drops one block in one package: the signed `rngCooked` table in
`math/rand/rng.go` (lines 87-90 and 114-117), moving `math/rand` from 39 to
38.1%. That is the only repeat in std that was a signed table on its own.
The other signed tables (`math/j0.go`, `j1.go`, `erf.go`, `lgamma.go`
coefficient arrays) sit in blocks merged with the functions next to them,
which the literal-only rule keeps whole by design. A2 cannot touch code: it
only widens the set of tokens that make a block literal-only, and a block
with any identifier, keyword or other operator is still kept.

## B: what it drops, read by hand

B strict drops 2 blocks, B loose 6. Every one was read.

| block | variant | reading |
|---|---|---|
| `net/http/internal/sniff.go:67-82`, `htmlSig("<HTML")` rows | strict, loose | data: a signature table written as constructor calls |
| `crypto/internal/fips140/bigmod/nat.go:1007-1009`, `NewNat(), NewNat(), ...` | strict, loose | data: an array of fifteen constructor calls, unrolled so they stay on the stack |
| `time/format.go:1619-1625`, `"ns": uint64(Nanosecond)` | loose | data: the unit map, conversions in a map literal |
| `internal/trace/tracev1.go:146-173`, `addBuiltin(sForever, "forever")` | loose | data: a registration table written as calls |
| `crypto/internal/fips140/nistec/p256_ordinv.go:30-40`, `p256OrdSqr(x, _1, 1)` / `p256OrdMul(_11, x, _1)` | loose | code: an addition chain, an algorithm whose operand order is the content |
| `math/big/natmul.go:236-245` and `:323-332`, `trace("x0", x0)` | loose | code: a debug dump pasted into `karatsuba` and `karatsubaSqr` |

B strict is data-only but reaches none of the named packages: it drops 2
blocks out of 4855 and leaves `nistec` and `math/big` where they were. B
loose is the only form that touches `nistec`, and it takes 0.4 points off
(33.3 to 32.9) by dropping the addition chain, which is code; it also drops
the `math/big` trace dump, which is the textbook copy-paste a helper
function or a loop over a slice would remove. That is the false positive
the rule risks in LLM-written code: a sequence of calls to one helper with
different arguments is what an agent writes when it should have written a
table and a loop, and the gate should see it. The four data-like blocks
B would drop are worth 4 of 4855 blocks and move no quantile past 0.1.

The addition chain is 0.4 points of `nistec`'s 33.3%; the rest is the
repeated limb arithmetic the previous note found, which holds operators
and so is outside any call-chain rule. `math/big`'s
23.2% is the unrolled loops of `natmul.go`, `natdiv.go`, `nat.go` and
`rat.go`, which no variant reaches because they hold operators and
keywords. Both scores are real duplication by the SPEC definition.

## Decision

- **Signed literals: ship A2 as `dup_fold_signs`, default true.** It removes
  a signed table and nothing else, cannot change what matches, and makes
  the literal-only rule consistent: `{-1, -2}` is as much data as `{1, 2}`.
  Its effect on std is one block, so it is a correctness fix, not a
  calibration lever. A1 is rejected: it adds more duplication than it
  removes and drops code blocks.
- **Call chains: nothing ships.** Addition chains and other straight-line
  call sequences differing only in arguments count as duplication. The
  strict rule is harmless but moves nothing; the loose rule that reaches
  `nistec` drops code, including the pattern the gate exists to catch.
  Tolerance for a package like `nistec` that is duplicated by construction
  belongs in thresholds and calibration, not in the block rule: a rule that
  excused its shape would also excuse the same shape in new code.

Threshold calibration for `duplication_pct` should use the A2 distribution,
which equals the baseline's at p50, p90 and p99.
