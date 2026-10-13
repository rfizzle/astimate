# rules fixture

Module `example.com/rules` exercises the option-func, held-interface and
test-support rules of `untested_exports` (SPEC.md 6.4). The tests assert
these counts directly; there are no golden files.

| Package | Result |
| --- | --- |
| `opts` | no tests. `WithTimeout` and `Builder.WithName` only set a field of their parameter and are left out (`option_funcs`); `WithRetry` validates and returns an error, and `Apply` is no option, so both count |
| `rec` | `streamRecording.Emit` implements `Recorder`, which `rec`'s test holds without calling `Emit`: left out (`interface_methods`) |
| `quiet` | the same shape, but no test holds `Sink`: `fileSink.Emit` counts, with `New` |
| `support` | imported only by `consumer`'s test, so a test-support package: `Fake.Do`, called by the non-test helper `Run`, reads as tested (`test_support`) |
| `shared` | imported by `consumer`'s non-test code, so not test support: `Thing.Go` counts |
