# Reading violations

This page is for the agent (or person) that `astimate check` has just refused. It explains the output and the order in which to fix it. Point your agent at it from `CLAUDE.md` or `AGENTS.md`.

## The output

```
violations:
  tested
    untested_exports: 0 -> 1, max_delta +0. 1 exported function has no test (JoinAgain); a rebuild would have to reverse-engineer its behavior.
warnings:
  big
    tokens_est: 21000 -> 24000, max 30000. at 80% of the 30000 ceiling; plan a split before the next feature. The package is 24000 tokens of non-test source; split it so a rebuild fits one agent pass.
tested: 0.1 passes (ONE_PASS), 1 violation, 0 warnings, +0.1 passes from baseline
big: 1.1 passes (ONE_PASS), 0 violations, 1 warning, +0.1 passes from baseline
```

Violations come first, then warnings, each grouped under the package directory, then one summary line per package (the `big` lines are illustrative). Each finding line is `metric: baseline -> head, limit. suggestion`; a package new since the baseline shows `head (no baseline)` instead, and `changed_func_cognitive_max`, which is itself measured against the baseline, shows `head (changed since baseline)`. The suggestion names the identifiers or file lines to start with when the extractor knows them. The Claude Code hook sends the same text as its `reason`; the JSON format carries the same fields as `violations` and `warnings` arrays.

**Fix every violation; read the warnings.** Violations fail the gate (exit 3, or a block decision in the hook). Warnings never do: they say a package is approaching a size ceiling. Do not trade a violation for a warning, and do not spend the turn on warnings while a violation remains.

Density rules are judged on the change, so the fix is always in what you just wrote: the baseline value is what the package had before, and bringing head back to it passes. Never "fix" a violation by editing the thresholds in `astimate.yaml`, deleting tests, or moving code into an ignored directory; those are the regressions the gate exists to catch.

## Fix order

Fix in this order. Earlier fixes often clear later violations, and the later ones are the more expensive to change.

**1. `untested_exports`.** An exported function or method that no test references. Write a test that calls it, in the package's existing test files and style; the suggestion names the functions. If the export does not need to be exported, unexport it instead. Do this first because the test is what lets you refactor safely for everything below, and because a duplicate you are about to remove may be the very function without a test.

**2. `dup_blocks` and `duplication_pct`.** Repeated token sequences, identifiers and literals normalized, so renamed copies still count. Extract the shared code into one function and call it from both places; the suggestion gives the file and line range of the first duplicate. One extraction usually clears both metrics, since `duplication_pct` is the share of lines the blocks cover. Do not disguise a copy by reordering statements; the fix is one implementation.

**3. `globals` and `init_funcs`.** New package-level variables or `init` functions: state and ordering that no function signature reveals. Pass the value as a parameter, hang it on a struct the caller constructs, or make it a constant or a function returning a fresh value. Replace an `init` with explicit construction. `has_tests` belongs with these: a new or growing package over the size guard with no test file needs one before anything else is meaningful.

**4. `max_nesting` and `cognitive_p90`.** Structural complexity. Return early instead of nesting `if` inside `if`, move the body of a deep loop into a named function, and split a function that does several things. `cognitive_p90` is the 90th percentile over the package's functions, so it moves when the most complex tenth of functions gets worse; simplify the function you just made hard to read rather than adding trivial functions to dilute the percentile. `changed_func_cognitive_max` catches the case the percentile misses: one function you added or modified since the baseline is past the ceiling on its own. The suggestion names it with its file and line; split it or flatten its branching. Editing only comments or formatting does not mark a function changed, and a legacy function you did not touch is never counted.

**5. Capacity rules: `tokens_est`, `sloc`, `largest_file_sloc`, `exported_symbols`, `internal_imports`.** These measure how much code there is, not how it is written. A warning means "plan a split": the package is past `warn_at` of its ceiling and one more feature may breach it; say so in your summary so a person can schedule it, and keep going. A capacity violation means the package no longer fits one agent pass and must be split along a real boundary before the change lands: move a cohesive group of types and their functions into a new package, or split an oversized file by concern. Do not shrink the feature or compress the code to squeeze under the ceiling.

## When the check itself fails

Exit code 2 (or an empty hook output with an error on stderr) is not a verdict: the module did not type-check, the base ref does not exist, or a package failed to load. Fix the build first; the gate cannot judge code that does not compile.
