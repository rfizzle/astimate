# Rebuild experiment

SPEC.md 11.2 measures the rebuild estimate's parameters by doing what the
estimate claims to predict: delete a package's implementation, have an agent
rebuild it against its tests and exported signatures, and record what that
cost. This directory defines that experiment. The runner that spends live
requests and the fit that turns its measurements into parameters build on
it.

| File | What it is |
| --- | --- |
| `rebuild.yaml` | The definition: one entry per package to rebuild. Generated; do not edit by hand. |
| `selection.md` | The acceptance record of the selection run: every package taken, with its oracle, and every candidate rejected, with the reason. Generated. |
| `*.go` | `go run ./calibration/rebuild`, with the `select` and `stub` subcommands, and the definition's Go types and `Validate`. |

## The definition

`rebuild.yaml` has a header and an `experiments` list. The header records
where the data came from and how it was checked:

| Field | Meaning |
| --- | --- |
| `source` | The collector's `packages.jsonl` the metrics and estimates were copied from |
| `config_version` | The configuration the estimates were made under (from the collector's `run.json`) |
| `go_version` | The toolchain the selection was verified with; the stub hashes depend on it |
| `env` | Environment for every go command of the experiment: `CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off` |
| `selection` | The parameters of the selection rule below |

Each experiment carries:

| Field | Meaning |
| --- | --- |
| `module`, `repo`, `commit` | The module path and its clone URL and pin from `calibration/corpus.yaml` |
| `package`, `dir` | The import path and its module-relative directory |
| `stub` | The stub strategy; `signatures` is the only one |
| `stub_sha256` | Hash of the stubbed files, so a run can prove it starts from the verified tree |
| `oracle` | `test` and `build` package patterns; the rebuild passes when `go test <test...> && go build <build...>` passes from the module root |
| `turn_cap` | The most agent turns a run may take (100 for every experiment) |
| `has_tests`, `tier`, `agent_passes`, `rebuild_tokens`, `human_days` | The estimate before the run, at the pin |
| `metrics` | The package's `RawMetrics` at the pin, under the report schema's field names |

`LoadDefinition` validates the file (`Definition.Validate`): at least 30
experiments, tested and untested packages in every tier, no package twice, a
full commit hash, a known stub strategy and a well-formed hash, an oracle of
the right shape, a positive turn cap, and valid metrics without cgo.
`go test ./calibration/rebuild` also checks that every experiment's module,
commit, metrics and estimate equal its row in `source`.

## The stub strategy

`signatures`: for every non-test `.go` file in the package directory, each
function and method body, `init` included, is replaced by
`panic("not implemented")`. Everything outside a body is kept byte for byte:
the package clause, build constraints (`//go:build`), types, constants,
variables with their initializers, signatures and doc comments. Comments
inside a body go with it. Declarations without a body (assembly or
`//go:linkname`) are kept. The file is then formatted with
`golang.org/x/tools/imports`, which also removes the imports no remaining
code uses. Files of every build configuration are stubbed, not only the
host's. Test files are untouched: they are the specification.

Two consequences are deliberate:

- **Variable initializers stay.** A package-level `var x = f()` keeps its
  initializer, so a stubbed `f` panics at program start and every test of
  the package fails. The oracle is compile-then-test, so this is only an
  earlier failure. Function literals inside a package-level initializer
  (a table of handlers, say) keep their bodies; that code is part of what
  the agent is given.
- **`init` is stubbed**, so a package with an `init` panics at start too.

The stub is deterministic: it depends only on the files' contents, the
toolchain's `go/printer` and the `golang.org/x/tools` version in this
repository's `go.mod`, so a runner stubs with `go run ./calibration/rebuild`
from the same astimate commit. `StubPackage` returns the files sorted by name, `TreeHash`
hashes them, and the selection stubs every package twice and compares.
`ApplyStub` refuses to write a stub whose hash differs from the
definition's `stub_sha256`.

By hand, on a clone at the pin:

```sh
go run ./calibration/rebuild stub --root <clone> --dir <dir> --sha256 <stub_sha256>
```

## The oracle

A package with tests (`has_tests`, so `test_funcs > 0`) is checked against
its own tests: `oracle.test` is `./<dir>`. Sub-packages are not included:
their tests are not this package's specification, and for the root package
`./...` would be the whole module.

A package without tests has no specification of its own, and its stub passes
`go test ./<dir>` trivially. Its oracle is the tests of every package in
the module that imports it, from any file, and has test files: consumers
that must keep working (the contract term of SPEC.md 7.1). An untested
package with no such importer is not eligible.

`oracle.build` is always `./...`, which compiles every importer in the module
and so answers "do importers still compile" for the runner.

## The selection rule

The selection is regenerated from the collector's data with:

```sh
go run ./calibration/rebuild select --work <scratch directory>
```

It needs network to clone the modules at their pins (the same shallow fetch
`calibration/collect` uses) and their dependencies, and takes a while: each
candidate's oracle runs twice.

1. **Candidates.** Every row of `packages.jsonl` from a cloned corpus module
   whose `uses_cgo` is false, whose `generated_files` is 0 (a rebuild would
   rerun the generator), with at least one function, with `agent_passes` at
   most `max_agent_passes` (10), and, when it has no tests, with a non-zero
   `fan_in + fan_in_tests`. Each estimate is recomputed from the metrics
   under the embedded default parameters and must round to the row's
   `agent_passes`, so the data and the parameters agree.
2. **Strata.** Candidates are split by tier (SPEC.md 7.4) and by
   `has_tests` into six strata, each sorted by `rebuild_tokens`, then import
   path.
3. **Slots.** Each stratum has `per_stratum` (7) slots. Slot `k` of a
   stratum with `n` candidates starts at index `floor(k * n / 7)`, so the
   slots spread across the stratum's size range, and takes the first
   candidate from there on that is not taken, whose module has fewer than
   `max_per_module` (3) packages taken, and that passes the checks below.
   Strata are filled in the order ONE_PASS, FEW_PASSES, PARTITION, tested
   before untested.
4. **Checks**, at the pin, with `env`:
   - the module builds (`go build ./...`);
   - the package has no assembly, C or object files, which a Go stub cannot
     replace;
   - the oracle's tests pass on the original tree;
   - stubbing twice gives identical files;
   - the stubbed module builds;
   - the oracle's tests build and fail on the stub, with the stub's
     `not implemented` panic in the output (a failure that never reaches
     the stub proves nothing).

   A candidate failing any check is recorded in `selection.md` with the
   reason, and the slot moves on to the next candidate.

The result depends only on the data, the pins, the toolchain and the tests'
outcomes; a flaky test can change a verdict, which `selection.md` would
show.

**Why not the standard library.** A std package's oracle would need a Go
source tree at exactly the measured release with a toolchain built from it,
and stubbing a package the test harness itself imports (`strings`, `fmt`,
`sync`) breaks every test binary, not only its own. The cloned modules give
enough packages in every tier, so the first selection leaves std out.

**Why a cap on passes.** PARTITION runs from 3.1 to over 700 passes in the
corpus. Packages above 10 passes cost the most to run and add little to
fitting the exponent past the knee; they can be added once the first runs
show the cost.

## What the runner and the fit take from here

The runner (SPEC.md 11.2 steps 2 and 3) loads the file with
`LoadDefinition`, which validates it, and for each experiment and run:
clones `repo` at `commit` with `CloneAt` into a fresh directory, applies
`ApplyStub` (which checks `stub_sha256`), gives the agent the package
directory with its tests and signatures and `turn_cap` turns, and then runs
`go test <oracle.test...>` and `go build <oracle.build...>` from the module
root with `env`, as two commands (`oracle.Command()` renders them for logs). The experiment's
`package` and a run index identify a run for resume. Each result row attaches
the experiment's `metrics` and pre-run `agent_passes`, `rebuild_tokens`,
`human_days` and `tier`, so the fit (steps 4 and 5) can regress measured
tokens and pass rate on the section 7.1 inputs without reloading the corpus
data.
