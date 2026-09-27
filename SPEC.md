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
5. **Honest defaults:** thresholds and rebuild parameters are labelled uncalibrated until the calibration in section 11 has run.

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
| Package bloat past what the next agent can hold | `tokens_est`, `sloc`, `largest_file_sloc` | Successful agent trajectories stay under 20 to 30k tokens; resolve rates collapse past 64k (arXiv 2602.16069, 2505.07897). | Strong |
| Exporting everything | `exported_symbols` | No direct study. Wider API surface raises fan-in cost and the facts the next agent must hold (Coherence Debt, arXiv 2608.16630). | Indirect |
| Untested additions | `untested_exports`, `has_tests` | Agents self-correct through a run-and-check loop; a package without tests denies the next agent that loop. | Moderate, indirect |
| Hidden state | `globals`, `init_funcs` | No direct study. Plausible; kept at low weight. | Unproven |
| Coupling shape | `instability`, `abstractness`, `main_sequence_distance` | Martin's package metrics are widely reported but their validation as predictors is mixed and none exists for Go, whose consumer-defined interfaces invert the abstract-provider assumption. Reported only until the corpus measurement in 11.1 shows where good Go modules sit. | Unproven |
| Deep nesting | `max_nesting`, `cognitive_p90` | Classical complexity shows no consistent correlation with LLM performance once length is controlled (arXiv 2602.07882). Kept as a gate on regressions only. | Weak |
| Coupling growth | `internal_imports` | Failures come from coupled facts absent from context (arXiv 2608.16630; CrossCodeEval; RepoBench). | Moderate |
| Blast radius | `fan_in` | Strongest predictor of task difficulty (SWE-bench analyses, arXiv 2511.00197). Rarely changes within one PR, so it drives the rebuild estimate more than gating. | Strong for ranking |

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

## 6. Raw metrics

Every field is reported in output. *(v0)* fields are required for the first release.

| Field | Type | Definition | Release |
| --- | --- | --- | --- |
| `files` | int | Non-test source files | v0 |
| `sloc` | int | Non-blank, non-comment lines in non-test files | v0 |
| `largest_file_sloc` | int | SLOC of the largest non-test file | v0 |
| `tokens_est` | int | Estimated tokens of non-test source; see 6.1 | v0 |
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
| `dup_blocks_cross_pkg` | int | Duplicate blocks shared with other packages in the module: exact normalized repeats found over one module-wide stream of non-test, non-generated files, counted once in each package they touch; `dup_blocks` is computed per package and never includes them, and a sequence repeated within a package and also copied elsewhere counts in both. Null for standard-library loads, which are not modules. Also reported on a module-level row for gating (8.1) | v1 |
| `instability` | float | Martin instability `Ce / (Ca + Ce)` with `Ca = fan_in`, `Ce = internal_imports`; null when both are 0 | v1 |
| `abstractness` | float | Exported interface types over all exported types; null with no exported types | v1 |
| `main_sequence_distance` | float | `|abstractness + instability - 1|`; reported, not gated, since idiomatic Go leaf packages sit near 1 by design | v1 |
| `uses_cgo` | bool | Imports `"C"` | v1 |
| `uses_reflect` | bool | Imports `reflect` or `unsafe` | v1 |
| `generated_files` | int | Files with a `Code generated ... DO NOT EDIT` header | v1 |
| `coverage_pct` | float | Statement coverage from `go test -cover`, only with `--coverage` | v1 |
| `changed_func_cognitive_max` | int | Highest cognitive complexity among functions added or modified since baseline (matched per 6.5); 0 when none changed; null without a function-level baseline diff, as in `assess`, against a baseline file written without function records, or for an extractor that cannot list functions | v1 |

### 6.1 Token estimation

Default: `tokens_est = bytes / chars_per_token`, `chars_per_token` defaulting to **3.2**. Tokenizers differ, and the default targets the Anthropic tokenizer family, since Claude Code is the first gate consumer: published measurements put code at about 2.7 characters per token on the tokenizer introduced with Opus 4.7 and about 3.7 on the one before it, so 3.2 is a middle estimate for a Claude session. OpenAI's `o200k_base` is sparser: measured on this repository's Go source it runs about 3.75 to 4.0 bytes per token for non-test code and about 2.7 to 3.3 for test code. The original spec's 4.0 therefore happens to be close for o200k and undercounts for Claude by roughly 20 percent. The ratio is a config value so it can be set per target model.

Opt-in exact counting: `--tokenizer=o200k` via `pkoukk/tiktoken-go`, offline, exact for that tokenizer only. There is no offline Anthropic tokenizer; the Anthropic count-tokens API is not called by default because it is a network dependency.

### 6.2 Test-file handling

Test files are excluded from every metric except `tokens_est_with_tests`, `test_files`, `test_funcs` and the reference side of `untested_exports`. External test packages (`foo_test`) in the same directory fold into the package.

### 6.3 Duplication

Tokens are taken from `go/scanner` over non-test files. Identifiers, literals and comments are normalized (identifier to `ID`, string and numeric literals to `LIT`) so renamed copies still match. The automatic semicolons that `go/scanner` inserts at line ends are dropped from the stream, so a repeat never extends onto a neighbouring declaration's line. A duplicate block is a maximal token sequence of length at least `duplication.min_tokens` that occurs at least twice, found with a suffix array or rolling hash over the normalized stream. Nested and overlapping repeats are merged: a shorter repeat wholly inside a longer one's occurrences is not counted separately, and occurrences that overlap are unioned before coverage is computed. `dup_blocks` counts distinct maximal sequences after merging; `duplication_pct` is covered SLOC divided by package SLOC times 100, rounded to one decimal. Generated files are excluded. A block whose tokens are all literals or the punctuation `, { } : [ ] ( ) ;` is a data table, not code, and is dropped after merging when `duplication.ignore_literal_only` is true, which is the default: measured over 225 standard-library packages it moves the 90th percentile of `duplication_pct` by 0.1 and removes `html` (an entity map at 94%) and `crypto/des` from the top ten while every dropped block inspected was data (see `calibration/notes/duplication-literal-only.md`). With `duplication.fold_signs` true, which is the default, a `+` or `-` that directly precedes a numeric literal and does not follow an operand (an identifier, a literal, `)`, `]` or `}`) counts as part of the literal for that rule, so a table of negative numbers is data too; the sign stays a separate token for matching, so no match is gained or lost. Folding the sign into the normalized stream was rejected: it lets `f(-1)` match `f(1)` and raised `math` from 37.7% to 44.1% (see `calibration/notes/duplication-refinements.md`). The literal-only rule applies to whole blocks after merging; a literal table inside a block that also holds code is counted, including tables separated only by declaration headers, because trimming literal edges strips closing braces off code and splitting at short runs cuts code too (see `calibration/notes/duplication-literal-runs.md`). The threshold and normalization rules are config values.

### 6.4 Untested exports

An exported function or method counts as untested when no identifier in any test file of the package (internal or external test package) resolves, via `types.Info.Uses`, to that function or to a method with the same name on the same receiver type. Exported types, vars and consts are not counted; the metric targets behavior, not declarations. Reported as a count and, in the gate, primarily as a delta so new untested behavior fails while legacy gaps are only reported.

Two refinements. A declaration whose doc comment contains the line `//astimate:untested` (optionally followed by a reason) is excluded from the count and listed separately, for intentionally untested wrappers. A method called in a test through an interface value counts as referenced when the concrete receiver type, or a pointer to it, implements that interface and has a method of that name; the interface may come from any package. A generic receiver type is checked through each instantiation of it that appears in the package's test files; an instantiation that a test never names or holds is not seen. A selection inside a generic function declared in a test file is checked once per instantiation of that function in the test files, with its type arguments substituted into the receiver type. A method of a generic type declared in a test file is checked the same way, once per instantiation of that type in the test files, and a generic function or type instantiated inside another with that one's type parameters takes each of the enclosing one's instantiations, substituted, through chains of up to eight. A generic test type that appears only as a field type of another generic test type is not carried through; that is a known gap.

### 6.5 Counting rules

Rules that the section 6 table leaves implicit, fixed here so goldens and implementations agree (`testdata/go/fixture/golden/COUNTING.md` shows each applied):

- `globals` counts names, not specs or blocks: `var a, b = 1, 2` is 2; `_` is excluded.
- `func_count`, `cognitive_total` and `cognitive_p90` include `init()` functions.
- `cognitive_p90` is the nearest-rank 90th percentile over per-function values; a package with no functions reports 0.
- `uses_cgo`, `uses_reflect` and `generated_files` are read from the source files' import specs and headers, never from files cgo generates; blank and renamed imports of `reflect` or `unsafe` count.
- `changed_func_cognitive_max` diffs functions, never the package: a head function is matched to a baseline function of the same package by receiver base type (pointer, type parameters and parentheses stripped; empty for a plain function) and name, as a multiset so repeated names such as `init` pair up. A matched function is modified when its fingerprint differs. The fingerprint is a hash of the body's syntax tree with identifiers normalized to `ID` and literals to `LIT` as in 6.3; comments and positions are not in it, so comment, layout and gofmt edits, variable renames and literal values do not mark a function changed, while any change to operators or control flow does. A direct call to the function's own name is kept distinct, since cognitive complexity scores recursion. A renamed function or a method moved to another receiver is new; a deleted function is not changed; every function of a package new at head is changed. `init` functions count.
- `tokens_est` sums bytes across files first, then divides by `chars_per_token` and truncates.
- `fan_in_tests` counts other packages whose test files import this package; a package's own external test package importing it does not count.
- `sloc` counts a line with code and a trailing comment as code.
- For a cgo package, every syntactic metric is counted from the package's Go source files, never from the files cgo generates; `import "C"` is not an import of any class. The type-dependent `untested_exports` uses the type-checked trees, whose declarations and doc comments match the source.

## 7. Rebuild estimate

The estimate answers one question: if this package were deleted and rebuilt from its tests and exported contract, how much work is that? It is secondary to the gate and drives `rank`, `assess` and the summary line of `check`. It has units, so it can be measured (section 11.2) and argued with.

### 7.1 Inputs

A rebuild must reproduce a contract and pass a spec, and the raw metrics describe both:

| Input | From | Role |
| --- | --- | --- |
| Essential volume | `tokens_est * (1 - duplication_pct / 100)` | Code that must be written; duplicates collapse in a rebuild |
| Spec | `tokens_est_with_tests - tokens_est`, `test_funcs` | Tests are the executable specification the rebuild is checked against |
| Contract | `exported_symbols`, `fan_in` | Signatures that must survive; consumers that must keep working |
| Unspecified behavior | `untested_exports` | Behavior that must be reverse-engineered from the old implementation |
| Hidden contract | `globals`, `init_funcs` | State and ordering that no signature reveals |

### 7.2 Agent estimate

Everything a rebuild needs must fit in context at once, or the work is partitioned and pays coordination overhead. With `B` the context budget (default 25,000 tokens, the knee in the evidence):

```
rebuild_tokens = essential_volume
               + spec_tokens
               + exported_symbols * tokens_per_export            (default 40)
               + untested_exports * tokens_per_untested_export   (default 800)
               + (globals + init_funcs) * tokens_per_hidden_state (default 400)

r            = rebuild_tokens / B
agent_passes = r                       when r <= 1
             = r ^ superlinear_exponent when r > 1     (default 1.3)
```

`agent_passes` is reported to one decimal. Below 1.0 the package is rebuildable in one pass with room to spare.

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

All parameters live in the `rebuild:` section of the config and are labelled uncalibrated until section 11.2 has run.

### 7.5 Acceptance invariants

- Go stdlib `errors` is ONE_PASS; `net/http` is PARTITION.
- A package with zero fan-in, tests present, no duplication and under 5k tokens is ONE_PASS.
- Monotonicity: increasing `tokens_est`, `exported_symbols`, `untested_exports`, `globals` or `init_funcs` never lowers `agent_passes`; increasing `duplication_pct` alone never raises it; adding test tokens raises only the `spec` term (on the worked example, 1,000 more test tokens move `agent_passes` from 1.1803 to 1.2346), and adding tests never raises `human_days`.

## 8. Quality gate

### 8.1 Semantics

`astimate check` evaluates every changed package (or all packages with `--all`) against a thresholds config. Thresholds fall into two kinds, and the distinction is what separates "got worse" from "got more features".

**Density rules** measure how the code is written, independent of how much there is. Adding features should never raise them, so they are gated on the change itself with `max_delta`: the largest permitted increase from baseline to head, usually 0. A feature written without copy-paste adds no duplicate blocks; ten new exports with tests leave `untested_exports` unchanged. Negative values require improvement. A density rule may also carry a `max`, but it is a ceiling on what a change may introduce, not a retroactive judgment: it is evaluated for a package with no baseline, and for a package whose value rose from baseline to head. An unchanged or improved legacy value above the `max` passes, so a package that was already at 80% duplication before a change is not failed for that history; a change that pushes it higher is. A density rule may carry `max` alone when its metric is already a change against the baseline (`changed_func_cognitive_max`); it is evaluated whenever the metric is non-null at head. `ratchet_from_zero` requires `max_delta`.

**Capacity rules** measure how much code there is. They are supposed to grow with features, so they carry no delta. They have an absolute `max` that answers a different question: has the package outgrown what one agent can hold in context? The fix for a capacity breach is a split, not a smaller feature. Like a density `max`, the ceiling is a violation for a new package or when the value rose; a legacy package already over the ceiling whose value is unchanged or fell gets a warning saying so, not a violation, so identical head and baseline trees never fail the gate. Each capacity rule also has a `warn_at` fraction (default 0.75) above which `check` emits a non-failing warning naming the headroom, so a split can be planned before a hard failure lands mid-feature.

**Module row.** A module-level row with package path `module` carries module-wide metrics, today `dup_blocks_cross_pkg` as the number of distinct cross-package blocks. It exists because one edit that copies code between packages changes two packages' counts. It is baselined under the id `module` by `baseline write` and by git baselines alike, and evaluated by the same thresholds as a package; a baseline without the row treats it as new. `check` reports it first, and only when at least one package is selected; a check of named packages (the MCP `check_package` tool) has no module row. No default rule gates it until corpus calibration; a config may add `dup_blocks_cross_pkg` with `max_delta: 0` and `ratchet_from_zero: true`.

Boolean metrics use `require: true` with an optional `when` guard (for example `has_tests` when `sloc > 100`). Requirements, like density `max`, do not fire on an unchanged legacy package: `has_tests` fails a package that lacks tests only when it is new or when its `sloc` grew.

Packages that are new at head have no baseline. They face the capacity ceilings and the absolute `max` of every density rule. Delta rules are evaluated against zero only for rules marked `ratchet_from_zero: true`, which are the count-of-things-added metrics (`dup_blocks`, `untested_exports`, `globals`, `init_funcs`): a new package with three untested exports fails, a new package with forty tested exports under the ceiling passes. Intensive metrics such as `max_nesting`, `cognitive_p90` and `duplication_pct` are not ratcheted from zero, since every real package has some nesting; for a new package only their `max` applies.

The rebuild estimate is not gated directly. It mixes size and density terms, so a large well-written feature raises it; it stays a ranking and summary signal. The capacity ceilings below are its gate-side expression: they are set so that a package under every ceiling is rebuildable in one pass.

Any violation fails the gate with exit code 3. Warnings never change the exit code. Violations and warnings are reported one per line with metric, baseline value, head value, limit and a fix suggestion.

### 8.2 Default thresholds

Uncalibrated placeholders, replaced by 90th-percentile values from the reference corpus in section 11.

Density rules (ratchet on the change):

| Metric | `max_delta` | `max` | Rationale |
| --- | --- | --- | --- |
| `dup_blocks` | +0 | none | No new duplicate blocks; copy-paste is the primary target. `ratchet_from_zero` |
| `duplication_pct` | +0.5 | 5.0 | Guards against a large duplicated feature that adds one block |
| `untested_exports` | +0 | none | New exported behavior needs a test; legacy gaps are reported, not failed. `ratchet_from_zero` |
| `globals` | +0 | none | No new package state. `ratchet_from_zero` |
| `init_funcs` | +0 | none | `ratchet_from_zero` |
| `max_nesting` | +0 | 5 | Never deeper than today |
| `cognitive_p90` | +3 | 25 | Small drift allowed since p90 moves with function count; the `max` is what a new package is judged by |
| `changed_func_cognitive_max` | none | 30 | One added or modified function past this is split-worthy even when the package p90 is low; the metric is itself a diff, so it has no delta |

Capacity rules (absolute ceiling with a warning band):

| Metric | `max` | `warn_at` | Rationale |
| --- | --- | --- | --- |
| `tokens_est` | 30,000 | 0.75 | The rebuild bar: past this a rebuild no longer fits one agent pass; breach means split |
| `largest_file_sloc` | 800 | 0.75 | Files past this rarely fit an edit in one view |
| `exported_symbols` | 60 | 0.75 | Surface past this is a package boundary problem |
| `internal_imports` | 12 | 0.75 | |
| `sloc` | 6,000 | 0.75 | |

Requirements:

| Metric | Rule |
| --- | --- |
| `has_tests` | `require: true` when `sloc > 100` |

A single very complex new function in a package whose 90th percentile stays low is caught by `changed_func_cognitive_max` (section 6), gated as a density rule with `max` alone.

### 8.3 Baselines

Two sources, chosen by flag:

- `--base <ref>` (default): the merge-base of `HEAD` and `<ref>` (default `origin/master`, then `master`, `origin/main`, `main`). The base tree is checked out into a temporary `git worktree`, analyzed, and removed. Packages are matched by import path.
- `--baseline <file>`: a committed `.astimate/baseline.json` written by `astimate baseline write`. For repositories that prefer explicit, reviewable baselines or that run outside git. The file records the tokenizer used to write it; `check` warns when its own tokenizer differs, since token counts from different tokenizers are not comparable. A baseline file also records each package's functions (receiver, name, a 16-hex-digit fingerprint and cognitive complexity) under a top-level `functions` key, so a check against it can compute `changed_func_cognitive_max`; a file without the key still loads, leaves that metric null and says so once on stderr. Git baselines carry the same records in memory.

### 8.4 Changed-package detection

`git diff --name-only <merge-base>` (the working tree against the merge-base, so committed, staged and unstaged changes all count) plus untracked non-ignored files, mapped to packages by directory. Rename detection is disabled so a moved file shows both its old and new directories. Paths under `testdata` or inside a nested module are ignored, and the set is intersected with the packages the Go tool reports, which drops `_`-prefixed, `.`-prefixed and `vendor` directories. A directory left with no Go files is reported as deleted in a summary line, never as a violation. A change to a package's test files alone still selects it. `--all` overrides. With `--baseline <file>`, the merge-base is taken against the ref the file records; if it records none or the ref does not resolve, every package is checked and a warning says so.

### 8.5 Output formats

- `text` (default): violations, then warnings, then a one-line summary per package.
- `json`: the section 10.2 report per package plus `violations` and `warnings` arrays and a `passed` bool.
- `hook`: the JSON shape Claude Code Stop hooks consume: `{ "decision": "block", "reason": "<violations as text>" }` on failure, `{}` on success, so the agent is told to keep working and why. Warnings are appended to the reason on failure and written to stderr on success. Because Claude Code reads a hook's JSON only when it exits 0, `--format hook` exits 0 whenever it produced a decision, whether or not there were violations; analysis failure still exits 2, which a Stop hook treats as a blocking error with stderr shown to the agent. In hook format, `check` reads the Stop hook's JSON from stdin when present and emits `{}` without analysis when `stop_hook_active` is true, so a hook that already blocked once never blocks again.
- `github`: `::error file=<pkgdir>::` annotations, one per violation, and `::warning file=<pkgdir>::` per warning; the module row's findings carry no `file` property and lead with `module: `.

## 9. CLI

```
astimate check    [<module-root>] [--base ref | --baseline file] [--all] [--config|--thresholds file] [--format text|json|hook|github] [--tokenizer=est|o200k]
astimate baseline write [<module-root>] [--out .astimate/baseline.json] [--tokenizer=est|o200k]
astimate assess   <package-dir> [--json] [--config astimate.yaml] [--tokenizer=est|o200k] [--coverage]
astimate rank     [<module-root>] [--json] [--top N] [--sort passes|days|fan_in|tokens|duplication] [--config astimate.yaml] [--tokenizer=est|o200k]
astimate serve    [--config astimate.yaml] [--allow-any-path]
astimate config init [--out astimate.yaml]     # rebuild parameters and thresholds in one file with comments
astimate version
```

Exit codes: 0 success or gate passed, 1 usage error, 2 analysis failure, 3 gate failed (except `--format hook`, which exits 0 with a decision; see 8.5). When a violation and an analysis failure both occur, 3 wins because a verdict was reached. Logs go to stderr only. `rank` defaults `<module-root>` to the current directory and ranks the whole module containing it; `--top 0` (the default) prints every row; a package whose extraction fails is logged and omitted, and the command exits 2 after printing the rest.

Config resolution: `--config <path>`, then `./astimate.yaml`, then the embedded default. Top-level keys (`config_version`, `chars_per_token`, `rebuild`, `thresholds`) are required; missing ones are errors rather than being filled from the default, and `config init` writes a complete file to start from. Sections that group optional tuning (`duplication`, with `min_tokens`, `ignore_literal_only`, `fold_signs`) may be partial or absent; absent keys take the embedded defaults. The pre-1.0 top-level `dup_*` keys are accepted with a deprecation warning for one release and rejected when the section is also present.

## 10. MCP server

Built on `github.com/modelcontextprotocol/go-sdk` v1.8 or later, which supports the 2026-07-28 protocol revision and the legacy initialize handshake.

### 10.1 Tools

| Tool | Input | Output |
| --- | --- | --- |
| `check_package` | `{ "path": string, "base"?: string, "baseline_file"?: string }` | Gate result: `passed`, `violations[]`, per-package report. The agent's self-check; the text opens with PASSED or FAILED and what to do next. |
| `assess_package` | `{ "path": string, "tokenizer"?: string }` | One report (10.2) |
| `rank_packages` | `{ "module_root": string, "top"?: int, "sort"?: string }` | Sorted array of `{ path, agent_passes, human_days, tier, fan_in, tokens_est, duplication_pct }` |
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
  "baseline": { "ref": "a1b2c3d", "metrics": { "...": "same fields" } },
  "violations": [
    { "metric": "dup_blocks", "base": 1, "head": 4, "limit": "max_delta +0", "suggestion": "..." }
  ],
  "warnings": [
    { "metric": "tokens_est", "head": 24100, "limit": "max 30000", "suggestion": "at 80% of the ceiling; plan a split before the next feature" }
  ],
  "passed": false,
  "astimate_version": "0.3.0",
  "config_version": "default-uncalibrated-1"
}
```

`check --format json` lists the module row first as an ordinary report whose `package_path` is `module`: v0 metrics are 0 and v1 metrics are null except the module-wide ones, and its rebuild block is zero. `metrics.changed_func_cognitive_max` is filled only by `check` (null in `assess`), `baseline.metrics` never carries it, and its violation shows no base value; the text format renders it `head (changed since baseline)`.

`package_path` is the package directory relative to the module root (`.` for the root package); the full import path is `module_path` joined with it. `agent_passes` and `human_days` are rounded to one decimal; `rebuild_tokens` and driver `tokens` are integers. `passed`, `baseline`, `violations` and `warnings` are present whenever a gate ran, with `violations` and `warnings` as empty arrays rather than omitted; all four are absent from `assess` output.

## 11. Calibration

Two calibrations, in priority order.

### 11.1 Thresholds from a reference corpus (primary)

1. Assemble a corpus of 20 or more well-regarded Go modules: the standard library plus widely used, actively maintained open-source modules with permissive licenses.
2. Run the collector under `calibration/collect` over each (it calls the engine directly and writes every package's raw metrics, which `rank --json` does not carry) and pool all packages.
3. Set each capacity rule's `max`, and each density rule's `max` where the rule has one, at the pooled 90th percentile (nearest rank), rounded to two significant figures and then to the nearest readable step: 500 above 1000, 50 above 100, 5 above 10, otherwise 1 (0.5 for a percentage); a capacity `max` is at least one step. Set each density rule's `max_delta` at a quarter of the interquartile range (p75 minus p25), rounded up to a whole step, at least 1 for a count and 0.5 for a percentage, except that a rule whose default `max_delta` is 0 keeps it: zero tolerance on new duplicate blocks, untested exports, globals, init functions and nesting is a policy, not a statistic. `internal_imports` is pooled from cloned-module rows only, since the standard library is loaded as one module. Kinds, `warn_at`, `ratchet_from_zero`, `when` guards, requirements and rebuild parameters are unchanged by the fit. `calibration/fit` applies these rules and records the distribution in `calibration/reports/`.
4. Ship as `config_version: thresholds-<date>`. Good code passes by construction; the gate flags what falls outside what good projects do.
5. Re-run yearly or when the metric set changes.

### 11.2 Rebuild parameters from rebuild experiments (secondary)

The estimate's parameters are measured by doing the thing it estimates. For each of 30 or more packages across the reference corpus:

1. Delete the non-test implementation, keeping test files and exported signatures as stubs.
2. Have the target agent (Claude Code first) reimplement until the package's tests pass, with a turn cap.
3. Record tokens, turns, wall time, whether tests passed, and whether importers still compile.
4. Regress the outcomes on the section 7.1 inputs and fit `context_budget`, the per-item token costs and the superlinear exponent.
5. Ship as `config_version: rebuild-<date>-<agent>` and flip `calibrated` to true for the estimate.

This is well-defined and repeatable, unlike a refactoring-task corpus, and it directly measures what the number claims. It does not affect the gate.

## 12. Integrations

- **CI:** `astimate check --format github` in a workflow step; exit 3 fails the job. A composite GitHub Action under `action/` wraps install and invocation.
- **Claude Code Stop hook:** `astimate check --format hook` returns a block decision with the violations as the reason, so the agent continues and fixes them. Documented with a ready-to-paste `settings.json` snippet.
- **Pre-commit:** documented invocation with `--all` disabled and `--base` set.
- **MCP:** `check_package` for in-task self-checks.

## 13. Multi-language extension

The `Extractor` interface is the only language-specific surface. Adding a language means implementing it, defining module, package and internal import for that ecosystem, providing fixtures, and calibrating thresholds per language. Go keeps the native toolchain because `untested_exports` and coupling need type information. Other languages use tree-sitter.

### 13.1 TypeScript

TypeScript is parsed with `github.com/odvcencio/gotreesitter` (v0.55.1), a pure-Go tree-sitter runtime that bundles the TypeScript and TSX grammars. It needs no cgo, so `CGO_ENABLED=0` release builds keep working; the official cgo binding was not needed because the pure-Go runtime parses the fixture and the test snippets with no error nodes. The runtime is pre-1.0 and embeds all grammars by default; release builds pass the subset tags listed as `BUILD_TAGS` in the Makefile (one per grammar in use, checked against goreleaser and the action by a test) so only the two in use are embedded. The extractor lives in `internal/lang/typescript`; `internal/lang/duptok` is the duplicate finder over abstract token classes it shares with nothing yet (the Go extractor keeps its own until they are unified).

Counting rules, the TypeScript counterpart of 6.5 (`testdata/ts/fixture/golden/COUNTING.md` shows each applied):

- **Module:** the directory of the nearest `package.json`; a directory below it with its own `package.json` is a separate module. There is no module path; package ids are directories relative to the root (`.` for the root).
- **Package:** a directory with at least one `.ts`, `.tsx`, `.mts` or `.cts` file that is neither a declaration file (`.d.ts`, `.d.mts`, `.d.cts`) nor a test file. `node_modules`, `dist`, `build` and dot-prefixed directories are skipped. Declaration files count nowhere. `.js` is not read.
- **Test files:** `*.test.*` or `*.spec.*` with extension `.ts`, `.tsx`, `.mts` or `.cts`, or anything under `__tests__/`, which belongs to the package containing that directory. A test file in a directory with no package is ignored. `test_funcs` counts calls to `it`, `test` or `bench` with a string or template first argument, at top level or inside `describe` bodies; member and curried forms such as `it.only` and `it.each(t)(...)` count once each.
- **Imports:** static `import` and `import type`, `export ... from`, `import x = require()`, and `require("lit")` or `import("lit")` calls. A relative specifier, a `paths` alias of the root `tsconfig.json` with its `extends` chain applied, or, with a `baseUrl`, a non-`node:` bare specifier resolves inside the module by the `tsc` rules (`moduleResolution` `bundler` where it and `node16` differ; exact alias match, then longest prefix, then the first target that resolves; JSONC accepted). A path resolves only to a file with a TypeScript source or declaration extension: the file named; for a `.js`, `.jsx`, `.mjs` or `.cjs` specifier its `.ts`/`.tsx`/`.d.ts`, `.tsx`, `.mts`/`.d.mts` or `.cts`/`.d.cts` source; with no extension, the name with `.ts`, `.tsx` or `.d.ts` appended. Failing a file, a directory resolves through its `package.json` `typings`, `types` or `main` target, else its `index` file (`.ts`, `.tsx`, `.d.ts`, then the `.mts` and `.cts` forms); `.`, `..` and a trailing `/` are directories only. A relative specifier that resolves to nothing counts nowhere; a matched alias none of whose targets resolves, and a bare specifier that does not resolve under `baseUrl`, fall through to the built-in and npm rules. `extends` takes a string or an array of relative paths or bare names under `node_modules`; a bare package name uses its `package.json` `exports` (a string or a `"."` entry), then its `tsconfig` field, then its `tsconfig.json`; a leading `${configDir}` in `extends`, `baseUrl` or a `paths` target is the root configuration's directory (tsc 5.5); each of `baseUrl` and `paths` comes from the last file setting it, a child's `paths` replacing the parent's whole map, with relative values resolved against the declaring file; a missing file, a cycle or a chain deeper than 32 ends the chain. Such an import is internal only when the resolved file's directory is another package; an import of the same package is not counted, one landing in a non-package directory counts nowhere, and one leaving the module is external. Bare specifiers are external, counted by npm package name (`@scope/name` or the first segment). `stdlib_imports` counts Node built-ins by name without the `node:` prefix.
- **`exported_symbols`:** each name an `export` declaration adds (function, class, interface, type, enum, namespace, each bound `let`/`const`/`var` name), one per `export default`, and one per name in an `export {...}` list, including re-exports from other packages. Overload signatures and class members are not counted.
- **`untested_exports`:** exported functions, exported arrow or function constants, functions exported through a list, and public methods of exported classes; constructors, accessors, private, protected and `#` members are excluded. A candidate is untested when its name appears as no identifier in any test file of the package. This is name matching without type information. As in Go (6.4), a candidate whose doc comment (the comments directly above it, no blank line) has the line `//astimate:untested`, optionally followed by a space and a reason, is excluded and listed separately; a directive above a class does not apply to its methods.
- **`globals`:** names bound by top-level `let` or `var` (not `const`), excluding `_`. **`init_funcs`:** the number of files with at least one top-level call statement, plain or awaited.
- **Functions:** top-level declarations, top-level variables initialised with a function or arrow function, and methods and function-valued fields of top-level classes; nested functions are scored inside their parent.
- **Cognitive complexity** follows the gocognit rules: +1 plus nesting for `if`, `switch`, `for`, `for-in`, `for-of`, `while`, `do`, `catch` and the ternary; +1 flat for `else` and `else if`; +1 per run of like `&&`, `||` or `??` operators, with parentheses starting a new run; +1 for a labelled `break` or `continue`; nesting deepens inside nested functions; recursion is not counted. **`max_nesting`** counts `if`, the loops, `switch`, `try` and nested functions; an `else if` adds a level.
- **Duplication:** tokens are tree-sitter leaves with comments and zero-width tokens (automatic semicolons) dropped. Identifiers and `undefined` become ID; strings, templates, regexes, numbers, `true`, `false`, `null` and JSX text become one LIT each; a unary sign on a number is marked as a sign for the fold rule; type keywords such as `number` stay keywords. The literal-only, fold-sign and coverage rules are the Go ones (6.3).
- **v1 fields:** `instability`, `abstractness` (exported interfaces over exported classes, interfaces, type aliases and enums) and `main_sequence_distance` are computed; the other v1 fields are null and the module row (8.1) is not produced. `--tokenizer o200k` is supported.

Changed-package detection (8.4) is currently Go-only, so plain `check` selects no TypeScript packages; `check --all` is the working form until that is fixed.

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

## 15. Open questions

- Should `check` also evaluate packages whose importers changed, since a change to `hub` can affect their metrics? Proposed: no in v0; `--all` covers it. Cross-package duplication is handled by a module-level row instead (section 6, `dup_blocks_cross_pkg`).
- Resolved: `duplication.ignore_literal_only` and `duplication.fold_signs` ship on by default (6.3). Resolved: straight-line call sequences that differ only in their arguments (the addition chains in `crypto/internal/fips140/nistec`, the unrolled loops in `math/big`) count as duplication. A rule dropping repeats made only of calls to one or two functions was measured over the standard library: the strict form drops two data-like blocks and reaches neither package; the loose form reaches `nistec` (33.3% to 32.9%) only by also dropping a pasted debug dump in `math/big`, which is the copy-paste the gate targets.
- Which reference modules go in the corpus? To be listed in S-034.
