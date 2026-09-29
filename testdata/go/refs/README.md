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
| `Box.Get` | method of generic `Box[T]`, called only through a `Getter[string]` holding a `Box[string]` (`external_test.go`); checked against that instantiation | tested |
| `Wrapper` | not referenced; carries `//astimate:untested` | excluded |
| `Never` | called only from `Wrapper`, a non-test file | untested |

Expected: `untested_exports=1`, names `[Never]`, excluded `[Wrapper]`.

## notests

Three exported funcs, `One`, `Two` and `Three`, one unexported func and no
test files. Expected: `untested_exports=3`, names `[One Three Two]`.

## lib, user and unseen

`lib` has no test files; `user`'s test (`user_test.go`, package `user`)
calls `lib.Lib` and calls `lib.Impl.Area` through a `refs.Shape` value.
References from another package's test files count (SPEC.md 6.4), so
`lib` expects `untested_exports=1`, names `[Unused]`; counting its own
tests only, it would be 3, `[Impl.Area Lib Unused]`.

`unseen.Tri` implements `refs.Shape` too, but no test package imports
`unseen`, directly or through another package, so no test binary holds a
`Tri` and the `Shape.Area` calls do not reach it: `untested_exports=1`,
names `[Tri.Area]`.
