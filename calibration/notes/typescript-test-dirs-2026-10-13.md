# TypeScript test-only directories and module-wide has_tests (2026-10-13)

## Change

A TypeScript package is a directory with at least one non-test source file
(SPEC 13.1). A test file in a directory with none, the Mocha, Vitest and Jest
layout with a top-level `test/` or `tests/`, was skipped at load, so what it
names read as untested and the packages it covers read as having no tests.
Such files are now parsed like any other test file and their identifiers join
the module-wide test reference index that `untested_exports` reads. The
directory is still no package and has no row. `has_tests` follows the Go rule
of the same date: `test_funcs > 0`, or some non-directive candidate of the
package is named in the index. `test_files`, `test_funcs` and `fan_in_tests`
stay the package's own; a test file of no package adds to none of them.

## Measurement

The corpus collection `calibration/data/2026-09-28-typescript` holds only
`packages.jsonl` and `run.json`. Re-measuring needs the 20 repositories it
names, which are not cloned here, and cloning is not allowed on this run, so
the corpus-wide count of packages whose `has_tests` flips and of the fall in
`untested_exports` is not measured. It needs one offline replay of that
collection once the clones exist.

What was measured:

- Fixture `testdata/ts/fixture`: the new package `covered` (`reach` and
  `stray`, tested only from `test/covered.test.ts`) reads `has_tests: true`
  and `untested_exports: 1`; without the change it read `false` and `2`.
  `trivial`, covered by `dupes/dupes.test.ts` through `answer`, flips from
  `has_tests: false` to `true`; its `untested_exports` is unchanged (3). No
  other fixture package changes.
- `/Users/copierson/Projects/ai-shell` has no test file at all, so nothing
  changes there: `src/helpers` still fails `has_tests` and
  `untested_exports`, and for the reason it always did.

## Limits

Matching is by name, without types, as before: a test-only directory that
names `parse` marks every package's `parse` tested. The module-wide index was
already this coarse for tests inside packages.
