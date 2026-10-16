# understanding fixture

Module `example.com/understanding` holds the package at the ceiling of the
human estimate's software understanding (SPEC.md 7.3): `opaque` has no
tests and no doc comment on any export, and its two functions put
`cognitive_p90` at 16, so structure rates nominal, application clarity and
self-descriptiveness very low, and the mean, (2 + 0 + 0) / 3 rounded down,
picks very low: SU 50. The floor, SU 10, is the `tested` package of
`testdata/go/fixture`, every export tested and documented. The assess goldens
`cmd/astimate/testdata/assess_understanding_opaque.txt` and
`assess_fixture_tested.txt` pin their metrics.

Expected: `exported_symbols=2`, `untested_exports=2`,
`documented_exports=0`, `cognitive_p90=16`, `dup_blocks=0`.
