# Commit replay

SPEC.md 11 calibrates the gate's thresholds from what packages look like.
Whether the gate catches bad changes and lets good ones through can only be
measured on changes. `calibration/replay` runs the gate over a repository's
history as it would have run at each commit, and writes what it said, with
every metric it measured, so later analyses can join the rows with labels
and re-evaluate any rule at any threshold without replaying again.

## Invocation

From the repository root:

```sh
go run ./calibration/replay --repo <path> [--range <rev-range>] [--dir <module dir>] \
    [--config <file>] [--out <dir>] [--only <hash>] [--parallel N]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--repo` | `.` | The git repository to replay |
| `--range` | the whole first-parent history of `master`, else `main`, else `HEAD` | The revisions, as `git log` takes them (`A..B`, a branch, ...), walked along first parents, oldest first |
| `--dir` | `.` | The module root relative to the repository's top level |
| `--config` | the embedded default | The astimate configuration the gate runs with; the target repository's own `astimate.yaml` is ignored |
| `--out` | `calibration/data/replay-<repo>-<date>` | The output directory; `<repo>` is the repository's directory name, also from a linked worktree |
| `--only` | | Replay only this commit of the range |
| `--parallel` | 1 | Commits replayed at a time |

For each commit the command adds a detached git worktree of the target
repository at the commit, in a directory under the system's temporary
directory (never inside the target's checkout), and runs `engine.Check`
in-process against the commit's first parent as the git baseline, which is
`astimate check --base <parent>`: the packages whose files changed since the
parent are checked, the baseline is extracted at the parent, and an
exemption's expiry is judged at the commit's committer date. A root commit,
or one whose parent has no `go.mod` (or `package.json`) at `--dir`, is
checked with every package new, against an empty baseline. The worktree is
removed and pruned after the commit, and on interrupt.

The go command runs with `GOWORK=off` and `GOFLAGS=-mod=mod` unless they are
already set, as the collector runs for cloned modules. A module whose
dependencies are not in the module cache downloads them on first use, which
needs the network; with `GOPROXY=off` such a commit is recorded as not
loaded.

Exit codes: 0 when every selected commit is recorded, 1 when the run
stopped early (interrupted, a git failure, a row that could not be written,
or `--out` holding another configuration's rows), 2 for a usage or setup
error. A commit that cannot be checked is not a failure of the run.

### Resume

A rerun with the same `--out` skips every commit `commits.jsonl` already
records, so an interrupted run is resumed by running the same command, and
a second run over the same range writes nothing, not even `run.json`. A
commit's package rows are written and synced before its commit row; on
start, a final line cut short by an interrupted write is truncated from
either file, and package rows whose commit has no commit row are dropped, so
every row is written exactly once. An interrupted batch of `--parallel`
commits writes none of them. A commit recorded as not loaded is not retried;
to retry one, delete its line from `commits.jsonl`. A run whose
configuration's `config_version` differs from the rows already in `--out`
stops rather than mix the two; use another `--out`. When git itself fails
for a commit (it cannot list its files or check it out), the run stops
without recording that commit, so the rerun retries it.

Rows are written in range order whatever `--parallel` is, and their content
does not depend on it; only `wall_ms` and `run.json`'s `date` vary between
runs.

## Output

### `commits.jsonl`

One row per commit.

| Field | Meaning |
| --- | --- |
| `commit`, `parent` | Full hashes; `parent` is the first parent, empty for a root commit |
| `author_name`, `author_email` | The commit's author |
| `committer_date` | RFC 3339 |
| `subject` | The first line of the message |
| `co_authored_by` | The values of every `Co-Authored-By` trailer, whatever its key's case, sorted |
| `trailers` | Every trailer, key as written to its values in order |
| `files_changed` | Every path the commit changed against its first parent, sorted |
| `config_version` | The configuration the commit was gated under |
| `baseline` | `parent`, or `empty` when every package was judged new |
| `loaded` | False when the module did not load, the baseline could not be extracted, or the check failed; the commit then has no package rows |
| `error` | Why, when not `loaded`; the temporary worktree's path reads `<worktree>` |
| `packages_changed` | The packages checked, module-relative (`.` for the root package), in check order |
| `packages_deleted` | The directories that lost their package since the parent |
| `packages_failed` | `{package, error}` for each row that failed to extract; it has no package row |
| `passed` | The gate's verdict on the commit: no row has a violation; null when not `loaded` |
| `violations` | The number of violations over every row |
| `wall_ms` | The commit's replay wall time, checkout included |

### `packages.jsonl`

One row per checked package per commit, and one for the module row
(`package: "<module>"`, SPEC.md 8.1) whenever at least one package was
checked.

| Field | Meaning |
| --- | --- |
| `commit` | The commit's full hash |
| `package` | Module-relative directory, or `<module>` |
| `language` | The extractor's language id |
| `new` | True when the baseline has no such row (a new package, or an `empty` baseline) |
| `passed` | The row's verdict |
| `violations`, `warnings` | `{metric, base, head, limit, location}`: `base` is null for a new row, `limit` names the rule (`max 1000`, `max_delta +0`, ...), `location` is `{file, line}` relative to the module root, or null |
| `exemptions` | The violations an exemption silenced, as above with its `reason` |
| `metrics` | Every metric at the commit (SPEC.md 10.2 field names), `changed_func_cognitive_max` included |
| `base` | Every metric at the parent; null when `new` |
| `sloc_delta` | `sloc` at the commit minus at the parent, the parent counting 0 when `new`: the size of the change, for a size-only baseline |

With `metrics` and `base` a rule can be re-evaluated at any `max` or
`max_delta` without replaying; `new` says whether a delta rule without
`ratchet_from_zero` applies. Under an `empty` baseline the module row is
`new` too, and its module-wide rules were gated against zero.

### `run.json`

`date`; `parallel`; `source` (`repository`, `remote`, `module_dir`,
`range`, `first` and `last` commit); `tool` (`config`, `config_version`,
`astimate_commit`, `go`, `tokenizer`); and `totals` over the directory:
`commits`, `loaded`, `not_loaded`, `passed`, `failed`, `package_rows`,
`wall_seconds` (the sum of `wall_ms`), and the last writing run's `written`
and `skipped`.

## Labels

The labels files that the gate-validation report joins with these rows
(`calibration/replay/labels/<repo>.yaml`, one entry per commit with a
`verdict`, `reason` and `provenance`) join on the full commit hash, `commit`
in both files. Agent-authored commits of an external corpus are identified
from `co_authored_by`, `trailers` and `author_name`/`author_email`. A commit
that is not `loaded` has no verdict to score and is left out of the tables.

## This repository's history

`calibration/data/replay-astimate-2026-09-28/` replays this repository's
`master`, all first-parent commits, under the embedded default
configuration (`thresholds-2026-09-28`), with `--parallel 4`, on a
darwin/arm64 laptop with go1.27.1, every dependency already in the
module cache:

- 222 commits, `e40cfab` to `5e8c777`, one commit row each; 426 package
  rows (module rows included); about 0.9 MB in all.
- 214 loaded. The 8 that did not are the first 8 commits, documents written
  before `go.mod` existed ("no go.mod found"). The first commit with a
  `go.mod`, `e5a22bc`, was judged against an empty baseline.
- 119 commits changed at least one package. The gate failed 89 of the 214
  loaded commits and passed 125.
- Wall time 4 min 12 s (the commit rows' `wall_ms` sum to 824 s); a
  second run over the same range wrote nothing and took well under a
  second.

The most common violations, counted per package row and per commit:

| Rule | Rows | Commits |
| --- | --- | --- |
| `dup_blocks` `max_delta` | 70 | 55 |
| `tokens_est` `max` | 61 | 52 |
| `sloc` `max` | 58 | 50 |
| `untested_exports` `max_delta` | 21 | 19 |
| `globals` `max_delta` | 18 | 16 |
| `max_nesting` `max_delta` | 18 | 16 |
| `dup_blocks_cross_pkg` `max_delta` | 16 | 16 |
| `cognitive_p90` `max_delta` / `max` | 14 / 5 | 15 |
| `duplication_pct` `max_delta` / `max` | 7 / 2 | 7 |
| `largest_file_sloc` `max` | 7 | 7 |

Most of this history predates the self-check that now gates each commit,
and the configuration is today's, not the one each commit was written
under. That is the point of the replay: it scores today's gate against
changes that were later judged by review, so the failures are candidates
for the labels, not verdicts.
