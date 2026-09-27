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
- `TestPreCommitSnippet` (`cmd/astimate/precommit_test.go`): see the pre-commit entry below.

### Live check in Claude Code (to be filled in by a person)

Install the snippet in a scratch repository, have the agent add an untested exported function to a package, and let it try to finish.

| Field | Value |
| --- | --- |
| Claude Code version (`claude --version`) | _not yet run_ |
| Date | _not yet run_ |
| Hooks reference matches the contract above | _yes / no, with differences_ |
| Degraded change blocked, violation text shown to the agent | _yes / no_ |
| Agent allowed to stop on the second attempt (`stop_hook_active` guard) | _yes / no_ |
| Checked by | |

## Pre-commit hook

2026-09-27: `TestPreCommitSnippet` installs the script from [pre-commit.md](pre-commit.md), read from the document, as `.git/hooks/pre-commit` in a temporary repository with `git` from the test machine. The first commit is allowed with the skip notice (no `HEAD`, no merge-base); a commit adding the degraded fixture's `degraded.go` is refused with `check exited 3` and the `dup_blocks`, `untested_exports` and `globals` violations on stderr; after the file is removed, a commit changing only a comment in the same package succeeds. The pre-commit framework snippet in the same document was not exercised.

## GitHub Action release install

2026-09-27: `goreleaser release --snapshot --clean` (goreleaser v2.17.0) built `astimate_0.0.1-next_{linux,darwin}_{amd64,arm64}.tar.gz`, each with `astimate` at the archive root, and `checksums.txt` with `<sha256>  <file>` lines, the layout of the recorded release under `action/testdata/releases/`. `action/install.sh` installed the darwin_arm64 archive from a `file://` copy of that output with a passing checksum, and the binary reported `version: v0.0.1-next`. The `release-snapshot` job in `ci.yml` repeats the name and checksum checks on every pull request.

| Check | Status |
| --- | --- |
| Release install: `version: <tag>` in a workflow downloads and runs the published release | _pending the first `v*` tag_ |
