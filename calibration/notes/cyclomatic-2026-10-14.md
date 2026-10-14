# Cyclomatic complexity against gocyclo on this repository

Date: 2026-10-14. Code: the commit that added `cyclomatic_total`,
`cyclomatic_p90` and `details.functions` (SPEC.md 6.5), 733c122. Reference:
`gocyclo` v0.6.0 (`github.com/fzipp/gocyclo`), installed under a scratch
`GOBIN` outside the module, never into `go.mod`.

## Question

The per-function count is meant to be McCabe's as gocyclo computes it: 1,
plus 1 for each `if`, `for` and `range`, each `case` of a `switch`, type
switch or `select` other than `default`, and each `&&` and `||`, func
literals counted in their parent. Does astimate give gocyclo's number on
every function of this repository, and how far does it track cognitive
complexity?

## Method

1. `go build -o astimate ./cmd/astimate`, then
   `./astimate check . --all --base master --format json`: every package's
   report, each with `details.functions` (name, file, line, cognitive,
   cyclomatic). A function's key is its package directory joined with its
   file, and its line.
2. `gocyclo .` from the repository root: one line per function,
   `<complexity> <package> <function> <file:line:column>`, test files and
   `testdata` included.
3. Join on file and line (both tools report the line of the `func`
   keyword). gocyclo's extra lines, test files, `testdata` and one function
   in `calibration/rebuild/internal/runner/proc_other.go` (`//go:build
   !unix`, not loaded on this platform), are not astimate functions and are
   left out.
4. Spearman's rank correlation between cognitive and cyclomatic complexity
   over the joined functions, ties given their average rank.

## Result

- Functions compared: 1,659, every function astimate lists for the
  repository's packages; none is missing from gocyclo's output.
- Mismatches: 0. The per-function count equals gocyclo's on all of them.
- Spearman correlation between the two counts: 0.961.

The ten most complex functions by cyclomatic complexity:

| Function | File:line | astimate | gocyclo | Cognitive |
| --- | --- | --- | --- | --- |
| `inspect.fpCode` | `internal/lang/golang/internal/inspect/fingerprint.go:105` | 49 | 49 | 1 |
| `gate.Threshold.Validate` | `internal/gate/threshold.go:127` | 34 | 34 | 42 |
| `tests.untested` | `internal/lang/golang/internal/tests/untested.go:136` | 32 | 32 | 42 |
| `definition.Experiment.validateTree` | `calibration/rebuild/internal/definition/tree.go:171` | 29 | 29 | 23 |
| `engine.Check` | `internal/engine/check.go:217` | 29 | 29 | 41 |
| `resolve.stripJSONC` | `internal/lang/typescript/internal/resolve/tsconfig.go:390` | 27 | 27 | 33 |
| `score.MetricSuggestion` | `internal/score/suggest.go:116` | 23 | 23 | 3 |
| `tests.genericHelpers` | `internal/lang/golang/internal/tests/generics.go:67` | 22 | 22 | 41 |
| `typescript.assemble` | `internal/lang/typescript/assemble.go:27` | 22 | 22 | 29 |
| `typescript.module.link` | `internal/lang/typescript/module.go:148` | 22 | 22 | 48 |

The ten most complex by cognitive complexity:

| Function | File:line | astimate | gocyclo | Cognitive |
| --- | --- | --- | --- | --- |
| `typescript.module.link` | `internal/lang/typescript/module.go:148` | 22 | 22 | 48 |
| `gate.Threshold.Validate` | `internal/gate/threshold.go:127` | 34 | 34 | 42 |
| `tests.untested` | `internal/lang/golang/internal/tests/untested.go:136` | 32 | 32 | 42 |
| `engine.Check` | `internal/engine/check.go:217` | 29 | 29 | 41 |
| `tests.genericHelpers` | `internal/lang/golang/internal/tests/generics.go:67` | 22 | 22 | 41 |
| `metricstest.TestExtractor` | `internal/metrics/metricstest/metricstest.go:106` | 22 | 22 | 40 |
| `resolve.stripJSONC` | `internal/lang/typescript/internal/resolve/tsconfig.go:390` | 27 | 27 | 33 |
| `resolve.loadCompilerOptions` | `internal/lang/typescript/internal/resolve/tsconfig.go:141` | 19 | 19 | 32 |
| `duptok.suffixArray` | `internal/lang/duptok/find.go:88` | 18 | 18 | 31 |
| `dup.dupTokenizer.scan` | `internal/lang/golang/internal/dup/duplication.go:211` | 19 | 19 | 31 |

## Reading

The two counts rank functions almost alike (0.96), so on this repository
cyclomatic complexity adds little ranking information over cognitive
complexity. Where they part, they part the way their definitions say. A
flat `switch` over many cases is cyclomatically large and cognitively
small: `fpCode` (49 against 1) and `MetricSuggestion` (23 against 3) are
lookup tables an agent reads at a glance. Nested branching weighs more in
cognitive complexity: `module.link` is tied eighth by cyclomatic count (22)
and the most complex function by cognitive count (48). That is why
cognitive complexity stays the gated measure (`cognitive_p90`,
`changed_func_cognitive_max`) and cyclomatic complexity is reported only;
a gate rule on it would need its own measurement against the hand labels
(SPEC.md 11.4).

## Reproduce

```sh
GOBIN=<scratch>/bin go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0
go build -o astimate ./cmd/astimate
./astimate check . --all --base master --format json > self.json
<scratch>/bin/gocyclo . > gocyclo.txt
```

then join `self.json`'s `details.functions` with `gocyclo.txt` on
module-relative file and line as above.
