# Field adjudication 2026-10-13

astimate v0.3.0 at 607596a run as `check --all --base HEAD~N` on sibling repositories, every finding judged TP / FP (with a cause) / LEGACY by Opus subagents reading the cited code and the diff. Per-commit replays of go-starter and ztp (last 60 commits) and per-commit spot checks on crush and shhh. Not reviewed line by line by a person. Untracked note; see the session summary for the pooled numbers.


---

# Adjudication A: astimate check --all on goKMS, kubesentry, ai-orchestration

Read-only review. Each row was judged against `git diff <base> HEAD` and the cited code.

## goKMS (base 08c2ae3, HEAD~5)

| package | rule | verdict | cause | evidence | note |
|---|---|---|---|---|---|
| `<module>` | dup_blocks_cross_pkg 29->32 (V) | FP | boilerplate-dup | cmd/devportal/main.go:8-20 and :24-36 | The new cross blocks are the new main's import block, `flag.String(...env(...))` lines, the ListenAndServe/os.Exit tail and the `env()` helper, all shared with cmd/kms and cmd/encryptiond; the fourth is the make-slice/sort-by-Name idiom in internal/kms/service.go:402-409 and internal/portal/portal.go:168-177. |
| internal/audit | cognitive_p90 1->8 (V) | FP | tiny-ratchet | internal/audit/audit.go:79 (`Ring.List`) | A 20-line ring-buffer read with loop, filter and limit. The package has only a few functions, so p90 is just the max. It is tested in test/admin_roles_test.go:131. |
| internal/audit | max_nesting 1->2 (V) | FP | tiny-ratchet | internal/audit/audit.go:89-96 | for>if inside `Ring.List`. Depth 2 is ordinary code. |
| internal/audit | untested_exports 2->3 (V) | FP | other | internal/audit/audit.go:45 (`Multi.Record`) | The one new untested export is a 3-line fan-out loop that only cmd/kms/main.go wires up. The new behaviour that matters (`NewRing`, `Ring.Record`, `Ring.List`) is tested in test/admin_roles_test.go:29,131. A reviewer would not ask for a test of the loop. |
| internal/auth | has_tests false->false (V) | LEGACY | - | internal/auth/auth.go:93-103 (`ReadmeRoles`), :124-135 (`AuthorizeAdmin`) | The change is a real authz change, but test/admin_roles_test.go:54 `TestAdminRolesSplitOperations` exercises it end to end. Failing a legacy no-tests package that the change covered from another package is not reasonable. |
| internal/kms | dup_blocks 61->63 (V) | FP | boilerplate-dup | internal/kms/handlers.go:252-259, :262-263 | The two new blocks are the connect handler shape (`x, err := h.svc...; if err != nil { return nil, err }`) and a proto-append loop in `ListAuditEvents`. The cited lead block, crypto.go:64-71 (uvarint decode chain), is old. |
| internal/kms | exported_symbols 88->90, max 60 (V) | FP | over-ceiling-growth | internal/kms/handlers.go:255, service.go:121 | +2 for one new RPC (`Handler.ListAuditEvents`, `Service.ListAuditEvents`). This rule has no growth allowance, unlike sloc and tokens_est, which only warned for the same change. |
| internal/kms | has_tests false->false (V) | LEGACY | - | 1,805 SLOC with no in-package tests | The package is driven by test/e2e_test.go and test/admin_roles_test.go. The change added 32 lines. Failing it is not reasonable. |
| internal/kms | largest_file_sloc 968->988 (V) | FP | over-ceiling-growth | internal/kms/service.go:109-132 | +20 lines (the `authorizeAdmin` helper and `ListAuditEvents`), against a 5-line allowance. The same change got a 100-line allowance on sloc. The helper deduplicated 18 call sites, which is a quality improvement. |
| internal/kms | untested_exports 44->45 (V) | FP | covered-export | internal/kms/service.go:121 (`Service.ListAuditEvents`) | test/admin_roles_test.go:100,124 calls it through the connect client. The coverage is transitive over HTTP, so the rule cannot see it. The whole `Service.*` API is listed as untested for the same reason. |
| internal/portal | dup_blocks 5, no baseline (V) | FP | boilerplate-dup | internal/portal/portal.go:101-108 (mux `HandleFunc` registrations) | The cited block and three others are `whoIs` / `json.Decode` / regexp-validate followed by `fail(w,400,...); return` chains (portal.go:214-222, 246-254, 256-263). One of the five is a real extractable helper: the component lookup and 404 in portal.go:282-297 and :313-332. |
| internal/portal | globals 4, no baseline (V) | FP | immutable-global | internal/portal/portal.go:45,52,53,57 | `personas` and `esMethods` are literal lookup maps and `nameRE`/`labelRE` are `regexp.MustCompile`. None is written anywhere. |
| internal/algo | exported_symbols 48 (W) | LEGACY | - | package not touched in the window | A warning on an unchanged package, at 80% of the ceiling. Harmless. |
| internal/encsvc | largest_file_sloc 618 (W) | LEGACY | - | internal/encsvc/service.go (2-line lint edit) | Unchanged size. Warning only. |
| internal/encsvc | sloc 763 (W) | LEGACY | - | same | Unchanged. Warning only. |
| internal/kms | sloc 1773->1805 (W) | LEGACY | - | internal/kms/service.go | Within the 100-line allowance. A warning is the right level. |
| internal/kms | tokens_est 20321->20880 (W) | LEGACY | - | same | Within the allowance. A warning is the right level. |

**Missed (goKMS):** nothing material. The new portal is 502 lines with portal_test.go and test/portal_test.go. The one real dup in it (component lookup) is buried in a boilerplate-dominated finding.

## kubesentry (base af04825, HEAD~15)

| package | rule | verdict | cause | evidence | note |
|---|---|---|---|---|---|
| cmd | dup_blocks 22->27 (V) | TP | - | cmd/audit.go:57-88, cmd/entity.go:45-77, cmd/role.go:41-74 | The same `switch viper.GetString("output") { case "text" / case "csv" / default }` block was pasted into three commands, and inside each switch the `text` and `default` arms are identical. A reviewer would ask for a `writeReport(out, fmt)` helper. The suggestion cites audit.go:1-28 (the old cobra preamble) instead of the new block. |
| cmd | duplication_pct 55.3->61.6, max_delta +6 (V) | FP | double-report | same blocks | Same issue as dup_blocks. |
| cmd | duplication_pct 61.6, max 40 (V) | FP | double-report | same blocks | Same issue, a third time. The package was already at 55.3 > 40. |
| cmd | globals 9->10 (V) | FP | other | cmd/report.go:10 (`flagOutput`) | A pflag-bound variable, the usual cobra pattern. It is written only by flag parsing and never read, since viper reads the value. If anything, the problem is that it is dead, not that it is hidden state. The other 9 are cobra command vars and logger/verbose. |
| cmd | has_tests false->false (V) | LEGACY | - | cmd, 529 SLOC | An untested cobra layer. The change did add untested branching, but the dup finding already covers that. |
| cmd/values | untested_exports 1, no baseline (V) | FP | covered-export | cmd/values/output.go:29 (`Output.Type`) | `Output` implements `pflag.Value` (String/Set/Type). `flag.Value` is on the closed list but `pflag.Value` is not, so `Type()` is counted even though cobra calls it. |
| internal/app | has_tests false->false (V) | LEGACY | - | internal/app/report.go:92-100 | +23 lines of call-site changes in a 313-SLOC untested package. Failing it is harsh. Note that the change has a correctness bug the gate does not target: it always renders `Text()` but uploads the file as `report.<output>` (report.go:204). |
| internal/output | has_tests false->false (V) | LEGACY | - | internal/output, 213 SLOC | Already untested. It overlaps the untested_exports row below, which carries the real point. |
| internal/output | untested_exports 5->9 (V) | TP | - | internal/output/audit.go:41, entity.go:38, role.go:38 (`*.CSV`) | The change added a new CSV output path (writer, header, rows, error wrapping) with no test anywhere in the module. These methods on unexported types are reachable through the `Output` interface (output.go:5). |
| pkg/config | duplication_pct 51->56.3, max 40 (V) | FP | over-ceiling-growth | pkg/config/config.go:53-62, 74-84, 94-104 | The three `Validate` methods each gained the same `if X.Output == "" { return err }` line. They are trivial and parallel, and the package was already over 40. (The new check also rejects configs where the report is disabled, but that is a behaviour bug, not maintainability.) |
| pkg/config | has_tests false->false (V) | LEGACY | - | pkg/config, 112 SLOC | Pre-existing. The change added 12 lines. |
| cmd | internal_imports 8->9 (W) | FP | other | cmd/report.go:6 (imports cmd/values) | The +1 comes from extracting a 31-line flag-value package. The metric penalises good factoring. It is only a warning. |
| internal/app | internal_imports 9 (W) | LEGACY | - | unchanged | Warning only. |
| internal/controller | internal_imports 8 (W) | LEGACY | - | package not touched | Warning on an untouched package. |

**Missed (kubesentry):** internal/output now has the `Text()` and `CSV()` bodies copied three times (audit.go:26-65, entity.go:24-58, role.go:24-58; entity.go and role.go are near-identical over lines 1-64). The gate saw dup_blocks fall 6->5 and duplication_pct fall 83.3->73.7, because the blocks merged and SLOC grew, so the new copy-paste passed. duplication_pct 73.7 is also far over max 40 but was not flagged, because it decreased.

## ai-orchestration (base e88b055, HEAD~15)

| package | rule | verdict | cause | evidence | note |
|---|---|---|---|---|---|
| `<module>` | dup_blocks_cross_pkg 2->4 (V) | FP | boilerplate-dup | internal/llm/tokens.go:10-22 ~ internal/tool/registry.go:13-26; internal/tool/permissions.go:59-74 ~ internal/state/ephemeral.go:24-39 | The new pairs are the "struct{mu; map} + NewX makes map + Lock/defer Unlock" idiom and the "os.ReadFile + Unmarshal + wrap errors" idiom. The cited lead pair (agent/registry.go vs skill/registry.go) is old: neither file changed in the window. |
| internal/agent | dup_blocks 0->1 (V) | TP | - | internal/agent/coordinator.go:11-33 ~ internal/agent/executor.go:7-29 | Two structs with identical fields (`id`, `definition`, `scratchpad`), the same constructor shape and four identical accessors. A reviewer would ask for an embedded `baseAgent`. The change is small but real. |
| internal/agent | duplication_pct 0->22.5 (V) | FP | double-report | same block | Same issue as the dup_blocks row. |
| internal/llm | max_nesting 4->5 (V) | FP | other | internal/llm/compaction.go:112-138 (`formatHistory`) | for > for > switch > if nil > if IsError: a readable 25-line message formatter. Extracting `formatBlock` would be optional. The rule fires at +1 with no floor. |
| internal/testutil | dup_blocks 1, no baseline (V) | FP | boilerplate-dup | internal/testutil/mock_tool.go:45-57 | The `WithResult` and `WithError` mock builder methods, each setting `execFunc` to a closure. |
| internal/testutil | has_tests false, no baseline (V) | FP | covered-export | internal/testutil (mock_llm.go, mock_tool.go, mock_hooks.go) | A test-support package should not need its own tests. Section 6.4 exempts such packages only in untested_exports, and only when other tests use them. |
| internal/testutil | untested_exports 23, no baseline (V) | TP | - | `grep -r internal/testutil` finds no importer | Right result for the wrong reason. Nothing in the module imports testutil, so 211 lines of mocks are dead code. A reviewer would ask to use or drop them. The message ("a rebuild would have to reverse-engineer their behavior") misstates the problem. |
| internal/tool | cognitive_p90 1->5 (V) | FP | tiny-ratchet | internal/tool/dispatcher.go:50-121 | A p90 of 5 is low. The package went from 5 to 11 functions. |
| internal/tool | globals 0->1 (V) | FP | immutable-global | internal/tool/permissions.go:22 (`ToolPermissions`) | A literal lookup map that is never written in the package. Caveat: it is exported and aliased into every `DefaultPermissions()` matrix (permissions.go:52), so an importer could mutate the permission policy. A security-minded reviewer might ask to unexport or copy it. The gate's message does not say this. |
| internal/tool | max_nesting 1->2 (V) | FP | tiny-ratchet | internal/tool/dispatcher.go | Depth 2. |
| internal/tool/builtin | dup_blocks 8, no baseline (V) | FP | boilerplate-dup | codesearch.go:35-85 ~ fileread.go:17-67; fileedit.go:1-60 ~ fileread.go:1-60 | Mostly `tool.Tool` interface scaffolding (NewX / Name / Description / JSON Schema literal / unmarshal params followed by `ToolResult{IsError:true}` returns). One small real helper candidate: the default-path / `os.Stat` / is-dir validation repeated in codesearch.go:82-96 and glob.go:56-70. |
| internal/tool/builtin | duplication_pct 55.6, max 40, no baseline (V) | FP | double-report | same blocks | Same issue as the dup_blocks row. |
| internal/tool/hooks | dup_blocks 1, no baseline (V) | FP | boilerplate-dup | internal/tool/hooks/chain.go:26-39 | `AddPreHook`, `AddPostHook` and `AddStopHook`: three typed one-line appenders. |

**Missed (ai-orchestration):** nothing material beyond what is noted. The log line "skipped package internal/tool/hooks: build constraints exclude all Go files" refers to the baseline, where the directory held only `.gitkeep`. The message is misleading.

## Summary

| repo | findings | TP | FP | LEGACY |
|---|---|---|---|---|
| goKMS (12 V + 5 W) | 17 | 0 | 10 | 7 |
| kubesentry (11 V + 3 W) | 14 | 2 | 6 | 6 |
| ai-orchestration (13 V) | 13 | 2 | 11 | 0 |
| **total** | **44** | **4** | **27** | **13** |

Counting violations only (36): TP 4, FP 24, LEGACY 8. 6 of the 8 legacy violations are has_tests false->false.

| FP cause (pooled) | count |
|---|---|
| boilerplate-dup | 7 |
| tiny-ratchet | 4 |
| double-report | 4 |
| other | 4 |
| over-ceiling-growth | 3 |
| covered-export | 3 |
| immutable-global | 2 |
| no-baseline | 0 |

## Observations

- **Dup findings point at the wrong block and miss new copy-paste.** The suggestion cites the first block in file order, which is usually old or boilerplate: cobra preamble audit.go:1-28, crypto.go:64-71, the legacy agent/skill registry pair. Meanwhile the block the change added sits further down the list. Worse, the count and percentage ratchets can fall while copy-paste grows: kubesentry internal/output triplicated Text/CSV and still went 6->5 blocks and 83->74%. Gating on "duplicated lines added in this diff" and citing those first would catch the real cases and skip the old ones.
- **Most dup noise is interface or registration scaffolding.** Examples: import blocks, `Name/Description/Schema` methods, mux and flag registration, `if err != nil { return ..., err }` chains, typed one-line appenders, mock `WithX` builders. These made up 7 of 27 FPs, and they inflate the totals behind the double-reported duplication_pct rows. One issue currently produces up to three lines (dup_blocks, duplication_pct delta, duplication_pct max); collapsing them would remove 4 FPs.
- **`max_delta: 0` with no floor fires on trivially small values and on every new package.** Examples: nesting 1->2, cognitive_p90 1->8 where p90 is just the max of a few functions, and "(no baseline)" packages failing on their first duplicate. A floor (for example nesting delta only at ≥4, p90 delta only above about 10), and judging no-baseline packages against absolute ceilings only, would remove about 6 FPs here without losing any TP.
- **has_tests and untested_exports disagree about tests in other packages.** untested_exports reads module-wide tests, but has_tests ignores them. So goKMS internal/auth and internal/kms fail has_tests even though test/admin_roles_test.go drives exactly the changed authz path. At the same time, untested_exports cannot see coverage through HTTP or connect, so every `kms.Service` method reads as untested. Separately, `pflag.Value` is missing from the closed interface list (`Output.Type`). And has_tests should not apply to test-support packages: internal/testutil was flagged by both rules, which was right only because it is dead code.
- **Level rules fail legacy packages on any touch, and allowances are inconsistent.** has_tests false->false is 6 of the 8 legacy violations. For capacity growth over a ceiling, sloc allows +100 and tokens +1000 (both warnings), largest_file_sloc allows only +5 (fail), and exported_symbols allows nothing (fail on +2 for one RPC). So one small feature in an oversized package fails on two of four size metrics and only warns on the other two.

---

# Adjudication B: astimate on ztp (`check . --all --base HEAD~20`, base 130d2c8)

Diff judged: `git diff 130d2c8` (working tree; the uncommitted changes touch no `.go` files). 47 Go files, +2726/-202.

## 1. Per-finding verdicts

| package | rule | verdict | cause | evidence | note |
| --- | --- | --- | --- | --- | --- |
| `<module>` | dup_blocks_cross_pkg 99->117 | TP | - | pkg/recording/wsframe/extractor.go:15-98 is an 84-line copy of internal/protocol/kubernetes/wsframe.go:12-105 (renamed type, `Feed` -> `Filter`); pkg/recording/protocol/ssh/orchestrator.go:212-232 and tcp/orchestrator.go:90-101 repeat the same format switch and the same `pcapngStreamRecording` wrapper type (ssh:378-392 = tcp:204-218) | Of 18 net new blocks I sampled 20 that overlap added lines: real copies are wsframe extractor (#36), the ssh/tcp recording-setup switch and wrapper types (#86, #87, #89, #90), the tcp relay `once/setErr` scaffolding copied from internal/tunnel/relay.go:26-42 (#44), and `compliance.StreamRecord` mirroring `session.Stream` field for field (sessions.go:116-126 vs session/session.go:20-30, #51). Boilerplate: sqlite open/err/`os.Exit` in two mains (cmd/broker/main.go:118-126 vs cmd/compliance-ui/main.go:50-57, #3), `Option`/`WithLogger` shells (sqlite/store.go:26-39 vs session/manager.go:21-33, #32, #109), Recording.Write mutex/closed guard (pcapng/writer.go:404-412 vs har/writer.go:171-180, #57, #61, #62), one-entry rule tables (protocol/http/rules.go:1-13 vs kubernetes/rules.go, #76, #77), struct field-copy converters (compliance/sqlite_streams.go:33-42, #52, #53, #55), wait-or-ctx select (tcp/orchestrator.go:137-146 vs internal/protocol/ssh/handler.go:162-171, #40). The suggestion names cmd/benchmark/main.go:41-49 vs cmd/broker/main.go:77-85, a legacy logger-setup block the window did not add. |
| cmd/broker | dup_blocks 3->4 | FP | boilerplate-dup | cmd/broker/main.go:43-47 | The new block is the `flag.String(...)` registration run, which matches itself one line down (43-46 vs 44-47) once a `-db` flag was added. |
| cmd/broker | internal_imports 20->21 (max 10) | FP | over-ceiling-growth | cmd/broker/main.go:32,39 | This is the composition root. It added `pkg/storage/sqlite` and `rechttp` and dropped `storagetest`, which removed a test fake from production wiring (an improvement). `over_max_delta: 0` means any change to main.go's wiring fails. |
| cmd/compliance-ui | cognitive_p90 5->9 | FP | tiny-ratchet | cmd/compliance-ui/main.go:45-66 | There is one function (`main`), so p90 is just main's score. It gained an `if stat exists { open sqlite } else { dir }` branch. A reviewer would accept it, or at most suggest a helper. |
| cmd/compliance-ui | max_nesting 1->2 | FP | tiny-ratchet | cmd/compliance-ui/main.go:51 | Nesting reaches 2 because of an `if err` inside the `if os.Stat` above. This is the same edit as the row above. |
| internal/broker | dup_blocks 6->7 | FP | boilerplate-dup | internal/broker/server.go:192-199 | The new block is the `mac.Write([]byte(x)); mac.Write([]byte{0})` chain in `computeSessionID`, which matches itself shifted by two lines. The suggestion points to the legacy option funcs at options.go:24-60. |
| internal/protocol/httpproxy | cognitive_p90 14->18 | FP | other | internal/protocol/httpproxy/forward.go:29-85 | The package has about 11 functions, so p90 is close to the second-largest function. `ServeHTTP` is a linear handler; its +4 comes from a 3-clause `needsChunked` predicate and an if/else around the body copy. A reviewer would not ask for a split. (The real defect, `writeChunked` ignoring write errors at :87-101, is not a complexity issue.) |
| internal/tunnel | untested_exports 0->1 | FP | covered-export | internal/tunnel/stream_readwriter.go:38; internal/protocol/tcp/handler_test.go:231 | `CloseWrite` is a one-line delegation. `TestTCPHandler_Handle_HalfCloseWithRecording` drives it through tcp `halfClose`'s anonymous `closeWriter` interface (pkg/recording/protocol/tcp/orchestrator.go:186-193). The call is in production code, so the rule's interface-dispatch refinement cannot see it. |
| pkg/compliance | dup_blocks 11->14 | FP | boilerplate-dup | pkg/compliance/sqlite_streams.go:34-42; store_sessions.go:132-141 vs 212-221 | The new blocks are struct field-copy converters and RFC3339 `IsZero` formatting. The suggestion names legacy compliance.go:38-51. The real smell behind them is three parallel stream structs (session.Stream -> StreamRecord -> StreamSummary), and the finding does not name it. |
| pkg/compliance | exported_symbols 54->62 (max 60) | TP | - | pkg/compliance/sessions.go:70-126; sqlite_streams.go:13-22 | The change crossed the ceiling with avoidable surface: `StreamRecord` duplicates `session.Stream`, and `SQLiteStreamLister`/`NewSQLiteStreamLister` put a concrete sqlite adapter inside the domain package (it now imports `pkg/storage/sqlite`). A reviewer would ask to move or trim these. |
| pkg/compliance | sloc 877->1012 (max 1000) | FP | double-report | pkg/compliance/* | It crossed by 12 lines in the same change as the exported_symbols row, with the same "split" advice. On its own merits a reviewer would not block on 1012 SLOC. |
| pkg/compliance | untested_exports 3->6 | TP | - | pkg/compliance/store_sessions.go:161-190; sqlite_streams.go:18-28 | Three of the six are new: `RecordingFileByStream` (new stream-specific lookup reached via server.go:141-155 `?stream=`), `NewSQLiteStreamLister` and `ListSessionStreams`. No test anywhere hits `?stream=` or the sqlite lister; the new test uses a `fakeStreamLister`. The legacy three (`WithLogger`, `WithRiskScorer`, `WithScorerLogger`) are option funcs. |
| pkg/recording/format/pcapng | untested_exports 0->3 | FP | covered-export | pkg/recording/format/pcapng/writer.go:378-414; pkg/recording/protocol/tcp/orchestrator_test.go:220 | `NewRecording` and `Write` are thin wrappers over the well-tested `pcapngWriter`. `TestTCPOrchestrator_PCAPNG_ValidFormat` parses the output with a pcapng reader, but it reaches them through production code. `WithMetadata` is called nowhere in the module, so it is dead and orchestrator pcapng files carry no ZTP metadata. The finding is right for the wrong reason there. |
| pkg/recording/pipeline | untested_exports 0->1 | FP | covered-export | pkg/recording/pipeline/plan.go:65; pkg/recording/session/manager.go:104; pkg/recording/protocol/ssh/orchestrator_test.go:184 | `Ext` is a 5-arm switch. The ssh orchestrator tests assert `sftp-operations-*.jsonl` and similar blob names, which come from `Ext`. (Missed: it duplicates `BlobName`'s switch at plan.go:47-61.) |
| pkg/recording/protocol/http | dup_blocks 1->3 | FP | boilerplate-dup | pkg/recording/protocol/http/orchestrator.go:35-49 | The suggestion names the `WithLogger`/`WithFlushInterval`/`WithFrameFilter` shells. The other new block is the asciicast/pcapng switch arms at :356-369. That switch is the third copy of the orchestrator format factory, which matters only across packages (see the module row). |
| pkg/recording/protocol/http | untested_exports 3->5 | TP | - | pkg/recording/protocol/http/orchestrator.go:47, 403-410; rules.go:8 | `WithFrameFilter` is an option func, but it is the only way into the new filter branch of `orchRecConn.Read`/`Write` (filtered S2C, C2S suppressed). That branch is the K8s exec corruption fix, and no test sets a filter; wsframe has no tests either. `DefaultHTTPRules` is a trivial one-entry table and on its own would be FP. |
| pkg/recording/protocol/ssh | dup_blocks 3->7 | TP | - | pkg/recording/protocol/ssh/orchestrator.go:378-392 vs 396-410; 215-221 vs 223-229 | `pcapngStreamRecording` and `asciicastStreamRecording` are identical apart from the inner field type; both inners implement `recording.Recording`, so one wrapper would do. The switch arms repeat too. A reviewer would ask to collapse them. The pointer (207-210 vs 264-267, two `OpenRecordingWithPrefix` calls) is the weakest pair. |
| pkg/recording/protocol/ssh | duplication_pct 9.9->17.6 | FP | double-report | same blocks as dup_blocks | Same blocks and same suggestion text as dup_blocks. |
| pkg/recording/protocol/ssh | untested_exports 14->16 | FP | covered-export | pkg/recording/protocol/ssh/orchestrator.go:384, 402 | The additions are `Write` methods on unexported wrapper types that satisfy `recording.Recording`. The `.cast` tests (orchestrator_test.go:98,155) reach the asciicast one. The real gap is that the ssh PCAPNG path has no test, and the finding does not say so. |
| pkg/recording/protocol/ssh/sftp | exported_symbols 126->127 (max 60) | FP | over-ceiling-growth | pkg/recording/protocol/ssh/sftp/message.go:114 | One `Message.Encode` was added; the 126 is mostly SSH_FXP opcode constants. Note: `Encode` is used only by ssh/orchestrator_test.go:229-232, so it is a production export that exists for a test. |
| pkg/recording/protocol/tcp | cognitive_p90 6->14 | TP | - | pkg/recording/protocol/tcp/orchestrator.go:76-160 | `Relay` now inlines recording setup (if / else-if / switch / if) in front of the relay goroutines. A reviewer would ask to extract `openRecording()`, which would also remove the copy shared with ssh. |
| pkg/recording/protocol/tcp | dup_blocks 0->2 | FP | boilerplate-dup | pkg/recording/protocol/tcp/orchestrator.go:118-128 vs 128-137; 166-176 vs 200-210 | The pairs are the symmetric server->client / client->server goroutines and two small adapter type shells. The copy worth fixing is the one shared with ssh, which only the module row counts. |
| pkg/recording/protocol/tcp | duplication_pct 0->15.1 | FP | double-report | same blocks as dup_blocks | Same blocks and same text as dup_blocks. |
| pkg/recording/protocol/tcp | max_nesting 3->5 | TP | - | pkg/recording/protocol/tcp/orchestrator.go:85-96 | This is the format switch nested inside `else if h != nil` inside `if len(plan.Outputs)`. It is the same function and fix as the cognitive_p90 row, but it is a legitimate separate signal. |
| pkg/recording/protocol/tcp | untested_exports 3->4 | FP | covered-export | pkg/recording/protocol/tcp/orchestrator.go:210; orchestrator_test.go:220 | `pcapngStreamRecording.Write` is a one-line delegation on an unexported type, exercised by the PCAPNG format test. |
| pkg/recording/session | untested_exports 2->3 | FP | covered-export | pkg/recording/session/manager.go:85; pkg/recording/protocol/ssh/orchestrator_test.go:184,347,392 | The ssh tests assert prefixed blob names (`sftp-operations-`, `scp-operations-`, `port-forward-channels-`) that only `OpenRecordingWithPrefix` produces. `OpenRecording` now delegates to it. |
| pkg/recording/wsframe | untested_exports 2 (no baseline) | TP | - | pkg/recording/wsframe/extractor.go:18,29; internal/protocol/kubernetes/wsframe_test.go:59-163 | The package is genuinely new (it is absent at 130d2c8). The extractor was copied out of internal/protocol/kubernetes, but the tests stayed behind on the old copy, which is now dead production code. A reviewer would ask to move the tests and delete the original. |
| pkg/storage/sqlite | dup_blocks 9 (no baseline) | FP | boilerplate-dup | pkg/storage/sqlite/store.go:53-61 | The suggestion names the `PRAGMA` exec/close/wrap-error chain. The rest is CRUD idiom: lock/ExecContext/wrap, `WHERE 1=1` filter appends (Query vs ListStreams), the rows loop, and Close*/RowsAffected. The one copy worth a review comment is `scanSessionRecord` vs `scanSessionRecordRows` (store.go:333-359), fixable with a row-scanner interface. The package really is new. |
| pkg/storage/sqlite | duplication_pct 53.2 (max 40, no baseline) | FP | double-report | same blocks as dup_blocks | Same blocks; 53% is inflated by overlapping windows over SQL idiom in a single 359-line file. |
| pkg/storage/sqlite | untested_exports 1 (no baseline) | FP | covered-export | pkg/storage/sqlite/store.go:33 | `WithLogger` is an option func; store_test.go (436 lines) covers the behavior. |
| internal/benchmark (warn) | internal_imports 8->8 | LEGACY | - | not in diff | `--all` reports an untouched package. It is non-failing, so that is fine. |
| internal/broker (warn) | internal_imports 12->12 | LEGACY | - | unchanged | It was over the ceiling at baseline and is unchanged. The warning is appropriate and does not fail. |
| internal/protocol/kubernetes (warn) | internal_imports 7->8 | FP | other | internal/protocol/kubernetes/handler.go:93 | The +1 import is `pkg/recording/wsframe`. Eight internal imports for a handler is ordinary, and nobody would act on it. |
| pkg/recording/pipeline (warn) | exported_symbols 58->59 | LEGACY | - | pkg/recording/pipeline/plan.go:65 | `Ext` adds one; the count is driven by pre-existing enum constants. A non-failing notice is reasonable. |
| pkg/recording/protocol/http (warn) | internal_imports 7->9 | FP | other | pkg/recording/protocol/http/orchestrator.go imports | The new imports are `format/asciicast` and `format/pcapng`, a symptom of the inlined format switch. An import count is not how a reviewer would see that problem. |
| pkg/recording/protocol/ssh (warn) | internal_imports 9->10 | FP | other | pkg/recording/protocol/ssh/orchestrator.go imports | `format/pcapng` was added. Same reasoning as the http row above. |
| pkg/recording/protocol/ssh/sftp (warn) | sloc 1142->1154 | LEGACY | - | sftp/message.go:114-124 | It was over the ceiling at baseline and grew 12, within the allowance. The policy is reasonable. |
| pkg/recording/protocol/ssh/sftp (warn) | tokens_est 79% | LEGACY | - | sftp/message.go | The value comes from the existing package size. |

## 2. Summary

| verdict | count |
| --- | --- |
| TP | 10 |
| FP | 23 |
| LEGACY | 5 |
| **total** | 38 (30 violations, 8 warnings) |

Violations only: 10 TP, 20 FP, 0 LEGACY. Warnings: 0 TP, 3 FP, 5 LEGACY.

| FP cause | count |
| --- | --- |
| covered-export | 7 |
| boilerplate-dup | 6 |
| double-report | 5 |
| other | 4 (httpproxy p90; three internal_imports warnings) |
| over-ceiling-growth | 2 |
| tiny-ratchet | 2 |
| immutable-global | 0 |
| no-baseline | 0 (wsframe and sqlite really are new) |

## 3. Observations for the astimate maintainer

- **Duplication points at the wrong block, and some real copies go unreported.** The real copies in this window are the 84-line wsframe extractor and the ssh/tcp/http recording-setup switch with its wrapper types. They are cross-package, so each package's `dup_blocks` suggestion instead names whichever in-package pair sorts first. That is usually option-func shells, a `flag.String` run, an `err`/`PRAGMA` chain, or a call sequence that matches itself shifted by two lines (`mac.Write` x5). The module rule names a legacy block in cmd/benchmark. Two changes would help: rank suggested blocks by size times "added in this change", and suppress a block that overlaps itself.
- **`dup_blocks` and `duplication_pct` fire together with identical text** (ssh, tcp, sqlite), and `sloc` plus `exported_symbols` both say "split" for the same crossing. That makes 5 of the 23 FPs double reports. Report one finding per block set, or fold `duplication_pct` into `dup_blocks` when both trip.
- **`untested_exports` misses coverage that reaches through production code.** The coverage it misses runs through production code into the target: `halfClose` taking an anonymous `CloseWrite` interface, orchestrators calling `pcapng.NewRecording`, `OpenRecording` delegating to `OpenRecordingWithPrefix`. It also counts exported-name methods on *unexported* types (`pcapngStreamRecording.Write`) and every `WithX` option. In this run, 7 of the 9 new-untested findings fell into these classes. The two that mattered (`RecordingFileByStream`, wsframe) were reported together with them and are easy to overlook. Consider excluding methods of unexported types that satisfy a module interface, and option funcs whose returned closure only assigns a field.
- **Capacity rules with `over_max_delta: 0` fail `main` packages on every wiring change** (cmd/broker internal_imports 20->21 while removing a test fake). The `internal_imports` ceiling of 10 also produces warnings for ordinary protocol handlers. Composition roots (`package main`) probably need an exemption or a higher ceiling for `internal_imports`.
- **Gaps the gate cannot see.** The move-then-copy left internal/protocol/kubernetes/wsframe.go as dead production code kept alive only by its tests. `pcapng.WithMetadata` is exported and called nowhere. `sftp.Message.Encode` is exported only for another package's test. `compliance.StreamRecord` mirrors `session.Stream`, and `pkg/compliance` now imports concrete `pkg/storage/sqlite`. `pipeline.Ext` duplicates `BlobName`'s switch below the duplicate-block threshold. A "production export or function referenced only from tests or from nowhere" metric would catch three of these and fits astimate's rebuild framing.

---

# Adjudication C: go-starter and mark (`check . --all --base HEAD~20`)

Method: every finding was re-judged against the base commit's real metrics. I ran `astimate assess --json` on each package in a base worktree and at HEAD (the worktrees have since been removed), applied the default thresholds from `astimate config init` and the evaluation logic in `internal/gate/evaluate.go` (density / capacity / requirement), and opened the cited code.

## go-starter (base cfb326ea, 20 commits, mostly deletions: 446 lines added, 1,484 removed in the touched packages)

| package | rule | verdict | cause | evidence | note |
| --- | --- | --- | --- | --- | --- |
| internal/platform/database/postgres | duplication_pct 26.8 -> 34.1 | FP | other (denominator shrink) | metrics.go:25-37, 54-109, 120-131, 146-158; pool.go:68-82 | The same 6 legacy blocks exist at base and head (pool.go:52-59 only moved to 68-75). sloc fell from 406 to 320 because options.go and code in postgres.go were deleted, so the ratio rose without any duplicate being added. |
| internal/platform/http/server | cognitive_p90 3 -> 11 | FP | tiny-ratchet | webserver.go:89 applyDefaults, :127 New, :192 Run | func_count fell from 21 to 6 when the 15 trivial `WithX` option funcs were deleted. Nearest-rank p90 over 6 functions is the maximum (New/Run at about 11), so the rise comes from the denominator. The refactor reduced complexity. |
| internal/templateinit | globals 4 -> 5 | FP | immutable-global | initializer.go:49 `removedPaths` | `removedPaths` is a `[]string` literal that is only ranged over (initializer.go:118, :287) and never written. The other four are `regexp.MustCompile` values; `modulePartPattern` was renamed to `modulePathPattern`. |
| cmd (warning) | internal_imports 8 -> 8, 80% | LEGACY | n/a | cmd/*.go | Unchanged. A warning is a reasonable policy here. |
| internal/app (warning) | internal_imports 13 -> 13 over 10 | LEGACY | n/a | internal/app/app.go | Over the ceiling but unchanged. A warning that does not fail is the right policy. |

## mark (base 9a26f657; module path renamed to `/v16` in 2fdcab2, inside the window)

All `(no baseline)` findings are artifacts of the rename. Every package except ascii, comment and confluence/confluencetest exists at the base under `github.com/kovetskiy/mark/...`. Verdicts below use the real base values (base>head).

| package | rule | verdict | cause | evidence | note |
| --- | --- | --- | --- | --- | --- |
| `<module>` | dup_blocks_cross_pkg 2 -> 3 | TP | | ascii/ascii.go:90-99 vs d2/d2.go:64-74 | This is the third copy of "append scale bytes to the source, then `attachment.GetChecksum`" (mermaid/d2/ascii); the comment at ascii.go:93 even says "matches mermaid/d2 pattern". The module row's key is not a path, so its baseline was found. |
| . | changed_func_cognitive_max 140 | TP | | mark.go:140 ProcessFile | ProcessFile was modified in eight hunks (dry-run PageID/folder branches, move logic) and was already the dominant function at base (base p90 78). It is now 140. |
| . | dup_blocks 1 -> 5 (real) | TP | | mark.go:250-260 / 322-333 / 390-398 / 683-689 | `GetPageByID` + nil check + `checkPageNotArchived` is pasted 4 times. The dry-run branch (249-286) re-implements the real path. Would fire with the real baseline. |
| . | internal_imports 10 -> 11 | FP | other (composition root +1 import) | mark.go:17-28 | The only new import is `comment`. The root package is the CLI's composition root, and crossing 10 by one import is not something a reviewer would block. The finding would still fire with the baseline. |
| . | untested_exports 2 -> 1 (real) | FP | no-baseline | mark.go:92 Run | Run was untested at base and ProcessFile gained tests, so the real delta is -1. |
| attachment | cognitive_p90 24 -> 24 | FP | no-baseline | attachment/attachment.go | Only the import path changed (4 lines). |
| attachment | dup_blocks 3 -> 3 | FP | no-baseline | attachment.go:50-60 | Unchanged legacy duplicates. |
| attachment | untested_exports 4 -> 4 | FP | no-baseline | attachment.go | Unchanged. |
| comment (new) | cognitive_p90 24 | FP | other (p90 on a 3-function package = max) | comment/comment.go:50 MergeMarkers | The p90 is a single 45-line function with a nested search loop, covered by 161 lines of tests. It is barely over 20; borderline. |
| comment (new) | globals markerRe | FP | immutable-global | comment/comment.go:18 | A compiled `regexp.MustCompile`, never reassigned. |
| confluence | dup_blocks 13 -> 23 (real) | TP | | confluence/folder.go:277-321 vs 333-378; api.go:138-146 vs 148-156 | findFolderInChildren and findFolderInPageChildren are the same 45-line paginated loop except for the "folders"/"pages" path segment. The Bearer-header setup is copied for rest and restV2. |
| confluence | largest_file_sloc 738 -> 774 | FP | over-ceiling-growth | confluence/api.go | api.go was already over 600 at base and grew 36 lines (more than the 5 allowed). The new feature code went into folder.go, which is the right place. |
| confluence | sloc 738 -> 1164 | TP | | confluence/folder.go (504 new lines) | This change crossed the ceiling. About 90 of the new lines are the duplicated pagination loops; under the rule's own semantics this is real growth. |
| confluence | untested_exports 19 -> 27 (real) | TP (partial) | | folder.go:98-475, api.go:537 | 10 new untested names. FindFolder, FindFolderInSpace, CreateFolder, GetSpaceIDByKey and NewAPIWithClient are exercised through FindOrCreateFolder/ResolveFolder tests on the confluencetest fake server, but MovePageV2, CreatePageV2, GetFolderByID and GetPageBodyByID (which write to or read from Confluence) have no test path. |
| confluence/confluencetest (new) | untested_exports rewriteHost.RoundTrip | FP | covered-export | confluencetest.go:56 | A RoundTripper on an unexported type, exercised by every test that uses `confluencetest.NewAPI`. `http.RoundTripper` is not in the closed interface list. |
| d2 | globals renderTimeout 1 -> 1 | FP | no-baseline | d2/d2.go | Import-path-only change. |
| includes | globals 1 -> 1 | FP | no-baseline | includes/templates.go:22 | Package not touched at all in the window. |
| includes | has_tests false | FP | no-baseline | includes/templates.go | sloc 141 -> 141. With a baseline, `grew()` is false and the rule does not apply. |
| includes | untested_exports 2 -> 2 | FP | no-baseline | templates.go:29, :82 | Unchanged. |
| macro | globals 1 -> 1 | FP | no-baseline | macro/macro.go | Import-path-only change. |
| macro | has_tests false | FP | no-baseline | macro/macro.go | sloc unchanged at 168. |
| macro | untested_exports 2 -> 2 | FP | no-baseline | macro/macro.go | Unchanged. |
| markdown | dup_blocks 1 -> 3 (real) | FP | boilerplate-dup | markdown/markdown.go:49-90 | goldmark `AddOptions(WithASTTransformers/WithNodeRenderers(util.Prioritized(...)))` registration lines. Would fire with the baseline. |
| markdown | untested_exports 3 -> 3 | FP | no-baseline | markdown.go | Unchanged. |
| mermaid | globals 1 -> 1 | FP | no-baseline | mermaid/mermaid.go | Import-path-only change. |
| metadata | cognitive_p90 43 -> 45 (real) | LEGACY | | metadata/metadata.go:60 ExtractMeta | Two new `case` arms and one `if` on a 43-point legacy function. It fires because a density `max` fails any rise above 20, even +2 when the delta (3) allows it. Failing this is harsh. |
| metadata | dup_blocks 1 -> 1 | FP | no-baseline | metadata.go:97-112 | Unchanged block. |
| metadata | globals 2 -> 2 | FP | no-baseline | metadata.go:56-57 | Unchanged regexps. |
| page | cognitive_p90 18 -> 29 (real) | TP | | page/page.go:13 ResolvePage, page/ancestry.go:19 EnsureAncestry | Folder handling was threaded through ResolvePage as scattered `meta.Folder != ""` branches (blogpost guard, folderRoot, title assembly), +11 over the +3 allowed. |
| page | untested_exports 5 -> 5 | FP | no-baseline | page/*.go | Unchanged set. |
| parser | dup_blocks 2 -> 4 (real) | FP | boilerplate-dup | parser/colwidth.go:36-44 vs imageattr.go:17-25 | The goldmark `ast.Walk` callback prologue (`if !entering \|\| n.Kind() != ... { return ast.WalkContinue, nil }`) in two transformers. |
| parser | globals 6 -> 8 (real) | FP | immutable-global | parser/colwidth.go `colwidthRegexp`, `validWidthRegexp` | Both are compiled regexps that are never written. |
| parser | untested_exports 11 -> 15 (real) | FP | covered-export | parser/colwidth.go:31, imageattr.go:11 | NewColWidthTransformer/NewImageAttrTransformer and their Transform methods are exercised end to end by markdown/markdown_test.go TestCompileMarkdown (goldens) and TestCompileMarkdownCloudColWidth. colwidth_test.go tests the helpers. |
| renderer | changed_func_cognitive_max 86 | TP | | renderer/link.go:29 renderLink | The `ac:id:` branch was added as a new nested if/else inside the existing branch, with 10 more `Write`/err-check pairs. A reviewer would ask for a `renderPageIDLink` helper. |
| renderer | dup_blocks 39 -> 48 (real) | TP | | renderer/fencedcodeblock.go:171-315 | The d2/mermaid/ascii branches are three copies of a 48-line block (scale override, Process, Attach, align/layout/width, ExecuteTemplate with an anonymous struct); the ascii copy was added in this window. The message's "starting with blockquote.go:7-21" points at a legacy block instead. |
| renderer | duplication_pct 48.3 -> 48.6 | FP | double-report | same blocks as dup_blocks | Same issue reported under a second rule, at +0.3 points. |
| renderer | exported_symbols 62 -> 66 | FP | over-ceiling-growth | renderer/table.go | Already over 60 at base. The +4 is the new table renderer type, constructor and filter. |
| renderer | globals 2 -> 3 (real) | FP | immutable-global | renderer/table.go:14 `TableAttributeFilter` | An `html.AttributeFilter` built once and only read (table.go:46). |
| renderer | max_nesting 7 -> 7 | FP | no-baseline | renderer | Unchanged. With a baseline, `introduced` is false. |
| renderer | sloc 1241 -> 1495 | TP | | renderer/table.go (117 new lines), fencedcodeblock.go | It grew 254 lines while over 1000, past the 100 allowed. Deduplicating the code-block branches would remove about 100 of those lines. |
| renderer | tokens_est 13637 -> 16020 | FP | double-report | same growth as sloc | It crosses 16000 by 20 tokens, from the same growth already failed by `sloc`. |
| renderer | untested_exports 33 -> 35 (real) | FP | covered-export | renderer/table.go:20 NewConfluenceTableRenderer, RegisterFuncs | Exercised by the markdown goldens (TestCompileMarkdownCloudColWidth renders tables). |
| stdlib | dup_blocks 20 -> 20 | FP | no-baseline | stdlib/stdlib.go:49-62 | Import-path-only change (2 lines). |
| stdlib | duplication_pct 82.1 -> 82.1 | FP | no-baseline | stdlib.go | Unchanged (template-definition table). |
| stdlib | has_tests false | FP | no-baseline | stdlib.go | sloc unchanged at 364. |
| util | dup_blocks 15 -> 15 | FP | no-baseline | util/cli.go:57-68 | Count unchanged. |
| util | duplication_pct 54.3 -> 56.4 (real) | FP | boilerplate-dup | util/flags.go (new `folder`, `no-preserve-comments`, `ascii-scale` flags) | `&cli.StringFlag{Name, Value, Usage, Sources: cli.NewValueSourceChain(...)}` rows. Would fire with the baseline, because a density `max` fails any rise above 40. |
| util | globals 2 -> 2 | FP | no-baseline | util/flags.go `filename`, `Flags` | Unchanged. |
| util | untested_exports 4 -> 4 | FP | no-baseline | util/cli.go | Unchanged. |
| vfs | globals LocalOS 1 -> 1 | FP | no-baseline | vfs/ | Not touched. |
| vfs | untested_exports 1 -> 1 | FP | no-baseline | vfs/ | Not touched. |
| . (warning) | largest_file_sloc 443 -> 589, 98% | TP | | mark.go | mark.go grew 146 lines, almost all in ProcessFile. The warning is accurate. |
| renderer (warning) | internal_imports 7 -> 8, 80% | TP | | renderer/fencedcodeblock.go (new `ascii` import) | An accurate, harmless notice. |

Missed by the gate (secondary):
- mark: `metadata.ExtractMeta` grew to 9 positional parameters, 3 of them adjacent bools/strings (metadata.go:60). Every caller was edited and the order is easy to swap. No parameter-count metric exists.
- mark: renderLink writes the `anchor` and `pageID` from markdown into XML attributes without escaping (link.go). This is a correctness issue, not maintainability, so it is out of scope.
- go-starter: nothing notable. The window is deletion-heavy, and the new `cmd/utils.go` removes duplication.

## Summary

| repo | findings (viol + warn) | TP | FP | LEGACY |
| --- | --- | --- | --- | --- |
| go-starter | 3 + 2 | 0 | 3 | 2 |
| mark | 51 + 2 | 12 (10 viol, 2 warn) | 40 | 1 |
| total | 58 | 12 | 43 | 3 |

FP by cause (pooled):

| cause | count |
| --- | --- |
| no-baseline | 25 (mark only) |
| immutable-global | 4 |
| other | 3 (postgres denominator shrink, mark root +1 import, comment p90 = max of 3 funcs) |
| covered-export | 3 |
| boilerplate-dup | 3 |
| over-ceiling-growth | 2 |
| double-report | 2 |
| tiny-ratchet | 1 |

Real mark violations after removing the rename artifact: 26 of 51 would still fire. Of those 26, 10 are TP, 1 is LEGACY and 15 are FP for other causes.

## Baseline key (mark)

Baseline rows are matched to head rows by full Go import path (`internal/baseline/baseline.go:10-16`, `Metrics(pkg string)` keyed by import path). The rename turned every `github.com/kovetskiy/mark/X` into `github.com/kovetskiy/mark/v16/X`, so no head package found its base row. Every package was then judged as new: ratchet-from-zero rules and absolute maxes applied, and `has_tests` applied with `grew()` true. Only the `<module>` row, whose key is not a path, kept its baseline.

## Observations for astimate maintainers

- **A module-path change voids the whole baseline.** Matching should fall back to the module-relative directory (or strip the old and new module path) when go.mod's module line differs between base and head. Without that, a `/vN` bump turns about half the report (25 of 51 here) into noise, including `has_tests` on untouched packages.
- **Ratio and percentile rules fire on deletion refactors.** `duplication_pct` rose 26.8 -> 34.1 with zero new blocks because sloc shrank, and `cognitive_p90` rose 3 -> 11 because 15 trivial option funcs were deleted (nearest-rank p90 of 6 functions is the max). Both should be gated on the numerator: a new duplicate block, or a changed function above the old p90.
- **A density `max` fails any rise of a legacy value already over it, even by a hair.** Examples: metadata cognitive_p90 43 -> 45, renderer duplication_pct 48.3 -> 48.6, util duplication_pct 54.3 -> 56.4 (from cli flag rows). Here the `max_delta` (3 or 6) is never consulted. Density maxes should get an over-max allowance like capacity rules do, or be judged only on new or changed code.
- **The "start with" pointer picks the first block in file order, not the new one.** renderer points at legacy blockquote.go:7-21 while the real regression is the triplicated 48-line d2/mermaid/ascii branch in fencedcodeblock.go:171-315. The suggestion should prefer blocks that are not in the baseline.
- **Remaining FP sources, one fix each:**
  - Only direct test references count, but the module's idiom is goldmark extension goldens in another package, which call through `markdown.CompileMarkdown`, and fake-server tests that reach exports through a non-test helper package (`confluencetest`).
  - Read-only lookup slices (`removedPaths`) and `html.AttributeFilter` values are still counted as mutable `globals`; never-written `regexp.MustCompile` results are counted by design (SPEC 6.5).
  - `sloc` and `tokens_est` double-report the same growth.

---

# Adjudication D: astimate on charmbracelet/crush

Run: `astimate check . --all --base HEAD~30` in /Users/copierson/Projects/crush (main @ 6d14dd93). Base 7d78d742 ("chore: standardize go toolchain on v1.26.6 (#3592)"). The window holds 31 commits (one is a merge). 12 of them touch Go files. Together they change 47 files (+2148/-286), of which 37 are Go files (+1877/-156).

## 1. Counts

- Reports: 76, which is 75 packages plus the `<module>` row. 5 packages were skipped because build constraints exclude all their files.
- Packages with at least one violation: 7. That becomes 8 if the module row counts. Exit code 3.
- Violations: 20.
  - dup_blocks 4
  - sloc 3
  - largest_file_sloc 3
  - tokens_est 2
  - changed_func_cognitive_max 2
  - exported_symbols 1
  - globals 1
  - untested_exports 1
  - duplication_pct 1
  - has_tests 1
  - dup_blocks_cross_pkg 1
- Warnings: 84, all on capacity rules: sloc 21, largest_file_sloc 17, tokens_est 17, exported_symbols 16, internal_imports 13.

## 2. Per-violation verdicts (aggregate window)

| package | rule | verdict | cause | evidence | note |
| --- | --- | --- | --- | --- | --- |
| `<module>` | dup_blocks_cross_pkg 268->270 | FP | boilerplate-dup | new blocks: internal/ui/model/keys.go:6-68 (struct of `key.Binding` fields, plus `key.NewBinding(...)` chains at 133-287) against internal/ui/styles/styles.go:73-588 (struct of style fields) and dialog key maps | Added by 08552fb9 (textarea selection keys). The reported location, agent.go:243, is a legacy block and not one of the new ones. |
| internal/agent/tools/mcp | largest_file_sloc 887->907 | FP | over-ceiling-growth | init.go:904-940 (sessionless handlers made conditional), init.go:981-998 (`unwrapTransport`) | Package is 1,596 sloc. Two unrelated bug fixes (cce88718, 82557e2a). A maintainer would not split a file for +20 lines of fix. |
| internal/config | exported_symbols 150->159 | FP | over-ceiling-growth | config.go:287 IsTransparent, :304 DiffMode consts, :317 ExitBanner type+consts, :501 IsSessionless; load.go:546 NormalizeOptions | This is the config-schema package, so every new user option adds exported consts. over_max_delta 0 means any new option fails. The violation is spread across 3 commits. |
| internal/config | globals 12->13 | FP | immutable-global | config.go:492 `knownSessionlessMCPs` map literal, never written | A read-only lookup table. Per SPEC 6.5 maps always count. |
| internal/config | largest_file_sloc 1121->1130 | FP | over-ceiling-growth | load.go:539-559 NormalizeOptions extracted from setDefaults | A refactor so remote configs get the same defaults. +9 lines in a file of 1,121. |
| internal/config | tokens_est 52953->54202 | FP | over-ceiling-growth | config.go +71 lines, mostly doc comments (sessionless, exit banner, IsTransparent) | Package is 3,467 sloc and 54k tokens. Three unrelated features each added under 1,000, so no single commit fails this rule (see section 4). Only the window aggregation crosses the ratchet. |
| internal/config | untested_exports 46->48 | FP | covered-export | new: load.go:546 NormalizeOptions, which load_test.go:278 exercises through setDefaults; config.go:287 IsTransparent, a 1-line nil-safe getter | The suggestion lists 5 legacy names (Attribution.JSONSchemaExtend, ...), not the 2 new ones. |
| internal/shellconfig | dup_blocks 30->31 | FP | boilerplate-dup | options.go:233-242: the `case "exit-banner":` validator arm mirrors the `scrollbar` and `diff` arms | Enum validation arms of 3 lines each. |
| internal/shellconfig | duplication_pct 48->49.4 | FP | double-report | same block as above, plus the mcp.go:56 `--sessionless` flag table row | Package was already 48% duplicated, which is the flag-spec tables. |
| internal/ui/dialog | changed_func_cognitive_max 64 | LEGACY | n/a | commands.go:157 Commands.HandleMsg was touched by 8661a1c4 (adds `if value != prevValue`) | The function was already over 50. A one-line Windows fix should not require refactoring a handler. Failing every touch is not reasonable policy. |
| internal/ui/dialog | dup_blocks 215->217 | LEGACY | n/a | the 8661a1c4 filter-guard fix applied identically to commands.go:231, models.go:224, notifications.go:157, reasoning.go:148, sessions.go:219 | The 5 copies predate the window, and the fix lengthened each one past the block threshold. Borderline TP: a reviewer could ask for an `updateFilter` helper at this moment. Failing is defensible but strict. |
| internal/ui/dialog | sloc 8228->8332 | FP | over-ceiling-growth | sessions.go +113 (90bc466d mouse support); the 4 other dialogs +~10 (8661a1c4) | Package is 8,332 sloc with 383 exports, so a split is genuinely warranted. But the +104 here came from 2 unrelated commits, each under 100, and neither fails per commit. |
| internal/ui/list | dup_blocks 3->4 | TP | n/a | highlight.go:60-75 `extractRows` re-implements the row and column-range loop of HighlightBuffer at highlight.go:211-230 | The fix (8f63ee59) copied 15 lines of range logic instead of sharing a helper. A reasonable review comment. |
| internal/ui/list | sloc 928->1014 | FP | other (marginal ceiling crossing by one bug fix) | highlight.go +145 lines (8f63ee59) | 1.4% over the ceiling. The corpus p90 is 1,224 and the ceiling was rounded down to 1,000. No maintainer splits a list widget over a bug fix. |
| internal/ui/model | changed_func_cognitive_max 385 | LEGACY | n/a | ui.go:706 UI.Update. 08552fb9 added 3 nested mouse-selection branches (+16, 369->385) and 90bc466d changed one call | The function really is a monster, but it was 369 before the window. Without a ratchet every future edit fails, so as configured this is not reasonable policy. |
| internal/ui/model | dup_blocks 116->118 | FP | boilerplate-dup | keys.go:6-72 key-map struct, keys.go:125-287 `key.NewBinding` chains, ui.go:2673-2691 Copy/Cut case arms, ui.go:3309/3384 help binding lists | Key-binding tables, plus two 8-line arms that differ by message and by Clear vs Delete. |
| internal/ui/model | largest_file_sloc 3970->4071 | FP | over-ceiling-growth | ui.go +150/-? (forwardMouseToTextarea :3495, pasteTextFromClipboard :4955, Copy/Cut arms) | ui.go is 4,071 lines in a 7,095-sloc package. Most growth is one feature (textarea selection, 08552fb9), with small parts from 3 others. |
| internal/ui/model | sloc 6974->7095 | FP | over-ceiling-growth | same as above | 08552fb9 alone is +123. Ordinary feature work in an already-huge package. |
| internal/ui/model | tokens_est 84923->86581 | FP | over-ceiling-growth | same as above | Same as above. |
| internal/ui/styles | has_tests | LEGACY | n/a | quickstyle.go:133,143: two `Selection:` style fields (08552fb9) | The package (1,548 sloc) never had tests. Two theme-table lines trip the rule because sloc "grew". This is not reasonable policy without a growth floor. |

## 3. Over-ceiling share at HEAD (75 packages)

| ceiling | packages over | share |
| --- | --- | --- |
| sloc > 1,000 | 18 | 24.0% |
| tokens_est > 16,000 | 16 | 21.3% |
| largest_file_sloc > 600 | 15 | 20.0% |
| exported_symbols > 60 | 12 | 16.0% |
| internal_imports > 10 | 11 | 14.7% |
| any of the five | 21 | 28.0% |
| any, among the 42 packages of at least 200 sloc | 21 | 50.0% |

The 21 packages over at least one ceiling hold 59,053 of the module's 71,001 sloc (83%). Most Go code in crush therefore lives in packages where every change is judged by the over-ceiling ratchet rather than by the ceiling.

## 4. Per-commit view (`astimate check . --base <c>~1`, detached worktree)

| commit | subject | lines changed (Go) | exit | violations -> verdict |
| --- | --- | --- | --- | --- |
| 90bc466d | session picker: add mouse support | 384 | 3 | dialog dup_blocks 215->216: FP boilerplate-dup (dialog Draw preamble re-chunked by the `s.bodyArea` insertion, sessions.go:271-289). model UI.Update 369: LEGACY (one call became `handleDialogMsg`). |
| 08552fb9 | feat: textarea selection (#3507) | 283 (295 all) | 3 | cross 268->271: FP boilerplate-dup (keys.go vs styles.go tables). UI.Update 385: LEGACY (+16). model dup 115->118: FP boilerplate-dup. model largest_file +103, sloc +123, tokens +1713: FP over-ceiling-growth (x3). styles has_tests: LEGACY. 7 violations. |
| 8661a1c4 | fix(windows): scrolling up on modifier keys alone (#3598) | 48 | 3 | Commands.HandleMsg 64: LEGACY. dialog dup 216->217: LEGACY (borderline TP). |
| cce88718 | fix(mcp): repair stdio startup diagnostics (#3634) | 100 | 3 | mcp largest_file 887->904: FP over-ceiling-growth (bug fix plus `unwrapTransport`). |
| a0fe47b6 | fix: return to the start of the line (#3635) | 7 (smallest Go) | 0 | none |
| 8f63ee59 | fix: wrapped messages counted as newlines (#3627) | 251 | 3 | list dup 3->4: TP. list sloc 928->1014: FP other (marginal crossing). |
| 82557e2a | fix(mcp): fix sessionless mcp servers (#3651) | 286 (313 all) | 3 | config exported 150->151: FP over-ceiling-growth. globals 12->13: FP immutable-global. shellconfig duplication_pct 48->48.1: FP boilerplate-dup (one flag-table row). |
| 773c3565 | resume: compact resume banner option | 139 (156 all) | 3 | cmd largest_file 791->815: FP over-ceiling-growth (reverted by the next commit's extraction). config exported 151->156: FP over-ceiling-growth. config untested 46->47 (`ExitBanner.Full`, one-line predicate): FP other (trivial). shellconfig dup_blocks and duplication_pct: FP boilerplate-dup and FP double-report. model exported 80->81 (`UI.Config` accessor): FP over-ceiling-growth. model largest_file +6 vs 5 allowed: FP over-ceiling-growth. model untested 55->56 (`UI.Config`): FP other (trivial accessor). 8 violations. |
| a83480eb | refactor: centralize TUI option defaults and extract exit banner rendering | 573 (largest) | 3 | config exported 156->159: FP over-ceiling-growth. config largest_file +9: FP over-ceiling-growth. config untested 47->48 (`NormalizeOptions`, tested via setDefaults): FP covered-export. UI.handleDialogMsg 86: LEGACY (the only edit swapped a `&&` chain for `IsTransparent()`, a simplification). This refactor extracted a tested package and shrank cmd, and it still fails. |
| 2edc4e68 | chore(legal): CLA signature | 8 (non-Go) | 0 | none |

Per-commit totals across the 9 Go commits run:
- 8 of 9 fail.
- 29 violations: 1 TP, 22 FP, 6 LEGACY.
- FP causes: over-ceiling-growth 12, boilerplate-dup 5, other 3, immutable-global 1, covered-export 1, double-report 1.

Not run: 3 small Go commits (db7fac6c test-only, 75962c48 xchroma +19 src, 460a877f stringext +4 src). The 18 non-Go commits change no packages and pass. Only 1 of 9 real code commits would have passed, and it was a 7-line fix.

## 5. Verdict summary

- Aggregate window, 20 violations: **TP 1, FP 15, LEGACY 4**.
  - FP by cause: over-ceiling-growth 8, boilerplate-dup 3, immutable-global 1, covered-export 1, double-report 1, other (marginal ceiling crossing) 1.
  - LEGACY (2 changed_func_cognitive_max, 1 dialog dup_blocks, 1 has_tests): failing is not reasonable policy for the 3 that are touch-triggered (cognitive x2, has_tests). The dialog dup_blocks one is defensible but strict.
- Per commit, 29 violations: TP 1, FP 22, LEGACY 6.

## 6. Observations for astimate's maintainer

1. **The over-ceiling ratchet is too tight for `exported_symbols` (0) and `largest_file_sloc` (5).** In crush, 50% of packages of at least 200 sloc are already over some ceiling, and they hold 83% of the code. So the ratchet, not the ceiling, decides almost every commit. Examples of what it fails:
   - Any new option in the config-schema package (150+ exports).
   - Bug fixes of +17, +9 and +6 lines in files over 600.

   Over-ceiling growth is 8 of 15 aggregate FPs and 12 of 22 per-commit FPs. A proportional allowance, or a non-zero exports delta, would remove most of them.
2. **`changed_func_cognitive_max` has no baseline treatment.** Any touch of a function already over 50 fails. That includes a one-line fix (Commands.HandleMsg 64) and a refactor that simplified the touched line (UI.handleDialogMsg 86). It needs the same over-max ratchet as the capacity rules: fail only when the function's own complexity rose by more than N. On UI.Update that would still catch 08552fb9's +16 if N is small.
3. **Window aggregation is not a proxy for per-PR behaviour.** `--base HEAD~30` produced violations that no single commit makes (config tokens_est +1,249, dialog sloc +104, each the sum of sub-threshold commits). It also missed per-commit violations that later commits undid (cmd root.go +24, model exported +1, UI.Config untested). False-failure estimates should come from per-commit replays.
4. **Locations and suggestions name legacy items instead of the new ones.**
   - The dup_blocks_cross_pkg location points at agent.go:243, which is not new. The new blocks are in keys.go and styles.go.
   - The untested_exports suggestion lists the alphabetically first five legacy names, not the newly untested NormalizeOptions and IsTransparent.
   - exported_symbols points at atomicwrite.go:1.

   An agent following these suggestions would edit the wrong code.
5. **Declarative code trips the density rules.** Struct-of-`key.Binding` and `key.NewBinding` chains match lipgloss style structs across packages. Flag-spec rows and enum-validation case arms count as duplication. A read-only map literal counts as a mutable global. `has_tests` fires on two theme-table lines in a legacy untested package. On this well-regarded module, the gate's only clear TP in 30 commits was one copied 15-line loop. A crush maintainer would switch the gate off as shipped.

---

# Adjudication E: astimate on shhh (`check . --all --base HEAD~30`)

Target: /Users/copierson/Projects/shhh, master at 12b5c978. Base (merge-base of HEAD~30) is bad6327c. The working tree's 2 uncommitted changes (`.shhh/agents/critic.toml`, `docs/document_orchestration.md`) contain no Go, so the gate's result is the same as the committed HEAD. The window holds 30 commits: 423 files, +15,341/-3,359. Three of them are package extractions done as git renames: a1d84dfc `internal/agent` -> `internal/agent/sidejobs`, 20c93884 `internal/cli` -> `internal/cli/doctor`, and 0d137dc2 `internal/observe` -> `internal/observe/record`. One adds a genuinely new package: dfee87dc `internal/calltier`.

jq is not installed on the host, so I did the counts with `python3 -I` over the same JSON. I also produced a self-check of the base tree (`check --all --base HEAD` at bad6327) so I could diff the baseline's cross-package blocks and per-package duplicate and untested lists against head.

## 1. Counts

| Item | Value |
| --- | --- |
| Rows in JSON | 83 (82 packages + `<module>` row); 3 more packages skipped for build constraints |
| Packages with >= 1 violation | 17 (18 counting `<module>`) |
| Packages new at head (no baseline) | 4: agent/sidejobs, calltier, cli/doctor, observe/record. Three of them are renames of existing code |
| Violations | 54 |
| Warnings | 111 (none has `severity: warn`; all are capacity `warn_at` or over-ceiling warnings) |

Violations by rule: dup_blocks 8, exported_symbols 8, untested_exports 8, sloc 7, tokens_est 7, internal_imports 5, globals 3, largest_file_sloc 3, dup_blocks_cross_pkg 2, duplication_pct 2, changed_func_cognitive_max 1.

Warnings by rule: tokens_est 31, sloc 28, largest_file_sloc 25, exported_symbols 23, internal_imports 4.

## 2. Per-violation verdicts

Abbreviations: ocg = over-ceiling-growth. "Moved" means the code arrived through a git rename (>= 93% similarity), so its history is in the parent package.

| package | rule | verdict | cause | evidence | note |
| --- | --- | --- | --- | --- | --- |
| `<module>` | dup_blocks_cross_pkg (max_delta, 805->860) | FP | other: package extraction | new blocks 114, lost 59; 70 of the 114 touch sidejobs/doctor/record; intra-package dup_blocks fell agent 73->31, cli 426->410, observe 30->21 | Most of the rise is duplicates that were already inside one package and became cross-package when that package was split. The cited block (agent/classifier.go:199-247 vs sidejobs/summary.go:218-266) is moved config-defaulting getters. A few real copies are among the 114 (structural/gitwrite.go:579 `rel` vs ui/chat/cmdwork.go:120 `workspaceRel`), but the finding does not name them. |
| `<module>` | dup_blocks_cross_pkg (max 450) | FP | double-report | same 805->860 | The baseline was already 805, well over 450, so any +1 produces this second finding. |
| internal/agent | internal_imports 12->14 | FP | other: split adds edge | agent/classifier.go:20 imports sidejobs; agent/tier.go imports calltier | Both new edges point at packages carved out of, or for, agent. agent itself shrank by 1,174 sloc. |
| internal/agent/sidejobs | dup_blocks 35 | FP | no-baseline | moved (account.go 99%, summary.go 96%, ...); account.go:1-59 vs title.go:1-57 | The repeated code is the sidejob preamble: imports, const block, Config struct. These blocks existed in agent before the move. |
| internal/agent/sidejobs | duplication_pct 52.7 | FP | no-baseline | same | Same moved blocks. |
| internal/agent/sidejobs | exported_symbols 111 | FP | no-baseline | e.g. ClampRunes summary.go:582, TruncateTail shared.go:48 | Moved code. The split also exported about 10 previously private helpers (clampRunes, collapseSpace, plural, truncateTail, modelAt) so agent can still call them. A reviewer might mention that, but would not block on it. |
| internal/agent/sidejobs | globals 5 | FP | no-baseline | account.go:160 `accountSchema` (json.RawMessage literal), summary.go:607 compiled regexp | Moved. The globals are also never written (secondary cause: immutable-global). |
| internal/agent/sidejobs | sloc 1329 | FP | no-baseline | pure move | The split halved agent's excess, which is exactly what the warnings ask for. The new package is itself over the ceiling. |
| internal/agent/sidejobs | tokens_est 24284 | FP | no-baseline | pure move | Restates the sloc finding. |
| internal/agent/sidejobs | untested_exports 18 | FP | no-baseline | ClampRunes summary.go:582, ModelAt shared.go:30 | Previously private helpers, tested through agent's callers. The `X.Model()` accessors were already exported before the move. |
| internal/calltier | untested_exports 2 | FP | covered-export | `Must` runs at agent init (agent/tier.go:189 `var registry = calltier.Must(...)`); `Declaration.Read` is a nil-guarded delegate called at agent/tier.go:202 | Genuinely new package (70 sloc). New, Merge and the error paths have tests (calltier_test.go:8-28). |
| internal/changeset | dup_blocks 5->6 | FP | boilerplate-dup | new block changeset.go:1114-1120 (`Unknown`: `if s == nil {return}; s.mu.Lock(); defer s.mu.Unlock()`) | The cited location changeset.go:427-446 is a legacy block (Persist/SetSlot, blame 7d73171b). |
| internal/changeset | exported_symbols 73->81 | FP | ocg | Record.BeforeUnknown/Created/Deleted, Store.Unknown/Origins, CommandBracket(+Records), UndoPlan.Kept/KeptPaths | One feature (13e5c704) adds 8 exports, all tested (untested_exports 4->3). |
| internal/changeset | largest_file_sloc 692->765 | TP | | changeset.go +146 raw lines in 13e5c704 | The file was already over 600 and took 73 more sloc. The commit had just created command.go, where Unknown/Origins/Paths fit naturally. |
| internal/changeset | sloc 922->1067 | TP | | single feature 13e5c704 (+400/-10 incl. tests) | The package crossed the ceiling in one feature: what the rule is built to catch. Borderline, because a reviewer would probably accept a 145-sloc feature. |
| internal/changeset | tokens_est 15667->18767 | FP | double-report | same growth as sloc | |
| internal/cli | changed_func_cognitive_max 57 | LEGACY | | todo.go:680 `todoVerb`, 127 lines; the only change in the window is `joinDetail(` -> `doctor.JoinDetail(` (20c93884) | Changing an Ident to a SelectorExpr changes the fingerprint. Failing a mechanical rename for a function's legacy complexity is not reasonable policy. |
| internal/cli | internal_imports 58->61 | FP | other: split adds edge | new edges: cli/doctor and agent/sidejobs (both splits), receipt (7dd5de43) | 58 is already 5.8x the ceiling. Two of the three new edges point at code that used to be in cli or agent. |
| internal/cli/doctor | dup_blocks 9 | FP | no-baseline | moved (keys.go 93%, plist.go 99%, store.go 67%); doctor.go:80-83 | 4-line `err != nil` fallback shapes. |
| internal/cli/doctor | globals 1 | FP | no-baseline | keys.go:103 `var ReadPrefs = func...` | This was `readPrefs` in cli. It was exported only so cli/doctor_test.go:657 can swap it. A reviewer might ask for injection, but the state is not new. |
| internal/cli/doctor | untested_exports 5 | FP | no-baseline | JoinDetail doctor.go:169, Store store.go:20 ... | Moved helpers that cli now calls across the boundary. |
| internal/cli/report | untested_exports 3->5 | FP | covered-export | report.go:578 Count, :587 ShortPath; wrapped by cli/history.go:509 `countOf` and cli/config.go:1559 `shortPath`, both used in cli tests (observe_test.go:1129) | Helpers moved so that cli and doctor can share them. |
| internal/eval | internal_imports 12->13 | FP | other: split adds edge | eval/mechanism.go, spawn.go now import agent/sidejobs | The only change in eval is the call-site move. |
| internal/observe/record | dup_blocks 10 | FP | no-baseline | record.go:31-74 vs record.go:469-520: const blocks of string codes with doc comments | Moved vocabulary (0d137dc2). Secondary cause: boilerplate-dup. |
| internal/observe/record | duplication_pct 69.7 | FP | no-baseline | same | A const-only file. |
| internal/observe/record | exported_symbols 161 | FP | no-baseline | record.go: Reason*/Outcome*/Class* constants | A constant vocabulary leaf. observe/vocabulary.go re-exports 158 aliases, so the surface now exists twice. That is worth a note, but the finding does not say it. |
| internal/observe/record | globals 2 | FP | no-baseline | record.go:684 `searchTools`, :694 `writeTools`: []string tables, copied on read | Moved, and also immutable-global. |
| internal/quality | exported_symbols 71->72 | FP | ocg | fingerprint.go:63 `PastContentBound` | +1 one-line predicate. |
| internal/quality | untested_exports 4->5 | FP | covered-export | fingerprint.go:63, passed as `Over:` func value at ui/chat/cmdwork.go:46 and cli/headlessclaim.go:37 | A one-line bound check exercised through CommandBracket tests. |
| internal/structural | dup_blocks 16->18 | TP | | gitwrite.go:404-414 `originWords` re-copies the both-spellings path loop of `stageable` at gitwrite.go:375-384; a third copy is at ui/chat/cmdwork.go:88-95 | Helper-worthy copy-paste introduced by 13e5c704. The cited location astgrep.go:11-50 is legacy. |
| internal/structural | largest_file_sloc 433->607 | TP | | gitwrite.go +250 raw lines in 13e5c704 | The file grew 40% in one feature; the claim/commit-receipt logic could have gone in its own file. |
| internal/structural | sloc 1533->1719 | FP | ocg | 13e5c704 (+174 per commit) and dfee87dc (tier declarations) | Ordinary feature growth of a 1.5k-sloc package. |
| internal/structural | tokens_est 27805->30726 | FP | double-report | | |
| internal/structural | untested_exports 7->9 | FP | covered-export | ClaimRows gitwrite.go:477 (4-line map, used at ui/chat/policy.go:237); Tiers structural.go:303 (registration run at agent/tier.go:189) | |
| internal/subagent | exported_symbols 163->164 | FP | ocg | +1 | |
| internal/subagent | internal_imports 21->22 | FP | other: split adds edge | subagent/env.go imports agent/sidejobs | |
| internal/subagent | sloc 5855->5961 | FP | ocg | +106 over 5 unrelated commits (3251a6e9, a4872440, de2261b2, a1d84dfc, ac1110f4) | Window aggregation: no single commit grows it by more than 100. |
| internal/subagent | tokens_est 125958->127658 | FP | double-report | | |
| internal/subagent/worktree | untested_exports 18->21 | FP | covered-export | MarkedFiles unreconciled.go:66, MarkedMerge merge.go:314, SeededFile.Same unreconciled.go:23; exercised by unreconciled_test.go and subagent/integrate_test.go (+55/+104 lines in a4872440/3251a6e9) | Behaviour is tested through Unreconciled and Integrate rather than by name. |
| internal/tools | dup_blocks 39->41 | TP | | shelledit.go:296-299 `under` is a verbatim re-implementation of gitpaths.go:163-166 `underDir` in the same package; shelledit.go:112-122 copies longrunning.go:70-78's quote-state loop | Any reviewer would say "use underDir". Introduced by 504a1c76/3ffa8f43. |
| internal/tools | exported_symbols 154->162 | FP | ocg | 5 feature commits | Tools package exports grow by one per tool feature. |
| internal/tools | sloc 6238->6952 | FP | ocg | +714 over 5 commits; ac1110f4 alone +446 (new editacross.go 358 lines + 455-line test) | The package is already 6x the ceiling. The largest feature landed in its own well-tested file. Borderline. |
| internal/tools | tokens_est 96554->108617 | FP | double-report | | |
| internal/ui/chat | dup_blocks 540->546 | FP | boilerplate-dup | e.g. claim.go:53-59 `claimPaths` vs startoffers.go:130-137 (map field, then strings.Join) | Six new blocks across 13 commits in a 44k-sloc package. The one real copy in the window (ce64275c dry-run) is diluted here; it is visible per commit. |
| internal/ui/chat | internal_imports 48->49 | FP | other: split adds edge | imports agent/sidejobs | |
| internal/ui/chat | largest_file_sloc 1058->1156 | TP | | approval.go: +148/-19 in ac1110f4 (`filesApproval` approval.go:1930, `patternFields` :1960) and +25 in 13e5c704 | A new card's code was put into an 1,100-sloc file instead of a new file. Cheap fix. |
| internal/ui/chat | sloc 43902->44365 | FP | ocg | +463 over 13 commits | 44k sloc, 44x the ceiling; aggregation of unrelated work. |
| internal/ui/chat | tokens_est 914553->924273 | FP | double-report | | |
| internal/ui/components | dup_blocks 310->313 | TP | | undo.go:123-146 `keptRows` is a copy of undo.go:151-174 `driftRows` (head line, truncate to undoDriftNames, "and %d more") | Added in 13e5c704; a `namedRows(head, list, width)` helper is the obvious fix. The cited location activityrow.go:393 is legacy. |
| internal/ui/components | exported_symbols 835->838 | FP | ocg | +3 on 838 | |
| internal/ui/components | sloc 22306->22452 | FP | ocg | +146 over 5 commits | |
| internal/ui/components | tokens_est 462805->466054 | FP | double-report | | |
| internal/web | exported_symbols 103->104 | FP | ocg | tool.go:205 `Tiers` (registration) | |
| internal/web | untested_exports 8->9 | FP | covered-export | tool.go:205 `Tiers`, run at agent init | |

### Sampled cross-package blocks (6 of 860)

1. agent/classifier.go:199-247 vs agent/sidejobs/summary.go:218-266. Config defaulting getters (`if c.X > 0 {return c.X}; return DefaultX`) plus `XWording()`, moved by a1d84dfc. Boilerplate.
2. observe/record/record.go:101-170 vs ui/components/activityrow.go:55-123. Two unrelated `const (Name = "string")` blocks with long doc comments. Token normalization makes all const lists alike. Boilerplate.
3. cli/web.go:103-128 vs cli/doctor/store.go:39-57. `dir, err := X.Dir(); if err != nil {return fallback}; return filepath.Join(dir, name)` followed by doc comments. Boilerplate. It is new only because the doctor moved.
4. changeset/changeset.go:1105-1120 vs meter/meter.go:344-356 vs scope/scope.go:116-129. Nil-receiver guard plus `mu.Lock(); defer mu.Unlock()` prologue. Boilerplate.
5. structural/gitwrite.go:378-385 vs ui/chat/cmdwork.go:88-95. Clean a path and insert both its spelling and its EvalSymlinks spelling into a set. This is a real small helper that was copied, both copies in 13e5c704. A related real copy: gitwrite.go:579 `rel` vs cmdwork.go:120 `workspaceRel` (same algorithm, written twice in one commit).
6. agent/sidejobs/explain.go:57-76 vs todo/extract.go:218-238. Same config-getter boilerplate as no. 1.

Four of the six are boilerplate. One (no. 5) is a genuine copy a reviewer would flag. No. 3 only exists because a package was split.

## 3. Over-ceiling share at HEAD (82 packages)

| Ceiling | Packages over | Share |
| --- | --- | --- |
| sloc > 1000 | 30 | 37% |
| tokens_est > 16000 | 32 | 39% |
| largest_file_sloc > 600 | 16 | 20% |
| exported_symbols > 60 | 24 | 29% |
| internal_imports > 10 | 6 | 7% |
| Any of the five | 37 | 45% |

91.1% of the module's non-test sloc (163,804 of 179,788) sits in packages over the 1,000-sloc ceiling. The three largest are ui/chat 44,365, cli 26,890 and ui/components 22,452. All 37 packages with warnings are these over-ceiling or near-ceiling packages.

## 4. Per-commit view (`check . --base <c>~1`, detached worktree, changed packages only)

| commit | subject | lines changed (all / Go) | exit | violations and verdicts |
| --- | --- | --- | --- | --- |
| ce64275c | fix(ui): say where a dry run ran and put it on the manager | 532 / 231 | 3 | 3. module cross 805->806: FP boilerplate-dup (callTable.drop nil-guard+lock prologue, calls.go:81-89). module max 450: FP double-report. ui/chat dup_blocks 540->542: **TP** (the dry run re-copies the real run's host/container `where` and outcome mapping, run.go:409-429 vs run.go:223-281). |
| a4872440 | fix(subagent): never keep a refused integration patch that carries conflict marks | 103 / 95 | 3 | 1. worktree untested_exports 20->21 (MarkedFiles): FP covered-export; tested via unreconciled_test.go and integrate_test.go. |
| 0d137dc2 | refactor(observe): move the record's vocabulary to a leaf | 2,511 / 2,509 | 3 | 6. module cross 806->810: FP other (move). module max: FP double-report. record dup_blocks, duplication_pct, exported_symbols, globals: 4x FP no-baseline (moved constants and immutable tool lists). |
| e4fa0c1f | feat(config): keep a prompts directory for overrides only | 521 / 443 | 0 | none |
| a1d84dfc | refactor(agent): move the side jobs into a sub-package | 1,347 / 1,347 | 3 | 14. module cross 810->829: FP other (move). module max: FP double-report. agent, cli, eval, subagent, ui/chat internal_imports +1 each: 5x FP other (split adds edge). sidejobs x7: FP no-baseline. |
| c63a2cf0 | docs(readme): install the cask (smallest, 6 lines) | 6 / 0 | 0 | none (no Go package changed) |
| 13e5c704 | feat(structural): record what a command changed and claim the rest on the card | 2,839 / 1,915 | 3 | 22. **TP (6)**: module cross 835->852 (structural `rel` vs chat `workspaceRel`, and the path-set loop; the suggestion names the legacy classifier/summary block), changeset largest_file_sloc 692->765, changeset sloc 922->1067 (crossing), structural dup_blocks 16->18, structural largest_file_sloc 433->607, components dup_blocks 309->313 (undo.go keptRows/driftRows). **FP (16)**: module max (double), agent exported_symbols 390->391 (ocg), changeset dup_blocks (boilerplate), changeset exported_symbols (ocg), changeset tokens_est (double), cli tokens_est +1,939 (ocg), quality exported_symbols (ocg), quality untested_exports (covered), structural sloc +174 (ocg), structural tokens_est (double), structural untested_exports ClaimRows (covered), chat dup_blocks (boilerplate), chat largest_file_sloc +10 (ocg), chat sloc +235 (ocg), chat tokens_est (double), components tokens_est +1,169 (ocg). |
| ac1110f4 | feat(tools): let edit_file take a pattern with groups and a list of files (largest) | 3,473 / 2,157 | 3 | 12. **TP (1)**: ui/chat largest_file_sloc 1077->1156 (filesApproval and patternFields added to approval.go). **FP (11)**: module cross 852->860 (boilerplate, e.g. editacross.go:234-255 err-return vs supervisor.go:435-445), module max (double), agent exported_symbols 391->392 (ocg), agent untested_exports 44->45 (`SetTurnStart` one-line setter, covered-export), subagent exported_symbols (ocg), tools exported_symbols 159->162 (ocg), tools sloc +446 (ocg; new file, well tested), tools tokens_est (double), chat sloc +103 (ocg), chat tokens_est (double), components exported_symbols 837->838 (ocg). |

Per-commit totals: 6 of 8 commits fail. Of 58 violations, 8 are TP and 50 FP. Only 3 of the 6 failing commits (ce64275c, 13e5c704, ac1110f4) carry at least one TP. a4872440, 0d137dc2 and a1d84dfc fail on FPs alone, and the last two are the package splits the capacity warnings recommend.

## Verdict summary (window run, 54 violations)

| Verdict | Count |
| --- | --- |
| TP | 7 (13%) |
| FP | 46 (85%) |
| LEGACY | 1 (2%) |

FP by cause:

| Cause | Count | Where |
| --- | --- | --- |
| no-baseline | 14 | sidejobs 7, record 4, doctor 3; all moved code |
| over-ceiling-growth | 11 | exported_symbols +1..+8 (6), sloc (4), plus tools sloc |
| double-report | 7 | tokens_est alongside sloc (6), module `max 450` alongside `max_delta` (1) |
| covered-export | 6 | calltier, cli/report, quality, structural, subagent/worktree, web |
| other: split adds edge or moved dups | 6 | internal_imports on agent, cli, eval, subagent, ui/chat (5); module cross-pkg rise (1) |
| boilerplate-dup | 2 | changeset (nil-guard/lock prologue), ui/chat |
| immutable-global | 0 as primary | 3 of the no-baseline globals are also immutable (schemas, regexp, tool lists) |
| tiny-ratchet | 0 | |

The 7 TPs are all of one kind: copy-paste a reviewer would ask to extract (structural, tools, components dup_blocks), or a file or package pushed across or further over the line by one feature (changeset sloc and largest file, structural largest file, ui/chat approval.go).

## Observations for the astimate maintainer

1. **The gate punishes the splits its own warnings ask for.** The three rename-based extractions account for 14 no-baseline violations, all 5 internal_imports violations, and most of the cross-package rise. 70 of the 114 "new" cross-package blocks touch the extracted packages, while intra-package dup_blocks in agent, cli and observe fell by 67, so total duplication went down. The split commits a1d84dfc and 0d137dc2 fail on 20 FPs between them. Suggested fixes:
   - Baseline a new package from the git renames (or the baseline fingerprints) of the code it holds.
   - Do not ratchet internal_imports for an edge to a package new at head whose code came from the importer or its siblings.
   - Count a block as cross-package-new only if its occurrences were not already all present (in any package) at baseline.
2. **Double reporting inflates every over-ceiling failure.** tokens_est fired together with sloc on the same growth in 6 of 7 cases. The module `dup_blocks_cross_pkg` `max 450` fires alongside `max_delta` on every +1 because the baseline was already 805, and that pair appears in 6 of the 8 per-commit runs. Merging these (one capacity finding per growth, no `max` finding when the baseline already exceeds it) would cut 7 of the 46 window FPs and 11 of the 50 per-commit FPs.
3. **Locations point at legacy code, not at what the change added.** The suggestions for dup_blocks_cross_pkg, changeset dup_blocks, structural dup_blocks and components dup_blocks all name the first block in the list, which is a legacy or moved block (classifier.go:199, changeset.go:427, astgrep.go:11, activityrow.go:393). The real new copies (gitwrite.go:579 vs cmdwork.go:120, gitwrite.go:404 vs :375, undo.go:123 vs :151) go unnamed. The gate already computes "new head blocks" for named-package checks. Using that to order the suggestion, and listing package-level new blocks first, would make the TPs actionable.
4. **A window run is not a stand-in for per-commit gating, in either direction.** The 30-commit window hid per-commit regressions in packages a refactor shrank: agent exported_symbols +1 twice, agent untested_exports +1 (SetTurnStart), cli tokens_est +1,939 in 13e5c704. All of these show as decreases, or only as warnings, at window level. In the other direction it aggregated unrelated small changes into ratchet violations: subagent sloc grew +106 over 5 commits, ui/chat +463 over 13. Precision is about the same either way (13% vs 14%).
5. **Two counting quirks produce FPs with no reviewer value:**
   - `changed_func_cognitive_max` marks a function changed when a call is requalified (`joinDetail(` -> `doctor.JoinDetail(` in cli/todo.go:680, cognitive 57). Normalizing a package-qualified selector like an identifier would fix it.
   - `untested_exports` counts exports that run on every test of an importing package through a package-level initializer (`calltier.Must`, the `Tiers()` registrations at agent/tier.go:189). It also counts helpers whose behaviour is pinned by tests of their only caller (MarkedFiles, ClampRunes). 6 of the 8 untested_exports findings are of this kind. Consider counting a function as referenced when a package-level var initializer of a package with tests calls it.

---

## Per-commit replay (calibration/replay, last 60 commits)

```
## go-starter
commits=60 loaded=60 touching_pkgs=41 failed=15 (37% of commits that touched a package)
rule -> commits failed by it (commits where it was the only rule):
  dup_blocks                     6  (sole 1)
  untested_exports               6  (sole 2)
  globals                        5  (sole 1)
  init_funcs                     3  (sole 0)
  duplication_pct                2  (sole 0)
  cognitive_p90                  2  (sole 1)
  internal_imports               2  (sole 1)
  dup_blocks_cross_pkg           1  (sole 1)
  max_nesting                    1  (sole 1)
failed commits:
  74f7726 files=146 refactor(architecture): enforce module platform boundaries   dup_blocks,globals,init_funcs,untested_exports
  804071b files= 34 refactor(http): group server infrastructure under platform   dup_blocks,globals,init_funcs,untested_exports
  bd1e892 files= 20 feat(database): add production-safe migration command        dup_blocks,duplication_pct,globals,init_funcs
  e9fbc1d files= 14 feat(health): add dependency-aware readiness                 cognitive_p90,untested_exports
  5d9bb00 files=  5 test(app): cover service lifecycle end to end                untested_exports
  72f02c0 files= 10 feat(auth): add credential authentication service            globals,untested_exports
  09783ec files= 21 feat(api): add login and token flow                          internal_imports
  f818863 files= 13 fix(seed): prevent default production credentials            dup_blocks
  96d2cff files=  7 refactor(ratelimit): replace placeholder implementations     dup_blocks_cross_pkg
  93137e7 files= 29 feat(ratelimit): protect login with trusted client identity  dup_blocks,internal_imports
  4f11de7 files=  2 fix(templateinit): prevent cascading binary-name replacement max_nesting
  eab9fe9 files=  5 refactor(cmd): share database pool setup across seed, migrat untested_exports
  e7cc411 files=  8 refactor(templateinit): delete the initializer sources on ma globals
  ea8a388 files=  5 refactor(server): replace the webserver options with a Confi cognitive_p90
  ef5b99f files= 12 refactor(seed): require seed.password and take the logger as dup_blocks,duplication_pct
## ztp
commits=60 loaded=29 touching_pkgs=18 failed=14 (78% of commits that touched a package)
rule -> commits failed by it (commits where it was the only rule):
  dup_blocks_cross_pkg          11  (sole 0)
  untested_exports              10  (sole 0)
  dup_blocks                     9  (sole 1)
  duplication_pct                5  (sole 0)
  exported_symbols               4  (sole 0)
  max_nesting                    4  (sole 0)
  cognitive_p90                  3  (sole 0)
  sloc                           2  (sole 0)
  globals                        1  (sole 0)
  changed_func_cognitive_max     1  (sole 0)
  has_tests                      1  (sole 0)
  internal_imports               1  (sole 0)
failed commits:
  edd3a53 files= 10 Add watch stream recording with event counting, policies, an dup_blocks,dup_blocks_cross_pkg,exported_symbols,globals,max_nesting,sloc
  2579fee files=  5 Add SessionMetadataStore interface, MemoryMetadataStore, and dup_blocks_cross_pkg,untested_exports
  7aa1539 files=  5 Add RecordingFileStore interface, LocalFileStore, and fake ( dup_blocks_cross_pkg,max_nesting,untested_exports
  4d185e9 files=  6 Add AccessSessionManager interface, DefaultAccessSessionMana dup_blocks,dup_blocks_cross_pkg,duplication_pct,untested_exports
  5015ee3 files= 32 Complete S-176: Wire AccessSessionManager into broker and pr changed_func_cognitive_max,dup_blocks,dup_blocks_cross_pkg,duplication_pct,has_tests,untested_exports
  5ded0c2 files=  5 Fix TCP proxy half-close hang and broker shutdown delay      cognitive_p90,dup_blocks,duplication_pct,untested_exports
  33851d1 files=  9 Add deterministic HMAC-based session IDs with ref counting   dup_blocks
  3feff9a files=  2 Fix kubectl hanging: re-encode chunked responses for keep-al cognitive_p90,dup_blocks_cross_pkg
  383d367 files=  7 Fix SSH recording: dissector output now reaches blob store,  dup_blocks,untested_exports
  a3a053e files=  6 Fix recording pipeline: wire format-aware recorders into all dup_blocks,dup_blocks_cross_pkg,duplication_pct,max_nesting,untested_exports
  878d5f0 files=  3 Fix SFTP/SCP file capture: upload captured files to blob sto dup_blocks_cross_pkg,exported_symbols
  fbb05c0 files=  3 Add DefaultHTTPRules and wire into broker recording router   dup_blocks_cross_pkg,internal_imports,untested_exports
  9fbbe74 files= 11 Add SQLite-backed unified SessionStore (S-177)               dup_blocks,dup_blocks_cross_pkg,duplication_pct,exported_symbols,untested_exports
  da4d686 files= 15 Fix K8s exec recording corruption, add per-stream playback t cognitive_p90,dup_blocks,dup_blocks_cross_pkg,exported_symbols,max_nesting,sloc,untested_exports
```

---

## After the mechanics fixes (2026-10-14, master 7fb7467)

Same windows, same base commits, re-run with the binary at 7fb7467 (S-182 to S-194 landed). The true findings named above (mark `ProcessFile` 140, `confluence` sloc crossing, `renderer` copies, kubesentry output switch, ztp wsframe copy, crush `extractRows`, shhh `gitwrite.go` loop) are still reported; paired findings are merged, renamed packages keep their baseline, and "added in this change" marks the new copy.

| Repo | Violation lines before | After | "no baseline" before | After |
| --- | --- | --- | --- | --- |
| go-starter | 3 | 1 | 0 | 0 |
| goKMS | 12 | 7 | 2 | 1 |
| kubesentry | 11 | 7 | 1 | 0 |
| ztp | 30 | 23 | 4 | 2 |
| ai-orchestration | 13 | 8 | 6 | 5 |
| mark | 51 | 20 | 50 | 1 |
| crush | 20 | 16 | 0 | 0 |
| shhh | 54 | 31 | 16 | 5 |
| total | 194 | 113 | 79 | 14 |

Per-commit fail rates from the S-189 replays (last 60 first-parent commits, binary c28a78f, before S-186's floors and S-188): go-starter 13 of 41 package-touching commits (32%, was 37%), ztp 26 of 36 (72%, was 78% of 18; S-187 made 53 of 60 commits loadable instead of 29), crush 17 of 26 (65%), shhh 24 of 44 (55%), ai-shell 14 of 36 (39%, all `has_tests` in a repo with no tests). The rules that still decide most failures are `dup_blocks`, `dup_blocks_cross_pkg`, `untested_exports` and `exported_symbols`: the next lever is S-158 (`min_tokens`), S-146's retune on these replays, and the `exported_symbols` allowance decision recorded under S-189.

## After the second run (2026-10-14, master c1654ec)

S-195 to S-204 landed (allowances 5 and 5, `metrics/explain` split, hand labels drafted, package skips, block kinds, hook-free baseline, `dryrun`, added-block cross rule with an 80-token floor, package roles, coverage-backed untested exports). Same eight windows, same base commits:

| Repo | Start | After run 1 | After run 2 |
| --- | --- | --- | --- |
| go-starter | 3 | 1 | 1 |
| goKMS | 12 | 7 | 5 |
| kubesentry | 11 | 7 | 7 |
| ztp | 30 | 23 | 22 |
| ai-orchestration | 13 | 8 | 7 |
| mark | 51 | 20 | 18 |
| crush | 20 | 16 | 16 |
| shhh | 54 | 31 | 27 |
| total | 194 | 113 | 103 |

`astimate dryrun --last 60` with the final binary: go-starter fails 10 of 42 package-touching commits (24%, was 37% at the start); ztp 30 of 41 (73%, was 78% of 18; 41 now load). ztp's remaining failures are `dup_blocks` (19), `dup_blocks_cross_pkg` (15), `untested_exports` (15), `duplication_pct` (7): the duplication rules still decide most of them, which is S-158 and S-180's territory, and `untested_exports` wants `source: coverage` where the tests run. Under the hand labels (S-198, draft) 43 of 287 findings on failed commits were real.

## After the third run (2026-10-14, master b67fb2d)

S-205 to S-211, S-158 and S-146 landed: standard-interface and syntactic-test exclusions, engine split, carved-out package seeding, five replays at 40/60/80/120 tokens, `duplicated_lines` measured and kept off, and the retune `thresholds-2026-10-14` on the confirmed hand labels (`min_tokens` 60; `untested_exports`, `globals`, `cognitive_p90` and `internal_imports` warn; allowances `sloc` 150, `tokens_est` 2000, `largest_file_sloc` 50). On the pooled hand labels the gate went from 26.8% false failures to 10.9% with recall unchanged at 85.7% (`calibration/reports/thresholds-2026-10-14.md`). Same eight windows, same base commits (`runs/*.check5.txt`):

| Repo | Start | After run 1 | After run 2 | After run 3 | Warnings now |
| --- | --- | --- | --- | --- | --- |
| go-starter | 3 | 1 | 1 | 0 | 3 |
| goKMS | 12 | 7 | 5 | 1 | 9 |
| kubesentry | 11 | 7 | 7 | 7 | 5 |
| ztp | 30 | 23 | 22 | 7 | 20 |
| ai-orchestration | 13 | 8 | 7 | 5 | 1 |
| mark | 51 | 20 | 18 | 10 | 12 |
| crush | 20 | 16 | 16 | 9 | 92 |
| shhh | 54 | 31 | 27 | 12 | 133 |
| total | 194 | 113 | 103 | 51 | 275 |

The seven true findings named above are all still violations (mark `ProcessFile` 140, `confluence`, `renderer`, kubesentry `internal/output`, ztp `wsframe`, shhh `gitwrite.go`; crush `extractRows` is reported under its package as before). The warning counts are mostly `untested_exports`, which now warns: crush and shhh carry it on most packages, which is the cost of the demotion and the reason `source: coverage` in hooks is the next step for that rule.

`astimate dryrun --last 60` with the final binary: go-starter would fail 3 of 42 package-touching commits (7%, was 24% after run 2 and 37% at the start; `dup_blocks` 3, `duplication_pct` 1, `init_funcs` 1). ztp 20 of 41 (49%, was 73% and 78%; 58 of 60 commits load): `dup_blocks` 13 and `dup_blocks_cross_pkg` 13 decide it, both over the 10% budget, with `duplication_pct` 4, `exported_symbols` 3, `max_nesting` 2, `sloc` 2. ztp's remaining failures are duplication between its `cmd/*` and protocol packages, which S-180 (self-overlap coalescing) and the parked `min_tokens_cross` 60 measurement address, not thresholds.
