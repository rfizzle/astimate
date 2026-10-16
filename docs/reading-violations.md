# Reading violations

This page is for the agent (or person) that `astimate check` has just refused. It explains the output and the order in which to fix it. Point your agent at it from `CLAUDE.md` or `AGENTS.md`.

## The words

Every surface of astimate (`report`, `check`, the hook and the MCP server) says the same sentences, written once in `internal/narrate`. `astimate report`'s glossary, and the MCP tool `explain_metric` for `rebuild_cost`, `split`, `watch`, `copied`, `untested`, `changes`, `limits`, `hard_to_follow` and `paths`, explain them in the same text.

- **Bands.** Every package is `Split`, `Watch` or `Fine`. Split: it is past what one agent pass can hold, or over two of its limits; carve it up before adding to it. Watch: it is over one limit, or large enough to need a few passes; the next growth will start failing the gate. Fine: everything else. The limits are the five size ceilings (lines, tokens, largest file, exports, internal imports).
- **Rebuild cost.** How long it would take to rewrite the package from its tests, as a range of one engineer's working time and as agent sittings (one sitting fits in one go; over three means split it first). The sittings are measured on agent rebuilds; the engineer time is not a measurement but a range, "2 to 3 weeks", from the lower to the higher of two textbook models, COCOMO 81 and COCOMO II under a rebuild-from-tests profile, printed once when both ends round the same. The change report's "days to rebuild" is the configured model's single figure.
- **Start here.** The three packages with the most changes lately times the agent passes they would take to rebuild, then the most engineer time at the low end: where a split pays back soonest. Each has a one-sentence story (what is wrong) and a **Do this** sentence (what to do about it). `astimate report` prints them; the MCP tool `report_module` returns them as `start_here` with the verdict, so an agent can ask what to split first in one call; `assess_package` returns one package's `story` and `todo`.
- **Heads up.** A warning: close to a limit, or a rule set to `severity: warn`. It never blocks.
- **Open at.** A finding's first place to look, a file and line (`open tested/degraded.go:15`).

## The output

In a terminal or a CI log, `astimate check` prints the change report (this is the repository's degraded fixture):

```
FAIL  1 package needs attention before this merges.  against master
      1 changed · 1 fail · 0 heads up · 0 pass clean · copies between packages 1 → 1

 ✗ tested  4 findings · 3.9 days to rebuild · 1 change lately
   changed func cognitive max  51     max 50
   A function this complex is the one a rebuild gets wrong.
   open tested/grade.go:8
   dup blocks                  0 → 1  1 new copy, none allowed
   A new copy is a second place every future fix has to land.
   open tested/degraded.go:15
   globals                     0 → 1  max_delta +0
   Shared mutable state is a dependency no signature shows, so callers and tests rely on it without saying so.
   open tested/degraded.go:9
   untested exports            0 → 1  max_delta +0
   Nothing pins what an untested export does, so a rebuild would have to guess it.
   open tested/degraded.go:13

 exit 3
```

`FAIL` or `PASS` comes first, then the counts: the packages that changed, failed, carry heads-ups or pass clean, and the code copied between packages at the baseline and now. Each failing package follows behind `✗`, any exempted violations after the heads-ups (see [Exemptions](#exemptions)). A finding gives its metric in words, baseline → head, and how far it moved against what the rule allows (`grew 416, allowed 150` for a package already past its ceiling, `4 new copies, none allowed` for new duplicates), else the rule's limit; then why the gate cares and what to do; then where to open. At 80 columns or fewer each finding takes one line and only the package's first place to open is named. The heads-ups are counted by rule on the `!` line; `astimate check --warnings` lists each with its package, limit and suggestion. The failing package's summary gives its rebuild days and how often it changed lately only when the rebuild parameters are calibrated, as the embedded default's are (its `config_version` starts `rebuild-`): an uncalibrated estimate can contradict the gate, rating a duplicated package one pass while the gate asks for a split. `astimate assess` and `rank` always show the estimate, labeled calibrated or uncalibrated.

The Claude Code hook sends the findings in full as its `reason`, without the counts. Each violation is its finding line, then, indented under it, why the gate cares and where to open:

```
violations:
  tested
    untested_exports: 0 -> 1, max_delta +0. 1 exported function has no test (JoinAgain); a rebuild would have to reverse-engineer its behavior.
      why: Nothing pins what an untested export does, so a rebuild would have to guess it.
      open tested/degraded.go:13
warnings:
  big
    tokens_est: 11000 -> 12800, max 16000. at 80% of the 16000 ceiling; plan a split before the next feature. The package is 12800 tokens of non-test source; split it so a rebuild fits one agent pass.
```

Each finding line is `metric: baseline -> head, limit. suggestion`; a package new since the baseline shows `head (no baseline)` instead, and `changed_func_cognitive_max`, which is itself measured against the baseline, shows `head (changed since baseline)` for a new function and the function's own `base -> head` for one that existed at the baseline. The `why:` line is the same sentence the report prints under the finding, and `open` names the same file and line as its **open at** line; both are left out when the finding has none. One cause is one finding: a rule over both its `max_delta` and its `max` reads `max_delta +6 and max 40`, and a `tokens_est` breach on the same growth as a `sloc` one, or a `duplication_pct` breach beside a `dup_blocks` one, is folded into the `sloc` or `dup_blocks` finding, whose limit names the other's values (`max 1000; tokens_est 15000 -> 17500, max 16000`) and whose JSON lists it under `also`. The suggestion names the identifiers or file lines to start with when the extractor knows them. A `dup_blocks` suggestion ending `added in this change` marks the block that sits in your own copy of the code, the one to extract; the other copy was already there. An `untested_exports` suggestion lists the exports your change added first, then the older untested ones, so the first names are the ones you owe a test. The JSON format carries the same fields as `violations`, `warnings` and `exemptions` arrays, and `--format github` writes each finding line as an annotation on the file and line it names.

**Fix every violation; read the heads-ups.** Violations fail the gate (exit 3, or a block decision in the hook). Heads-ups (warnings) never do: they say a package is approaching a size ceiling, or they are the breach of a rule the configuration sets to `severity: warn`, which reads exactly like a violation (and carries `"severity": "warn"` in JSON) but is reported without failing. Treat such a warning as a violation you are not forced to fix: fix it when it is in the code you changed. Do not trade a violation for a warning, and do not spend the turn on warnings while a violation remains.

Density rules are judged on the change, so the fix is always in what you just wrote: the baseline value is what the package had before, and bringing head back to it passes. Never "fix" a violation by editing the thresholds in `astimate.yaml`, deleting tests, or moving code into an ignored directory; those are the regressions the gate exists to catch.

## Fix order

Fix in this order. Earlier fixes often clear later violations, and the later ones are the more expensive to change.

**1. `untested_exports`.** An exported function or method that no test references. Write a test that calls it, in the package's existing test files and style; the suggestion names the functions. If the export does not need to be exported, unexport it instead. Do this first because the test is what lets you refactor safely for everything below, and because a duplicate you are about to remove may be the very function without a test.

**2. `dup_blocks` and `duplication_pct`.** Repeated token sequences, identifiers and literals normalized, so renamed copies still count. Extract the shared code into one function and call it from both places; the suggestion gives the file and line range of the first duplicate. One extraction usually clears both metrics, since `duplication_pct` is the share of lines the blocks cover. Do not disguise a copy by reordering statements; the fix is one implementation.

**3. `globals` and `init_funcs`.** New package-level variables or `init` functions: state and ordering that no function signature reveals. Pass the value as a parameter, hang it on a struct the caller constructs, or make it a constant or a function returning a fresh value. Replace an `init` with explicit construction. `has_tests` belongs with these: a new or growing package over the size guard with no test file needs one before anything else is meaningful.

**4. `max_nesting` and `cognitive_p90`.** Structural complexity. Return early instead of nesting `if` inside `if`, move the body of a deep loop into a named function, and split a function that does several things. `cognitive_p90` is the 90th percentile over the package's functions, so it moves when the most complex tenth of functions gets worse; simplify the function you just made hard to read rather than adding trivial functions to dilute the percentile. Deleting simple functions can raise the percentile too, which does not fail: the rule judges a rise only when a function you added or modified is more complex than the baseline's p90, and never below its floor (10 by default; 4 for `max_nesting`). `changed_func_cognitive_max` catches the case the percentile misses: one function you added or modified since the baseline is past the ceiling on its own. The suggestion names it with its file and line; split it or flatten its branching. A function that was already past the ceiling at the baseline fails only when your change raised it by more than the rule's `over_max_delta` (5 by default), so a small fix inside a legacy function passes; when several fail, the finding names the most complex of them. Editing only comments or formatting does not mark a function changed, and a legacy function you did not touch is never counted.

**5. Capacity rules: `tokens_est`, `sloc`, `largest_file_sloc`, `exported_symbols`, `internal_imports`.** These measure how much code there is, not how it is written. A warning means "plan a split": the package is past `warn_at` of its ceiling and one more feature may breach it; say so in your summary so a person can schedule it, and keep going. A warning whose limit reads `over max by N, allowed M` means the package was already past its ceiling and this change grew it by no more than the M one change may add there; it passes, but the package needs the split, so say so. A capacity violation means the package no longer fits one agent pass (the change crossed the ceiling, or grew a package already past it by more than that allowance) and must be split along a real boundary before the change lands: move a cohesive group of types and their functions into a new package, or split an oversized file by concern. Do not shrink the feature or compress the code to squeeze under the ceiling. The default does not judge a `main` package (the report's `role`) on `exported_symbols` or `internal_imports`: a composition root imports what it wires.

## Exemptions

A person can accept one rule's violation on one package by recording why in `astimate.yaml`:

```yaml
exemptions:
  - package: internal/registry   # module-relative directory; . for the root package, <module> for the module row
    metric: globals               # a metric some threshold gates
    reason: the registry is process-wide by design; see ADR 12   # required
    expires: 2026-12-31           # optional; the exemption applies through that day
```

An exempted violation does not fail the gate, but it never disappears: every format still reports it, with its reason. The text format lists it after the heads-ups, `exempted` with its package and metric and the reason under it; the JSON report moves it from `violations` to an `exemptions` array whose entries add a `reason` (the array is empty, not absent, whenever a gate ran); the hook puts it in the block reason on failure and on stderr on success; and the GitHub format writes it as a `::notice` annotation. Warnings are never exempted, since they never fail, except the breach of a `severity: warn` rule, which an exemption on its rule moves to `exemptions` as it would the violation. An exemption past its `expires` date is ignored, so the violation fails again, and a warning names it. `check --all` also warns about an exemption that matched no violation, on its package or, for a package the module no longer has, on `<module>`, so stale ones get removed; a check of the changed packages cannot tell and says nothing.

If you are an agent, do not add, widen or extend an exemption to get past the gate: that is the same as editing a threshold. Exemptions are a person's decision, and the reason is there for the reviewer who reads it. Treat an exempted finding as settled and leave it alone.

## When the check itself fails

Exit code 2 (or an empty hook output with an error on stderr) is not a verdict: the module did not type-check, the base ref does not exist, or a package failed to load. Fix the build first; the gate cannot judge code that does not compile.
