# External corpus of agent-authored commits

Date: 2026-09-28. Machine: darwin/arm64, 14 cores, Go 1.27.1.

This repository's own history is one repository reviewed by one person.
Whether the gate generalizes has to be measured on others. This note
records the corpus in `calibration/corpus-commits.yaml`: how its
repositories were chosen, how they were replayed with
`calibration/replay` (SPEC.md 11.3) and labeled by rule with
`calibration/replay/label`, the counts, and what the rule gets wrong.

## What counts as an agent commit

A commit is agent-authored when one of its `Co-Authored-By` trailer values
(the replay's `co_authored_by`), or its `author name <author email>`,
contains one of the corpus entry's `agent` strings, ignoring case. The
strings name coding agents: Claude Code (`noreply@anthropic.com`, the
address every Claude trailer carries), Codex, Amp (`amp@ampcode.com`),
GitHub Copilot (the coding agent `copilot-swe-agent`, the Copilot app and
Copilot Autofix, all `...+Copilot@users.noreply.github.com`), Cursor's
agent (`cursoragent@cursor.com`) and Factory's Droid (`factory-droid`).
Each entry lists only the agents that appear in its range. A dependency
bot (`dependabot[bot]`, `renovate[bot]`) or a CI bot (`github-actions[bot]`)
is not an agent: it writes no code a reviewer would judge. A trailer says
the agent took part in the change, not that it wrote all of it; a human may
have edited the result, and a commit an agent wrote without a trailer reads
as not agent-authored. Non-agent commits of each range are replayed and
labeled too, with `agent: false` on the label, so the validation can report
agent commits alone or every commit.

## Selection

A repository is in the corpus when all of these hold:

1. Public, permissively licensed, Go at the module root (`dir: .`), so the
   replay loads one module per commit.
2. Hundreds of first-parent commits whose trailers or author name an agent,
   counted on a blobless clone with `git log --first-parent --format=%B`.
3. A different organization, maintainer or agent from the other entries,
   so no one workflow dominates.

For a long history the range is the most agent-dense window of 250
first-parent commits, found by sliding a window over the history's agent
flags, plus the 20 commits after it, so the fix-up part of the rule sees a
full window for all but the range's last 20 commits.

| Repository | First-parent commits | Agent commits (whole history) | Range | Replayed | Agent commits replayed |
| --- | --- | --- | --- | --- | --- |
| `wesm/roborev` | 975 | 488 | commits 1 to 272 (2026-01-05 to 2026-02-04) | 272 | 227 |
| `github/github-mcp-server` | 1,140 | 297 | commits 836 to 1105 (2026-05-19 to 2026-08-25) | 270 | 136 |
| `steveyegge/beads` | 7,446 | 2,774 | commits 3769 to 4038 (2026-01-14 to 2026-01-27) | 270 | 175 |
| Total | | | | 812 | 538 |

The whole-history agent counts come from the candidate scan (author and
trailer values matched against `claude|codex`, `copilot` and `claude|amp`
respectively) and are approximate; the replayed counts are exact, from the
committed `commits.jsonl` under each entry's rule, and
`go test ./calibration/replay/label` fails if `agent_commits` drifts from
them.

### Considered and rejected

Counts are first-parent commits whose message matches `Co-authored-by:
.*<agent>`, from a blobless clone of each default branch on 2026-09-28.

| Repository | Agent commits | Reason |
| --- | --- | --- |
| `steveyegge/gastown` | ~3,100 Claude of 5,659 | Same maintainer and workflow as beads. Hundreds of commits are authored by Gas Town's own agent identities (`mayor`, `furiosa`, `gastown/crew/max`), a share of them without a trailer, so a trailer rule would misclassify them. Kept in reserve. |
| `microsoft/typescript-go` | ~390 Copilot of 2,487 | A port of the TypeScript compiler; its module is large enough that a replay of hundreds of commits did not fit this run. Not tried. |
| `goreleaser/goreleaser` | ~170 Copilot of 7,179 | Agent commits are 2% of the history and spread thin; no dense window. |
| `charmbracelet/crush` | ~26 Crush trailers of 2,857 | Few agent trailers. |
| `mark3labs/mcp-go` | 4 Claude trailers of 549 | Most "Claude" mentions are in squash-merge bodies, not trailers; too few. |
| `modelcontextprotocol/go-sdk` | ~15 of 812 | Too few. |
| `humanlayer/humanlayer` | 32 Claude trailers of 643 | Too few, and several Go and TypeScript modules below the root. |
| `sst/opencode` | ~250 opencode, ~34 Claude of 14,767 | TypeScript monorepo; a replay takes one `--dir`, and the agent commits spread across its workspaces. |

## Replay

Clones were full clones under the scratch directory; network was used for
the clones and for module downloads on first load.

```sh
go run ./calibration/replay --repo <clone>/roborev \
  --range 21e05fe6758eed3f984f68821ab0466278562092 \
  --out calibration/data/replay-roborev-2026-09-28 --parallel 4
go run ./calibration/replay --repo <clone>/github-mcp-server \
  --range d4e1231cf7d7d54b742fde715fff23e3b04d729b..f0baf1c80276711bd7e26d4b8fdc26dae7802d5f \
  --out calibration/data/replay-github-mcp-server-2026-09-28 --parallel 4
go run ./calibration/replay --repo <clone>/beads \
  --range 218ee6d2b411d701e63a5b61d7bda9f21abede5e..de64d817e588ed8048f7f00308edb1d1fbbc1591 \
  --out calibration/data/replay-beads-2026-09-28 --parallel 4
```

The replay's `run.json` names each repository after its clone directory,
so the clones are named after the corpus entries. github-mcp-server was
first cloned under another name; its `run.json` was rewritten by a rerun
over the complete directory after the rename, which is why it records
`written: 0, skipped: 270`. Every commit of each range has a commit row:

| Repository | Commits | Loaded | Not loaded | Agent commits loaded | Gate failed | Package rows | Replay time (sum of `wall_ms`) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| roborev | 272 | 272 | 0 | 227 | 147 | 592 | 55 min |
| github-mcp-server | 270 | 267 | 3 | 134 | 155 | 500 | 101 min |
| beads | 270 | 196 | 74 | 127 | 136 | 425 | 145 min |
| Total | 812 | 735 | 77 | 488 | 438 | 1,517 | |

The three runs went side by side at `--parallel 4` each, so the times are
inflated by contention; a beads commit took about 45 s (a large dependency
graph). A commit that does not load is one whose own tree does not
type-check: in github-mcp-server, three commits where a test file calls
`buildStaticInventory` with the old arity; in beads, 74 commits whose test
files (`compactor_unit_test.go`, `dolt/concurrent_test.go`) or `cmd/bd` did
not compile, broken builds that later commits repaired. They keep their
rule label but, having no gate verdict, are left out of the validation's
tables. 488 agent-authored commits have a gate verdict, above the 300 the
corpus needs.

Two run hiccups, neither affecting the rows: git refused one `worktree add`
in each of the github-mcp-server and beads runs (`failed to read
.git/worktrees/.../commondir`), a race between concurrent worktree
additions in one repository. The replay stopped without recording the
commit, as designed, and the same command resumed it. The data directories
total about 3.5 MB and are committed whole.

## Labels

```sh
go run ./calibration/replay/label --repo <clone>/<name> --data calibration/data/replay-<name>-2026-09-28 \
  --out calibration/replay/labels/<name>.yaml --corpus calibration/corpus-commits.yaml --name <name>
```

The rule (`calibration/replay/README.md`, "Rule labels") marks a commit
`block` when a later commit of the range reverts it (part `revert`), or
when one of the next 20 first-parent commits, with a subject starting
`fix` or `revert`, changes a Go function the commit changed (part
`fixup`), and `allow` otherwise. Functions are matched by file and
`Receiver.Name` from a `go/parser` parse of both sides of each `-U0` hunk.
A function literal in a package-level variable (a cobra command's `RunE`
in `var depListCmd = &cobra.Command{...}`) counts as the function
`var depListCmd`. The review of this change found beads' commands written
that way invisible to a first version that read only `func` declarations;
counting them raised beads' `block` labels from 36 to 52 and left the other
two repositories unchanged, since their commands and tool handlers are
built inside functions.

| Repository | Commits | `block` | part `revert` | part `fixup` | both | `allow` | changed no Go function | short window | agent `block` of agent commits |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| roborev | 272 | 76 | 0 | 76 | 0 | 196 | 97 | 10 | 76 of 227 |
| github-mcp-server | 270 | 45 | 1 | 44 | 0 | 225 | 88 | 13 | 24 of 136 |
| beads | 270 | 52 | 0 | 52 | 0 | 218 | 63 | 14 | 34 of 175 |
| Total | 812 | 173 | 1 | 172 | 0 | 639 | 248 | 37 | 134 of 538 |

"Changed no Go function" counts `allow` labels on commits the fix-up part
could not see (no function of a non-test `.go` file touched). "Short
window" counts `allow` labels on commits with fewer than 20 later commits
in the range. Of the `block` commits that loaded, the replayed gate failed
68 of 76 in roborev, 37 of 44 in github-mcp-server and 29 of 32 in beads.
What that means for recall and false failures is for the validation.

The revert part fired once in 812 commits: github-mcp-server's
`Revert "ci(lint): skip remote config schema verification"` reverted a
non-agent commit that changed CI configuration and no Go code. Reverts are
rare in all three histories, so nearly every `block` label here comes from
the fix-up part, the weaker of the two. There are 172 fix-up labels (134 on
agent commits), enough to measure recall only with that caveat attached.

## Known failure modes

- **An unrelated fix in the same function.** A fix-up of one line in a
  long function marks every earlier commit that touched that function in
  the window, whether or not it caused the bug. Long, central functions
  (a TUI's `Update`, a command's `RunE`) draw most such labels. The rule
  over-counts `block`. A "fix" that only satisfies a linter (beads'
  `fix(lint): add nolint comments ...`, which marks the commit that wrote
  `dolt.New`) counts the same as a bug fix.
- **An unreverted bad change reads as `allow`.** A change nobody fixed
  within 20 commits, fixed later, fixed in another function, or fixed by a
  commit whose subject does not start with `fix` (`feat: handle nil ...`,
  `refactor: ...`, a merge commit) is `allow`. The rule under-counts
  `block`, and a commit near the end of a range sees fewer than 20 later
  commits (the label's reason says so).
- **Squash-merged repositories hide fix-ups.** A pull request that went
  through review rounds lands as one squashed commit; the fixes inside it
  never appear on the first-parent history, so the review that caught them
  is invisible, and the squash commit reads as `allow`. roborev and
  github-mcp-server are squash-merged. Merge-commit histories hide them the
  same way: a merge's subject is `Merge pull request ...`, never `fix`, and
  its diff is the whole branch.
- **Changes outside Go functions.** A commit that only changes types,
  constants, variables, tests or non-Go files touches no function, so the
  fix-up part cannot fire for it; only a revert can mark it `block`.
- **Coarse function identity.** A function literal is attributed to the
  declaration or package-level variable that holds it, so a fix to one
  closure of a large function or handler table matches every earlier edit
  to any other closure in it.
- **Renames and moves.** A function renamed or moved to another file
  between the commit and its fix is a different function to the rule.
- **Revert matching.** A revert is recognised by `This reverts commit
  <hash>` or a `Revert "<subject>"` subject; a manual revert with any other
  message is invisible, and a subject repeated by an unrelated earlier
  commit can match the wrong one (the latest earlier commit with that
  subject is taken).
