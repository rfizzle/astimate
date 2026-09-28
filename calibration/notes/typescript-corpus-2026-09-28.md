# TypeScript reference corpus

Date: 2026-09-28. Machine: darwin/arm64, 14 cores, Go 1.27.1.

SPEC.md 13 calibrates thresholds per language; the defaults are fitted to
Go (`calibration/corpus.md`). This note records the TypeScript corpus in
`calibration/corpus-typescript.yaml`, how it was collected into
`calibration/data/2026-09-28-typescript/`, and how the `typescript`
override in `internal/config/default.yaml` was fitted from it. The fit and
its distributions are in `calibration/reports/thresholds-2026-09-28-typescript.md`.

## Selection criteria

A repository is in the corpus when all of these hold:

1. **Well regarded and widely used.** Roughly 10k GitHub stars or more, or
   a place among the most depended-on npm packages. The
   `stars_or_dependents` figures are the author's approximate recollection,
   as for the Go corpus.
2. **Actively maintained.** A commit on the default branch within six
   months of the run. Every pin is the default branch's HEAD on the run
   date, and `run.json` records each pin's commit date: the oldest is
   rxjs, 2026-08-05.
3. **Permissively licensed.** MIT or Apache-2.0 here, read from each
   clone's `LICENSE`; the collector's schema check admits the same five
   licenses as for Go.
4. **Written in TypeScript.** `.ts` and `.tsx` source, not JavaScript with
   JSDoc types (svelte, webpack, eslint and prettier are out) and not
   mostly `.vue` or `.svelte` files, which the extractor does not read.
5. **Analyzable from a plain clone.** The extractor reads no
   `node_modules`: it resolves internal imports from relative specifiers
   and the module root's `tsconfig.json` (`paths`, `baseUrl`, `extends`),
   and an `extends` naming an npm package that is not installed simply ends
   the chain. Since nothing it resolves depends on an install, no
   repository was installed or built; a code generator's output that is
   not committed is absent from the measurement, as it would be for a user
   checking a clone.
6. **Not generated-heavy or a test corpus.** The TypeScript compiler's own
   repository is out: `tests/cases` holds thousands of plain `.ts` test
   inputs that no file rule marks as tests.
7. **Spread across kinds.** Single-package libraries and monorepos,
   framework cores and adapters, tooling, and applications (Electron,
   React and server code), with sizes from immer's 6 packages to actual's
   179.

### Module roots

TypeScript has no module path: a module is the directory of a
`package.json` (SPEC.md 13.1), and a directory below it with its own
`package.json` is a separate module. A monorepo is therefore many modules,
and each corpus entry lists the module roots collected from it (`modules`,
`path.Match` patterns allowed). Only the directories that ship the product
are listed: samples (nest's `sample/`), integration-test apps (graphql-js's
`integrationTests/`, TanStack Query's `integrations/`), starter templates
(create-vite's `template-*`, xstate's `templates/`), documentation sites
and test-only workspaces (`insomnia-smoke-test`, `query-test-utils`,
trpc's `tests`, vue's `runtime-test` and `packages-private`) are not. Each
root is ranked on its own, with one load, exactly as `astimate rank` ranks
it, so `fan_in` and `internal_imports` count edges inside one workspace
package; an import of a sibling workspace package by its npm name is
external, as it is for a user checking that package.

### Excluded packages

TypeScript has no test-file convention as strict as Go's `_test.go`: the
extractor counts `*.test.*`, `*.spec.*` and `__tests__/` as tests, but many
repositories keep plain `.ts` files in a separate test tree (typeorm's
`test/` alone is 903 packages). The collector leaves out of the pool any
package whose directory, relative to its module root, has a segment named
`test`, `tests`, `__tests__`, `__mocks__`, `__fixtures__`, `fixture`,
`fixtures`, `e2e`, `example`, `examples`, `bench`, `benchmark`,
`benchmarks`, `demo`, `demos`, `docs`, `playground`, `sandbox`, `website`
or `www`, or a segment one of whose words (split at `-`, `_` and `.`) is
`test`, `tests`, `spec`, `specs` or `e2e` (`runtime-tests`,
`__performance_tests__`, `dts-test`). Such packages still count in the
other packages' `fan_in` and imports, which the module load measures;
1,052 packages were left out, and `run.json` counts them per repository.

### Considered and excluded

| Repository | Reason |
| --- | --- |
| `microsoft/TypeScript` | `tests/cases` is thousands of plain `.ts` compiler inputs; `checker.ts` alone is an outlier of a kind no user writes |
| `microsoft/vscode` | thousands of packages; one repository would dominate the pool |
| `angular/angular`, `vercel/next.js`, `storybookjs/storybook` | very large, mixed JavaScript and TypeScript, fixture-heavy |
| `sveltejs/svelte`, `webpack/webpack`, `eslint/eslint`, `prettier/prettier` | JavaScript with JSDoc types |
| `date-fns/date-fns` | one directory per function: hundreds of one-file packages would skew every size percentile down |
| `n8n-io/n8n`, `calcom/cal.com`, `twentyhq/twenty`, `outline/outline`, `tldraw/tldraw` | not permissively licensed (Sustainable Use, AGPL-3.0, BSL, custom) |

## Running the collection

From the repository root, with network access for the clones only (no
dependency is installed):

```sh
go run ./calibration/collect --corpus calibration/corpus-typescript.yaml --pin
go run ./calibration/collect --corpus calibration/corpus-typescript.yaml --out calibration/data/2026-09-28-typescript
```

The run took 75 seconds and collected 1,209 packages from 112 of the 113
module roots: mermaid's `packages/mermaid-layout-elk` holds only a
declaration file and a test file at the pin, so it has no package and no
row, though `run.json` lists it among the roots. `packages.jsonl` has the Go corpus's row format
with `module` the repository's corpus name joined with the module root
(`github.com/vuejs/core/packages/reactivity`), `package` the directory
relative to that root, and `language: typescript` on every row (Go rows
carry no `language`, so the Go data stays byte for byte what it was).
`run.json` adds the corpus `language` and, per repository, `commit_date`,
the module `roots` collected and `excluded_packages`. The TypeScript
extractor measures no module row, so there is no `modules.jsonl`, and
`--modules-only` is refused for a TypeScript corpus.

## Fitting the override

```sh
go run ./calibration/fit --data calibration/data/2026-09-28-typescript/packages.jsonl \
  --date 2026-09-28 --language typescript --base internal/config/default.yaml \
  --base-data calibration/data/2026-09-28-corpus/packages.jsonl
```

`--language typescript` fits the TypeScript rows by the SPEC.md 11.1 rules
against the base's top-level (Go) rules and writes the
`languages.typescript` block, not a whole configuration, to
`calibration/thresholds/astimate-thresholds-2026-09-28-typescript.yaml`.
The block holds one whole rule for each base rule the data fitted a
statistic for: a `max`, or a `max_delta` from the IQR. The zero-tolerance
ratchets (`dup_blocks`, `untested_exports`, `globals`, `init_funcs`), whose
only limit is a `max_delta` of 0 kept by policy, and the `has_tests`
requirement are left to the top level, since the fit would copy them
unchanged. `max_nesting` is overridden for its `max`, with its
`max_delta` 0 kept. `dup_blocks_cross_pkg` gets no rule: the TypeScript
extractor leaves it null, and the gate skips a rule whose metric is null
at head, so the top-level rule never fires on a TypeScript package and no
`disabled: true` is needed. `internal_imports` is pooled from every row,
as every row is from a cloned module. The rebuild parameters are not
overridden: they are calibrated by the rebuild experiments of SPEC.md
11.2, not by a corpus. `--base-data` adds the Go rows' percentiles beside
the TypeScript ones in the report.

The block is pasted into `internal/config/default.yaml` under
`languages:`; `config_version` stays `thresholds-2026-09-28`, and reports
on TypeScript show `thresholds-2026-09-28+typescript`. Tests in
`calibration/fit` check that the refit reproduces the committed block and
report byte for byte and that the default carries exactly the block's
rules.

## Known limits

- **Syntax errors.** tree-sitter reported syntax errors in 45 files (12 in
  graphql-js, 10 in rxjs, 8 in kysely, the rest one to three per
  repository), mostly newer syntax the bundled grammar lacks. The
  extractor measures the rest of each file; the error is logged at info
  level.
- **Separate test trees.** Where tests live apart from the code they test
  (`test/` beside `src/`), the source package has no test files, so
  `has_tests` is false and every export counts as untested: 864 of the
  1,209 pooled packages have no tests, and as new packages 47% would fail
  `has_tests` and 74% `untested_exports`. Those rules are policies the fit
  does not change; the numbers are in the report.
- **`tokens_est` above the context budget.** The fitted `tokens_est` max,
  35,000, is above the default `context_budget` of 25,000: 167 of the
  1,209 pooled packages (14%) are above the budget, and 280 (23%) have
  `agent_passes` above 1, so a TypeScript package can pass the gate's
  "rebuild bar" and still not rebuild in one agent pass. The Go top
  level's 16,000 happened to sit below the budget; SPEC.md 11.1 fits the
  capacity max at the p90 regardless.
- **Popularity figures are unverified.** See criterion 1.
