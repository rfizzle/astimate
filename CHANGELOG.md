# Changelog

All notable changes to this project are documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project will adhere to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from its first release.

Changes before this file was created were not recorded here; see the git
history.

## [Unreleased]

### Added

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
