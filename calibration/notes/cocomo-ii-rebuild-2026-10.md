# COCOMO II and the rebuild-from-tests profile

Date: 2026-10-16. Code: the commit that replaced COCOMO 81 basic organic
mode with COCOMO II's post-architecture form for `human_days` (SPEC.md
7.3). Reference: the COCOMO II.2000 post-architecture calibration, Boehm et
al., *Software Cost Estimation with COCOMO II* (Prentice Hall, 2000),
chapter 2's tables of scale factors and effort multipliers. Every value
below is that calibration's published value; nothing is fitted.

## Question

The engineer time astimate prints was COCOMO 81 basic organic mode,
`2.4 × KLOC^1.05` person-months. That model has no way to say what a
rebuild from existing tests is: the behaviour is known, the target is
exact, the signatures and package boundaries are already decided and there
is nothing to document. COCOMO II rates exactly those things. Which
ratings describe a rebuild, which of the effort multipliers can the code
itself rate, and how far does the change move the figure?

## The form

```
E             = B + 0.01 × ΣSF                       B = 0.91
person_months = A × KSLOC^E × ΠEM                    A = 2.94
human_days    = person_months × 19
```

KSLOC is astimate's `kloc_eff`, unchanged: the non-duplicated thousand
lines, inflated by up to half for untested exports. With every rating
nominal, ΣSF = 3.72 + 3.04 + 4.24 + 3.29 + 4.68 = 18.97, E = 1.0997, and
every multiplier is 1.00, so the coefficient is A, 2.94. `days_per_month`
stays 19.

## The scale factors

Ratings run very low, low, nominal, high, very high, extra high.

| Factor | Table row (VL, L, N, H, VH, XH) | Rating | Value | Reason |
| --- | --- | --- | --- | --- |
| PREC, precedentedness | 6.20, 4.96, 3.72, 2.48, 1.24, 0.00 | very high | 1.24 | The behaviour is known and the tests say what passes. Extra high is "thoroughly familiar", which a rebuilder reading the package for the first time is not |
| FLEX, development flexibility | 5.07, 4.05, 3.04, 2.03, 1.01, 0.00 | very low | 5.07 | "Rigorous" conformance: the rebuild must pass the existing tests and keep every exported signature, with no room to relax a requirement |
| RESL, architecture and risk resolution | 7.07, 5.65, 4.24, 2.83, 1.41, 0.00 | extra high | 0.00 | The architecture is fully resolved before work starts: the package boundary, its imports and its exported signatures are fixed by the code being replaced |
| TEAM, team cohesion | 5.48, 4.38, 3.29, 2.19, 1.10, 0.00 | nominal | 3.29 | Nothing in the code says who rebuilds it |
| PMAT, process maturity | 7.80, 6.24, 4.68, 3.12, 1.56, 0.00 | nominal | 4.68 | Likewise unknowable from the code |

ΣSF = 1.24 + 5.07 + 0 + 3.29 + 4.68 = 14.28, so E = 0.91 + 0.1428 =
1.0528. FLEX very low is the one rating that raises the exponent over
nominal; PREC and RESL lower it by more.

## The effort multipliers

| Multiplier | Table row (VL, L, N, H, VH, XH; - none) | Rating | Value | Reason |
| --- | --- | --- | --- | --- |
| DOCU, documentation match to life-cycle needs | 0.81, 0.91, 1.00, 1.11, 1.23, - | very low | 0.81 | Nothing to write: the rebuild reproduces code against tests that already exist |
| DATA, database size | -, 0.90, 1.00, 1.14, 1.28, - | nominal | 1.00 | Not knowable from a package's metrics |
| RUSE, developed for reusability | -, 0.95, 1.00, 1.07, 1.15, 1.24 | nominal | 1.00 | The exported contract is already fixed; no reuse beyond it is asked |
| TIME, execution time constraint | -, -, 1.00, 1.11, 1.29, 1.63 | nominal | 1.00 | Not knowable |
| STOR, main storage constraint | -, -, 1.00, 1.05, 1.17, 1.46 | nominal | 1.00 | Not knowable |
| PVOL, platform volatility | -, 0.87, 1.00, 1.15, 1.30, - | nominal | 1.00 | Not knowable |
| ACAP, analyst capability | 1.42, 1.19, 1.00, 0.85, 0.71, - | nominal | 1.00 | Describes the team, not the code |
| PCAP, programmer capability | 1.34, 1.15, 1.00, 0.88, 0.76, - | nominal | 1.00 | Describes the team |
| PCON, personnel continuity | 1.29, 1.12, 1.00, 0.90, 0.81, - | nominal | 1.00 | Describes the team |
| APEX, applications experience | 1.22, 1.10, 1.00, 0.88, 0.81, - | nominal | 1.00 | Describes the team |
| PLEX, platform experience | 1.19, 1.09, 1.00, 0.91, 0.85, - | nominal | 1.00 | Describes the team |
| LTEX, language and tool experience | 1.20, 1.09, 1.00, 0.91, 0.84, - | nominal | 1.00 | Describes the team |
| TOOL, use of software tools | 1.17, 1.09, 1.00, 0.90, 0.78, - | nominal | 1.00 | Describes the environment |
| SITE, multisite development | 1.22, 1.09, 1.00, 0.93, 0.86, 0.80 | nominal | 1.00 | Describes the environment |
| SCED, required development schedule | 1.43, 1.14, 1.00, 1.00, 1.00, - | nominal | 1.00 | No schedule is implied |

The product of the configured multipliers is 0.81, so the profile's
coefficient is A' = 2.94 × 0.81 = 2.3814, printed as 2.381 in the labels.

Two multipliers are not in the configuration: CPLX is rated per package
from its own metrics, and RELY is held nominal.

### CPLX, product complexity

Table row: 0.73, 0.87, 1.00, 1.17, 1.34, 1.74. Rated from `cognitive_p90`,
the 90th percentile of the package's per-function cognitive complexity:

| `cognitive_p90` | Rating | Value | Share of the 2,158 packages of the 2026-09-28 corpus |
| --- | --- | --- | --- |
| up to 1 | very low | 0.73 | 16.7% |
| 2 to 4 | low | 0.87 | 18.0% |
| 5 to 10 | nominal | 1.00 | 29.3% |
| 11 to 20 | high | 1.17 | 22.5% |
| 21 to 40 | very high | 1.34 | 11.0% |
| above 40 | extra high | 1.74 | 2.5% |

A package without functions (`func_count` 0) is nominal: there complexity
is not measured. The cut points follow the glossary's reading of a
function's score (up to 10 easy, 11 to 20 needs care, over 20 hard and the
usual linter limit), with the nominal band holding the corpus median (7);
very low is straight-line code, and extra high is the top 2.5%, where the
corpus p99 is 56. The plan called for `cognitive_max` as a tie-break;
astimate records no package-level maximum (`changed_func_cognitive_max` is
the changed functions' only), so the rating reads the p90 alone.

### RELY, required reliability

Table row: 0.82, 0.92, 1.00, 1.10, 1.26, -. Rating: nominal, 1.00, for
every package, and `rely` is not a configuration key.

The tests are the specification a rebuild must pass, not a reliability
requirement on top of it: COCOMO's RELY asks what a failure of the
delivered software would cost, which nothing in a package's metrics says.
Deriving it from the tests was tried (very low without tests, low with
tests and no coverage figure or under 50%, nominal to 80%, high above)
and dropped. It made a package's first tests raise its figure (0.82 to
0.92) and more measured coverage raise it again, and the project holds
that adding tests never makes a package look worse (SPEC.md 7.5: adding
tests never raises `human_days`). With RELY nominal, tests reach the
figure only through the untested-export inflation of `kloc_eff`, which
falls as tests cover exports.

## The figure, before and after

`go build -o astimate ./cmd/astimate`, then `./astimate rank <root>
--json` and `./astimate report <root> --format json` with the embedded
default and with the same file under `rebuild.cocomo.model: basic-81`.

| Module | Packages | COCOMO II, rebuild profile | COCOMO 81 basic | Ratio |
| --- | --- | --- | --- | --- |
| this repository | 77 | 1,698 days (8 engineer-years) | 1,544 days (7 engineer-years) | 1.10 |
| shhh | 82 | 9,377 days (43 engineer-years) | 9,036 days (41 engineer-years) | 1.04 |

The totals move little. Per thousand lines the profile's base is
slightly cheaper (2.3814 against 2.4, to the power 1.0528 against 1.05),
so a package of ordinary complexity lands near COCOMO 81's 45 working days
per thousand lines; CPLX low and very low take 13% and 27% off simple
packages, while high and very high add 17% and 34% where functions are
complex, and the slightly larger exponent grows faster on the largest
packages. The profile comes out 10% above COCOMO 81 on this repository
and 4% above on shhh.

The model is now argued per rating rather than taken whole: a reader who
thinks a rebuild deserves PREC extra high, or that their team is high
ACAP, changes one named line under `rebuild.cocomo:` and the figure and
its label follow. `rebuild.cocomo.model: basic-81` keeps COCOMO 81 for
comparison.

## Software understanding replaces the untested-export inflation

Date: 2026-10-16. Code: the commit that replaced `kloc_eff`'s
`1 + 0.5 × untested_ratio` with COCOMO II's reuse-model understanding
increment (SPEC.md 7.3). The form above described `kloc_eff` before it;
everything else in it stands.

### The values

COCOMO II.2000's reuse model charges for understanding code that is
reused rather than written, `1 + 0.02 × SU × UNFM` on the size. A
rebuild from tests is that: the tests and the signatures are the assets
to understand, and the exports no test or comment describes are the blind
spots. Both tables are the published ones; nothing is fitted.

| SU rating | very low | low | nominal | high | very high |
| --- | --- | --- | --- | --- | --- |
| SU, percent | 50 | 40 | 30 | 20 | 10 |

| UNFM rating | completely familiar | mostly familiar | somewhat familiar | considerably familiar | mostly unfamiliar | completely unfamiliar |
| --- | --- | --- | --- | --- | --- | --- |
| UNFM | 0.0 | 0.2 | 0.4 | 0.6 | 0.8 | 1.0 |

`rebuild.cocomo.unfm` ships `completely_unfamiliar`: an agent rebuilding a
stranger's package starts from nothing. With it the inflation runs from
1.2 (SU 10) to 2.0 (SU 50), where `1 + 0.5 × untested_ratio` ran from 1.0
to 1.5; `completely_familiar` removes it.

### The mapping

The manual rates SU on structure, application clarity and
self-descriptiveness and takes one rating for all three. Here each is
scored from the package's metrics, very low 0 to very high 4, and the
mean of the three, rounded down toward the worse rating, picks SU.

| Sub-score | Metric | very high | high | nominal | low | very low |
| --- | --- | --- | --- | --- | --- | --- |
| Structure, the worse of | `cognitive_p90` | up to 4 | 5 to 10 | 11 to 20 | 21 to 40 | above 40 |
| | `dup_blocks` | 0 | 1 to 2 | 3 to 5 | 6 to 10 | above 10 |
| Application clarity | `untested_exports / max(exported_symbols, 1)` | up to 0.05 | up to 0.15 | up to 0.35 | up to 0.6 | above 0.6 |
| Self-descriptiveness | `1 - documented_exports / exported_symbols` | up to 0.05 | up to 0.15 | up to 0.35 | up to 0.6 | above 0.6 |

The `cognitive_p90` cut points are CPLX's above with its two lowest bands
merged, five ratings instead of six; on the 2026-09-28 corpus they hold
35%, 29%, 23%, 11% and 2.5% of packages. A duplicate block is code the
rebuilder reads twice and must decide is the same. Application clarity is
the old inflation's own ratio; a share of exported symbols, not of
functions, because `exported_symbols` is the denominator the estimate
already uses. Self-descriptiveness reads the new `documented_exports`,
the exported symbols with a doc comment (the declaration's, its group's,
or a `var` or `const` line comment). TypeScript does not measure it yet,
so there the sub-score is nominal. A package without exports scores very
high on both.

Testing an export can only lower `untested_exports`, so it can only raise
application clarity and lower SU, and no other sub-score reads tests:
SPEC.md 7.5's "adding tests never raises `human_days`" still holds, and
`internal/invariants` `TestAddingTests` passes unchanged.

### Floor and ceiling on fixture packages

| Package | `cognitive_p90` | `dup_blocks` | exports | untested | documented | Sub-scores (S, C, D) | SU |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `testdata/go/fixture` `tested` | 4 | 0 | 3 | 0 | 3 | 4, 4, 4 | 10, the floor |
| `testdata/go/understanding` `opaque` | 16 | 0 | 2 | 2 | 0 | 2, 0, 0 | 50, the ceiling |
| `testdata/go/fixture-degraded` `tested` | 4 | 0 | 4 | 1 | 4 | 4, 2, 4 | 20 |
| `testdata/go/fixture-degraded` `dupes` | 6 | 1 | 4 | 3 | 4 | 3, 0, 4 | 30 |

`opaque` is a fixture added for this: no tests, no doc comment, two
functions of cognitive complexity 12 and 16. The assess goldens
`cmd/astimate/testdata/assess_fixture_tested.txt`,
`assess_understanding_opaque.txt` and `assess_degraded_tested.txt` pin
the metrics each row reads. Rounding down makes the ceiling need
structure nominal or worse: a package with no export tested or
documented but simple, uncopied code scores (4 + 0 + 0) / 3, low, SU 40.

### The figure, before and after

`go build`, then `astimate rank . --json` on this repository's tree at
this commit, summing `human_days`, with the commit's binary and with its
parent's, each under the embedded default and under
`rebuild.cocomo.model: basic-81`.

| Model | Before, `1 + 0.5 × untested_ratio` | After, `1 + 0.02 × SU × UNFM` | After / before |
| --- | --- | --- | --- |
| COCOMO II, rebuild profile | 1,703 days | 2,424 days | 1.42 |
| COCOMO 81 basic | 1,549 days | 2,199 days | 1.42 |
| Profile / basic-81 | 1.10 | 1.10 | |

(The section above measured 1,698 and 1,544 days at its own commit; the
tree has grown since.) Of the 77 packages, 6 rate SU 10, 69 SU 20 and 2
SU 30. This repository tests and documents nearly every export, so the
old inflation was close to 1.0 on it; the new one cannot go below 1.2
under `completely_unfamiliar`, and structure, nominal for 42 packages
here and high for 24 (5 low, 6 very high), holds the mean at high
while clarity and self-descriptiveness rate very high. Both models rise by
the same 42%, about 1.4^1.05, because they share `kloc_eff`.
