# AGENTS.md

Guidance for any coding agent or human working in this repository. Read `SPEC.md` before writing code. Stories live in `.backlog.md` (untracked; local working file).

## What this project is

Astimate is a Go static-analysis tool used as a quality gate on LLM-written changes. It extracts package metrics, compares them against a baseline and thresholds, and fails when a package got worse. It also estimates rebuild effort and ranks packages. It runs as a CLI, a CI step, a Claude Code hook and a Model Context Protocol (MCP) server. The gate and the rebuild estimate are language-agnostic; extractors are per-language. Go is the first language. See `SPEC.md` for metrics, gate semantics, interfaces and milestones.

## Workflow

1. Pick a story from `.backlog.md`. Respect its `Depends on` line.
2. Branch from `master` as `s-NNN-short-slug` (for example `s-007-fan-in`).
3. Implement tasks in order. Every acceptance criterion must be demonstrably met, and every listed test must exist and pass, before the story is done.
4. Run the full check before committing: `gofmt -l .`, `go vet ./...`, `golangci-lint run`, `go test -race ./...`.
5. If implementation forces a deviation from `SPEC.md`, update `SPEC.md` in the same change and say why in the commit body.
6. Mark the story `Status: done` in `.backlog.md` when merged.

## Commits: Conventional Commits

Format: `<type>(<scope>): <subject>`

- Types: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `build`, `ci`, `chore`.
- Scopes match the story tags: `metrics`, `lang/go`, `lang/ts`, `score`, `gate`, `baseline`, `cli`, `mcp`, `report`, `integrations`, `calibration`, `testdata`, `infra`, `docs`.
- Subject: imperative mood, lower case, no trailing period, at most 72 characters.
- Body: explain why, not what. Do not reference backlog story ids; the backlog is a local file and the ids mean nothing in git history.
- Breaking changes to the report schema or CLI flags get a `BREAKING CHANGE:` footer.
- One logical change per commit. Never use `--no-verify`.

Example:

```
feat(lang/go): compute fan-in from module-wide reverse import graph

Fan-in is the blast-radius signal with the strongest evidence in SPEC.md
section 4. Built once per module load and cached, not per package.
```

## Go standards

Target Go 1.27 or later. Follow Effective Go and the Go Code Review Comments wiki. Specifically:

- `gofmt` and `goimports` clean. Zero `go vet` and `golangci-lint` findings.
- Doc comments on every exported identifier, starting with the identifier's name.
- Errors: wrap with `fmt.Errorf("loading %s: %w", path, err)`. Add context at each layer, never log and return. Sentinel errors are `var ErrX = errors.New(...)`; typed errors when callers need fields.
- No `panic` in library code. `cmd/` may exit on fatal errors via `os.Exit` after printing to stderr.
- `context.Context` is the first parameter of anything that does I/O or may be long-running.
- Accept interfaces, return structs. Keep interfaces small and define them where they are consumed.
- No package-level mutable state and no `init()` functions. This tool penalizes both; we do not ship them.
- Table-driven tests with `t.Run` subtests. Use `t.TempDir()` for filesystem tests. Golden files under `testdata/`, regenerated with `-update` flag.
- An interface with more than one implementation (or a planned second one) gets a conformance suite in an importable `<pkg>test` package next to it, in the style of `testing/fstest`; each implementation's own tests call it once and keep only implementation-specific tests locally. A package with one implementation and no external implementers uses ordinary local tests. See SPEC.md section 5 and the `go-conformance-suite` skill.
- Package names are short, lower case, no underscores. No `util`, `common` or `helpers` packages.
- Prefer the standard library. A new dependency needs a one-line justification in the commit body.

## Efficient code

Performance matters because agents call this tool interactively and `rank` runs over whole modules.

- Load a module with `go/packages` exactly once per invocation and share the result across all packages. Never reload per package.
- Compute all AST-derived metrics in a single `ast.Inspect` pass per file. Do not walk the tree once per metric.
- Build the reverse import graph once per module and cache it in `ModuleContext`.
- Preallocate slices and maps when the size is known. Avoid `fmt.Sprintf` in loops; use `strconv` or `strings.Builder`.
- No reflection in hot paths. No regex where `strings` functions suffice.
- Add a benchmark for any function that runs per file or per AST node. Include `benchstat` output in the PR when changing one.
- Bound memory: stream file bytes for size counting rather than reading whole files when the AST is not needed.

## Architecture boundaries

- `internal/metrics` defines `RawMetrics` and `Extractor`. It imports nothing language-specific.
- `internal/lang/<lang>` implements one extractor. Language-specific code lives only here.
- `internal/score` and `internal/gate` depend on `internal/metrics` only. Neither may import any `internal/lang` package.
- `internal/config` is the composition point: it imports `metrics`, `score` and `gate`, parses YAML, and produces `score.RebuildParams` and `[]gate.Threshold`. Those types and their `Validate` methods live in `score` and `gate`, which never import `config`.
- `internal/baseline` may call `git` and the extractor registry, and nothing in `score` or `gate` may call `git`.
- `internal/report` shapes output, including the hook and GitHub formats. `internal/mcpserver` and `cmd/astimate` depend on `report`, `gate`, `baseline` and `score`, never on `lang` directly except to register extractors.
- Go analysis uses the standard toolchain (`go/packages`, `go/types`, `go/ast`). Never tree-sitter for Go.

## MCP server rules

- Built on `github.com/modelcontextprotocol/go-sdk`. No hand-rolled JSON-RPC.
- Stdout carries protocol messages only. All logging goes to stderr via `log/slog`.
- Tool failures return a result with `isError: true` and a readable message, not a JSON-RPC error.
- Every tool returns both `content` (text) and `structuredContent` (JSON).
- No network calls in the default path. Exact tokenizers and coverage are opt-in flags.

## Definition of done

A story is done when all of these are true:

- [ ] Every task in the story is implemented.
- [ ] Every acceptance criterion is checked and demonstrable.
- [ ] Every test listed in the story exists and passes under `go test -race ./...`.
- [ ] `gofmt`, `go vet` and `golangci-lint` are clean.
- [ ] Exported identifiers have doc comments.
- [ ] `SPEC.md` still describes the behavior, or was updated in the same change.
- [ ] Commits follow the Conventional Commits rules above.

## Do not

- Do not add rebuild parameters or thresholds that are not in `SPEC.md` sections 7 and 8 without updating the spec.
- Do not print anything to stdout in `serve` mode except protocol output.
- Do not reintroduce hard caps in normalization; use the saturating curve.
- Do not count test files toward any metric except the test metrics and `tokens_est_with_tests`.
- Do not commit `.backlog.md`, build artifacts or coverage files.
