# Astimate

Astimate is a quality gate for LLM-written Go and TypeScript code. It measures how a change affects a package's maintainability, compares the result against a baseline and a set of thresholds, and fails when the package got worse. It also estimates how much effort a from-scratch rebuild of each package would take, in agent passes and human days, so you can see which packages have outgrown the "rewritable in one pass" bar.

The gate targets the ways LLM-written changes tend to degrade a package: copy-paste instead of extraction, exported behavior without tests, package-level state, and packages that grow past what one agent can hold in context. Rules that measure *how* code is written ratchet on the change, so legacy debt passes and new debt fails. Rules that measure *how much* code there is are absolute ceilings with a warning band, so a large well-written feature is not a regression.

## Status

Every command below, the GitHub Action, the Claude Code hook, the pre-commit hook and the MCP server are implemented and tested, and `v0.1.0` is released. The default thresholds are calibrated against a corpus of the standard library and 36 well-regarded Go modules (`SPEC.md` section 11, [the report](calibration/reports/thresholds-2026-09-28.md)); the rebuild estimate's parameters are not calibrated yet, and every estimate line says so (`estimate from uncalibrated parameters`); that label is about the estimate, not the gate. See `SPEC.md` for the full design and `AGENTS.md` for contribution rules.

Go and TypeScript are supported. The extractor interface and its conformance suite are language-agnostic; Go is analyzed with the standard toolchain, TypeScript with a pure-Go tree-sitter runtime, so neither needs cgo.

## What it measures

Per package, from non-test files unless noted:

| Metric | Meaning |
| --- | --- |
| `tokens_est` | Estimated tokens of source (generated files excluded, reported as `tokens_est_generated`), so the package can be judged against an agent's context budget |
| `dup_blocks`, `duplication_pct` | Repeated token sequences after normalizing identifiers and literals, and the share of lines they cover |
| `untested_exports` | Exported functions and methods no test file references |
| `exported_symbols`, `fan_in`, `internal_imports` | The contract other packages depend on and the coupling in both directions |
| `globals`, `init_funcs` | Hidden state and ordering no signature reveals |
| `max_nesting`, `cognitive_p90`, `changed_func_cognitive_max` | Structural complexity, including that of the most complex function changed since the baseline |
| `test_files`, `test_funcs`, `has_tests` | Whether the next agent has a feedback loop |

The full list, definitions and counting rules are in `SPEC.md` section 6.

`assess`, `rank` and `check` take `--coverage` (bounded by `--coverage-timeout`, default 2m) to run the tests with `go test -cover`, report `coverage_pct` and scale the rebuild estimate's untested-behavior term by it; it is off by default because it runs code, and it is reported, never gated.

## Requirements

- Go 1.27 or later to build astimate. The release binaries, once published, need nothing.
- A Go target: a `go.mod` whose packages type-check. No layout convention is required; the unit of analysis is the Go package as the toolchain sees it.
- A TypeScript target: a `package.json` at the module root. A package is a directory of `.ts`, `.tsx`, `.mts` or `.cts` files; `tsconfig.json` `paths` and `baseUrl` are followed, `node_modules` is not read (`SPEC.md` section 13.1).
- git, for `check` against a git ref (the default) and for `--staged`.

## Install

The module path is `github.com/rfizzle/astimate`:

```
go install github.com/rfizzle/astimate/cmd/astimate@latest
```

`@latest` resolves to the newest release tag, `v0.1.0` today; `@master` builds the newest commit. A `go install` build embeds every tree-sitter grammar the parser ships and is larger than `make build`'s, which embeds only the TypeScript ones; it behaves the same.

Each [GitHub release](https://github.com/rfizzle/astimate/releases) carries `astimate_<version>_<os>_<arch>.tar.gz` for `linux` and `darwin` on `amd64` and `arm64`, with `astimate` at the archive root, and a `checksums.txt` to verify it against (the layout the GitHub Action installs from; see below).

From a clone, `make build` writes `./astimate` with the version, commit and date injected.

## Build and test

```
make check           # actionlint, go mod tidy, gofmt, go vet, golangci-lint, go test -race, self-check, README sample check
make selfcheck       # build ./astimate and run its gate on this repository against master
make build           # ./astimate with version, commit and date injected
make readme-samples  # regenerate the sample outputs in this README
```

`golangci-lint` v2 is required for `make check`; `actionlint` is used when it is on `PATH`.

`make build` and the release builds pass `-tags` with the tags listed as `BUILD_TAGS` in the Makefile so only the TypeScript and TSX tree-sitter grammars are embedded; a plain `go build ./cmd/astimate` works too but embeds every grammar and is larger (`make test-subset` runs the TypeScript tests under the tags).

## Commands

Seven commands: `assess`, `rank`, `baseline write`, `check`, `serve`, `config init` and `version`. Logs go to stderr only. Exit codes are 0 for success or a passed gate, 1 for a usage error, 2 for an analysis failure (the module does not load, the base ref does not exist) and 3 for a failed gate (`check` only; `--format hook` exits 0 with a decision instead). When a violation and an analysis failure both occur, 3 wins. `astimate <command> -h` lists a command's flags.

The samples below are the real output for the fixture modules under `testdata/`, regenerated by `make readme-samples` and checked by `make check` and CI, so they never drift from the CLI. The fixture is deliberately small, so every package is `ONE_PASS`.

### assess

`astimate assess <package-dir>` scores one package: its rebuild estimate, the terms driving it, suggestions and every metric.

<!-- sample:assess -->
```
$ astimate assess testdata/go/fixture/dupes
package: dupes (example.com/fixture)
rebuild: 0.1 agent passes, 0.7 human days (estimate from uncalibrated parameters)
tier: ONE_PASS

drivers:
  unspecified  2400 tokens  untested_exports=3
  contract     160 tokens   exported_symbols=4 fan_in=0

suggestions:
  - 3 exported functions have no test (CountVisits, SumOrders, TallyScores); a rebuild would have to reverse-engineer their behavior.

metrics:
  files                  1      sloc                  67
  largest_file_sloc      67     tokens_est            506
  tokens_est_with_tests  561    internal_imports      0
  external_imports       0      stdlib_imports        0
  fan_in                 0      fan_in_tests          0
  exported_symbols       4      globals               0
  init_funcs             0      max_nesting           2
  cognitive_total        19     cognitive_p90         6
  func_count             4      dup_blocks            1
  duplication_pct        80.6   test_files            1
  test_funcs             1      has_tests             true
  untested_exports       3      dup_blocks_cross_pkg  0
  uses_cgo               false  uses_reflect          false
  generated_files        0      tokens_est_generated  0
```
<!-- /sample:assess -->

`--json` prints the report `check --format json` and the MCP tools use (`SPEC.md` section 10.2):

<!-- sample:assess-json -->
```
$ astimate assess testdata/go/fixture/dupes --json | head -25
{
  "language": "go",
  "package_path": "dupes",
  "module_path": "example.com/fixture",
  "rebuild": {
    "agent_passes": 0.1,
    "rebuild_tokens": 2713,
    "human_days": 0.7,
    "tier": "ONE_PASS",
    "calibrated": false,
    "drivers": [
      {
        "term": "unspecified",
        "tokens": 2400,
        "detail": "untested_exports=3"
      },
      {
        "term": "contract",
        "tokens": 160,
        "detail": "exported_symbols=4 fan_in=0"
      }
    ]
  },
  "suggestions": [
    "3 exported functions have no test (CountVisits, SumOrders, TallyScores); a rebuild would have to reverse-engineer their behavior."
...
```
<!-- /sample:assess-json -->

### rank

`astimate rank [<module-root>]` ranks every package of the module containing the directory (default the current one) by rebuild effort, largest first. `--sort passes|days|fan_in|tokens|duplication` changes the key, `--top N` keeps the first N rows and `--json` prints an array.

<!-- sample:rank -->
```
$ astimate rank testdata/go/fixture
PATH     PASSES  DAYS  TIER      FAN_IN  TOKENS  DUP%
a           0.1   1.0  ONE_PASS       0     134   0.0
b           0.1   1.0  ONE_PASS       0     154   0.0
dupes       0.1   0.7  ONE_PASS       0     506  80.6
hidden      0.1   1.9  ONE_PASS       0     226   0.0
hub         0.1   1.1  ONE_PASS       4     177   0.0
tested      0.0   1.0  ONE_PASS       0     237   0.0
trivial     0.0   0.2  ONE_PASS       0      47   0.0
```
<!-- /sample:rank -->

A TypeScript module ranks the same way; the language is detected from the module root (`go.mod` or `package.json`):

<!-- sample:rank-ts -->
```
$ astimate rank testdata/ts/fixture
PATH     PASSES  DAYS  TIER      FAN_IN  TOKENS  DUP%
b           0.1   1.0  ONE_PASS       0     344   0.0
hidden      0.1   1.3  ONE_PASS       0     210   0.0
hub         0.1   1.7  ONE_PASS       4     232   0.0
trivial     0.1   1.0  ONE_PASS       1     214   0.0
a           0.0   0.2  ONE_PASS       0      48   0.0
dupes       0.0   0.5  ONE_PASS       0     527  81.8
tested      0.0   0.9  ONE_PASS       0     216   0.0
```
<!-- /sample:rank-ts -->

### baseline write

`astimate baseline write [<module-root>]` writes every package's metrics to `.astimate/baseline.json` (or `--out`) for a repository that prefers an explicit, reviewable baseline to a git ref, or that runs outside git. Commit the file and pass it to `check --baseline`.

<!-- sample:baseline-write -->
```
$ astimate baseline write testdata/go/fixture --out /tmp/fixture-baseline.json
wrote /tmp/fixture-baseline.json (7 packages)
```
<!-- /sample:baseline-write -->

### check

`astimate check [<module-root>]` is the gate. It compares the packages changed since the baseline, or every package with `--all`, against the thresholds and exits 3 when any has a violation. The baseline is the merge-base of `HEAD` and `--base <ref>` (default `origin/master`, then `master`, `origin/main`, `main`), checked out into a temporary worktree, or a file written by `baseline write` with `--baseline <file>`. `--staged` judges the git index instead of the working tree, as a commit would record it. `--format` is `text` (default), `json`, `hook` (Claude Code Stop hook) or `github` (workflow annotations).

`testdata/go/fixture-degraded` is the fixture after an agent copied a function into package `tested`, added an untested export and a package variable, and grew a function's branching. Checked against a baseline of the pristine fixture:

<!-- sample:check -->
```
$ astimate check testdata/go/fixture-degraded --baseline /tmp/fixture-baseline.json --all
violations:
  tested
    changed_func_cognitive_max: 51 (changed since baseline), max 50. Changed function grade (grade.go:8) has cognitive complexity 51; split it into smaller functions or flatten its branching.
    dup_blocks: 0 -> 1, max_delta +0. 1 duplicate block covers 12.2% of lines; extract shared helpers, starting with degraded.go:15-20.
    duplication_pct: 0 -> 12.2, max_delta +6. 1 duplicate block covers 12.2% of lines; extract shared helpers, starting with degraded.go:15-20.
    globals: 0 -> 1, max_delta +0. 1 package-level variable holds state no signature reveals (joins); pass it explicitly or move it into a struct.
    untested_exports: 0 -> 1, max_delta +0. 1 exported function has no test (JoinAgain); a rebuild would have to reverse-engineer its behavior.
<module>: dup_blocks_cross_pkg 1, 0 violations, 0 warnings, 0 exempted
a: 0 violations, 0 warnings, 0 exempted
b: 0 violations, 0 warnings, 0 exempted
dupes: 0 violations, 0 warnings, 0 exempted
hidden: 0 violations, 0 warnings, 0 exempted
hub: 0 violations, 0 warnings, 0 exempted
tested: 5 violations, 0 warnings, 0 exempted
trivial: 0 violations, 0 warnings, 0 exempted
$ echo $?
3
```
<!-- /sample:check -->

Each finding reads `metric: baseline -> head, limit. suggestion`. [docs/reading-violations.md](docs/reading-violations.md) explains them and the order to fix them in. The `<module>` row carries module-wide metrics, today `dup_blocks_cross_pkg`, the code copied between packages; it is reported first, and the default rule fails any new cross-package copy (`max_delta: 0`; a module with no baseline is held to `max: 450`, the corpus p90).

#### Gate semantics

Density rules measure how the code is written (duplicate blocks, untested exports, globals, nesting, complexity), so adding features should never raise them: they ratchet on the change with `max_delta`, usually 0, against the baseline, and a `max` on one limits what a change may introduce, never a legacy value the change left alone. Capacity rules measure how much code there is (`tokens_est`, `sloc`, `largest_file_sloc`, `exported_symbols`, `internal_imports`), which features are supposed to grow, so they carry no delta: an absolute `max` says the package has outgrown one agent pass and needs a split, and a warning from `warn_at` (0.75 of the ceiling) gives notice before it lands. A package new at head faces every `max`, and the count-of-things-added rules (`dup_blocks`, `untested_exports`, `globals`, `init_funcs`) ratchet from zero. One requirement completes the defaults: a package over 100 `sloc` must have tests (`has_tests`), judged like a density `max`, so only when the package is new or grew. Warnings never fail the gate. `SPEC.md` section 8 has the full rules and the default thresholds.

| Rule | Kind | Baseline | Head | Verdict |
| --- | --- | --- | --- | --- |
| `dup_blocks` `max_delta: 0` | density | 0 | 1 | violation: one copied block fails however small the change |
| `tokens_est` `max: 16000`, `warn_at: 0.75` | capacity | 6,000 | 12,800 | passes with a warning: at 80% of the ceiling, plan a split before the next feature |

### serve

`astimate serve` starts the Model Context Protocol server on stdio for an agent to call while it works. It offers four tools: `check_package` (the gate for one package, the agent's self-check before it finishes), `assess_package`, `rank_packages`, and `explain_metric` (a metric's definition, evidence and default threshold). Stdout carries protocol messages only and logs go to stderr. Tools read only paths inside the directory the server was started in unless `--allow-any-path` is given; `--config` applies to every tool. See [MCP server](#mcp-server) for the client entry.

### config init

`astimate config init` writes the default configuration to `astimate.yaml` (or `--out`, refusing to overwrite without `--force`) with a comment on every setting:

<!-- sample:config-init -->
```
$ astimate config init
wrote astimate.yaml
$ head -15 astimate.yaml
# Astimate configuration: rebuild parameters and gate thresholds in one file.
#
# The gate thresholds are calibrated (SPEC.md sections 8.2 and 11.1): fitted
# by calibration/fit from the reference corpus in calibration/corpus.md, with
# the evidence in calibration/reports/thresholds-2026-09-28.md. The rebuild
# parameters are uncalibrated placeholders (SPEC.md section 7) until the
# rebuild experiments in SPEC.md section 11.2 have run. TypeScript packages
# are judged by the typescript override at the end of this file, fitted from
# a TypeScript corpus (SPEC.md section 13).

# Identifies the defaults this file was generated from.
config_version: thresholds-2026-09-28

# Bytes of source per estimated token; tokens_est = bytes / chars_per_token.
chars_per_token: 3.2
...
```
<!-- /sample:config-init -->

### version

<!-- sample:version -->
```
$ astimate version
version: <version>
commit: <commit>
date: <date>
```
<!-- /sample:version -->

`make build` injects the three values; a `go install` build reports its module version and VCS data.

## Configuration

One file, `astimate.yaml`, holds the rebuild-estimate parameters and the gate thresholds. `astimate config init` writes the defaults with a comment on every line. Resolution order is `--config` (`--thresholds` is an alias on `check`), then `./astimate.yaml`, then the embedded default. The top-level keys `config_version`, `chars_per_token`, `rebuild` and `thresholds` are required: a missing one is an error, not filled from the default, so start from the file `config init` writes. Sections that group optional tuning, today `duplication` (`min_tokens`, `ignore_literal_only`, `fold_signs`, `split_literal_runs`), may be partial or absent, and absent keys take the embedded defaults. An optional `languages:` section, keyed by language id (`go` or `typescript`), overrides the configuration for one language: its `rebuild:` sets only the parameters it names, the rest coming from the top-level `rebuild`; its `thresholds:` rules replace the top-level rule on the same metric or add one, and `- metric: <name>` with `disabled: true` drops that metric's rules for the language. Everything else is shared. Reports judged with an override show `config_version` suffixed with `+<language>`. The embedded default ships a `typescript` override, fitted on a corpus of 20 TypeScript repositories (`calibration/reports/thresholds-2026-09-28-typescript.md`), so TypeScript packages are judged by `thresholds-2026-09-28+typescript`: larger size limits (`sloc` 2,500, `tokens_est` 35,000, `largest_file_sloc` 900) and slightly looser complexity limits than Go's. `SPEC.md` section 9 has the full rules.

The default thresholds (`config_version: thresholds-2026-09-28`) are the rounded 90th percentiles of a reference corpus of the standard library and 36 well-regarded Go modules, fitted by `calibration/fit` (`SPEC.md` section 11.1), except `changed_func_cognitive_max`, which is the rounded 99th percentile of the corpus's per-function cognitive complexity (50); [calibration/reports/thresholds-2026-09-28.md](calibration/reports/thresholds-2026-09-28.md) has the distributions and a before and after table, and `configs/uncalibrated.yaml` keeps the earlier placeholders. The zero-tolerance ratchets stay at 0 by policy. Measured on 688 labeled agent-authored commits from this repository and three public ones (`SPEC.md` section 11.4, [the report](calibration/reports/gate-validation-2026-09-28.md)), the default gate fails 87.8% of the changes labeled `block` (target at least 80%) and 51.5% of those labeled `allow` (target at most 10%), so it does not yet meet its false-failure target. The rebuild parameters are still uncalibrated, and every estimate line says so; `SPEC.md` section 11.2 is the experiment that measures them.

## Integrations

- [Claude Code Stop hook](docs/claude-code-hook.md): a `settings.json` snippet that keeps an agent working while `astimate check --format hook` reports violations.
- [Pre-commit](docs/pre-commit.md): a git hook script and a pre-commit framework entry that refuse a commit that makes a package worse, and their limitations.
- [Reading violations](docs/reading-violations.md): how to read `check` output and which metrics to fix first; worth pointing an agent at.
- [Verification](docs/verification.md): what was checked of each integration's other side, and what is left for a person to confirm.

### Claude Code Stop hook

In `.claude/settings.json` (or `.claude/settings.local.json`), merged into any existing `hooks` object. The agent is blocked from finishing while the gate fails and sees the violations as the reason; the hook blocks at most once per stop, so it cannot loop. Use your default branch for `--base`. [docs/claude-code-hook.md](docs/claude-code-hook.md) covers the contract, loop protection and failure modes.

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "astimate check \"$CLAUDE_PROJECT_DIR\" --format hook --base master",
            "timeout": 300
          }
        ]
      }
    ]
  }
}
```

### Pre-commit

The line at the heart of the git hook in [docs/pre-commit.md](docs/pre-commit.md), which also skips the first commit (it has no merge-base) and has a pre-commit framework entry. `--staged` judges what the commit records, not the working tree:

```sh
astimate check . --base master --staged
```

### MCP server

Register `astimate serve` with Claude Code from the repository root:

```
claude mcp add astimate -- astimate serve
```

or commit it for the team in `.mcp.json` at the repository root:

```json
{
  "mcpServers": {
    "astimate": {
      "command": "astimate",
      "args": ["serve"]
    }
  }
}
```

Then ask the agent to call `check_package` on the package it changed before it finishes. The result opens with PASSED or FAILED and what to do next; a gate failure is a result with `passed: false`, not a tool error.

### GitHub Action

The composite action in [`action/`](action/action.yml) installs astimate and runs `astimate check --format github`. Violations become `::error` annotations and warnings `::warning` annotations, each on the file and line that caused it where the extractor can say (a duplicate block, an untested export's or a global's declaration, the complex function, the largest file for size rules, else the package's `doc.go` or first file), so they land on the pull request's diff; paths are relative to the repository root even for a module below it. Exit 3 (gate failed) and exit 2 (analysis failed) both fail the job.

```yaml
on: pull_request

jobs:
  astimate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
        with:
          fetch-depth: 0   # the merge-base with the base branch must be in the clone
      - uses: rfizzle/astimate/action@master
        with:
          version: v0.1.0
          base: origin/${{ github.base_ref }}
```

| Input | Default | Meaning |
| --- | --- | --- |
| `version` | `latest` | A release tag such as `v0.3.0`, `latest`, or `source` to build the repository containing the action with the Go on `PATH` (add `actions/setup-go` first). |
| `base` | `origin/master` | Ref whose merge-base with `HEAD` is the baseline. Empty uses astimate's default ref. |
| `config` | empty | Path to `astimate.yaml`; empty uses `./astimate.yaml`, then the embedded default. |
| `all` | `false` | `true` checks every package, not only those changed since the merge-base. |
| `path` | `.` | Module root to check, relative to the repository root, for a module below it such as `services/api`. Annotation paths stay relative to the repository root. |
| `format` | `github` | `check --format` value. |

`fetch-depth: 0` is required: with the default shallow clone the merge-base does not exist and the action stops with exit 2 (`base ref ... not found`) or warns that the clone is shallow. For a `pull_request` event the checkout is the merge commit, so the packages checked are those the pull request changes. If `origin/<base>` might be missing, fetch it first with `git fetch --no-tags origin "+refs/heads/<base>:refs/remotes/origin/<base>"`, as this repository's `gate` job in [`ci.yml`](.github/workflows/ci.yml) does.

A release install downloads `astimate_<version>_<os>_<arch>.tar.gz` (`<version>` without the leading `v`, `<os>` `linux` or `darwin`, `<arch>` `amd64` or `arm64`) and `checksums.txt` from the GitHub release and refuses to install when the archive's SHA-256 does not match its line in `checksums.txt`. Linux and macOS runners are supported. Releases are built by goreleaser from [`.goreleaser.yaml`](.goreleaser.yaml) when a `v*` tag is pushed. The first release is `v0.1.0`; `version: latest` follows the newest one. `version: source` builds the checked-out repository with `actions/setup-go`, for testing the action itself; this repository's own `selfcheck` job uses it, since the root `astimate.yaml` carries config keys a released binary may not know yet.

This repository gates itself with its own action. The `selfcheck` job in [`ci.yml`](.github/workflows/ci.yml) builds the action's astimate from source (`version: source`, `path: .`) and runs `astimate check --format github`: on a pull request against the packages it changes since the merge-base with `origin/<base>`, and on a push to `master` with `--all`, against the commit `master` pointed at before the push. Exit 3 or 2 fails the job; the legacy packages over a capacity ceiling only warn, the same policy every user gets. `make check` runs the same gate locally as `make selfcheck` (`--all --base master`).

## Layout

```
cmd/astimate/                   CLI entrypoint
internal/engine/                Operations shared by the CLI and the MCP server
internal/metrics/               RawMetrics and the Extractor interface (language-agnostic)
internal/metrics/metricstest/   Conformance suite every extractor runs, plus a fake extractor
internal/lang/golang/           Go extractor
internal/lang/typescript/       TypeScript extractor (tree-sitter, no cgo)
internal/lang/duptok/           Duplicate-block finder shared by both extractors
internal/config/                Embedded default config and loader
internal/score/                 Rebuild estimate, tiers, drivers, suggestions
internal/gate/                  Thresholds and evaluation
internal/baseline/              Git-ref and file baselines
internal/report/                Output formats
internal/mcpserver/             MCP server
internal/invariants/            Rebuild and gate invariants every configuration must pass
action/                         Composite GitHub Action and its install and run scripts
calibration/                    Reference corpus, metric collector, threshold fit and reports
configs/                        Alternative configurations: the uncalibrated placeholder defaults
docs/                           Integration guides and verification records
scripts/                        README sample generator
testdata/                       Fixture modules with hand-verified golden metrics
```

## License

[MIT](LICENSE).
