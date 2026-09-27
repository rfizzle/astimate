# Verification

Manual checks of integrations whose other side is not under this repository's tests. Each entry says what was checked, how, against which version and when, and what remains for a person to confirm. Append new entries; do not rewrite old ones.

## Claude Code Stop hook

### Contract source

The hook contract in [claude-code-hook.md](claude-code-hook.md) was taken from the Claude Code hooks reference as known to the agent that wrote the integration on 2026-09-27. The documentation was not fetched: the work ran without network access. The Claude Code installed on that machine reported `2.1.283 (Claude Code)`, but no Claude Code session was run against the hook. The contract relied on:

- Stop hooks receive JSON on stdin with `session_id`, `transcript_path`, `hook_event_name: "Stop"` and `stop_hook_active`.
- Hook stdout is parsed as JSON only on exit 0; `{"decision": "block", "reason": "..."}` keeps the agent working and shows it `reason`.
- Exit 2 is a blocking error whose stderr is shown to the agent; other non-zero codes are non-blocking.
- `stop_hook_active: true` means a Stop hook already blocked once; a hook should not block again.

**Re-check this against the live hooks reference on first use** and record the result below.

### Automated evidence (2026-09-27)

- `TestHookContract` (`cmd/astimate/hookcontract_test.go`): `astimate check --all --baseline <pristine> --format hook` on the degraded fixture exits 0 and prints exactly one JSON object with only the keys `decision` (`"block"`) and `reason`, which names `dup_blocks`, `untested_exports` and `globals`; on the pristine fixture it exits 0 and prints exactly `{}` and a newline.
- `TestStopHookSnippet` (same file): the command from the documented `settings.json` snippet, run through `sh -c` with a Stop hook input on stdin and `CLAUDE_PROJECT_DIR` set, in a git repository whose working tree degrades the fixture since `master`: prints `{}` on a clean tree, the block decision naming the three violations when `stop_hook_active` is `false`, and `{}` when it is `true`.
- `TestCheckHookStdin` (`cmd/astimate/checkcmd_test.go`): `check --format hook` on the degraded fixture prints `{}` without analysis when the hook input has `stop_hook_active: true`, and the block decision when it is `false`, absent (stdin a terminal) or malformed. Since the command reads `stop_hook_active` itself, `TestStopHookSnippet` runs a snippet with no `jq` guard.
- `TestPreCommitSnippet` (`cmd/astimate/precommit_test.go`): see the pre-commit entry below.

### Live check in Claude Code (to be filled in by a person)

Install the snippet in a scratch repository, have the agent add an untested exported function to a package, and let it try to finish.

| Field | Value |
| --- | --- |
| Claude Code version (`claude --version`) | _not yet run_ |
| Date | _not yet run_ |
| Hooks reference matches the contract above | _yes / no, with differences_ |
| Degraded change blocked, violation text shown to the agent | _yes / no_ |
| Agent allowed to stop on the second attempt (`stop_hook_active` read by `astimate check`) | _yes / no_ |
| Checked by | |

## Pre-commit hook

2026-09-27: `TestPreCommitSnippet` installs the script from [pre-commit.md](pre-commit.md), read from the document, as `.git/hooks/pre-commit` in a temporary repository with `git` from the test machine. The first commit is allowed with the skip notice (no `HEAD`, no merge-base); a commit adding the degraded fixture's `degraded.go` is refused with `check exited 3` and the `dup_blocks`, `untested_exports` and `globals` violations on stderr; after the file is removed, a commit changing only a comment in the same package succeeds. The pre-commit framework snippet in the same document was not exercised.

## GitHub Action release install

2026-09-27: `goreleaser release --snapshot --clean` (goreleaser v2.17.0) built `astimate_0.0.1-next_{linux,darwin}_{amd64,arm64}.tar.gz`, each with `astimate` at the archive root, and `checksums.txt` with `<sha256>  <file>` lines, the layout of the recorded release under `action/testdata/releases/`. `action/install.sh` installed the darwin_arm64 archive from a `file://` copy of that output with a passing checksum, and the binary reported `version: v0.0.1-next`. The `release-snapshot` job in `ci.yml` repeats the name and checksum checks on every pull request.

| Check | Status |
| --- | --- |
| Release install: `version: <tag>` in a workflow downloads and runs the published release | _pending the first `v*` tag_ |

## GitHub Action

2026-09-27: the `gate` job in `ci.yml` runs this repository's own action with `version: source` on every pull request. Locally, `astimate check . --base master --all` on an unchanged tree exits 0, and `action/run.sh` passes `check`'s exit code 3 and its `--format github` stdout through; `action/action_test.go` covers both scripts. No pull request had run the job when this was written, so the live behavior is the user's to record on the first one.

| Check | Status |
| --- | --- |
| `gate` job passes on a pull request that leaves every package unchanged or better | _pending the first pull request; record the run URL and date_ |
| A pull request that degrades a copy of the fixture fails the job with `::error` annotations on the diff lines | _pending the first pull request; record the run URL and date_ |

## CI run time

2026-09-27: the `check` job in `ci.yml` took 2m12s on the first push to `master` and 1m25s on the second, against a two-minute target for an unchanged tree. `actions/setup-go@v6` restores the Go module and build caches by default (`cache` defaults to `true` in its `action.yml`), keyed on `go.sum`, so both runs already had caching and the second run was the warm one. The `setup-go` steps in the `check`, `gate` and `release-snapshot` jobs now set `cache: true` and `cache-dependency-path: go.sum` explicitly. `golangci/golangci-lint-action@v8` caches its analysis results by default (`skip-cache: false`); no extra cache step was added. The run time after this change is to be observed on the next push.

| Check | Status |
| --- | --- |
| `check` job on an unchanged tree finishes in under two minutes with a warm cache | _pending the next push_ |

## Grammar subset build tags

2026-09-27: `gotreesitter` v0.55.1 embeds every grammar it ships unless built with `grammar_subset`, and then only those named by a `grammar_subset_<name>` tag (the names come from the `//go:build` lines in its `grammars` package). `make build`, `.goreleaser.yaml` and `action/install.sh` (the action's `version: source` path) build with `-tags 'grammar_subset grammar_subset_typescript grammar_subset_tsx'`; `make check` and `go build ./...` stay untagged. Sizes of `./cmd/astimate` with Go 1.27.1 on darwin/arm64, from `ls -l`, not stripped unless stated:

| Build | Size (bytes) |
| --- | --- |
| `go build`, no tags | 53,890,850 |
| `go build -tags '<subset tags>'` (`make build`) | 34,105,298 |
| `go build -trimpath -ldflags '-s -w'`, no tags (release flags) | 44,014,002 |
| `goreleaser build --snapshot --clean --single-target` (release flags plus the tags) | 25,397,154 |

`go version -m` on the goreleaser binary reports `-tags=grammar_subset,grammar_subset_typescript,grammar_subset_tsx`, and `astimate rank testdata/ts/fixture` from the tagged `make build` binary ranks the fixture's packages. A misspelled tag still compiles but leaves that grammar out: `go test -tags 'grammar_subset grammar_subset_typescrpt grammar_subset_tsx' ./internal/lang/typescript/...` fails in the conformance suite, and `make test-subset`, run by the `check` job in `ci.yml`, passes with the correct tags.

## actionlint in CI

2026-09-27: the `actionlint` step of the `check` job in `ci.yml` pins `ACTIONLINT_VERSION` (1.7.12) and `ACTIONLINT_SHA256`, the SHA-256 of the linux_amd64 archive. It downloads that release's `actionlint_<version>_checksums.txt` and fails before downloading the archive when the file's linux_amd64 hash differs from the pinned one, naming the version, both hashes and the file; the archive is then checked against the pinned hash before it is unpacked. The step body, run locally with `RUNNER_TEMP` set to a scratch directory, failed with `pinned ACTIONLINT_SHA256 0000…0000 does not match 8aca8db9…a3d8` for version 1.7.12 with a zeroed pin, with a `does not match 900919a8…b0a` message for version 1.7.11 with the 1.7.12 pin, and with the correct pin verified the archive (`OK`). To bump, change `ACTIONLINT_VERSION` and copy the linux_amd64 hash from the new release's checksums file into `ACTIONLINT_SHA256`. The workflow was linted with the actionlint 1.7.12 darwin_arm64 binary, checked against the same release's checksums file, with no findings; `shellcheck` was not installed, so the `run:` scripts were not shellchecked locally.
