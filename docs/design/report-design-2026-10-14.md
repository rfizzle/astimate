# Report design, 2026-10-14

The hand-made mockup this note was drawn from is no longer in the tree, because its data came from a real repository. The reference now is the tool's own page, `astimate report testdata/go/fixture --format html`, plus the anonymised 61-package module `internal/narrate/testdata/module-61.json` (raw metrics and the derived fields the mockup added: `band`, `over`, `hot`, `drivers`, `chunks`, `fn_easy`/`fn_tricky`/`fn_hard`, `top_fns`; package, file and function names replaced, every number unchanged). Stories S-218 to S-224 build it.

## Patterns borrowed (survey by Sonnet agent, URLs in its report)
1. One headline verdict with a colour band and a plain sentence (CodeScene code-health bands with a sentence each; Sonar quality gate Passed/Failed card; Lighthouse 3 bands, sub-scores hidden).
2. A short ranked list of what matters with an effort number per item (Sonar effort minutes; Lighthouse "opportunities" with a savings bar; CodeScene refactoring targets).
3. A two-axis hotspot view: how hot (churn) x how bad (health/complexity) (CodeScene enclosure map, Code Climate churn-vs-grade quadrant, "fix first" corner).
4. Progressive disclosure: summary, ranked list, map, table, per-package detail; passed items collapsed (Lighthouse "passed audits (n)").
5. Every finding: what, why it matters, what to do; a word not just a colour; a delta against the baseline. GitHub: <=10 annotations, ranked table in the step summary.
Avoid: a composite letter/score that hides which metric drove it; 20 tiles with no ranking; boxes/bars in non-TTY; colour-only severity (NO_COLOR).

## Two reports
A. Repository report (first look and periodic): `astimate report [dir] [--html out.html] [--json]`.
   Headline sentence + 6 tiles; band bar (Split / Watch / Fine, derived from existing ceilings and tiers, no new score); Fix first top 5 by churn x rebuild days with why/do sentences and effort; scatter churn vs days (dot = tokens) + treemap (area = tokens, colour = band); sortable table with over-ceiling marks; per-package detail with ceiling bars.
B. Change report (CI, hook, PR, two hashes): `astimate check` redesigned output + `--format github` step summary + `--html`.
   Verdict FAIL/PASS + sentence + tiles; failed packages with findings (metric, base -> head, over-vs-allowed bar, why, first place to look); warnings grouped by rule, compact; passed collapsed; Markdown block for PR comment/step summary; annotations capped at 10.
C. Terminal: lipgloss (MIT) with ASCII preset on non-TTY, NO_COLOR, width-aware (drop columns < 100 cols, stack < 80), unicode block bars, words FAIL/WARN/ok before colour. No chart library.
D. Web: single self-contained HTML (no CDN; offline, CI artifact), hand-written squarified treemap + scatter in SVG (~150 lines), light/dark.

## Band rule used in the mockup (to be stated in SPEC if adopted)
split: tier PARTITION or over >= 2 ceilings; watch: over 1 ceiling, or FEW_PASSES, or dup_pct >= 25; fine: otherwise. Ceilings are the default max values.
Hotspot score: churn_commits x human_days.

## Decisions taken with the user (2026-10-14)
- Bands Split / Watch / Fine; headings "Start here", "Heads up", "Before this merges"; bubble map is the default view.
- Rebuild cost is shown as one engineer's time (21 working days a month, 220 a year) and as agent sittings (one sitting <= 1 pass, a few sittings <= 3, split it first > 3), with the cost drivers as a share bar.
- Copied code as a 100-square waffle (repeated logic, tables and boilerplate, written once) with the longest repeat named.
- Complexity as easy (<= 10), needs care (11-20), hard (> 20) per function, the hardest three named with file:line and their path count (cyclomatic).
- `report` is a new command; `rank` and `assess` stay; `check` text output becomes the change report; JSON, hook shape and exit codes do not change.
- The page is self-contained and offline: layouts computed in Go, fonts embedded as OFL subsets. charmbracelet libraries are allowed for the terminal if they earn their place.

## Open questions (earlier)
- Band names (Split/Watch/Fine) and whether a band belongs in `check` output too.
- Whether `report` replaces `rank` or sits beside it.
- Whether the HTML should embed the per-function details (size).
