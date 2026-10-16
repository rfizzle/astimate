## Fixture has no package that needs a split.

12 packages; 0 need more than one agent sitting to rebuild from their tests. For one engineer the whole module is about 2 weeks of rebuild time. 11 are fine, 1 needs watching, and 0 need a split. There is no change history to read, so the packages to start with are ranked by rebuild time alone.

_Engineer time runs, per package, from the lower to the higher of COCOMO 81 and COCOMO II, rebuild profile; not a measurement; agent sittings are measured. rebuild.cocomo and rebuild.cocomo_a set it._

### Start here

1. `dupes` (watch): one sitting, 1 d for one engineer
   It is inside every limit, and 80.6% of its lines are copies of each other.
   Do this: Extract the copied code next time you work here; nothing needs a split yet.

### Every package

Most changed and costly first; * marks a package over its size limit.

| Package | Band | Sittings | Time | Lines | Copied | Untested | Changes |
|---|---|---|---|---|---|---|---|
| `hidden` | fine | one sitting | 2 d | 32 | 0% | 1 | n/a |
| `immut` | fine | one sitting | 1–2 d | 31 | 0% | 3 | n/a |
| `a` | fine | one sitting | 1 d | 22 | 0% | 2 | n/a |
| `b` | fine | one sitting | 1 d | 22 | 0% | 2 | n/a |
| `hub` | fine | one sitting | 1 d | 20 | 0% | 3 | n/a |
| `tested` | fine | one sitting | 1 d | 27 | 0% | 0 | n/a |
| `dupes` | watch | one sitting | 1 d | 67 | 80.6% | 3 | n/a |
| `errs` | fine | one sitting | 1 d | 6 | 0% | 1 | n/a |
| `support` | fine | one sitting | 1 d | 8 | 0% | 0 | n/a |
| `trivial` | fine | one sitting | 1 d | 4 | 0% | 1 | n/a |

… 2 more packages; `astimate report --format json` has every row.
