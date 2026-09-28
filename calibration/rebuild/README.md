# Rebuild experiment

SPEC.md 11.2 measures the rebuild estimate's parameters by doing what the
estimate claims to predict: delete a package's implementation, have an agent
rebuild it against its tests and exported signatures, and record what that
cost. This directory defines that experiment and runs it. The fit that
turns the measurements into parameters builds on the runner's output.

| File | What it is |
| --- | --- |
| `rebuild.yaml` | The definition: one entry per package to rebuild. Generated; do not edit by hand. |
| `selection.md` | The acceptance record of the selection run: every package taken, with its oracle, and every candidate rejected, with the reason. Generated. |
| `*.go` | `go run ./calibration/rebuild`, with the `select`, `stub` and `run` subcommands, the definition's Go types and `Validate`, and the runner's row type `RunRow`. |
| `dryrun-agent.sh` | The stand-in agent for a dry run of the runner; it spends no requests. |

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

## The runner

`go run ./calibration/rebuild run` carries out SPEC.md 11.2 steps 2 and 3:
it hands each experiment to an agent and records what the rebuild cost.
Claude Code is the default agent.

**It spends live requests.** Every run is a Claude Code session of up to
`turn_cap` (100) turns on your account. It needs `claude` on `PATH` and a
logged-in session (`claude auth`). The runner refuses to start Claude Code,
as the default agent or from an `--agent` template that calls `claude`,
unless you pass `--live`; without it the command exits 2 and says so.
`--plan` lists the runs an invocation would start and the most turns they
may take, and runs nothing.

### One run

Runs go in run-major order: every experiment's first run, then every
second run, so a partial result covers every experiment. Each run:

1. Clones `repo` at `commit` with `CloneAt` into a fresh temporary
   directory. Every run gets its own clone, removed afterwards unless
   `--keep` is set, so no run sees another's edits. The agent's project
   state (Claude Code keys it by directory) is new each time too.
2. Applies the stub with `ApplyStub`, which refuses a tree whose hash is
   not `stub_sha256`.
3. Runs `go build ./...` and compiles the oracle's tests
   (`go test -count=1 -run '^$' <oracle.test...>`), which downloads every
   module they need before the agent starts. The agent's wall time holds
   no downloads, and a stub that does not build stops the run here.
4. Writes the prompt to a file beside the clone, outside the module. The
   prompt names the package, its directory and the module root. It asks
   the agent to make the oracle's tests pass, not to edit test files and
   not to touch other packages, and it gives the oracle command.
5. Runs the agent command through `sh -c`, with the module root as its
   working directory, the definition's `env` added to the environment and
   a wall-clock limit (`--timeout`, 60 minutes by default). When the limit
   is hit, the runner kills the agent's whole process group.
6. Runs the oracle from the module root with `env`:
   `go test -count=1 <oracle.test...>`, then `go build <oracle.build...>`.
   The row records each result separately.
7. Lists the changed files with `git status`. The row is `valid` only when
   no `_test.go` file changed and nothing outside the package directory
   did.

The default agent command, with `{model}` from `--model` and `{turn_cap}`
from the experiment:

```sh
claude -p "$(cat {prompt_file})" --output-format stream-json --verbose \
  --model {model} --max-turns {turn_cap} --permission-mode dontAsk \
  --allowedTools 'Read,Edit,Write,Glob,Grep,Bash(go build *),Bash(go test *),Bash(go vet *),Bash(go doc *),Bash(go list *),Bash(gofmt *)' \
  --disallowedTools 'Bash(*git *),Bash(*-toolexec*),Bash(*-exec*),Bash(*-vettool*),Bash(*-overlay*),Bash(*pkg/mod*)' \
  --safe-mode --strict-mcp-config --no-session-persistence
```

`dontAsk` denies every tool the allowed list leaves out. The agent can read
and edit files and run the go commands it needs to check its work. It
cannot run `git` (the clone's history holds the original implementation),
fetch from the web, start subagents or run other programs. The disallowed
list wins over the allowed one. It also refuses go commands that mention
git, run a program through `-toolexec`, `-exec` or `-vettool`, swap sources
with `-overlay`, or read the module cache, which can hold a released copy
of the package. `--safe-mode` and
`--strict-mcp-config` keep your CLAUDE.md, skills, plugins, hooks and MCP
servers out of the run, and the fresh clone directory gives each session
new project state. `stream-json` output carries every tool
call and ends with the result object that holds usage, cost, turns and
duration. `--model` defaults to `claude-fable-5-1`, the model Claude Code
was configured with when the runner was written. The row records the model
asked for and the models the session reported using.

`--agent` replaces the command with a template. Its placeholders are
`{root}` (module root), `{dir}` (package directory), `{package}`,
`{prompt_file}`, `{turn_cap}` and `{model}`. Each is replaced shell-quoted,
and `${...}` is left to the shell. The runner reads the agent's standard
output as Claude Code print-mode JSON: one result object, or stream-json
lines ending with one. Unknown fields are ignored. A measurement the
output does not carry is null in the row and named in `measured.missing`.
`--agent-name` names the agent in the rows and in the default output
directory (`claude-code`, or `custom` with `--agent`).

**What isolation does not cover.** The tool rules raise the bar, but they
are not a sandbox. `go test` runs the package's code, so an agent could
write code that shells out to `git` and recover the original. Each
transcript records every tool call, so a suspicious row can be audited;
`valid` only checks which files changed. Every session shares your home
directory: Claude Code's login and global settings are the same for every
run, and only project state is new. `--parallel` starts several sessions
against that shared state at once, so the default is one run at a time.
The `--live` gate reads the template: it refuses a template that runs
`claude` directly, but not a wrapper script that runs it for you. Such a
template spends requests without `--live`.

### Output

The output directory (`--out`, by default
`calibration/data/rebuild-<date>-<agent name>/`) holds:

| File | What it is |
| --- | --- |
| `runs.jsonl` | One row per run whose oracle completed, appended and synced as each run ends |
| `run.json` | The environment of the latest invocation (host, toolchain, astimate commit, definition, agent, settings), its failures, and totals over every row |
| `transcripts/<run>-<package>.jsonl` | Each agent's standard output (on by default; `--transcripts=false` turns it off). Not meant for committing: a live session writes megabytes |

### Resume

A run is keyed by `(package, run)`, with `run` from 1 to `--repeats`. On
start the runner reads `runs.jsonl` and skips every key it already holds
with `oracle.completed`. It first truncates a last line cut short by a
crash. A run that fails before the oracle gives a verdict writes no row:
a failed clone, a stub hash mismatch, a stub that does not build, an
agent that exits with an error before its time is up and prints no result
(a bad flag, no login), or an interrupt (Ctrl-C). The next invocation
retries it. After three such failures in a row the runner starts no more
runs, since the cause is likely to hit every run. An agent that timed out,
or that ended its session with an error result, still gets the oracle and
a row. Raising `--repeats` later adds only the new run indexes. The runner will not mix agents: when `runs.jsonl`
holds rows made with another agent name, template or model, or from
another stub, it exits 2 and asks for another `--out`. The exit code is 0
when every pending run wrote a row and 1 when some did not (they are
listed in `run.json` and on stderr).

### Row schema

Every row has every field below. A measurement the agent did not report
is `null`, never left out. Rows have `schema` 1. `TestRunRowSchema` checks
that a fully populated row carries every field of the Go type `RunRow`.

| Field | Meaning |
| --- | --- |
| `schema` | Row layout version, 1 |
| `module`, `package`, `dir`, `commit` | The experiment and its pin |
| `stub_sha256` | The stub the run started from |
| `run` | Run index, 1 to `--repeats`; with `package` the resume key |
| `turn_cap` | The experiment's turn cap |
| `agent.name`, `agent.template`, `agent.command`, `agent.model` | The agent, the template, the command as run, the model asked for |
| `agent.exit_code`, `agent.timed_out`, `agent.wall_ms`, `agent.stderr_tail` | How the command ended; `wall_ms` is measured by the runner; `exit_code` is -1 when it was killed |
| `estimate.tier`, `estimate.agent_passes`, `estimate.rebuild_tokens`, `estimate.human_days`, `estimate.has_tests` | The estimate before the run, from the definition |
| `metrics` | The package's `RawMetrics` at the pin, with the section 7.1 inputs |
| `config_version` | The configuration the estimate was made under |
| `go_version` | The toolchain the oracle ran with |
| `measured.input_tokens`, `measured.output_tokens`, `measured.cache_read_tokens`, `measured.cache_write_tokens` | Token counts, summed over every model of the session |
| `measured.token_source` | `modelUsage` (per-model totals) or `usage` (the result's usage object) |
| `measured.cost_usd` | The session's cost as the agent reported it |
| `measured.turns`, `measured.tool_calls` | Agentic turns; `tool_use` blocks in the stream (null for non-streamed output) |
| `measured.duration_ms`, `measured.api_duration_ms` | Session and API time as the agent reported them |
| `measured.session_id`, `measured.result_subtype`, `measured.is_error` | The result's identity and status (`success`, `error_max_turns`, `error_during_execution`) |
| `measured.turn_cap_hit` | True when the result says the cap stopped the session or the turns reached it |
| `measured.models` | Models the session reported using |
| `measured.missing` | The `measured` fields the output did not provide |
| `oracle.test`, `oracle.build` | The patterns the oracle ran |
| `oracle.tests_pass`, `oracle.build_passes`, `oracle.passed` | Whether the tests passed, whether importers still compile, and both |
| `oracle.test_tail`, `oracle.build_tail`, `oracle.wall_ms` | The end of each command's output and the oracle's wall time |
| `oracle.completed` | Both oracle commands ran to a verdict |
| `changes.test_files`, `changes.outside_package` | Test files, and paths outside the package, the agent changed |
| `valid` | Neither list is non-empty, so the verdict stands |
| `started_at`, `finished_at` | The run's bounds in UTC, clone and oracle included |

### Commands

From the repository root, with `claude` on `PATH` and logged in:

```sh
# What a full run would start, and the most turns it may take (spends nothing)
go run ./calibration/rebuild run --plan

# The full run: 34 experiments x 3 runs (live)
go run ./calibration/rebuild run --live

# One experiment (live), for a first look at the cost
go run ./calibration/rebuild run --live --only google.golang.org/grpc/resolver/manual --repeats 1

# Resume after an interrupt or failures: the same command again, with the
# same --out when the date has changed since the first invocation
go run ./calibration/rebuild run --live --out calibration/data/rebuild-2026-09-28-claude-code

# Another model: pass --model and give the runs their own directory
go run ./calibration/rebuild run --live --model claude-opus-5-5 --agent-name claude-code-opus
```

The output directory's default name has the date in it, so a resume on a
later day needs `--out`. `--parallel N` runs N experiments at a time. It
is faster, but the runs then compete for CPU during `go test`, and their
wall times are less comparable.

**What it costs.** The upper bound on requests is 34 experiments x 3 runs
x 100 turns = 10,200 agent turns. Each turn is one model request, and the
tool list allows no subagents. Real sessions are expected to stop well
below the cap. Wall time has not been
measured yet. Setup and the oracle take about 10 to 60 seconds a run (the
grpc dry run below took 10 seconds), and the agent's session dominates.
At 5 to 20 minutes a session, 102 sequential runs take roughly 8 to 34
hours; the 60-minute timeout bounds the worst case at about 105 hours.
Try one experiment first and read its row's `agent.wall_ms` and
`measured.cost_usd`.

### Dry run

`dryrun-agent.sh` is a stand-in agent that spends nothing. It restores the
package's original implementation from the clone's git history
(`git checkout HEAD -- <dir>`) and prints the stream-json lines Claude
Code would, with zero tokens, one turn and one tool call. The oracle
passes, and every field of the row is filled. Use it to check the runner
end to end, with network for the clone:

```sh
go run ./calibration/rebuild run --agent "sh $PWD/calibration/rebuild/dryrun-agent.sh {dir}" \
  --only google.golang.org/grpc/resolver/manual --repeats 1 --out /tmp/rebuild-dryrun
```

The row that dry run wrote on 2026-09-28 is below, pretty-printed, with the
checkout path in `agent.template` and `agent.command` shortened to
`/path/to/astimate`. Dry-run data is not committed. Running the same
command again with `--repeats 2` skipped run 1 and added run 2.

```json
{
  "schema": 1,
  "module": "google.golang.org/grpc",
  "package": "google.golang.org/grpc/resolver/manual",
  "dir": "resolver/manual",
  "commit": "acccf8cd101ae1eb33385f27e12740b3edd730f0",
  "stub_sha256": "e19118ce2ef4305401890e213e80be17061d99bbdb7df1b1b3f3fabb65d971c1",
  "run": 1,
  "turn_cap": 100,
  "agent": {
    "name": "custom",
    "template": "sh /path/to/astimate/calibration/rebuild/dryrun-agent.sh {dir}",
    "command": "sh /path/to/astimate/calibration/rebuild/dryrun-agent.sh resolver/manual",
    "model": "claude-fable-5-1",
    "exit_code": 0,
    "timed_out": false,
    "wall_ms": 39,
    "stderr_tail": ""
  },
  "estimate": {
    "tier": "ONE_PASS",
    "agent_passes": 0.2,
    "rebuild_tokens": 5431,
    "human_days": 3.2,
    "has_tests": true
  },
  "metrics": {
    "files": 1,
    "sloc": 65,
    "largest_file_sloc": 65,
    "tokens_est": 1360,
    "tokens_est_with_tests": 1871,
    "internal_imports": 1,
    "external_imports": 0,
    "stdlib_imports": 1,
    "fan_in": 3,
    "fan_in_tests": 21,
    "exported_symbols": 9,
    "globals": 0,
    "init_funcs": 0,
    "max_nesting": 1,
    "cognitive_total": 3,
    "cognitive_p90": 1,
    "func_count": 8,
    "dup_blocks": 0,
    "duplication_pct": 0,
    "test_files": 1,
    "test_funcs": 1,
    "has_tests": true,
    "untested_exports": 4,
    "dup_blocks_cross_pkg": 0,
    "instability": 0.25,
    "abstractness": 0,
    "main_sequence_distance": 0.75,
    "uses_cgo": false,
    "uses_reflect": false,
    "generated_files": 0,
    "tokens_est_generated": 0,
    "coverage_pct": null,
    "changed_func_cognitive_max": null
  },
  "config_version": "thresholds-2026-09-27",
  "go_version": "go1.27.1",
  "measured": {
    "input_tokens": 0,
    "output_tokens": 0,
    "cache_read_tokens": 0,
    "cache_write_tokens": 0,
    "token_source": "modelUsage",
    "cost_usd": 0,
    "turns": 1,
    "tool_calls": 1,
    "duration_ms": 0,
    "api_duration_ms": 0,
    "session_id": "dry-run",
    "result_subtype": "success",
    "is_error": false,
    "turn_cap_hit": false,
    "models": [
      "dry-run"
    ],
    "missing": []
  },
  "oracle": {
    "test": [
      "./resolver/manual"
    ],
    "build": [
      "./..."
    ],
    "tests_pass": true,
    "build_passes": true,
    "passed": true,
    "test_tail": "ok google.golang.org/grpc/resolver/manual",
    "build_tail": "",
    "wall_ms": 3029,
    "completed": true
  },
  "changes": {
    "test_files": [],
    "outside_package": []
  },
  "valid": true,
  "started_at": "2026-09-28T05:56:37.43518Z",
  "finished_at": "2026-09-28T05:56:47.750124Z"
}
```

## What the fit takes from here

Each row carries the experiment's `metrics` and the pre-run estimate, so
the fit (SPEC.md 11.2 steps 4 and 5) regresses measured tokens and pass
rate on the section 7.1 inputs from `runs.jsonl` alone, without reloading
the corpus data. Rows with `valid: false` broke the prompt's rules, and
their oracle verdict does not count.
