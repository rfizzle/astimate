# Hand labels for this repository's history

Date: 2026-09-28. Labels: `calibration/replay/labels/astimate.yaml`.
Replay: `calibration/data/replay-astimate-2026-09-28/` (222 first-parent
commits, `e40cfab` to `5e8c777`).

## Summary

| | Commits |
| --- | --- |
| Labeled | 222 (all `provenance: proposed`, all `agent: true`) |
| `block` | 30 |
| `allow` | 192 |
| `block` from the rule's evidence, confirmed by reading both commits | 9 |
| `block` from judgment alone | 21 |
| Reverted | 0 |

Every label is a draft awaiting the user's review; none is `confirmed`
yet. Every commit carries a `Co-Authored-By: Claude` trailer, so every
label has `agent: true`.

With 30 `block` labels this corpus clears the 15 the story asks for, so
recall on it can be reported. It is still one repository, written by one
agent and reviewed by one person, and 18 of the 30 rest on capacity
(below), so the external corpus stays the test of whether the gate
generalizes.

### Reasons given for `block`, by kind

A commit can have more than one kind; each count below is the number of
commits with that kind.

| Kind | Commits | What the reason cites |
| --- | --- | --- |
| Fixed up for a defect it introduced | 9 | The fixing commit and the function both changed: `faccda1` (by `8cd4c35`), `635a95a` (`716fe75`), `b241edc` (`d7cf439`), `be9ac98` (`02d4dec`), `5e351a8` (`b26f5b6`), `66b5a08` (`3f01734`), `2b0a320` (`3f01734`), `4ac37dc` (`d8b029c`), `4f5b236` (`e25b497`) |
| Package pushed past a capacity ceiling that a later commit had to split | 18 | Took a package over `sloc`, `tokens_est` or `largest_file_sloc`, or added 100 or more SLOC to a package already over one. The packages: `internal/lang/golang` (8 commits, split by `9a10fd6`), `internal/lang/typescript` (4, split by `46ffb57`), `internal/engine` (2; `ab2d630` also grows it but is counted under `internal/lang/golang`), `calibration/rebuild` (2), `calibration/fit` with `calibration/collect` (1) and `internal/metrics` (1). The last four were split or reshaped by `9843e5c`, after the replayed range |
| Copy-paste that a later commit had to extract | 8 | The per-command flag, usage, argument and logger code of `cmd/astimate` (`688b5ea`, `e10a0f5`, `399af8c`, `49adff2`, `8915b89`), replaced by shared helpers in `9843e5c`; the per-field switches of `internal/metrics` (`c5564d8`, `b69ea5f`), replaced by one field table in `9843e5c`; and `duptok` written as a second copy of the Go duplicate finder (`66b5a08`), unified by `4132fa0` |

`metrics` on a `block` label names the rules that should have fired:
`sloc`, `tokens_est` and `largest_file_sloc` for the capacity kind,
`dup_blocks` (with `duplication_pct` where it rose past its delta, and
`dup_blocks_cross_pkg` for the second duplicate finder) for copy-paste.
The fix-up-only labels list no rule: they are defects in behavior, which
no size, duplication or state rule is meant to catch, and they measure
what the gate cannot see.

## How the labels were drafted

1. The rule labeler (`go run ./calibration/replay/label`) over the replay
   marks 22 commits `block`, all by the fix-up part; no commit was
   reverted. For each of the 22 the fixing commit's message and diff were
   read. Nine fixes repair behavior the flagged commit introduced; those
   are `block` with the fixing commit in the reason. The other 13 match
   only because a later, unrelated fix edited the same function (for
   example `1532219`, a cgo fix, edits `dupStreamOf` from `7db060b`'s
   sign folding); those are `allow`, and each reason names the fixing
   commit and why it is unrelated. One of them, `6e8312f`, is `block`
   for capacity instead.
2. For every commit the subject, body and `git show --stat` were read; the
   diff was read where those were not enough, and at every place the
   replay reports a new package-level variable or new duplicate blocks.
3. `block` by judgment needs evidence a reviewer can check in the history:
   a later split of the package that was pushed past a ceiling, or a later
   extraction of the copied code. Growth in a package that was never split
   and repetition that was never extracted are `allow`.
4. Documentation, CI, build, release and calibration-data commits that
   change no Go package are `allow`; so are the 8 commits before `go.mod`
   ("predates the module").

The gate's verdict in `packages.jsonl` was not used to decide any label.
The capacity kind does use the replay's measured sizes, which are the
same numbers the capacity rules read; what makes those labels independent
of the gate is the later split, not the size. The validation report
should show the capacity `block` labels separately, since the gate is
bound to agree with most of them.

### What the gate reports that the draft allows

- Every new package-level variable in the range is a sentinel error
  (`var ErrX = errors.New(...)`, which AGENTS.md prescribes), ldflags build
  information, or the embedded default configuration. None is mutable
  state, so no commit is `block` for `globals`.
- Duplicate blocks in packages whose code was never extracted: the MCP
  tool handlers (`29f89f7`, `68327d3`, `cb7fd8a`), the calibration
  commands (`d7b8831`, `94bc679`), rows of a data table (`e7de35a`'s
  extension table) and the conformance suite's fakes (`566d78a`,
  `4749e47`). The per-tool registration and error handling in
  `internal/mcpserver` has the same shape as the `cmd/astimate` code
  blocked above; it is `allow` only because nothing extracted it.
- Growth under 100 SLOC in a package already over a ceiling (for example
  `02d4dec` in `internal/lang/golang`, `fafd115` in `internal/engine`).
  The reason says so.
- `untested_exports` findings: the exports are `Error`/`Unwrap` methods,
  constructors reached from other packages' tests, and test helpers in
  test-support packages.

## Where to start the review

The ten labels this draft is least sure of:

| Commit | Draft | Why it is uncertain |
| --- | --- | --- |
| `e1623a4` | block | A 21-SLOC refactor that happens to take `internal/engine` over `sloc` and `tokens_est`; the crossing is real but the commit is small |
| `3f01734` | block | A fix that improves TypeScript resolution; blocked only for adding 165 SLOC to a package at twice the `sloc` ceiling |
| `fafd115` | allow | Adds 98 SLOC to `internal/engine` and grows `check.go` from 643 to 741 lines, two short of the 100-SLOC line; arguably worse than `e1623a4` |
| `4ac37dc` | block | `d8b029c` fixes the `hasMarker` it wrote, but the code it replaced also ignored read errors |
| `faccda1` | block | `8cd4c35` fixes generic receivers in the untested-export detection it added; that may be a missing case rather than a defect |
| `8cd4c35` | allow | The rule matched `be9ac98`, which fixes a neighboring gap (generic test helpers) in the same detector; treated as an older gap, unlike `be9ac98`'s own follow-up |
| `4f5b236` | block | `e25b497` extracted the encoder it repeated and made assess and rank match; a small inconsistency |
| `688b5ea` | block | The first command to copy the flag and logger setup; the copy only became a pattern with the commands after it |
| `d345888` | block | `calibration/rebuild` landed 3.6% over the `sloc` ceiling; the split came after its size doubled in `9da995a` |
| `6052227` | block | Takes `internal/metrics` 716 tokens over `tokens_est` with 73 SLOC; the package's real problem was its per-field duplication |
