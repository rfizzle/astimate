# Pre-commit

`astimate check` can run as a git pre-commit hook, so a commit that makes a package worse is refused before it is recorded. It suits a developer or an agent that commits locally; CI (`--format github`) remains the gate that cannot be skipped.

## Git hook script

Save this as `.git/hooks/pre-commit` and make it executable (`chmod +x .git/hooks/pre-commit`). To share it, commit it under a directory such as `.githooks/` and have each clone run `git config core.hooksPath .githooks`.

<!-- pre-commit-snippet:begin -->
```sh
#!/bin/sh
# Refuse a commit that makes a Go package worse than the base branch.
# ASTIMATE_MODULE is the module root relative to the repository root;
# ASTIMATE_BASE is the ref whose merge-base with HEAD is the baseline.
module="${ASTIMATE_MODULE:-.}"
base="${ASTIMATE_BASE:-master}"

# The first commit has no HEAD, so there is no merge-base to compare against.
if ! git rev-parse --verify --quiet HEAD >/dev/null; then
	echo "astimate: no commit yet; skipping the gate for the first commit" >&2
	exit 0
fi

astimate check "$module" --base "$base" --staged >&2
status=$?
if [ "$status" -ne 0 ]; then
	echo "astimate: check exited $status (3: gate failed, 2: analysis failed); commit refused" >&2
fi
exit "$status"
```
<!-- pre-commit-snippet:end -->

`--staged` makes `check` judge what the commit records: the head tree is the git index, copied into a temporary directory with `git checkout-index`, and only staged changes select packages. Unstaged edits and untracked files are ignored, so a partial commit (`git add -p`) passes or fails on the part being committed. Under `git commit -a` or `git commit <paths>` git hands the hook a temporary index in `GIT_INDEX_FILE`, and `--staged` reads that one. The baseline is still the merge-base with `ASTIMATE_BASE`.

Git runs the hook from the repository root, so `ASTIMATE_MODULE` only needs setting when `go.mod` is in a subdirectory. Set `ASTIMATE_BASE` to your default branch (`main`, `origin/main`) if it is not `master`. The output goes to stderr, where git shows hook output, and a failing run prints the exit code so a refused commit says whether the gate failed (3) or the analysis did (2).

`cmd/astimate/precommit_test.go` installs this exact script, read from this file, in a temporary repository, commits a pristine module, and checks that a commit staging only a harmless change passes while a degrading change sits unstaged beside it, that the same degradation is refused with exit 3 once it is staged, and that the commit succeeds once the change is reverted.

## pre-commit framework

With [pre-commit](https://pre-commit.com), add a local hook to `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: local
    hooks:
      - id: astimate
        name: astimate check
        entry: sh -c 'git rev-parse --verify --quiet HEAD >/dev/null || exit 0; exec astimate check . --base master --staged'
        language: system
        pass_filenames: false
        always_run: true
```

`pass_filenames: false` because `astimate` selects packages itself from the diff against the merge-base; `always_run: true` because a test-only or non-Go change can still change a package's metrics. Only the git hook script above is exercised by the tests.

## Limitations

- **No merge-base on the first commit.** Before the first commit `HEAD` does not exist, so there is nothing to compare against and `astimate check` would fail with an analysis error. The script skips the gate in that case; run `astimate check --all --baseline <file>` by hand if the first commit needs gating.
- **Untracked build inputs are missing from the staged tree.** The index copy holds tracked files only, as the baseline worktree does. A module that needs generated or ignored files to load (a TypeScript `node_modules`, generated Go code that is not committed) fails to analyze under `--staged`; commit those files or drop `--staged`, which checks the working tree instead.
- **The whole branch is judged, not only this commit.** With `--base master` on a feature branch the baseline is the merge-base with `master`, so a violation committed earlier with `--no-verify` keeps failing every later commit until it is fixed. On `master` itself the merge-base is `HEAD`, which gates just the new commit.
- **`--all` is off.** Only packages changed since the merge-base are checked, which keeps the hook fast; legacy debt in untouched packages never blocks a commit.
- **It costs two module loads.** The baseline tree is checked out into a temporary worktree and the index into a temporary directory, and both are analyzed; on a large module that takes seconds.
- **It can be skipped.** `git commit --no-verify` bypasses every pre-commit hook. Keep the CI gate.
