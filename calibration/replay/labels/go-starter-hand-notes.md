# Hand labels for go-starter: causes and evidence

Per-finding causes and `file:line` evidence behind `calibration/replay/labels/go-starter-hand.yaml`. The labels file keeps one verdict per commit and a reason that starts with the cause; this file keeps every finding of a failed commit with its own cause. Every label is a draft (`provenance: proposed`) awaiting the user's confirmation. The taxonomy and the standard of evidence are those of the 2026-10-13 field adjudication; the counts and the least certain labels are in `calibration/notes/hand-labels-2026-10-14.md`.

## Failed commits (13)

### 74f772631 `allow`: refactor(architecture): enforce module platform boundaries

An architecture move: the domain dup is an interface method set in a new package and the user-module dup is the pre-existing ErrNoRows error chain carried over by the rename, so no new copy-paste was introduced. Evidence: `internal/domain/user.go:42`, `internal/modules/user/repository.go:52`, `internal/modules/user/repository.go:65`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/domain` | `dup_blocks` | no-baseline | New leaf package; the cited block is the UserRepository interface method set (internal/domain/user.go:42-47), which restates the method signatures already on modules/user.Repository. Nothing to extract. |
| `internal/modules/user` | `dup_blocks` | other: moved duplicates | Package renamed from internal/user; ByID and ByEmail's `row, err := ...; if errors.Is(err, pgx.ErrNoRows) {return ErrNotFound}` chain (internal/modules/user/repository.go:52-60 vs 65-73) existed before the move; the diff only swapped User/storage for domain types. |

### bd1e892c7 `block`: feat(database): add production-safe migration command

The new migrate command was built by pasting seed's command preamble, flag block and pool/open/ping setup; a reviewer would ask for shared helpers, and the author added them a few commits later (186d1c4, eab9fe9). Evidence: `cmd/migrate.go:65`, `cmd/migrate.go:119`, `cmd/migrate.go:138`, `cmd/seed.go:57`, `cmd/seed.go:94`, `cmd/seed.go:117`, `internal/platform/config/validate.go:30`. Confidence medium: Some of the five new cmd blocks are flag-registration boilerplate; the block rests on the pool-setup and RunE preamble copies, which the later refactors confirm.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `cmd` | `dup_blocks` | real | New cmd/migrate.go copies seed's RunE preamble (resolve/validate/newLogger/NotifyContext, migrate.go:65-81 vs seed.go:57-77), the full database+logging flag set (migrate.go:119-130 vs seed.go:94-111) and the NewPool/Open/Ping setup (migrate.go:138-161 vs seed.go:117-141). The maintainer later extracted exactly these in 186d1c4 (cmd/utils.go addDatabaseFlags) and eab9fe9 (shared pool setup). |
| `cmd` | `globals` | other: cobra command var | migrate.go:33 defaultMigrateDependencies is a never-written dependency literal and migrate.go:39 migrateCmd is the usual cobra command var. |
| `cmd` | `init_funcs` | other: cobra AddCommand init | migrate.go:193-195 init only calls rootCmd.AddCommand(migrateCmd), the pattern every command in cmd uses. |
| `internal/platform/config` | `dup_blocks` | boilerplate-dup | The only code change is adding ValidationScopeMigrate to the const block and the case list (internal/platform/config/validate.go:18-20, :30); the rise comes from the existing `[]struct{name,value}` field tables (validate.go:68-80, :123-133), not a new copy. |

### e9fbc1d3a `allow`: feat(health): add dependency-aware readiness

The only flagged export is a trivial boolean wrapper over the tested Check, and integration tests call it directly. Evidence: `internal/platform/health/health.go:87`, `internal/platform/health/health_integration_test.go:62`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/platform/health` | `untested_exports` | covered-export | Aggregator.Ready (internal/platform/health/health.go:87) is a one-line `a.Check(ctx) == nil` wrapper; Check is unit tested and Ready itself is called in health_integration_test.go:62,68 (build tag integration). |

### 5d9bb0069 `allow`: test(app): cover service lifecycle end to end

A test-focused commit; both flagged exports are an accessor and an unexported-type listener method that tests do run. Evidence: `internal/platform/http/server/webserver.go:268`, `internal/platform/http/server/webserver_test.go:62`, `internal/app/app_integration_test.go:128`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/app` | `untested_exports` | covered-export | The new export is App.Started (internal/app/app.go:306), a channel accessor exercised by app_integration_test.go:94,128 (build tag integration); the cited app.go:331 Logger accessor is old. |
| `internal/platform/http/server` | `untested_exports` | covered-export | webserver.go:268 is Accept on the unexported acceptNotificationListener, called by http.Server.Serve in every Run test; Webserver.Started is tested at webserver_test.go:62. |

### 72f02c0de `allow`: feat(auth): add credential authentication service

The new global is a read-only precomputed hash and the new repository method has integration tests the gate does not see. Evidence: `internal/modules/credential/service.go:32`, `internal/modules/user/repository.go:80`, `internal/modules/user/repository_integration_test.go:43`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/modules/credential` | `globals` | immutable-global | unknownAccountHash (internal/modules/credential/service.go:32) is a bcrypt hash computed once at init and only read; never written. |
| `internal/modules/user` | `untested_exports` | covered-export | Repository.CredentialByEmail (internal/modules/user/repository.go:80) is tested in repository_integration_test.go:43,62 against Postgres (build tag integration). |

### 09783ecbb `allow`: feat(api): add login and token flow

Crossing the import ceiling by one in the composition root to wire a new module is expected, not a structural problem. Evidence: `internal/app/app.go:19`, `internal/app/app.go:209`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/app` | `internal_imports` | other: composition root wiring | The +1 is internal/modules/credential (internal/app/app.go:19), wired into the login flow at app.go:209; internal/app is the composition root, so a new module means a new import. |

### f818863ca `allow`: fix(seed): prevent default production credentials

A security fix that added seed flags; the duplicate is cobra flag registration nobody would extract. Evidence: `cmd/seed.go:127`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `cmd` | `dup_blocks` | boilerplate-dup | The new block is the four `command.Flags().String("seed.*", ...)` registrations (cmd/seed.go:127-131) beside the existing environment/database flags; cobra flag registration. |

### 96d2cffa9 `allow`: refactor(ratelimit): replace placeholder implementations

The ratelimiter rewrite removed far more than it added; the extra cross-package block is the standard HTTP middleware closure shell. Evidence: `internal/platform/ratelimiter/middleware.go:16`, `internal/auth/audit.go:12`. Confidence low: I could not pin which new token run produced the +1; the 546-line ratelimit.go rewrite might hold a copy of something elsewhere, but the cited block points at the middleware shell.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `<module>` | `dup_blocks_cross_pkg` | boilerplate-dup | Only middleware.go changed outside ratelimit.go; RateLimitMiddleware's `func(next http.Handler) http.Handler { return http.HandlerFunc(func(w, r) {` shell (internal/platform/ratelimiter/middleware.go:16-18) now matches auth.AuditMiddleware's (internal/auth/audit.go:12-14), the cited block. The getClientIP copy (middleware.go:41 vs audit.go:77) is old. |

### 93137e71f `allow`: feat(ratelimit): protect login with trusted client identity

A feature that adds flags, config validation and wiring; every duplicate is flag/option/validation-table scaffolding and the import rise is the composition root wiring a new middleware. Evidence: `cmd/serve.go:175`, `internal/app/app.go:308`, `internal/app/app.go:25`, `internal/platform/config/validate.go:138`. Confidence medium: The config validate table is now in three places; a strict reviewer could ask for a shared checkFields helper, though each copy is about five lines.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `cmd` | `dup_blocks` | boilerplate-dup | Five `command.Flags().Int/StringSlice("rate-limit.*")` registrations (cmd/serve.go:175-180); cobra flag registration. |
| `internal/app` | `dup_blocks` | boilerplate-dup | internal/app/app.go:308 is the httpserver.WithX(time.Duration(cfg...)*time.Second) option list; the new rate-limit config adds more of the same `time.Duration(x)*time.Second` mapping (app.go:270-277). |
| `internal/app` | `internal_imports` | other: composition root wiring | +2 imports (platform/http/middleware, platform/ratelimiter at internal/app/app.go:25,28) to wire the login limiter in the composition root. |
| `internal/platform/config` | `dup_blocks` | boilerplate-dup | validateRateLimit (internal/platform/config/validate.go:138-163) repeats the `[]struct{name,value}` table plus range/append check used in validateServe and validateDatabase; a table-driven validation idiom. |

### 4f11de727 `allow`: fix(templateinit): prevent cascading binary-name replacement

Nesting 2->3 from a small single-pass replacement loop is ordinary code, not a maintainability regression. Evidence: `internal/templateinit/initializer.go:407`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/templateinit` | `max_nesting` | tiny-ratchet | applyReplacements (internal/templateinit/initializer.go:407) is a for/for/if longest-match scan, depth 3, in a 25-line function with tests; ordinary code that fixes a real bug. |

### eab9fe958 `allow`: refactor(cmd): share database pool setup across seed, migrate, and app

A deduplication refactor; the flagged export is a trivial struct mapping. Evidence: `internal/platform/database/postgres/pool.go:41`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/platform/database/postgres` | `untested_exports` | covered-export | PoolConfigFromDatabase (internal/platform/database/postgres/pool.go:41) is a pure field-to-field mapping with no logic, called by every composition root (cmd/utils.go, internal/app/app.go) that integration tests drive. |

### ea8a388d1 `allow`: refactor(server): replace the webserver options with a Config struct

p90 rose only because 15 trivial option functions were deleted; the refactor made the package simpler. Evidence: `internal/platform/http/server/webserver.go:127`, `internal/platform/http/server/webserver.go:192`.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `internal/platform/http/server` | `cognitive_p90` | tiny-ratchet | func count fell from 21 to 6 when the WithX option funcs were deleted (internal/platform/http/server/webserver.go:89-271 now has applyDefaults, tlsEnabled, New, Run, Started, Accept), so nearest-rank p90 became the max (New/Run at about 11). Matches the field note's recorded verdict. |

### ef5b99f79 `allow`: refactor(seed): require seed.password and take the logger as a parameter

The commit only deleted code; the new duplicates are dependency-struct declarations that became identical once a field was removed. Evidence: `cmd/seed.go:20`, `cmd/seed.go:26`. Confidence medium: I inferred which blocks the gate paired from the diff; the only additions are signature tweaks, so a real new copy is unlikely.

| Package | Rule | Cause | Note |
| --- | --- | --- | --- |
| `cmd` | `dup_blocks` | boilerplate-dup | Deletion-only change to cmd/seed.go: dropping generatePassword made seedDependencies and defaultSeedDependencies (cmd/seed.go:20-30) token-identical to migrate's dependency struct/literal; duplication_pct also rose from the smaller denominator. |

## Sampled passed commits (14)

| Commit | Verdict | Subject | Why | Evidence |
| --- | --- | --- | --- | --- |
| 3d226d162 | `allow` | refactor(storage): remove unused generic database API | Deletion of the unused generic storage API (about 1,100 lines removed, a few call-site edits in internal/app/app.go); nothing to catch. | `internal/storage/postgres/postgres.go:1`, `internal/storage/errors.go:1` |
| 6c9f0f73f | `allow` | refactor(platform): internalize config and observability | Six added lines in the config struct as part of a move; no logic, no copy. | `internal/platform/config/config.go:1` |
| bef19d605 | `allow` | test(integration): add postgres test harness | Adds a test harness behind //go:build integration (postgrestest/harness.go:1) and an export_test.go; test-support code only. | `internal/platform/database/postgrestest/harness.go:1` |
| aaee4660b | `allow` | fix(auth): equalize credential verification cost | Moves the dummy hash into NewService with a cost check and merges Authenticate's branches into one comparison path; simpler, removes a package global, and the tests were updated alongside. | `internal/modules/credential/service.go:56`, `internal/modules/credential/service_test.go:1` |
| 46c00d959 | `allow` | refactor(auth): delete dead subsystem and enable unused linter | Deletes the dead auth subsystem and middleware chain (more than 1,400 lines removed, 60 added, mostly test rewrites); nothing to catch. | `internal/auth/auth.go:1`, `internal/platform/http/middleware/chain.go:1` |
| bd8e156c2 | `allow` | test(integration): share one Postgres container per package | Changes only integration tests and the build-tagged test harness so packages share one container. | `internal/platform/database/postgrestest/harness.go:1` |
| c6a211328 | `allow` | build: stop vendoring dependencies | Build change: one removed line in templateinit and a tools.go import swap. | `tools.go:1`, `internal/templateinit/initializer.go:1` |
| 74f6a0bf9 | `allow` | refactor(auth): remove the request-based Validator surface | Removes the request-based Validator surface (about 40 lines out, 5 in); deletion only. | `internal/auth/jwt.go:1`, `internal/auth/interfaces.go:1` |
| c856d535b | `allow` | refactor(postgres): remove the unused URI, connection, and close surface | Removes the unused URI/connection/close surface and its tests; deletion only. | `internal/platform/database/postgres/options.go:1`, `internal/platform/database/postgres/postgres.go:1` |
| a609d270c | `allow` | test(postgres): remove low-value option and validate tests | Deletes 173 lines of low-value tests and no source; the options it covered were deleted two commits later in 5cca615. | `internal/platform/database/postgres/options_test.go:1` |
| 186d1c44d | `allow` | refactor(cmd): share database and logging flag registration across commands | Extracts addDatabaseFlags/addLoggingFlags into cmd/utils.go and removes three copies; it reduces duplication. | `cmd/utils.go:75`, `cmd/utils.go:87` |
| 5cca615b6 | `allow` | refactor(postgres): replace the lifecycle client with free functions | Replaces the Client type and option funcs with free Ping/Migrate/MigrationStatus functions that take the pool; net removal of about 300 lines, with no new copy or complexity. | `internal/platform/database/postgres/postgres.go:21`, `internal/platform/database/postgres/migration.go:86` |
| ce6099c3f | `allow` | build: remove the amd64-only linux, osx, and windows targets | A one-line build-target change in templateinit, plus test deletions. | `internal/templateinit/initializer.go:1` |
| b2f5f2096 | `allow` | refactor(domain): delete the unused UserRepository interface | Deletes the unused UserRepository interface and its assertion; deletion only. | `internal/domain/user.go:1`, `internal/modules/user/repository.go:1` |
