# Astimate: Specification

**Status:** draft v0.2, 2026-09-27. Supersedes the original `astimate_spec.md`, whose scoring and MCP server were found unworkable (see the assessment doc).

Astimate is a static-analysis tool that tells a coding agent, before it starts, how much friction a package will cause it. It reports raw structural metrics and a composite **AI Friction Index** (0.0 to 10.0), exposed as a CLI and as a Model Context Protocol (MCP) server. Go is the first supported language. The architecture keeps everything downstream of metric extraction language-agnostic so more languages can be added.

Sections 7 through 9 (interfaces), 10 (calibration) and 12 (milestones) are written to be split directly into stories. Each milestone lists acceptance criteria.

---

## 1. Goals

1. Give an agent a cheap, deterministic, package-level signal it can call before editing: "this will be easy" versus "partition this first".
2. Expose the raw metrics behind the score, so an agent (or a human) can see *why* a package is hard and act on the specific cause.
3. Rank every package in a module so an agent can plan an order of attack.
4. Be honest about uncertainty: the default weights are labelled uncalibrated until the calibration harness (section 10) has run.

## 2. Non-goals

- Not a linter. Astimate does not report individual findings or fail builds.
- Not a human-effort estimator. `scc` already does COCOMO.
- Not a code-health product. CodeScene covers that space with a validated metric; Astimate targets agent friction specifically and is free and local.
- No network calls in the default path. Exact token counting via a remote API is opt-in.

## 3. Definitions

| Term | Meaning |
| --- | --- |
| Module | A Go module rooted at a `go.mod`. The unit of analysis for fan-in and "internal". |
| Package | A Go package directory within the module. The unit that gets a score. |
| Internal import | An import whose path is inside the module path from `go.mod`. |
| Fan-out | Number of distinct internal packages this package imports. |
| Fan-in | Number of distinct internal packages that import this package. This is the blast radius of a change. |
| Raw metrics | The language-agnostic struct in section 6 that an extractor produces. |
| Friction Index | The 0.0 to 10.0 composite produced by the scorer from raw metrics and a weights config. |

## 4. Why these metrics (evidence summary)

The metric set is chosen from published evidence on what predicts coding-agent failure, not from intuition. Weighting reflects the strength of that evidence.

| Signal | Evidence strength | Source |
| --- | --- | --- |
| Change size and cross-file propagation (fan-in) | Strong. Hard SWE-bench tasks average 11x more lines changed; missing call sites is the dominant failure on large patches. | arXiv 2511.00197, SWE-bench Verified analysis |
| Context size (tokens) | Strong. Successful trajectories stay under 20 to 30k tokens; resolve rates collapse past 64k. | arXiv 2602.16069, arXiv 2505.07897 |
| Cross-file coupling (internal imports) | Moderate. Failures come from coupled facts absent from context. | arXiv 2608.16630, CrossCodeEval, RepoBench |
| Test presence | Moderate (indirect). Agents self-correct through a run-and-check loop. | Every readiness scorer surveyed includes it |
| Structural complexity (nesting, cognitive) | Weak. No consistent correlation once length is controlled. | arXiv 2602.07882 |
| Globals, init functions, concrete-struct params | Unproven. Plausible; kept at low weight pending calibration. | None found |

## 5. Architecture

```
                 ┌──────────────────────┐
                 │  Go extractor        │
                 │  go/packages+go/types│──┐
                 └──────────────────────┘  │   ┌─────────────┐   ┌────────────────┐   ┌──────────────┐
                                           ├──►│ Raw metrics │──►│ Scorer         │──►│ CLI (JSON)   │
                 ┌──────────────────────┐  │   │ (per pkg)   │   │ weights config │   ├──────────────┤
                 │  Other extractors    │──┘   └─────────────┘   └───────┬────────┘   │ MCP server   │
                 │  (tree-sitter, later)│                                │            └──────────────┘
                 └──────────────────────┘                                │ weights
                                                                ┌────────┴────────┐
                                                                │ Calibration     │
                                                                │ agent run logs  │
                                                                └─────────────────┘
```

**Packages** (Go module `github.com/<owner>/astimate`, Go 1.27 or later):

```
astimate/
├── go.mod
├── cmd/astimate/main.go        # CLI entrypoint; `astimate serve` starts MCP
├── internal/
│   ├── metrics/                 # RawMetrics struct and Extractor interface (language-agnostic)
│   ├── lang/
│   │   └── golang/              # Go extractor: loader, imports, globals, complexity, tokens, tests
│   ├── score/                   # Scorer, weights config, tiers, explanations
│   ├── report/                  # JSON and text output shaping
│   └── mcpserver/               # MCP tools on github.com/modelcontextprotocol/go-sdk
├── testdata/                    # Fixture modules with known expected metrics
└── SPEC.md
```

**Extractor interface** (language-agnostic):

```go
type Extractor interface {
    // Language returns the identifier, e.g. "go".
    Language() string
    // Detect reports whether this extractor can analyze the module at root.
    Detect(root string) bool
    // Packages lists analyzable package paths under root.
    Packages(root string) ([]string, error)
    // Extract computes raw metrics for one package. Fan-in requires module-wide
    // knowledge, so the extractor receives the module context.
    Extract(ctx ModuleContext, pkg string) (RawMetrics, error)
}
```

## 6. Raw metrics

Every field is reported in output. Fields marked *(v0)* are required for the first release; others are v1.

| Field | Type | Definition | Release |
| --- | --- | --- | --- |
| `files` | int | Non-test source files in the package | v0 |
| `sloc` | int | Non-blank, non-comment source lines in non-test files | v0 |
| `largest_file_sloc` | int | SLOC of the largest non-test file | v0 |
| `tokens_est` | int | Estimated tokens of non-test source; see 6.1 | v0 |
| `tokens_est_with_tests` | int | Same including test files | v0 |
| `internal_imports` | int | Fan-out: distinct internal packages imported (non-test files) | v0 |
| `external_imports` | int | Distinct non-stdlib, non-module imports | v0 |
| `stdlib_imports` | int | Distinct standard-library imports | v0 |
| `fan_in` | int | Distinct internal packages importing this package | v0 |
| `exported_symbols` | int | Exported funcs, types, vars and consts at package level | v0 |
| `globals` | int | Package-level `var` declarations in non-test files (count of specs, excluding `_`) | v0 |
| `init_funcs` | int | Number of `init()` functions | v0 |
| `max_nesting` | int | Deepest nesting of `if`/`for`/`range`/`switch`/`select`/`func` literal across all functions | v0 |
| `cognitive_total` | int | Sum of cognitive complexity per gocognit's rules | v0 |
| `cognitive_p90` | int | 90th percentile cognitive complexity per function | v0 |
| `func_count` | int | Functions and methods in non-test files | v0 |
| `test_files` | int | `_test.go` files | v0 |
| `test_funcs` | int | `Test*`, `Benchmark*`, `Fuzz*` and `Example*` functions | v0 |
| `has_tests` | bool | `test_funcs > 0` | v0 |
| `concrete_param_ratio` | float | Share of exported-function parameters whose type is a struct or pointer to struct from another package, versus interface | v1 |
| `uses_cgo` | bool | Imports `"C"` | v1 |
| `uses_reflect` | bool | Imports `reflect` or `unsafe` | v1 |
| `generated_files` | int | Files with a `Code generated ... DO NOT EDIT` header | v1 |
| `coverage_pct` | float | Statement coverage from `go test -cover`, only with `--coverage` flag | v1 |

### 6.1 Token estimation

Default: `tokens_est = bytes / chars_per_token` with `chars_per_token` defaulting to **3.2** for Go source. The original spec's 4.0 is a prose figure and undercounts code by 15 to 50 percent on current tokenizers. The ratio is a config value so it can be set per target model.

Opt-in exact counting: `--tokenizer=o200k` uses `pkoukk/tiktoken-go` offline. There is no offline Anthropic tokenizer; the Anthropic count-tokens API is not called by default because it is a network dependency.

### 6.2 Test-file handling

Test files are excluded from every metric except `tokens_est_with_tests`, `test_files` and `test_funcs`. External test packages (`foo_test`) in the same directory are folded into the package's test metrics.

## 7. Scoring

### 7.1 Normalization

Each raw metric is mapped to a 0.0 to 1.0 pressure with a saturating curve, never a hard cap:

```
pressure(x, k) = 1 - exp(-x / k)
```

`k` is the value at which pressure reaches about 0.63. Per-metric `k` values live in the weights config. This avoids the original spec's failure where nine imports pinned a term at maximum.

Test presence is a penalty term: `pressure = 1.0` when `has_tests` is false, `0.0` when true, scaled by `coverage_pct` in v1.

### 7.2 Composite

```
friction = 10 * Σ (weight_i * pressure_i)      where Σ weight_i = 1
```

Default weights, **labelled uncalibrated in output** until section 10 has run:

| Term | Metric(s) | Weight | k |
| --- | --- | --- | --- |
| Context size | `tokens_est` | 0.25 | 25,000 |
| Blast radius | `fan_in` | 0.20 | 6 |
| Coupling | `internal_imports` | 0.15 | 8 |
| Feedback loop | `has_tests` penalty | 0.15 | n/a |
| Hidden state | `globals + 2 * init_funcs` | 0.10 | 10 |
| Complexity | `cognitive_p90` | 0.10 | 25 |
| Interface opacity | `concrete_param_ratio` (v1; weight 0 in v0, redistributed to context size) | 0.05 | n/a |

### 7.3 Tiers and explanations

| Tier | Range | Meaning |
| --- | --- | --- |
| LOW | < 3.5 | Suitable for direct autonomous work in one pass |
| MEDIUM | 3.5 to 6.5 | Work in one pass but load dependency interfaces first and run tests after each change |
| HIGH | > 6.5 | Partition before delegating; identify importers and add tests first |

Output includes a `drivers` list naming the top two terms by contribution and a `suggestions` list generated from those drivers, not a fixed string per tier. Examples: high `fan_in` yields "N packages import this; changes to exported symbols propagate to them". Missing tests yields "no tests; add characterization tests before refactoring".

### 7.4 Acceptance invariants

These hold for any weights config shipped as default:

- Go stdlib `errors` scores LOW.
- Go stdlib `net/http` scores HIGH.
- A package with zero fan-in, tests present and under 5k tokens scores LOW.
- Monotonicity: increasing any raw metric never lowers the score.

## 8. CLI

```
astimate assess <package-dir> [--json] [--config weights.yaml] [--tokenizer=est|o200k] [--coverage]
astimate rank   <module-root> [--json] [--top N] [--sort friction|fan_in|tokens]
astimate serve  [--config weights.yaml]        # MCP over stdio
astimate config init                            # writes default weights.yaml with comments
astimate version
```

- Exit code 0 on success, 2 on analysis failure, 1 on usage error.
- Default output is a short human-readable table. `--json` emits the full report (section 9.2).
- Logs go to stderr only. Stdout is reserved for results, which matters in MCP mode.

## 9. MCP server

Built on `github.com/modelcontextprotocol/go-sdk` (v1.8 or later), which supports both the 2026-07-28 protocol revision and the legacy initialize handshake. The hand-rolled JSON-RPC loop in the original spec is dropped entirely.

### 9.1 Tools

| Tool | Input | Output |
| --- | --- | --- |
| `assess_package` | `{ "path": string, "tokenizer"?: string }` | One report (9.2) |
| `rank_packages` | `{ "module_root": string, "top"?: int }` | Array of `{ path, friction_index, tier, fan_in, tokens_est }` sorted by friction descending |
| `explain_metric` | `{ "metric": string }` | Definition and evidence note for one metric name |

Results return both `content` (a compact text rendering) and `structuredContent` (the JSON report), with `isError: true` on analysis failure rather than a JSON-RPC error.

### 9.2 Report schema

```json
{
  "language": "go",
  "package_path": "internal/billing",
  "module_path": "github.com/acme/app",
  "friction_index": 6.8,
  "tier": "HIGH",
  "calibrated": false,
  "drivers": [
    { "term": "blast_radius", "contribution": 1.9, "detail": "fan_in=11" },
    { "term": "context_size", "contribution": 1.7, "detail": "tokens_est=31200" }
  ],
  "suggestions": [
    "11 packages import this package; changes to exported symbols propagate to them.",
    "Estimated 31k tokens exceeds the ~25k range where agent success drops; split by file group."
  ],
  "metrics": { "...": "every field from section 6" },
  "astimate_version": "0.2.0",
  "weights_version": "default-uncalibrated-1"
}
```

## 10. Calibration

The default weights are a hypothesis. This section defines how they become a measurement.

1. **Task corpus.** Choose 30 or more packages across 3 or more real Go modules spanning the expected range of sizes. For each, define one refactoring task with a passing test suite as the oracle.
2. **Runs.** Execute each task with the target agent (Claude Code as the first target) at least 3 times. Record: turns, total tokens, tool calls, wall time, whether tests pass, and whether the diff touched every required call site.
3. **Fit.** Regress outcome (pass/fail and cost) on the raw metrics. Report which metrics carry signal. Fit weights and `k` values; freeze them as `weights_version: calibrated-<date>-<agent>`.
4. **Report.** Publish the fitted weights, the correlation table and the corpus so the claim is reproducible.
5. **Re-run** when the target model changes. CodeScene's study found code-health effects vanished for Claude Sonnet 4.5, so weights are expected to drift toward size and coupling over time.

Until step 3 completes, every report carries `"calibrated": false`.

## 11. Multi-language extension

The `Extractor` interface (section 5) is the only language-specific surface. Adding a language means:

1. Implement `Extractor` producing `RawMetrics`. Fields that do not apply are reported as null and excluded from scoring with weights renormalized.
2. Define what "module", "package" and "internal import" mean for that ecosystem (e.g. TypeScript: `package.json` workspace, directory, relative or path-alias import).
3. Provide fixtures under `testdata/<lang>/`.
4. Run calibration per language. Weights are stored per language.

Parsing: Go uses the standard toolchain and must not be migrated to tree-sitter, because coupling metrics depend on type information. Other languages use tree-sitter. The official binding (`tree-sitter/go-tree-sitter`) requires cgo; `odvcencio/gotreesitter` is a pure-Go alternative that is pre-1.0. Decide per language at implementation time; the interface hides the choice.

First candidate after Go: TypeScript, because agent usage is highest there and tree-sitter grammar quality is mature.

## 12. Milestones

Each bullet under a milestone is intended to become one story. Acceptance criteria are listed per milestone.

### M0: Skeleton

- Initialize module, `cmd/astimate`, `internal/` layout, CI running `go vet`, `go test`, and `golangci-lint`.
- Define `RawMetrics` and `Extractor` in `internal/metrics`.
- Implement `astimate version` and `astimate config init`.
- Add `testdata/go/` fixture module with 4 packages: trivial, tested, untested-with-globals, high-fan-in.

*Accepts when:* `go build ./...` and `go test ./...` pass in CI; fixture module loads with `go/packages`.

### M1: Go extractor (v0 metrics)

- Loader: load a module with `go/packages` in `NeedTypes | NeedSyntax | NeedImports | NeedDeps` mode; resolve module path from `go.mod`.
- Imports: internal, external, stdlib classification; fan-out.
- Fan-in: module-wide reverse import graph.
- Globals and `init()` counting at top level only, non-test files only.
- Nesting depth and cognitive complexity (import gocognit as a library).
- Size: files, SLOC, largest file, exported symbols, function count.
- Tokens: byte-ratio estimate with configurable ratio; `--tokenizer=o200k` via tiktoken-go.
- Tests: test files, test functions, `has_tests`.

*Accepts when:* every fixture package's metrics match a golden JSON file; stdlib `errors` reports `globals=2`, `internal_imports=0`, `test_funcs>0`.

### M2: Scorer and CLI

- Weights config loader (YAML) with defaults embedded; validation that weights sum to 1.
- Saturating normalization and composite score.
- Tiers, drivers and generated suggestions.
- `astimate assess` with table and `--json` output.
- `astimate rank` over a module.
- Monotonicity property test.

*Accepts when:* section 7.4 invariants pass as tests; `astimate rank` on the fixture module orders high-fan-in above trivial.

### M3: MCP server

- `astimate serve` on the official go-sdk stdio transport.
- Tools `assess_package`, `rank_packages`, `explain_metric` with typed input structs and generated schemas.
- Integration test using the SDK's in-memory client transport covering discovery, list and call for both protocol eras.
- Install instructions for Claude Code and Cursor.

*Accepts when:* the integration test passes; Claude Code lists all three tools and `assess_package` returns a report on the fixture module.

### M4: Calibration harness

- Task corpus definition format and 30-package initial corpus.
- Runner that executes tasks against Claude Code and records the section 10 measurements.
- Fitting script and report generator.
- `weights_version` bump and `calibrated: true` flip.

*Accepts when:* a calibrated weights file exists with a published correlation table, and the 7.4 invariants still pass.

### M5: v1 metrics and second language

- `concrete_param_ratio`, `uses_cgo`, `uses_reflect`, `generated_files`, `coverage_pct`.
- TypeScript extractor behind tree-sitter with its own fixtures.
- Per-language weights.

*Accepts when:* TypeScript fixture metrics match golden files and `astimate rank` works on a mixed module.

## 13. Open questions

- Should fan-in count test-only importers? Proposed: no, report separately as `fan_in_tests`.
- Should `rank_packages` cache results between MCP calls within a session? Proposed: yes, keyed by module root and file mtimes.
- Is a per-file score useful in addition to per-package? Deferred until an agent asks for it.
- Which agent besides Claude Code should be a calibration target first?
