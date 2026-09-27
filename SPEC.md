# Astimate: Specification

**Status:** draft v0.3, 2026-09-27. Supersedes v0.2, which framed the tool as a pre-work planning signal. This revision makes the post-implementation quality gate the primary use and keeps planning and ranking as secondary uses.

Astimate is a static-analysis tool that checks whether an LLM-written change left a package in a state a human or the next agent can maintain. It extracts structural metrics per package, compares them against a baseline and a set of thresholds, and fails when the package got worse. It also produces a composite **AI Friction Index** (0.0 to 10.0) for ranking and planning. It runs as a CLI, a CI step, a Claude Code hook and a Model Context Protocol (MCP) server. Go is the first supported language; everything downstream of metric extraction is language-agnostic.

Sections 8 through 11 (gate, CLI, MCP, calibration) and 14 (milestones) are written to be split directly into stories.

---

## 1. Goals

1. **Gate:** fail a change that makes a package materially less manageable, and say exactly which rule was broken so the agent can fix it without human interpretation.
2. **Ratchet, not absolute:** judge a change against the package's own baseline, so work on legacy packages is not blocked by pre-existing debt while new debt is.
3. **Self-check loop:** let an agent run the gate itself before declaring work done, via CLI, hook or MCP.
4. **Rank and plan:** score every package in a module so humans and agents can see where debt is concentrated.
5. **Honest defaults:** thresholds and weights are labelled uncalibrated until the calibration in section 11 has run.

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
| Friction Index | The 0.0 to 10.0 composite produced from raw metrics and a weights config. |

## 4. What the gate targets (evidence summary)

The gate targets the ways LLM-written changes tend to degrade a package. The metric set and default thresholds follow from that, with the composite score's evidence kept for ranking.

| Failure mode | Metric(s) | Evidence | Strength |
| --- | --- | --- | --- |
| Copy-paste instead of extraction | `duplication_pct`, `dup_blocks` | Industry reports on AI-assisted repositories (GitClear, 2024 and 2025) show rising duplicated blocks and falling moved-or-refactored code. Not peer-reviewed, but consistent across years. | Moderate; the most specific LLM failure mode found |
| Package bloat past what the next agent can hold | `tokens_est`, `sloc`, `largest_file_sloc` | Successful agent trajectories stay under 20 to 30k tokens; resolve rates collapse past 64k (arXiv 2602.16069, 2505.07897). | Strong |
| Exporting everything | `exported_symbols` | No direct study. Wider API surface raises fan-in cost and the facts the next agent must hold (Coherence Debt, arXiv 2608.16630). | Indirect |
| Untested additions | `untested_exports`, `has_tests` | Agents self-correct through a run-and-check loop; a package without tests denies the next agent that loop. | Moderate, indirect |
| Hidden state | `globals`, `init_funcs` | No direct study. Plausible; kept at low weight. | Unproven |
| Deep nesting | `max_nesting`, `cognitive_p90` | Classical complexity shows no consistent correlation with LLM performance once length is controlled (arXiv 2602.07882). Kept as a gate on regressions only. | Weak |
| Coupling growth | `internal_imports` | Failures come from coupled facts absent from context (arXiv 2608.16630; CrossCodeEval; RepoBench). | Moderate |
| Blast radius | `fan_in` | Strongest predictor of task difficulty (SWE-bench analyses, arXiv 2511.00197). Rarely changes within one PR, so it drives ranking more than gating. | Strong for ranking |

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
                                        └─────────►│ Scorer       │──────────┘
                                                   │ weights      │
                                                   └──────┬───────┘
                                                          │ thresholds + weights
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
│   ├── lang/
│   │   └── golang/              # Go extractor
│   ├── gate/                    # Thresholds config, baseline, ratchet comparison, violations
│   ├── score/                   # Scorer, weights config, tiers, explanations
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
    Extract(ctx ModuleContext, pkg string) (RawMetrics, error)
}
```

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
| `dup_blocks` | int | Duplicate token sequences of at least `dup_min_tokens` (default 40) within the package; see 6.3 | v0 |
| `duplication_pct` | float | Share of non-test SLOC covered by a duplicate block | v0 |
| `test_files` | int | `_test.go` files | v0 |
| `test_funcs` | int | `Test*`, `Benchmark*`, `Fuzz*`, `Example*` functions | v0 |
| `has_tests` | bool | `test_funcs > 0` | v0 |
| `untested_exports` | int | Exported funcs and methods not referenced from any test file in the package; see 6.4 | v0 |
| `concrete_param_ratio` | float | Share of exported-function params typed as struct or pointer-to-struct from another package versus interface | v1 |
| `dup_blocks_cross_pkg` | int | Duplicate blocks shared with other packages in the module | v1 |
| `uses_cgo` | bool | Imports `"C"` | v1 |
| `uses_reflect` | bool | Imports `reflect` or `unsafe` | v1 |
| `generated_files` | int | Files with a `Code generated ... DO NOT EDIT` header | v1 |
| `coverage_pct` | float | Statement coverage from `go test -cover`, only with `--coverage` | v1 |

### 6.1 Token estimation

Default: `tokens_est = bytes / chars_per_token`, `chars_per_token` defaulting to **3.2** for Go source. The original spec's 4.0 is a prose figure and undercounts code by 15 to 50 percent on current tokenizers. Opt-in exact counting: `--tokenizer=o200k` via `pkoukk/tiktoken-go`, offline.

### 6.2 Test-file handling

Test files are excluded from every metric except `tokens_est_with_tests`, `test_files`, `test_funcs` and the reference side of `untested_exports`. External test packages (`foo_test`) in the same directory fold into the package.

### 6.3 Duplication

Tokens are taken from `go/scanner` over non-test files. Identifiers, literals and comments are normalized (identifier to `ID`, string and numeric literals to `LIT`) so renamed copies still match. A duplicate block is a maximal token sequence of length at least `dup_min_tokens` that occurs at least twice, found with a suffix array or rolling hash over the normalized stream. `dup_blocks` counts distinct sequences; `duplication_pct` is the share of SLOC covered by any occurrence. Generated files are excluded. The threshold and normalization rules are config values.

### 6.4 Untested exports

An exported function or method counts as untested when no identifier in any test file of the package (internal or external test package) resolves, via `types.Info.Uses`, to that function or to a method with the same name on the same receiver type. Exported types, vars and consts are not counted; the metric targets behavior, not declarations. Reported as a count and, in the gate, primarily as a delta so new untested behavior fails while legacy gaps are only reported.

## 7. Scoring (composite)

The composite is secondary to the gate. It drives `rank`, the planning use and the summary line of `check`.

### 7.1 Normalization

```
pressure(x, k) = 1 - exp(-x / k)
```

`k` is the value at which pressure reaches about 0.63. Per-metric `k` values live in the weights config. No hard caps. The `has_tests` penalty is 1.0 when false, 0.0 when true, scaled by `coverage_pct` in v1.

### 7.2 Composite

```
friction = 10 * Σ (weight_i * pressure_i)      where Σ weight_i = 1
```

Default weights, labelled uncalibrated until section 11 has run:

| Term | Metric(s) | Weight | k |
| --- | --- | --- | --- |
| Context size | `tokens_est` | 0.20 | 25,000 |
| Duplication | `duplication_pct` | 0.15 | 8 |
| Blast radius | `fan_in` | 0.15 | 6 |
| Coupling | `internal_imports` | 0.10 | 8 |
| Feedback loop | `has_tests` penalty, `untested_exports` | 0.15 | 5 (untested) |
| Surface | `exported_symbols` | 0.10 | 40 |
| Hidden state | `globals + 2 * init_funcs` | 0.05 | 10 |
| Complexity | `cognitive_p90` | 0.10 | 25 |

Terms whose metric is null are dropped and remaining weights renormalized.

### 7.3 Tiers and explanations

| Tier | Range |
| --- | --- |
| LOW | < 3.5 |
| MEDIUM | 3.5 to 6.5 |
| HIGH | > 6.5 |

Output includes `drivers` (top two terms by contribution) and `suggestions` generated from those drivers.

### 7.4 Acceptance invariants

- Go stdlib `errors` scores LOW; `net/http` scores HIGH.
- A package with zero fan-in, tests present, no duplication and under 5k tokens scores LOW.
- Monotonicity: increasing any raw metric never lowers the score.

## 8. Quality gate

### 8.1 Semantics

`astimate check` evaluates every changed package (or all packages with `--all`) against a thresholds config. Each threshold names a metric and one or both of:

- `max`: an absolute ceiling at head.
- `max_delta`: the largest permitted increase from baseline to head. Negative values require improvement.

A threshold with only `max_delta` is a pure ratchet: legacy debt passes, new debt fails. A threshold with only `max` is absolute. With both, either breach is a violation. Boolean metrics use `require: true` (for example `has_tests` when `sloc > 100`).

Packages that are new at head have no baseline; `max_delta` rules are evaluated against zero, so a new package is judged absolutely on what it introduces.

Any violation fails the gate with exit code 3. Violations are reported one per line with metric, baseline value, head value, limit and a fix suggestion.

### 8.2 Default thresholds

Uncalibrated placeholders, replaced by 90th-percentile values from the reference corpus in section 11.

| Metric | `max` | `max_delta` | Rationale |
| --- | --- | --- | --- |
| `tokens_est` | 30,000 | +5,000 | Keep packages inside the range agents handle |
| `largest_file_sloc` | 800 | +150 | Files past this rarely fit an edit in one view |
| `exported_symbols` | 60 | +8 | Surface growth per change |
| `duplication_pct` | 5.0 | +1.0 | Copy-paste is the primary target |
| `dup_blocks` | none | +0 | No new duplicate blocks |
| `untested_exports` | none | +0 | New exported behavior needs a test |
| `max_nesting` | 4 | +0 | Never deeper |
| `cognitive_p90` | 25 | +5 | |
| `globals` | 5 | +0 | No new package state |
| `init_funcs` | 1 | +0 | |
| `internal_imports` | 12 | +3 | |
| `friction_index` | none | +0.5 | Composite ratchet as a catch-all |
| `has_tests` | require true when `sloc > 100` | | |

### 8.3 Baselines

Two sources, chosen by flag:

- `--base <ref>` (default): the merge-base of `HEAD` and `<ref>` (default `origin/main` or `main`). The base tree is checked out into a temporary `git worktree`, analyzed, and removed. Packages are matched by import path.
- `--baseline <file>`: a committed `.astimate/baseline.json` written by `astimate baseline write`. For repositories that prefer explicit, reviewable baselines or that run outside git.

### 8.4 Changed-package detection

`git diff --name-only <merge-base>...HEAD` mapped to packages by directory. A change to a package's test files alone still selects it. `--all` overrides.

### 8.5 Output formats

- `text` (default): violations then a one-line summary per package.
- `json`: the section 10.2 report per package plus a `violations` array and `passed` bool.
- `hook`: the JSON shape Claude Code Stop hooks consume: `{ "decision": "block", "reason": "<violations as text>" }` on failure, `{}` on success, so the agent is told to keep working and why.
- `github`: `::error file=<pkgdir>::` workflow annotations, one per violation.

## 9. CLI

```
astimate check    [<module-root>] [--base ref | --baseline file] [--all] [--thresholds file] [--format text|json|hook|github]
astimate baseline write [<module-root>] [--out .astimate/baseline.json]
astimate assess   <package-dir> [--json] [--config weights.yaml] [--tokenizer=est|o200k] [--coverage]
astimate rank     <module-root> [--json] [--top N] [--sort friction|fan_in|tokens|duplication]
astimate serve    [--config weights.yaml] [--thresholds file] [--allow-any-path]
astimate config init [--out astimate.yaml]     # weights and thresholds in one file with comments
astimate version
```

Exit codes: 0 success or gate passed, 1 usage error, 2 analysis failure, 3 gate failed. Logs go to stderr only.

## 10. MCP server

Built on `github.com/modelcontextprotocol/go-sdk` v1.8 or later, which supports the 2026-07-28 protocol revision and the legacy initialize handshake.

### 10.1 Tools

| Tool | Input | Output |
| --- | --- | --- |
| `check_package` | `{ "path": string, "base"?: string }` | Gate result: `passed`, `violations[]`, per-package report. The agent's self-check. |
| `assess_package` | `{ "path": string, "tokenizer"?: string }` | One report (10.2) |
| `rank_packages` | `{ "module_root": string, "top"?: int, "sort"?: string }` | Sorted array of `{ path, friction_index, tier, fan_in, tokens_est, duplication_pct }` |
| `explain_metric` | `{ "metric": string }` | Definition, evidence note and default threshold for one metric |

Results return `content` (text) and `structuredContent` (JSON), with `isError: true` on analysis failure. A gate failure is not an error; it is a result with `passed: false`.

### 10.2 Report schema

```json
{
  "language": "go",
  "package_path": "internal/billing",
  "module_path": "github.com/acme/app",
  "friction_index": 6.8,
  "tier": "HIGH",
  "calibrated": false,
  "drivers": [ { "term": "duplication", "contribution": 1.4, "detail": "duplication_pct=9.2" } ],
  "suggestions": [ "9.2% of lines are in duplicate blocks (4 blocks); extract shared helpers." ],
  "metrics": { "...": "every field from section 6" },
  "baseline": { "ref": "a1b2c3d", "metrics": { "...": "same fields" } },
  "violations": [
    { "metric": "dup_blocks", "base": 1, "head": 4, "limit": "max_delta +0", "suggestion": "..." }
  ],
  "passed": false,
  "astimate_version": "0.3.0",
  "config_version": "default-uncalibrated-1"
}
```

## 11. Calibration

Two calibrations, in priority order.

### 11.1 Thresholds from a reference corpus (primary)

1. Assemble a corpus of 20 or more well-regarded Go modules: the standard library plus widely used, actively maintained open-source modules with permissive licenses.
2. Run `astimate rank --json` over each and pool all packages.
3. Set each metric's default `max` at the pooled 90th percentile, rounded to a human-readable value, and `max_delta` at a fraction of the interquartile range. Record the corpus, commit hashes and the resulting distribution.
4. Ship as `config_version: thresholds-<date>`. Good code passes by construction; the gate flags what falls outside what good projects do.
5. Re-run yearly or when the metric set changes.

### 11.2 Composite weights from agent outcomes (secondary, optional)

Run an agent on 30 or more refactoring tasks with test oracles, record turns, tokens and pass/fail, and fit the composite weights. This improves `rank` and `assess` but does not affect the gate. Until it runs, reports carry `calibrated: false` for the composite; the thresholds carry their own version.

## 12. Integrations

- **CI:** `astimate check --format github` in a workflow step; exit 3 fails the job. A composite GitHub Action under `action/` wraps install and invocation.
- **Claude Code Stop hook:** `astimate check --format hook` returns a block decision with the violations as the reason, so the agent continues and fixes them. Documented with a ready-to-paste `settings.json` snippet.
- **Pre-commit:** documented invocation with `--all` disabled and `--base` set.
- **MCP:** `check_package` for in-task self-checks.

## 13. Multi-language extension

The `Extractor` interface is the only language-specific surface. Adding a language means implementing it, defining module, package and internal import for that ecosystem, providing fixtures, and calibrating thresholds per language. Go keeps the native toolchain because `untested_exports` and coupling need type information. Other languages use tree-sitter; the official binding needs cgo, `odvcencio/gotreesitter` is pure Go and pre-1.0. First candidate: TypeScript.

## 14. Milestones

Each bullet is intended to become one story.

### M0: Skeleton

- Module, layout, CI with `gofmt`, `go vet`, `golangci-lint`, `go test -race`.
- `RawMetrics`, `Extractor`, `ModuleContext`, registry.
- `version` and `config init`.
- Fixture module with packages exercising every v0 metric, including a duplication case and an untested-export case.

*Accepts when:* build and tests pass in CI; fixture loads with `go/packages`.

### M1: Go extractor (v0 metrics)

- Loader, import classification, fan-in, globals and init, nesting and cognitive complexity, size, tokens, tests.
- Duplication (6.3) and untested exports (6.4).
- Full assembly with golden verification and the stdlib `errors` checks.

*Accepts when:* every fixture package matches its golden; `errors` reports `globals=2`, `internal_imports=0`.

### M2: Scorer, thresholds and CLI

- Unified config (weights and thresholds) loader and validation.
- Normalization, composite, tiers, drivers, suggestions.
- Thresholds evaluation and violation reporting.
- `assess`, `rank`, `baseline write`, `check` with git-ref and file baselines, changed-package detection and all four output formats.
- Section 7.4 invariants and monotonicity property test.

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

- `concrete_param_ratio`, `dup_blocks_cross_pkg`, `uses_cgo`, `uses_reflect`, `generated_files`, `coverage_pct`.
- TypeScript extractor behind tree-sitter with fixtures.
- Per-language config overrides.

### M7: Composite weight calibration (optional)

- Task corpus, agent runner, fitting, ship calibrated weights.

## 15. Open questions

- Should `check` also evaluate packages whose importers changed, since a change to `hub` can affect their metrics? Proposed: no in v0; `--all` covers it.
- Should the duplication detector ignore table-driven test-like literal blocks in non-test code? Proposed: literals normalize to `LIT`, so long literal tables will match; add a `dup_ignore_literal_only` option in v1.
- Should `untested_exports` accept an `//astimate:untested` directive for intentionally untested wrappers? Proposed: yes, in M2, logged in output.
- Which reference modules go in the corpus? To be listed in S-034.
