# Astimate

Astimate is a quality gate for LLM-written Go code. It measures how a change affects a package's maintainability, compares the result against a baseline and a set of thresholds, and fails when the package got worse. It also estimates how much effort a from-scratch rebuild of each package would take, in agent passes and human days, so you can see which packages have outgrown the "rewritable in one pass" bar.

The gate targets the ways LLM-written changes tend to degrade a package: copy-paste instead of extraction, exported behavior without tests, package-level state, and packages that grow past what one agent can hold in context. Rules that measure *how* code is written ratchet on the change, so legacy debt passes and new debt fails. Rules that measure *how much* code there is are absolute ceilings with a warning band, so a large well-written feature is not a regression.

## Status

Pre-release. The Go extractor, the rebuild estimate, the config loader and the threshold evaluator are implemented and tested. The `assess`, `rank`, `baseline` and `check` commands, the GitHub Action, the Claude Code hook and the MCP server are in progress. See `SPEC.md` for the full design and `AGENTS.md` for contribution rules.

Go is the first supported language. The extractor interface and its conformance suite are language-agnostic, and a TypeScript extractor is planned.

## What it measures

Per package, from non-test files unless noted:

| Metric | Meaning |
| --- | --- |
| `tokens_est` | Estimated tokens of source, so the package can be judged against an agent's context budget |
| `dup_blocks`, `duplication_pct` | Repeated token sequences after normalizing identifiers and literals, and the share of lines they cover |
| `untested_exports` | Exported functions and methods no test file references |
| `exported_symbols`, `fan_in`, `internal_imports` | The contract other packages depend on and the coupling in both directions |
| `globals`, `init_funcs` | Hidden state and ordering no signature reveals |
| `max_nesting`, `cognitive_p90`, `changed_func_cognitive_max` | Structural complexity, including that of the most complex function changed since the baseline |
| `test_files`, `test_funcs`, `has_tests` | Whether the next agent has a feedback loop |

The full list, definitions and counting rules are in `SPEC.md` section 6.

## Requirements

- Go 1.27 or later
- A target project with a `go.mod` that type-checks. No layout convention is required; the unit of analysis is the Go package as the toolchain sees it.

## Build and test

```
make check      # gofmt, go vet, golangci-lint, go test -race ./...
make build      # ./astimate with version, commit and date injected
./astimate version
./astimate config init   # writes astimate.yaml with the default thresholds and rebuild parameters, commented
```

`golangci-lint` v2 is required for `make check`.

`make build` and the release builds pass `-tags` with the tags listed as `BUILD_TAGS` in the Makefile so only the TypeScript and TSX tree-sitter grammars are embedded; a plain `go build ./cmd/astimate` works too but embeds every grammar and is larger (`make test-subset` runs the TypeScript tests under the tags).

## Configuration

One file, `astimate.yaml`, holds the rebuild-estimate parameters and the gate thresholds. `astimate config init` writes the defaults with a comment on every line. A user config must be complete; there is no merging onto the defaults. Resolution order is `--config`, then `./astimate.yaml`, then the embedded default. An optional `languages:` section overrides rebuild parameters and thresholds for one language (`go` or `typescript`), and reports for that language show `config_version` suffixed with `+<language>`.

The default thresholds are placeholders until they are calibrated against a corpus of well-regarded Go modules (`SPEC.md` section 11), and every report says so.

## Integrations

- [Claude Code Stop hook](docs/claude-code-hook.md): a `settings.json` snippet that keeps an agent working while `astimate check --format hook` reports violations.
- [Pre-commit](docs/pre-commit.md): a git hook script and a pre-commit framework entry that refuse a commit that makes a package worse, and their limitations.
- [Reading violations](docs/reading-violations.md): how to read `check` output and which metrics to fix first; worth pointing an agent at.

### GitHub Action

The composite action in [`action/`](action/action.yml) installs astimate and runs `astimate check --format github`. Violations become `::error` annotations and warnings `::warning` annotations, each on the package directory. Exit 3 (gate failed) and exit 2 (analysis failed) both fail the job.

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
          version: v0.3.0
          base: origin/${{ github.base_ref }}
```

| Input | Default | Meaning |
| --- | --- | --- |
| `version` | `latest` | A release tag such as `v0.3.0`, `latest`, or `source` to build the repository containing the action with the Go on `PATH` (add `actions/setup-go` first). |
| `base` | `origin/master` | Ref whose merge-base with `HEAD` is the baseline. Empty uses astimate's default ref. |
| `config` | empty | Path to `astimate.yaml`; empty uses `./astimate.yaml`, then the embedded default. |
| `all` | `false` | `true` checks every package, not only those changed since the merge-base. |
| `format` | `github` | `check --format` value. |

`fetch-depth: 0` is required: with the default shallow clone the merge-base does not exist and the action stops with exit 2 (`base ref ... not found`) or warns that the clone is shallow. For a `pull_request` event the checkout is the merge commit, so the packages checked are those the pull request changes. If `origin/<base>` might be missing, fetch it first with `git fetch --no-tags origin "+refs/heads/<base>:refs/remotes/origin/<base>"`, as this repository's `gate` job in [`ci.yml`](.github/workflows/ci.yml) does.

A release install downloads `astimate_<version>_<os>_<arch>.tar.gz` (`<version>` without the leading `v`, `<os>` `linux` or `darwin`, `<arch>` `amd64` or `arm64`) and `checksums.txt` from the GitHub release and refuses to install when the archive's SHA-256 does not match its line in `checksums.txt`. Linux and macOS runners are supported. Releases are built by goreleaser from [`.goreleaser.yaml`](.goreleaser.yaml) when a `v*` tag is pushed. No release has been published yet, so `version: latest` fails until one is; this repository's CI uses `version: source`.

## Layout

```
cmd/astimate/           CLI entrypoint
internal/metrics/       RawMetrics and the Extractor interface (language-agnostic)
internal/metrics/metricstest/   Conformance suite every extractor runs, plus a fake extractor
internal/lang/golang/   Go extractor
internal/config/        Embedded default config and loader
internal/score/         Rebuild estimate, tiers, drivers, suggestions
internal/gate/          Thresholds and evaluation
internal/baseline/      Git-ref and file baselines (in progress)
internal/report/        Output formats (in progress)
internal/mcpserver/     MCP server (in progress)
testdata/               Fixture modules with hand-verified golden metrics
```

## License

To be decided before the first release.
