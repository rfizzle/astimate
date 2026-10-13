# Hand labels for ai-shell: causes and evidence

Per-finding causes and `file:line` evidence behind `calibration/replay/labels/ai-shell-hand.yaml`. The labels file keeps one verdict per commit and a reason that starts with the cause; this file keeps every finding of a failed commit with its own cause. Every label is a draft (`provenance: proposed`) awaiting the user's confirmation. The taxonomy and the standard of evidence are those of the 2026-10-13 field adjudication; the counts and the least certain labels are in `calibration/notes/hand-labels-2026-10-14.md`.

## Failed commits (14)

### 064db6e6b `allow`: Merge pull request #76 from revanapriyandi/main

Adds the Indonesian locale: a JSON file and three registration lines in i18n.ts; the duplicate is the per-locale resource stanza nobody would extract and there is no logic to test. Evidence: `src/helpers/i18n.ts:14`, `src/helpers/i18n.ts:75`, `src/locales/id.json:1`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/helpers` | `dup_blocks` | boilerplate-dup | new dup is the locale registration idiom: one more import plus a `{ translation: idTranslation }` resource entry (src/helpers/i18n.ts:14, :75), same shape as every existing locale |
| `src/helpers` | `has_tests` | other: legacy has_tests false->false | only a locale wired in (src/helpers/i18n.ts:14); no new logic |

### 0ba6522a8 `allow`: Merge pull request #80 from georgesstein/config-get-known-error

A one-branch error for `ai config get <unknown key>` in a package that never had tests; not substantial new logic. Evidence: `src/commands/config.ts:42`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/commands` | `has_tests` | other: legacy has_tests false->false | adds a four-line else branch throwing KnownError for an unknown key (src/commands/config.ts:42-45) |

### 2768ec2de `allow`: Merge pull request #87 from AlaEddineBoughanmmi/main

Renames the `model` config key to MODEL and threads it into the chat request; plumbing only, no logic a reviewer would demand tests for. Evidence: `src/commands/chat.ts:20`, `src/helpers/config.ts:39`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/commands` | `has_tests` | other: legacy has_tests false->false | chat.ts only destructures MODEL and passes it through (src/commands/chat.ts:17-21, :50) |

### 77114d362 `allow`: Merge pull request #90 from AlaEddineBoughanmmi/main

Help-text restructuring and registering an already-existing command; no new logic. Evidence: `src/commands/chat.ts:13`, `src/commands/config.ts:16`, `src/cli.ts:27`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/commands` | `has_tests` | other: legacy has_tests false->false | commands move `description` under `help:` and cli.ts registers the existing update command (src/commands/chat.ts:13, src/cli.ts:27) |

### b25500ff7 `allow`: Merge pull request #92 from AlaEddineBoughanmmi/main

Parameter plumbing so the configured model is used in every request; nothing new to test. Evidence: `src/prompt.ts:102`, `src/prompt.ts:148`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src` | `has_tests` | other: legacy has_tests false->false | threads `model` through prompt/runOrReviseFlow/revisionFlow parameters (src/prompt.ts:102, :148, :217) |

### c0738a5a5 `block`: fix for streaming data including the shell command name erroneously

readData, already the hardest function in the repo, gains an interleaved second flag and a skip-until-newline continue inside a three-deep streaming loop (+21 cognitive); a reviewer would ask for this state machine to be simplified or extracted, and the next PR (#98) did rewrite it. Evidence: `src/helpers/completion.ts:198`, `src/helpers/completion.ts:215`, `src/helpers/completion.ts:220`. Confidence medium: It is a 20-line bug fix by the maintainer; a lenient reviewer might accept it, but the added interleaved state is exactly what the metric targets.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/helpers` | `changed_func_cognitive_max` | real | readData gains a second state flag `waitUntilNewline` (src/helpers/completion.ts:198) set inside the start-detection branch (:215) and consumed by a new nested `continue` block (:220-225) inside the for-await/for/if nest; cognitive 51->72 |
| `src/helpers` | `has_tests` | other: legacy has_tests false->false | the change is a fix to existing readData; the repo never had tests and the commit did not make that worse |

### 89ba88f77 `allow`: Merge pull request #98 from BurnOutDev/main

Replaces the previous commit's waitUntilNewline hack with a start buffer and a variadic exclusion list; nesting rises by one in an already 7-deep closure but the function is no harder to follow, and the new export is a trivial reduce. Evidence: `src/helpers/completion.ts:211`, `src/helpers/strip-regex-patterns.ts:1`. Confidence medium: max_nesting 7->8 is not the small-value tiny-ratchet case; it is allowed because the rewrite removes state rather than adding it.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/helpers` | `has_tests` | other: legacy has_tests false->false | logic change is a rewrite of existing readData plus a 9-line reduce helper (src/helpers/strip-regex-patterns.ts:1) |
| `src/helpers` | `max_nesting` | other: rewrite of already-deep readData, not harder | the `waitUntilNewline` continue block is replaced by a buffered `if (!dataStart) { ... if (match) {...} }` (src/helpers/completion.ts:211-219); one level deeper but one flag fewer and cognitive does not fire |
| `src/helpers` | `untested_exports` | other: trivial pure helper in testless repo | new export stripRegexPatterns is a one-expression reduce over replaceAll (src/helpers/strip-regex-patterns.ts:1-9) |

### 35a71c51d `allow`: Merge pull request #103 from thelastmayday/mayday

Lets q/escape stop the stream with one flag and one extra condition; cognitive 51->56 crosses the 55 ceiling but the function is not materially harder to follow. Evidence: `src/helpers/completion.ts:191`, `src/helpers/completion.ts:206`, `src/helpers/completion.ts:214`. Confidence medium: A reviewer would object to the readline interface, raw mode and keypress listener leaked on every call (completion.ts:201-210), but that is a resource bug, not the complexity the finding measures.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/helpers` | `changed_func_cognitive_max` | other: +5 cognitive, not materially harder | adds a `stopTextStream` flag set from a keypress listener (src/helpers/completion.ts:191, :206-210) and ORs it into the existing DONE check (:214); one flag, one condition |
| `src/helpers` | `has_tests` | other: legacy has_tests false->false | keypress-to-stop feature wiring, no new testable unit |

### 6fac82b6a `allow`: fix bugs

No code was copied: the cited block is the old getPrompt/promptForRevision clack prompt pair, now counted as one more block after a line was inserted; the stray `console.log('script', script)` (src/prompt.ts:123) is a real nit but no metric covers it and 62ec0f9 removed it. Evidence: `src/prompt.ts:51`, `src/prompt.ts:57`, `src/prompt.ts:76`. Confidence medium: Could not run the detector to confirm the 3->4 is the getPrompt/promptForRevision pair; inferred from the cited line and the only code change in src.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src` | `dup_blocks` | other: pre-existing duplicate re-detected after line shift | the p.group/p.text/onCancel block of getPrompt (src/prompt.ts:52-70) already mirrored promptForRevision (:73-92) before this commit; the diff only inserts `await initPromise;` at src/prompt.ts:51 |
| `src` | `has_tests` | other: legacy has_tests false->false | adds an init promise await and a debug log (src/prompt.ts:25, :51, :123) |

### 70ff00fb5 `allow`: revert

An exact revert of the previous commit's readData edit (diff is the inverse of 6fac82b); the numbers only return to what they were before and nothing new was introduced. Evidence: `src/helpers/completion.ts:195`, `src/helpers/completion.ts:222`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/helpers` | `changed_func_cognitive_max` | other: revert restores prior code | restores the buffer start-detection block (src/helpers/completion.ts:222-232) removed two minutes earlier by 6fac82b; readData returns to its 35a71c5 state (56) |
| `src/helpers` | `has_tests` | other: legacy has_tests false->false | revert, no new logic |
| `src/helpers` | `max_nesting` | other: revert restores prior code | nesting 8 is the pre-6fac82b value restored (src/helpers/completion.ts:223) |

### ce1fcfba6 `allow`: Merge pull request #109 from chen201724/main

Replaces free-text model entry with a select populated from the OpenAI models list; the new export is a three-line API wrapper with no logic worth a unit test. Evidence: `src/helpers/completion.ts:322`, `src/helpers/config.ts:191`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/helpers` | `has_tests` | other: legacy has_tests false->false | adds a model picker: getModels plus a p.select over its result (src/helpers/config.ts:191-199) |
| `src/helpers` | `untested_exports` | other: thin SDK wrapper in testless repo | getModels (src/helpers/completion.ts:322-330) is listModels() plus a one-line filter |

### 2d2c6bd03 `block`: Merge pull request #112 from llagerlof/shell_history

Adds a new exported helper that writes into the user's shell history files using a per-shell path table and last-line de-dup, with no tests; the de-dup and append are wrong for fish and extended-zsh formats, exactly the logic a reviewer would want tested before merge. Evidence: `src/helpers/shell-history.ts:10`, `src/helpers/shell-history.ts:40`, `src/prompt.ts:47`. Confidence medium: The repo has never had tests, so a maintainer might merge any feature untested; this is the most substantial new logic in the replay, which is why it is the one has_tests block.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src` | `has_tests` | other: legacy has_tests false->false | prompt.ts only wraps execaCommand in try/catch and calls appendToShellHistory (src/prompt.ts:42-50) |
| `src/helpers` | `has_tests` | real | new 52-line module that maps $SHELL to a history file and appends to the user's real history (src/helpers/shell-history.ts:6-25, :40-52) with no test |
| `src/helpers` | `untested_exports` | real | appendToShellHistory (src/helpers/shell-history.ts:40) has branching and file IO; it writes plain lines into zsh/fish history files whose formats (zsh extended `: ts:0;cmd`, fish YAML) it does not handle, which a test would have caught |

### 625ad5c7c `allow`: Merge pull request #115 from songlairui/main

A one-line fix passing the configured model to the explanation request. Evidence: `src/prompt.ts:137`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src` | `has_tests` | other: legacy has_tests false->false | one added argument `model` to getExplanation (src/prompt.ts:137) |

### 8e4a29b2d `allow`: Merge pull request #155 from loviuz/main

Locale addition only; no logic. Evidence: `src/helpers/i18n.ts:15`, `src/helpers/i18n.ts:81`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `src/helpers` | `has_tests` | other: legacy has_tests false->false | adds the Italian locale: import and resource registration (src/helpers/i18n.ts:15, :81) |

## Sampled passed commits (13)

| Commit | Verdict | Subject | Why | Evidence |
| --- | --- | --- | --- | --- |
| dc0a91493 | `allow` | 0.1.22 | Version bump 0.1.22; only package.json and package-lock.json change, no source. | `package.json:4` |
| c334cbb93 | `allow` | 0.1.23 | Version bump 0.1.23; no source changed. | `package.json:4` |
| 4304a5536 | `allow` | Merge pull request #78 from georgesstein/typo-fix | Typo fix 'me to to' in two prompt strings and the matching locale keys; no logic. | `src/prompt.ts:53`, `src/prompt.ts:77` |
| a517a37fc | `allow` | 1.0.1 | Version bump 1.0.1; no source changed. | `package.json:4` |
| 9b1ff8268 | `allow` | Merge pull request #85 from sbezludny/fix-await-set-config | Adds missing `await` on five setConfigs calls in showConfigUI; a bug fix with no new logic or structure. | `src/helpers/config.ts:175` |
| 653f6060f | `allow` | 1.0.3 | Version bump 1.0.3; no source changed. | `package.json:4` |
| b46e4e882 | `allow` | 1.0.4 | Version bump 1.0.4; no source changed. | `package.json:4` |
| 78e23f9bb | `allow` | 1.0.6 | Version bump 1.0.6; no source changed. | `package.json:4` |
| ea0f69b5b | `allow` | 1.0.8 | Version bump 1.0.8; no source changed. | `package.json:4` |
| 62ec0f909 | `allow` | remove log | Deletion-only: removes the debug `console.log('script', script)` added in 6fac82b. | `src/prompt.ts:127` |
| 6a2afb72c | `allow` | use gpt-4o-mini instead of gpt-3.5-turbo | Changes the default model string from gpt-3.5-turbo to gpt-4o-mini in two places; no logic change. | `src/helpers/completion.ts:75`, `src/helpers/config.ts:43` |
| e89f1c993 | `allow` | 1.0.11 | Version bump 1.0.11; no source changed. | `package.json:4` |
| ee266ed34 | `allow` | 1.0.12 | Version bump 1.0.12; no source changed. | `package.json:4` |
