## Fixture has no package too big to rebuild in one go.

12 packages, about 2 weeks of one engineer's time to rebuild from their tests. 11 are fine, 1 is close to a limit, and 0 need a split.

### Start here

1. `hidden` (fine): 2 d to rebuild
   It is inside every limit, and it has no tests at all.
   Do this: Nothing. Leave it alone.

2. `immut` (fine): 2 d to rebuild
   It is inside every limit, and it has no tests at all.
   Do this: Nothing. Leave it alone.

3. `a` (fine): 1 d to rebuild
   It is inside every limit, and it has no tests at all.
   Do this: Nothing. Leave it alone.

### Every package

By rebuild days; * marks a package over its size limit.

| Package | Band | Days | Lines | Copied | Untested | Changes |
|---|---|---|---|---|---|---|
| `hidden` | fine | 2 d | 32 | 0% | 1 | n/a |
| `immut` | fine | 2 d | 31 | 0% | 3 | n/a |
| `a` | fine | 1 d | 22 | 0% | 2 | n/a |
| `b` | fine | 1 d | 22 | 0% | 2 | n/a |
| `hub` | fine | 1 d | 20 | 0% | 3 | n/a |
| `tested` | fine | 1 d | 27 | 0% | 0 | n/a |
| `dupes` | watch | 1 d | 67 | 80.6% | 3 | n/a |
| `support` | fine | 1 d | 8 | 0% | 0 | n/a |
| `errs` | fine | 1 d | 6 | 0% | 1 | n/a |
| `trivial` | fine | 1 d | 4 | 0% | 1 | n/a |

… 2 more packages; `astimate report --format json` has every row.
