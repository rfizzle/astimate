# Generated files out of the size metrics: x/tools spot check

Date: 2026-09-27. Machine: darwin/arm64.
Toolchain: `go version go1.27.1 darwin/arm64` (Homebrew).
Target: `golang.org/x/tools` v0.50.0 from the module cache
(`$(go env GOMODCACHE)/golang.org/x/tools@v0.50.0`), default configuration,
`est` tokenizer at 3.2 bytes per token.

## Question

`rank` over x/tools put `internal/stdlib` second in the module, at 19.9
agent passes and 1152 human days, because two of its four non-test files are
generated tables. A rebuild reruns the generator; it does not write the
tables. SPEC.md 6.5 now keeps a non-test file with a
`// Code generated ... DO NOT EDIT.` header out of every size and structure
metric (it still counts in `files`, `generated_files` and the import
metrics) and reports its volume as `tokens_est_generated`. Does the package
drop to one pass, and what else moves?

## Runs

Two binaries of `cmd/astimate`: "before" built from master at `e3e6f96`,
"after" from the change.

```
astimate assess $(go env GOMODCACHE)/golang.org/x/tools@v0.50.0/internal/stdlib
astimate rank   $(go env GOMODCACHE)/golang.org/x/tools@v0.50.0
```

## internal/stdlib

Non-test files: `deps.go` (28,844 bytes) and `manifest.go` (739,926 bytes)
carry the generated header; `import.go` (2,840) and `stdlib.go` (2,877) are
hand-written. `generate.go` is excluded by its `ignore` build tag.

| Metric | Before | After |
| --- | --- | --- |
| `files` | 4 | 4 |
| `generated_files` | 2 | 2 |
| `sloc` | 19,271 | 127 |
| `largest_file_sloc` | 18,608 | 64 |
| `tokens_est` | 242,027 | 1,786 |
| `tokens_est_with_tests` | 242,537 | 2,296 |
| `tokens_est_generated` | (absent) | 240,240 |
| `exported_symbols` | 20 | 18 |
| `globals` | 4 | 1 |
| `duplication_pct` | 0.1 | 15.0 |
| `rebuild_tokens` | 249,095 | 7,548 |
| `agent_passes` | 19.9 (PARTITION) | 0.3 (ONE_PASS) |
| `human_days` | 1,151.8 | 5.0 |

Arithmetic: `tokens_est` = (2,840 + 2,877) / 3.2 = 1,786.6, truncated to
1,786; `tokens_est_generated` = (28,844 + 739,926) / 3.2 = 240,240.6,
truncated to 240,240. Their sum, 242,026, is one below the old 242,027
because each sum is truncated on its own. The function counts and
cognitive metrics did not move: the generated files hold only tables, no
functions.

`duplication_pct` rose from 0.1 to 15.0 because the denominator is now the
hand-written `sloc` (127) instead of 19,271 lines that were mostly
generated; the covered lines were already hand-written only, since the
duplication stream has always skipped generated files. This is the
consistency the change intended: the percentage describes the code a
rebuild would write. The 15% is the two blocks in `import.go`.

## Module-wide effect

In `rank`, `internal/stdlib` moves from second (19.9 passes) to 93rd of 215
rows (0.3). Six packages changed, every one holding a generated file:

| Package | Passes before | After | Days before | After | Tokens before | After | Dup% before | After |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `internal/stdlib` | 19.9 | 0.3 | 1,151.8 | 5.0 | 242,027 | 1,786 | 0.1 | 15.0 |
| `cmd/splitdwarf/internal/macho` | 6.0 | 5.6 | 50.3 | 45.5 | 17,760 | 16,391 | 44.4 | 46.7 |
| `internal/typesinternal` | 3.0 | 2.7 | 48.2 | 39.4 | 27,143 | 24,748 | 19.5 | 22.6 |
| `internal/pkgbits` | 3.0 | 2.9 | 36.1 | 31.7 | 11,556 | 10,622 | 19.5 | 21.5 |
| `internal/typeparams` | 0.8 | 0.6 | 29.7 | 16.3 | 7,976 | 5,507 | 4.8 | 8.1 |
| `internal/excfg` | 0.5 | 0.4 | 16.3 | 15.0 | 4,550 | 4,319 | 5.5 | 5.8 |

`internal/pkgbits` drops from PARTITION to FEW_PASSES; the other tiers
hold.

## go/ast/inspector

The backlog item named `go/ast/inspector` (46% duplication) as the same
case for the duplication denominator. It is not: it has no file with a
generated header (`generated_files` 0), so its numbers are unchanged
(1.0 passes, 13,484 tokens, 46.1%). Its `typeof.go` tables are
hand-maintained source, which the literal-only rule of 6.3 governs, not
this one.
