# Threshold override thresholds-2026-09-28+typescript

The `languages.typescript` override block of the configuration (SPEC.md 9 and 13), fitted from typescript rows alone. The base's top-level rules, fitted to Go, are what every other language is judged by; "Base" below means them, and "Candidate" the typescript override. Each rule the data fitted a statistic for replaces the top-level rule on its metric for typescript; every other rule is inherited. The rebuild parameters are not overridden: they cannot be calibrated from a corpus and wait for the rebuild experiments of SPEC.md 11.2.

- Data: `calibration/data/2026-09-28-typescript/packages.jsonl`, 1209 packages from 112 module(s): `github.com/Kong/insomnia/packages/insomnia`, `github.com/Kong/insomnia/packages/insomnia-analytics`, `github.com/Kong/insomnia/packages/insomnia-api`, `github.com/Kong/insomnia/packages/insomnia-data`, `github.com/Kong/insomnia/packages/insomnia-inso`, `github.com/Kong/insomnia/packages/insomnia-scripting-environment`, `github.com/Kong/insomnia/packages/insomnia-testing`, `github.com/Kong/insomnia/packages/insomnia-vcs`, `github.com/ReactiveX/rxjs/packages/rxjs`, `github.com/TanStack/query/packages/angular-query-experimental`, `github.com/TanStack/query/packages/eslint-plugin-query`, `github.com/TanStack/query/packages/lit-query`, `github.com/TanStack/query/packages/preact-query`, `github.com/TanStack/query/packages/query-async-storage-persister`, `github.com/TanStack/query/packages/query-broadcast-client-experimental`, `github.com/TanStack/query/packages/query-core`, `github.com/TanStack/query/packages/query-devtools`, `github.com/TanStack/query/packages/query-persist-client-core`, `github.com/TanStack/query/packages/query-sync-storage-persister`, `github.com/TanStack/query/packages/react-query`, `github.com/TanStack/query/packages/react-query-devtools`, `github.com/TanStack/query/packages/react-query-next-experimental`, `github.com/TanStack/query/packages/react-query-persist-client`, `github.com/TanStack/query/packages/solid-query`, `github.com/TanStack/query/packages/vue-query`, `github.com/actualbudget/actual/packages/api`, `github.com/actualbudget/actual/packages/cli`, `github.com/actualbudget/actual/packages/component-library`, `github.com/actualbudget/actual/packages/crdt`, `github.com/actualbudget/actual/packages/desktop-client`, `github.com/actualbudget/actual/packages/desktop-electron`, `github.com/actualbudget/actual/packages/loot-core`, `github.com/actualbudget/actual/packages/mobile-client`, `github.com/actualbudget/actual/packages/plugins-service`, `github.com/actualbudget/actual/packages/sync-server`, `github.com/colinhacks/zod/packages/zod`, `github.com/desktop/desktop/app`, `github.com/excalidraw/excalidraw/excalidraw-app`, `github.com/excalidraw/excalidraw/packages/common`, `github.com/excalidraw/excalidraw/packages/element`, `github.com/excalidraw/excalidraw/packages/excalidraw`, `github.com/excalidraw/excalidraw/packages/fractional-indexing`, `github.com/excalidraw/excalidraw/packages/laser-pointer`, `github.com/excalidraw/excalidraw/packages/math`, `github.com/excalidraw/excalidraw/packages/utils`, `github.com/graphql/graphql-js`, `github.com/honojs/hono`, `github.com/immerjs/immer`, `github.com/kysely-org/kysely`, `github.com/mermaid-js/mermaid/packages/mermaid`, `github.com/mermaid-js/mermaid/packages/mermaid-layout-tidy-tree`, `github.com/mermaid-js/mermaid/packages/mermaid-zenuml`, `github.com/mermaid-js/mermaid/packages/parser`, `github.com/microsoft/playwright`, `github.com/microsoft/playwright/packages/dashboard`, `github.com/microsoft/playwright/packages/extension`, `github.com/microsoft/playwright/packages/html-reporter`, `github.com/microsoft/playwright/packages/playwright`, `github.com/microsoft/playwright/packages/playwright-client`, `github.com/microsoft/playwright/packages/playwright-core`, `github.com/microsoft/playwright/packages/recorder`, `github.com/microsoft/playwright/packages/trace-viewer`, `github.com/microsoft/playwright/packages/web`, `github.com/nestjs/nest/packages/common`, `github.com/nestjs/nest/packages/core`, `github.com/nestjs/nest/packages/microservices`, `github.com/nestjs/nest/packages/platform-express`, `github.com/nestjs/nest/packages/platform-fastify`, `github.com/nestjs/nest/packages/platform-socket.io`, `github.com/nestjs/nest/packages/platform-ws`, `github.com/nestjs/nest/packages/testing`, `github.com/nestjs/nest/packages/websockets`, `github.com/reduxjs/redux-toolkit/packages/rtk-codemods`, `github.com/reduxjs/redux-toolkit/packages/rtk-query-codegen-openapi`, `github.com/reduxjs/redux-toolkit/packages/rtk-query-graphql-request-base-query`, `github.com/reduxjs/redux-toolkit/packages/toolkit`, `github.com/statelyai/xstate/packages/core`, `github.com/statelyai/xstate/packages/xstate-immer`, `github.com/statelyai/xstate/packages/xstate-inspect`, `github.com/statelyai/xstate/packages/xstate-react`, `github.com/statelyai/xstate/packages/xstate-solid`, `github.com/statelyai/xstate/packages/xstate-store`, `github.com/statelyai/xstate/packages/xstate-store-angular`, `github.com/statelyai/xstate/packages/xstate-store-preact`, `github.com/statelyai/xstate/packages/xstate-store-react`, `github.com/statelyai/xstate/packages/xstate-store-solid`, `github.com/statelyai/xstate/packages/xstate-store-svelte`, `github.com/statelyai/xstate/packages/xstate-store-vue`, `github.com/statelyai/xstate/packages/xstate-svelte`, `github.com/statelyai/xstate/packages/xstate-vue`, `github.com/trpc/trpc/packages/client`, `github.com/trpc/trpc/packages/next`, `github.com/trpc/trpc/packages/openapi`, `github.com/trpc/trpc/packages/react-query`, `github.com/trpc/trpc/packages/server`, `github.com/trpc/trpc/packages/tanstack-react-query`, `github.com/trpc/trpc/packages/upgrade`, `github.com/typeorm/typeorm/packages/typeorm`, `github.com/vitejs/vite/packages/create-vite`, `github.com/vitejs/vite/packages/plugin-legacy`, `github.com/vitejs/vite/packages/vite`, `github.com/vuejs/core/packages/compiler-core`, `github.com/vuejs/core/packages/compiler-dom`, `github.com/vuejs/core/packages/compiler-sfc`, `github.com/vuejs/core/packages/compiler-ssr`, `github.com/vuejs/core/packages/reactivity`, `github.com/vuejs/core/packages/runtime-core`, `github.com/vuejs/core/packages/runtime-dom`, `github.com/vuejs/core/packages/server-renderer`, `github.com/vuejs/core/packages/shared`, `github.com/vuejs/core/packages/vue`, `github.com/vuejs/core/packages/vue-compat`
- Base configuration: `thresholds-2026-09-28`
- Candidate: `calibration/thresholds/astimate-thresholds-2026-09-28-typescript.yaml`
- Generated by `go run ./calibration/fit`

## Method

- Percentiles are nearest-rank: the p-th percentile of n values is the value at rank ceil(p/100 * n), so it is always an observed value. IQR is p75 minus p25.
- A capacity rule's max, and a density rule's max where the base rule has one, is the 90th percentile rounded to two significant figures and then to the nearest readable step: 500 above 1000, 50 above 100, 5 above 10, otherwise 1 (0.5 for a percentage). A capacity max is at least one step.
- A density rule's max_delta is a quarter of the IQR rounded up to a whole step, at least 1 for a count and 0.5 for a percentage, except that a rule whose base max_delta is 0 keeps it: zero tolerance on new duplicate blocks, untested exports, globals, init functions and nesting is a policy, not a statistic.
- internal_imports is pooled from cloned-module rows only, since the standard library is loaded as one module and counts every standard-library import as internal; a module-wide metric (dup_blocks_cross_pkg) is pooled from the module rows only, one per module, since the gate evaluates it there alone; every other metric is pooled from all package rows.
- changed_func_cognitive_max is a diff against a baseline, so it is fitted per function with every function of every row counted as new, as in a package new at head: its max is the 99th percentile of per-function cognitive complexity, rounded the same way, since a single function past the corpus's own worst percentile is the signal.
- Kinds, warn_at, ratchet_from_zero, when guards, requirement rules, the rebuild parameters and every other key are copied from the base unchanged; a density rule with no max in the base gets none.

"Fail as new" counts pooled packages that would violate the rule as a package new at head, with no baseline: above max, above max_delta from zero where ratchet_from_zero is set, or not meeting a requirement whose guard holds. For `changed_func_cognitive_max` it counts functions above max, and its section also counts the packages holding one.

## Summary

Base is `thresholds-2026-09-28`, candidate `thresholds-2026-09-28+typescript`. Rows counts the rows the metric was measured on and names which rows fed it.

| Metric | Kind | Rows | p90 | IQR | Base max | Candidate max | Base max_delta | Candidate max_delta | Fail as new, base | Fail as new, candidate |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `dup_blocks` | density | 1209, all rows | 40 | 11 | none | none | 0 | 0 | 783 (64.8%) | 783 (64.8%) |
| `duplication_pct` | density | 1209, all rows | 46 | 26.8 | 40 | 45 | 6 | 7 | 170 (14.1%) | 129 (10.7%) |
| `untested_exports` | density | 1209, all rows | 37 | 13 | none | none | 0 | 0 | 891 (73.7%) | 891 (73.7%) |
| `globals` | density | 1209, all rows | 1 | 0 | none | none | 0 | 0 | 182 (15.1%) | 182 (15.1%) |
| `init_funcs` | density | 1209, all rows | 0 | 0 | none | none | 0 | 0 | 120 (9.9%) | 120 (9.9%) |
| `max_nesting` | density | 1209, all rows | 7 | 4 | 5 | 7 | 0 | 0 | 221 (18.3%) | 81 (6.7%) |
| `cognitive_p90` | density | 1209, all rows | 27 | 13 | 20 | 25 | 3 | 4 | 192 (15.9%) | 133 (11.0%) |
| `changed_func_cognitive_max` | density | 39878, functions of all rows | 11 | 4 | 50 | 55 | none | none | 464 (1.2%) | 404 (1.0%) |
| `dup_blocks_cross_pkg` | density | 0, module rows | none | none | 450 | 450 | 0 | 0 | 0 (0%) | 0 (0%) |
| `tokens_est` | capacity | 1209, all rows | 34818 | 11045 | 16000 | 35000 | none | none | 233 (19.3%) | 120 (9.9%) |
| `largest_file_sloc` | capacity | 1209, all rows | 891 | 350 | 600 | 900 | none | none | 196 (16.2%) | 118 (9.8%) |
| `exported_symbols` | capacity | 1209, all rows | 57 | 18 | 60 | 55 | none | none | 111 (9.2%) | 124 (10.3%) |
| `internal_imports` | capacity | 1209, cloned-module rows only | 8 | 4 | 10 | 8 | none | none | 78 (6.5%) | 120 (9.9%) |
| `sloc` | capacity | 1209, all rows | 2508 | 854 | 1000 | 2500 | none | none | 284 (23.5%) | 121 (10.0%) |
| `has_tests` | requirement | 1209, all rows | 1 | 1 | none | none | none | none | 567 (46.9%) | 567 (46.9%) |

## Override

- Replaced for typescript: `duplication_pct`, `max_nesting`, `cognitive_p90`, `changed_func_cognitive_max`, `tokens_est`, `largest_file_sloc`, `exported_symbols`, `internal_imports`, `sloc`.
- Inherited, zero-tolerance ratchets with no max (max_delta 0 is a policy, not a statistic): `dup_blocks`, `untested_exports`, `globals`, `init_funcs`.
- Inherited, requirements (not fitted): `has_tests`.
- Inherited, measured by no typescript row: `dup_blocks_cross_pkg`.

## Corpus

`calibration/corpus-typescript.yaml`, 20 repositories collected at astimate `1d8c6d8b3274`. Module roots are the workspace packages collected, each ranked on its own; excluded packages are test, fixture, example, benchmark and documentation directories left out of the pool.

| Repository | Commit | Commit date | Module roots | Packages | Excluded |
| --- | --- | --- | ---: | ---: | ---: |
| `github.com/honojs/hono` | `fc2343dbad8a` | 2026-09-28 | 1 | 72 | 8 |
| `github.com/immerjs/immer` | `061c2425e1c9` | 2026-08-19 | 1 | 6 | 0 |
| `github.com/kysely-org/kysely` | `50ccaf8c7f44` | 2026-09-27 | 1 | 31 | 3 |
| `github.com/graphql/graphql-js` | `ee5ce41d4b68` | 2026-09-09 | 1 | 14 | 1 |
| `github.com/colinhacks/zod` | `2bf7b0630d53` | 2026-09-23 | 1 | 13 | 2 |
| `github.com/ReactiveX/rxjs` | `54796b38a57e` | 2026-08-05 | 1 | 4 | 3 |
| `github.com/typeorm/typeorm` | `f279fd1367f2` | 2026-09-21 | 1 | 68 | 903 |
| `github.com/TanStack/query` | `2e1ad64b525f` | 2026-09-28 | 16 | 50 | 1 |
| `github.com/trpc/trpc` | `ec0b0a47ab6c` | 2026-09-27 | 7 | 52 | 41 |
| `github.com/vuejs/core` | `4ab865a848a1` | 2026-09-18 | 11 | 27 | 0 |
| `github.com/vitejs/vite` | `e57f4da9c6fd` | 2026-09-28 | 3 | 18 | 2 |
| `github.com/nestjs/nest` | `b58554ea5857` | 2026-09-25 | 9 | 121 | 5 |
| `github.com/statelyai/xstate` | `fbee62e7c158` | 2026-09-15 | 14 | 32 | 3 |
| `github.com/mermaid-js/mermaid` | `69778e6e995c` | 2026-09-21 | 5 | 84 | 3 |
| `github.com/reduxjs/redux-toolkit` | `c9dac937d77a` | 2026-09-19 | 4 | 27 | 10 |
| `github.com/excalidraw/excalidraw` | `438d89861f53` | 2026-09-27 | 8 | 62 | 4 |
| `github.com/desktop/desktop` | `f2686bcec923` | 2026-09-25 | 1 | 125 | 19 |
| `github.com/microsoft/playwright` | `b9a34ac7783a` | 2026-09-25 | 10 | 78 | 20 |
| `github.com/Kong/insomnia` | `ae09eea24dc0` | 2026-09-24 | 8 | 146 | 11 |
| `github.com/actualbudget/actual` | `24deae74e653` | 2026-09-27 | 10 | 179 | 13 |

## Go against typescript

Go is `calibration/data/2026-09-28-corpus/packages.jsonl`, the rows the top-level rules were fitted from, pooled by the same rules; typescript is this data. Percentiles are per package, except `changed_func_cognitive_max`, per function (p99 in the p90 column, since its max is fitted there). Max and max_delta are the top-level rule's and the rule typescript is judged by.

| Metric | Go rows | Go p50 | Go p90 | typescript rows | typescript p50 | typescript p90 | Go max | typescript max | Go max_delta | typescript max_delta |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `dup_blocks` | 2347 | 1 | 20 | 1209 | 2 | 40 | none | none | 0 | 0 |
| `duplication_pct` | 2347 | 6.9 | 40.7 | 1209 | 10.9 | 46 | 40 | 45 | 6 | 7 |
| `untested_exports` | 2347 | 2 | 23 | 1209 | 3 | 37 | none | none | 0 | 0 |
| `globals` | 2347 | 0 | 7 | 1209 | 0 | 1 | none | none | 0 | 0 |
| `init_funcs` | 2347 | 0 | 1 | 1209 | 0 | 0 | none | none | 0 | 0 |
| `max_nesting` | 2347 | 3 | 5 | 1209 | 3 | 7 | 5 | 7 | 0 | 0 |
| `cognitive_p90` | 2347 | 6 | 22 | 1209 | 7 | 27 | 20 | 25 | 3 | 4 |
| `changed_func_cognitive_max` | 80280 | 1 | 51 | 39878 | 1 | 56 | 50 | 55 | none | none |
| `dup_blocks_cross_pkg` | 0 | none | none | 0 | none | none | 450 | 450 | 0 | 0 |
| `tokens_est` | 2347 | 2077 | 16272 | 1209 | 3773 | 34818 | 16000 | 35000 | none | none |
| `largest_file_sloc` | 2347 | 123 | 586 | 1209 | 158 | 891 | 600 | 900 | none | none |
| `exported_symbols` | 2347 | 8 | 60 | 1209 | 7 | 57 | 60 | 55 | none | none |
| `internal_imports` | 1989 | 3 | 11 | 1209 | 2 | 8 | 10 | 8 | none | none |
| `sloc` | 2347 | 154 | 1224 | 1209 | 289 | 2508 | 1000 | 2500 | none | none |
| `has_tests` | 2347 | 1 | 1 | 1209 | 0 | 1 | none | none | none | none |

## What changed most

- `sloc` max loosens: 1000 to 2500. The pooled median is 289 and p90 is 2508; as new packages, 284 (23.5%) of the pool failed the base max and 121 (10.0%) fail the candidate.
- `tokens_est` max loosens: 16000 to 35000. The pooled median is 3773 and p90 is 34818; as new packages, 233 (19.3%) of the pool failed the base max and 120 (9.9%) fail the candidate.
- `largest_file_sloc` max loosens: 600 to 900. The pooled median is 158 and p90 is 891; as new packages, 196 (16.2%) of the pool failed the base max and 118 (9.8%) fail the candidate.
- `max_nesting` max loosens: 5 to 7. The pooled median is 3 and p90 is 7; as new packages, 221 (18.3%) of the pool failed the base max and 81 (6.7%) fail the candidate.
- `cognitive_p90` max loosens: 20 to 25. The pooled median is 7 and p90 is 27; as new packages, 192 (15.9%) of the pool failed the base max and 133 (11.0%) fail the candidate.
- `internal_imports` max tightens: 10 to 8. The pooled median is 2 and p90 is 8; as new packages, 78 (6.5%) of the pool failed the base max and 120 (9.9%) fail the candidate.
- `duplication_pct` max loosens: 40 to 45. The pooled median is 10.9 and p90 is 46; as new packages, 170 (14.1%) of the pool failed the base max and 129 (10.7%) fail the candidate.
- `changed_func_cognitive_max` max loosens: 50 to 55. The per-function median is 1, p90 11 and p99 56; 464 (1.2%) of the pooled functions are above the base max and 404 (1.0%) above the candidate.
- `exported_symbols` max tightens: 60 to 55. The pooled median is 7 and p90 is 57; as new packages, 111 (9.2%) of the pool failed the base max and 124 (10.3%) fail the candidate.
- max_delta moves on `duplication_pct` 6 to 7, `cognitive_p90` 3 to 4: a quarter of each IQR.
- max_delta stays 0 on `dup_blocks`, `untested_exports`, `globals`, `init_funcs`, `max_nesting` whatever the IQR: zero tolerance on these is a policy, not a statistic (SPEC.md 11.1).

## Per-function cognitive complexity

Every function of every pooled row, counted as new (`changed_func_cognitive_max`), 39878 functions in 1031 packages:

| Functions | p50 | p90 | p99 | max |
| ---: | ---: | ---: | ---: | ---: |
| 39878 | 1 | 11 | 56 | 993 |

The candidate max is p99 56 rounded to 55. Above the base max 50: 464 (1.2%) of functions, in 206 (20.0%) of packages. Above the candidate: 404 (1.0%) of functions, in 187 (18.1%) of packages.

## Not fitted

- `dup_blocks_cross_pkg`: no typescript row measures it, so the override sets no rule and the top-level rule applies (max 450, max_delta 0). The gate skips a rule whose metric is null at head, so it never fires on a typescript package whose extractor leaves the metric null.

## Per metric

### `dup_blocks` (density)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 0 | 2 | 11 | 40 | 75 | 453 | 11 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 8.33) | 866 | 71.6% |
| [8.33, 16.67) | 106 | 8.8% |
| [16.67, 25) | 52 | 4.3% |
| [25, 33.33) | 50 | 4.1% |
| [33.33, 41.67) | 19 | 1.6% |
| [41.67, 50) | 17 | 1.4% |
| [50, 58.33) | 11 | 0.9% |
| [58.33, 66.67) | 16 | 1.3% |
| [66.67, 75] | 12 | 1.0% |
| > 75 (to 453) | 60 | 5.0% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | none | 0 | 783 (64.8%) |
| Candidate | none | 0 | 783 (64.8%) |

### `duplication_pct` (density)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 0 | 10.9 | 26.8 | 46 | 66 | 99.8 | 26.8 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 7.33) | 530 | 43.8% |
| [7.33, 14.67) | 165 | 13.6% |
| [14.67, 22) | 150 | 12.4% |
| [22, 29.33) | 94 | 7.8% |
| [29.33, 36.67) | 69 | 5.7% |
| [36.67, 44) | 63 | 5.2% |
| [44, 51.33) | 35 | 2.9% |
| [51.33, 58.67) | 23 | 1.9% |
| [58.67, 66] | 20 | 1.7% |
| > 66 (to 99.8) | 60 | 5.0% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 40 | 6 | 170 (14.1%) |
| Candidate | 45 | 7 | 129 (10.7%) |

### `untested_exports` (density)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 0 | 3 | 13 | 37 | 83 | 803 | 13 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 9.22) | 843 | 69.7% |
| [9.22, 18.44) | 158 | 13.1% |
| [18.44, 27.67) | 56 | 4.6% |
| [27.67, 36.89) | 31 | 2.6% |
| [36.89, 46.11) | 25 | 2.1% |
| [46.11, 55.33) | 11 | 0.9% |
| [55.33, 64.56) | 9 | 0.7% |
| [64.56, 73.78) | 9 | 0.7% |
| [73.78, 83] | 10 | 0.8% |
| > 83 (to 803) | 57 | 4.7% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | none | 0 | 891 (73.7%) |
| Candidate | none | 0 | 891 (73.7%) |

### `globals` (density)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 0 | 0 | 0 | 1 | 3 | 28 | 0 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 0.33) | 1027 | 84.9% |
| [0.33, 0.67) | 0 | 0.0% |
| [0.67, 1) | 0 | 0.0% |
| [1, 1.33) | 86 | 7.1% |
| [1.33, 1.67) | 0 | 0.0% |
| [1.67, 2) | 0 | 0.0% |
| [2, 2.33) | 34 | 2.8% |
| [2.33, 2.67) | 0 | 0.0% |
| [2.67, 3] | 10 | 0.8% |
| > 3 (to 28) | 52 | 4.3% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | none | 0 | 182 (15.1%) |
| Candidate | none | 0 | 182 (15.1%) |

### `init_funcs` (density)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 0 | 0 | 0 | 0 | 1 | 8 | 0 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 0.11) | 1089 | 90.1% |
| [0.11, 0.22) | 0 | 0.0% |
| [0.22, 0.33) | 0 | 0.0% |
| [0.33, 0.44) | 0 | 0.0% |
| [0.44, 0.56) | 0 | 0.0% |
| [0.56, 0.67) | 0 | 0.0% |
| [0.67, 0.78) | 0 | 0.0% |
| [0.78, 0.89) | 0 | 0.0% |
| [0.89, 1] | 95 | 7.9% |
| > 1 (to 8) | 25 | 2.1% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | none | 0 | 120 (9.9%) |
| Candidate | none | 0 | 120 (9.9%) |

### `max_nesting` (density)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 1 | 3 | 5 | 7 | 8 | 29 | 4 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 0.89) | 258 | 21.3% |
| [0.89, 1.78) | 109 | 9.0% |
| [1.78, 2.67) | 148 | 12.2% |
| [2.67, 3.56) | 171 | 14.1% |
| [3.56, 4.44) | 174 | 14.4% |
| [4.44, 5.33) | 128 | 10.6% |
| [5.33, 6.22) | 97 | 8.0% |
| [6.22, 7.11) | 43 | 3.6% |
| [7.11, 8] | 31 | 2.6% |
| > 8 (to 29) | 50 | 4.1% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 5 | 0 | 221 (18.3%) |
| Candidate | 7 | 0 | 81 (6.7%) |

### `cognitive_p90` (density)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 1 | 7 | 14 | 27 | 42 | 993 | 13 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 4.67) | 491 | 40.6% |
| [4.67, 9.33) | 251 | 20.8% |
| [9.33, 14) | 139 | 11.5% |
| [14, 18.67) | 103 | 8.5% |
| [18.67, 23.33) | 73 | 6.0% |
| [23.33, 28) | 32 | 2.6% |
| [28, 32.67) | 27 | 2.2% |
| [32.67, 37.33) | 20 | 1.7% |
| [37.33, 42] | 15 | 1.2% |
| > 42 (to 993) | 58 | 4.8% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 20 | 3 | 192 (15.9%) |
| Candidate | 25 | 4 | 133 (11.0%) |

### `changed_func_cognitive_max` (density)

Rows: 39878, functions of all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 39878 | 0 | 0 | 1 | 4 | 11 | 20 | 993 | 4 |

| Range | Functions | Share |
| --- | ---: | ---: |
| [0, 2.22) | 26734 | 67.0% |
| [2.22, 4.44) | 4403 | 11.0% |
| [4.44, 6.67) | 2310 | 5.8% |
| [6.67, 8.89) | 1379 | 3.5% |
| [8.89, 11.11) | 1363 | 3.4% |
| [11.11, 13.33) | 555 | 1.4% |
| [13.33, 15.56) | 485 | 1.2% |
| [15.56, 17.78) | 354 | 0.9% |
| [17.78, 20] | 408 | 1.0% |
| > 20 (to 993) | 1887 | 4.7% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 50 | none | 464 (1.2%) |
| Candidate | 55 | none | 404 (1.0%) |

### `dup_blocks_cross_pkg` (density)

Rows: 0, module rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 450 | 0 | 0 (0%) |
| Candidate | 450 | 0 | 0 (0%) |

### `tokens_est` (capacity)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 5 | 1043 | 3773 | 12088 | 34818 | 59143 | 345720 | 11045 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [5, 6575.89) | 759 | 62.8% |
| [6575.89, 13146.78) | 175 | 14.5% |
| [13146.78, 19717.67) | 72 | 6.0% |
| [19717.67, 26288.56) | 44 | 3.6% |
| [26288.56, 32859.44) | 32 | 2.6% |
| [32859.44, 39430.33) | 28 | 2.3% |
| [39430.33, 46001.22) | 13 | 1.1% |
| [46001.22, 52572.11) | 15 | 1.2% |
| [52572.11, 59143] | 11 | 0.9% |
| > 59143 (to 345720) | 60 | 5.0% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 16000 | none | 233 (19.3%) |
| Candidate | 35000 | none | 120 (9.9%) |

### `largest_file_sloc` (capacity)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 1 | 57 | 158 | 407 | 891 | 1354 | 11801 | 350 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [1, 151.33) | 587 | 48.6% |
| [151.33, 301.67) | 218 | 18.0% |
| [301.67, 452) | 129 | 10.7% |
| [452, 602.33) | 79 | 6.5% |
| [602.33, 752.67) | 46 | 3.8% |
| [752.67, 903) | 32 | 2.6% |
| [903, 1053.33) | 20 | 1.7% |
| [1053.33, 1203.67) | 27 | 2.2% |
| [1203.67, 1354] | 11 | 0.9% |
| > 1354 (to 11801) | 60 | 5.0% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 600 | none | 196 (16.2%) |
| Candidate | 900 | none | 118 (9.8%) |

### `exported_symbols` (capacity)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 2 | 7 | 20 | 57 | 107 | 1147 | 18 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 11.89) | 755 | 62.4% |
| [11.89, 23.78) | 195 | 16.1% |
| [23.78, 35.67) | 72 | 6.0% |
| [35.67, 47.56) | 44 | 3.6% |
| [47.56, 59.44) | 29 | 2.4% |
| [59.44, 71.33) | 18 | 1.5% |
| [71.33, 83.22) | 15 | 1.2% |
| [83.22, 95.11) | 16 | 1.3% |
| [95.11, 107] | 5 | 0.4% |
| > 107 (to 1147) | 60 | 5.0% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 60 | none | 111 (9.2%) |
| Candidate | 55 | none | 124 (10.3%) |

### `internal_imports` (capacity)

Rows: 1209, cloned-module rows only.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 0 | 2 | 4 | 8 | 13 | 94 | 4 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 1.44) | 577 | 47.7% |
| [1.44, 2.89) | 157 | 13.0% |
| [2.89, 4.33) | 194 | 16.0% |
| [4.33, 5.78) | 60 | 5.0% |
| [5.78, 7.22) | 74 | 6.1% |
| [7.22, 8.67) | 27 | 2.2% |
| [8.67, 10.11) | 42 | 3.5% |
| [10.11, 11.56) | 6 | 0.5% |
| [11.56, 13] | 17 | 1.4% |
| > 13 (to 94) | 55 | 4.5% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 10 | none | 78 (6.5%) |
| Candidate | 8 | none | 120 (9.9%) |

### `sloc` (capacity)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 1 | 80 | 289 | 934 | 2508 | 4307 | 29389 | 854 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [1, 479.44) | 735 | 60.8% |
| [479.44, 957.89) | 181 | 15.0% |
| [957.89, 1436.33) | 74 | 6.1% |
| [1436.33, 1914.78) | 49 | 4.1% |
| [1914.78, 2393.22) | 43 | 3.6% |
| [2393.22, 2871.67) | 21 | 1.7% |
| [2871.67, 3350.11) | 17 | 1.4% |
| [3350.11, 3828.56) | 19 | 1.6% |
| [3828.56, 4307] | 10 | 0.8% |
| > 4307 (to 29389) | 60 | 5.0% |

| | max | max_delta | Fail as new |
| --- | ---: | ---: | ---: |
| Base | 1000 | none | 284 (23.5%) |
| Candidate | 2500 | none | 121 (10.0%) |

### `has_tests` (requirement)

Rows: 1209, all rows.

| n | min | p25 | p50 | p75 | p90 | p95 | max | IQR |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1209 | 0 | 0 | 0 | 1 | 1 | 1 | 1 | 1 |

| Range | Packages | Share |
| --- | ---: | ---: |
| [0, 0.11) | 864 | 71.5% |
| [0.11, 0.22) | 0 | 0.0% |
| [0.22, 0.33) | 0 | 0.0% |
| [0.33, 0.44) | 0 | 0.0% |
| [0.44, 0.56) | 0 | 0.0% |
| [0.56, 0.67) | 0 | 0.0% |
| [0.67, 0.78) | 0 | 0.0% |
| [0.78, 0.89) | 0 | 0.0% |
| [0.89, 1] | 345 | 28.5% |
| > 1 (to 1) | 0 | 0.0% |

Requirement kept: require true when sloc > 100. Fail as new: 567 (46.9%).

