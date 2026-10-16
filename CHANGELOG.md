# Changelog

All notable changes to this project are documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project will adhere to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from its first release.

Changes before this file was created were not recorded here; see the git
history.

## [Unreleased]

### Added

- An `exclude:` list in the configuration keeps files out of the analysis,
  for generated code no header marks and vendored or experimental trees.
  Patterns match a file's path relative to the module root (`*`, `?` and
  `[...]` within a segment, `**` for any number of segments, a directory
  for everything under it). An excluded file is never read and counts in
  no metric, its imports and churn included; a package with every source
  file excluded is listed under "Not analysed" as
  `excluded by config (<pattern>)`. Both extractors apply it, `check` uses
  it on both sides so a new pattern does not read as a deleted package,
  `--debug` logs each excluded file, a malformed pattern is a config error
  naming it, and `config init` writes the key commented out.
- TypeScript files marked as generated are recognised. A non-test `.ts`,
  `.tsx`, `.mts` or `.cts` file whose leading comment block holds
  `@generated`, a `Code generated ... DO NOT EDIT` line, or a "generated"
  line and a "do not edit" or "do not modify" line (any case) counts in
  `files`, `generated_files` and `tokens_est_generated`, which is no longer
  null for TypeScript, and is read for the import metrics and `fan_in` but
  left out of size, complexity, exports, duplication and churn, as in Go. A
  package of generated files only has the role `generated-only`.
- The report names the packages it did not analyse, and why. A directory
  whose Go files build constraints all exclude (a `//go:build ignore`
  generator, a cgo package with cgo off) and a package that does not load
  used to be a log line only, so 77 analysed packages read the same as 80
  with three left out. Text and Markdown now close with a "Not analysed"
  block grouped by reason, the HTML page notes them under the package
  table, and the JSON report and the MCP `report_module` carry a
  `skipped` array. `check` adds `· N not analysed` to its counts line and
  lists them after the heads-ups, and its JSON lists them in the module
  row's `skipped`; they never fail the gate. `rank` ends its table with
  the count, and `--debug` names each.
- A Homebrew cask. `brew install --cask rfizzle/tap/astimate` installs the
  release archive's binary on macOS; goreleaser writes `Casks/astimate.rb`
  to `rfizzle/homebrew-tap` on every release. The darwin binaries are
  signed with a Developer ID and notarized on the release runner when the
  signing secrets are set, so Gatekeeper runs them without a prompt; the
  release still builds, unsigned, without them. The archive now also
  carries `THIRD_PARTY_LICENSES`, the license text of every module the
  binary links (`make third-party-licenses`), and builds are reproducible
  from the tag (the commit's timestamp is the build's). The release
  workflow waits for the commit's `ci` run to finish green before
  goreleaser runs, and fails naming the run otherwise. `docs/release.md`
  is the runbook: the tag, the secrets and what to check afterwards.

- Terminal text for the module report. `astimate report` now prints, by
  default, a verdict sentence, the packages counted as Split, Watch and
  Fine, "Start here" with the three hottest packages and what to do about
  each, and the ten costliest packages (`--all` for every one); with
  `--base` the change report follows. `--format json` prints the data as
  before, and no notice is written. Every sentence comes from the report's
  data, so the text says what the JSON, Markdown and HTML say.
- `astimate check --format text` (the default) is now the change report:
  `FAIL` or `PASS`, the counts, each failing package with its findings
  (what moved against what is allowed, why it matters, where to open),
  one line of heads-ups by rule (`--warnings` lists each), the exempted
  findings with their reasons, skipped packages and the exit code. It no
  longer prints a summary line per package or agent passes; a failing
  package names its rebuild days when the estimate is calibrated. Exit
  codes and the `json`, `hook`, `github` and `md` formats are unchanged.
- Width and colour for both: the terminal's width (100 columns in a
  pipe), the copied and untested columns from 100 columns, one line per
  finding at 80 columns or fewer, long paths cut from the left; colour and
  bars only on a terminal without `NO_COLOR`, or with
  `--color always`. Severity is always a word or sign first.
- One voice across the MCP server and the Stop hook (SPEC.md 8.5 and
  10.1). The new MCP tool `report_module` (`path`, optional `base`)
  returns the verdict (`headline`, `lead`), the three packages to start
  with, each with its story and what to do, and the whole module report,
  so an agent can ask what to split first in one call. `assess_package`
  adds the package's `story` and `todo`. `explain_metric` also explains
  the report's words (`rebuild_cost`, `split`, `watch`, `copied`,
  `untested`, `changes`, `limits`, `hard_to_follow`, `paths`) with the text
  the report page's glossary prints. In the hook format each violation
  keeps its first line and adds `why: ...` and `open file:line` under it;
  the shared sentences also gained a why for `globals`, `untested_exports`,
  `tokens_est` and `internal_imports`.
- `astimate report [dir] [--base ref] --format json`: a module report in
  plain words, the data model every report format will share (SPEC.md
  10.3). Each package gets a band (Split when its rebuild tier is
  PARTITION or it is over two of the five size ceilings, Watch when over
  one, at FEW_PASSES or 25% copied, else Fine), a hotspot score
  (`churn_commits` times `human_days`), its ceilings, its rebuild cost as
  one engineer's time (21 working days a month, 220 a year) and as agent
  sittings (one up to 1 pass, a few up to 3, split it first above), its
  copied code with the two longest repeats, its functions bucketed as easy
  (cognitive complexity up to 10), needing care (11 to 20) or hard (over
  20) with the three hardest named, and the sentences that say what is
  wrong and what to do. With `--base` the report adds the change: the
  check of the changed packages with each finding's base, head, the
  over-the-ceiling and allowed numbers, why the gate cares and where to
  look. It reports and does not gate: it exits 0 whatever the verdict.
  Without `--format` it notes that text output is coming and prints the
  JSON; `check`, `rank` and `assess` output is unchanged.
- `--format md` on `report` and `check`: the module report as Markdown for
  a pull request comment or a step summary (SPEC.md 8.5 and 10.3). `check
  --format md` prints the change block: a heading with the verdict and
  counts, a table of the findings (package, what moved, base to head,
  where to open), the heads-ups by rule, the passed count and the
  cross-package copies before and after. `report --format md` adds the
  headline, the lead, the three packages to start with and the ten largest
  packages, the change block first under `--base`.
- `--format html` on `report`, with `--out` (default
  `astimate-report.html`) and `--open`: the module report as one
  self-contained page, the Repository tab and, with `--base`, the "This
  change" tab of the report design (SPEC.md 10.3). It loads nothing: the
  fonts are embedded latin subsets of Bricolage Grotesque, DM Sans and
  JetBrains Mono (SIL Open Font License), and the bubble map, the
  churn-against-cost scatter and the band ring are laid out in Go (circle
  packing as d3.pack does it) and written as SVG. The page's script only
  switches tabs, sorts, filters, opens rows and shows tooltips; light and
  dark follow the system. Under 1 MB for a 60-package module.
- The GitHub Action takes `html: true`, which writes the HTML report and
  uploads it as the `astimate-report` artifact, and `comment: true`, which
  posts the Markdown as one pull request comment through `GITHUB_TOKEN`
  and updates it in place on the next run, found by a hidden marker (the
  job needs `pull-requests: write`).
- Cyclomatic complexity beside cognitive complexity: `cyclomatic_total`
  and `cyclomatic_p90` per package, and each function's count, McCabe's as
  gocyclo computes it (1 plus each `if`, `for`, `range`, non-default
  `case`, `&&` and `||`; for TypeScript the same over the loops, `case`,
  `catch`, the ternary and `??`). They are reported, not gated: `assess`
  prints them, every JSON report carries them with each function's
  cognitive and cyclomatic complexity in `details.functions`, baseline
  files record them per function, and replay and corpus rows carry them
  from the next run. On this repository the count equals gocyclo's on all
  1,659 functions and its Spearman correlation with cognitive complexity
  is 0.96 (`calibration/notes/cyclomatic-2026-10-14.md`).
- Churn: `churn_commits`, the first-parent commits of a window that
  touched a package's non-test, non-generated files, and `churn_authors`,
  their distinct author emails, read with one `git log` per run. The
  window is the new optional `churn.commits` setting (default 200; 0
  turns churn off). Both are reported, not gated: in `assess` (`n/a`
  outside a git repository or in a shallow clone), `check --format json`, the MCP tools,
  `baseline write`, the replay and `dryrun` rows (each from its commit's
  own history) and the corpus rows. `rank` gains a `CHURN` column, a
  `churn_commits` field in its JSON and `--sort churn`; churn moves
  neither the rebuild estimate nor the tier.
- Replay rows record, on the module row, the cross-package blocks each
  commit may have added (`cross_added`): per set of packages, the blocks'
  lengths, kinds, packages and occurrences with the parent's block lengths
  on the same set. `calibration/validate` re-derives
  `dup_blocks_cross_pkg` from them at the configuration's
  `duplication.min_tokens_cross`, so its recheck reproduces the recorded
  verdicts and a configuration with another floor can be validated on the
  same rows. It also sets aside package rows whose baseline was
  unavailable, with their violations, and says how many.
- A configuration may gate `dup_blocks_code`, the code blocks of
  `dup_blocks`, in place of `dup_blocks`: its finding carries
  `duplication_pct` and `duplicated_lines` as `dup_blocks` would, merges
  into `dup_blocks` when both are gated, cites the first code block (an
  added one first) and is located on it. The default keeps `dup_blocks`:
  on the relabelled hand labels of 2026-10-14 the code-block rule lowers
  the rule's J from +60.8 to +50.4 points and the gate's from +76.4 to
  +72.0, with recall 80.0% against 86.7%
  (`calibration/notes/dup-blocks-code-2026-10-14.md`). `calibration/validate`
  accepts such a rule in a replayed rule's place and re-evaluates it on the
  stored metrics.

- A package that existed at the baseline but failed to load there (a type
  error, a missing module) is no longer judged as new. It is judged
  against the capacity and density `max` values only, with the ratchets
  from zero and the requirement rules skipped, so a parent that did not
  compile no longer fails the commit that fixes it on the blocks the
  package already had. The text and hook output print `baseline
  unavailable for <package>: <reason>; ratchets skipped`, the GitHub
  format adds a notice, the JSON report carries `baseline_unavailable`,
  and replay rows record `"baseline": "unavailable"` beside `"new": true`.
  A package the baseline never had still ratchets from zero.
- `calibration/validate` reports each rule's part in the gate (the
  commits it fired on and those no other failing rule fired on), and, for
  hand labels that mark their sample, how many passed commits were read
  and how many were not.
- `calibration/validate` accepts rows replayed under another configuration
  committed under `calibration/thresholds` when it has the same
  `chars_per_token`, duplication settings, exemptions and rules: each rule
  whose limits differ is re-evaluated on the stored metrics. It used to
  refuse any other `config_version` with "their rules differ".
- A configuration may gate `duplicated_lines` as a density rule: its
  finding names the duplicated lines, cites a duplicate block like the
  other duplication rules and is located on it, and merges into a
  `dup_blocks` finding of the same change, or takes a `duplication_pct`
  one when `dup_blocks` did not fire, so one paste stays one finding. The
  embedded default does not carry the rule: measured over the five
  replays of 2026-10-14 at floors 0, 10 and 20 it catches kubesentry's
  third copy that merged two blocks, but lowers the gate's pooled J on the
  hand labels by 1.4 points at every floor
  (`calibration/notes/duplicated-lines-rule-2026-10-14.md`).
- `calibration/validate --candidate <file>` measures the rules of a second
  configuration that the replays' configuration lacks: each is
  re-evaluated on the rows it would judge, swept and advised like any
  rule, and a Candidate rules section gives every view's gate without it
  and with it, under both label sets, so a rule is measured before it
  ships.
- The five replays of 2026-10-13 replayed again with the current
  extractor, labelled by the split-or-extraction rule, with the hand
  labels joined by commit hash, and scored in
  `calibration/reports/gate-validation-2026-10-14-replays.md`.
- A package skipped because it does not load still counts for its
  neighbours: its test files are read syntactically, matching
  `pkg.Name` selectors and method names against the loaded packages they
  import, so an export only its tests call is no longer counted as
  untested (and `has_tests` follows); its non-test files join the
  cross-package duplication stream, so a block copied between it and a
  loaded package counts in that package's `dup_blocks_cross_pkg` and on
  the module row. The skipped package still has no row.
- `astimate dryrun --out <dir>` keeps the replay's `commits.jsonl`,
  `packages.jsonl` and `run.json` in a new or empty directory, so
  `calibration/replay/label` and `calibration/validate` read a dryrun with
  no manual step.
- `untested_exports` leaves out a method implementing `http.Flusher`,
  `http.Hijacker` or `http.Pusher`, which the HTTP server calls; a response
  writer's `Flush` is no longer counted as untested.
- `--coverage` (and `coverage: true` on the MCP tools) now also reports
  `untested_exports_covered`: `untested_exports` less the exports whose
  body a test ran, read from `go test -coverpkg=./... -coverprofile`
  profiles merged across the invocation's runs. For a package with
  untested exports, the tests of every module package whose test binary
  links it run too, a test-only directory included, so behaviour reached
  through an HTTP handler, a connect client, a pipeline golden or an
  interface counts as tested. An `untested_exports` rule may set
  `source: coverage` to judge that count when the run has it and the
  static one otherwise; in `check` the baseline's count is its untested
  exports less those head's run executed. The default rule stays static
  and `config init` shows the option commented out. `coverage_pct` is
  unchanged in meaning, now read from each package's own profile. On
  goKMS `internal/kms` the count drops from 45 to 8, `internal/auth` from
  6 to 0, and mark's `confluence` from 27 to 20
  (`calibration/notes/coverage-untested-2026-10-14.md`).
- Every package row carries a `role`: `main` for a composition root (a Go
  `package main`, or a TypeScript package a `bin` entry names or that
  exports nothing), `test-support` for a package only tests use,
  `generated-only`, or `library`. Reports carry it as a top-level `role`,
  `rank` as a ROLE column and a `role` field, and replay rows beside the
  metrics. A threshold may set `roles: [...]` to judge only rows of those
  roles, and `explain_metric` says which roles each rule judges. The
  default no longer judges `main` packages on `internal_imports`,
  `exported_symbols` or `untested_exports` (nor `test-support` ones on
  `untested_exports`), requires tests of a `main` package only past 300
  SLOC, and does not count a declarative cross-package block shared only by
  `main` packages as added. On the 94 failed commits of the five
  2026-10-13 replays this removes one finding, ztp's `cmd/broker`
  `internal_imports` 20 -> 21, and changes no verdict
  (`calibration/notes/package-roles-2026-10-14.md`).
- `astimate dryrun [<module-root>] --last N` (default 50) answers "how
  often would this have failed us?" before the gate is turned on: it
  replays the last N first-parent commits of `HEAD`, each against its
  parent in a temporary worktree under the resolved configuration, and
  prints the share of the commits it checked that the gate would have
  failed (the share `calibration/validate` reports for the same rows), and
  of those that touched a package, a per-rule table (commits failed,
  share, commits failed by that rule alone), the five most-failed packages
  and the commits it could not check. `--suggest <file>` writes the configuration with
  `severity: warn` on every rule whose share exceeds `--budget` (default
  10%); `--json` prints the replay's rows for the calibration tools;
  `--timeout` and `--parallel` bound the run. It exits 0 unless the replay
  itself fails. The replay engine moved from `calibration/replay` to
  `internal/replay`; the calibration command keeps its flags and data
  format.

- A Go package that does not load, such as one whose test imports a
  private module missing from the module cache, is skipped instead of
  failing the whole module: `check`, `rank` and `assess` measure and gate
  every other package. Each skipped package is logged on stderr with its
  reason and first error, listed by `check` in every format (a `skipped`
  array on the JSON module row, a `::warning` per package in `github`), and
  `check` exits 2 only when a skipped package is among those it judges.
  The import graph keeps the skipped package's edges, so its neighbours'
  `fan_in` is unchanged. `check_package` on a skipped package returns a
  result saying so, not an error.
- Duplicate blocks carry a kind, declarative, idiom, self-overlap or code,
  from the syntax around their tokens (Go from the syntax tree, TypeScript
  from node kinds), in the extractor details of package and cross-package
  blocks, and a new v1 metric `dup_blocks_code` counts the code blocks
  beside `dup_blocks`. It is reported, not gated: over the five 2026-10-13
  replays declarative and idiom blocks explain the duplication findings of
  only 10 of 94 failed commits, all labelled `allow`
  (`calibration/notes/dup-block-kinds-2026-10-14.md`).
- Hand labels for the five 2026-10-13 replays (go-starter, ztp, shhh,
  crush, ai-shell): `calibration/replay/labels/<name>-hand.yaml`, drafted
  by reading every failed commit's diff and 60 sampled passed ones, with
  each finding's cause and evidence in `<name>-hand-notes.md`. 23 of the
  94 failed commits are `block`; no sampled passed commit hid a missed
  regression. `calibration/reports/gate-validation-2026-10-14-hand.md`
  scores the gate with them first and the split-or-extraction labels
  second (`calibration/notes/hand-labels-2026-10-14.md`). The labels are
  drafts awaiting confirmation.
- `largest_file_sloc` and `exported_symbols` now allow growth of 5 in a
  package already over their ceiling (the TypeScript override: 8 and 5),
  hand-set from the field adjudication instead of the sweep's 3 and 0. crush's config package
  failed on +1, +3 and +5 exports and bug fixes of +6 to +24 lines failed in
  files over 600; the same changes now pass with a warning.

- The over-ceiling allowance (`over_max_delta`) of three more capacity rules
  is fitted from per-commit replays of five more repositories, and the
  TypeScript override now has one on each capacity rule. At the top level
  `internal_imports` takes 1, so a composition root that gains an import per
  wired component passes with a warning, `largest_file_sloc` moves from 5 to
  3 and `exported_symbols` stays 0, where no allowance beat it. The
  TypeScript override sets `sloc` 250, `tokens_est` 2,200,
  `largest_file_sloc` 5, `exported_symbols` 0, `internal_imports` 1 and
  `changed_func_cognitive_max` 5, the Go values scaled by the ratio of the
  ceilings because the TypeScript replay had no package over one; before, a
  TypeScript package over a ceiling failed on any growth.
- Density rules take a `floor`: a head value at or below it never fails
  the rule, however much it rose. The embedded default sets `max_nesting`
  4, `cognitive_p90` 10 and `duplication_pct` 10, at the top level and in
  the TypeScript override, so a package going from nesting 1 to 2 passes
  while 4 to 5 still fails. `explain_metric` reports the floor, and
  `calibration/validate` sweeps it.
- `duplication_pct` and `cognitive_p90` no longer fail a rise caused only
  by deleting code. Against a baseline, `duplication_pct` is judged only
  when the new `duplicated_lines` metric (the duplicated SLOC, reported
  by both extractors) rose, and `cognitive_p90` only when a function
  changed since the baseline is more complex than the baseline's p90.
  A baseline file written before `duplicated_lines` existed, or a check
  without a function-level diff, judges them as before. `explain_metric`
  says when a rise is judged.
- `check` now cites what the change added first. A `dup_blocks` or
  `duplication_pct` suggestion names the first duplicate block the change
  added, ahead of every legacy block, and says `added in this change`;
  `untested_exports` names the exports the change left untested first;
  `dup_blocks_cross_pkg` ranks its new blocks first against a git
  baseline. The GitHub annotation and the report's `details` follow the
  same order. Baselines record each package's duplicate blocks, as two
  hashes of each block's ends that survive an edit at one end, and its
  untested exports, under `package_rows` in a baseline file; a file
  written before keeps the previous order until it is rewritten. `assess`
  is unchanged.
- `changed_func_cognitive_max` now judges each changed function against
  its own baseline complexity. A function over `max` fails when it is new,
  crossed `max`, or rose by more than the rule's new `over_max_delta`
  (5 in the embedded default) from a value already over it; a legacy
  function that holds passes, and no longer hides a new function that
  fails. The finding names and locates that function, with its own base
  and head values. Gate replay rows record the changed functions over
  complexity 20 and each finding's `also` list, and `calibration/validate`
  sweeps the rule's `over_max_delta` on rows that carry them.
- TypeScript test files in a directory with no non-test source file, such
  as a top-level `test/` or `tests/`, now count as test references: the
  exports they name read as tested. Such a directory is still no package and
  reports no row. TypeScript `has_tests` is true when a test file anywhere
  in the module names an exported function or method of the package, as for
  Go; `test_files` and `test_funcs` stay the package's own.
- Go `untested_exports` leaves out three more shapes no test needs to name,
  and lists each separately in the details: functional-option constructors
  whose body only sets fields of the options value; exported methods of
  unexported types that implement an in-module interface a test holds; and,
  in a test-support package (used only by other packages' tests), the
  exports its own non-test code refers to, which now read as tested.
  `pflag.Value` and `pflag.SliceValue` join the closed interface list.
- Go `globals` also leaves out a compiled pattern (`regexp.MustCompile` or
  `MustCompilePOSIX` of constants) and a read-only table (a non-empty map,
  slice or array literal of constants, nested literals and compiled
  patterns) that the package never writes, never indexes on the left of an
  assignment and never passes to `append`, `delete`, `clear`, `copy`, a
  `maps`, `slices` or `sort` function or a function of the package; a table
  of function values or pointers still counts (SPEC.md 6.5 (d), (e)). The
  standard library loses 254 more of 1880 globals
  (`calibration/notes/globals-readonly-tables-2026-10-13.md`).
- Go `has_tests` is true when a test file anywhere in the module references
  an exported func or method of the package, the references
  `untested_exports` already reads, so a package exercised only by another
  package's tests, or a test-support package, no longer fails the
  requirement. `test_files` and `test_funcs` stay the package's own.
- Baselines match packages by their directory relative to the module root
  (`package_path`) instead of by import path, so a module-path change such
  as a `/vN` bump no longer makes every package read as new. A package
  whose non-test files were moved by git rename since the baseline is
  judged against a row seeded from the packages they came from (the source
  most files came from gives the metrics, every source its functions), so
  a package carved out of another is held by the ratchet rather than
  judged as new; the report records the sources as `baseline_from` and the
  text summary says `baseline seeded from <dirs> (rename)`. The import from
  a package to one carved out of it in the same change is not a rise in
  its `internal_imports` (nor in the carved-out package's `fan_in`).
  `baseline write` now also records each package's `package_path` under a
  new `package_rows` key; older files still load, with one warning. Replay
  rows record `baseline_from`.
- Per-rule severity: every threshold takes `severity`, `fail` (the default)
  or `warn`. A `warn` rule is evaluated as its kind requires, and what
  would be a violation is reported as a warning with the violation's fields
  and `"severity": "warn"` in every format (text, JSON, hook, GitHub
  `::warning`, `check_package`); it never changes `passed` or the exit
  code, and an exemption on it moves it to `exemptions` as it would the
  violation. The field is additive to the report schema. Replay rows keep
  the severity, and `calibration/validate` reports a `warn` rule as its own
  row, marked `(warn)`, with its precision and recall while leaving it out
  of the gate's verdict. The embedded default still ships every rule at
  `fail`.
- Capacity ratchet: a capacity rule takes `over_max_delta` (default 0), the
  most a package already at or over its `max` may grow in one change. Such
  growth is a warning whose `limit` reads `over max by N, allowed M` and
  whose suggestion says to split; more is a violation that says the same.
  Crossing the `max`, or a new package over it, still fails, and under 0
  every rise over the ceiling fails as before. The embedded default ships
  `over_max_delta` 100 for `sloc`, 1,000 for `tokens_est` and 5 for
  `largest_file_sloc`, fitted by a new `over_max_delta` sweep in
  `calibration/validate`, which now recommends retuning those rules to the
  ratchet instead of dropping them
  (`calibration/reports/gate-validation-2026-09-29.md`). `explain_metric`
  shows the field, and each capacity metric's explanation says what
  happens over the ceiling. The 2026-09-28 replays now validate only with
  `--config calibration/thresholds/astimate-thresholds-2026-09-28.yaml`,
  the rules they ran under.
- Split-or-extraction labels: `go run ./calibration/replay/label --rule
  split-extract` labels a replayed history from its rows alone, `block`
  when a later commit of the range split a package the commit took over a
  capacity `max` (or grew by 100 or more SLOC while over one), or, changing
  that package, removed the duplicate blocks the commit added. The labels
  of this repository and the three external corpora are committed as
  `calibration/replay/labels/<name>-split-extract.yaml`, beside the fix-up
  labels, and `calibration/validate --corpus name=<dir>:<labels>,<second
  labels>` reports every view under both label sets side by side
  (`calibration/reports/gate-validation-2026-09-29.md`,
  `calibration/notes/split-extract-labels-2026-09-29.md`).
- Rebuild fit: `go run ./calibration/rebuild/fit --runs <runs.jsonl>` fits
  the SPEC.md 7.2 parameters to the rebuild runner's measurements. It takes
  each package's median footprint tokens (input, cache writes and output)
  over its passing runs, fits the 7.2 form with its knee by nonlinear least
  squares (per-session overhead and scale as nuisance terms, budget held
  and profiled), and writes the base configuration with only the rebuild
  parameters and `config_version: rebuild-<date>-<agent>` changed, parsed
  and validated. Its report gives fit quality per output (tokens, turns,
  wall time, passing), residuals by tier, each 7.1 input's and each
  complexity candidate's correlation and coefficient with robust standard
  errors, names the inputs with no measurable contribution, and lists
  failed runs as censored rather than dropping them. Tested on synthetic
  runs with planted parameters before the real fit (see Changed).
- Gate validation: `go run ./calibration/validate` joins replay rows with
  commit labels and reports the gate's recall and false-failure rate against
  the 80% and 10% targets, each rule's precision and recall per corpus beside
  a size-only `sloc_delta` rule, threshold sweeps re-evaluated with the gate's
  own `Evaluate`, a keep, retune or drop recommendation per rule, and the
  missed `block` commits. On the four labeled corpora
  (`calibration/reports/gate-validation-2026-09-28.md`) the gate fails 87.8%
  of `block` and 51.5% of `allow` agent commits: recall met, false failures
  not.
- Hand-drafted block/allow labels for this repository's 222 replayed commits
  (`calibration/replay/labels/astimate.yaml`), awaiting review; 30 block,
  summary in `calibration/notes/astimate-labels-2026-09-28.md`.
- Calibration: replayed and rule-labeled 812 commits (538 agent-authored) from
  roborev, github-mcp-server and beads; shared labels format and loader in
  `calibration/replay/labels`.
- Commit replay: `go run ./calibration/replay` runs the gate at every commit
  of a repository's history against its parent and writes per-package and
  per-commit rows for gate validation.
- Self-check: this repository gates itself with its own action. The CI
  `selfcheck` job builds the action from source and runs `astimate check
  --format github` over the packages a pull request changes and with `--all`
  on pushes to `master`; `make check` runs `make selfcheck` after the tests.
- Exemptions: an `exemptions:` list in the config accepts one rule's
  violations on one package (`package`, `metric`, a required `reason` and an
  optional `expires` date). An exempted violation does not fail the gate but
  is still reported with its reason in every format: an `exempted:` text
  section, a JSON `exemptions` array (empty when a gate ran and none matched),
  the hook reason or stderr, and a GitHub `::notice`; summary lines count them.
  An expired exemption is ignored with a warning, and `check --all` warns about
  one that matched nothing.
- Rebuild runner: `go run ./calibration/rebuild run --live` runs each rebuild
  experiment against Claude Code in a fresh clone and writes one JSONL row per
  run with the measured tokens, turns, wall time and oracle outcome beside the
  package's metrics and pre-run estimate; it resumes interrupted runs and never
  spends requests without `--live`.
- Rebuild experiment definition: `calibration/rebuild` selects 34 corpus
  packages across every tier, tested and untested, stubs them to signatures
  deterministically and records each one's oracle, pin and pre-run estimate.
- `duplication.split_literal_runs` (default off) cuts duplicate blocks at
  literal-only runs of at least `min_tokens`, so literal tables joined by
  their declaration headers stop counting as duplication.

### Changed

- Every command logs at warn level by default, so routine notes such as a
  package left out for its build constraints no longer print beside the
  report. A new `--debug` flag, taken by every command including `serve`,
  restores the detail. `dryrun` still prints its progress lines.
- Agent sittings are the primary measure and the engineer time is
  demoted. The agent estimate is fitted on 85 measured rebuilds and its
  tiers carry measured success rates (one sitting rebuilt in 97% of runs,
  a few sittings in 78%); the engineer time is two uncalibrated textbook
  models whose agreement reads as precision it lacks. So every surface
  names the sittings first, with their evidence where a sentence has room
  (the lead, each package's rebuild sentence, the glossary), and shows the
  engineer time once: the counts line in text, a line under the lead in
  Markdown, the hero's sticker on the HTML page, and each package's
  detail, as an "Engineer time" fact. The Start here cards, the Every
  package tables (the TIME column is gone), the bubbles, the table cells
  and the tooltips carry sittings only. The HTML scatter's y axis is
  `agent_passes`, titled "agent sittings". `check`'s failing summary reads
  "rebuilds in one sitting", "2 sittings to rebuild" or "split before a
  rebuild" instead of days; its verdicts, findings and JSON are
  unchanged. `rank`'s DAYS column moves after TIER, and its default sort
  stays passes. The glossary's "Rebuild cost" splits into "Sittings" and
  "Engineer time"; `explain_metric` answers `sittings` and
  `engineer_time`, and still `rebuild_cost`, as Sittings. The MCP tool
  descriptions say which figure is measured. The JSON keeps every
  `human_*` field.

- Engineer time is a range. Until human rebuild time is measured no
  single figure is honest, so each package carries the lower and the
  higher of COCOMO 81 and COCOMO II under the rebuild profile, on the same
  size, as `human_days_low` and `human_days_high` beside `human_days`,
  which stays the configured model's figure (the JSON change is additive;
  `rank --json` and the MCP tools carry both). Over this repository the
  profile comes to 1.10 times COCOMO 81, so neither model is always the
  low end. The report prints the range in one unit, the high end's, once
  when both ends round the same: the counts line reads "10 to 11
  engineer-years for one engineer", and Start here, Every package, the
  Markdown, the HTML stickers, bubbles, tooltips and details, the `assess`
  line ("1.1 to 1.2 human days ... from COCOMO II, rebuild profile to
  COCOMO 81") and the `rank` DAYS column ("1.1-1.2") change with it. The
  scatter's days and the hotspot tie-break use the low end; `rank --sort
  days` still orders by `human_days`. `check` output is unchanged. The labels saying where the engineer
  time comes from name both models and their numbers, and the long one
  now describes the software-understanding inflation under the configured
  `rebuild.cocomo.unfm` in place of "inflated by up to half where exports
  have no tests", which was no longer true. An empty
  `rebuild.cocomo.unfm` is now a configuration error, like any unknown
  rating, where it was read as `completely_unfamiliar`.

- Engineer time inflates the code to rebuild by COCOMO II's software
  understanding, `1 + 0.02 × SU × UNFM`, in place of the guessed
  `1 + 0.5 × untested_ratio`, under both `cocomo-ii` and `basic-81`. SU
  (10 to 50) is rated per package from three sub-scores: structure from
  `cognitive_p90` and `dup_blocks`, application clarity from the share of
  untested exports, and self-descriptiveness from the new
  `documented_exports` metric, the exports with a doc comment (null for
  TypeScript, where the sub-score is nominal). UNFM is the new profile
  rating `rebuild.cocomo.unfm`, shipped `completely_unfamiliar` (1.0), so
  the inflation runs from 1.2 to 2.0 where it ran from 1.0 to 1.5; this
  repository's total rises 42%, from 1,703 to 2,424 days.
  `completely_familiar` removes it. SPEC.md 7.3 and
  `calibration/notes/cocomo-ii-rebuild-2026-10.md` give the cut points.

- The report leads with the measured agent estimate. The counts line reads
  "<n> packages, <k> need more than one sitting, <time> of one engineer's
  time", the HTML hero and the bubbles show the sittings with the engineer
  time beside them, and the lead says how many packages an agent needs more
  than one sitting for. "Start here", "Every package" (text, Markdown and
  the page's table), the MCP `report_module` `start_here` and the new
  `rank --sort hotspot` now order by `churn_commits` times `agent_passes`,
  then by `human_days`, instead of churn times `human_days`, so a package
  ranks by the figure fitted on 85 measured rebuilds, not by a textbook
  rule. The module JSON gains `counts.multi_sitting`, and a package's
  `hotspot` is now churn times agent passes.

- The engineer time is now COCOMO II's post-architecture form under a
  rebuild-from-tests profile instead of COCOMO 81 basic organic mode. A
  rebuild has known behaviour, an exact target and fixed boundaries, which
  COCOMO II rates (precedentedness very high, flexibility very low,
  architecture extra high, documentation very low) and COCOMO 81 cannot.
  The ratings sit by name under the new `rebuild.cocomo:` section, so a
  reader can argue with one; complexity is rated per package from
  `cognitive_p90`, and required reliability stays nominal, so adding tests
  never raises the figure.
  `rebuild.cocomo.model: basic-81` restores the old figure. The labels now
  name the model and print the configured numbers: the `assess` line ends
  "human days by COCOMO II, rebuild profile", the `rank` footer names it,
  and the JSON report adds `human_basis_short` beside `human_basis`. Simple
  packages come out lower and complex ones higher. The agent estimate,
  the tiers and the gate are unchanged. The reasoning is in
  `calibration/notes/cocomo-ii-rebuild-2026-10.md`.
- The engineer time is now labelled as COCOMO's textbook rule, not a
  measurement, wherever it appears: the glossary's "Rebuild cost" entry, a
  new `human_basis` field beside `human_time` in the JSON report, a footnote
  under the headline of the text and Markdown reports, the HTML page's
  tooltip and panel heading, the `assess` line and a footer under the `rank`
  table. The numbers are unchanged; `rebuild.cocomo_a` still scales them.
- `check --format github` writes at most ten `::error` and ten `::warning`
  annotations, the failing packages ranked as the module report ranks them
  (GitHub shows ten of each per step), and ends with a plain line saying
  how many findings and warnings it left out. When `GITHUB_STEP_SUMMARY`
  is set it appends the Markdown change block to that file, with the same
  count, so the job summary lists every finding. Within the cap and
  without the variable the output is unchanged.
- The default judges cross-package copies from 120 tokens instead of 80
  (`duplication.min_tokens_cross`), as `config_version:
  thresholds-2026-10-14b`. On the 2026-10-14 hand labels (15 `block`, 137
  `allow` agent commits) the gate's recall stays 86.7% and its false
  failures fall from 10.2% to 5.8%; the commits it stops failing were
  functional-options scaffolds, a block split by two struct fields and a
  block an import alias grew (`calibration/notes/cross-floor-2026-10-14.md`).
- The `dup_blocks` suggestion says it counts blocks of any kind and how
  many of them are code, and `explain_metric` says which kinds the
  default gate judges.

- The embedded default is `thresholds-2026-10-14`, retuned on 14
  hand-labelled `block` and 138 `allow` agent commits from five
  repositories (`calibration/reports/thresholds-2026-10-14.md`): duplicate
  blocks count from `duplication.min_tokens` 60 (was 40);
  `untested_exports`, `globals`, `cognitive_p90` and `internal_imports`
  ship at `severity: warn`; `over_max_delta` is 150 for `sloc` (was 100),
  2,000 for `tokens_est` (was 1,000) and 50 for `largest_file_sloc` (was
  5), with the TypeScript override's scaled to 375, 4,400 and 75. On those
  commits the gate fails 85.7% of `block` and 10.9% of `allow` commits,
  against 92.9% and 27.9% for the previous default at 40 tokens (85.7%
  and 26.8% at 60 tokens with the previous rules). A configuration without a `duplication`
  section now counts blocks from 60 tokens.
- `rebuild.calibrated` is also true for a `thresholds-<date>` version dated
  after the 2026-09-28 rebuild fit, so the retuned default keeps its
  calibrated estimate.
- `astimate dryrun --suggest` also sets `warn` on a rule whose every
  violation was merged into by a rule over the budget (the over-budget
  metric is in its `also` list). Before, the over-budget rule warned and
  the rule the finding was attributed to kept failing the commit, so the
  suggestion did not make the commit pass.
- `astimate dryrun` builds into a Go build cache of its own, a temporary
  directory removed when it ends, as the calibration replay does, so
  replaying history no longer grows the user's `GOCACHE`. The package that
  does this moved from `calibration/internal/gocache` to `internal/gocache`.
- The commit replay checks each commit out with no hooks run, and records
  a commit whose check selected a package that does not load as not
  loaded, with the skip's reason, as it records a module that does not
  load.
- `dup_blocks_cross_pkg` ratchets on the cross-package blocks a change
  added instead of on the module's count. Against a git baseline the rule
  judges the baseline's count plus the new blocks of at least
  `duplication.min_tokens_cross` normalized tokens (a new key, default 80)
  that no rename moved: a block whose every package came out of one rename,
  as a split leaves its halves sharing code, is moved, not copied. A short
  preamble repeated across packages, or a package split in two, no longer
  fails a module already over the ceiling, while a copied function still
  does; the finding names only the added blocks and the row keeps the full
  count. A check of named packages (the MCP `check_package` tool) judges the
  same blocks. Against a baseline file, which stores only the count, the
  count is judged as before and the suggestion says so. Each cross-package
  block in the extractor's details now carries its length in tokens. The
  fixture's `a.Checksum`/`b.Digest` copy grew from 65 to 90 tokens so it
  still clears the floor, which moves `a` and `b`'s goldens. The floor was
  swept on a sample of 20 replay commits
  (`calibration/notes/cross-blocks-added-2026-10-14.md`).
- The duplicate locations an extractor's `Details` lists, and so the one a
  duplication suggestion cites and `details.duplicates` leads with, are
  ordered by the block's length in tokens, longest first, ties in file
  order, instead of by first occurrence, so the suggestion names the copy
  worth extracting rather than the first short run of setters. Every
  occurrence is still listed.
- One finding per cause: the gate merges findings of the same change
  before severity and exemptions apply. A rule breaching both its
  `max_delta` and its `max` is one finding (`limit` `max_delta +6 and max
  40`); a `tokens_est` breach is folded into a `sloc` breach on the same
  package in the same direction, and a `duplication_pct` breach into a
  `dup_blocks` one, the kept finding's `limit` naming the other's values
  and limit (`max 1000; tokens_est 15000 -> 17500, max 16000`). Only two
  violations, or two breaches of `warn` rules, merge, so `passed` and the
  exit code never change; capacity warnings are left apart. Findings gain
  an optional `also` list of the merged rules, additive to the report
  schema; an exemption silences a merged finding only when every rule in
  it is exempted. `explain_metric` names the rule a merged finding is
  attributed to, and `calibration/validate` reads `also` from replay rows
  so a merged rule's precision is still measured.
- A density rule's `max` no longer judges a package already at or over it
  at baseline: such a package is judged by the rule's `max_delta` alone,
  so `cognitive_p90` 43 -> 45 passes under the default `max` 20 and
  `max_delta` +3, and 43 -> 47 fails on `max_delta +3` only. A new package
  over the `max`, or a change that crosses it, still fails on the `max`,
  and a rule with `max` alone (`changed_func_cognitive_max`) still fails
  any rise over it. `explain_metric` says so on each density rule with a
  `max`, and `calibration/validate` sweeps pick the rule up through
  `gate.Evaluate`, so its recheck of older replays reports the legacy
  rises they recorded as disagreements.
- `globals` counts mutable state only (SPEC.md 6.5). A Go package-level var
  spec that the package never assigns, increments or takes the address of
  is left out when it is a sentinel error (`errors.New` or `fmt.Errorf`
  with constant arguments), carries `//go:embed`, or is build information
  (a boolean, numeric or string var with a constant initializer or none).
  Maps, slices, `sync` values, pointers, `regexp.MustCompile` and `flag`
  variables and anything written still count. The standard library loses
  422 of 2304 globals and this repository all 23
  (`calibration/notes/globals-mutable-state-2026-09-29.md`); stdlib
  `errors` now reports 1. The suggestion says "mutable package-level
  variable". TypeScript is unchanged; SPEC.md 13.1 records why a module-level
  `const` object is not counted.
- `untested_exports` counts a reference from a test file of any package of
  the module, not only the package's own, for Go and TypeScript alike, so
  constructors exercised by other packages' tests and test-support packages
  such as `metricstest` read as tested. In Go, a method whose receiver
  implements an interface on the closed list of SPEC.md 6.4 (`error`,
  `fmt.Stringer`, `io.Writer`, the `errors` package's `Unwrap`, `Is` and
  `As` conventions and the rest) is covered, since the runtime or the
  standard library calls it; the same name with another signature still
  counts. The Go test references are indexed once per module load. On the
  standard library the count falls from 3,427 to 2,247 and on this
  repository from 24 to 12; extraction time is unchanged within noise
  (`calibration/notes/untested-exports-refinements-2026-09-29.md`).
- calibration: the rebuild stub now removes initialization code that calls the
  stubbed package, so every test binary starts, and select checks that it does
  (rebuild.yaml regenerated, 33 experiments, the 26 recorded stub hashes
  unchanged); new whole-tree rebuild experiments (`select --unit tree`,
  rebuild-trees.yaml) with unit-aware runs and fit.

- The embedded default's rebuild parameters are calibrated: `config_version`
  is `rebuild-2026-09-28-claude-code-opus`, fitted by
  `calibration/rebuild/fit` from 85 Claude Code (Opus) rebuilds of 31
  packages, three runs each
  (`calibration/reports/rebuild-2026-09-28-claude-code-opus.md`).
  `context_budget` 25,000 → 37,500 (the data preferred it beyond noise),
  `tokens_per_untested_export` 800 → 300 (fitted 304 ± 100),
  `tokens_per_export` 40 → 0 and `tokens_per_hidden_state` 400 → 0 (both
  fitted negative, so clamped), `superlinear_exponent` 1.3 → 1 (past the
  knee the measured cost grows no faster than the estimate, in the package
  runs and in 14 whole-tree runs alike); the form explains 92.5% of the
  variance in measured tokens, and one estimate token corresponds to about
  7.6 tokens of session footprint. A first fit from one run per package
  (25,000 / 120 / 2.79) is superseded. The gate thresholds are unchanged
  (`thresholds-2026-09-28`'s). Estimate lines read `estimate from
  calibrated parameters`, JSON reports `rebuild.calibrated: true`, and
  `check`'s text summary lines carry agent passes, tier and the change
  from the baseline again. Passes grow linearly past the knee: a package
  at 50,000 rebuild tokens scores 1.3 passes (2.5 under the placeholders).
  `calibration/validate` accepts replays recorded under
  `thresholds-<date>` when the configuration's rules equal that committed
  candidate's.
- The Go and TypeScript extractors are split into internal subpackages, each
  under every default ceiling; the repository carries a root `astimate.yaml`
  (the embedded default plus one recorded exemption for the Go fingerprint
  dispatch table) that the Stop hook, `make selfcheck` and CI all use. The
  `gate` CI job pinned to `v0.1.0` is removed: that release rejects the new
  config keys, and the `selfcheck` job covers pull requests.
- Every package of the repository is now under every default ceiling:
  `internal/engine` gates rows through `internal/engine/internal/judge`,
  `internal/metrics` reads one field table for names, values, deltas,
  validation and explanations, `cmd/astimate` shares its flag and output
  handling, and `calibration/fit`, `calibration/rebuild` and
  `calibration/collect` are split into internal packages. Output, flags and
  reports are unchanged.

- `check` summary lines count exempted violations after the warnings:
  `<pkg>: N violations, M warnings, K exempted`.
- SPEC: `fan_in` is documented as unproven and outside the rebuild formula, the
  capacity ceilings no longer claim to ensure one pass, section 4 citations were
  checked against their abstracts, and milestone M8 was added.

- `check` text, and `check_package`'s text, no longer show agent passes, tier
  or the change in passes from the baseline while the rebuild estimate is
  uncalibrated (a `config_version` not starting `rebuild-`): the summary line
  reads `<pkg>: N violations, M warnings`. The estimate rewards duplication
  and penalizes tests, so beside a gate finding it told the agent the
  opposite. A calibrated config brings the line back unchanged. The hook
  reason and GitHub annotations never carried them; `assess`, `rank` and
  every JSON output are unchanged.
- TypeScript packages are judged by their own thresholds: the default
  configuration ships a `languages.typescript` override fitted on 1,209
  packages from 20 TypeScript repositories (`calibration/corpus-typescript.yaml`,
  data in `calibration/data/2026-09-28-typescript/`, report in
  `calibration/reports/thresholds-2026-09-28-typescript.md`). It raises
  `sloc` to 2,500, `tokens_est` to 35,000, `largest_file_sloc` to 900,
  `max_nesting` to 7, `cognitive_p90` to 25 (+4), `duplication_pct` to 45
  (+7) and `changed_func_cognitive_max` to 55, and lowers `exported_symbols`
  to 55 and `internal_imports` to 8; the zero-tolerance ratchets,
  `has_tests` and the rebuild parameters are the top level's. Reports on
  TypeScript show `config_version` `thresholds-2026-09-28+typescript`; Go
  is unchanged. `config init` writes the override.
- `calibration/collect` collects a TypeScript corpus (`--corpus` with a
  file whose `language` is `typescript`, each repository naming its module
  roots, test and fixture directories left out, every row tagged
  `language`), and `calibration/fit --language <id>` writes a
  `languages.<id>` override block instead of a whole configuration, with
  `--base-data` setting the base's percentiles beside it in the report.
- The default configuration gates cross-package duplication:
  `dup_blocks_cross_pkg` with `max_delta: 0` and `max: 450`, evaluated on
  the module row only, so a change that copies code between packages fails
  `check`. The max is the p90 of the module rows of the 36 cloned corpus
  modules (`calibration/data/2026-09-28-modules/`) and judges a module with
  no baseline. `config_version` stays `thresholds-2026-09-28`; its numbers
  now include this rule, and `calibration/reports/thresholds-2026-09-28.md`
  carries it. A baseline file written before the module row existed skips
  the rule with a note until `astimate baseline write` is rerun.
- `calibration/collect` records each cloned module's module row, with what
  its module pass cost, in `modules.jsonl` (`--modules-only` collects just
  those), and `calibration/fit --modules` fits module-wide rules from them
  and reports their distribution.
  `calibration/notes/cross-package-duplication-2026-09-28.md` measures the
  pass: at most 0.5 s and 214 MiB on the corpus, and 0.8 s and 317 MiB on
  the standard library, for which the metric is still null.

- The text report's estimate line now reads `(estimate from uncalibrated
  parameters)` instead of `(estimate, uncalibrated)`, so it no longer reads
  as a statement about the gate thresholds, which are calibrated. The JSON
  field `rebuild.calibrated` is unchanged.
- `changed_func_cognitive_max` is calibrated: its default `max` moves from
  the placeholder 30 to 50, the 99th percentile (51) of per-function
  cognitive complexity over the 80,280 functions of the reference corpus,
  rounded per SPEC.md 11.1. `config_version` is `thresholds-2026-09-28`;
  every other limit is unchanged. The collector now records each package's
  per-function cognitive counts (`func_cognitive`), the data is in
  `calibration/data/2026-09-28-corpus/` and the evidence in
  `calibration/reports/thresholds-2026-09-28.md`.

### Fixed

- `astimate report` finishes the move to sittings where there is no change
  history. "Start here" and "Every package" order by agent passes, then
  engineer time, then path, and say "most sittings first" (the lead says
  "ranked by sittings alone"). The words "rebuilds in one sitting", "N
  sittings to rebuild" and "split before a rebuild" follow the configured
  `rebuild.tiers` bounds instead of a fixed 1 and 3, so a changed config
  cannot put them out of step with the tier.
- `astimate report` words its orders truthfully. Without change history the
  "Start here" subtitle says "no change history: costliest to rebuild
  first" and the "Every package" heading says "costliest to rebuild first";
  with history that heading says "by changes × sittings". A package's
  Rebuild fact reads "1 human day", never "1 human days" or a figure under
  one day.
- Sentences of `astimate report` read against eleven repositories. "Start
  here" lists only split and watch packages (a fine package said "Nothing.
  Leave it alone." there), by rebuild time with a sentence saying so when
  there is no git history. The headline says a package "needs a split"
  rather than "too big to rebuild in one go", which most split packages
  are not, and names a package only when the first to start with is one.
  The longest repeat names two places of the same block; it could pair two
  different blocks. The text change report prints a finding's why and what
  to do together, so a global's or an untested export's name is kept; the
  size finding no longer says "already past" of a change that crossed the
  limit. Smaller fixes: "1 time", "did not change", "Half or more" copied,
  a watch package that is only copied code is told to extract it, the
  change lead counts a package without tests as untested, and the
  Markdown table's column is Time.
- `calibration/fit --language` keeps the base rule's `floor`, `severity`
  and a density rule's `over_max_delta` in a refitted override rule; it
  dropped them.
- A package carved out of code that moved from a file which keeps its
  path is seeded from that file's package: rename detection also lists
  copies (`git diff -M -C`), and a copy from a file the same diff modified
  counts as a rename. Before, such a package read "no baseline" on every
  rule.
- A legacy cross-package block that a carved-out package took with it from
  its source, its copy in a third package staying put, is matched with its
  baseline self instead of counting as added: the matching reads each
  package a rename joined as its whole group.
- A seeded row records duplicate blocks and untested exports only when
  every source does, so a source without them no longer makes the other
  source's blocks read as added.
- In the change that carves a package out of another, a package that
  imported the source at the baseline and imports both the source and the
  carved-out package at head no longer rises in `internal_imports`; one
  that imports the carved-out package for the first time still does.
- The commit rows of a replay and of `dryrun --json` name the failed
  package in `packages_failed`: the replay scrubbed a package's error to
  plain text before the row read its package, so `package` was always
  empty. A commit recorded as not loaded because a package it changed does
  not load now lists that package there too.
- A finding for growth past a ceiling on the module row (a module-wide
  metric) now says to extract the shared code instead of to split the
  package, since the module row has no package to split.
- `explain_metric` writes a negative `max_delta` as `-1`, not `+-1`.
- The `calibration/validate` report tells rules on one metric apart: the two
  `has_tests` rules, for library and for main packages, are no longer two
  identical rows but carry their roles and `when` condition in the row name.
- The hook and reading-violations docs show the current finding text, with
  the merged `dup_blocks` line and `added in this change`, and say what both
  mark. The repository's `astimate.yaml` drops a stale `dup_blocks`
  exemption that the self-check warned about on every run.
- `check --base` no longer runs the repository's git hooks: every git
  command astimate starts gets `-c core.hooksPath=/dev/null`, so a
  `post-checkout` hook that needs `git-lfs`, absent on the host, no longer
  makes the baseline worktree fail and the check exit 2.
- `check --all`, and any check that selects every package, no longer exits
  2 because one package does not load: a skipped package is selected only
  when its own files changed since the baseline, and is otherwise listed
  without `selected`. The skipped-package line on stderr is logged once per
  check, not once for head and again for the baseline.
- Go: a package whose `//go:embed` pattern matches no file, such as a
  gitignored frontend build absent from the temporary merge-base checkout,
  no longer fails the load, so `check` no longer exits 2 for a module that
  embeds a build directory. The package is extracted as usual and one
  warning per package and pattern is written to stderr; any other load
  error still fails.

- calibration: rebuild runs, `select`'s verification, replays and corpus
  collection no longer build into the shared Go build cache, which every
  rebuild run grew by a whole corpus module and its tests until it filled
  the disk (242 GB). Each points `GOCACHE` and `GOTMPDIR` into its own
  temporary directory, removed with the run's clone (kept with it under
  `--keep`), the collected module's clone or the replay. `make clean-runs`
  removes the `astimate-rebuild-*`, `astimate-replay-*` and
  `astimate-collect-*` directories interrupted or kept runs left in the
  temporary directory, and prints each one. The collector's clones are now
  `astimate-collect-*` (were `astimate-corpus-*`) and `select`'s default
  scratch directory `astimate-rebuild-select-*`.

## [0.1.0] - 2026-09-27

First release: Go and TypeScript extractors, the rebuild estimate, the
quality gate with calibrated defaults, `assess`, `rank`, `baseline`,
`check` (text, JSON, hook and GitHub formats, `--staged`), the MCP server,
the GitHub Action, the Claude Code Stop hook and pre-commit integrations,
under the MIT license.

### Changed

- The embedded default thresholds are calibrated: `config_version` is
  `thresholds-2026-09-27`, fitted by `calibration/fit` (SPEC.md 11.1) from
  2,347 packages of the standard library and 36 open-source Go modules
  (`calibration/data/2026-09-27-corpus/`). Capacity ceilings tighten:
  `tokens_est` 30000 to 16000, `sloc` 6000 to 1000, `largest_file_sloc` 800
  to 600, `internal_imports` 12 to 10; `exported_symbols` stays 60. Density
  limits: `duplication_pct` max 5 to 40 and max_delta 0.5 to 6,
  `cognitive_p90` max 25 to 20; `max_nesting` stays at max 5. The
  zero-tolerance ratchets (`dup_blocks`, `untested_exports`, `globals`,
  `init_funcs`, `max_nesting`) keep max_delta 0 by policy, and
  `changed_func_cognitive_max` keeps 30. A package already over a new
  ceiling gets a warning, not a violation, until a change grows it. The
  evidence is in `calibration/reports/thresholds-2026-09-27.md`; the previous
  placeholders are kept as `configs/uncalibrated.yaml`. The rebuild
  parameters are unchanged and still uncalibrated.

[Unreleased]: https://github.com/rfizzle/astimate/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/rfizzle/astimate/releases/tag/v0.1.0
