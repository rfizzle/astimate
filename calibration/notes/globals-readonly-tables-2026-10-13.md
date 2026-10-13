# `globals` leaves out compiled regexps and read-only tables: what it removes

Date: 2026-10-13. Machine: linux/arm64.
Toolchain: Go 1.27.1.
Targets: the standard library (`std`, 381 packages loaded, those outside
`vendor/` measured) and this repository's module (58 packages) at the
change. The cloned corpus modules were not measured (no network access, as
in `globals-mutable-state-2026-09-29.md`); measuring them is a follow-up.

## Question

SPEC.md 6.5 counted `regexp.MustCompile` results and composite-literal
tables as mutable state, since a map or slice can be mutated through its
reference. In field findings on seven repositories this produced false
findings and no true one: compiled patterns (`markerRe`, `nameRE`,
`binaryNamePattern`) and constant tables (`knownSessionlessMCPs`,
`removedPaths`), none written anywhere in the package. SPEC.md 6.5 (d) and
(e) now leave out, when no name of the spec is written, a compiled pattern
and a read-only table of constants. How many names does each remove?

## Method

A throwaway test in `internal/lang/golang/internal/inspect`, not committed
and with temporary switches in the classification, ran `Complexity` (for the
writes) and `Globals` over every package with each rule off and on, and
summed the counted names.

## Names removed

| Target | Before | Without (d) | Without (e) | After (d) and (e) | Removed | Packages changed |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| std | 1880 | 1630 | 1876 | 1626 | 254 (13.5%) | 70 |
| this repository | 0 | 0 | 0 | 0 | 0 | 0 |

"Without (d)" keeps only (e) on; "without (e)" keeps only (d) on. Compiled
patterns are rare in the standard library (4 names); read-only tables are
nearly all of the effect (250 names, with (d) removing 4 more). This
repository had no counted global after the earlier exclusions, so nothing
changes. The earlier note's 1882 differs from the 1880 here by the
toolchain and package set.

## Consequences

- No refit: `globals` is `max_delta: 0` with no `max` (SPEC.md 8.2); the
  rebuild weight `tokens_per_hidden_state` is 0.
- Conservative where unsure: an empty literal, a table of function values,
  pointers or other variables, and any table passed to a function of the
  package or to a mutator still count. A table exported and mutated by an
  importer is excluded, the same blind spot as the other exclusions.
