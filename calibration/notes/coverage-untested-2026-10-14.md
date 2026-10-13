# untested_exports under --coverage: the field cases

Date: 2026-10-14. Machine: linux/arm64. Toolchain: Go 1.27.1.
Targets: goKMS (`/Users/copierson/Projects/goKMS`, module `gokms`)
`internal/kms` and `internal/auth`, and mark (`github.com/kovetskiy/mark/v16`)
`confluence`, at their working trees, read-only. No commit was replayed and
nothing was cloned or downloaded.

## Question

After the static refinements of SPEC.md 6.4, the exports `untested_exports`
still counts in the field are mostly ones no test names because the tests
reach them another way: goKMS drives `kms.Service` and its connect
`Handler` through an end-to-end suite in a test-only `test/` directory
over real HTTP, and mark drives `confluence` through pipeline tests. With
`--coverage` each test run now writes a `-coverpkg=./...` profile, and an
export whose body holds a statement any passing run executed counts as
tested (`untested_exports_covered`). For a package with untested exports
the tests of every module package whose test binary links it run too, a
test-only directory included. How many of the counted exports does the
profile settle?

## Method

This change's binary, built from the worktree, run as

    GOPROXY=off GOFLAGS=-mod=readonly astimate assess <dir> --coverage --coverage-timeout 10m --json

in each module. Both modules' tests ran offline from the module cache,
and no measured package logged a coverage warning; the runs of the
linking packages are not reported, so one that failed would only leave
its statements out of the merge. The exports left were listed from the extractor's
details (`UntestedExports` less `UntestedCovered`) with a throwaway test
that was not kept. Wall time is for a warm build cache, the whole
`assess` invocation, with and without `--coverage`.

## Results

| Package | `test_files` | `untested_exports` | `untested_exports_covered` | `coverage_pct` | Time static | Time `--coverage` |
| --- | --- | --- | --- | --- | --- | --- |
| goKMS `internal/kms` | 0 | 45 | 8 | null | 0.17 s | 0.92 s |
| goKMS `internal/auth` | 0 | 6 | 0 | null | 0.16 s | 0.60 s |
| mark `confluence` | 1 | 27 | 20 | 24 | 0.21 s | 0.81 s |

`coverage_pct` stays null for the two goKMS packages: they have no test
files of their own, and `coverage_pct` counts only a package's own tests.
Their covered counts come from the runs of the packages whose tests link
them, `gokms/test` (test files only) among them.

goKMS `internal/kms`: the profile settles 37 of 45 (82%). The 8 left are
`Handler.PutGrant`, `Handler.RevokeGrant`, `Handler.Rewrap`,
`Service.PutGrant`, `Service.RevokeGrant`, `Service.Rewrap`,
`Service.RunRotationSchedule` and `Verify`: grant management, rewrap,
the rotation scheduler and signature verification, which the end-to-end
suite does not exercise. They are real gaps, so the count is far below
45 and what remains is the behaviour no test runs.

goKMS `internal/auth`: all 6 are settled by the end-to-end suite.

mark `confluence`: 7 of 27 settled (26%). The 20 left are
`API.AddPageLabels`, `API.CreateAttachment`, `API.CreatePage`,
`API.CreatePageV2`, `API.DeletePageLabel`, `API.FindPage`,
`API.FindRootPage`, `API.GetAttachments`, `API.GetCurrentUser`,
`API.GetFolderByID`, `API.GetPageBodyByID`, `API.GetPageLabels`,
`API.GetUserByName`, `API.MovePageV2`, `API.RestrictPageUpdates`,
`API.RestrictPageUpdatesCloud`, `API.RestrictPageUpdatesServer`,
`API.UpdateAttachment`, `API.UpdatePage` and `tracer.Printf`: the calls to
a Confluence server, which mark's tests do not make. The pipeline goldens
exercise rendering, not the API client.

## Decision

`untested_exports_covered` is reported with `--coverage`, and an
`untested_exports` rule may take `source: coverage` to judge it (SPEC.md
8.1). The embedded default keeps the rule static and shows the option
commented out: the run executes the module's tests, which a hook or CI step
must opt into, and the replay corpora were not run under it, so the
threshold is unchanged. The cost on these packages is under a second with
a warm cache; a package that many test packages link runs all of their
tests, bounded by `--coverage-timeout`.
