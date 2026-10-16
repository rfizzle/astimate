## Fixture has no package that needs a split.

12 packages; 0 need more than one agent sitting to rebuild from their tests. In measured agent rebuilds, a package of one sitting was rebuilt in 97% of runs and one of a few sittings in 78%. 11 are fine, 1 needs watching, and 0 need a split. There is no change history to read, so the packages to start with are ranked by rebuild time alone.

**2 weeks** for one engineer · _Engineer time runs, per package, from the lower to the higher of COCOMO 81 and COCOMO II, rebuild profile; not a measurement; agent sittings are measured. rebuild.cocomo and rebuild.cocomo_a set it._

### Start here

1. `dupes` (watch): one sitting
   It is inside every limit, and 80.6% of its lines are copies of each other.
   Do this: Extract the copied code next time you work here; nothing needs a split yet.

### Every package

Costliest to rebuild first; * marks a package over its size limit.

| Package | Band | Sittings | Lines | Copied | Untested | Changes |
|---|---|---|---|---|---|---|
| `hidden` | fine | one sitting | 32 | 0% | 1 | n/a |
| `immut` | fine | one sitting | 31 | 0% | 3 | n/a |
| `a` | fine | one sitting | 22 | 0% | 2 | n/a |
| `b` | fine | one sitting | 22 | 0% | 2 | n/a |
| `hub` | fine | one sitting | 20 | 0% | 3 | n/a |
| `tested` | fine | one sitting | 27 | 0% | 0 | n/a |
| `dupes` | watch | one sitting | 67 | 80.6% | 3 | n/a |
| `errs` | fine | one sitting | 6 | 0% | 1 | n/a |
| `support` | fine | one sitting | 8 | 0% | 0 | n/a |
| `trivial` | fine | one sitting | 4 | 0% | 1 | n/a |

… 2 more packages; `astimate report --format json` has every row.
