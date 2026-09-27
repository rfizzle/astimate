# How the TypeScript fixture goldens were counted

Each `<pkg>.json` in this directory holds every v0 field from `SPEC.md`
section 6 for one package of the TypeScript fixture module, plus each v1
field the extractor computes that is non-null for that package. The package
identifiers are directories relative to the module root (there is no module
path), so `hub` is `golden/hub.json`. The values were counted by hand from
the source and cross-checked with `wc -c` for bytes and
`grep -cvE '^\s*(//.*)?$'` for source lines (the fixture has no block
comments). The packages mirror the Go fixture in `testdata/go/fixture`.

If you edit any `.ts` file under `testdata/ts/fixture`, the byte-derived
fields (`tokens_est`, `tokens_est_with_tests`) and possibly line counts change.
Recount and update the goldens and this file in the same change. Do not
regenerate goldens from the extractor; that defeats their purpose.

The fixture is never compiled: `extmod` and `vitest` are not installed, and
nothing needs them, since the extractor reads syntax only.
`testdata/ts/.gitattributes` disables line-ending conversion so byte counts
are the same on every checkout.

## Module layout

- The module root is the directory holding `package.json`.
- `tsconfig.json` maps the alias `@app/*` to `./*` from `baseUrl` `.`. It has
  a comment and trailing commas, which the extractor accepts as `tsc` does.
- `tools/` has its own `package.json`, so it is a separate module and not a
  package here: `tools/gen.ts` counts nowhere.
- `hub/types.d.ts` is a declaration file and counts nowhere.
- `tested/__tests__/count.test.ts` is a test file of package `tested`.

## Counting rules used

- **files**: non-test `.ts` and `.tsx` files, declaration files excluded.
- **sloc**: lines holding a byte outside comments and white space. The
  fixture's comments are all whole-line `//` comments.
- **largest_file_sloc**: the largest per-file `sloc`.
- **tokens_est**: total bytes of non-test files divided by 3.2, truncated.
- **tokens_est_with_tests**: the same over non-test and test files.
- **internal_imports**: distinct other packages that a relative or `@app/*`
  specifier in a non-test file resolves to. A specifier resolving to the
  importing package itself (`./count` in `tested`) is intra-package and not
  counted. A specifier resolves to the file it names, with `.ts`, `.tsx`,
  `.d.ts`, `.js` or `.jsx` appended, before a directory of that name; the
  package is the file's directory, or the directory itself.
- **external_imports**: distinct npm package names of bare specifiers.
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
  `*.spec.tsx`, or under `__tests__/`.
- **test_funcs**: calls to `it`, `test` or `bench`, including member and
  curried forms such as `it.each(table)(...)`, whose first argument is a
  string or template literal, at the top level of a test file or in the body
  of a `describe` callback. Each call counts once.
- **untested_exports**: exported functions (including exported arrow-function
  constants and functions exported through an `export { ... }` list) and the
  public methods of exported classes, other than constructors and accessors,
  whose name appears as no identifier in any test file of the package. This
  is name matching without type information.
- **instability** (v1): `internal_imports / (fan_in + internal_imports)`,
  null when both are 0.
- **abstractness** (v1): exported interfaces over exported classes,
  interfaces, type aliases and enums, null with none. Only `hub` exports
  types.
- **main_sequence_distance** (v1): `|abstractness + instability - 1|`, null
  when either is null.

## trivial

One file, `trivial.ts`, 109 bytes.

- `sloc=3`: `export function answer(): number {`, `return 42;`, `}`.
- `tokens_est = 109 / 3.2 = 34.1 -> 34`, the same with tests (none).
- `answer` scores 0; `func_count=1`, `max_nesting=0`.
- `exported_symbols=1`; no test files, so `untested_exports=1`.
- `fan_in=0`, `fan_in_tests=1`: `dupes/dupes.test.ts` imports `@app/trivial`
  and `dupes`' source does not.
- No internal edges: instability null.

## a

One file, `a.ts`, 154 bytes. Imports `../hub`, which resolves to the `hub`
directory.

- `sloc=4`: import, function line, return, closing brace.
- `tokens_est = 154 / 3.2 = 48.1 -> 48`.
- `label` scores 0. `internal_imports=1`, `exported_symbols=1`,
  `untested_exports=1`, instability `1 / (0 + 1) = 1`.

## b

One file, `b.ts`, 143 bytes. Imports `@app/hub`, which the alias maps to
`./hub`.

- `sloc=2`: import and the `export const limit = ...` line.
- `tokens_est = 143 / 3.2 = 44.7 -> 44`.
- `limit` is an arrow function bound to a `const`: one function scoring 0,
  one exported symbol, one untested export. `internal_imports=1`,
  instability 1.

## hub

One counted file, `index.ts` (745 bytes, 31 SLOC); `types.d.ts` is
excluded.

- SLOC: two imports, `export type Level`, `normalize` 3 lines, `clamp` 9,
  `twice` 3, `double` 3, class `Counter` 10 (declaration, field, `add` 4
  lines, `reset` 3 lines, closing brace) = 2 + 1 + 3 + 9 + 3 + 3 + 10 = 31.
- `tokens_est = 745 / 3.2 = 232.8 -> 232`, the same with tests.
- Imports: `node:path` (stdlib `path`), `extmod` (external). No internal.
- `fan_in=4`: `a` (`../hub`), `b` (`@app/hub`), `hidden` (`@app/hub/index`
  and `../hub`, one edge) and `tested` (`../hub/index`).
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
`dupes.test.ts` (347 bytes).

- `sloc=66`: the five-line comment is excluded; three copies of 18 lines
  each, `const partialLimit = 7;` 1, `type Tally = { n: number }` 1,
  `describeValue` 10. 54 + 1 + 1 + 10 = 66.
- `tokens_est = 1689 / 3.2 = 527.8 -> 527`.
- `tokens_est_with_tests = 2036 / 3.2 = 636.3 -> 636`.
- No imports in the non-test file. The test's import of `@app/trivial` is
  `trivial`'s `fan_in_tests`.
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
