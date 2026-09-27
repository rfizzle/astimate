# How the cgo goldens were counted

`native.json` and `user.json` hold every v0 field from `SPEC.md` section 6
for the two packages of the `example.com/cgo` module. They are counted by
hand from `native/native.go` and `user/user.go` with the rules in
`../../fixture/golden/COUNTING.md`, and describe the source files only. With
a working C compiler, go/packages hands the extractor the files cgo generated
for `native` (its rewritten source plus `_cgo_gotypes.go` and an import
stub); none of those count. The conformance test that compares them skips
when cgo or its C compiler is unavailable, because the module does not load
then.

If you edit either `.go` file, recount and update the goldens and this file in
the same change. Do not regenerate goldens from the extractor.

## native

`native/native.go` is 12 lines, 245 bytes:

| Line | Text | Counts |
|------|------|--------|
| 1 | `// Package native ...` | comment |
| 2 | `package native` | code |
| 3 | | blank |
| 4-6 | `/* static int add(...) ... */` | comment (the cgo preamble) |
| 7 | `import "C"` | code |
| 8 | | blank |
| 9 | `// Add returns ...` | comment |
| 10 | `func Add(a, b int) int {` | code |
| 11 | `return int(C.add(C.int(a), C.int(b)))` | code |
| 12 | `}` | code |

- **files** 1, **sloc** 5, **largest_file_sloc** 5.
- **tokens_est** and **tokens_est_with_tests**: 245 / 3.2 = 76.6, truncated
  to 76. There are no test files.
- **imports**: the only import is `"C"`, a pseudo-package that is not a
  dependency, so internal, external and stdlib are all 0. The `unsafe`,
  `syscall` and `runtime/cgo` imports belong to the generated files.
- **fan_in** 1: `user` imports `native`. **fan_in_tests** 0.
- **exported_symbols** 1 (`Add`). **globals** 0, **init_funcs** 0: the
  generated package-level vars (`_Cgo_always_false` and the C function
  pointers) are not in the source.
- **func_count** 1 (`Add`), **max_nesting** 0, **cognitive_total** 0,
  **cognitive_p90** 0.
- **dup_blocks** 0, **duplication_pct** 0: one short function holds no
  repeated block.
- **test_files** 0, **test_funcs** 0, **has_tests** false,
  **untested_exports** 1 (`Add`).

## user

`user/user.go` is 13 lines, 272 bytes. Code lines are 2 (`package`), 4
(`import`), 7 (`func Sum`), 8 (`total := 0`), 9 (`for`), 10 (the call), 11
(`}`), 12 (`return`) and 13 (`}`); lines 1 and 6 are comments and 3 and 5
blank.

- **files** 1, **sloc** 9, **largest_file_sloc** 9.
- **tokens_est** and **tokens_est_with_tests**: 272 / 3.2 = 85.
- **internal_imports** 1 (`example.com/cgo/native`), external and stdlib 0.
- **fan_in** 0, **fan_in_tests** 0.
- **exported_symbols** 1 (`Sum`), **globals** 0, **init_funcs** 0.
- **func_count** 1; the `for` loop gives **max_nesting** 1 and
  **cognitive_total** 1, so **cognitive_p90** is 1.
- **dup_blocks** 0, **duplication_pct** 0.
- **test_files** 0, **test_funcs** 0, **has_tests** false,
  **untested_exports** 1 (`Sum`).
