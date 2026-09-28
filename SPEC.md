# Astimate: Specification

**Status:** draft v0.5, 2026-09-27. Supersedes v0.2, which framed the tool as a pre-work planning signal. This revision makes the post-implementation quality gate the primary use and keeps planning and ranking as secondary uses.

Astimate is a static-analysis tool that checks whether an LLM-written change left a package in a state a human or the next agent can maintain. It extracts structural metrics per package, compares them against a baseline and a set of thresholds, and fails when the package got worse. It also produces a **rebuild estimate**: how many agent passes, and roughly how many human days, a from-scratch rebuild of the package would take. That is the microservices "rewritable in two weeks" bar, restated for packages and agents, and it drives ranking and planning. It runs as a CLI, a CI step, a Claude Code hook and a Model Context Protocol (MCP) server. Go is the first supported language; everything downstream of metric extraction is language-agnostic.

Sections 8 through 11 (gate, CLI, MCP, calibration) and 14 (milestones) are written to be split directly into stories.

---

## 1. Goals

1. **Gate:** fail a change that makes a package materially less manageable, and say exactly which rule was broken so the agent can fix it without human interpretation.
2. **Ratchet, not absolute:** judge a change against the package's own baseline, so work on legacy packages is not blocked by pre-existing debt while new debt is.
3. **Self-check loop:** let an agent run the gate itself before declaring work done, via CLI, hook or MCP.
4. **Rank and plan:** estimate rebuild effort for every package in a module so humans and agents can see which packages have outgrown the rebuildable bar and where debt is concentrated.
5. **Honest defaults:** the gate thresholds are calibrated against a reference corpus (section 11.1); the rebuild estimate is labelled as coming from uncalibrated parameters until the rebuild experiments of section 11.2 have run.

## 2. Non-goals

- Not a linter for individual findings. Astimate reports package-level structure, not line-level style.
- Not a human-effort estimator. `scc` already does COCOMO.
- Not a code-health product with proprietary scoring. CodeScene covers that space; Astimate is free, local and targets LLM failure modes specifically.
- No network calls in the default path.
- No auto-fixing. The gate reports; the agent or human changes the code.

## 3. Definitions

| Term | Meaning |
| --- | --- |
| Module | A Go module rooted at a `go.mod`. The unit of analysis for fan-in and "internal". |
| Package | A Go package directory within the module. The unit that gets metrics, a score and gate verdicts. |
| Internal import | An import whose path is inside the module path from `go.mod`. |
| Fan-out | Number of distinct internal packages this package imports. |
| Fan-in | Number of distinct internal packages that import this package. |
| Baseline | The raw metrics of a package at a reference point: a git ref (usually the merge-base with the default branch) or a committed baseline file. |
| Threshold | A per-metric limit: an absolute `max`, a `max_delta` versus the baseline, or both. |
| Violation | A metric that exceeds a threshold at head. Any violation fails the gate. |
| Raw metrics | The language-agnostic struct in section 6 that an extractor produces. |
| Rebuild estimate | Estimated agent passes and human days to rebuild the package from its tests and exported contract, computed from raw metrics and the rebuild parameters in config. |

## 4. What the gate targets (evidence summary)

The gate targets the ways LLM-written changes tend to degrade a package. The metric set and default thresholds follow from that, with the rebuild estimate's evidence kept for ranking.

| Failure mode | Metric(s) | Evidence | Strength |
| --- | --- | --- | --- |
| Copy-paste instead of extraction | `duplication_pct`, `dup_blocks` | Industry reports on AI-assisted repositories (GitClear, 2024 and 2025) show rising duplicated blocks and falling moved-or-refactored code. Not peer-reviewed, but consistent across years. | Moderate; the most specific LLM failure mode found |
| Package bloat past what the next agent can hold | `tokens_est`, `sloc`, `largest_file_sloc` | Successful agent trajectories typically stay under 20 to 30k tokens, and single-shot resolve rates collapse at 64k tokens of context (arXiv 2602.16069); long context remains a weakness for every model tested (arXiv 2505.07897); failed agent trajectories are consistently longer than successful ones (arXiv 2511.00197). | Strong |
| Exporting everything | `exported_symbols` | No direct study. Wider API surface raises fan-in cost and the facts the next agent must hold (Coherence Debt, arXiv 2608.16630). | Indirect |
| Untested additions | `untested_exports`, `has_tests` | Agents self-correct through a run-and-check loop; a package without tests denies the next agent that loop. | Moderate, indirect |
| Hidden state | `globals`, `init_funcs` | No direct study. Plausible; kept at low weight. | Unproven |
| Coupling shape | `instability`, `abstractness`, `main_sequence_distance` | Martin's package metrics are widely reported but their validation as predictors is mixed and none exists for Go, whose consumer-defined interfaces invert the abstract-provider assumption. Reported only until the corpus measurement in 11.1 shows where good Go modules sit. | Unproven |
| Deep nesting | `max_nesting`, `cognitive_p90` | Classical complexity metrics show no consistent correlation with LLM performance (arXiv 2602.07882). Kept as a gate on regressions only. | Weak |
| Coupling growth | `internal_imports` | Failures come from coupled facts absent from context (arXiv 2608.16630; CrossCodeEval, arXiv 2310.11248). | Moderate |
| Blast radius | `fan_in` | No direct study; blast-radius reasoning: a change to a package with many importers can break each of them. Reported for ranking (`rank --sort fan_in`) and recorded in the rebuild estimate's contract detail; it enters neither the estimate formula (7.2) nor the default gate. | Unproven; reported for ranking |

## 5. Architecture

```
  ┌──────────────────────┐
  │ Go extractor         │──┐
  │ go/packages+go/types │  │   ┌─────────────┐   ┌──────────────┐   ┌────────────────┐
  └──────────────────────┘  ├──►│ Raw metrics │──►│ Gate         │──►│ CLI / CI exit  │
  ┌──────────────────────┐  │   │ head + base │   │ thresholds   │   │ GitHub Action  │
  │ Other extractors     │──┘   └──────┬──────┘   │ ratchet      │   │ Claude hook    │
  │ (tree-sitter, later) │              │          └──────────────┘   │ MCP server     │
  └──────────────────────┘              │          ┌──────────────┐   └────────────────┘
                                        └─────────►│ Rebuild est. │──────────┘
                                                   │ params       │
                                                   └──────┬───────┘
                                                          │ thresholds + params
                                                   ┌──────┴───────┐
                                                   │ Calibration  │
                                                   │ p90 of good  │
                                                   │ Go modules   │
                                                   └──────────────┘
```

**Packages** (Go module `github.com/rfizzle/astimate`, Go 1.27 or later):

```
astimate/
├── go.mod
├── cmd/astimate/main.go        # CLI entrypoint; `astimate serve` starts MCP
├── internal/
│   ├── metrics/                 # RawMetrics struct and Extractor interface (language-agnostic)
│   │   └── metricstest/         # Conformance suite every extractor runs, plus a fake extractor
│   ├── config/                  # Embedded default config and the unified loader
│   ├── lang/
│   │   └── golang/              # Go extractor
│   ├── gate/                    # Thresholds config, baseline, ratchet comparison, violations
│   ├── score/                   # Rebuild estimate, tiers, drivers, explanations
│   ├── baseline/                # Git-ref and file baselines
│   ├── report/                  # JSON, text and hook output shaping
│   └── mcpserver/               # MCP tools on github.com/modelcontextprotocol/go-sdk
├── action/                      # GitHub Action definition
├── testdata/                    # Fixture modules with known expected metrics
└── SPEC.md
```

**Extractor interface** (language-agnostic):

```go
type Extractor interface {
    Language() string
    Detect(root string) bool
    Packages(root string) ([]string, error)
    // Extract computes raw metrics for one package. The module context is a
    // pointer so the extractor can fill its cache slot on first use.
    Extract(ctx context.Context, mod *ModuleContext, pkg string) (RawMetrics, error)
}
```

**Testing pattern.** The contract is defined once in `internal/metrics` and verified once by a conformance suite in `internal/metrics/metricstest`, an importable non-test package in the style of `testing/fstest`. It exports `TestExtractor(t, ext, fixture)`, which checks detection, package listing, validation, determinism, error handling, cross-package invariants (the module-wide sum of `fan_in` equals the sum of `internal_imports`; `has_tests` agrees with `test_funcs`) and golden comparison against `testdata/<lang>/fixture/golden/*.json`. Every language extractor's own tests call it once; language-specific unit tests (import path rules, statement kinds, tokenizer normalization) stay in the nested package. `metricstest` also exports a fake extractor built from a map of `RawMetrics`, so the estimate, gate and CLI can be tested without loading a real module. `Packages` returns the language's native package identifier (Go: the full import path); golden files are named by that identifier with the module path prefix removed, so `example.com/fixture/a` is `golden/a.json` and the root package is `golden/root.json`.

When an extractor also implements the optional `ModuleMetrics`, the suite checks the module row: it validates, its v0 fields are 0 and its v1 fields null except the module-wide ones, it is deterministic and honours cancellation, the per-package sum of `dup_blocks_cross_pkg` is between twice the row's value and the row's value times the package count, and it matches `golden/module.json`. The fake takes a module row through `WithModuleRow`. An extractor may also implement the optional `ModuleDetailer`, whose `ModuleDetails` names every cross-package block of the module row (package, file relative to the module root, line range); `Details` lists the blocks touching a package. When `Details` returns `CrossBlocks`, the suite requires exactly `dup_blocks_cross_pkg` of them, each with at least two occurrences in at least two packages that `Packages` lists (the package itself among them), with slash-separated relative files and line ranges where 1 <= start <= end; when the extractor is a `ModuleDetailer`, `ModuleDetails` must name exactly the module row's `dup_blocks_cross_pkg` blocks, each listed in the `Details` of every package it touches; recorded `UntestedPositions` and `GlobalPositions` must number one per untested export and one per global; the fake takes module details through `WithModuleDetails`. An extractor implementing the optional `ImporterLister` has its `Importers` checked: sorted, distinct, excluding the package itself, exactly `fan_in` entries, unknown package rejected.

## 6. Raw metrics

Every field is reported in output. *(v0)* fields are required for the first release.

| Field | Type | Definition | Release |
| --- | --- | --- | --- |
| `files` | int | Non-test source files, generated files included (6.5) | v0 |
| `sloc` | int | Non-blank, non-comment lines in non-test, non-generated files | v0 |
| `largest_file_sloc` | int | SLOC of the largest non-test, non-generated file | v0 |
| `tokens_est` | int | Estimated tokens of non-test, non-generated source; see 6.1 | v0 |
| `tokens_est_with_tests` | int | Same including test files | v0 |
| `internal_imports` | int | Fan-out: distinct internal packages imported | v0 |
| `external_imports` | int | Distinct non-stdlib, non-module imports | v0 |
| `stdlib_imports` | int | Distinct standard-library imports | v0 |
| `fan_in` | int | Distinct internal packages importing this package | v0 |
| `fan_in_tests` | int | Packages importing this one from test files only | v0 |
| `exported_symbols` | int | Exported funcs, methods, types, vars and consts | v0 |
| `globals` | int | Package-level `var` specs in non-test files, excluding `_` | v0 |
| `init_funcs` | int | Number of `init()` functions | v0 |
| `max_nesting` | int | Deepest nesting of `if`/`for`/`range`/`switch`/`select`/func literal | v0 |
| `cognitive_total` | int | Sum of cognitive complexity (gocognit rules) | v0 |
| `cognitive_p90` | int | 90th percentile cognitive complexity per function | v0 |
| `func_count` | int | Functions and methods in non-test files | v0 |
| `dup_blocks` | int | Duplicate token sequences of at least `duplication.min_tokens` (default 40) within the package; see 6.3 | v0 |
| `duplication_pct` | float | Share of non-test SLOC covered by a duplicate block | v0 |
| `test_files` | int | `_test.go` files | v0 |
| `test_funcs` | int | `Test*`, `Benchmark*`, `Fuzz*`, `Example*` functions | v0 |
| `has_tests` | bool | `test_funcs > 0` | v0 |
| `untested_exports` | int | Exported funcs and methods not referenced from any test file in the package; see 6.4 | v0 |
| `dup_blocks_cross_pkg` | int | Duplicate blocks shared with other packages in the module: exact normalized repeats found over one module-wide stream of non-test, non-generated files, counted once in each package they touch; `dup_blocks` is computed per package and never includes them, and a sequence repeated within a package and also copied elsewhere counts in both. Null for standard-library loads, which are not modules (the module pass on the whole library is affordable, 0.8 s and 317 MiB, see `calibration/notes/cross-package-duplication-2026-09-28.md`). Gated on the module row only (8.1) | v1 |
| `instability` | float | Martin instability `Ce / (Ca + Ce)` with `Ca = fan_in`, `Ce = internal_imports`; null when both are 0 | v1 |
| `abstractness` | float | Exported interface types over all exported types; null with no exported types | v1 |
| `main_sequence_distance` | float | `|abstractness + instability - 1|`; reported, not gated, since idiomatic Go leaf packages sit near 1 by design | v1 |
| `uses_cgo` | bool | Imports `"C"` | v1 |
| `uses_reflect` | bool | Imports `reflect` or `unsafe` | v1 |
| `generated_files` | int | Files with a `Code generated ... DO NOT EDIT` header; they count in `files` and the import metrics and in no other size or structure metric (6.5) | v1 |
| `tokens_est_generated` | int | Estimated tokens of the generated non-test files that `tokens_est` leaves out, by the same method; null for a language with no generated-file convention | v1 |
| `coverage_pct` | float | Statement coverage of the package's own statements from `go test -cover -count=1 -run .`, measured only with `--coverage` (or `coverage: true` on an MCP tool): one run per invocation over the selected packages that have test files, bounded by `--coverage-timeout` (default 2m, also passed as `-timeout`). Null without the opt-in, for a package without test files or statements, and when its tests fail to build or run or the run times out (one stderr warning per package naming the reason; other metrics unaffected); 0 when passing tests cover nothing. Baselines, git or file, never run tests, so it is compared only when both sides have it; reported only, no gate rule | v1 |
| `changed_func_cognitive_max` | int | Highest cognitive complexity among functions added or modified since baseline (matched per 6.5); 0 when none changed; null without a function-level baseline diff, as in `assess`, against a baseline file written without function records, or for an extractor that cannot list functions | v1 |

### 6.1 Token estimation

Default: `tokens_est = bytes / chars_per_token`, `chars_per_token` defaulting to **3.2**. Tokenizers differ, and the default targets the Anthropic tokenizer family, since Claude Code is the first gate consumer: published measurements put code at about 2.7 characters per token on the tokenizer introduced with Opus 4.7 and about 3.7 on the one before it, so 3.2 is a middle estimate for a Claude session. OpenAI's `o200k_base` is sparser: measured on this repository's Go source it runs about 3.75 to 4.0 bytes per token for non-test code and about 2.7 to 3.3 for test code. The original spec's 4.0 therefore happens to be close for o200k and undercounts for Claude by roughly 20 percent. The ratio is a config value so it can be set per target model.

Opt-in exact counting: `--tokenizer=o200k` via `pkoukk/tiktoken-go`, offline, exact for that tokenizer only. There is no offline Anthropic tokenizer; the Anthropic count-tokens API is not called by default because it is a network dependency.

### 6.2 Test-file handling

Test files are excluded from every metric except `tokens_est_with_tests`, `test_files`, `test_funcs` and the reference side of `untested_exports`. External test packages (`foo_test`) in the same directory fold into the package.

### 6.3 Duplication

Tokens are taken from `go/scanner` over non-test files. Identifiers, literals and comments are normalized (identifier to `ID`, string and numeric literals to `LIT`) so renamed copies still match. The automatic semicolons that `go/scanner` inserts at line ends are dropped from the stream, so a repeat never extends onto a neighbouring declaration's line. A duplicate block is a maximal token sequence of length at least `duplication.min_tokens` that occurs at least twice, found with a suffix array or rolling hash over the normalized stream. Nested and overlapping repeats are merged: a shorter repeat wholly inside a longer one's occurrences is not counted separately, and occurrences that overlap are unioned before coverage is computed. `dup_blocks` counts distinct maximal sequences after merging; `duplication_pct` is covered SLOC divided by package SLOC times 100, rounded to one decimal. Generated files are excluded, from the token stream and from the package SLOC denominator alike. A block whose tokens are all literals or the punctuation `, { } : [ ] ( ) ;` is a data table, not code, and is dropped after merging when `duplication.ignore_literal_only` is true, which is the default: measured over 225 standard-library packages it moves the 90th percentile of `duplication_pct` by 0.1 and removes `html` (an entity map at 94%) and `crypto/des` from the top ten while every dropped block inspected was data (see `calibration/notes/duplication-literal-only.md`). With `duplication.fold_signs` true, which is the default, a `+` or `-` that directly precedes a numeric literal and does not follow an operand (an identifier, a literal, `)`, `]` or `}`) counts as part of the literal for that rule, so a table of negative numbers is data too; the sign stays a separate token for matching, so no match is gained or lost. Folding the sign into the normalized stream was rejected: it lets `f(-1)` match `f(1)` and raised `math` from 37.7% to 44.1% (see `calibration/notes/duplication-refinements.md`). The literal-only rule applies to whole blocks after merging; a literal table inside a block that also holds code is counted, because trimming literal edges strips closing braces off code and splitting at short runs cuts code too (see `calibration/notes/duplication-literal-runs.md`). With `duplication.split_literal_runs` true, under the literal-only rule, each block is first cut at every run of at least `duplication.min_tokens` literal-only tokens (a sign counting as literal when `fold_signs` is on); the parts of at least `duplication.min_tokens` tokens between the runs are kept as blocks with every occurrence of their block, and parts with the same first occurrence and length count once. This drops tables that declaration headers join into one block. It is off by default: over std and 36 corpus modules every changed block read was data and the only code uncovered was fragments below `min_tokens`, but it moves `duplication_pct` in 43 of 1039 corpus packages and the thresholds were fitted without it (see `calibration/notes/duplication-literal-runs-corpus-2026-09-28.md`). The threshold and normalization rules are config values.

### 6.4 Untested exports

An exported function or method counts as untested when no identifier in any test file of the package (internal or external test package) resolves, via `types.Info.Uses`, to that function or to a method with the same name on the same receiver type. Exported types, vars and consts are not counted; the metric targets behavior, not declarations. Reported as a count and, in the gate, primarily as a delta so new untested behavior fails while legacy gaps are only reported.

Two refinements. A declaration whose doc comment contains the line `//astimate:untested` (optionally followed by a reason) is excluded from the count and listed separately, for intentionally untested wrappers. A method called in a test through an interface value counts as referenced when the concrete receiver type, or a pointer to it, implements that interface and has a method of that name; the interface may come from any package. A generic receiver type is checked through each instantiation of it that appears in the package's test files; an instantiation that a test never names or holds is not seen. A selection inside a generic function declared in a test file is checked once per instantiation of that function in the test files, with its type arguments substituted into the receiver type. A method of a generic type declared in a test file is checked the same way, once per instantiation of that type in the test files, and a generic function or type instantiated inside another with that one's type parameters takes each of the enclosing one's instantiations, substituted, through chains of up to eight. A generic test type that appears only as a field type of another generic test type is not carried through; that is a known gap.

### 6.5 Counting rules

Rules that the section 6 table leaves implicit, fixed here so goldens and implementations agree (`testdata/go/fixture/golden/COUNTING.md` shows each applied):

- `globals` counts names, not specs or blocks: `var a, b = 1, 2` is 2; `_` is excluded.
- `func_count`, `cognitive_total` and `cognitive_p90` include `init()` functions.
- `cognitive_p90` is the nearest-rank 90th percentile over per-function values; a package with no functions reports 0.
- `uses_cgo`, `uses_reflect` and `generated_files` are read from the source files' import specs and headers, never from files cgo generates; blank and renamed imports of `reflect` or `unsafe` count.
- Generated files: a non-test file with a line matching `^// Code generated .* DO NOT EDIT\.$` before the package clause (`ast.IsGenerated`) counts in `files`, `generated_files` and `tokens_est_generated`, and is still read for `internal_imports`, `external_imports`, `stdlib_imports`, `fan_in`, `fan_in_tests`, `uses_cgo` and `uses_reflect`, since generated code imports real packages. It is left out of every size and structure metric: `sloc`, `largest_file_sloc`, `tokens_est`, `tokens_est_with_tests`, `func_count`, `cognitive_total`, `cognitive_p90`, `max_nesting`, `globals`, `init_funcs`, `exported_symbols`, `abstractness`, `untested_exports` (a generated export needs no test), `dup_blocks` and `duplication_pct` (the stream and the `sloc` denominator alike), and the function list behind `changed_func_cognitive_max`. A rebuild reruns the generator rather than writing its output, so the section 7 estimate, unchanged in formula, leaves the generated volume out. A generated `_test.go` file is a test file and still counts in `tokens_est_with_tests`. For a cgo package the rule reads the source files, never the files cgo writes, which cgo marks as generated.
- `internal_imports`, `external_imports`, `stdlib_imports`, `fan_in` and `fan_in_tests` count the distinct packages that the import specs of source files resolve to, keyed by package path. A blank (`_`) or dot (`.`) import counts like any other. An import that only files cgo generates hold (such as `runtime/cgo`) counts in no class, so every internal edge counts once in `fan_in` and once in `internal_imports`, and their module-wide sums are equal.
- `abstractness` counts an exported type as an interface when its underlying type is an interface, so a definition or alias naming one (`type R io.Reader`, `type R = io.Reader`) counts.
- `instability`, `abstractness` and `main_sequence_distance` are rounded to three decimals; `main_sequence_distance` is computed from the unrounded ratios.
- `changed_func_cognitive_max` diffs functions, never the package: a head function is matched to a baseline function of the same package by receiver base type (pointer, type parameters and parentheses stripped; empty for a plain function) and name, as a multiset so repeated names such as `init` pair up. A matched function is modified when its fingerprint differs. The fingerprint is a hash of the body's syntax tree with identifiers normalized to `ID` and literals to `LIT` as in 6.3; comments and positions are not in it, so comment, layout and gofmt edits, variable renames and literal values do not mark a function changed, while any change to operators or control flow does. A direct call to the function's own name is kept distinct, since cognitive complexity scores recursion. A renamed function or a method moved to another receiver is new; a deleted function is not changed; every function of a package new at head is changed. `init` functions count.
- `tokens_est` sums bytes across files first, then divides by `chars_per_token` and truncates.
- `fan_in_tests` counts other packages whose test files import this package; a package's own external test package importing it does not count.
- `sloc` counts a line with code and a trailing comment as code.
- For a cgo package, every syntactic metric is counted from the package's Go source files, never from the files cgo generates; `import "C"` is not an import of any class. The type-dependent `untested_exports` uses the type-checked trees, whose declarations and doc comments match the source.

## 7. Rebuild estimate

The estimate answers one question: if this package were deleted and rebuilt from its tests and exported contract, how much work is that? It is secondary to the gate and drives `rank`, `assess` and the summary line of `check` once calibrated (8.5). It has units, so it can be measured (section 11.2) and argued with.

### 7.1 Inputs

A rebuild must reproduce a contract and pass a spec, and the raw metrics describe both:

| Input | From | Role |
| --- | --- | --- |
| Essential volume | `tokens_est * (1 - duplication_pct / 100)` | Code that must be written; duplicates collapse in a rebuild |
| Spec | `tokens_est_with_tests - tokens_est`, `test_funcs` | Tests are the executable specification the rebuild is checked against |
| Contract | `exported_symbols` | Signatures that must survive. `fan_in` is recorded in the term's detail, not in the formula (7.2) |
| Unspecified behavior | `untested_exports`, scaled by `coverage_pct` when measured | Behavior that must be reverse-engineered from the old implementation |
| Hidden contract | `globals`, `init_funcs` | State and ordering that no signature reveals |

### 7.2 Agent estimate

Everything a rebuild needs must fit in context at once, or the work is partitioned and pays coordination overhead. With `B` the context budget (default 25,000 tokens, the knee in the evidence):

```
rebuild_tokens = essential_volume
               + spec_tokens
               + exported_symbols * tokens_per_export            (default 40)
               + untested_exports * tokens_per_untested_export * (1 - coverage_pct / 100)   (default 800)
               + (globals + init_funcs) * tokens_per_hidden_state (default 400)

r            = rebuild_tokens / B
agent_passes = r                       when r <= 1
             = r ^ superlinear_exponent when r > 1     (default 1.3)
```

`agent_passes` is reported to one decimal. Below 1.0 the package is rebuildable in one pass with room to spare. The coverage factor applies only when `coverage_pct` is non-null (`--coverage`); without it the term is the full step penalty of `tokens_per_untested_export` per untested export, as in the worked example. The unspecified driver's detail then records `coverage_pct`, and its suggestion says what coverage scaled the term to. `human_days` (7.3) does not read coverage. In `check`, the baseline has no coverage, so the summary's baseline `agent_passes` is estimated with head's `coverage_pct`.

Worked example (the reference test reproduces every value to four decimals): `sloc` 1200, `tokens_est` 10000, `tokens_est_with_tests` 16000, `duplication_pct` 20, `exported_symbols` 30, `untested_exports` 15, `globals` 2, `init_funcs` 1, default parameters.

```
volume      = 10000 * 0.8          =  8000
spec        = 16000 - 10000        =  6000
contract    = 30 * 40              =  1200
unspecified = 15 * 800             = 12000
hidden      = (2 + 1) * 400        =  1200
rebuild_tokens                     = 28400
r           = 28400 / 25000        = 1.136
agent_passes = 1.136 ^ 1.3         = 1.1803   (reported as 1.2)
```

Note that `duplication_pct` also lowers `human_days` through 7.3; "halves the volume term and nothing else" in the tests refers to the five token terms.

### 7.3 Human estimate

A rough figure using published COCOMO basic organic-mode coefficients, the same model `scc` uses, with unspecified behavior inflating the effective size:

```
kloc_eff   = (sloc * (1 - duplication_pct / 100) / 1000) * (1 + 0.5 * untested_ratio)
person_months = 2.4 * kloc_eff ^ 1.05
human_days = person_months * days_per_month   (default 19)
```

where `untested_ratio = untested_exports / max(exported_symbols, 1)`, clamped to [0, 1]. Labelled as an estimate in every output. For the worked example above: `untested_ratio` 0.5, `kloc_eff` = 0.96 × 1.25 = 1.2, `person_months` = 2.4 × 1.2^1.05 = 2.9064, `human_days` = 55.2211.

### 7.4 Tiers, drivers and suggestions

| Tier | `agent_passes` | Meaning |
| --- | --- | --- |
| ONE_PASS | <= 1.0 | Rebuildable by one agent in one context window. This is the bar. |
| FEW_PASSES | 1.0 to 3.0 | Rebuildable with partitioning; plan the split |
| PARTITION | > 3.0 | Not rebuildable as a unit; split before any large change |

`drivers` lists the two largest non-zero terms of `rebuild_tokens` (volume, spec, contract, unspecified, hidden). `suggestions` are generated from drivers whose term is at least 10% of `rebuild_tokens`, with the metric values, for example "7 exported functions have no test (Parse, Encode, Decode, Flush, Close and 2 more); a rebuild would have to reverse-engineer their behavior". At most five names are listed. Gate violations and warnings always carry a suggestion from the same per-metric templates.

All parameters live in the `rebuild:` section of the config. Until section 11.2 has run, the text report labels the estimate line `(estimate from uncalibrated parameters)` and the JSON report carries `rebuild.calibrated: false`; the label describes the estimate only, never the gate thresholds, which section 11.1 calibrates independently.

### 7.5 Acceptance invariants

- Go stdlib `errors` is ONE_PASS; `net/http` is PARTITION.
- A package with zero fan-in, tests present, no duplication and under 5k tokens is ONE_PASS.
- Monotonicity: increasing `tokens_est`, `exported_symbols`, `untested_exports`, `globals` or `init_funcs` never lowers `agent_passes`; increasing `duplication_pct` alone never raises it; raising `coverage_pct`, or measuring it where it was null, never raises it; adding test tokens raises only the `spec` term (on the worked example, 1,000 more test tokens move `agent_passes` from 1.1803 to 1.2346), and adding tests never raises `human_days`.

## 8. Quality gate

### 8.1 Semantics

`astimate check` evaluates every changed package (or all packages with `--all`) against a thresholds config. Thresholds fall into two kinds, and the distinction is what separates "got worse" from "got more features".

**Density rules** measure how the code is written, independent of how much there is. Adding features should never raise them, so they are gated on the change itself with `max_delta`: the largest permitted increase from baseline to head, usually 0. A feature written without copy-paste adds no duplicate blocks; ten new exports with tests leave `untested_exports` unchanged. Negative values require improvement. A density rule may also carry a `max`, but it is a ceiling on what a change may introduce, not a retroactive judgment: it is evaluated for a package with no baseline, and for a package whose value rose from baseline to head. An unchanged or improved legacy value above the `max` passes, so a package that was already at 80% duplication before a change is not failed for that history; a change that pushes it higher is. A density rule may carry `max` alone when its metric is already a change against the baseline (`changed_func_cognitive_max`); it is evaluated whenever the metric is non-null at head. `ratchet_from_zero` requires `max_delta`.

**Capacity rules** measure how much code there is. They are supposed to grow with features, so they carry no delta. They have an absolute `max` that answers a different question: has the package outgrown what one agent can hold in context? The fix for a capacity breach is a split, not a smaller feature. Like a density `max`, the ceiling is a violation for a new package or when the value rose; a legacy package already over the ceiling whose value is unchanged or fell gets a warning saying so, not a violation, so identical head and baseline trees never fail the gate. Each capacity rule also has a `warn_at` fraction (default 0.75) above which `check` emits a non-failing warning naming the headroom, so a split can be planned before a hard failure lands mid-feature.

**Module row.** A module-level row with package path `<module>` carries module-wide metrics, today `dup_blocks_cross_pkg` as the number of distinct cross-package blocks. It exists because one edit that copies code between packages changes two packages' counts. It is baselined under the id `<module>`, which no Go import path or TypeScript package directory can equal, by `baseline write` and by git baselines alike. A rule whose metric is module-wide (today only `dup_blocks_cross_pkg`) is evaluated on the module row only: package rows still report their own count but are not gated on it, so one cross-package copy is one finding, not one per package it touches. Every other rule is evaluated on package rows only, so the module row, whose v0 metrics are zero, is never judged by a package rule. A baseline file without the row, one written before it existed, skips the module-wide rules for that run with one note on stderr (`baseline file has no module row; module-wide rules skipped; run \`astimate baseline write\` to add it`) and still reports the row's metrics; `baseline write` always writes the row, and a git baseline always has it because it is extracted. A baseline file written before the id was reserved stores the row under `module`; it is read as the module row when the file has no `<module>` key and every v0 field of that row is zero (otherwise it is the package of that import path), with one warning on stderr (`baseline file stores the module row under the old key "module"; run \`astimate baseline write\` to rewrite it`). `check` reports it first whenever at least one package is selected, including a check of named packages: the MCP `check_package` tool gates it too, but for a check of named packages the module-wide rule judges only the cross-package blocks blamed on them: with a git baseline, whose merge-base blocks are recorded, the new head blocks (those whose set of packages occurs more often at head than at the baseline) that touch a named package, counted as an increase over the baseline's value; with a baseline file, which stores only the count, the head count when any head block touches a named package and the baseline's count otherwise. A copy between two other packages therefore neither fails the check nor appears in its findings, while the row still reports the full count; the finding's suggestion names the packages sharing each blamed block. `check` from the CLI names no packages and gates the full count. The default gates it with `max_delta: 0` and `max: 450` (8.2), without `ratchet_from_zero`, so a module with no baseline is held to the max rather than counted from zero.

Boolean metrics use `require: true` with an optional `when` guard (for example `has_tests` when `sloc > 100`). Requirements, like density `max`, do not fire on an unchanged legacy package: `has_tests` fails a package that lacks tests only when it is new or when its `sloc` grew.

Packages that are new at head have no baseline. They face the capacity ceilings and the absolute `max` of every density rule. Delta rules are evaluated against zero only for rules marked `ratchet_from_zero: true`, which are the count-of-things-added metrics (`dup_blocks`, `untested_exports`, `globals`, `init_funcs`): a new package with three untested exports fails, a new package with forty tested exports under the ceiling passes. Intensive metrics such as `max_nesting`, `cognitive_p90` and `duplication_pct` are not ratcheted from zero, since every real package has some nesting; for a new package only their `max` applies.

The rebuild estimate is not gated directly. It mixes size and density terms, so a large well-written feature raises it; it stays a ranking and summary signal. The capacity ceilings below bound two of its five terms and do not ensure one pass: `tokens_est` caps the volume term and `exported_symbols` the contract term, so at the default ceilings (8.2) those two come to at most 16,000 + 60 × 40 = 18,400 tokens, leaving 6,600 of the 25,000-token context budget for the spec, unspecified and hidden terms, which no ceiling bounds. Nor is a ceiling breach a failed pass: duplication shrinks the volume term, so this repository's `internal/metrics`, at `tokens_est` 16,694 with 42.4% of it duplicated, scored 0.9 agent passes (ONE_PASS) on 2026-09-28 while over the `tokens_est` ceiling.

Any violation fails the gate with exit code 3. Warnings never change the exit code. Violations and warnings are reported one per line with metric, baseline value, head value, limit and a fix suggestion.

### 8.2 Default thresholds

Calibrated on 2026-09-28 (`config_version: thresholds-2026-09-28`) from the reference corpus of section 11: 2,347 packages from the standard library and 36 cloned modules in `calibration/corpus.yaml`, data in `calibration/data/2026-09-28-corpus/`, report in `calibration/reports/thresholds-2026-09-28.md`. Every limit but `changed_func_cognitive_max` is unchanged from `thresholds-2026-09-27`, the same fit of the same rows before they carried per-function counts. `dup_blocks_cross_pkg` was added the same day, fitted from the module rows in `calibration/data/2026-09-28-modules/` (`calibration/notes/cross-package-duplication-2026-09-28.md`). The uncalibrated placeholders are kept in `configs/uncalibrated.yaml`. Limits are rounded to the nearest readable step per 11.1, which makes `cognitive_p90` (p90 22) and `sloc` (p90 1,224) stricter than a plain 90th percentile: about 12% of corpus packages would fail each as a new package.

TypeScript packages are judged by the `languages.typescript` override the default carries (section 9), reported as `thresholds-2026-09-28+typescript`, fitted by the section 11.1 rules from a TypeScript corpus: 1,209 packages from 20 repositories in `calibration/corpus-typescript.yaml`, data in `calibration/data/2026-09-28-typescript/`, report in `calibration/reports/thresholds-2026-09-28-typescript.md`, criteria in `calibration/notes/typescript-corpus-2026-09-28.md`. It replaces `duplication_pct` (+7, max 45), `max_nesting` (+0, max 7), `cognitive_p90` (+4, max 25), `changed_func_cognitive_max` (max 55), `tokens_est` (35,000), `largest_file_sloc` (900), `exported_symbols` (55), `internal_imports` (8) and `sloc` (2,500). The zero-tolerance ratchets, `has_tests` and `dup_blocks_cross_pkg` (null for TypeScript, so the gate skips it) are the top level's, and no rebuild parameter is overridden.

Density rules (ratchet on the change):

| Metric | `max_delta` | `max` | Rationale |
| --- | --- | --- | --- |
| `dup_blocks` | +0 | none | No new duplicate blocks; copy-paste is the primary target. `ratchet_from_zero` |
| `duplication_pct` | +6 | 40 | A quarter of the corpus IQR; guards against a large duplicated feature |
| `untested_exports` | +0 | none | New exported behavior needs a test; legacy gaps are reported, not failed. `ratchet_from_zero` |
| `globals` | +0 | none | No new package state. `ratchet_from_zero` |
| `init_funcs` | +0 | none | `ratchet_from_zero` |
| `max_nesting` | +0 | 5 | Never deeper than today |
| `cognitive_p90` | +3 | 20 | Small drift allowed since p90 moves with function count; the `max` is what a new package is judged by |
| `changed_func_cognitive_max` | none | 50 (p99 of per-function cognitive complexity, 51, over the 80,280 corpus functions counted as new; p90 is 11) | One added or modified function past the corpus's own worst percentile is split-worthy even when the package p90 is low; fitted at p99 rather than p90 because the rule fires on a single function, and 1.0% of corpus functions exceed it; the metric is itself a diff, so it has no delta |
| `dup_blocks_cross_pkg` | +0 | 450 (p90 of the 36 cloned modules' module rows, 443) | No new code copied between packages; evaluated on the module row only (8.1). The max judges a module with no baseline, which is not counted from zero: 29 of the 36 corpus modules have a cross-package block |

Capacity rules (absolute ceiling with a warning band):

| Metric | `max` | `warn_at` | Rationale |
| --- | --- | --- | --- |
| `tokens_est` | 16,000 | 0.75 | The rebuild bar: bounds the volume term of the estimate; breach means split; breach means split |
| `largest_file_sloc` | 600 | 0.75 | Files past this rarely fit an edit in one view |
| `exported_symbols` | 60 | 0.75 | Surface past this is a package boundary problem |
| `internal_imports` | 10 | 0.75 | Fitted from cloned-module rows only (11.1) |
| `sloc` | 1,000 | 0.75 | |

Requirements:

| Metric | Rule |
| --- | --- |
| `has_tests` | `require: true` when `sloc > 100` |

A single very complex new function in a package whose 90th percentile stays low is caught by `changed_func_cognitive_max` (section 6), gated as a density rule with `max` alone.

### 8.3 Baselines

Two sources, chosen by flag:

- `--base <ref>` (default): the merge-base of `HEAD` and `<ref>` (default `origin/master`, then `master`, `origin/main`, `main`). The base tree is checked out into a temporary `git worktree`, analyzed, and removed. Packages are matched by import path.
- `--staged` changes the head, not the baseline: the head tree is the git index rather than the working tree. `git checkout-index -a` copies it (the index `GIT_INDEX_FILE` names, when a hook sets it) into a temporary directory, which is analyzed and removed. `--staged` requires a git repository.
- `--baseline <file>`: a committed `.astimate/baseline.json` written by `astimate baseline write`. For repositories that prefer explicit, reviewable baselines or that run outside git. The file records the tokenizer used to write it; `check` warns when its own tokenizer differs, since token counts from different tokenizers are not comparable. A baseline file also records each package's functions (receiver, name, a 16-hex-digit fingerprint and cognitive complexity) under a top-level `functions` key, so a check against it can compute `changed_func_cognitive_max`; a file without the key still loads, leaves that metric null and says so once on stderr. Git baselines carry the same records in memory.

### 8.4 Changed-package detection

`git diff --name-only <merge-base>` (the working tree against the merge-base, so committed, staged and unstaged changes all count) plus untracked non-ignored files, mapped to packages by directory. Rename detection is disabled so a moved file shows both its old and new directories. Files map to packages by the extractor's file rules (`metrics.SourceClassifier`): for Go, a `.go` file selects its directory and paths under `testdata` are ignored; for TypeScript, a source, test or declaration file selects the package that 13.1 assigns it to, the root `package.json` or any `tsconfig*.json` selects every package, and `node_modules`, `dist`, `build` and dot directories are ignored. Paths inside a nested module (a directory below the root holding the extractor's module marker, `go.mod` or `package.json`) are ignored, and the set is intersected with the packages the extractor lists, which for Go drops `_`-prefixed, `.`-prefixed and `vendor` directories. A directory left with no file that makes it a package is reported as deleted in a summary line, never as a violation. An extractor without file rules gets every package checked, with a warning. A changed TypeScript declaration file (`.d.ts`, `.d.mts`, `.d.cts`) can change where other packages' imports resolve, so it also selects the packages that import its package from non-test files in the head import graph (the edges `fan_in` counts, through the optional `ImporterLister`), and a log line names the package each was selected for; the changed-file listing is `git diff --name-status`, so a deleted declaration file is told apart from an added one, and a git baseline records the merge-base reverse import graph when it is extracted; the importers of a removed declaration file's package are taken from that graph, and those of a changed one's from the union of the head and merge-base graphs, so the importer whose import turned external (its `external_imports` rose) is selected, with a log line saying it is an importer at baseline. A baseline file records no import graph, so against one a removed declaration file's former importers are not selected and one info line says so. Go selects no importers: an import path names its package whatever files it holds, so an importer's `internal_imports` changes only through its own import declarations, whose change selects it, and a removed exported symbol breaks the importer's build rather than moving its gated metrics. A change to a package's test files alone still selects it. `--all` overrides. With `--baseline <file>`, the merge-base is taken against the ref the file records; if it records none or the ref does not resolve, every package is checked and a warning says so. With `--staged`, changed files are `git diff --name-status --cached <merge-base>`: staged changes only, with untracked files excluded, mapped by the same file rules. A directory counts as deleted when the index copy has no file that makes it a package. `--all --staged` checks every package of the index copy.

### 8.5 Output formats

- `text` (default): violations, then warnings, then a one-line summary per package: `<pkg>: N violations, M warnings`, with `, new since baseline` for a package the baseline lacks. When the rebuild estimate is calibrated (`rebuild.calibrated`, 10.2), the line leads the counts with `<agent_passes> passes (<tier>)` and ends with `, ±X passes from baseline`; while it is uncalibrated, gate output leaves the estimate out, since its invariants (7.5) can contradict the gate. The `hook` reason and `github` annotations carry findings only, never passes or tier.
- `json`: the section 10.2 report per package plus `violations` and `warnings` arrays and a `passed` bool.
- `hook`: the JSON shape Claude Code Stop hooks consume: `{ "decision": "block", "reason": "<violations as text>" }` on failure, `{}` on success, so the agent is told to keep working and why. Warnings are appended to the reason on failure and written to stderr on success. Because Claude Code reads a hook's JSON only when it exits 0, `--format hook` exits 0 whenever it produced a decision, whether or not there were violations; analysis failure still exits 2, which a Stop hook treats as a blocking error with stderr shown to the agent. In hook format, `check` reads the Stop hook's JSON from stdin when present and emits `{}` without analysis when `stop_hook_active` is true, so a hook that already blocked once never blocks again.
- `github`: `::error` annotations, one per violation, and `::warning` per warning, each on the file and line that caused it where the extractor can say: a duplicate block's occurrence for `dup_blocks` and `duplication_pct`, the untested export's declaration for `untested_exports`, the global's declaration for `globals`, the largest file for `sloc`, `largest_file_sloc`, `tokens_est` and `tokens_est_with_tests`, the function for `changed_func_cognitive_max`, else the package's `doc.go` or first file; among several candidates, the first in a file holding a changed function. A finding with no location is annotated `file=<pkgdir>`. Paths are relative to the repository top level (the module root's directory in the git repository is prefixed; outside git, paths are module-relative). The module row's findings lead with `module: ` (the row's id, `<module>`, is used in every other format); a `dup_blocks_cross_pkg` finding is annotated on the first occurrence of the first cross-package block when the extractor names them, and otherwise carries no `file` property. When the baseline's tokenizer differs from the check's, one `::warning title=astimate::` line says token counts are not comparable; the hook format adds the same text as a `warning:` line.

## 9. CLI

```
astimate check    [<module-root>] [--base ref | --baseline file] [--all] [--staged] [--config|--thresholds file] [--format text|json|hook|github] [--tokenizer=est|o200k] [--coverage] [--coverage-timeout 2m]
astimate baseline write [<module-root>] [--out .astimate/baseline.json] [--tokenizer=est|o200k]
astimate assess   <package-dir> [--json] [--config astimate.yaml] [--tokenizer=est|o200k] [--coverage] [--coverage-timeout 2m]
astimate rank     [<module-root>] [--json] [--top N] [--sort passes|days|fan_in|tokens|duplication] [--config astimate.yaml] [--tokenizer=est|o200k] [--coverage] [--coverage-timeout 2m]
astimate serve    [--config astimate.yaml] [--allow-any-path]
astimate config init [--out astimate.yaml]     # rebuild parameters and thresholds in one file with comments
astimate version
```

Exit codes: 0 success or gate passed, 1 usage error, 2 analysis failure, 3 gate failed (except `--format hook`, which exits 0 with a decision; see 8.5). When a violation and an analysis failure both occur, 3 wins because a verdict was reached. Logs go to stderr only. `rank` defaults `<module-root>` to the current directory and ranks the whole module containing it; `--top 0` (the default) prints every row; a package whose extraction fails is logged and omitted, and the command exits 2 after printing the rest.

Config resolution: `--config <path>`, then `./astimate.yaml`, then the embedded default. Top-level keys (`config_version`, `chars_per_token`, `rebuild`, `thresholds`) are required; missing ones are errors rather than being filled from the default, and `config init` writes a complete file to start from. Sections that group optional tuning (`duplication`, with `min_tokens`, `ignore_literal_only`, `fold_signs`, `split_literal_runs`) may be partial or absent; absent keys take the embedded defaults. The pre-1.0 top-level `dup_*` keys are accepted with a deprecation warning for one release and rejected when the section is also present. An optional `languages:` section, keyed by language id (the ids the registered extractors report, today `go` and `typescript`), overrides the configuration for one language. Its `rebuild:` sets only the parameters it names, the rest coming from the top-level `rebuild`. Its `thresholds:` rules replace the top-level rules on the same metric, in the first one's place, or add a rule; a rule of the form `- metric: <name>` with `disabled: true` drops the top-level rules on that metric for the language, and `disabled` is an error at the top level. `chars_per_token`, `duplication` and every other setting are shared. Each override is validated on its own, after merging, and its errors name the language; the config parser accepts any id, and the engine warns once for an id no registered extractor reports. A report judged with an override shows `config_version` suffixed with `+<language>` (for example `thresholds-2026-09-28+typescript`). `config init` writes the embedded default's `typescript` override (section 8.2) and a commented example of the other keys.

## 10. MCP server

Built on `github.com/modelcontextprotocol/go-sdk` v1.8 or later, which supports the 2026-07-28 protocol revision and the legacy initialize handshake.

### 10.1 Tools

| Tool | Input | Output |
| --- | --- | --- |
| `check_package` | `{ "path": string, "base"?: string, "baseline_file"?: string, "staged"?: bool, "coverage"?: bool }` | Gate result: the package's report (10.2) with `passed` false when the package or the module row has a violation, plus a `module` block holding the module row's report (8.1) with its own `violations`, `warnings` and `passed`, absent when the extractor has no module row. The agent's self-check; the text opens with PASSED or FAILED and what to do next, and lists the module row's findings under `<module>` as `check`'s text format does, with the summary line of 8.5 (no agent passes or tier while the estimate is uncalibrated; the structured report always carries `rebuild`). `staged: true` judges the git index instead of the working tree, as `check --staged` does (8.3), reading the repository's own index; outside a git repository it is an `isError` result. Its module block fails only on cross-package copies that touch the checked package; the suggestion names the packages sharing each such copy. On every tool, `coverage: true` measures `coverage_pct` as `--coverage` does (section 6), with the default timeout. |
| `assess_package` | `{ "path": string, "tokenizer"?: string, "coverage"?: bool }` | One report (10.2) |
| `rank_packages` | `{ "module_root": string, "top"?: int, "sort"?: string, "coverage"?: bool }` | Sorted array of `{ path, agent_passes, human_days, tier, fan_in, tokens_est, duplication_pct }` |
| `explain_metric` | `{ "metric": string }` | Definition, evidence note and default threshold for one metric |

Results return `content` (text) and `structuredContent` (JSON), with `isError: true` on analysis failure. A gate failure is not an error; it is a result with `passed: false`.

### 10.2 Report schema

```json
{
  "language": "go",
  "package_path": "internal/billing",
  "module_path": "github.com/acme/app",
  "rebuild": {
    "agent_passes": 2.4,
    "rebuild_tokens": 61000,
    "human_days": 9.5,
    "tier": "FEW_PASSES",
    "calibrated": false,
    "drivers": [ { "term": "unspecified", "tokens": 5600, "detail": "untested_exports=7" } ]
  },
  "suggestions": [ "7 exported functions have no test; a rebuild would have to reverse-engineer their behavior." ],
  "metrics": { "...": "every field from section 6" },
  "baseline": { "ref": "a1b2c3d", "tokenizer": "est", "tokens_comparable": true, "metrics": { "...": "same fields" } },
  "violations": [
    { "metric": "dup_blocks", "base": 1, "head": 4, "limit": "max_delta +0", "suggestion": "..." }
  ],
  "warnings": [
    { "metric": "tokens_est", "head": 24100, "limit": "max 30000", "suggestion": "at 80% of the ceiling; plan a split before the next feature" }
  ],
  "passed": false,
  "astimate_version": "0.3.0",
  "config_version": "thresholds-2026-09-28"
}
```

`check --format json` lists the module row first as an ordinary report whose `package_path` is `<module>` (written without HTML escaping): v0 metrics are 0 and v1 metrics are null except the module-wide ones, and its rebuild block is zero. `metrics.changed_func_cognitive_max` is filled only by `check` (null in `assess`), `baseline.metrics` never carries it, and its violation shows no base value; the text format renders it `head (changed since baseline)`.
`config_version` carries a `+<language>` suffix when a `languages:` override applied (section 9). `baseline.tokenizer` is the tokenizer the baseline counted tokens with, and `baseline.tokens_comparable` is false when it differs from the check's, in which case token deltas against the baseline are not meaningful; the gate still runs.

Each finding in `violations` and `warnings` may carry `"location": {"file": string, "line": int}`, `file` relative to the module root and `line` 0 when only the file is known; it is omitted when the finding has no location. A report may carry an optional top-level `"details"` object, omitted when the extractor records none or the row has nothing to show, with every inner key also omitted when empty: `duplicates` (`[{file, start_line, end_line}]`, one entry per occurrence of each duplicate block), `untested_exports` (`[{name, file?, line?}]`), `excluded_untested` (`[string]`), `globals` (`[{name?, file?, line?}]`, `name` being the variable's name when the extractor records it), `largest_file` (string) and `cross_blocks` (`[{occurrences: [{package, file, start_line, end_line}]}]`). Files inside `details` are relative to the package directory, as the extractor's `Details` gives them, except `cross_blocks` occurrence files, which are relative to the module root; `package` is the module-relative directory in the same form as `package_path`. On the module row, `details.cross_blocks` lists every cross-package block in the module. Text and table outputs do not render details.

`package_path` is the package directory relative to the module root (`.` for the root package); the full import path is `module_path` joined with it. `agent_passes` and `human_days` are rounded to one decimal; `rebuild_tokens` and driver `tokens` are integers. `rebuild.calibrated` is true only when `config_version` starts with `rebuild-`, the prefix section 11.2 assigns to measured rebuild parameters; a `thresholds-<date>` version has calibrated thresholds and still reports `false`, because the flag describes the estimate's parameters, not the gate. `passed`, `baseline`, `violations` and `warnings` are present whenever a gate ran, with `violations` and `warnings` as empty arrays rather than omitted; all four are absent from `assess` output.

## 11. Calibration

Two calibrations, in priority order.

### 11.1 Thresholds from a reference corpus (primary)

1. Assemble a corpus of 20 or more well-regarded Go modules: the standard library plus widely used, actively maintained open-source modules with permissive licenses.
2. Run the collector under `calibration/collect` over each (it calls the engine directly and writes every package's raw metrics, which `rank --json` does not carry) and pool all packages.
3. Set each capacity rule's `max`, and each density rule's `max` where the rule has one, at the pooled 90th percentile (nearest rank), rounded to two significant figures and then to the nearest readable step: 500 above 1000, 50 above 100, 5 above 10, otherwise 1 (0.5 for a percentage); a capacity `max` is at least one step. Set each density rule's `max_delta` at a quarter of the interquartile range (p75 minus p25), rounded up to a whole step, at least 1 for a count and 0.5 for a percentage, except that a rule whose default `max_delta` is 0 keeps it: zero tolerance on new duplicate blocks, untested exports, globals, init functions and nesting is a policy, not a statistic. `internal_imports` is pooled from cloned-module rows only, since the standard library is loaded as one module. `changed_func_cognitive_max` is a diff against a baseline, so it is fitted per function: every function of every row (the collector's `func_cognitive` counts) is counted as new, and its `max` is the pooled 99th percentile, rounded the same way. A module-wide metric (`dup_blocks_cross_pkg`) is pooled from the module rows only, one per cloned module, which the collector writes to `modules.jsonl`; the per-package counts on package rows are not used. Each other language is calibrated the same way from its own corpus (`calibration/corpus-<language>.yaml`), and `calibration/fit --language <id>` writes the result as that language's `languages.<id>` override, with one rule per rule the data fitted a statistic for. The rebuild parameters are not overridden. Kinds, `warn_at`, `ratchet_from_zero`, `when` guards, requirements and rebuild parameters are unchanged by the fit. `calibration/fit` applies these rules and records the distribution in `calibration/reports/`.
4. Ship as `config_version: thresholds-<date>`. Good code passes by construction; the gate flags what falls outside what good projects do.
5. Re-run yearly or when the metric set changes.

### 11.2 Rebuild parameters from rebuild experiments (secondary)

The estimate's parameters are measured by doing the thing it estimates. For each of 30 or more packages across the reference corpus:

1. Delete the non-test implementation, keeping test files and exported signatures as stubs.
   The experiments are defined in `calibration/rebuild/rebuild.yaml`, generated by `go run ./calibration/rebuild select` and validated by its Go types. Each entry pins a cloned corpus module at its `corpus.yaml` commit and records the package's `RawMetrics` and pre-run estimate, a turn cap, and a hash of the stubbed tree. The stub strategy `signatures` replaces every function and method body in the package's non-test files, `init` included, with `panic("not implemented")`, keeping everything else and dropping imports that are no longer used. The oracle, run from the module root with `CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off`, is `go test` of the package when it has tests, else of its in-module importers that have tests, followed by `go build ./...`. Selection: cloned-module packages without cgo or generated files, with at most 10 agent passes, split into six strata by tier and `has_tests` and sorted by `rebuild_tokens`. Seven slots per stratum start at evenly spaced positions, and each takes the first candidate that verifies at its pin (oracle passes before stubbing; the stub is deterministic and builds; the oracle's tests fail on the stub's panic), with at most three packages per module. The standard library is excluded from the first selection.
2. Have the target agent (Claude Code first) reimplement until the package's tests pass, with a turn cap.
   `go run ./calibration/rebuild run` runs each experiment `--repeats` times (default 3), each run in a fresh clone at the pin with the stub applied and hash-checked. The agent is Claude Code in print mode (`claude -p`, stream-json output, `--max-turns` at the experiment's `turn_cap`, a fixed prompt, tools limited to reading and editing files and the go commands, a pinned `--model`), started with the module root as working directory and a wall-clock timeout; another agent can be given as a command template. The runner never starts Claude Code without `--live`.
3. Record tokens, turns, wall time, whether tests passed, and whether importers still compile.
   Each run appends one row to `runs.jsonl`: the experiment and run index, the agent command and model, the pre-run estimate (`agent_passes`, `rebuild_tokens`, `human_days`, tier), the `RawMetrics`, `config_version` and `go_version`, the measured input, output and cache tokens, cost, turns, tool calls, wall time, session id and whether the turn cap was hit (null when the agent does not report one), whether the oracle's tests passed and whether `go build ./...` still compiles, whether the agent changed test files or other packages, and timestamps. `run.json` records the environment and totals. A rerun skips the (package, run) pairs whose oracle completed.
4. Regress the outcomes on the section 7.1 inputs and fit `context_budget`, the per-item token costs and the superlinear exponent.
5. Ship as `config_version: rebuild-<date>-<agent>` and flip `calibrated` to true for the estimate.

This is well-defined and repeatable, unlike a refactoring-task corpus, and it directly measures what the number claims. It does not affect the gate.

## 12. Integrations

- **CI:** `astimate check --format github` in a workflow step; exit 3 fails the job. A composite GitHub Action under `action/` wraps install and invocation; its `path` input names a module below the repository root.
- **Claude Code Stop hook:** `astimate check --format hook` returns a block decision with the violations as the reason, so the agent continues and fixes them. Documented with a ready-to-paste `settings.json` snippet.
- **Pre-commit:** documented invocation with `--all` disabled and `--base` set.
- **MCP:** `check_package` for in-task self-checks.

## 13. Multi-language extension

The `Extractor` interface is the only language-specific surface. Adding a language means implementing it, defining module, package and internal import for that ecosystem, providing fixtures, and calibrating thresholds per language. Go keeps the native toolchain because `untested_exports` and coupling need type information. Other languages use tree-sitter.

### 13.1 TypeScript

TypeScript is parsed with `github.com/odvcencio/gotreesitter` (v0.55.1), a pure-Go tree-sitter runtime that bundles the TypeScript and TSX grammars. It needs no cgo, so `CGO_ENABLED=0` release builds keep working; the official cgo binding was not needed because the pure-Go runtime parses the fixture and the test snippets with no error nodes. The runtime is pre-1.0 and embeds all grammars by default; release builds pass the subset tags listed as `BUILD_TAGS` in the Makefile (one per grammar in use, checked against goreleaser and the action by a test) so only the two in use are embedded. The extractor lives in `internal/lang/typescript`; `internal/lang/duptok` is the duplicate finder over abstract token classes; the Go extractor uses it too, for both `dup_blocks` and `dup_blocks_cross_pkg`, with its `go/scanner` tokens mapped to the same classes.

Counting rules, the TypeScript counterpart of 6.5 (`testdata/ts/fixture/golden/COUNTING.md` shows each applied):

- **Module:** the directory of the nearest `package.json`; a directory below it with its own `package.json` is a separate module. There is no module path; package ids are directories relative to the root (`.` for the root).
- **Package:** a directory with at least one `.ts`, `.tsx`, `.mts` or `.cts` file that is neither a declaration file (`.d.ts`, `.d.mts`, `.d.cts`) nor a test file. `node_modules`, `dist`, `build` and dot-prefixed directories are skipped. Declaration files count nowhere. `.js` is not read.
- **Test files:** `*.test.*` or `*.spec.*` with extension `.ts`, `.tsx`, `.mts` or `.cts`, or anything under `__tests__/`, which belongs to the package containing that directory. A test file in a directory with no package is ignored. `test_funcs` counts calls to `it`, `test` or `bench` with a string or template first argument, at top level or inside `describe` bodies; member and curried forms such as `it.only` and `it.each(t)(...)` count once each.
- **Imports:** static `import` and `import type`, `export ... from`, `import x = require()`, and `require("lit")` or `import("lit")` calls. A relative specifier, a `paths` alias of the root `tsconfig.json` with its `extends` chain applied, or, with a `baseUrl`, a non-`node:` bare specifier resolves inside the module by the `tsc` rules (`moduleResolution` `bundler` where it and `node16` differ; exact alias match, then longest prefix, then the first target that resolves; JSONC accepted). A path resolves only to a file with a TypeScript source or declaration extension: for a `.ts`, `.d.ts` or `.js` specifier its `.ts`, `.tsx` or `.d.ts` form; for `.tsx` or `.jsx` its `.tsx`, `.ts` or `.d.ts` form; for `.mts`, `.d.mts` or `.mjs` its `.mts` or `.d.mts` form; for `.cts`, `.d.cts` or `.cjs` its `.cts` or `.d.cts` form; failing those, and for any other name, the name with `.ts`, `.tsx` or `.d.ts` appended (so `a.js` may resolve to `a.js.ts`). With `compilerOptions.resolveJsonModule` set (read through `extends` like `baseUrl`, the last file setting it winning), a specifier ending in `.json` also resolves to that file and counts by its directory like any other resolved file; JSON files add nothing to source metrics. With `compilerOptions.allowJs` set, or `checkJs` when `allowJs` is unset (each read through `extends` the same way), a lookup that finds no TypeScript file is retried for JavaScript, relative, alias and `baseUrl` lookups alike, after all TypeScript attempts as in tsc: a `.js` or `.jsx` specifier names that file or its `.jsx`/`.js` sibling, `.mjs` and `.cjs` the file itself, an extensionless one the name with `.js` or `.jsx` appended, and a directory its `package.json` `main` target, then `index.js` or `index.jsx`; the resolved file counts by its directory like any other, so a JavaScript-only directory counts nowhere, and JavaScript files add nothing to source metrics. Failing a file, a directory resolves through its `package.json` `typings`, `types` or `main` target, else its `index.ts`, `index.tsx` or `index.d.ts` (never an `.mts` or `.cts` index, as tsc resolves a directory as the extensionless name `index`); `.`, `..` and a trailing `/` are directories only. A relative specifier that resolves to nothing counts nowhere; a matched alias none of whose targets resolves, and a bare specifier that does not resolve under `baseUrl`, fall through to the built-in and npm rules. `node_modules` is not modelled, so a bare name that resolves under `baseUrl`, including under `allowJs` only to a local JavaScript file, is internal even where tsc would pick an installed typed package of that name (a deliberate difference). `extends` takes a string or an array of relative paths or bare names under `node_modules`; a bare package name uses its `package.json` `exports` (a string or a `"."` entry), then its `tsconfig` field, then its `tsconfig.json`; a leading `${configDir}` in `extends`, `baseUrl` or a `paths` target is the root configuration's directory (tsc 5.5); each of `baseUrl` and `paths` comes from the last file setting it, a child's `paths` replacing the parent's whole map, with relative values resolved against the declaring file; a missing file, a cycle or a chain deeper than 32 ends the chain. Such an import is internal only when the resolved file's directory is another package; an import of the same package is not counted, one landing in a non-package directory counts nowhere, and one leaving the module is external. Bare specifiers are external, counted by npm package name (`@scope/name` or the first segment). `stdlib_imports` counts Node built-ins by name without the `node:` prefix.
- **`exported_symbols`:** each name an `export` declaration adds (function, class, interface, type, enum, namespace, each bound `let`/`const`/`var` name), one per `export default`, and one per name in an `export {...}` list, including re-exports from other packages. Overload signatures and class members are not counted.
- **`untested_exports`:** exported functions, exported arrow or function constants, functions exported through a list, and public methods of exported classes; constructors, accessors, private, protected and `#` members are excluded. A candidate is untested when its name appears as no identifier in any test file of the package. This is name matching without type information. As in Go (6.4), a candidate whose doc comment (the comments directly above it, no blank line) has the line `//astimate:untested`, optionally followed by a space and a reason, is excluded and listed separately; a directive above a class does not apply to its methods. For an overloaded function or method, the directive applies when it is in the doc comment of any of its overload signatures or of the implementation, since TypeScript convention puts the doc comment on the first signature.
- **`globals`:** names bound by top-level `let` or `var` (not `const`), excluding `_`. **`init_funcs`:** the number of files with at least one top-level call statement, plain or awaited.
- **Functions:** top-level declarations, top-level variables initialised with a function or arrow function, and methods and function-valued fields of top-level classes; nested functions are scored inside their parent. The extractor lists these functions for `changed_func_cognitive_max` (6.5): the receiver is the class name for a method or function-valued field, empty otherwise; the fingerprint hashes the body's duplication tokens (identifiers ID, literals LIT, comments and zero-width tokens dropped) with token kinds hashed by name, semicolons left out so semicolon style does not mark a function changed, and the tokens of template substitutions included; a call to the function itself is kept distinct (a direct call by its bare name, or for a method `this.m(...)`, including from a nested arrow function but not from a nested function expression or class, which bind their own `this`, nor through `super` or another object); overload signatures have no body and are not listed.
- **Cognitive complexity** follows the gocognit rules: +1 plus nesting for `if`, `switch`, `for`, `for-in`, `for-of`, `while`, `do`, `catch` and the ternary; +1 flat for `else` and `else if`; +1 per run of like `&&`, `||` or `??` operators, with parentheses starting a new run; +1 for a labelled `break` or `continue`; nesting deepens inside nested functions; recursion is not counted. **`max_nesting`** counts `if`, the loops, `switch`, `try` and nested functions; an `else if` adds a level.
- **Duplication:** tokens are tree-sitter leaves with comments and zero-width tokens (automatic semicolons) dropped. Identifiers and `undefined` become ID; strings, templates, regexes, numbers, `true`, `false`, `null` and JSX text become one LIT each; a unary sign on a number is marked as a sign for the fold rule; type keywords such as `number` stay keywords. The literal-only, fold-sign and coverage rules are the Go ones (6.3).
- **v1 fields:** `instability`, `abstractness` (exported interfaces over exported classes, interfaces, type aliases and enums) and `main_sequence_distance` are computed, and `changed_func_cognitive_max` is computed by `check` from the function list above; the other v1 fields are null and the module row (8.1) is not produced; `generated_files` and `tokens_est_generated` are null because TypeScript has no generated-file header convention yet, so no file is left out of the size metrics. `--tokenizer o200k` is supported.
- Thresholds: the `typescript` override of section 8.2, fitted from the TypeScript corpus. The collector ranks each module root the corpus names and leaves out packages under test, fixture, example, benchmark and documentation directories.

Changed-package detection follows 8.4 with the TypeScript file rules stated there.

## 14. Milestones

Each bullet is intended to become one story.

### M0: Skeleton

- Module, layout, CI with `gofmt`, `go vet`, `golangci-lint`, `go test -race`.
- `RawMetrics`, `Extractor`, `ModuleContext`, registry.
- `version` and `config init`.
- Fixture module with packages exercising every v0 metric, including a duplication case and an untested-export case.

*Accepts when:* build and tests pass in CI; fixture loads with `go/packages`.

### M1: Go extractor (v0 metrics)

- Conformance suite `metricstest` with a fake extractor.
- Loader, import classification, fan-in, globals and init, nesting and cognitive complexity, size, tokens, tests.
- Duplication (6.3) and untested exports (6.4).
- Full assembly, passing the conformance suite against the fixture, plus the stdlib `errors` checks.

*Accepts when:* `metricstest.TestExtractor` passes for the Go extractor on the fixture; `errors` reports `globals=2`, `internal_imports=0`.

### M2: Estimate, thresholds and CLI

- Unified config (rebuild parameters and thresholds) loader and validation.
- Rebuild estimate, tiers, drivers, suggestions.
- Thresholds evaluation: density deltas, capacity ceilings with warning bands, requirements; violation and warning reporting.
- `assess`, `rank`, `baseline write`, `check` with git-ref and file baselines, changed-package detection and all four output formats.
- Section 7.5 invariants and monotonicity property test.

*Accepts when:* `check` on the fixture with a deliberately degraded package exits 3 and names the violated metrics; the same package unchanged exits 0.

### M3: Gate integrations

- GitHub Action.
- Claude Code Stop hook documentation and an end-to-end test of the hook JSON contract.
- README with install, commands and integration snippets, samples generated from the fixture.

*Accepts when:* the action runs on this repository's own CI and the hook contract test passes.

### M4: MCP server

- Scaffold on the official SDK with stdout discipline.
- `check_package`, `assess_package`, `rank_packages`, `explain_metric`.
- Integration tests for both protocol eras.

*Accepts when:* Claude Code lists all four tools and `check_package` returns a gate result on the fixture.

### M5: Threshold calibration

- Reference corpus definition and collection script.
- Percentile fitting and distribution report.
- Ship calibrated thresholds as the default.

*Accepts when:* the shipped thresholds pass the fixture and stdlib checks and the distribution report is committed.

### M6: v1 metrics and second language

- `instability`, `abstractness`, `main_sequence_distance` (reported only), `dup_blocks_cross_pkg` with a module-level gate row, `uses_cgo`, `uses_reflect`, `generated_files`, `coverage_pct`.
- TypeScript extractor behind tree-sitter with fixtures, passing the conformance suite.
- Per-language config overrides.

### M7: Rebuild parameter calibration

- Rebuild experiment definition over the reference corpus, agent runner, fitting, ship calibrated parameters.

### M8: Trustworthy gate for LLM changes

The gate is the product: it should block LLM-written changes that make a package harder to maintain and let the rest through, and the milestone makes that demonstrable.

- Make the tool consistent with its own message: the spec's evidence and estimate claims match the code, every bypass of the gate is visible, and this repository passes its own gate in CI.
- Measure the gate on labeled corpora of LLM-authored changes, each change labeled `block` or `allow`.
- Close the ways LLM code degrades that no metric sees, in the order the measurement ranks them.

*Accepts when:* all four hold:

- On the labeled corpora, the gate fails at least 80% of the changes labeled `block` and at most 10% of the changes labeled `allow`.
- Every gated metric has a section 4 row naming the failure mode it catches and evidence that supports the row.
- `astimate check . --all` on this repository reports no package over a capacity or density `max`, and CI fails on a violation.
- Every bypass is visible: a finding is silenced only by an exemption that carries a reason and appears in the report.

## 15. Open questions

- Should `check` also evaluate packages whose importers changed, since a change to `hub` can affect their metrics? Proposed: no in v0; `--all` covers it. Cross-package duplication is handled by a module-level row instead (section 6, `dup_blocks_cross_pkg`).
- Resolved: `duplication.ignore_literal_only` and `duplication.fold_signs` ship on by default (6.3). Resolved: straight-line call sequences that differ only in their arguments (the addition chains in `crypto/internal/fips140/nistec`, the unrolled loops in `math/big`) count as duplication. A rule dropping repeats made only of calls to one or two functions was measured over the standard library: the strict form drops two data-like blocks and reaches neither package; the loose form reaches `nistec` (33.3% to 32.9%) only by also dropping a pasted debug dump in `math/big`, which is the copy-paste the gate targets.
- Which reference modules go in the corpus? To be listed in S-034.
