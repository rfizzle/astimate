# Rebuild parameters rebuild-2026-09-28-claude-code-opus

Fitted by `calibration/rebuild/fit` (SPEC.md 7.2 and 11.2) from `calibration/data/rebuild-2026-09-28-claude-code-opus/runs.jsonl`. Candidate configuration: `calibration/rebuild/astimate-rebuild-2026-09-28-claude-code-opus.yaml`, the base `thresholds-2026-09-28` with only the rebuild parameters and `config_version` changed.

## Summary

26 runs of 26 packages by claude-code-opus (model claude-opus-5-5): 25 packages have a passing run,
1 runs failed and are censored, 0 rows were excluded. The 7.2 form explains 93.5% of the variance in
measured tokens (R², 25 packages). A session spends about 23694 tokens before it reads the package,
and each token of the estimate costs about 4.346 measured tokens. Changed: tokens_per_export 40 →
0, tokens_per_untested_export 800 → 120, tokens_per_hidden_state 400 → 0, superlinear_exponent
1.3 → 2.79. tokens_per_export fitted -58.88, clamped to 0: Validate requires >= 0, and the form
cannot say that this input lowers the cost. tokens_per_hidden_state fitted -777.6, clamped to 0:
Validate requires >= 0, and the form cannot say that this input lowers the cost. Fitted freely, one
test token costs 0.7029 volume tokens (95% interval -0.5 to 1.906; 7.2 fixes it at 1): tests carry
no measurable cost, and the data cannot tell cost from help. No measurable contribution, to tokens
or to passing: exported_symbols, untested_exports, globals+init_funcs, test_funcs, cognitive_p90,
max_nesting, fan_in. The fit uses passing runs only. Of the 1 failed runs, 0 had already spent more
than the fit predicts a pass of their package costs, so the fit underestimates at least those
packages. 1 packages never passed and are not in the fit at all; if they are the hard ones, every
fitted cost is biased low. Of the budgets tried, 25000 fits best.

Before this configuration ships, check it against the acceptance invariants (SPEC.md 7.5):

```sh
ASTIMATE_CONFIG=$PWD/calibration/rebuild/astimate-rebuild-2026-09-28-claude-code-opus.yaml go test ./internal/invariants
```

## Parameters

| Parameter | Base | Fitted | SE | Emitted |
| --- | ---: | ---: | ---: | ---: |
| `context_budget` | 25000 | held | - | 25000 |
| `tokens_per_export` | 40 | -58.88 | 231.2 | 0 |
| `tokens_per_untested_export` | 800 | 116.6 | 456.8 | 120 |
| `tokens_per_hidden_state` | 400 | -777.6 | 962.3 | 0 |
| `superlinear_exponent` | 1.3 | 2.791 | 2.89 | 2.79 |
| overhead (fit only) | - | 23694 | 21009 | - |
| scale (fit only) | - | 4.346 | 3.274 | - |

The overhead (tokens a session spends whatever the package) and the scale (measured tokens per token of the estimate) belong to the fit, not the configuration: the estimate counts what a rebuild must hold in context, and the scale converts it to what the agent spent. The per-item costs are rounded to two significant figures and the exponent to two decimals.

- `tokens_per_export`: fitted -58.88, clamped to 0: Validate requires >= 0, and the form cannot say that this input lowers the cost.
- `tokens_per_hidden_state`: fitted -777.6, clamped to 0: Validate requires >= 0, and the form cannot say that this input lowers the cost.

## Data

| | |
| --- | --- |
| Agent | claude-code-opus |
| Model asked for | claude-opus-5-5 |
| Models reported | claude-opus-5-5 |
| Rows | 26 |
| Packages with a verdict | 26 |
| Packages with a passing run | 25 |
| Censored runs (failed) | 1 |
| Excluded rows | 0 |
| Passing runs that hit the turn cap | 1 |

Measured tokens are the `footprint` measure, input + cache writes + output, summed over the session. The footprint counts every token that entered the agent's context once: what it read, what tools returned and what it wrote. That is the quantity 7.2's rebuild_tokens models, what a rebuild must hold in context. Cache reads are left out of it because each turn re-reads the whole context, so they grow with turns times context size and count the same token many times; `--measure total` regresses on everything the session consumed instead. A package's measurement is the median of its passing runs; the spread is in the package table.

## Fit quality per output

| Output | N | Against | R² | Note |
| --- | ---: | --- | ---: | --- |
| Measured tokens | 25 | the 7.2 form | 0.9352 | 4 packages past the knee (r > 1), 4 at r ≥ 1.1 |
| Measured tokens | 25 | free linear fit, on tokens with the knee undone | 0.8754 | adjusted R² 0.8426 |
| Turns (median of passes) | 25 | fitted rebuild_tokens | 0.6793 | 10.35 + 1.468 per 1,000 rebuild tokens |
| Agent wall seconds (median of passes) | 25 | fitted rebuild_tokens | 0.8306 | 9.205 + 21.52 per 1,000 rebuild tokens |
| Passing (per run) | 26 | emitted agent_passes | - | point-biserial r = -0.9087* |

### Residuals by tier

Tiers under the fitted parameters; residual = measured − predicted.

| Tier | Packages | Mean residual | Mean abs % | Max abs % |
| --- | ---: | ---: | ---: | ---: |
| ONE_PASS | 21 | 1585 | 71.53 | 300.6 |
| FEW_PASSES | 3 | -17395 | 10.08 | 15.01 |
| PARTITION | 1 | 18900 | 4.669 | 4.669 |

### Pass rate by tier

Tiers under the emitted parameters, over every run with a verdict.

| Tier | Runs | Passed | Pass rate |
| --- | ---: | ---: | ---: |
| ONE_PASS | 21 | 21 | 100.0% |
| FEW_PASSES | 0 | 0 | - |
| PARTITION | 5 | 4 | 80.0% |

## What each input contributes

Correlations of each input with the packages' measured tokens (Pearson and Spearman) and with passing over every run with a verdict (point-biserial). The coefficient is the input's measured tokens per unit in a linear fit with an intercept and every 7.2 term: for a 7.2 term that fit itself, for a candidate that fit plus the candidate alone, with the change in adjusted R² it brings. `*` marks a value that differs from zero at about the 95% level. An input with no marked coefficient and no marked pass correlation has no measurable contribution.

| Input | In 7.2 | Pearson | Spearman | Pass r | Coefficient | SE | t | ΔR²adj | Verdict |
| --- | :---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `volume` | yes | 0.9727 | 0.8869 | -0.5699* | 5.089* | 1.393 | 3.653 | - | adds cost |
| `spec_tokens` | yes | 0.7039 | 0.6375 | -0.6582* | 3.577 | 3.26 | 1.097 | - | pass/fail only |
| `exported_symbols` | yes | 0.471 | 0.7055 | -0.2388 | -539.4 | 1503 | -0.3589 | - | no measurable contribution |
| `untested_exports` | yes | 0.2062 | 0.6135 | -0.1863 | 1243 | 2457 | 0.5058 | - | no measurable contribution |
| `globals+init_funcs` | yes | 0.5389 | 0.7364 | -0.2673 | -1435 | 10162 | -0.1412 | - | no measurable contribution |
| `test_funcs` |  | 0.4708 | 0.5813 | -2.551e-18 | 1340 | 2994 | 0.4475 | 0.006556 | no measurable contribution |
| `cognitive_total` |  | 0.8738 | 0.8407 | -0.6133* | 68.56 | 281.2 | 0.2438 | -0.00153 | pass/fail only |
| `cognitive_p90` |  | 0.5414 | 0.4597 | -0.1694 | 1384 | 1113 | 1.243 | 0.001678 | no measurable contribution |
| `max_nesting` |  | 0.699 | 0.7451 | -0.1757 | 4984 | 9055 | 0.5504 | 0.007324 | no measurable contribution |
| `func_count` |  | 0.6433 | 0.8374 | -0.488* | 993.2 | 619.2 | 1.604 | 0.05815 | pass/fail only |
| `fan_in` |  | 0.2347 | 0.2036 | 0.09854 | 1609 | 2392 | 0.6724 | 0.03523 | no measurable contribution |

Fitted freely, one test token costs 0.7029 volume tokens (95% interval -0.5 to 1.906; 7.2 fixes it
at 1): tests carry no measurable cost, and the data cannot tell cost from help.

## Does the data fit the 7.2 form?

The free linear fit gives every term its own coefficient. Dividing by the volume coefficient puts each in rebuild tokens, the units 7.2 uses, where volume and spec are fixed at 1. It is fitted on the measured tokens with the fitted knee undone (each past-the-knee measurement taken back through the exponent), so the superlinear growth the form already models does not load onto the terms that grow with size.

| Term | Coefficient | SE | Per volume token | Form fit |
| --- | ---: | ---: | ---: | ---: |
| `intercept` | 18038* | 6855 | - | - |
| `volume` | 5.089* | 1.393 | 1 ± 0 | 1 |
| `spec_tokens` | 3.577 | 3.26 | 0.7029 ± 0.5747 | 1 |
| `exported_symbols` | -539.4 | 1503 | -106 ± 276.1 | -58.88 |
| `untested_exports` | 1243 | 2457 | 244.2 ± 440.7 | 116.6 |
| `globals+init_funcs` | -1435 | 10162 | -282 ± 1963 | -777.6 |

The intercept is the per-session overhead; 7.2 has none, since it counts only what the package costs. A term whose per-volume-token value is far from the 7.2 column, measured in its standard errors, is one the form gets wrong, or one the knee absorbs differently.

## Context budget

The budget is held fixed in the fit: it only matters through the knee, where the exponent takes over, and the two trade off. Refitted at other budgets:

| Budget | R² | SSE | Exponent | Past the knee |
| ---: | ---: | ---: | ---: | ---: |
| 6250 | 0.906 | 22585004814 | 1.472 | 11 |
| 12500 | 0.9102 | 21567213223 | 1.565 | 7 |
| 18750 | 0.9195 | 19328819182 | 2.042 | 4 |
| 25000 | 0.9352 | 15558014618 | 2.791 | 4 |
| 37500 | 0.8952 | 25182036373 | 1.3 (held) | 0 |
| 50000 | 0.8952 | 25182036373 | 1.3 (held) | 0 |
| 75000 | 0.8952 | 25182036373 | 1.3 (held) | 0 |
| 100000 | 0.8952 | 25182036373 | 1.3 (held) | 0 |

## Packages

Tokens are the median of the passing runs, with the smallest and largest. Predicted is the fitted form's measured tokens.

| Package | Tier before | Runs | Passed | Tokens | Spread | Turns | Wall s | Predicted | Residual % |
| --- | --- | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| `grpc/resolver/manual` | ONE_PASS | 1 | 1 | 18087 | 18087–18087 | 15 | 43.2 | 31550 | -74.43 |
| `v2/pkg/cmd/project/shared/format` | ONE_PASS | 1 | 1 | 6898 | 6898–6898 | 7 | 14.1 | 24282 | -252 |
| `v2/pkg/cmd/project/delete` | ONE_PASS | 1 | 1 | 15113 | 15113–15113 | 8 | 31.65 | 39993 | -164.6 |
| `v2/internal/pipe/discourse` | ONE_PASS | 1 | 1 | 15666 | 15666–15666 | 11 | 30.82 | 30466 | -94.47 |
| `v2/pkg/cmd/project/list` | ONE_PASS | 1 | 1 | 35985 | 35985–35985 | 15 | 55.76 | 56901 | -58.12 |
| `viper/internal/testutil` | ONE_PASS | 1 | 1 | 6060 | 6060–6060 | 6 | 12.24 | 24275 | -300.6 |
| `pebble/sstable/virtual` | ONE_PASS | 1 | 1 | 8300 | 8300–8300 | 7 | 20.99 | 25549 | -207.8 |
| `pebble/internal/ascii/table` | ONE_PASS | 1 | 1 | 36126 | 36126–36126 | 17 | 109.4 | 38583 | -6.802 |
| `v2/internal/pipe/krew` | ONE_PASS | 1 | 1 | 68488 | 68488–68488 | 27 | 131.2 | 75345 | -10.01 |
| `gitea.dev/models/gituser` | ONE_PASS | 1 | 1 | 31894 | 31894–31894 | 23 | 60.34 | 28621 | 10.26 |
| `v4/internal/third_party/k8s.io/kubernetes/deployment/util` | ONE_PASS | 1 | 1 | 22812 | 22812–22812 | 7 | 63.64 | 35820 | -57.02 |
| `opa/internal/strings` | ONE_PASS | 1 | 1 | 36236 | 36236–36236 | 17 | 86.85 | 27147 | 25.08 |
| `opa/v1/loader` | FEW_PASSES | 1 | 1 | 116442 | 116442–116442 | 30 | 297.1 | 98197 | 15.67 |
| `crypto/cryptobyte` | FEW_PASSES | 1 | 1 | 73025 | 73025–73025 | 14 | 181.1 | 95372 | -30.6 |
| `gitea.dev/modules/web` | FEW_PASSES | 1 | 1 | 129753 | 129753–129753 | 36 | 409.1 | 62053 | 52.18 |
| `smithy-go/traits` | FEW_PASSES | 1 | 1 | 30016 | 30016–30016 | 14 | 69.62 | 32833 | -9.386 |
| `v4/pkg/chart` | FEW_PASSES | 1 | 1 | 36974 | 36974–36974 | 15 | 88.46 | 26620 | 28 |
| `opa/internal/wasm/instruction` | FEW_PASSES | 1 | 1 | 31004 | 31004–31004 | 15 | 70.34 | 29469 | 4.951 |
| `toml/internal/toml-test` | FEW_PASSES | 1 | 1 | 93484 | 93484–93484 | 39 | 259.2 | 43890 | 53.05 |
| `v9/internal/proto` | PARTITION | 1 | 1 | 164933 | 164933–164933 | 30 | 345 | 189697 | -15.01 |
| `smithy-go/eventstream` | PARTITION | 1 | 1 | 136422 | 136422–136422 | 31 | 321.3 | 84257 | 38.24 |
| `v2/caddyconfig/caddyfile` | PARTITION | 1 | 1 | 254343 | 254343–254343 | 47 | 855.3 | 287746 | -13.13 |
| `pebble/internal/compact` | PARTITION | 1 | 1 | 404832 | 404832–404832 | 103 | 1107 | 385932 | 4.669 |
| `go-cmp/cmp/internal/teststructs` | PARTITION | 1 | 1 | 48673 | 48673–48673 | 20 | 91.31 | 52951 | -8.789 |
| `v9/maintnotifications` | PARTITION | 1 | 1 | 287284 | 287284–287284 | 62 | 712.5 | 281302 | 2.082 |
| `go-cmp/cmp` | PARTITION | 1 | 0 | - | - | - | - | - | - |

## Censored runs

A run that did not pass is a lower bound: the rebuild costs at least what it spent. The least-squares fit cannot use a bound, so these runs are left out of it and listed here. Where the spend exceeds the fitted cost of a pass, the fit underestimates that package.

| Package | Run | Spent | Turns | Predicted pass | Spent > predicted | Reason |
| --- | ---: | ---: | ---: | ---: | :---: | --- |
| `cmp` | 1 | 359741 | 55 | 1855284 | no | tests fail |

Packages with no passing run, so no measurement at all: `go-cmp/cmp` (PARTITION).

## Excluded rows

None.

## Reproduce

```sh
go run ./calibration/rebuild/fit --runs calibration/data/rebuild-2026-09-28-claude-code-opus/runs.jsonl --base internal/config/default.yaml --date 2026-09-28 --out calibration/rebuild/astimate-rebuild-2026-09-28-claude-code-opus.yaml --report calibration/reports/rebuild-2026-09-28-claude-code-opus.md
```
