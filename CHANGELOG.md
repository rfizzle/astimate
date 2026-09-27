# Changelog

All notable changes to this project are documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project will adhere to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
from its first release.

Changes before this file was created were not recorded here; see the git
history.

## [Unreleased]

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
