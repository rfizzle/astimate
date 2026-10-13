# Hand labels for crush: causes and evidence

Per-finding causes and `file:line` evidence behind `calibration/replay/labels/crush-hand.yaml`. The labels file keeps one verdict per commit and a reason that starts with the cause; this file keeps every finding of a failed commit with its own cause. Every label is a draft (`provenance: proposed`) awaiting the user's confirmation. The taxonomy and the standard of evidence are those of the 2026-10-13 field adjudication; the counts and the least certain labels are in `calibration/notes/hand-labels-2026-10-14.md`.

## Failed commits (17)

### b82d7d05d `block`: feat(session): restore last used provider/model from session

The same restore-model-from-session routine was pasted into three packages (app, cmd, ui/model) instead of living once next to config.IsModelAvailable; a reviewer would ask for one shared helper, and the cross-package and per-package dup findings name it. Evidence: `internal/app/app.go:449`, `internal/cmd/run.go:546`, `internal/ui/model/ui.go:2183`, `internal/config/config.go:737`. Confidence medium: The three copies differ in their I/O (service call vs client RPC vs workspace), so the shared part is the selection logic only; a reviewer might accept it.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | restoreModelFromSession is written three times: internal/app/app.go:449, internal/cmd/run.go:546, internal/ui/model/ui.go:2183 (same last-assistant scan, same IsModelAvailable guard, same small-model fallback); cited agent.go:243 is a legacy block |
| `internal/app` | `changed_func_cognitive_max` | other: +6 guard in legacy 52 function | RunNonInteractive (internal/app/app.go:267) gained one nested if/if calling the new helper; the problem is the copy, not the guard |
| `internal/cmd` | `dup_blocks` | real | internal/cmd/run.go:546 restoreModelFromSession repeats overrideModels' UpdatePreferredModel / GetDefaultSmallModel sequence (run.go:500-535) as part of the triple copy |
| `internal/config` | `exported_symbols` | over-ceiling-growth | one method, IsModelAvailable (internal/config/config.go:737), on a 144-export config package |
| `internal/config` | `untested_exports` | real | IsModelAvailable (internal/config/config.go:737) has logic (disabled-provider check, catalog loop) and no test names it at this commit |
| `internal/message` | `dup_blocks` | boilerplate-dup | GetLastAssistantMessage (internal/message/message.go:502) is the 5-line query-then-fromDBItem getter idiom shared by every service method |
| `internal/message` | `exported_symbols` | over-ceiling-growth | one interface method added (internal/message/message.go:54) |
| `internal/ui/model` | `dup_blocks` | real | internal/ui/model/ui.go:2183 copies handleSelectModel's set-large/default-small block (ui.go:2286-2300) as the third copy of the restore logic |
| `internal/ui/model` | `largest_file_sloc` | over-ceiling-growth | +53 in ui.go, which is 3,900+ lines; the size is legacy, the actionable part is the copy already reported |

### f50509d46 `allow`: chore(mcp): log how long each MCP server takes to connect

Two duration log lines in an already-large file; nobody would split init.go for this. Evidence: `internal/agent/tools/mcp/init.go:295`, `internal/agent/tools/mcp/init.go:627`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent/tools/mcp` | `largest_file_sloc` | over-ceiling-growth | +6 lines of timing logs in Initialize and goInitClient (internal/agent/tools/mcp/init.go:295, :627) in an 880-line file |

### 804324fb8 `allow`: feat(session): print session id on exit (#3398)

A small feature: an exit banner in cmd plus a trivial accessor; the dup is an options literal and the capacity hits are ordinary growth in already-over packages. Evidence: `internal/cmd/root.go:172`, `internal/cmd/root.go:176`, `internal/ui/model/ui.go:3954`. Confidence medium: The banner code in root.go is a reasonable thing to ask to move out of cmd (and it was, in a83480eb), but +64 lines is not a split a maintainer would demand at review.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | new logo.Render(... logo.Opts{FieldColor: t.Logo.FieldColor, ...}) literal at internal/cmd/root.go:176 mirrors internal/ui/model/ui.go:5059: a struct-of-fields options literal |
| `internal/cmd` | `internal_imports` | over-ceiling-growth | adds ui/logo and ui/styles imports (internal/cmd/root.go:41-43) to render the exit banner; package was already at 30 |
| `internal/cmd` | `largest_file_sloc` | over-ceiling-growth | printSessionResume and the exit-message table (internal/cmd/root.go:172, :221) add 64 lines; extracted to ui/exitbanner two commits later (a83480eb) |
| `internal/ui/model` | `exported_symbols` | over-ceiling-growth | one accessor, UI.CurrentSession (internal/ui/model/ui.go:3954) |
| `internal/ui/model` | `untested_exports` | covered-export | UI.CurrentSession (internal/ui/model/ui.go:3954) is a one-line field getter |

### ec2fe4379 `block`: fix: send hyper api key when fetching provider catalog (#3546)

Adds an exported ~50-line RefetchHyperProvider that mutates store state with no test, and duplicates its own replace-or-prepend provider loop in two places; a reviewer would ask for a test and one helper. Evidence: `internal/config/store.go:188`, `internal/config/store.go:210`, `internal/config/provider.go:251`, `internal/config/hyper_test.go:31`. Confidence medium: The cross-package dup could not be pinned, and which in-package block the dup detector counted is inferred; the untested_exports call alone carries the block.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | other: undecided | could not identify the new cross-package pair; the hyperSync.fetch body (internal/config/hyper.go:78) is moved code from Get, the likeliest source |
| `internal/config` | `dup_blocks` | real | RefetchHyperProvider's replace-or-prepend loop (internal/config/store.go:210-221) re-implements UpdateProviderInList (internal/config/provider.go:251-260) added in the same commit |
| `internal/config` | `exported_symbols` | over-ceiling-growth | +5 exports (HyperTokenRefresher, UpdateProviderInList, RefetchHyperProvider, SetClient/Refetch on an unexported type) in a 145-export package |
| `internal/config` | `sloc` | over-ceiling-growth | +122 sloc of a bug fix in a 3,300-sloc config package (merged tokens_est restates it) |
| `internal/config` | `untested_exports` | real | RefetchHyperProvider (internal/config/store.go:188, ~50 lines: refetch, lock, replace, clone config, SetupAgents) and UpdateProviderInList (internal/config/provider.go:251) have no test references; the new hyper_test.go tests only realHyperClient |

### 25bf6a255 `allow`: feat: ctrl+end to go to the bottom and follow (#3535)

A keybinding feature that adds one case arm and one helper, and the helper deduplicates three call sites; nothing a reviewer would push back on. Evidence: `internal/ui/model/ui.go:2398`, `internal/ui/model/chat.go:631`, `internal/ui/model/keys.go:241`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | new EndFollow key.Binding field and key.NewBinding chain (internal/ui/model/keys.go:46, :241) extend the key-map tables that match style/key tables in other packages |
| `internal/ui/model` | `changed_func_cognitive_max` | other: one case arm in legacy 327 switch | handleKeyPressMsg gained a single EndFollow arm (internal/ui/model/ui.go:2398) shaped like its TogglePills neighbour; the function was 327 before |
| `internal/ui/model` | `dup_blocks` | boilerplate-dup | key-binding entries and help-binding lists (internal/ui/model/ui.go:3238, :3292) plus the case arm; the commit actually removed three copies by adding ScrollToBottomAndSelectLast (internal/ui/model/chat.go:631) |
| `internal/ui/model` | `exported_symbols` | over-ceiling-growth | one method, Chat.ScrollToBottomAndSelectLast (internal/ui/model/chat.go:631) |

### 6abd61ec5 `allow`: Merge pull request #3426 from joestump-agent/upstream/bash-permission-ast

A merged PR that rewrites the read-only command check as a parsed allow-list with tests; the duplication is table rows and the growth is one coherent feature in an already-oversized package. Evidence: `internal/agent/tools/safe.go:145`, `internal/agent/tools/safe.go:279`, `internal/agent/tools/bash.go:209`. Confidence medium: The tools package is 6.4k sloc and the safety parser is self-contained enough that a reviewer could ask for its own package; I judged it ordinary growth.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent/tools` | `dup_blocks` | boilerplate-dup | new safeCommand table rows with restrictFlags/allowFlags (internal/agent/tools/safe.go:145, :169, git branch/tag pairs) are declarative allow-list entries |
| `internal/agent/tools` | `sloc` | over-ceiling-growth | +237 sloc for the AST-based bash safety check (internal/agent/tools/safe.go:279 isSafeReadOnly) replacing a prefix match; bash.go:209 shrank; merged tokens_est restates it |

### 240c487ee `allow`: Merge pull request #3565 from charmbracelet/revert-3426-upstream/bash-permission-ast

A clean revert of PR #3426; it restores the exact pre-merge tree, so the complexity rise only undoes the previous merge's reduction. Evidence: `internal/agent/tools/bash.go:209`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent/tools` | `changed_func_cognitive_max` | other: revert restores prior code | the revert re-inlines the prefix-match loop into NewBashTool's closure (internal/agent/tools/bash.go:209); diff 6abd61ec^1..240c487e is empty, so the function is back to its pre-merge self |

### ed15da4c4 `allow`: fix(ui): use dialog background for bash syntax highlighting in permissions (#3575)

A one-field bug fix in the theme table; the duplicate is lipgloss style assignments. Evidence: `internal/ui/styles/quickstyle.go:963`, `internal/ui/dialog/permissions.go:574`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/styles` | `dup_blocks` | boilerplate-dup | one style-assignment line (internal/ui/styles/quickstyle.go:963 s.Dialog.ContentPanelBg = o.bgLessVisible) inside the theme table re-chunks a block of s.Dialog.* assignments |

### 7dc88e247 `allow`: feat(ui): show user@host:cwd in the header by default (#3583)

The new global is a lazily computed constant via sync.OnceValue, not mutable state. Evidence: `internal/ui/model/header.go:38`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/model` | `globals` | immutable-global | currentUserHost (internal/ui/model/header.go:38) is a sync.OnceValue memoizer of user/host, assigned once and never written |

### 90bc466d6 `allow`: session picker: add mouse support

Mouse support in the session picker; the new duplicate is a pre-existing Draw preamble shifted by one inserted line, per the field adjudication, which the diff confirms. Evidence: `internal/ui/dialog/sessions.go:281`, `internal/ui/dialog/sessions.go:371`. Confidence medium: The detector's exact pairing was not reproduced; the inference rests on the adjudication note plus the diff showing no copied block.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/dialog` | `dup_blocks` | other: legacy block re-chunked | the dialog Draw preamble shared by all dialogs was re-chunked by the inserted s.bodyArea reset (internal/ui/dialog/sessions.go:281); the new mouse code (sessions.go:240 handleMouseClick, :371 updateSessionListArea) is not a copy |

### 08552fb91 `allow`: feat: textarea selection (#3507)

Textarea selection is ordinary feature work in an already-huge UI package; the new logic sits in a helper and the duplicates are key tables and two short case arms. Evidence: `internal/ui/model/ui.go:711`, `internal/ui/model/ui.go:3503`, `internal/ui/model/ui.go:2687`, `internal/ui/model/keys.go:142`. Confidence medium: +16 cognitive on UI.Update is above the +5 allowance and a strict reviewer could want the three mouse guards folded into one dispatch.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | new key.Binding struct fields and key.NewBinding chains in internal/ui/model/keys.go match styles/dialog key and style tables (field adjudication, confirmed in diff) |
| `internal/ui/model` | `changed_func_cognitive_max` | other: three delegating guards in legacy 369 Update | UI.Update (internal/ui/model/ui.go:711) got three if-guards that only call the new forwardMouseToTextarea helper (ui.go:3503); the logic was correctly extracted |
| `internal/ui/model` | `dup_blocks` | boilerplate-dup | key-binding tables, the two FullHelp editor lists, and the Copy/Cut case arms (internal/ui/model/ui.go:2678-2695) that differ by message and Clear vs Delete |
| `internal/ui/model` | `largest_file_sloc` | over-ceiling-growth | +103 lines in a 4,000-line ui.go for one feature (textarea selection) |
| `internal/ui/model` | `sloc` | over-ceiling-growth | +123 sloc in a 7,000-sloc package; merged tokens_est restates it |

### 8661a1c4b `allow`: fix(windows): scrolling up on modifier keys alone (#3598)

A minimal Windows bug fix applied identically to five filter blocks that were already copies; it did not create the duplication. Evidence: `internal/ui/dialog/notifications.go:157`, `internal/ui/dialog/commands.go:231`. Confidence low: Borderline: a reviewer could reasonably ask for an updateFilter helper while touching all five copies (the adjudication called it borderline TP).

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/dialog` | `dup_blocks` | other: pre-existing copies lengthened by a uniform fix | the same prevValue guard was applied to five pre-existing filter blocks (internal/ui/dialog/commands.go:231, models.go:224, notifications.go:157, reasoning.go:148, sessions.go:219), pushing one more past the block threshold |

### cce887189 `allow`: fix(mcp): repair stdio startup diagnostics (#3634)

A focused stdio-diagnostics bug fix with a test; no maintainer would split init.go over 17 lines. Evidence: `internal/agent/tools/mcp/init.go:980`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent/tools/mcp` | `largest_file_sloc` | over-ceiling-growth | bug fix: unwrapTransport (internal/agent/tools/mcp/init.go:980) plus the argv0 fix; +17 lines in an 887-line file |

### 8f63ee593 `block`: fix: wrapped messages counted as newlines (#3627)

The fix copied a 15-line row/column-range loop from HighlightBuffer into extractRows; a reviewer would ask to share it, and the dup_blocks finding names it. Evidence: `internal/ui/list/highlight.go:58`, `internal/ui/list/highlight.go:211`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/list` | `dup_blocks` | real | extractRows (internal/ui/list/highlight.go:58-77) re-implements the row loop and column-range clamping of HighlightBuffer (highlight.go:211-230) instead of sharing a helper |
| `internal/ui/list` | `sloc` | other: marginal ceiling crossing by one fix | 928->1014 is 1.4% over a 1,000 ceiling, from highlight.go +145 for one bug fix; not a split anyone would ask for |

### 82557e2af `allow`: fix(mcp): fix sessionless mcp servers (e.g. github mcp) (#3651)

A bug fix adding one option and a read-only lookup table; neither finding is a maintainability regression. Evidence: `internal/config/config.go:461`, `internal/config/config.go:471`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/config` | `exported_symbols` | over-ceiling-growth | one config field plus MCPConfig.IsSessionless (internal/config/config.go:471) in the config-schema package |
| `internal/config` | `globals` | immutable-global | knownSessionlessMCPs (internal/config/config.go:461) is a map literal lookup table, never written |

### 773c35656 `allow`: resume: compact resume banner option

Adds one config option end to end; every finding is a new enum, an accessor, a validation arm or small growth in already-over files. Evidence: `internal/cmd/root.go:231`, `internal/config/config.go:313`, `internal/shellconfig/options.go:237`, `internal/ui/model/ui.go:4089`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/cmd` | `largest_file_sloc` | over-ceiling-growth | +24 in root.go for the compact banner option (internal/cmd/root.go:231 sessionResumeLines extracted); undone by the next commit's extraction |
| `internal/config` | `exported_symbols` | over-ceiling-growth | ExitBanner type, three consts and Full (internal/config/config.go:281-313): a new user option in the config-schema package |
| `internal/config` | `untested_exports` | covered-export | ExitBanner.Full (internal/config/config.go:313) is a one-line predicate |
| `internal/shellconfig` | `dup_blocks` | boilerplate-dup | the case "exit-banner": enum validation arm (internal/shellconfig/options.go:237) mirrors the scrollbar and diff arms |
| `internal/ui/model` | `exported_symbols` | over-ceiling-growth | one accessor, UI.Config (internal/ui/model/ui.go:4089) |
| `internal/ui/model` | `largest_file_sloc` | over-ceiling-growth | +6 lines for the UI.Config accessor in a 4,075-line file |
| `internal/ui/model` | `untested_exports` | covered-export | UI.Config (internal/ui/model/ui.go:4089) is a nil-safe getter |

### a83480eb1 `allow`: refactor: centralize TUI option defaults and extract exit banner rendering

A refactor that extracts a tested exitbanner package and centralises option defaults; it makes the code easier to maintain and the findings are capacity ratchets and covered exports. Evidence: `internal/config/load.go:546`, `internal/config/config.go:287`, `internal/ui/exitbanner/exitbanner.go:73`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/config` | `exported_symbols` | over-ceiling-growth | DiffMode consts, IsTransparent and NormalizeOptions (internal/config/config.go:287, internal/config/load.go:546) in the config-schema package |
| `internal/config` | `largest_file_sloc` | over-ceiling-growth | +9 in load.go for NormalizeOptions extracted from setDefaults (internal/config/load.go:546) |
| `internal/config` | `untested_exports` | covered-export | NormalizeOptions (internal/config/load.go:546) is exercised by load_test.go through setDefaults; IsTransparent (config.go:287) is a one-line nil-safe getter |

## Sampled passed commits (9)

| Commit | Verdict | Subject | Why | Evidence |
| --- | --- | --- | --- | --- |
| 48768f8b3 | `allow` | fix(ui): don't block interactive prompts on MCP initialization | Removes two mcp.WaitForInit waits and rewrites comments; deletion-only logic change, nothing the gate should catch. | `internal/agent/coordinator.go:227` |
| c78cc6745 | `allow` | fix(mcp): only wait for slow MCP servers in non-interactive runs | Restores the wait for non-interactive runs behind one if and adds DisarmInit, a 3-line test-support export; no copy, no complexity jump. | `internal/agent/coordinator.go:242`, `internal/agent/tools/mcp/init.go:120` |
| b8453c4ad | `allow` | chore: gofumpt | gofumpt reformat of one call (one line split in two); formatting only. | `internal/agent/tools/mcp/init.go:629` |
| 47df446e7 | `allow` | Revert "feat(ui): show user@host:cwd in the header by default (#3583)" (#3584) | Reverts the user@host header feature; deletion only. | `internal/ui/model/header.go:24` |
| 6ee6e5c4c | `allow` | ci: re-record vcr cassettes | Re-recorded VCR cassettes and a one-line test model change; no source change. | `internal/agent/agent_test.go:33` |
| db7fac6cc | `allow` | test: unset API keys during auth test | Test-only: adds an env-isolation helper to a test file. | `internal/config/refresh_singleflight_test.go:18` |
| a0fe47b62 | `allow` | fix: return to the start of the line (#3635) | Adds ctrl+a to one key binding and rewrites a comment; trivial. | `internal/ui/model/ui.go:405` |
| 75962c488 | `allow` | fix: weird indentation on code blocks (#3642) | Adds a small unexported renderLines helper for a rendering bug; no copy, no complexity, no new export. | `internal/ui/xchroma/chroma.go:94` |
| 460a877fc | `allow` | fix: preserve first line indentation in tool call previews (#3652) | One-line change from TrimSpace to Trim("\n") with a doc update; trivial. | `internal/stringext/string.go:23` |
