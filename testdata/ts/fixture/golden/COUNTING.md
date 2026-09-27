# How the TypeScript fixture goldens were counted

Each `<pkg>.json` in this directory holds every v0 field from `SPEC.md`
section 6 for one package of the TypeScript fixture module, plus each v1
field the extractor computes that is non-null for that package. The package
identifiers are directories relative to the module root (there is no module
path), so `hub` is `golden/hub.json`. The values were counted by hand from
the source and cross-checked with `wc -c` for bytes and
`grep -cvE '^\s*(//.*)?$'` for source lines (the fixture has no block
comments). The packages mirror the Go fixture in `testdata/go/fixture`.

If you edit any source file under `testdata/ts/fixture`, the byte-derived
fields (`tokens_est`, `tokens_est_with_tests`) and possibly line counts change.
Recount and update the goldens and this file in the same change.
`go test ./internal/lang/typescript -run TestConformance -update` rewrites
the goldens from the extractor, but a rewritten number is only accepted once
it has been recounted by hand here; the flag saves typing, not counting.

The fixture is never compiled: `extmod` and `vitest` are not installed, and
nothing needs them, since the extractor reads syntax only.
`testdata/ts/.gitattributes` disables line-ending conversion so byte counts
are the same on every checkout.

## Module layout

- The module root is the directory holding `package.json`.
- `tsconfig.json` extends `./config/tsconfig.base.json`, which sets `baseUrl`
  `..`, relative to itself, so the module root, and maps `@app/*` to
  `./nowhere/*`. The root file sets its own `paths`, which replace the
  parent's whole as `tsc` merges them: `@app/*` maps to `./*`, and
  `@multi/*` to `./missing/*` then `./trivial/*`. Both paths and bare
  specifiers resolve against the inherited `baseUrl`. The root file has
  comments and trailing commas, which the extractor accepts as `tsc` does.
  `config/` holds no source, so it is not a package.
- `tools/` has its own `package.json`, so it is a separate module and not a
  package here: `tools/gen.ts` counts nowhere.
- `hub/types.d.ts` and `b/types.d.mts` are declaration files and count
  nowhere.
- `tested/__tests__/count.test.ts` is a test file of package `tested`.

## Counting rules used

- **files**: non-test `.ts`, `.tsx`, `.mts` and `.cts` files, declaration
  files (`.d.ts`, `.d.mts`, `.d.cts`) excluded.
- **sloc**: lines holding a byte outside comments and white space. The
  fixture's comments are all whole-line `//` comments.
- **largest_file_sloc**: the largest per-file `sloc`.
- **tokens_est**: total bytes of non-test files divided by 3.2, truncated.
- **tokens_est_with_tests**: the same over non-test and test files.
- **internal_imports**: distinct other packages that a relative, `@app/*`
  or `@multi/*` specifier in a non-test file resolves to, or a bare
  specifier naming a file or directory under `baseUrl` (`hub` in
  `b/esm.mts`). A specifier resolving to the importing package itself
  (`./count` in `tested`) is intra-package and not counted. A specifier
  resolves to a TypeScript file only, as `tsc` resolves it: the file it
  names when that has a TypeScript source or declaration extension; with
  a JavaScript extension replaced by its TypeScript one (`.js` by `.ts`,
  `.tsx` or `.d.ts`; `.jsx` by `.tsx`; `.mjs` by `.mts` or `.d.mts`;
  `.cjs` by `.cts` or `.d.cts`); with no extension, with `.ts`, `.tsx` or
  `.d.ts` appended. Failing a file, a directory of that name resolves
  through its `package.json` `typings`, `types` or `main` target, else its
  `index` file (`.ts`, `.tsx`, `.d.ts`, then the `.mts` and `.cts` forms);
  `.`, `..` and a trailing `/` resolve as a directory only. The package is
  the resolved file's directory. So `../hub` is `hub/index.ts`, while
  `./trivial` and `./tested`, which have no index file, resolve to
  nothing. Of several alias targets the first that resolves wins. A
  relative specifier that resolves to nothing counts nowhere; a bare one
  under `baseUrl`, or an alias none of whose targets resolves, is
  classified as an npm package or built-in instead (see `b`).
- **external_imports**: distinct npm package names of bare specifiers,
  including those that fall through from `baseUrl` or an alias.
- **stdlib_imports**: distinct Node built-ins, with or without `node:`.
- **fan_in / fan_in_tests**: as for Go: packages whose non-test files import
  this one, and packages that import it from test files only.
- **exported_symbols**: names declared by `export` declarations (functions,
  classes, interfaces, type aliases, enums, namespaces, and each name a
  `let`, `const` or `var` binds), one per `export default`, and one per name
  in an `export { ... }` list. Class members are not counted.
- **globals**: names bound by top-level `let` and `var`, not `const`.
- **init_funcs**: the number of non-test files with at least one top-level
  statement that is a call (module initialization run on import).
- **func_count**: top-level function declarations, top-level `let`, `const`
  or `var` names initialized with a function or arrow function, and the
  methods and function-valued fields of top-level classes. Functions nested
  inside those are scored as part of them.
- **max_nesting / cognitive_total / cognitive_p90**: see the comment in
  `internal/lang/typescript/complexity.go`; p90 is nearest-rank as for Go.
- **dup_blocks / duplication_pct**: see `dupes` below.
- **test_files**: files named `*.test.ts`, `*.spec.ts`, `*.test.tsx`,
  `*.spec.tsx`, the `.mts` and `.cts` forms of those, or under
  `__tests__/`.
- **test_funcs**: calls to `it`, `test` or `bench`, including member and
  curried forms such as `it.each(table)(...)`, whose first argument is a
  string or template literal, at the top level of a test file or in the body
  of a `describe` callback. Each call counts once.
- **untested_exports**: exported functions (including exported arrow-function
  constants and functions exported through an `export { ... }` list) and the
  public methods of exported classes, other than constructors and accessors,
  whose name appears as no identifier in any test file of the package. This
  is name matching without type information. As in Go, a candidate whose
  doc comment has the line comment `//astimate:untested`, alone or followed
  by a space and a reason, is left out and listed by `Details` as excluded.
  The doc comment is the run of comments directly above the declaration
  with no blank line; for an export list it is the comment above the
  function's own declaration, and a comment above a class does not reach
  its methods. The doc comment of any overload signature of a function or
  method counts as the implementation's.
- **instability** (v1): `internal_imports / (fan_in + internal_imports)`,
  null when both are 0.
- **abstractness** (v1): exported interfaces over exported classes,
  interfaces, type aliases and enums, null with none. Only `hub` exports
  types.
- **main_sequence_distance** (v1): `|abstractness + instability - 1|`, null
  when either is null.

## trivial

Two files: `trivial.ts` (109 bytes, 3 SLOC) and `wrappers.ts` (577 bytes,
17 SLOC), which exercises the untested directive.

- `trivial.ts` SLOC: `export function answer(): number {`, `return 42;`,
  `}`.
- `wrappers.ts` SLOC: `wrapped` 3, `spaced` 1, `detached` 3, class `Box` 8
  (declaration, `open` 3, `close` 3, closing brace), `const listed` 1, and
  `export { listed };` 1 = 17. The comment lines and blank lines are not
  counted.
- `sloc=20`, `largest_file_sloc=17`.
- `tokens_est = 686 / 3.2 = 214.4 -> 214`, the same with tests (none).
- `exported_symbols=6`: `answer`, `wrapped`, `spaced`, `detached`, `Box`,
  and `listed` through the list.
- Functions: `answer`, `wrapped`, `spaced`, `detached`, `Box.open`,
  `Box.close` and `listed`, each scoring 0: `func_count=7`, total 0, p90 0,
  `max_nesting=0`.
- `untested_exports=4`. The candidates are `answer`, `wrapped`, `spaced`,
  `detached`, `Box.open`, `Box.close` and `listed`, and no test file names
  any. The directive leaves out three: `wrapped` (directive with a reason
  on the line above), `Box.open` (directive above the method) and `listed`
  (directive on the second line of the comment above its `const`, exported
  through the list). Four remain: `answer`; `spaced`, whose comment has a
  space after `//` and is not the directive; `detached`, whose directive is
  separated from it by a blank line and so is not its doc comment; and
  `Box.close`. `TestDetails` checks both lists.
- `fan_in=1`: `b/esm.mts` imports `@multi/trivial.js`. The first `@multi/*`
  target, `./missing/trivial.js`, does not exist; the second,
  `./trivial/trivial.js`, exists as `trivial.ts` through the `.js` to `.ts`
  mapping. `b/rejected.ts` imports `../trivial` too, but the directory has
  no index file, so that import counts nowhere. `fan_in_tests=1`:
  `dupes/dupes.test.ts` imports `@app/trivial/trivial` and `dupes`' source
  does not.
- instability `0 / (1 + 0) = 0`; abstractness `0 / 1 = 0` (`Box`, no
  interface); main_sequence_distance `|0 + 0 - 1| = 1`.

## a

One file, `a.ts`, 154 bytes. Imports `../hub`, which resolves to
`hub/index.ts`.

- `sloc=4`: import, function line, return, closing brace.
- `tokens_est = 154 / 3.2 = 48.1 -> 48`.
- `label` scores 0. `internal_imports=1`, `exported_symbols=1`,
  `untested_exports=1`, instability `1 / (0 + 1) = 1`.

## b

Four non-test files: `b.ts` (143 bytes, 2 SLOC), `esm.mts` (238 bytes,
6 SLOC), `common.cts` (118 bytes, 3 SLOC) and `rejected.ts` (355 bytes,
3 SLOC). One test file, `esm.test.mts` (129 bytes). `types.d.mts` is a
declaration file and counts nowhere.

- `b.ts` SLOC: the import and the `export const limit = ...` line.
  `esm.mts`: three imports and `bounded` 3 lines. `common.cts`: `scale` 3
  lines. `rejected.ts`: three side-effect imports under a four-line
  comment. `sloc=14`, `largest_file_sloc=6`.
- `tokens_est = (143 + 238 + 118 + 355) / 3.2 = 854 / 3.2 = 266.9 -> 266`.
- `tokens_est_with_tests = (854 + 129) / 3.2 = 983 / 3.2 = 307.2 -> 307`.
- Imports: `@app/hub` (alias to `./hub`) and bare `hub` (resolved under the
  inherited `baseUrl` to `hub/index.ts`) are package `hub`;
  `@multi/trivial.js` is package `trivial` (see `trivial`); `./common.cjs`
  resolves to `common.cts` in `b` itself and is not counted.
  `rejected.ts` holds the imports `tsc` does not resolve inside the
  module: `../trivial` names a directory with no index file and counts
  nowhere; bare `trivial` names the same directory under `baseUrl`, which
  does not resolve, so it is the npm package `trivial`; `@app/tested`
  matches `@app/*`, whose target `./tested` has no index file either, so
  it falls through to the npm package `@app/tested`.
  `internal_imports=2`, `external_imports=2`. Were `extends` not followed,
  there would be no `baseUrl` and `hub` would count as an external package.
- `exported_symbols=3`: `limit`, `bounded`, `scale`.
- Functions: `limit` (arrow function bound to a `const`), `bounded` and
  `scale`, each scoring 0: `func_count=3`.
- `test_files=1`, `test_funcs=1` (`it("bounds")`). The test names
  `bounded`, so `untested_exports=2` (`limit`, `scale`).
- instability `2 / (0 + 2) = 1`.

## hub

One counted file, `index.ts` (745 bytes, 31 SLOC); `types.d.ts` is
excluded.

- SLOC: two imports, `export type Level`, `normalize` 3 lines, `clamp` 9,
  `twice` 3, `double` 3, class `Counter` 10 (declaration, field, `add` 4
  lines, `reset` 3 lines, closing brace) = 2 + 1 + 3 + 9 + 3 + 3 + 10 = 31.
- `tokens_est = 745 / 3.2 = 232.8 -> 232`, the same with tests.
- Imports: `node:path` (stdlib `path`), `extmod` (external). No internal.
- `fan_in=4`: `a` (`../hub`), `b` (`@app/hub` and bare `hub`, one edge),
  `hidden` (`@app/hub/index` and `../hub`, one edge) and `tested`
  (`../hub/index`).
- `exported_symbols=5`: `Level`, `normalize`, `clamp`, `twice`, `Counter`.
  `double` is not exported.
- Functions: `normalize` 0, `clamp` 2 (two `if` at nesting 0), `twice` 0,
  `double` 0, `Counter.add` 0, `Counter.reset` 0. `func_count=6`, total 2,
  sorted `[0, 0, 0, 0, 0, 2]`, rank `ceil(5.4) = 6` -> p90 2.
  `max_nesting=1`.
- `untested_exports=4`: `normalize`, `clamp`, `twice` and `Counter.add`;
  `reset` is private.
- instability `0 / (4 + 0) = 0`; abstractness `0 / 2 = 0` (`Level` and
  `Counter`, no interface); main_sequence_distance `|0 + 0 - 1| = 1`.

## hidden

Two files: `hidden.ts` (619 bytes, 21 SLOC) and `setup.ts` (56 bytes,
2 SLOC).

- `sloc=23`, `largest_file_sloc=21`.
- `tokens_est = 675 / 3.2 = 210.9 -> 210`.
- Imports: `@app/hub/index` and `../hub`, both package `hub`:
  `internal_imports=1`.
- `globals=4`: `let limit`, `var events, done` (two names), `let counter`.
  `const ceiling` is not a global.
- `init_funcs=2`: `hidden.ts` has two top-level call statements
  (`events.push(...)`), counted once for the file; `limit = clamp(...)` is an
  assignment, not a call statement. `setup.ts` has `console.log(...)`.
- `drain`: `if` +1, `for...of` +2 (nesting 1), `switch` +3 (nesting 2),
  `while` +4 (nesting 3), the `&&` sequence +1, the `??` sequence +1 = 12.
  `max_nesting=4` (`if`, `for`, `switch`, `while`; the `case` adds nothing).
  `func_count=1`, total 12, p90 12.
- `exported_symbols=1`, `untested_exports=1`, instability 1.

## tested

Non-test files: `tested.ts` (539 bytes, 15 SLOC) and `count.ts` (154 bytes,
9 SLOC). Test files: `tested.test.ts` (305 bytes), `bench.spec.ts`
(111 bytes) and `__tests__/count.test.ts` (171 bytes).

- `sloc=24`, `largest_file_sloc=15`.
- `tokens_est = 693 / 3.2 = 216.6 -> 216`.
- `tokens_est_with_tests = (693 + 305 + 111 + 171) / 3.2 = 1280 / 3.2 = 400`.
- Imports in non-test files: `util` (stdlib 1), `../hub/index` (internal 1),
  `./count` (intra-package, not counted). The test files' `vitest` and
  `./tested` imports are not counted.
- `exported_symbols=4`: `join` and `bounded` through the export list,
  `shout`, and `countUpper`.
- Functions: `join` 1 (`for...of` at nesting 0), `bounded` 0, `shout` 0,
  `countUpper` 4 (`for...of` +1, `if` at nesting 1 +2, one `&&` sequence
  +1). Sorted `[0, 0, 1, 4]`, rank `ceil(3.6) = 4` -> p90 4. Total 5.
  `max_nesting=2`.
- `test_files=3`; `test_funcs=4`: `it("joins numbers")` inside `describe`,
  `test("bounded")`, `bench("join")`, and `it.each([...])("counts %s")`
  counted once.
- `untested_exports=0`: the tests name `join`, `bounded`, `shout` and
  `countUpper`.
- instability 1.

## dupes

One non-test file, `dupes.ts` (1689 bytes, 66 SLOC), and one test file,
`dupes.test.ts` (355 bytes).

- `sloc=66`: the five-line comment is excluded; three copies of 18 lines
  each, `const partialLimit = 7;` 1, `type Tally = { n: number }` 1,
  `describeValue` 10. 54 + 1 + 1 + 10 = 66.
- `tokens_est = 1689 / 3.2 = 527.8 -> 527`.
- `tokens_est_with_tests = 2044 / 3.2 = 638.75 -> 638`.
- No imports in the non-test file. The test's import of
  `@app/trivial/trivial` is `trivial`'s `fan_in_tests`; it names the file,
  as `@app/trivial` would name a directory with no index file, which `tsc`
  does not resolve.
- Functions: `sumOrders`, `tallyScores`, `countVisits` 6 each (`for...of`
  +1, two `if` at nesting 1 +2 each, a top-level `if` +1; unlabeled `break`
  and `continue` add nothing), `describeValue` 1 (`switch`). Sorted
  `[1, 6, 6, 6]` -> p90 6. Total 19. `max_nesting=2`.
- `exported_symbols=4`; `partialLimit` and `Tally` are not exported.
- `test_files=1`, `test_funcs=2` (`it`, `test`). The test names
  `describeValue`, `sumOrders` and `tallyScores`, so `untested_exports=1`
  (`countVisits`).

### Duplication

Tokens are the leaves of the tree-sitter tree, comments dropped. Identifiers
of every kind (`identifier`, `property_identifier`, `type_identifier`, ...,
and `undefined`) become `ID`; strings, template literals, regular
expressions, numbers, `true`, `false`, `null` and JSX text become `LIT`, each
one token however many leaves it has; keywords, operators and punctuation
keep their kind. Type keywords such as `number` and `string` are keywords,
not identifiers. Zero-width tokens (automatic semicolons, missing nodes) are
dropped, as Go drops its automatic semicolons; an explicit `;` is kept.

The three copies (`dupes.ts` lines 6-23, 27-44, 48-65) have different
names, identifiers and literals but the same normalized sequence, well over
40 tokens. Their neighbors differ on both sides:

| Copy | Token before | Token after |
| --- | --- | --- |
| `sumOrders` | none (start of the stream) | `const` |
| `tallyScores` | `;` (after `7`) | `type` |
| `countVisits` | `}` (end of `{ n: number }`, no semicolon) | `export` |

So the maximal repeat is exactly one function long, it occurs three times,
and no other repeat of 40 or more tokens exists: `dup_blocks=1`. Covered
lines are 3 x 18 = 54, all SLOC: `duplication_pct = 54 / 66 * 100 = 81.82
-> 81.8`. `TestDetails` checks the three locations.

Had `type Tally` ended with `;`, the second and third copies would share the
preceding `;` and form a second, one-token-longer block with two occurrences,
giving `dup_blocks=2`.

All other packages have no repeated 40-token sequence: `dup_blocks=0`,
`duplication_pct=0`.
