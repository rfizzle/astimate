# Hand labels for shhh: causes and evidence

Per-finding causes and `file:line` evidence behind `calibration/replay/labels/shhh-hand.yaml`. The labels file keeps one verdict per commit and a reason that starts with the cause; this file keeps every finding of a failed commit with its own cause. Every label is a draft (`provenance: proposed`) awaiting the user's confirmation. The taxonomy and the standard of evidence are those of the 2026-10-13 field adjudication; the counts and the least certain labels are in `calibration/notes/hand-labels-2026-10-14.md`.

## Failed commits (24)

### e3ca57505 `allow`: feat(eval): measure whether a server starts as a process

A new eval kind in its own file following the package's per-kind pattern; the repeated lines are the harness's request/usage idiom, and the capacity findings are ordinary growth of an already-large eval package. Evidence: `internal/eval/processchoice.go:47`, `internal/eval/delegation.go:85`, `internal/eval/table.go:357`. Confidence medium: The 10-line ask prologue now exists three times (delegation, readonly, processchoice); a reviewer could ask for a helper, but each variant differs in loop/rounds.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/eval` | `dup_blocks` | boilerplate-dup | processchoice.go:52-62 repeats the eval harness's ask shape (system+user msgs, oneReply, usage accumulate, firstLine err) seen at delegation.go:85-95 and readonly.go:100; a label const block like delegation.go:40-50 |
| `internal/eval` | `exported_symbols` | over-ceiling-growth | +4: KindProcessChoice and three Label consts (processchoice.go:31-43), the same per-kind vocabulary every eval kind exports |
| `internal/eval` | `internal_imports` | over-ceiling-growth | new edge to internal/process (processchoice.go:22) because the eval puts the process tool's definition in front of the model |
| `internal/eval` | `tokens_est` | over-ceiling-growth | +1261 tokens for one new 95-line eval kind in its own file (processchoice.go) |

### 6f84361cb `allow`: feat(ui): stop the rail carrying rows that never change

The commit removes rail rows (+474/-1081 overall, chat source is deletion-only); a duplicate count rising on pure deletion is not a regression. Evidence: `internal/ui/chat/todo.go:143`, `internal/ui/chat/inspector.go:216`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/chat` | `dup_blocks` | other: deletion-only change exposed a match | the chat diff only deletes (agentsHintRail, planHintRail, todoHintRail, spend model rows; inspector.go:216-231, todo.go:132-146, plan.go:633); no code was added, so the +1 block at todo.go:422 is pre-existing text newly matched |

### fe415c4ec `block`: fix: five hygiene fixes from the quality report

The fix pastes a 14-line temp-and-rename write from pricing into update instead of extracting the atomic-write helper the repository now needs in three places. Evidence: `internal/update/check.go:212`, `internal/pricing/pricing.go:441`, `internal/todo/run/sprint.go:223`. Confidence medium: The copy is small and correct; some maintainers would accept it in a 'hygiene fixes' commit. The cited location (agent/account.go:135) is legacy.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | update/check.go:209-225 copies pricing/pricing.go:441-455 verbatim (CreateTemp beside the cache, deferred Remove, Write/Close/Chmod/Rename); the commit message says 'like the pricing cache'. A third variant exists at todo/run/sprint.go:223 and no shared atomic-write helper exists |

### fdb5f71fc `allow`: fix(safety): close the Go read verbs by a safe-flag list

The only new package-level var is a compiled regexp. Evidence: `internal/agent/readonlyrules.go:156`, `internal/agent/readonlyrules.go:17`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent` | `globals` | immutable-global | the new global is goTemplateAction, a regexp.MustCompile at readonlyrules.go:156, never written; the other addition is entries in the existing readOnlyRules map literal |

### 3102b86c5 `allow`: feat(sandbox): require containment in plan and ask mode

A tri-state config option with its accessors and doctor wording; the duplicates are accessor and switch shapes, the exports are covered via callers. Evidence: `internal/cli/doctorsandbox.go:37`, `internal/config/config.go:1198`, `internal/cli/sandbox.go:174`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/cli` | `dup_blocks` | boilerplate-dup | withModeRequirement (doctorsandbox.go:37-57) is two parallel three-arm switches over the same predicates writing Detail vs Consequence text; different sentences, not extractable copy |
| `internal/config` | `dup_blocks` | boilerplate-dup | SandboxRequired/SandboxRequiredIn (config.go:1198-1211) are pointer-bool option accessors of the same shape as MCPEnvMaskEnabled/AgentsRequireSandboxEnabled beside them |
| `internal/config` | `exported_symbols` | over-ceiling-growth | +2 accessors on the 214-export config schema package (config.go:1198, :1208) |
| `internal/config` | `untested_exports` | covered-export | SandboxRequired and SandboxRequiredIn are one-line accessors exercised by cli/sandbox_test.go and ui/chat/containment_test.go through buildContainment/policy (both changed with +50/+55 test lines in this commit) |

### ff3ef767e `block`: feat(ui): open /ps as a manager that stops a started process

The new /ps screen copies the tools screen's account-field renderer and skeleton instead of sharing it (+18 dup blocks), and Snapshot re-copies List's sorted gather. Evidence: `internal/ui/components/managerscreen.go:433`, `internal/ui/components/toolsscreen.go:274`, `internal/process/process.go:978`, `internal/process/process.go:928`. Confidence medium: The managerField/toolsField copy is clear; whether the rest of the 18 blocks are screen-shape idiom is not checked block by block, and the process copy is only 7 lines.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | the new cross-package matches are surface wiring (chat/manager.go open/update/close pattern, process.Info struct-of-fields); the real copies are intra-package and reported below |
| `internal/cli` | `dup_blocks` | boilerplate-dup | runchatsession.go:661-668 adds four func fields to the chat.Processes wiring literal |
| `internal/process` | `dup_blocks` | real | Supervisor.Snapshot (process.go:978-984) re-copies List's lock/gather/sort of procs (process.go:928-937); a sortedProcs helper is the obvious fix |
| `internal/process` | `largest_file_sloc` | over-ceiling-growth | +67 sloc of Supervisor methods (Snapshot/Tail/Stop/Kill, process.go:955-1041) in the file that holds the Supervisor; file was already 653 |
| `internal/process` | `tokens_est` | double-report | same growth as largest_file_sloc |
| `internal/ui/chat` | `dup_blocks` | boilerplate-dup | overlay.go:300 heldScreens.manager() is the one-line heldAs accessor every screen has; manager.go open/close mirror the other surfaces |
| `internal/ui/chat` | `sloc` | over-ceiling-growth | +125 sloc for the /ps manager wiring in its own manager.go on a 43.6k-sloc package |
| `internal/ui/components` | `dup_blocks` | real | managerscreen.go:433-445 managerField is toolsscreen.go:274-286 toolsField verbatim but for the width constant, and the screen's chrome/panes/FocusSection/sync are lifted from ToolsScreen; +18 blocks |
| `internal/ui/components` | `exported_symbols` | over-ceiling-growth | +25 for a new screen (ManagerSection/State/Row/Act/Result/Screen and their consts) in the package that holds every screen |
| `internal/ui/components` | `sloc` | over-ceiling-growth | +408 is the new 534-line managerscreen.go, the natural home for the screen |
| `internal/ui/components` | `untested_exports` | covered-export | ManagerScreen.Update/View/Reading/FocusSection/SetSize and ManagerRow.Word/Live/Failed are driven by ui/chat/manager_test.go (+194 lines) through the host; no components test names them |

### 0b74b67bc `allow`: feat(ui): stop one running call from /ps while its round goes on

Ordinary feature growth in over-ceiling packages with the new export reached through ExecuteCalls; the duplicate is lock-prologue boilerplate. Evidence: `internal/agent/agent.go:506`, `internal/ui/chat/stream.go:147`, `internal/ui/chat/calls.go:1`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | new ui/chat/calls.go callTable methods open with the nil-receiver guard plus mu.Lock/defer Unlock prologue matched across packages (field note sample 4 shape) |
| `internal/agent` | `exported_symbols` | over-ceiling-growth | +1 ExecuteCallsWith (agent.go:514) on a 485-export package |
| `internal/agent` | `untested_exports` | covered-export | ExecuteCallsWith is the body ExecuteCalls now delegates to (agent.go:506), so TestExecuteCalls_* exercise it; ui/chat/stream.go:147 calls it under manager_test.go |
| `internal/ui/chat` | `sloc` | over-ceiling-growth | +127 sloc, mostly the new calls.go table in its own file, on a 43.7k-sloc package |

### 228d31aed `allow`: feat(ui): draw started processes and running calls on the rail

Two new rail blocks in their own file; nothing a maintainer would move or extract. Evidence: `internal/ui/chat/inspector.go:218`, `internal/ui/components/inspectorprocesses.go:16`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/chat` | `dup_blocks` | boilerplate-dup | inspector.go:218-232 re-aligns the InspectorRail struct literal with two new fields; field-list shape |
| `internal/ui/components` | `exported_symbols` | over-ceiling-growth | +2 InspectorProcess/InspectorCall view structs (inspectorprocesses.go:16,35) |
| `internal/ui/components` | `sloc` | over-ceiling-growth | +105 sloc in a new inspectorprocesses.go rail block file, where rail blocks live |

### 019895254 `block`: feat(ui): hold a long call and a dead process on the short rail

The commit pastes the block's meta-building lines a second time for the collapsed heading instead of reusing them. Evidence: `internal/ui/components/inspectorprocesses.go:81`, `internal/ui/components/inspectorprocesses.go:72`. Confidence medium: Only 7 lines in one function; a lenient reviewer might let it pass.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/components` | `dup_blocks` | real | processesBlock's new collapsed heading (inspectorprocesses.go:81-87) recomputes the meta string from lines 72-78 line for line, differing only in 'exited' vs 'ended'; compute once (and the two words now disagree) |

### 759f9adb5 `allow`: feat(config): disable the rows a checkout may not decide on its own scope

A small feature in the config screen and its host; the file was already over 1000 lines and no new copy-paste was added. Evidence: `internal/cli/config.go:928`, `internal/ui/components/configscreen.go:570`. Confidence medium: Did not identify which block the detector matched for components.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/cli` | `largest_file_sloc` | over-ceiling-growth | +20 sloc (disableRefused, config.go:928-949) in the config command file that already holds the screen's host |
| `internal/ui/components` | `dup_blocks` | boilerplate-dup | configscreen.go adds a Disabled field, a guard in updateMenu and one inlineRows branch; no copied logic found, the cited configscreen.go:743 block is legacy |

### ce64275ca `block`: fix(ui): say where a dry run ran and put it on the manager

The dry run re-pastes the real run's execution and outcome mapping instead of sharing it, so the two will drift. Evidence: `internal/ui/chat/run.go:399`, `internal/ui/chat/run.go:276`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | callTable.drop (calls.go:81-90) is the nil-guard plus mu.Lock/defer Unlock prologue matched across packages |
| `internal/ui/chat` | `dup_blocks` | real | dryRunKey's command closure (run.go:399-415: tailFn-or-run, commandEnding, the outcome switch to ExecTimedOut/ExecStopped) and its host/container 'where' are copied from executeRun (run.go:276-292, :252-256) |

### 3251a6e9e `allow`: fix(subagent): refuse a reconciliation that deletes the conflicting file

The new exports are a wrapper and an equality helper whose behaviour is tested through their callers. Evidence: `internal/subagent/worktree/merge.go:314`, `internal/subagent/worktree/unreconciled.go:23`, `internal/subagent/integrate.go:368`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/subagent/worktree` | `untested_exports` | covered-export | MarkedMerge (merge.go:314, a thin wrapper on mergeLines) and SeededFile.Same (unreconciled.go:23, one-line equality) are exercised through Unreconciled tests and subagent/integrate.go:368/406 under integrate_test.go |

### a48724402 `allow`: fix(subagent): never keep a refused integration patch that carries conflict marks

The new export is tested through Unreconciled and Integrate rather than by name. Evidence: `internal/subagent/worktree/unreconciled.go:66`, `internal/subagent/integrate.go:423`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/subagent/worktree` | `untested_exports` | covered-export | MarkedFiles (unreconciled.go:66) is called by Unreconciled (unreconciled.go:57), which unreconciled_test.go covers, and by subagent/integrate.go:423 |

### 7dd5de43e `allow`: refactor(cli): ask the receipt which tool a call is in the unattended approver, behind the fence

An intentional decoupling refactor that adds one import edge to an already over-ceiling package; not a regression. Evidence: `internal/cli/printapprover.go:23`, `internal/cli/printapprover.go:256`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/cli` | `internal_imports` | over-ceiling-growth | the refactor routes printapprover.go's tool-name checks through receipt.IsFetch/IsProcess/IsGitWrite (printapprover.go:23, :139, :256) to match the screen, adding one edge to a package with 58 |

### 0d137dc27 `allow`: refactor(observe): move the record's vocabulary to a leaf, and unexport single-importer names

A package extraction of an existing constant vocabulary; every finding is moved code. Evidence: `internal/observe/record/record.go:31`, `internal/observe/record/record.go:684`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | other: moved duplicates | the vocabulary const blocks moved from internal/observe into the new leaf observe/record, so existing const-list matches became cross-package |
| `internal/observe/record` | `dup_blocks` | no-baseline | record.go:31-74 vs :469-520 are moved const blocks of string codes (also boilerplate-dup) |
| `internal/observe/record` | `exported_symbols` | no-baseline | 161 moved Reason*/Outcome*/Class* constants in a vocabulary leaf |
| `internal/observe/record` | `globals` | no-baseline | record.go:684 searchTools and :694 writeTools are moved []string tables, never written (also immutable-global) |

### dfee87dc8 `allow`: refactor(tools): declare each tool's tier beside its definition

A refactor that moves tier declarations beside each tool; the new global is an init-time registry, the exports run on every agent test, and the duplicates are the declaration idiom. Evidence: `internal/agent/tier.go:174`, `internal/tools/tiers.go:13`, `internal/web/tool.go:205`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | web/tool.go:205-216 and structural/structural.go:303-314 Tiers() are the same declaration map-literal shape by design |
| `internal/agent` | `globals` | immutable-global | tier.go:174 `var registry = calltier.Must(...)` is built once at init and only read (declared, fromDeclaration) |
| `internal/agent` | `internal_imports` | other: split adds edge | new edge to internal/calltier (tier.go:8), the leaf created by this commit for the declarations |
| `internal/calltier` | `untested_exports` | covered-export | Must runs at agent init (agent/tier.go:174) and Declaration.Read at tier.go:187; New/Merge are tested in calltier_test.go |
| `internal/structural` | `untested_exports` | covered-export | Tiers (structural.go:303) is a registration map run at agent package init by every agent test |
| `internal/tools` | `exported_symbols` | over-ceiling-growth | +1 Tiers (tiers.go:13) |
| `internal/tools` | `untested_exports` | covered-export | Tiers (tiers.go:13) runs at agent init |
| `internal/web` | `exported_symbols` | over-ceiling-growth | +1 Tiers (tool.go:205) |
| `internal/web` | `untested_exports` | covered-export | Tiers (tool.go:205) runs at agent init |

### a1d84dfc8 `allow`: refactor(agent): move the side jobs into a sub-package taking the provider

A package extraction that shrinks agent; every finding is moved code or an edge to the split-off package. Evidence: `internal/agent/sidejobs/summary.go:218`, `internal/agent/classifier.go:193`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | other: moved duplicates | side jobs moved from internal/agent to agent/sidejobs (13 new files, 3227 lines, all moved); e.g. agent/classifier.go:193 config getters vs sidejobs/summary.go:218 became cross-package; intra-package dup in agent fell |
| `internal/agent/sidejobs` | `duplication_pct` | no-baseline | package created by this commit (no history before a1d84dfc); the repeated sidejob preamble (account.go:1-59 vs title.go:1-57) existed in agent; base 21.5 is not a real prior package |
| `internal/cli` | `internal_imports` | other: split adds edge | new edge to agent/sidejobs, code that used to be in agent |
| `internal/eval` | `internal_imports` | other: split adds edge | eval now imports agent/sidejobs for the moved call sites |
| `internal/subagent` | `internal_imports` | other: split adds edge | subagent imports agent/sidejobs |
| `internal/ui/chat` | `internal_imports` | other: split adds edge | ui/chat imports agent/sidejobs |

### 20c938846 `allow`: refactor(cli): move the doctor into its own package with the store and config passed in

A package extraction; the duplicates are moved and the exported helpers were made shareable for the split and are covered through cli. Evidence: `internal/cli/report/report.go:578`, `internal/cli/doctor/store.go:39`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | other: moved duplicates | the doctor moved from internal/cli into cli/doctor; existing intra-cli matches (e.g. cli/web.go:103 vs cli/doctor/store.go:39 dir fallback) became cross-package |
| `internal/cli/report` | `untested_exports` | covered-export | Count (report.go:578) and ShortPath (:587) are moved helpers wrapped by cli/history.go countOf and cli/config.go shortPath, which cli tests exercise |

### de2261b2c `allow`: feat(chat): say how old the working list is and ask the model to reconcile it

An ordinary feature (list age on the rail, close and check-in) spread across the files that own each surface. Evidence: `internal/ui/chat/worksteps.go:93`, `internal/ui/components/turnclose.go:1`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent` | `exported_symbols` | over-ceiling-growth | +1 for the check-in's step reading in checkin.go |
| `internal/ui/chat` | `dup_blocks` | boilerplate-dup | worksteps.go:93-109 checkInSteps/turnList open with the same inspectorSteps()-nil guard then build a value; no copied logic |
| `internal/ui/components` | `exported_symbols` | over-ceiling-growth | +2 (TurnList and an Age field surface) for the turn close and steps screen |
| `internal/ui/components` | `tokens_est` | over-ceiling-growth | +1384 tokens across inspectorsteps.go (+50) and turnclose.go (+44), each block's own file |

### 9b5901892 `allow`: feat(tools): let write_file make a file executable

Seven lines added to the card code that already lives in approval.go; not worth a split. Evidence: `internal/ui/chat/approval.go:1`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/ui/chat` | `largest_file_sloc` | over-ceiling-growth | approval.go 1058->1065, +7 sloc to show the executable bit on the existing write card |

### 504a1c76d `block`: feat(agent): answer an in-place edit written as a command with the editor

The new recogniser writes its own copy of the package's underDir path-containment check instead of calling it; the rest is ordinary growth. Evidence: `internal/tools/shelledit.go:199`, `internal/tools/gitpaths.go:163`. Confidence medium: The copy is three lines; the detector's block may instead be the tee/find flag loops, which are idiom. The field note credited this pair of commits with the tools TP.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent` | `exported_symbols` | over-ceiling-growth | +3 (Redirects, NewRedirects, Classified.AfterRedirect) for the new redirect memory in redirects.go |
| `internal/tools` | `dup_blocks` | real | shelledit.go:199-202 `under` re-implements gitpaths.go:163-166 underDir in the same package line for line; the tee/find arg loops are idiom |
| `internal/tools` | `exported_symbols` | over-ceiling-growth | +4 ShellEditError, ShellScope, ShellEdit and the Error method's type |
| `internal/tools` | `sloc` | over-ceiling-growth | +151 sloc in a new shelledit.go, the right home for the recogniser, with 72 lines of tests |

### 3ffa8f433 `block`: fix(agent): read heredocs, perl operands and rounds when answering an in-place edit

The heredoc reader adds a second hand-written shell quote scanner beside splitSegments; two quote parsers in one package will drift. Evidence: `internal/tools/shelledit.go:108`, `internal/tools/longrunning.go:72`. Confidence low: The author reads physical lines on purpose (an apostrophe in a body must not join lines), so the second scanner is arguably justified; a reviewer could accept it.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/agent` | `dup_blocks` | boilerplate-dup | agent.go:372 RoundSeq is a one-line counter accessor beside Rounds/RunID; the struct field block re-aligned |
| `internal/agent` | `exported_symbols` | over-ceiling-growth | +1 RoundSeq |
| `internal/agent` | `untested_exports` | covered-export | RoundSeq (agent.go:374) is a trivial accessor passed as the round func to NewRedirects and exercised in redirects_test.go |
| `internal/tools` | `dup_blocks` | real | heredocDelimiters (shelledit.go:108-152) re-implements splitSegments' quote-state scanner (longrunning.go:72-87: quote byte, backslash inside double quotes, escape) in the same package |

### 13e5c7041 `block`: feat(structural): record what a command changed and claim the rest on the card

The feature copies keptRows from driftRows, re-copies the path-set loop and a rel helper across structural and chat, and grows gitwrite.go and changeset.go past their ceilings in one go. Evidence: `internal/ui/components/undo.go:123`, `internal/structural/gitwrite.go:404`, `internal/structural/gitwrite.go:579`, `internal/ui/chat/cmdwork.go:120`, `internal/changeset/changeset.go:1`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | structural/gitwrite.go:579 `rel` vs ui/chat/cmdwork.go:120 `workspaceRel` and the both-spellings path-set loop (gitwrite.go:378-385 vs cmdwork.go:88-95), all written in this commit; cited classifier.go:199 is legacy |
| `internal/agent` | `exported_symbols` | over-ceiling-growth | +1 on a 390-export package |
| `internal/changeset` | `dup_blocks` | boilerplate-dup | new block changeset.go:1114-1120 Store.Unknown nil guard + mu.Lock/defer Unlock; cited changeset.go:720 legacy |
| `internal/changeset` | `exported_symbols` | over-ceiling-growth | +8 (Record.BeforeUnknown/Created/Deleted, Store.Unknown/Origins, CommandBracket, UndoPlan.Kept/KeptPaths), all tested |
| `internal/changeset` | `largest_file_sloc` | real | changeset.go 692->765 in one feature while the commit created command.go where Unknown/Origins fit |
| `internal/changeset` | `sloc` | real | 922->1067 crosses the 1000 ceiling in one feature (borderline); tokens_est merged |
| `internal/cli` | `tokens_est` | over-ceiling-growth | +1939 tokens on a 26k-sloc package |
| `internal/quality` | `exported_symbols` | over-ceiling-growth | +1 PastContentBound (fingerprint.go:63) |
| `internal/quality` | `untested_exports` | covered-export | PastContentBound is passed as Over: at ui/chat/cmdwork.go:46 and cli/headlessclaim.go:37, exercised through CommandBracket tests |
| `internal/structural` | `dup_blocks` | real | gitwrite.go:404-414 originWords re-copies stageable's both-spellings path loop at gitwrite.go:375-384 |
| `internal/structural` | `largest_file_sloc` | real | gitwrite.go 433->607 (+40%) in one feature; the claim/receipt logic could have its own file |
| `internal/structural` | `sloc` | over-ceiling-growth | +174 ordinary feature growth on a 1.5k package (the file-level finding carries the real complaint) |
| `internal/structural` | `untested_exports` | covered-export | ClaimRows (gitwrite.go:477) 4-line map used at ui/chat/policy.go:237 |
| `internal/ui/chat` | `dup_blocks` | boilerplate-dup | e.g. claim.go:53-59 claimPaths vs startoffers.go:130-137 map-then-Join shape |
| `internal/ui/chat` | `largest_file_sloc` | over-ceiling-growth | approval.go +10 |
| `internal/ui/chat` | `sloc` | over-ceiling-growth | +235 on a 44k-sloc package |
| `internal/ui/components` | `dup_blocks` | real | undo.go:123-146 keptRows is a copy of driftRows undo.go:151-174 (head row, truncate to undoDriftNames, 'and %d more') |
| `internal/ui/components` | `tokens_est` | over-ceiling-growth | +1169 tokens on a 22k-sloc package |

### ac1110f4e `block`: feat(tools): let edit_file take a pattern with groups and a list of files

The new multi-file edit card's code went into the already-oversized approval.go rather than a file of its own; everything else is ordinary growth. Evidence: `internal/ui/chat/approval.go:1930`, `internal/ui/chat/approval.go:1960`, `internal/tools/editacross.go:1`. Confidence medium: Only the approval.go growth is a real complaint, and a cheap one to fix; a maintainer might let it ride.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | e.g. tools/editacross.go:234-255 err-return chain vs subagent/supervisor.go:435-445 |
| `internal/agent` | `exported_symbols` | over-ceiling-growth | +1 |
| `internal/agent` | `untested_exports` | covered-export | SetTurnStart is a one-line setter |
| `internal/subagent` | `exported_symbols` | over-ceiling-growth | +1 |
| `internal/tools` | `exported_symbols` | over-ceiling-growth | +3 for the multi-file edit tool |
| `internal/tools` | `sloc` | over-ceiling-growth | +446, mostly the new editacross.go (358 lines) with a 455-line test; the right home (borderline) |
| `internal/ui/chat` | `largest_file_sloc` | real | approval.go +148/-19: filesApproval (approval.go:1930) and patternFields (:1960), a new card's code put into an 1,077-sloc file instead of its own file |
| `internal/ui/chat` | `sloc` | over-ceiling-growth | +103 on a 44k-sloc package |
| `internal/ui/components` | `exported_symbols` | over-ceiling-growth | +1 |

## Sampled passed commits (14)

| Commit | Verdict | Subject | Why | Evidence |
| --- | --- | --- | --- | --- |
| c470d9e3d | `allow` | feat(runner): teach the next attempt on the ceiling hand-over | One added line in capture.go plus a test; nothing the gate should catch. | `internal/runner/capture.go:1` |
| 3d0e73b45 | `allow` | ci: pin goimports, goreleaser and the sandbox base image | CI pins and a 10-line test in docker/ci; no package logic added. | `docker/ci/ci_image_test.go:1` |
| da6dd1264 | `allow` | fix(cli): create the inbox socket under a private umask too | Five lines wrapping the inbox Listen in the existing tightenUmask helper. | `internal/cli/send.go:69` |
| de482b4ab | `allow` | test(cli): pin the bytes one tool definition may cost per request | Test-only commit. | `internal/cli/toolbytes_test.go:1` |
| 92bcefb23 | `allow` | fix(ui): name CALLS beside PROCESSES in the rail click's help | Help text change of six lines. | `internal/ui/chat/help.go:1` |
| 2b865b7b9 | `allow` | fix(ui): shed whole rungs from the waiting and gated foot rows | frame.go +23 makes two hint rows shed rungs by width using the existing givesUp ladder; no copy, no new complexity of note. | `internal/ui/chat/frame.go:381` |
| 69446ff53 | `allow` | feat(config): call the two settings files global and project everywhere | Rename of user-facing words (98 lines in, 98 out); no logic added. | `internal/config/project.go:1`, `internal/cli/config.go:1` |
| 823777ccc | `allow` | docs(config): call the two files global and project in the project scaffold header | Comment and scaffold-text change only. | `internal/config/scaffold.go:1` |
| 6b8a51f58 | `allow` | fix(todo): verify and land a no-commit lane the way a committing lane lands | landUncommitted reuses catchUpLocked and verifyAfterCarry like laneCommit does; the parallel shape returns different outcomes and is tested by a new 77-line test. | `internal/cli/todorunlanes.go:897`, `internal/cli/todorunlanes.go:801` |
| c448b2c51 | `allow` | refactor(chat): keep the turn's pending decisions behind one value | Refactor that gathers pending decisions behind one value; stream.go loses 15 lines. | `internal/ui/chat/decision.go:1`, `internal/ui/chat/stream.go:1` |
| 3216a3344 | `allow` | feat(config): print a built-in wording to start an override from | A --wording flag with two small helpers (wordingKeyList, builtinWording) and tests in configinit_test.go and the report golden. | `internal/cli/configinit.go:82`, `internal/cli/prompts.go:195` |
| a6b273b2d | `allow` | docs: give chat and cli a package doc, put changeset and approval on the map, split the long paragraphs | Package doc comments only. | `internal/cli/doc.go:1`, `internal/ui/chat/doc.go:1` |
| 9c2bea46c | `allow` | feat(skill): carry built-in skills for the quality gate and .shhh/ | Built-in skills in a new builtin.go with an 85-line test; LoadFile's parse was extracted rather than copied. The embed.FS var is immutable. | `internal/skill/builtin.go:33`, `internal/skill/skill.go:206` |
| d5a4b9d19 | `allow` | feat(skill): carry built-in skills for the global configuration and for shhh itself | Extracts skillSearchLines and adds hasFileSkill in cli/skills.go with 91 lines of tests. | `internal/cli/skills.go:96` |
