# Hand labels for the five 2026-10-13 replays

## Relabel 2026-10-14: the ztp placeholders

Seven ztp labels were placeholders: `allow` with the reason "not loaded: the replay could not load the module at this commit", written because the 2026-10-13 replay did not load those commits. The 2026-10-14 replays load five of them, so the validation had scored them as `allow` without anyone having read them, and c4af29e was one of ztp's eleven false failures (`calibration/notes/ztp-remaining-2026-10-14.md`). No other repository's hand labels, `shhh-hand-2026-10-14*.yaml` included, carry the placeholder.

Each of the five was read the way the labels below were made: the diff from its parent, the gate's findings in `calibration/data/replay-ztp-2026-10-14-min60/packages.jsonl`, and the later first-parent history for a split or an extraction. The reasons keep the cause-and-evidence format, the per-finding causes are in `ztp-hand-notes.md`, and every label is `provenance: confirmed`.

| Commit | Was | Now | Cause |
| --- | --- | --- | --- |
| c4af29e | `allow` (placeholder) | `block` (`dup_blocks_cross_pkg`) | real: `pkg/recording/http/router.go:146` copies `policy.matches` and `boolPtr` (`pkg/recording/policy/policy.go:128`) token for token, and `metadata.go:23` is a fourth copy of the NDJSON file logger (`dissect/directtcpip/logger.go:16`). Twelve first-parent commits later 056cf0d deleted both matchers for one `pipeline.matchesRule` (`pkg/recording/pipeline/router.go:74`) |
| bb7f0cd | `allow` (placeholder) | `allow` | other: the cross block is re-keyed, since deleting the dead directtcpip logger changed the package set of an unchanged metadata/scp logger block (`format/metadata/metadata.go:44`, `protocol/ssh/scp/logger.go:35`). The `max_nesting` 3 to 5 is an asciicast case with a fallback nested inside `relayPassive`'s existing guards (`protocol/ssh/orchestrator.go:206`); a3a053e extended it in place and no later commit split it |
| c0373bb | `allow` (placeholder) | `allow` | other: a rename-only commit. The new `nethttp` import alias lines up with the ssh orchestrator's `gossh` import, so an existing http/ssh orchestrator block grows into the import list (`protocol/http/orchestrator.go:11`) |
| 9a006d6 | `allow` (placeholder) | `allow` | sampled pass, read: deletes the unimported `pkg/session` (-416 lines) and adds nothing |
| 130d2c8 | `allow` (placeholder) | `allow` | no-baseline + boilerplate-dup: a net de-duplication refactor. The `internal/broker` and `internal/protocol/ssh` rows had no baseline although both packages existed at the parent, so blocks already there (`channel_relay.go:275`) ratcheted from zero, and the two added cross blocks are functional-option setters (`internal/broker/options.go:60`, `internal/protocol/ssh/handler.go:46`) |

The current binary judges a package that existed at the baseline but failed to load there against the capacity ceilings only. Under it, 130d2c8's two `dup_blocks` findings would not fire. The stored rows predate that change and still show them, and the label rests on the code either way.

4539a1d and 056cf0d still do not load under the 2026-10-14 replays, because each is broken at the commit itself. 4539a1d imports `pkg/recording/http`, which only its child c4af29e adds. At 056cf0d, `cmd/broker` calls `broker.WithRecordingBlobStore`, which is not defined there. Their placeholders stay, and the validation leaves them out.

bb7f0cd is the closest call. The `ztp-remaining` reading calls its nesting real, and a3a053e's label counts the same pattern in tcp as real. It stays `allow` because the standard for these labels is a later split or extraction, or a copy plain on reading, and the nested switch is neither. The gate fails bb7f0cd either way, so marking it `block` would add one caught `block` commit and remove one false failure.

Re-running `calibration/validate` on the shipped default over the five 60-token replays (`calibration/reports/gate-validation-2026-10-14.md`) moves the headline. Pooled with capacity-only labels set aside, the gate fails 13 of 15 `block` commits (86.7%, was 12 of 14, 85.7%) and 14 of 137 `allow` commits (10.2%, was 15 of 138, 10.9%), so J goes from +74.8 to +76.4 points. Over every author it fails 18 of 21 and 38 of 275 (85.7% and 13.8%, was 85.0% and 14.1%). On ztp alone it fails 9 of 9 and 10 of 48 (20.8%, was 22.4%). The passed commits read as a sample rise from 33 to 34 with 9a006d6. SPEC.md 8.2's Measured column and the before and after tables of `calibration/reports/thresholds-2026-10-14.md` were updated, and every retune decision holds (that report's Relabel section). The tables below this section are the 2026-10-13 drafting and are left as written.

Date: 2026-10-14. Replays: `calibration/data/replay-{go-starter,ztp,shhh,crush,ai-shell}-2026-10-13/` (60 first-parent commits each, binary c28a78f, configuration `rebuild-2026-09-28-claude-code-opus`). Labels: `calibration/replay/labels/<name>-hand.yaml`, with each finding's cause and `file:line` evidence in `<name>-hand-notes.md` beside them. Report: `calibration/reports/gate-validation-2026-10-14-hand.md`, hand labels first and split-or-extraction labels second.

Every label is a draft (`provenance: proposed`) awaiting the user's confirmation. None is confirmed yet.

## How the labels were made

Every commit the replay failed (94) was read diff against findings, using the standard of `calibration/notes/field-adjudication-2026-10-13.md`. A commit is `block` when it introduced something a careful reviewer would ask to fix before merging, or that measurably makes a package harder to maintain, and one of the gate's findings names it. A finding counts as naming the problem when its package and metric cover it, even if the cited line points at an older block. A commit with several findings gets one verdict, `block` if any finding is real, but each finding keeps its own cause from the field taxonomy: `real`, or for an allowed finding `boilerplate-dup`, `covered-export`, `over-ceiling-growth`, `tiny-ratchet`, `no-baseline`, `double-report`, `immutable-global` or `other` (what it is).

Recall needs a denominator, so 60 passed commits that touched a package were also read: a stratified sample spread evenly over each range. ztp and crush have only 10 and 9 such commits, so all of them were read; the other 41 were split 14, 14 and 13 across go-starter, shhh and ai-shell. A sampled commit is `block` only where the gate missed a regression of a kind it claims to catch.

The remaining commits are labelled `allow` with a reason saying so:

- commits that touched no package;
- commits the replay did not load (left out of the validation);
- 29 passed commits outside the sample, whose diffs were not read (14 go-starter, 6 shhh, 9 ai-shell).

The labels loader rejects unknown fields, so `cause` and `evidence` are not YAML fields. Each `reason` starts with the commit's cause and ends with its `file:line` evidence, and `metrics` names the rules behind a `block`.

`agent` follows the rule of `calibration/corpus-commits.yaml`: a Co-Authored-By trailer or author naming an agent. That gives:

| Repository | Agent-written commits | Which |
| --- | --- | --- |
| ztp | 60 of 60 | all |
| shhh | 60 of 60 | all |
| go-starter | 36 of 60 | its first 20 commits carry no trailer and read `agent: false` |
| crush | 2 of 60 | 7dc88e2 and cce8871 |
| ai-shell | 0 of 60 | none |

The drafting was split by repository across five reading passes. Every `block` was then re-read against the cited code before it was written into a labels file.

## Counts

| Repository | Failed | `block` | `allow` | Findings on failed commits | Sampled passed | Missed regressions found |
| --- | --- | --- | --- | --- | --- | --- |
| go-starter | 13 | 1 | 12 | 22 | 14 | 0 |
| ztp | 26 | 9 | 17 | 91 | 10 | 0 |
| shhh | 24 | 8 | 16 | 98 | 14 | 0 |
| crush | 17 | 3 | 14 | 51 | 9 | 0 |
| ai-shell | 14 | 2 | 12 | 25 | 13 | 0 |
| total | 94 | 23 | 71 | 287 | 60 | 0 |

The next table gives the cause of every finding on a failed commit. The real findings are 43; the rest are false positives, including the non-real findings on `block` commits.

| Cause | go-starter | ztp | shhh | crush | ai-shell | total |
| --- | --- | --- | --- | --- | --- | --- |
| real | 1 | 18 | 14 | 7 | 3 | 43 |
| boilerplate-dup | 7 | 34 | 16 | 9 | 1 | 67 |
| over-ceiling-growth | 0 | 3 | 38 | 21 | 0 | 62 |
| covered-export | 5 | 18 | 14 | 4 | 0 | 41 |
| tiny-ratchet | 2 | 8 | 0 | 0 | 0 | 10 |
| immutable-global | 1 | 4 | 2 | 2 | 0 | 9 |
| no-baseline | 1 | 0 | 4 | 0 | 0 | 5 |
| double-report | 0 | 1 | 1 | 0 | 0 | 2 |
| other | 5 | 5 | 9 | 8 | 21 | 48 |

The 71 `allow` commits break down by the causes of their findings as follows.

| Cause | go-starter | ztp | shhh | crush | ai-shell |
| --- | --- | --- | --- | --- | --- |
| boilerplate-dup | 6 | 21 | 8 | 8 | 1 |
| over-ceiling-growth | 0 | 2 | 16 | 16 | 0 |
| covered-export | 5 | 10 | 9 | 4 | 0 |
| tiny-ratchet | 2 | 4 | 0 | 0 | 0 |
| immutable-global | 1 | 2 | 2 | 2 | 0 |
| no-baseline | 1 | 0 | 4 | 0 | 0 |
| other | 3 | 2 | 9 | 5 | 19 |

The `other` findings break down by repository:

- **ai-shell:** 15 legacy `has_tests` false->false findings in a repository with no tests, a revert restoring older code, and small complexity rises in an already-deep function.
- **shhh:** package splits adding an import edge to the split-off package, and duplicates moved by a split.
- **crush:** a revert, and guards added to functions already far over the complexity ceiling.
- **go-starter:** composition-root wiring, and duplicates moved by a module refactor.

No commit-level label is `other: undecided`. One finding is undecided: the cross-package pair behind crush ec2fe437's `dup_blocks_cross_pkg`, which could not be identified. That commit is `block` on its other findings.

## Headline numbers

The validation scores the hand labels first and the split-or-extraction labels second. The report uses the default `--agent-only`, so it drops `agent: false` labels: go-starter's first 20 commits (24 labels dropped in all), crush except two commits, and all of ai-shell.

| View | Labels | Gate recall | Gate false failures |
| --- | --- | --- | --- |
| all corpora pooled | hand | 17 of 17 (100.0%) | 39 of 134 (29.1%) |
| all corpora pooled | split-extract | 8 of 8 (100.0%) | 48 of 143 (33.6%) |
| all pooled, capacity-only set aside | hand | 15 of 15 (100.0%) | 39 of 134 (29.1%) |
| all pooled, capacity-only set aside | split-extract | 8 of 8 (100.0%) | 46 of 141 (32.6%) |
| ztp | hand | 9 of 9 | 17 of 44 (38.6%) |
| ztp | split-extract | 4 of 4 | 22 of 49 (44.9%) |
| shhh | hand | 8 of 8 | 16 of 52 (30.8%) |
| shhh | split-extract | 4 of 4 | 20 of 56 (35.7%) |

The two capacity-only hand labels are ztp f430f2f (`exported_symbols`) and shhh ac1110f4 (`largest_file_sloc`).

A second run over every author, with `--agent-only=false`, was written to scratch and not committed:

| View | Labels | Gate recall | Gate false failures |
| --- | --- | --- | --- |
| all pooled | hand | 23 of 23 (100.0%) | 71 of 270 (26.3%) |
| all pooled | split-extract | 17 of 17 (100.0%) | 77 of 276 (27.9%) |
| go-starter | hand | 1 of 1 | 12 of 59 (20.3%) |
| crush | hand | 3 of 3 | 14 of 57 (24.6%) |
| ai-shell | hand | 2 of 2 | 12 of 58 (20.7%) |

**Recall on the sample.** No sampled passed commit hid a regression, so recall is 100% on every view. The block commits behind it are the 23 failed `block` labels, plus 0 missed among the 60 passed commits read. The 0 of 60 is an estimate, not a census: 29 passed commits that touched a package were not read. The report does not print the sample size beside its recall, so this note carries it. The report's headline names the hand labels in its corpus list and its "Label sets" table.

**What changes under hand labels.** The false-failure rate barely moves (29% against 34% on the agent-only pool). The gate's problem is precision, not the labels: only 23 of the 94 failed commits hold a real problem, and 43 of 287 findings are real. The causes repeat the field adjudication:

- boilerplate duplicates (67 findings);
- growth in packages already over a ceiling (62);
- covered exports (41).

**How the two label sets disagree.** They agree on only 4 `block` commits. The hand labels block 19 commits the split-extract rule allows. These are mostly copies no later commit extracted within the 60-commit window, such as the ztp recording switches and the shhh dry-run copy. The split-extract rule blocks 13 commits the hand labels allow. Those are boilerplate dup rises that a later commit happened to lower, or a later package move counted as a split, such as go-starter 74f7726, 93137e7, f818863 and 96d2cff, and ztp 4c07cb0 and a33321b.

## Five labels for the user to check first

1. **shhh 3ffa8f43, `block` (`dup_blocks`).** `heredocDelimiters` (internal/tools/shelledit.go:108) is a second shell quote scanner beside `splitSegments`. It reads whole lines on purpose, so an apostrophe inside a here-document cannot join lines. A reviewer could accept the second scanner as deliberate.
2. **ztp edd3a53, `block` (`sloc`, `dup_blocks`).** The watch-stream subsystem takes pkg/recording/http from 816 to 1,021 SLOC and copies a short Close/Snapshot sequence (pkg/recording/http/metadata.go:137, :206). The new files are cohesive and tested, so `over-ceiling-growth` is a fair alternative.
3. **shhh 504a1c76, `block` (`dup_blocks`).** `under` (internal/tools/shelledit.go:199) is `underDir` (internal/tools/gitpaths.go:163) re-typed in the same package. The copy is real, but it is three lines and may sit below the detector's block size. The +1 the gate counted may be the routine tee/find argument loops instead, which would make the finding boilerplate.
4. **crush 8661a1c4, `allow` (`dup_blocks`, other: pre-existing copies lengthened).** The same `prevValue` guard was added to five filter blocks that were already copies (internal/ui/dialog/commands.go:231 and others). A reviewer could fairly ask for an `updateFilter` helper at this point.
5. **ai-shell c0738a5, `block` (`changed_func_cognitive_max`).** A 20-line maintainer bug fix adds a second state flag, `waitUntilNewline`, to the streaming loop of `readData` (src/helpers/completion.ts:198, :215), and cognitive complexity rises from 51 to 72. A lenient reviewer would merge it. The next pull request rewrote the same code.

Close behind are:

- go-starter 96d2cff (`allow`): the new cross-package block in the 546-line ratelimit rewrite could not be pinned down.
- ztp f430f2f (`block`): `exported_symbols` rises from 47 to 82 because K8s-specific code landed in a generic package, but many of the new symbols are getters.
- ai-shell 2d2c6bd (`block`): `has_tests` in a repository that never had tests, blocked because the new `shell-history.ts` appends to the user's real shell history without tests.

## Confirmation 2026-10-14

The user delegated the confirmation to the orchestrating session ("you make the decision with the information you have"). Every label's provenance is now `confirmed`. Of the five labels listed above, one was overturned: shhh 3ffa8f43 is `allow`, because the second scanner is a deliberate variant with a stated reason a reviewer accepts. ztp edd3a53 stays `block` (a new subsystem that takes a package past the ceiling is what the capacity rule is for), shhh 504a1c76 stays `block` (a re-typed helper is a copy a reviewer asks to reuse, however short), crush 8661a1c4 stays `allow` (a one-line fix applied to existing copies does not create the pattern), ai-shell c0738a5 stays `block` (the next change rewrote the function). The three "close behind" labels stand as drafted.
