# Claude Code Stop hook

A Claude Code Stop hook runs when the agent is about to finish its turn. Wired to `astimate check --format hook`, it keeps the agent working while the gate fails and hands it the violations as the reason, so the agent fixes them before it stops.

## The contract

The contract below is the Claude Code hooks reference as known when this integration was written. [verification.md](verification.md) records what was checked and has the line to fill in once you confirm it against the live documentation.

- Claude Code sends the hook a JSON object on stdin with `session_id`, `transcript_path`, `hook_event_name` (`"Stop"`) and `stop_hook_active`.
- Claude Code reads the hook's stdout as JSON only when the hook exits 0. `{"decision": "block", "reason": "..."}` prevents the stop and shows `reason` to the agent as its next instruction. `{}`, or no decision at all, lets the agent stop.
- Exit code 2 is a blocking error: the stop is prevented and the hook's stderr is shown to the agent.
- Any other non-zero exit code is a non-blocking error: the agent stops and stderr is shown to the user only.
- `stop_hook_active` is `true` when the agent is already continuing because a Stop hook blocked it. A hook should not block again in that case, or the agent can loop indefinitely.

`astimate check --format hook` follows all of them (SPEC.md 8.5):

| Outcome | stdout | Exit code | What Claude Code does |
| --- | --- | --- | --- |
| A package has a violation | `{"decision":"block","reason":"violations:\n  ..."}` | 0 | Blocks the stop; the agent sees the violations and warnings |
| No violation | `{}` (warnings, if any, go to stderr) | 0 | Lets the agent stop |
| Analysis failed (the module does not type-check, the base ref does not exist) | empty; the error is on stderr | 2 | Blocks the stop; the agent sees the error |
| The hook input has `stop_hook_active: true` | `{}`, without running the check | 0 | Lets the agent stop |

In the hook format, and only when stdin is not a terminal, `astimate check` reads the hook input from stdin and applies the loop rule itself, so the snippet below needs no shell guard.

## settings.json snippet

Paste this into `.claude/settings.json` in the repository (shared with the team) or `.claude/settings.local.json` (yours only). Merge it into an existing `hooks` object rather than replacing one.

<!-- stop-hook-snippet:begin -->
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
<!-- stop-hook-snippet:end -->

`cmd/astimate/hookcontract_test.go` runs this exact command, read from this file, against a temporary repository, so an edit here that breaks it fails the tests.

### Loop protection

Claude Code sets `stop_hook_active` to `true` in the hook input when the agent is already continuing because a Stop hook blocked it. `astimate check --format hook` reads that input from stdin and, when the flag is `true`, prints `{}` without running the check and logs on stderr that the hook already blocked once, so the agent may stop: it has had its chance to fix the violations. Missing, empty or malformed input is ignored and the check runs as usual, so an unexpected input costs the loop protection, never the gate. Stdin is read only in the hook format and only when it is not a terminal, so running the same command by hand never waits for input.

The consequence is one blocking round per stop. If the agent's fix still fails the gate, it stops anyway, and the violations remain visible the next time you run `astimate check`. That is deliberate: an unbounded loop spends tokens without a human noticing. If you want the gate to keep blocking, redirect the command's stdin from `/dev/null` (append `< /dev/null` to it) and rely on your own supervision; do so knowingly.

### Choosing `--base`

`--base master` compares the working tree against the merge-base of `HEAD` and `master`, so the agent is judged on everything the branch changed, committed or not. Use your default branch name: `main`, or `origin/main` if the local branch is often stale. Omitting `--base` tries `origin/master`, `master`, `origin/main` and `main` in that order, which is fine when exactly one of them exists. If the repository keeps a reviewed baseline file instead, replace `--base master` with `--baseline .astimate/baseline.json`.

`"$CLAUDE_PROJECT_DIR"` is the project directory Claude Code was started in. `astimate` finds the `go.mod` at or above it; if the module lives in a subdirectory, name it, for example `"$CLAUDE_PROJECT_DIR/service"`.

The `timeout` is in seconds. A check loads the module twice (head and the baseline worktree); raise it on a large module.

## What the agent sees

When a change adds a copied function, an untested export and a package variable to package `tested` (the repository's degraded fixture), the agent receives this as the reason to continue:

```
violations:
  tested
    dup_blocks: 0 -> 1, max_delta +0. 1 duplicate block covers 29.3% of lines; extract shared helpers, starting with degraded.go:15-20.
    duplication_pct: 0 -> 29.3, max_delta +6. 1 duplicate block covers 29.3% of lines; extract shared helpers, starting with degraded.go:15-20.
    globals: 0 -> 1, max_delta +0. 1 package-level variable holds state no signature reveals; pass it explicitly or move it into a struct.
    untested_exports: 0 -> 1, max_delta +0. 1 exported function has no test (JoinAgain); a rebuild would have to reverse-engineer its behavior.
```

Each line reads `metric: baseline -> head, limit. suggestion`. [reading-violations.md](reading-violations.md) gives the order in which to fix them; point the agent at it from `CLAUDE.md` or `AGENTS.md`.

## Requirements and failure modes

- `astimate` must be on the `PATH` Claude Code runs hooks with. If it is not, the shell exits 127, a non-blocking error: the agent stops and the gate did nothing. Check with `command -v astimate` in the shell you start Claude Code from.
- The base ref must exist locally. A missing ref is an analysis failure (exit 2), which blocks the stop with the git error on stderr; on the next attempt `stop_hook_active` is `true` and the agent may stop.
- The module must type-check. A broken build is also an analysis failure, and blocking on it is usually what you want.
