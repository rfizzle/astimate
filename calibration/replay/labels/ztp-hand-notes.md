# Hand labels for ztp: causes and evidence

Per-finding causes and `file:line` evidence behind `calibration/replay/labels/ztp-hand.yaml`. The labels file keeps one verdict per commit and a reason that starts with the cause; this file keeps every finding of a failed commit with its own cause. Every label is a draft (`provenance: proposed`) awaiting the user's confirmation. The taxonomy and the standard of evidence are those of the 2026-10-13 field adjudication; the counts and the least certain labels are in `calibration/notes/hand-labels-2026-10-14.md`.

## Failed commits (26)

### ac0064179 `allow`: Add access session correlation with query-time grouping (S-152)

Access-session grouping adds one well-tested 50-line function and a small filter loop; no copy-paste and nothing a reviewer would ask to split. Evidence: `internal/web/grouper.go:14`, `internal/web/server.go:102`, `internal/web/sessions.go:56`. Confidence medium: Could not pinpoint which pair the cross-package rule matched; judged from the diff containing no copied logic.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | No cross-package copy in the diff: new code is grouper.go (unique logic) plus re-aligned struct field lists (sessions.go:55-67, 85-94); the rise is struct-of-fields/filter idiom; cited cmd/benchmark/main.go:41 is legacy. |
| `internal/web` | `cognitive_p90` | tiny-ratchet | p90 over ~36 small funcs moved because GroupAccessSessions (grouper.go:14-72, sort comparator + grouping loop) and an 8-line access_session filter in handleListSessions (server.go:100-110) were added; both linear and tested (grouper_test.go, server_test.go). |

### 4c07cb00d `allow`: Add declarative recording policy engine for K8s request filtering (S-154)

Policy engine plus interface accessors; every duplicate is accessor/option/guard-chain scaffolding and the flagged exports are trivial getters. Evidence: `internal/protocol/kubernetes/classify.go:129`, `internal/recording/policy/policy.go:129`, `internal/recording/policy/policy.go:75`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | New policy package repeats the Option/WithLogger/New+logutil.Resolve shell (policy.go:72-99) used across recording packages; no logic copied. |
| `internal/protocol/kubernetes` | `dup_blocks` | boilerplate-dup | Five one-line Get* accessors (classify.go:129-142) match each other; interface method set. |
| `internal/protocol/kubernetes` | `untested_exports` | covered-export | GetResource/GetVerb/GetSubresource/GetIsWatch/GetIsUpgrade are trivial field accessors satisfying policy.ClassifiedRequest (classify.go:10, 129-142). |
| `internal/recording/policy` | `dup_blocks` | boilerplate-dup | New package; the block is the `if m.X != "" && m.X != req.GetX() { return false }` guard chain in matches (policy.go:129-146). |

### 8fa49eced `allow`: Add recording filter for policy-driven redaction, truncation, and skip (S-155)

Self-contained, tested filter package; cross-package matches are constructor and lock/closed-guard idioms. Evidence: `internal/recording/filter/filter.go:22`, `internal/recording/filter/filter.go:53`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | New filter package adds the Option/WithLogger/New+Resolve shell (filter.go:19-51) and the mutex/closed Write guard (filter.go:53-60) shared by every recording writer; its policy-application switch is not copied anywhere yet. |

### a33321bf8 `allow`: Move recording packages to pkg/ for cross-team import (S-164)

Package move with import rewrites only; no code added, so no new copy. Evidence: `pkg/recording/filter/filter.go:13`, `pkg/recording/recordingtest/fake_recorder.go:1`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | other: moved duplicates | Pure internal/recording -> pkg/recording move (67 files, +65/-63, import-path edits only, e.g. pkg/recording/filter/filter.go:13-16); pre-existing pairs re-counted under new paths. |

### e9eac7151 `allow`: Add net.Conn recording wrapper package (S-166)

A 54-line net.Conn recording wrapper; the duplicates are the Read/Write mirror pair every conn wrapper has. Evidence: `pkg/recording/connwrap/conn.go:24`, `pkg/recording/pcap/recording_conn.go:47`. Confidence medium: The new wrapper overlaps pcap's private recordingConn; a strict reviewer might ask pcap to reuse it, but the shared code is a tiny delegation idiom.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | connwrap.Conn Read/Write/CloseWrite (conn.go:24-46) mirror the net.Conn delegation in pkg/recording/pcap/recording_conn.go:47-75; 6-line wrapper idiom. |
| `pkg/recording/connwrap` | `dup_blocks` | boilerplate-dup | Read vs Write (conn.go:24-38) are the symmetric 6-line delegation pair; duplication_pct 41% of a 54-line package. |

### bb794c0f3 `allow`: Add io.Reader/Writer recording wrappers package (S-167)

Symmetric io.Reader/io.Writer wrappers; the duplication is the unavoidable mirror pair, not extractable logic. Evidence: `pkg/recording/iowrap/iowrap.go:11`, `pkg/recording/iowrap/iowrap.go:31`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | Reader.Read/Writer.Write (iowrap.go:23-29, 44-50) repeat the same n>0 record-then-return idiom as connwrap. |
| `pkg/recording/iowrap` | `dup_blocks` | boilerplate-dup | Reader and Writer struct/constructor/method are mirror images (iowrap.go:11-29 vs 31-50); 84% of a 50-line file. |

### 0a8b69b2e `block`: Add composable HTTP recording RoundTripper package (S-168)

The RoundTripper copies the policy redaction/truncation logic and marker format from the filter package instead of sharing it; a reviewer would ask for one policy.Apply helper. Evidence: `pkg/recording/httprecord/recorder.go:157`, `pkg/recording/filter/filter.go:136`. Confidence medium: The copied switch is ~15 lines; some reviewers would accept it in a new package.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | applyPolicy (httprecord/recorder.go:149-167) re-implements the Redact/Skip/Truncate switch and marker strings of filter.applyAndForward (pkg/recording/filter/filter.go:130-142, marker at :136 vs :157); policy application belongs in pkg/recording/policy. |
| `pkg/recording/httprecord` | `dup_blocks` | boilerplate-dup | recordRequest vs recordResponseHeader (recorder.go:105-121): dump/warn/Write sequence, 6 lines each. |

### f312542f6 `block`: Add SSH session recorder with transparent channel dispatch (S-169)

New package triplicates the dissector relay body and copies two helpers from internal/protocol/ssh; b098ecc later had to collapse the triplication. Evidence: `pkg/recording/sshrecord/recorder.go:203`, `pkg/recording/sshrecord/recorder.go:252`, `pkg/recording/sshrecord/recorder.go:301`, `internal/protocol/ssh/channel_relay.go:549`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | copyWithRecording and forwardRequests (sshrecord/recorder.go:351-382) are verbatim copies of internal/protocol/ssh/channel_relay.go:549, 609. |
| `pkg/recording/sshrecord` | `dup_blocks` | real | relaySFTP/relaySCP/relayDirectTCPIP (recorder.go:203-347) are three ~45-line copies differing only in dissector type; duplication_pct 56%. |

### 6b9cbe650 `block`: Refactor sshrecord: move all classification inside HandleChannel (S-169)

Moves channel classification into sshrecord by copying ~100 lines of peeker/classifier code from internal/protocol/ssh without deleting the original. Evidence: `pkg/recording/sshrecord/recorder.go:88`, `internal/protocol/ssh/channel_relay.go:481`, `pkg/recording/sshrecord/recorder.go:133`. Confidence medium: Copy-then-switch: the original is removed two commits later (2efbd01), so a reviewer of the whole series might accept the temporary copy.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | classifySession/isSCPCommand/requestPeeker/parseStringPayload (sshrecord/recorder.go:52-149) are copied from internal/protocol/ssh/channel_relay.go:139-545; both copies live until 2efbd01. |
| `pkg/recording/sshrecord` | `untested_exports` | covered-export | Setup/Requests (recorder.go:127, 133) are methods on unexported requestPeeker, driven through HandleChannel in recorder_test.go. |

### b098eccfc `allow`: Refactor sshrecord: unify relay pattern, add benchmarks and parallel tests (S-170)

A de-duplication refactor: relayWithDissector replaces three copies; the flagged items are adapter shells on unexported types. Evidence: `pkg/recording/sshrecord/recorder.go:384`, `pkg/recording/sshrecord/recorder.go:428`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `pkg/recording/sshrecord` | `dup_blocks` | boilerplate-dup | The new block is the three 4-line dissector adapters sftpDissector/scpDissector/directtcpipDissector (recorder.go:363-384); the commit removes the 3x45-line relay triplication. |
| `pkg/recording/sshrecord` | `untested_exports` | covered-export | Relay/Close on unexported adapter types (recorder.go:366-384) satisfy the internal dissector interface and run in the relay tests. |

### 51ef8c346 `allow`: Restructure pkg/recording into role-based hierarchy

Directory restructure; globals are never-written Direction aliases and the wrap duplicates are the pre-existing conn/io mirror pairs merged into one file. Evidence: `pkg/recording/dissect/scp/message.go:59`, `pkg/recording/dissect/sftp/tracker.go:13`, `pkg/recording/wrap/wrap.go:54`. Confidence medium: The const->var change is a small step back a reviewer might nit (could stay const), but nothing writes them.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `pkg/recording/dissect/scp` | `globals` | immutable-global | `var ( ClientToServer = recording.ClientToServer; ServerToClient = ... )` (message.go:59-62) replaces former consts with never-written aliases of recording.Direction. |
| `pkg/recording/dissect/sftp` | `globals` | immutable-global | Same Direction alias vars (tracker.go:13-16), never written. |
| `pkg/recording/wrap` | `dup_blocks` | other: moved duplicates | connwrap and iowrap merged into wrap/wrap.go (iowrap.go renamed, +49); the Read/Write mirror pairs already existed in both packages. |

### f430f2f03 `block`: Add Classifier/Handler middleware, PassthroughClassifier, profile classifier bundling (S-175 T-251–T-254)

The generic pkg/recording/http package jumps from 47 to 82 exported symbols by absorbing the K8s classifier and a middleware layer; the other findings are accessor scaffolding, a pool, or a deletion-driven p90. Evidence: `pkg/recording/http/classifier.go:18`, `pkg/recording/http/handler.go:35`, `pkg/recording/http/classifier.go:143`, `internal/protocol/kubernetes/classify.go:16`. Confidence medium: Many of the 35 new symbols are getters and options; a maintainer could call this ordinary growth.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | K8s classifier was moved, not copied (kubernetes/classify.go -149 lines); new cross-package matches are recConn Read/Write (handler.go:296-310) vs wrap.Conn and Get* accessor sets. |
| `internal/protocol/kubernetes` | `cognitive_p90` | tiny-ratchet | p90 rose because Classify/parse/resolve/getters were deleted from the package (classify.go now 24 lines), not because code got harder. |
| `internal/protocol/kubernetes` | `globals` | immutable-global | `var classifier httprecorder.K8sClassifier` (classify.go:16) is a zero-size stateless struct. |
| `pkg/recording/http` | `dup_blocks` | boilerplate-dup | K8sClassification vs PassthroughClassification getter sets (classifier.go:31-35 vs 143-147) and recConn Read/Write mirror (handler.go:296-310). |
| `pkg/recording/http` | `exported_symbols` | real | 47->82 crosses the 60 ceiling in one commit by putting the Kubernetes-specific classifier (classifier.go:18-131) and a full Handler middleware with 4 options (handler.go:35-60) into the generic HTTP recording package; a reviewer would ask for a k8s subpackage. |
| `pkg/recording/http` | `globals` | other: sync.Pool | bufPool (handler.go:18) is a sync.Pool buffer cache, the standard idiom, not shared domain state. |
| `pkg/recording/http` | `untested_exports` | covered-export | PassthroughClassification Get* (classifier.go:143-147) are constant/field accessors; TestPassthroughClassifier_Classify (classifier_test.go:355) exercises the type. |

### edd3a530b `block`: Add watch stream recording with event counting, policies, and periodic flush (S-157 T-245–T-249)

Second large feature in a row lands K8s-specific watch recording in the generic HTTP recording package, pushing it past the sloc ceiling, and copies Close/Snapshot logic in metadata.go. Evidence: `pkg/recording/http/metadata.go:137`, `pkg/recording/http/metadata.go:206`, `pkg/recording/http/watch_counter.go:86`, `pkg/recording/http/periodic_flusher.go:17`. Confidence low: The sloc crossing is real but the new files are cohesive and tested; a maintainer could reasonably call this ordinary over-ceiling growth, and the Close/Snapshot copy is only ~6 lines.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | Watch counter Option/With* shells (watch_counter.go:25-46) and the stop/done goroutine shell (periodic_flusher.go:24-65) are idioms; no logic copied across packages. |
| `pkg/recording/http` | `dup_blocks` | real | WatchMetadataRecording.Close (metadata.go:206-220) re-implements the base Close and Snapshot field assignments (metadata.go:137-156) instead of calling Snapshot; small but genuine copy. |
| `pkg/recording/http` | `exported_symbols` | double-report | 82->104 restates the sloc crossing: same watch feature, same 'split' advice. |
| `pkg/recording/http` | `globals` | immutable-global | rvKey (watch_counter.go:86) is a never-written []byte search key. |
| `pkg/recording/http` | `max_nesting` | tiny-ratchet | 2->3 from the RV lock inside `if resourceVersion != ""` (metadata.go:186-193) and the line loop in WatchEventCounter.Write. |
| `pkg/recording/http` | `sloc` | real | 816->1021 crosses 1000 by adding a self-contained K8s watch subsystem (watch_counter.go, watch_policy.go, periodic_flusher.go, WatchMetadataRecording) to a package already at 82+ exported symbols; a reviewer would ask for a watch subpackage. |

### 2579fee0c `allow`: Add SessionMetadataStore interface, MemoryMetadataStore, and fake (S-171 T-255–T-258)

Interface plus in-memory store plus fake; duplicates are constructor/field scaffolding and the untested exports are a test fake. Evidence: `pkg/storage/memory_metadata.go:94`, `pkg/storage/storagetest/fake_metadata.go:29`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | Option/WithLogger/New+Resolve shell (memory_metadata.go:14-42), SessionRecord/SessionFilter field structs and the matchesFilter guard chain (memory_metadata.go:94-111); no logic copy found (pkg/compliance/fs_sessions.go:91 is a different filter). |
| `pkg/storage/storagetest` | `untested_exports` | covered-export | FakeMetadataStore Store/Get/Query (fake_metadata.go:24-35) are test doubles in a test-support package. |

### 7aa15397b `allow`: Add RecordingFileStore interface, LocalFileStore, and fake (S-172 T-259–T-262)

LocalFileStore plus fake; all findings are scaffolding, a nesting step of one, or a test fake. Evidence: `pkg/storage/filestore/local.go:21`, `pkg/storage/storagetest/fake_filestore.go:45`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | filestore Option/WithLogger/New+Resolve shell (local.go:18-41) and os error-wrap chains; no logic copied. |
| `pkg/storage/storagetest` | `max_nesting` | tiny-ratchet | 0->1: the single `if f.GetErr != nil` in FakeFileStore.Get (fake_filestore.go:45-48). |
| `pkg/storage/storagetest` | `untested_exports` | covered-export | FakeFileStore Put/Get/List/Delete (fake_filestore.go:37-56) are a test double. |

### 4d185e95c `allow`: Add AccessSessionManager interface, DefaultAccessSessionManager, and fake (S-173 T-263–T-266)

Manager, scoped-store adapter and fake; duplicates are delegation and fake scaffolding. Evidence: `pkg/storage/default_manager.go:114`, `pkg/storage/storagetest/fake_manager.go:44`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | Manager option shell and the fakes' record-call-return-field methods; no logic copied. |
| `pkg/storage` | `dup_blocks` | boilerplate-dup | scopedFileStore Put/Get/List/Delete one-line delegations (default_manager.go:114-128) match each other. |
| `pkg/storage/storagetest` | `dup_blocks` | boilerplate-dup | FakeAccessSessionManager append-and-return methods (fake_manager.go:34-50) mirror the other fakes. |
| `pkg/storage/storagetest` | `untested_exports` | covered-export | Fake methods in a test-support package (fake_manager.go:34-50). |

### 5015ee36a `block`: Complete S-176: Wire AccessSessionManager into broker and protocol handlers (T-267–T-273)

Wiring the session manager copies the whole HAR recorder into internal/protocol/http and inlines recording setup into an already 60-cognitive K8s Handle. Evidence: `internal/protocol/http/recording.go:103`, `pkg/recording/format/har/har_recorder.go:263`, `internal/protocol/kubernetes/handler.go:82`, `cmd/broker/main.go:75`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | internal/protocol/http/recording.go:20-296 is a ~280-line copy of the HAR recorder in pkg/recording/format/har/har_recorder.go (harEntry :73, tryParseResponse :263, countingReader :411) with a FileStore flush target. |
| `cmd/broker` | `dup_blocks` | boilerplate-dup | registry.Register("http"...) and ("https"...) lines (main.go:75-76) are identical registration calls. |
| `internal/benchmark` | `untested_exports` | covered-export | nopFileStore Put/Get/List/Delete (harness.go:21-31) are methods on an unexported no-op fake. |
| `internal/protocol/http` | `dup_blocks` | real | tryParseRequest vs tryParseResponse (recording.go:78-140) come with the copied HAR recorder; reusing format/har with a FileStore sink removes them. |
| `internal/protocol/http` | `untested_exports` | covered-export | harFileStoreRecording.Write (recording.go:46) is a method on an unexported type implementing recording.Recording, driven by handler_test.go. |
| `internal/protocol/kubernetes` | `changed_func_cognitive_max` | real | Handle, already at 60, grows to 68 by inlining temp-dir/K8sProfile setup and a flush-and-cleanup defer (handler.go:82-102); a reviewer would ask to extract a per-session recorder helper. |
| `pkg/compliance` | `dup_blocks` | boilerplate-dup | StoreSessionService repeats the 4-line scorer block and summary steps of FSSessionService (store_sessions.go:68-72, 109-113 vs fs_sessions.go:99-103, 151-155). |
| `pkg/recording/ssh` | `untested_exports` | covered-export | Count rise lands on directtcpipDissector.Relay/Close (relay.go:43-47), methods of an unexported adapter; the diff only adds wg.Add/Done in relay.go:81-83. |
| `pkg/storage/storagetest` | `has_tests` | other: legacy has_tests false->false | Test-support package of fakes had no tests before or after; this commit only edits fake fields. |

### 5ded0c26b `allow`: Fix TCP proxy half-close hang and broker shutdown delay

Half-close hang fix; findings are a covered one-line export, a few-function p90 and the symmetric goroutine pair. Evidence: `internal/tunnel/stream_readwriter.go:38`, `pkg/recording/protocol/tcp/orchestrator.go:106`, `pkg/recording/protocol/tcp/orchestrator.go:144`. Confidence medium: Relay's once/setErr scaffolding mirrors internal/tunnel/relay.go and leaves `_ = firstErr` dead (orchestrator.go:144); no finding names that copy, and the p90 rise to 12 alone is modest.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/tunnel` | `untested_exports` | covered-export | CloseWrite (stream_readwriter.go:38) is a one-line delegation driven via halfClose's closeWriter interface by the new TCP half-close test (internal/protocol/tcp/handler_test.go, +97). |
| `pkg/recording/protocol/tcp` | `cognitive_p90` | tiny-ratchet | Package has a handful of functions so p90 is Relay's score; Relay gained once/setErr and two selects (orchestrator.go:93-145) for the half-close fix, a linear coordination block. |
| `pkg/recording/protocol/tcp` | `dup_blocks` | boilerplate-dup | server->client vs client->server goroutines (orchestrator.go:106-124) are the symmetric relay pair. |

### 33851d167 `allow`: Add deterministic HMAC-based session IDs with ref counting

Deterministic session IDs; the only finding is a self-overlapping HMAC write sequence. Evidence: `internal/broker/server.go:192`, `internal/broker/options.go:24`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/broker` | `dup_blocks` | boilerplate-dup | The new block is the mac.Write(x); mac.Write([]byte{0}) chain in computeSessionID (server.go:192-199) matching itself shifted by two lines; cited options.go:24 is legacy option funcs. |

### 3feff9a4f `allow`: Fix kubectl hanging: re-encode chunked responses for keep-alive connections

Chunked re-encoding fix; a read-loop idiom and a small p90 move, nothing a reviewer would split (the real defect, ignored write errors in writeChunked, is not a metric issue). Evidence: `internal/protocol/httpproxy/forward.go:64`, `internal/protocol/httpproxy/forward.go:87`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | writeChunked's read loop (forward.go:87-101) matches the buf/Read/if n>0/if err idiom used by copyWithRecording elsewhere. |
| `internal/protocol/httpproxy` | `cognitive_p90` | tiny-ratchet | ServeHTTP gains a 3-clause needsChunked predicate and an if/else around the copy (forward.go:64-70, 79-83); ~11 functions so p90 tracks the second-largest. |

### 383d367f9 `allow`: Fix SSH recording: dissector output now reaches blob store, port-forward uses NDJSON

SSH dissector output fix; exports are reached through production delegation and the dup is pre-existing case arms. Evidence: `pkg/recording/pipeline/plan.go:65`, `pkg/recording/session/manager.go:85`, `pkg/recording/protocol/ssh/orchestrator.go:304`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `pkg/recording/pipeline` | `untested_exports` | covered-export | Ext (plan.go:65) is a 5-arm switch reached through OpenRecordingWithPrefix; ssh orchestrator tests assert sftp-operations-*.jsonl names. |
| `pkg/recording/protocol/ssh` | `dup_blocks` | boilerplate-dup | Pair is the two OpenRecordingWithPrefix call sites (orchestrator.go:204-206 vs 253-254) / pre-existing sftp-vs-scp case arms (orchestrator.go:304-322) that gained one logFile line. |
| `pkg/recording/protocol/ssh` | `untested_exports` | covered-export | Cleanup (orchestrator.go:420) is on unexported tempDirDissector, called by orchFlusher.Close in the dissector tests. |
| `pkg/recording/session` | `untested_exports` | covered-export | OpenRecordingWithPrefix (manager.go:85) is what OpenRecording delegates to; ssh tests assert its prefixed blob names. |

### a3a053ea4 `block`: Fix recording pipeline: wire format-aware recorders into all orchestrators

Wires format-aware recorders by pasting the same format switch and wrapper type into three orchestrators instead of one shared factory. Evidence: `pkg/recording/protocol/ssh/orchestrator.go:366`, `pkg/recording/protocol/tcp/orchestrator.go:204`, `pkg/recording/protocol/http/orchestrator.go:344`, `pkg/recording/protocol/tcp/orchestrator.go:90`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | The format switch and pcapngStreamRecording wrapper are pasted into ssh (orchestrator.go:220-227, 366-382), tcp (orchestrator.go:90-101, 202-218) and http Hijack (orchestrator.go:344-360). |
| `pkg/recording/format/pcapng` | `untested_exports` | covered-export | NewRecording/Write (writer.go:394, 409) are thin wrappers over the tested pcapngWriter, driven by the new tcp PCAPNG test (tcp/orchestrator_test.go, +69). |
| `pkg/recording/protocol/http` | `dup_blocks` | boilerplate-dup | orchRecConn Read vs Write (orchestrator.go:393-407) symmetric pair. |
| `pkg/recording/protocol/ssh` | `dup_blocks` | real | pcapngStreamRecording (orchestrator.go:366-382) is identical to asciicastStreamRecording apart from the inner type, and the switch arms repeat (orchestrator.go:212-227); one wrapper over recording.Recording would do. |
| `pkg/recording/protocol/ssh` | `untested_exports` | covered-export | pcapngStreamRecording.Write (orchestrator.go:374) is on an unexported type. |
| `pkg/recording/protocol/tcp` | `dup_blocks` | boilerplate-dup | Repeated streamWriterRecording fallback arm inside the new switch (orchestrator.go:94-101); the copy worth fixing is cross-package. |
| `pkg/recording/protocol/tcp` | `max_nesting` | real | Format switch nested in `else if h != nil` inside `if len(plan.Outputs)` in Relay (orchestrator.go:85-101); extract openRecording(). |
| `pkg/recording/protocol/tcp` | `untested_exports` | covered-export | pcapngStreamRecording.Write (orchestrator.go:210) one-line delegation on an unexported type, exercised by the PCAPNG test. |

### 878d5f068 `allow`: Fix SFTP/SCP file capture: upload captured files to blob store before cleanup

SFTP/SCP capture upload fix; an I/O idiom and one export on a constants-heavy package already far over the ceiling. Evidence: `pkg/recording/protocol/ssh/orchestrator.go:476`, `pkg/recording/protocol/ssh/sftp/message.go:114`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | uploadFile (orchestrator.go:476-492) is the open/defer-close/blobs.Write/io.Copy idiom; nearest relatives (storage/flusher.go:59, stream_writer.go:135) are not copies. |
| `pkg/recording/protocol/ssh/sftp` | `exported_symbols` | over-ceiling-growth | 126->127 for one Message.Encode (message.go:114); the 126 is mostly SSH_FXP opcode constants. |

### fbb05c060 `allow`: Add DefaultHTTPRules and wire into broker recording router

Adds a one-rule table and registers it in main; nothing to fix. Evidence: `pkg/recording/protocol/http/rules.go:8`, `cmd/broker/main.go:200`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | DefaultHTTPRules one-entry table (rules.go:8-16) matches the kubernetes/tcp rule tables. |
| `cmd/broker` | `internal_imports` | over-ceiling-growth | Composition root adds rechttp (main.go:32) to register the rules (main.go:200); main is the place for it. |
| `pkg/recording/protocol/http` | `untested_exports` | covered-export | DefaultHTTPRules (rules.go:8) is a constant one-entry rule table with no logic. |

### 9fbbe74f4 `block`: Add SQLite-backed unified SessionStore (S-177)

The SQLite store is fine, but compliance gains a third parallel stream struct and an untested concrete sqlite adapter that push it over the export ceiling. Evidence: `pkg/compliance/sessions.go:114`, `pkg/compliance/sqlite_streams.go:18`, `pkg/compliance/store_sessions_test.go:288`, `pkg/storage/sqlite/store.go:176`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | Matches are sqlite open/err/os.Exit in main, Option/WithLogger shells (sqlite/store.go:26-39) and field-copy converters; the StreamRecord-mirrors-session.Stream smell is what the exported_symbols row names. |
| `cmd/broker` | `dup_blocks` | boilerplate-dup | flag.String registration run matching itself after the new -db flag (main.go:43-46). |
| `pkg/compliance` | `dup_blocks` | boilerplate-dup | Struct field-copy converters and RFC3339 IsZero formatting (sqlite_streams.go:30-45; store_sessions.go:117-131). |
| `pkg/compliance` | `exported_symbols` | real | 54->61 crosses 60 with avoidable surface: StreamRecord duplicates session.Stream field for field (sessions.go:114-125) and SQLiteStreamLister/NewSQLiteStreamLister put a concrete sqlite adapter in the domain package (sqlite_streams.go:13-20). |
| `pkg/compliance` | `untested_exports` | real | NewSQLiteStreamLister/ListSessionStreams (sqlite_streams.go:18, 22) have no test; store_sessions_test.go:288 uses a fakeStreamLister. |
| `pkg/storage/sqlite` | `dup_blocks` | boilerplate-dup | New package; CRUD idiom (PRAGMA exec chain store.go:53-61, lock/Exec/wrap at :176, WHERE-1=1 appends, rows loops). Only scanSessionRecord vs scanSessionRecordRows is worth a comment. |

### da4d686e6 `block`: Fix K8s exec recording corruption, add per-stream playback to compliance UI

Copies the 84-line WebSocket frame extractor into a new package without moving its tests or deleting the original, and adds an untested per-stream recording lookup. Evidence: `pkg/recording/wsframe/extractor.go:33`, `internal/protocol/kubernetes/wsframe.go:32`, `pkg/compliance/store_sessions.go:161`, `pkg/compliance/server.go:141`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | real | pkg/recording/wsframe/extractor.go:15-98 is an 84-line copy of internal/protocol/kubernetes/wsframe.go:12-105 (Feed renamed Filter); the original stays as dead production code kept alive by its tests. |
| `cmd/compliance-ui` | `cognitive_p90` | tiny-ratchet | Single function main gains an if-sqlite-exists-else-dir branch (main.go:48-66). |
| `cmd/compliance-ui` | `max_nesting` | tiny-ratchet | 1->2 from `if err` inside `if os.Stat` (main.go:49-53); same edit. |
| `pkg/compliance` | `exported_symbols` | over-ceiling-growth | 61->62 for RecordingFileByStream (store_sessions.go:161) on a package already over 60; the crossing happened in 9fbbe74. |
| `pkg/compliance` | `sloc` | other: marginal ceiling crossing | 963->1012 by an ordinary per-stream playback feature (store_sessions.go, server.go:141-155); a reviewer would not split for 12 lines over. |
| `pkg/compliance` | `untested_exports` | real | RecordingFileByStream (store_sessions.go:161) is reached only via server.go:141-155 `?stream=`; no test hits either. |
| `pkg/recording/protocol/http` | `dup_blocks` | boilerplate-dup | Option shells WithLogger/WithFlushInterval/WithFrameFilter (orchestrator.go:35-49). |
| `pkg/recording/wsframe` | `untested_exports` | real | NewExtractor/Filter (extractor.go:22, 33) have no tests; the tests stayed on the internal copy, and the K8s exec corruption fix depends on this path. |

## Sampled passed commits (10)

| Commit | Verdict | Subject | Why | Evidence |
| --- | --- | --- | --- | --- |
| 88abdc4c0 | `allow` | Add K8s HTTP request classifier for recording policy decisions (S-153) | New K8s request classifier (134 lines) with a 330-line test; small functions, no copy, no package state. | `internal/protocol/kubernetes/classify.go:22`, `internal/protocol/kubernetes/classify_test.go:1` |
| fb59d3ab7 | `allow` | Move logutil, audit, and session packages to pkg/ for cross-team import (S-163) | Package move of logutil/audit/session to pkg/ with import rewrites and README deletions (+130/-228); no new logic. | `pkg/logutil/logutil.go:1`, `pkg/session/manager.go:1` |
| 006bfffb6 | `allow` | Move web package to pkg/compliance for cross-team import (S-165) | Rename internal/web -> pkg/compliance, one package-clause line per file; nothing added. | `pkg/compliance/server.go:1`, `cmd/compliance-ui/main.go:14` |
| 2efbd012b | `allow` | Refactor SSH handler to delegate channel relay to sshrecord (S-170) | Deletes the duplicated relay/peeker code from internal/protocol/ssh (channel_relay.go -238, handler.go -65) by delegating to sshrecord; HandleChannel gains a dir-empty fast path and stderr recording, modest complexity. | `internal/protocol/ssh/channel_relay.go:1`, `pkg/recording/sshrecord/recorder.go:238` |
| 7c96483bd | `allow` | Fix relay and recording-conn shutdown hangs, add --token to e2e docs | Adds bounded waits (select with time.After) to two shutdown paths and one const; nothing the gate measures regressed. | `internal/tunnel/relay.go:118`, `pkg/recording/format/pcapng/recording_conn.go:13` |
| 2e5d78086 | `allow` | Fix web/embed.go compile failure when compliance build dir is empty | One-line go:embed pattern change. | `web/embed.go:10` |
| 2910d8dba | `allow` | Fix TCP proxy producing session.har: change router default to PCAPNG and add protocol catch-all rules | Changes the router default to PCAPNG and adds one-entry catch-all rule tables; trivial and tested. | `pkg/recording/protocol/tcp/rules.go:9`, `pkg/recording/pipeline/router.go:45` |
| 5b3c3eaa8 | `allow` | Use random UIDs for stream blob names to avoid collisions across connections | Swaps a sequence counter for random stream UIDs (removes a mutable field); the ignored rand.Read error is not a gate metric. | `pkg/recording/session/manager.go:183`, `pkg/recording/pipeline/plan.go:46` |
| e2697546c | `allow` | Fix HTTP upgrade relay ownership and e2e recording assertions | Moves hijack relay ownership from ResponseWriter to the K8s OnUpgrade callback; small, removes a field and an import. | `internal/protocol/httpproxy/response_writer.go:81`, `internal/protocol/kubernetes/handler.go:66` |
| 6fe1121a1 | `allow` | Fix orchRecConn Read/Write direction for upstream exec recordings | Swaps Read/Write recording directions in orchRecConn; same code reordered. | `pkg/recording/protocol/http/orchestrator.go:404`, `pkg/recording/protocol/http/orchestrator.go:418` |
