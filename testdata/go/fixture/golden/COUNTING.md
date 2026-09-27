# How the fixture goldens were counted

Each `<pkg>.json` in this directory holds every v0 field from `SPEC.md`
section 6 for one package of the `example.com/fixture` module, plus each v1
field the extractor computes that is non-null for that package. `module.json`
holds the module-level row (`SPEC.md` section 8.1); see "module" at the end. The values
were counted by hand from the source, then re-derived independently: a
throwaway `go/ast` + `go/scanner` script for sizes, imports, declarations and
duplication, `wc -c` for bytes, and golangci-lint's `gocognit` (min complexity
0) for cognitive complexity. All three agreed with the hand counts. A v1 field
missing from a golden is not compared, so a null value cannot be expressed
here; `internal/lang/golang/assemble_test.go` checks the null cases.

If you edit any `.go` file under `testdata/go/fixture`, the byte-derived
fields (`tokens_est`, `tokens_est_with_tests`) and possibly line counts change.
Recount and update the goldens and this file in the same change. Do not
regenerate goldens from the extractor; that defeats their purpose.

## Isolation from the root module

The fixture has its own `go.mod` and is not listed in any `go.work`. The Go
tool ignores directories named `testdata` when expanding `./...`, and
golangci-lint loads packages the same way, so the root module's `go build`,
`go vet`, `go test` and lint never see these packages. That matters because
`hidden` deliberately contains package-level vars and `init()` functions that
the root lint config forbids. `go list ./...` at the repository root prints no
`example.com/fixture` package.

Build and vet the fixture from its own root:

```sh
cd testdata/go/fixture && go build ./... && go vet ./...
```

`example.com/extmod` lives in `testdata/go/extmod` and is wired in by
`replace example.com/extmod => ../extmod`. Directory replacements need no
`go.sum` entry.

`testdata/go/.gitattributes` disables line-ending conversion so byte counts
are the same on every checkout.

## Counting rules used

- **files**: non-test `.go` files, generated files included.
- **sloc**: lines of non-test files that are not blank and whose first
  non-space characters are not `//`. The fixture has no `/* */` comments.
  Lines with code and a trailing comment would count, but there are none.
- **largest_file_sloc**: the largest per-file `sloc`.
- **tokens_est**: total bytes of non-test files divided by 3.2, truncated
  toward zero. Bytes are summed first, then divided once.
- **tokens_est_with_tests**: the same over all `.go` files in the directory,
  including internal and external test files.
- **internal_imports / external_imports / stdlib_imports**: distinct imported
  packages in non-test files, keyed by package path. Blank (`_`) and dot (`.`)
  imports count like any other; the fixture has none. Internal means the path
  starts with `example.com/fixture/`. Standard library means the first path
  element has no dot. Everything else is external.
- **fan_in**: distinct fixture packages whose non-test files import this one,
  by the same rule, so every internal edge counts once on each side and
  `sum(fan_in) = sum(internal_imports) = 4` over the fixture.
- **fan_in_tests**: distinct fixture packages that import this one only from
  test files. An external test package (`tested_test`) importing its own
  package is folded into that package (section 6.2) and is not a fan-in edge.
  No fixture test file imports another fixture package, so this is 0
  everywhere.
- **exported_symbols**: exported top-level funcs, methods, types, and var and
  const names in non-test files. The fixture has no exported types, vars,
  consts or methods, so this equals the exported func count.
- **globals**: names declared by package-level `var` in non-test files,
  excluding `_`. See the `hidden` section for why specs and names agree here.
- **init_funcs**: top-level `func init()` declarations.
- **max_nesting**: deepest stack of `if`, `for`, `range`, `switch`, type
  switch, `select` and func literal inside any function body. The function
  body itself is depth 0; `case` clauses and `else` add nothing.
- **func_count**: top-level funcs and methods in non-test files, including
  `init` functions and functions in generated files.
- **cognitive_total / cognitive_p90**: gocognit rules per function in
  non-test files. p90 is nearest-rank: sort the per-function scores
  ascending, take the value at rank `ceil(0.9 * n)` (1-based).
- **dup_blocks / duplication_pct**: see `dupes` below. Generated files and
  test files are excluded from duplication.
- **test_files**: `_test.go` files, internal and external test packages.
- **test_funcs**: top-level `Test*`, `Benchmark*`, `Fuzz*` and `Example*`
  funcs in test files.
- **has_tests**: `test_funcs > 0`.
- **untested_exports**: exported funcs and methods with no reference from any
  test file of the package (section 6.4).
- **instability**, **abstractness**, **main_sequence_distance** (v1) are
  rounded to 3 decimals; the distance is computed from the unrounded ratios.
  Every fixture value is 0 or 1, so rounding changes none.
- **instability** (v1): `internal_imports / (fan_in + internal_imports)`,
  null when both are 0. Present in a golden only when non-null: `hub` is 0
  (fan-in 4, fan-out 0); `a`, `b`, `hidden` and `tested` are 1 (fan-in 0,
  fan-out 1); `dupes` and `trivial` have no internal edges and are null.
- **abstractness** (v1): exported interface types over exported types, null
  with no exported types. A type counts as an interface when its underlying
  type is one, so `type R io.Reader` and `type R = io.Reader` count. No
  fixture package exports a type (`dupes`' `tally`
  is unexported), so it is null everywhere and absent from every golden.
- **main_sequence_distance** (v1): `|abstractness + instability - 1|`, null
  when either is null, so null everywhere in the fixture.
- **dup_blocks_cross_pkg** (v1): duplicate blocks, by the `dup_blocks` rules,
  found over one stream of every package's non-test, non-generated files
  and whose occurrences lie in two or more packages; each counts once in
  every package it touches. See "Cross-package duplication" below: `a` and
  `b` are 1, every other package 0. The module-level row that `check`
  reports under `module` counts the distinct blocks, so it is 1 too.
- **uses_cgo** (v1): a non-test file imports `"C"`. False everywhere in the
  fixture; `testdata/go/cgo` covers true.
- **uses_reflect** (v1): a non-test file imports `reflect` or `unsafe`.
  True for `hidden` only, whose `setup.go` imports `unsafe`.
- **generated_files** (v1): non-test files with a
  `// Code generated ... DO NOT EDIT.` line before the package clause. 1 for
  `hub` (`zz_generated.go`), 0 elsewhere.

## trivial

One file, `trivial.go`, 151 bytes.

- `sloc=4`: `package`, `func Answer() int {`, `return 42`, `}`.
- `tokens_est = 151 / 3.2 = 47.2 -> 47`, same with tests (no test files).
- Functions: `Answer` 0. Total 0, p90 0. `func_count=1`, `max_nesting=0`.
- `exported_symbols=1` (`Answer`); no test files, so `untested_exports=1`.
- Nothing imports it: `fan_in=0`.

## a

One file, `a.go`, 431 bytes. Imports `example.com/fixture/hub` only.

- `sloc=18`: package, import, `Checksum` (lines 7-19, 13 lines), `Label`
  (3 lines).
- `tokens_est = 431 / 3.2 = 134.7 -> 134`.
- Functions: `Checksum` 4 (`for` +1 at nesting 0, `if` +2 at nesting 1,
  top-level `if` +1), `Label` 0. Sorted `[0, 4]`, rank `ceil(1.8) = 2` ->
  p90 4. Total 4. `func_count=2`, `max_nesting=2` (`for` then `if`).
- `exported_symbols=2`, `untested_exports=2`, `internal_imports=1`.
- `dup_blocks=0` (one copy in the package), `dup_blocks_cross_pkg=1`.

## b

One file, `b.go`, 494 bytes. Imports `example.com/fixture/hub` only.

- `sloc=18`: package, import, `Limit` (3 lines), `Digest` (lines 13-25,
  13 lines).
- `tokens_est = 494 / 3.2 = 154.4 -> 154`.
- Functions: `Limit` 0, `Digest` 4, scored as `Checksum`. Sorted `[0, 4]`
  -> p90 4. Total 4. `func_count=2`, `max_nesting=2`.
- `exported_symbols=2`, `untested_exports=2`, `internal_imports=1`.
- `dup_blocks=0`, `dup_blocks_cross_pkg=1`.

### Cross-package duplication

`Digest` in `b` is `Checksum` from `a` with every identifier and literal
renamed, so both normalize to the same 65-token sequence:

```
func ID ( ID ID ) ID { ID := LIT for ID := LIT ; ID < ID ( ID ) ; ID ++ {
ID = ( ID * LIT + ID ( ID [ ID ] ) ) % LIT if ID < LIT { ID = - ID } }
if ID == LIT { return LIT } return ID }
```

well over the 40-token minimum. The neighbours differ on both sides:
`Checksum` follows `import LIT` and precedes `func` (`Label`), `Digest`
follows `LIT ) }` (the end of `Limit`) and ends the file, so the maximal
repeat is exactly one function long: one block, covering `a.go` lines 7-19
and `b.go` lines 13-25. Each package holds one occurrence, so neither has a
`dup_blocks` repeat of its own, and each counts the block once in
`dup_blocks_cross_pkg`. A throwaway `go/scanner` script comparing every
pair of non-test, non-generated files in different packages found no other
shared run of 40 or more tokens: after `a.go` and `b.go` (65), the
longest is 17 tokens, between `a.go` and `tested/count.go`. `dupes`' three
copies lie in one package and are not cross-package.

## hub

Two files: `hub.go` (568 bytes, 20 SLOC) and `zz_generated.go` (111 bytes,
4 SLOC). The generated file's first line is
`// Code generated by hand for the fixture. DO NOT EDIT.`, which matches Go's
generated-file convention. It is excluded from duplication only; every other
metric counts it. So `files=2`, `sloc=24`, `func_count=4`, and its bytes are
in `tokens_est`. It is the one file that `generated_files=1` counts.

- `hub.go` SLOC: package, `import (`, `"strings"`, `"example.com/extmod"`,
  `)`, then `Normalize` (3 lines), `Clamp` (9 lines), `Twice` (3 lines) = 20.
- `largest_file_sloc=20`.
- `tokens_est = (568 + 111) / 3.2 = 679 / 3.2 = 212.2 -> 212`.
- Imports: `strings` (stdlib 1), `example.com/extmod` (external 1). No
  internal imports.
- `fan_in=4`: `tested`, `hidden`, `a` and `b` each import `hub` from a
  non-test file.
- Functions: `Normalize` 0, `Clamp` 2 (two top-level `if`, +1 each), `Twice`
  0, `double` 0 (generated file). Sorted `[0, 0, 0, 2]`, rank
  `ceil(3.6) = 4` -> p90 2. Total 2.
- `max_nesting=1` (the `if` statements in `Clamp`).
- `exported_symbols=3` (`Normalize`, `Clamp`, `Twice`; `double` is
  unexported). No test files, so `untested_exports=3`.
- `instability = 0 / (4 + 0) = 0`: the most stable package in the fixture.

## tested

Non-test files: `tested.go` (514 bytes, 16 SLOC) and `count.go` (245 bytes,
11 SLOC). Test files: `tested_test.go` (359 bytes, package `tested`),
`bench_test.go` (137 bytes, package `tested`) and `external_test.go`
(208 bytes, package `tested_test`).

- `sloc=27`, `largest_file_sloc=16`.
- `tokens_est = 759 / 3.2 = 237.2 -> 237`.
- `tokens_est_with_tests = (759 + 359 + 137 + 208) / 3.2 = 1463 / 3.2 =
  457.2 -> 457`.
- Imports in non-test files: `strconv`, `strings`, `unicode` (stdlib 3) and
  `example.com/fixture/hub` (internal 1). Test-file imports (`testing`, and
  the external test's import of `tested` itself) are not counted.
- Functions: `Join` 1 (`for` at nesting 0), `Bounded` 0, `CountUpper` 4
  (`for` +1, `if` at nesting 1 +2, one `&&` sequence +1). Sorted `[0, 1, 4]`,
  rank `ceil(2.7) = 3` -> p90 4. Total 5.
- `max_nesting=2` (`for` then `if` in `CountUpper`).
- `test_files=3`; `test_funcs=4`: `TestJoin`, `TestBounded`,
  `TestCountUpper`, `BenchmarkJoin`.
- `untested_exports=0`: `Join` (from `TestJoin` and `BenchmarkJoin`),
  `Bounded` (`TestBounded`) and `CountUpper` (`TestCountUpper` in the
  external test package) are all referenced.

## hidden

Two files: `hidden.go` (628 bytes, 27 SLOC) and `setup.go` (96 bytes, 5 SLOC:
package, import, `func init() {`, the send, `}`). Imports
`example.com/fixture/hub` (in `hidden.go`) and `unsafe` (in `setup.go`,
for `unsafe.Sizeof`).

- `sloc=32`, `largest_file_sloc=27`.
- `tokens_est = 724 / 3.2 = 226.3 -> 226`.
- `internal_imports=1`, `stdlib_imports=1` (`unsafe` is a standard-library
  package), `uses_reflect=true`.
- `globals=4`: `var limit = 3` declares one name. The `var ( ... )` block
  declares `events`, `done` and `counter`, one name per spec. Counting names
  gives 4 and counting specs also gives 4, so the fixture does not depend on
  which reading of "var specs" the extractor uses. Counting declarations
  (blocks) would give 2, which is wrong.
- `init_funcs=2`: one `init` in `hidden.go`, one in `setup.go`.
- `max_nesting=4`: in `Drain`, `if` (1) contains `for ... range` (2) contains
  `switch` (3) whose `case "wait":` contains `select` (4). Case clauses do not
  add depth.
- Functions: `init` (hidden.go) 0, `Drain` 10, `init` (setup.go) 0. `Drain`
  scores `if` +1 at nesting 0, `for` +1+1, `switch` +1+2, `select` +1+3 =
  1 + 2 + 3 + 4 = 10. Sorted `[0, 0, 10]`, rank 3 -> p90 10. Total 10.
- `func_count=3` (two `init` plus `Drain`).
- `exported_symbols=1` (`Drain`; the vars are unexported). No test files, so
  `untested_exports=1`.

## dupes

One non-test file, `dupes.go` (1621 bytes, 67 SLOC), and one test file,
`dupes_test.go` (175 bytes, package `dupes`).

- `sloc=67`: the five-line package comment is excluded; package clause 1,
  three copies of 18 lines each, `const partialLimit = 7` 1,
  `type tally struct{ n int }` 1, `Describe` 10. 1 + 54 + 1 + 1 + 10 = 67.
- `tokens_est = 1621 / 3.2 = 506.6 -> 506`.
- `tokens_est_with_tests = 1796 / 3.2 = 561.3 -> 561`.
- Functions: `SumOrders` 6, `TallyScores` 6, `CountVisits` 6, `Describe` 1.
  Each copy scores `for` +1 at nesting 0, two `if` at nesting 1 (+2 each),
  and a top-level `if` +1 = 6; unlabeled `break` and `continue` add nothing.
  `Describe` has one `switch` +1. Sorted `[1, 6, 6, 6]`, rank `ceil(3.6) = 4`
  -> p90 6. Total 19.
- `max_nesting=2` (`for` then `if` in each copy).
- `exported_symbols=4`; `partialLimit` and `tally` are unexported.
- `test_files=1`, `test_funcs=1` (`TestDescribe`). The test references only
  `Describe`, so `untested_exports=3` (`SumOrders`, `TallyScores`,
  `CountVisits`).

### Duplication

Tokens come from `go/scanner` without comments. Identifiers (including
`true`, `nil` and type names like `int`) become `ID`; string, char, int,
float and imaginary literals become `LIT`; keywords and operators stay as
they are.

The three copies (`dupes.go` lines 9-26, 31-48, 53-70) have different names,
identifiers and literals but the same 68-token normalized sequence each, well
over the 40-token minimum. The declarations around them are chosen so each
copy has a different neighbor on both sides once auto-inserted semicolons are
set aside:

| Copy | Token before | Token after |
| --- | --- | --- |
| `SumOrders` | `ID` (`dupes` in the package clause) | `const` |
| `TallyScores` | `LIT` (`7`) | `type` |
| `CountVisits` | `}` (end of `struct{ n int }`) | `func` (`Describe`) |

So the maximal repeated sequence is exactly one function body long, the same
sequence occurs three times, and no second maximal repeat of 40 or more tokens
exists anywhere in the package. `dup_blocks=1`. A brute-force search over all
position pairs confirmed this.

Covered lines are every line holding a token of any occurrence: 3 x 18 = 54
lines, all of them SLOC. `duplication_pct = 54 / 67 * 100 = 80.597 -> 80.6`.

**Semicolon rule (SPEC.md 6.3):** automatically inserted semicolons (the
`token.SEMICOLON` tokens `go/scanner` emits with literal `"\n"`) are dropped
from the normalized stream before matching. They have no source text, and the
one before each `func` sits on the previous declaration's line. If an
extractor keeps them, each occurrence grows by that leading semicolon (82
tokens with semicolons) and also covers lines 6, 28 and 50 (the package
clause, the const and the type). That gives 57 covered lines and
`duplication_pct=85.1`, still with `dup_blocks=1`. Section 6.3 of `SPEC.md`
requires dropping them, so the golden is 80.6. The other counting rules above
are fixed in section 6.5.

All other packages have no repeated 40-token sequence: `dup_blocks=0`,
`duplication_pct=0`. `hub`'s generated file would not count even if it
repeated.

## module

`module.json` is the module-level row the extractor reports under the id
`module`, not a package. Every v0 field is 0 and every v1 field is null
except the module-wide `dup_blocks_cross_pkg`, which is the number of
distinct cross-package blocks: 1, the `Checksum`/`Digest` block described
under "Cross-package duplication". The conformance suite requires the sum
of `dup_blocks_cross_pkg` over packages to be at least twice the row, since
a block counts once in each of the two or more packages it touches; here
the sum is `a` 1 + `b` 1 = 2, exactly twice the row, because the one block
touches exactly two packages.
