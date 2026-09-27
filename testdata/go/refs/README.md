# refs fixture

Module `example.com/refs` exercises `untested_exports` (SPEC.md 6.4): which
test references mark an exported func or method as tested. The tests assert
these counts directly; there are no golden files.

## refs

| Declaration | How a test refers to it | Result |
| --- | --- | --- |
| `Direct` | called directly (`refs_test.go`, package `refs`) | tested |
| `Counter.Inc` | method value `c.Inc`, then called (`refs_test.go`) | tested |
| `Square.Area` | called through a `Shape` value (`external_test.go`, package `refs_test`) | tested |
| `Inner.Hello` | promoted, called on an `Outer` value (`refs_test.go`) | tested |
| `Wrapper` | not referenced; carries `//astimate:untested` | excluded |
| `Never` | called only from `Wrapper`, a non-test file | untested |

Expected: `untested_exports=1`, names `[Never]`, excluded `[Wrapper]`.

## notests

Three exported funcs, `One`, `Two` and `Three`, one unexported func and no
test files. Expected: `untested_exports=3`, names `[One Three Two]`.
