# Rebuild parameters rebuild-2026-09-28-claude-code-opus

Fitted by `calibration/rebuild/fit` (SPEC.md 7.2 and 11.2) from `calibration/data/rebuild-2026-09-28-claude-code-opus/runs.jsonl`. Candidate configuration: `calibration/rebuild/astimate-rebuild-2026-09-28-claude-code-opus.yaml`, the base `rebuild-2026-09-28-claude-code-opus` with only the rebuild parameters and `config_version` changed.

## Summary

85 runs of 31 packages by claude-code-opus (model claude-opus-5-5): 31 packages have a passing run,
6 runs failed and are censored, 0 rows were excluded. The 7.2 form explains 92.5% of the variance in
measured tokens (R², 31 packages). A session spends about 8399 tokens before it reads the package,
and each token of the estimate costs about 7.629 measured tokens. Changed: context_budget 25000 →
37500, tokens_per_untested_export 120 → 300, superlinear_exponent 2.79 → 1. tokens_per_export
fitted -169.4, clamped to 0: Validate requires >= 0, and the form cannot say that this input lowers
the cost. tokens_per_hidden_state fitted -357.2, clamped to 0: Validate requires >= 0, and the form
cannot say that this input lowers the cost. superlinear_exponent fitted -0.00757, clamped to 1:
Validate requires >= 1; past the knee the measured cost grows no faster than r. Fitted freely, one
test token costs 5.964 volume tokens (95% interval -77.58 to 89.5; 7.2 fixes it at 1): tests carry
no measurable cost, and the data cannot tell cost from help. No measurable contribution, to tokens
or to passing: exported_symbols, untested_exports, globals+init_funcs, test_funcs, cognitive_p90,
max_nesting, fan_in. The fit uses passing runs only. Of the 6 failed runs, 5 had already spent more
than the fit predicts a pass of their package costs, so the fit underestimates at least those
packages. Of the budgets tried, 37500 fits best.

Before this configuration ships, check it against the acceptance invariants (SPEC.md 7.5):

```sh
ASTIMATE_CONFIG=$PWD/calibration/rebuild/astimate-rebuild-2026-09-28-claude-code-opus.yaml go test ./internal/invariants
```

## Parameters

| Parameter | Base | Fitted | SE | Emitted |
| --- | ---: | ---: | ---: | ---: |
| `context_budget` | 25000 | held | - | 37500 |
| `tokens_per_export` | 0 | -169.4 | 68.59 | 0 |
| `tokens_per_untested_export` | 120 | 304.5 | 99.9 | 300 |
| `tokens_per_hidden_state` | 0 | -357.2 | 169.7 | 0 |
| `superlinear_exponent` | 2.79 | -0.00757 | 0.3078 | 1 |
| overhead (fit only) | - | 8399 | 5589 | - |
| scale (fit only) | - | 7.629 | 1.503 | - |

The overhead (tokens a session spends whatever the package) and the scale (measured tokens per token of the estimate) belong to the fit, not the configuration: the estimate counts what a rebuild must hold in context, and the scale converts it to what the agent spent. The per-item costs are rounded to two significant figures and the exponent to two decimals.

- `tokens_per_export`: fitted -169.4, clamped to 0: Validate requires >= 0, and the form cannot say that this input lowers the cost.
- `tokens_per_hidden_state`: fitted -357.2, clamped to 0: Validate requires >= 0, and the form cannot say that this input lowers the cost.
- `superlinear_exponent`: fitted -0.00757, clamped to 1: Validate requires >= 1; past the knee the measured cost grows no faster than r.

## Data

| | |
| --- | --- |
| Agent | claude-code-opus |
| Model asked for | claude-opus-5-5 |
| Models reported | claude-opus-5-5 |
| Rows | 85 |
| Packages with a verdict | 31 |
| Packages with a passing run | 31 |
| Censored runs (failed) | 6 |
| Excluded rows | 0 |
| Passing runs that hit the turn cap | 3 |

Measured tokens are the `footprint` measure, input + cache writes + output, summed over the session. The footprint counts every token that entered the agent's context once: what it read, what tools returned and what it wrote. That is the quantity 7.2's rebuild_tokens models, what a rebuild must hold in context. Cache reads are left out of it because each turn re-reads the whole context, so they grow with turns times context size and count the same token many times; `--measure total` regresses on everything the session consumed instead. A package's measurement is the median of its passing runs; the spread is in the package table.

## Fit quality per output

| Output | N | Against | R² | Note |
| --- | ---: | --- | ---: | --- |
| Measured tokens | 31 | the 7.2 form | 0.9246 | 1 packages past the knee (r > 1), 1 at r ≥ 1.1 |
| Measured tokens | 31 | free linear fit, on tokens with the knee undone | 0.8107 | adjusted R² 0.7728 |
| Turns (median of passes) | 31 | fitted rebuild_tokens | 0.5649 | 13.04 + 1.102 per 1,000 rebuild tokens |
| Agent wall seconds (median of passes) | 31 | fitted rebuild_tokens | 0.793 | 47.41 + 16.83 per 1,000 rebuild tokens |
| Passing (per run) | 85 | emitted agent_passes | - | point-biserial r = -0.3228* |

### Residuals by tier

Tiers under the fitted parameters; residual = measured − predicted.

| Tier | Packages | Mean residual | Mean abs % | Max abs % |
| --- | ---: | ---: | ---: | ---: |
| ONE_PASS | 31 | -1.314e-11 | 33.93 | 111.2 |
| FEW_PASSES | 0 | 0 | 0 | 0 |
| PARTITION | 0 | 0 | 0 | 0 |

### Pass rate by tier

Tiers under the emitted parameters, over every run with a verdict.

| Tier | Runs | Passed | Pass rate |
| --- | ---: | ---: | ---: |
| ONE_PASS | 67 | 65 | 97.0% |
| FEW_PASSES | 18 | 14 | 77.8% |
| PARTITION | 0 | 0 | - |

## What each input contributes

Correlations of each input with the packages' measured tokens (Pearson and Spearman) and with passing over every run with a verdict (point-biserial). The coefficient is the input's measured tokens per unit in a linear fit with an intercept and every 7.2 term: for a 7.2 term that fit itself, for a candidate that fit plus the candidate alone, with the change in adjusted R² it brings. `*` marks a value that differs from zero at about the 95% level. An input with no marked coefficient and no marked pass correlation has no measurable contribution.

| Input | In 7.2 | Pearson | Spearman | Pass r | Coefficient | SE | t | ΔR²adj | Verdict |
| --- | :---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `volume` | yes | 0.9602 | 0.9202 | -0.279* | 1.111 | 6.72 | 0.1653 | - | pass/fail only |
| `spec_tokens` | yes | 0.7682 | 0.7539 | -0.3761* | 6.625 | 5.707 | 1.161 | - | pass/fail only |
| `exported_symbols` | yes | 0.4869 | 0.7033 | -0.1091 | -585.4 | 1876 | -0.3121 | - | no measurable contribution |
| `untested_exports` | yes | 0.2249 | 0.557 | -0.06319 | 1244 | 3310 | 0.3758 | - | no measurable contribution |
| `globals+init_funcs` | yes | 0.3883 | 0.6352 | -0.08086 | 1465 | 8563 | 0.1711 | - | no measurable contribution |
| `test_funcs` |  | 0.4213 | 0.6185 | -0.1311 | -455.8 | 3437 | -0.1326 | -0.004916 | no measurable contribution |
| `cognitive_total` |  | 0.8779 | 0.8762 | -0.4043* | 154.5 | 230.6 | 0.6701 | 0.01283 | pass/fail only |
| `cognitive_p90` |  | 0.6041 | 0.546 | -0.2104 | 1960 | 1827 | 1.073 | 0.001098 | no measurable contribution |
| `max_nesting` |  | 0.7331 | 0.7932 | -0.1575 | 9226 | 6844 | 1.348 | 0.02262 | no measurable contribution |
| `func_count` |  | 0.6779 | 0.8138 | -0.2418* | 682.9 | 1031 | 0.6625 | 0.00379 | pass/fail only |
| `fan_in` |  | 0.1549 | 0.08251 | -0.03178 | 2415 | 1602 | 1.508 | 0.03539 | no measurable contribution |

Fitted freely, one test token costs 5.964 volume tokens (95% interval -77.58 to 89.5; 7.2 fixes it
at 1): tests carry no measurable cost, and the data cannot tell cost from help.

## Does the data fit the 7.2 form?

The free linear fit gives every term its own coefficient. Dividing by the volume coefficient puts each in rebuild tokens, the units 7.2 uses, where volume and spec are fixed at 1. It is fitted on the measured tokens with the fitted knee undone (each past-the-knee measurement taken back through the exponent), so the superlinear growth the form already models does not load onto the terms that grow with size.

| Term | Coefficient | SE | Per volume token | Form fit |
| --- | ---: | ---: | ---: | ---: |
| `intercept` | 19522* | 8793 | - | - |
| `volume` | 1.111 | 6.72 | 1 ± 0 | 1 |
| `spec_tokens` | 6.625 | 5.707 | 5.964 ± 40.56 | 1 |
| `exported_symbols` | -585.4 | 1876 | -527 ± 4301 | -169.4 |
| `untested_exports` | 1244 | 3310 | 1120 ± 8730 | 304.5 |
| `globals+init_funcs` | 1465 | 8563 | 1319 ± 12806 | -357.2 |

The intercept is the per-session overhead; 7.2 has none, since it counts only what the package costs. A term whose per-volume-token value is far from the 7.2 column, measured in its standard errors, is one the form gets wrong, or one the knee absorbs differently.

## Context budget

The budget is held fixed in the fit: it only matters through the knee, where the exponent takes over, and the two trade off. Refitted at other budgets:

| Budget | R² | SSE | Exponent | Past the knee |
| ---: | ---: | ---: | ---: | ---: |
| 9375 | 0.8633 | 41090661859 | 0.7088 | 13 |
| 18750 | 0.8755 | 37415061541 | 0.5291 | 7 |
| 28125 | 0.9103 | 26966095745 | 0.1598 | 4 |
| 37500 | 0.9246 | 22675479544 | -0.00757 | 1 |
| 56250 | 0.7145 | 85840898433 | 2.79 (held) | 1 |
| 75000 | 0.8353 | 49508525147 | 2.79 (held) | 0 |
| 112500 | 0.8353 | 49508525147 | 2.79 (held) | 0 |
| 150000 | 0.8353 | 49508525147 | 2.79 (held) | 0 |

## Packages

Tokens are the median of the passing runs, with the smallest and largest. Predicted is the fitted form's measured tokens.

| Package | Tier before | Runs | Passed | Tokens | Spread | Turns | Wall s | Predicted | Residual % |
| --- | --- | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| `grpc/resolver/manual` | ONE_PASS | 3 | 3 | 15966 | 15829–18087 | 7 | 38.53 | 20335 | -27.37 |
| `v2/pkg/cmd/project/shared/format` | ONE_PASS | 3 | 3 | 6903 | 6898–6949 | 7 | 13.91 | 7744 | -12.19 |
| `v2/pkg/cmd/project/delete` | ONE_PASS | 3 | 3 | 17128 | 15113–18248 | 10 | 59.46 | 36166 | -111.2 |
| `v2/internal/pipe/discourse` | ONE_PASS | 3 | 3 | 15900 | 15666–16018 | 10 | 30.82 | 15227 | 4.236 |
| `v2/pkg/cmd/project/list` | ONE_PASS | 3 | 3 | 34899 | 30733–35985 | 13 | 55.76 | 65844 | -88.67 |
| `viper/internal/testutil` | ONE_PASS | 3 | 3 | 6030 | 5978–6060 | 6 | 14.35 | 10010 | -66 |
| `pebble/sstable/virtual` | ONE_PASS | 3 | 3 | 10692 | 8300–11809 | 8 | 24.16 | 11403 | -6.649 |
| `pebble/internal/ascii/table` | ONE_PASS | 3 | 3 | 41974 | 36126–60349 | 18 | 109.4 | 26612 | 36.6 |
| `v2/internal/pipe/krew` | ONE_PASS | 3 | 3 | 76013 | 68488–77945 | 26 | 133.3 | 93334 | -22.79 |
| `gitea.dev/models/gituser` | ONE_PASS | 3 | 3 | 31894 | 31508–41178 | 21 | 60.34 | 16289 | 48.93 |
| `v4/internal/third_party/k8s.io/kubernetes/deployment/util` | ONE_PASS | 3 | 3 | 20185 | 19619–22812 | 5 | 63.64 | 34495 | -70.89 |
| `opa/internal/strings` | ONE_PASS | 3 | 1 | 36236 | 36236–36236 | 17 | 86.85 | 15641 | 56.84 |
| `opa/v1/loader` | FEW_PASSES | 3 | 3 | 116442 | 96627–117158 | 23 | 297.1 | 129405 | -11.13 |
| `crypto/cryptobyte` | FEW_PASSES | 3 | 3 | 73764 | 73025–80198 | 14 | 182.9 | 109102 | -47.91 |
| `gitea.dev/modules/web` | FEW_PASSES | 3 | 3 | 117657 | 114899–129753 | 36 | 407.6 | 78703 | 33.11 |
| `smithy-go/traits` | FEW_PASSES | 3 | 3 | 30016 | 28549–31425 | 14 | 67.79 | 32643 | -8.751 |
| `v4/pkg/chart` | FEW_PASSES | 3 | 3 | 36974 | 30944–42140 | 13 | 85.97 | 32611 | 11.8 |
| `opa/internal/wasm/instruction` | FEW_PASSES | 3 | 3 | 32398 | 31004–37634 | 18 | 70.42 | 18981 | 41.41 |
| `toml/internal/toml-test` | FEW_PASSES | 3 | 3 | 83936 | 75541–93484 | 31 | 259.2 | 60312 | 28.14 |
| `v9/internal/proto` | PARTITION | 3 | 2 | 154620 | 144307–164933 | 27 | 335.5 | 194768 | -25.97 |
| `smithy-go/eventstream` | PARTITION | 3 | 3 | 136422 | 128327–149001 | 31 | 331.6 | 116934 | 14.29 |
| `v2/caddyconfig/caddyfile` | PARTITION | 3 | 2 | 264697 | 254343–275050 | 51.5 | 904 | 284445 | -7.461 |
| `pebble/internal/compact` | PARTITION | 3 | 3 | 379882 | 349039–404832 | 108 | 1107 | 277595 | 26.93 |
| `go-cmp/cmp/internal/teststructs` | PARTITION | 3 | 3 | 48673 | 44455–51974 | 21 | 102.6 | 66119 | -35.84 |
| `v9/maintnotifications` | PARTITION | 3 | 3 | 287284 | 281016–347977 | 66 | 784 | 268688 | 6.473 |
| `go-cmp/cmp` | PARTITION | 3 | 1 | 293173 | 293173–293173 | 49 | 869.9 | 293173 | 1.985e-14 |
| `crypto/openpgp/errors` | ONE_PASS | 2 | 2 | 9774 | 9739–9808 | 5 | 26.72 | -1019 | 110.4 |
| `gin/codec/json` | ONE_PASS | 2 | 2 | 17220 | 14862–19577 | 13 | 37.31 | 8462 | 50.86 |
| `v3/middleware/logger` | FEW_PASSES | 1 | 1 | 171925 | 171925–171925 | 41 | 388.5 | 203147 | -18.16 |
| `grpc/internal/xds/server` | FEW_PASSES | 1 | 1 | 212059 | 212059–212059 | 72 | 553.1 | 251361 | -18.53 |
| `v4/y` | PARTITION | 1 | 1 | 90901 | 90901–90901 | 35 | 266.6 | 93106 | -2.425 |

## Censored runs

A run that did not pass is a lower bound: the rebuild costs at least what it spent. The least-squares fit cannot use a bound, so these runs are left out of it and listed here. Where the spend exceeds the fitted cost of a pass, the fit underestimates that package.

| Package | Run | Spent | Turns | Predicted pass | Spent > predicted | Reason |
| --- | ---: | ---: | ---: | ---: | :---: | --- |
| `cmp` | 1 | 359741 | 55 | 293173 | yes | tests fail |
| `strings` | 3 | 26076 | 16 | 15641 | yes | tests fail |
| `strings` | 2 | 45323 | 29 | 15641 | yes | tests fail |
| `proto` | 2 | 150718 | 19 | 194768 | no | tests fail |
| `caddyfile` | 2 | 288549 | 41 | 284445 | yes | tests fail |
| `cmp` | 3 | 311820 | 50 | 293173 | yes | tests fail |

## Excluded rows

None.

## Reproduce

```sh
go run ./calibration/rebuild/fit --runs calibration/data/rebuild-2026-09-28-claude-code-opus/runs.jsonl --base internal/config/default.yaml --date 2026-09-28 --budget 37500 --out calibration/rebuild/astimate-rebuild-2026-09-28-claude-code-opus.yaml --report calibration/reports/rebuild-2026-09-28-claude-code-opus.md
```
