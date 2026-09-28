# Rebuild parameters rebuild-trees-2026-09-28-claude-code-opus

Fitted by `calibration/rebuild/fit` (SPEC.md 7.2 and 11.2) from `calibration/data/rebuild-trees-2026-09-28-claude-code-opus/runs.jsonl`. Candidate configuration: `/private/tmp/claude-502/-Users-copierson-Projects-astimate/a4587da2-a070-447b-9477-6211eb476f75/scratchpad/trees-candidate.yaml`, the base `rebuild-2026-09-28-claude-code-opus` with only the rebuild parameters and `config_version` changed.

## Summary

14 runs of 14 packages by claude-code-opus (model claude-opus-5-5): 12 packages have a passing run,
2 runs failed and are censored, 0 rows were excluded. The 7.2 form explains 98.1% of the variance in
measured tokens (R², 12 packages). A session spends about 14299 tokens before it reads the package,
and each token of the estimate costs about 3.714 measured tokens. Changed:
tokens_per_untested_export 120 → 110, tokens_per_hidden_state 0 → 940, superlinear_exponent 2.79
→ 1. tokens_per_export fitted -52.7, clamped to 0: Validate requires >= 0, and the form cannot say
that this input lowers the cost. superlinear_exponent fitted 0.9616, clamped to 1: Validate requires
>= 1; past the knee the measured cost grows no faster than r. Fitted freely, one test token costs
0.7876 volume tokens (95% interval -10.53 to 12.1; 7.2 fixes it at 1): tests carry no measurable
cost, and the data cannot tell cost from help. No measurable contribution, to tokens or to passing:
volume, spec_tokens, exported_symbols, untested_exports, globals+init_funcs, cognitive_total,
cognitive_p90, max_nesting, func_count. The fit uses passing runs only. Of the 2 failed runs, 1 had
already spent more than the fit predicts a pass of their package costs, so the fit underestimates at
least those packages. 2 packages never passed and are not in the fit at all; if they are the hard
ones, every fitted cost is biased low. The residuals are smallest at a context budget of 6250, but
not significantly below those at 25000: the data cannot locate the knee, so the budget stays.

Before this configuration ships, check it against the acceptance invariants (SPEC.md 7.5):

```sh
ASTIMATE_CONFIG=/private/tmp/claude-502/-Users-copierson-Projects-astimate/a4587da2-a070-447b-9477-6211eb476f75/scratchpad/trees-candidate.yaml go test ./internal/invariants
```

## Parameters

| Parameter | Base | Fitted | SE | Emitted |
| --- | ---: | ---: | ---: | ---: |
| `context_budget` | 25000 | held | - | 25000 |
| `tokens_per_export` | 0 | -52.7 | 362.3 | 0 |
| `tokens_per_untested_export` | 120 | 108.8 | 422.4 | 110 |
| `tokens_per_hidden_state` | 0 | 941.6 | 4347 | 940 |
| `superlinear_exponent` | 2.79 | 0.9616 | 0.8854 | 1 |
| overhead (fit only) | - | 14299 | 12136 | - |
| scale (fit only) | - | 3.714 | 3.07 | - |

The overhead (tokens a session spends whatever the package) and the scale (measured tokens per token of the estimate) belong to the fit, not the configuration: the estimate counts what a rebuild must hold in context, and the scale converts it to what the agent spent. The per-item costs are rounded to two significant figures and the exponent to two decimals.

- `tokens_per_export`: fitted -52.7, clamped to 0: Validate requires >= 0, and the form cannot say that this input lowers the cost.
- `superlinear_exponent`: fitted 0.9616, clamped to 1: Validate requires >= 1; past the knee the measured cost grows no faster than r.

## Data

| | |
| --- | --- |
| Agent | claude-code-opus |
| Model asked for | claude-opus-5-5 |
| Models reported | claude-opus-5-5 |
| Unit | tree: each row rebuilds a directory tree, and its metrics are the members' aggregate |
| Rows | 14 |
| Packages with a verdict | 14 |
| Packages with a passing run | 12 |
| Censored runs (failed) | 2 |
| Excluded rows | 0 |
| Passing runs that hit the turn cap | 0 |

Measured tokens are the `footprint` measure, input + cache writes + output, summed over the session. The footprint counts every token that entered the agent's context once: what it read, what tools returned and what it wrote. That is the quantity 7.2's rebuild_tokens models, what a rebuild must hold in context. Cache reads are left out of it because each turn re-reads the whole context, so they grow with turns times context size and count the same token many times; `--measure total` regresses on everything the session consumed instead. A package's measurement is the median of its passing runs; the spread is in the package table.

## Fit quality per output

| Output | N | Against | R² | Note |
| --- | ---: | --- | ---: | --- |
| Measured tokens | 12 | the 7.2 form | 0.9814 | 4 packages past the knee (r > 1), 2 at r ≥ 1.1 |
| Measured tokens | 12 | free linear fit, on tokens with the knee undone | 0.9822 | adjusted R² 0.9674 |
| Turns (median of passes) | 12 | fitted rebuild_tokens | 0.7904 | 16.63 + 0.6717 per 1,000 rebuild tokens |
| Agent wall seconds (median of passes) | 12 | fitted rebuild_tokens | 0.9725 | 36.01 + 9.858 per 1,000 rebuild tokens |
| Passing (per run) | 14 | emitted agent_passes | - | point-biserial r = -0.2773 |

### Residuals by tier

Tiers under the fitted parameters; residual = measured − predicted.

| Tier | Packages | Mean residual | Mean abs % | Max abs % |
| --- | ---: | ---: | ---: | ---: |
| ONE_PASS | 8 | 1687 | 21.05 | 72.22 |
| FEW_PASSES | 4 | -3373 | 6.853 | 12.71 |
| PARTITION | 0 | 0 | 0 | 0 |

### Pass rate by tier

Tiers under the emitted parameters, over every run with a verdict.

| Tier | Runs | Passed | Pass rate |
| --- | ---: | ---: | ---: |
| ONE_PASS | 9 | 8 | 88.9% |
| FEW_PASSES | 5 | 4 | 80.0% |
| PARTITION | 0 | 0 | - |

## What each input contributes

Correlations of each input with the packages' measured tokens (Pearson and Spearman) and with passing over every run with a verdict (point-biserial). The coefficient is the input's measured tokens per unit in a linear fit with an intercept and every 7.2 term: for a 7.2 term that fit itself, for a candidate that fit plus the candidate alone, with the change in adjusted R² it brings. `*` marks a value that differs from zero at about the 95% level. An input with no marked coefficient and no marked pass correlation has no measurable contribution.

| Input | In 7.2 | Pearson | Spearman | Pass r | Coefficient | SE | t | ΔR²adj | Verdict |
| --- | :---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `volume` | yes | 0.9761 | 0.965 | 0.01935 | 4.42 | 17.72 | 0.2494 | - | no measurable contribution |
| `spec_tokens` | yes | 0.9598 | 0.7904 | -0.368 | 3.481 | 6.69 | 0.5203 | - | no measurable contribution |
| `exported_symbols` | yes | 0.4176 | 0.5664 | 0.07461 | -257.5 | 1748 | -0.1473 | - | no measurable contribution |
| `untested_exports` | yes | 0.2705 | 0.3818 | 0.01026 | 470 | 2769 | 0.1697 | - | no measurable contribution |
| `globals+init_funcs` | yes | 0.2119 | 0.5848 | -0.04915 | 2925 | 14614 | 0.2002 | - | no measurable contribution |
| `test_funcs` |  | 0.7321 | 0.6381 | -0.595* | -618.6 | 1522 | -0.4064 | 0.0003251 | pass/fail only |
| `cognitive_total` |  | 0.825 | 0.8687 | -0.3068 | 17.59 | 337.2 | 0.05217 | -0.006327 | no measurable contribution |
| `cognitive_p90` |  | 0.2212 | 0.5845 | -0.5243 | 1362 | 958 | 1.422 | 0.02478 | no measurable contribution |
| `max_nesting` |  | 0.4757 | 0.6336 | -0.2252 | 3085 | 10694 | 0.2885 | -0.00355 | no measurable contribution |
| `func_count` |  | 0.7561 | 0.7203 | 0.02098 | -1366 | 1705 | -0.8016 | 0.01076 | no measurable contribution |
| `fan_in` |  | 0.5049 | 0.6253 | 0.292 | 3241* | 1095 | 2.961 | 0.02912 | adds cost |

Fitted freely, one test token costs 0.7876 volume tokens (95% interval -10.53 to 12.1; 7.2 fixes it
at 1): tests carry no measurable cost, and the data cannot tell cost from help.

## Does the data fit the 7.2 form?

The free linear fit gives every term its own coefficient. Dividing by the volume coefficient puts each in rebuild tokens, the units 7.2 uses, where volume and spec are fixed at 1. It is fitted on the measured tokens with the fitted knee undone (each past-the-knee measurement taken back through the exponent), so the superlinear growth the form already models does not load onto the terms that grow with size.

| Term | Coefficient | SE | Per volume token | Form fit |
| --- | ---: | ---: | ---: | ---: |
| `intercept` | 13548 | 21767 | - | - |
| `volume` | 4.42 | 17.72 | 1 ± 0 | 1 |
| `spec_tokens` | 3.481 | 6.69 | 0.7876 ± 4.623 | 1 |
| `exported_symbols` | -257.5 | 1748 | -58.26 ± 515.1 | -52.7 |
| `untested_exports` | 470 | 2769 | 106.3 ± 963.4 | 108.8 |
| `globals+init_funcs` | 2925 | 14614 | 661.8 ± 5941 | 941.6 |

The intercept is the per-session overhead; 7.2 has none, since it counts only what the package costs. A term whose per-volume-token value is far from the 7.2 column, measured in its standard errors, is one the form gets wrong, or one the knee absorbs differently.

## Context budget

The budget is held fixed in the fit: it only matters through the knee, where the exponent takes over, and the two trade off. Refitted at other budgets:

| Budget | R² | SSE | Exponent | Past the knee |
| ---: | ---: | ---: | ---: | ---: |
| 6250 | 0.985 | 846423693 | 0.8061 | 6 |
| 12500 | 0.9846 | 871018003 | 0.807 | 5 |
| 18750 | 0.9823 | 1000997508 | 0.8938 | 4 |
| 25000 | 0.9814 | 1051839541 | 0.9616 | 4 |
| 37500 | 0.9602 | 2246419141 | 2.79 (held) | 2 |
| 50000 | 0.9657 | 1939861126 | 2.79 (held) | 1 |
| 75000 | 0.9813 | 1058790435 | 2.79 (held) | 0 |
| 100000 | 0.9813 | 1058790435 | 2.79 (held) | 0 |

## Packages

Tokens are the median of the passing runs, with the smallest and largest. Predicted is the fitted form's measured tokens.

| Package | Tier before | Runs | Passed | Tokens | Spread | Turns | Wall s | Predicted | Residual % |
| --- | --- | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| `smithy-go/container/private/cache` | ONE_PASS | 1 | 1 | 10835 | 10835–10835 | 9 | 23.3 | 18660 | -72.22 |
| `v2/pkg/cmd/config` | ONE_PASS | 1 | 1 | 40679 | 40679–40679 | 27 | 92.61 | 38000 | 6.585 |
| `v2/pkg/cmd/preview` | ONE_PASS | 1 | 1 | 20221 | 20221–20221 | 12 | 53.08 | 21001 | -3.857 |
| `opa/storage/inmem` | ONE_PASS | 1 | 1 | 15826 | 15826–15826 | 10 | 38.79 | 21975 | -38.85 |
| `v4/pkg/cli` | ONE_PASS | 1 | 1 | 77385 | 77385–77385 | 30 | 198.4 | 53658 | 30.66 |
| `client_golang/prometheus/testutil` | FEW_PASSES | 1 | 1 | 95851 | 95851–95851 | 43 | 270.5 | 108034 | -12.71 |
| `grpc/internal/xds/clients/lrsclient` | FEW_PASSES | 1 | 1 | 105839 | 105839–105839 | 40 | 293.9 | 113377 | -7.122 |
| `toml/internal` | FEW_PASSES | 1 | 1 | 83558 | 83558–83558 | 37 | 273.7 | 78123 | 6.505 |
| `smithy-go/testing` | FEW_PASSES | 1 | 0 | - | - | - | - | - | - |
| `opa/util` | FEW_PASSES | 1 | 1 | 34467 | 34467–34467 | 17 | 77.55 | 34857 | -1.13 |
| `v2/caddytest` | PARTITION | 1 | 0 | - | - | - | - | - | - |
| `grpc/internal/xds/bootstrap` | PARTITION | 1 | 1 | 189499 | 189499–189499 | 41 | 438.6 | 178722 | 5.687 |
| `go-cmp/cmp/internal/teststructs` | PARTITION | 1 | 1 | 37417 | 37417–37417 | 22 | 90.02 | 40621 | -8.564 |
| `v2/hclwrite` | PARTITION | 1 | 1 | 240228 | 240228–240228 | 55 | 686.2 | 244777 | -1.894 |

## Censored runs

A run that did not pass is a lower bound: the rebuild costs at least what it spent. The least-squares fit cannot use a bound, so these runs are left out of it and listed here. Where the spend exceeds the fitted cost of a pass, the fit underestimates that package.

| Package | Run | Spent | Turns | Predicted pass | Spent > predicted | Reason |
| --- | ---: | ---: | ---: | ---: | :---: | --- |
| `testing` | 1 | 99703 | 20 | 74303 | yes | tests fail |
| `caddytest` | 1 | 51121 | 20 | 205143 | no | tests fail |

Packages with no passing run, so no measurement at all: `smithy-go/testing` (FEW_PASSES), `v2/caddytest` (PARTITION).

## Excluded rows

None.

## Reproduce

```sh
go run ./calibration/rebuild/fit --unit tree --runs calibration/data/rebuild-trees-2026-09-28-claude-code-opus/runs.jsonl --base internal/config/default.yaml --date 2026-09-28 --out /private/tmp/claude-502/-Users-copierson-Projects-astimate/a4587da2-a070-447b-9477-6211eb476f75/scratchpad/trees-candidate.yaml --report calibration/reports/rebuild-trees-2026-09-28-claude-code-opus.md
```
