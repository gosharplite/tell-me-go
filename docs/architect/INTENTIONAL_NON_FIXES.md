# Intentional Non-Fixes

Items evaluated by the architect and deliberately NOT pursued.
Any AI agent recommending these should consult the rationale below.

---

## Coverage Gaps (ACCEPTED)

### pidlock/pidlock.go — 6 gaps

- **Status**: ACCEPTED (2026-06-28, commit `0f882423`)
- **Rationale**:
  - **2 gaps are platform-specific**: the Windows `return true` branch in
    `isProcessAlive` and the `IsStale` `os.Stat`-failure fallback are
    unreachable in `pidlock_unix_test.go` (the only test file).
  - **2 gaps are structurally unreachable on Linux**: `os.FindProcess`
    never fails on Linux (it wraps the PID without a syscall); the ESRCH
    branch is tested by `TestIsProcessAlive_DeadProcess` but the coverage
    tool reports it as uncovered when the dead-PID test Skips.
  - **2 gaps require filesystem fault injection**: `fmt.Fprintf` and
    `f.Sync()` to a just-opened `*os.File` can only fail on disk-full or
    hardware error. Not worth the test complexity for a process-lock helper.
- **See**: `internal/infrastructure/pidlock/pidlock.go` (architect-acceptance
  comments at each gap site)

### history/global_prompt_tracker.go — writeCompactedData branch

- **Status**: ACCEPTED (2026-06-28, commit `0f882423`)
- **Rationale**: `writeCompactedData` cannot return false when writing to
  `bytes.Buffer`: `json.Marshal` on `promptEntry` never fails (all string
  fields), and `bytes.Buffer.Write` never returns an error. Structurally
  unreachable.
- **See**: `internal/infrastructure/history/global_prompt_tracker.go`
  (architect-acceptance comment at the `prepareCompactedEntries` call site)

### history/global_prompt_tracker.go — json.Marshal in Append

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `json.Marshal(entry)` cannot fail for `promptEntry` because all fields
  are `string`. The error path exists only for interface contract compliance.
  Structurally unreachable — same rationale as the `writeCompactedData` branch
  already documented below. An architect-acceptance comment already exists at
  the gap site (`global_prompt_tracker.go:129-130`).
- **See**: `internal/infrastructure/history/global_prompt_tracker.go:131-133`

### history/global_prompt_tracker.go — json.Marshal in writeCompactedData

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Same as the `Append` gap above: `json.Marshal(entry)` cannot fail
  for `promptEntry` (all `string` fields). An architect-acceptance comment
  already exists at the gap site (`global_prompt_tracker.go:365-366`).
- **See**: `internal/infrastructure/history/global_prompt_tracker.go:367-369`

### persistence/mock_fs.go — Chmod always returns nil

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `mockFileSystem.Chmod` is a test-double stub that returns `nil`
  by design. Adding a test for a mock's no-op method would test the mock itself,
  not production behavior. Mocks exist to satisfy interfaces with canned
  responses; testing them is circular and provides no value.
- **See**: `internal/domain/persistence/mock_fs.go:146-148`

### persistence/os_fs.go — domainFS.Chmod delegation wrapper

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `domainFS.Chmod` is a pure delegation wrapper that calls
  `f.fs.Chmod(ctx, name, mode)`. Testing a delegation pass-through provides
  no value — the underlying `OSFileSystem.Chmod` error path is exercised
  by the `os_fs.go:50-53` gap (triaged separately as NEEDS TEST). Same
  rationale as `mockFileSystem.Chmod`.
- **See**: `internal/infrastructure/persistence/os_fs.go:159-161`

### persistence/os_fs.go — OSFileSystem.Chmod error path

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Triggering `os.Chmod` failure requires OS-level permission manipulation
  (changing file ownership, read-only filesystem, or Windows ACL restrictions).
  The `fsRetry` wrapper is already tested through other `OSFileSystem` methods
  (e.g., `Stat`, `ReadFile`), so the retry logic itself is covered. Same
  acceptance rationale as `pidlock/pidlock.go` — "require filesystem fault
  injection... not worth the test complexity for a process-lock helper."
- **See**: `internal/infrastructure/persistence/os_fs.go:50-53`

### tools/workspace/process_executor.go — StdoutPipe error

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `os/exec.Cmd.StdoutPipe()` only returns an error when called after
  `cmd.Start()` or when called more than once on the same command. In
  `prepareCommand`, `StdoutPipe()` is called immediately after constructing
  the `exec.Cmd` and before `Start()`, making this error path structurally
  unreachable in correct code. Testing it would require deliberately
  violating the `os/exec` contract (calling `StdoutPipe` after `Start`),
  which is a programming error, not a recoverable runtime condition.
- **See**: `internal/tools/workspace/process_executor.go:143-145`

### tools/workspace/process_executor.go — StderrPipe error

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Same rationale as `StdoutPipe` above: `os/exec.Cmd.StderrPipe()`
  only fails when called after `cmd.Start()` or called twice. In
  `prepareCommand`, it is called before `Start()` and exactly once, making
  this error path structurally unreachable.
- **See**: `internal/tools/workspace/process_executor.go:147-149`

### tools/analysis/complexity.go — errgroup.Wait error

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The `g.Wait()` error path handles a goroutine failure during
  concurrent complexity metric gathering. The source already contains an
  architect-acceptance annotation: "fails selectively mid-traversal, which
  is not reproducible." Reproducing this requires injecting a fault into a
  specific goroutine mid-walk, which is not feasible without restructuring
  the concurrency model for testability — a change disproportionate to the
  value of covering this one error branch.
- **See**: `internal/tools/analysis/complexity.go:110-112`

### persistence/sqlite_store.go — GetByID general database error

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The general database error path (`err != nil && err != sql.ErrNoRows`)
  in `sqliteTaskStore.GetByID` requires a corrupted database, closed connection,
  or driver-level failure to trigger. Reproducing this in a unit test requires
  DB fault injection disproportionate to the value. The `sql.ErrNoRows` path
  (querying a non-existent ID) is tested separately.
- **See**: `internal/infrastructure/persistence/sqlite_store.go:70`

### persistence/sqlite_store.go — GetByID time.Parse error

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The `time.Parse(time.RFC3339Nano, createdAtStr)` error path
  in `sqliteTaskStore.GetByID` is structurally unreachable. The `created_at`
  column is always written by this same store using `time.Format(time.RFC3339Nano)`,
  guaranteeing a round-trippable format. Same acceptance rationale as
  `json.Marshal` on all-string structs in `global_prompt_tracker.go`.
- **See**: `internal/infrastructure/persistence/sqlite_store.go:72-73`

### persistence/persistencetest/plain_os_fs.go — Chmod delegation stub

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `plainOSFS.Chmod` is a test-double delegation stub that passes
  through to `os.Chmod`. Same acceptance rationale as `mockFileSystem.Chmod` and
  `domainFS.Chmod`: testing a mock's delegation method provides no value — the
  method exists solely to satisfy the `FileSystem` interface in test contexts.
- **See**: `internal/infrastructure/persistence/persistencetest/plain_os_fs.go:130-132`

---

## Structural Concerns (ACCEPTED)

### infrastructure/testing/ — shared testdata helper directory with no Go package

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `internal/infrastructure/testing/` exists solely as a container
  for `testdata/helper/` — a test binary providing 14 subcommands (echo, cat,
  stderr, sleep, exit, grep, diff, etc.) that simulates real OS processes for
  tests. It is shared by two packages in different subtrees:
  `internal/infrastructure/exec/main_test.go` and
  `internal/tools/workspace/main_test.go`. Placing it under either consumer
  would force a cross-subtree `testdata` reference from the other. The
  directory contains no production `.go` files, which is why it does not
  appear in the dependency graph. The Makefile `test-coverage` target already
  explicitly excludes this path from coverage metrics alongside the other
  `*test/` sub-packages. The directory name `testing/` clearly signals its
  purpose as test infrastructure. No action is needed.
- **See**: `Makefile` (test-coverage target, line filtering `internal/infrastructure/testing/`),
  `internal/infrastructure/testing/testdata/helper/main.go`,
  `internal/infrastructure/exec/main_test.go` (builds the helper),
  `internal/tools/workspace/main_test.go` (builds the helper)

---

## ADR-021 Follow-Ups (ALL COMPLETE)

### verify-testutil-convention CI lint

- **Status**: DONE
- **Detail**: `verify-testutil-convention` target in the Makefile, wired into
  `make test`. Fails the build if any `internal/.*/testutil/.*\.go` path or
  `internal/.*/testutil` import is found.

### testify/mock migration

- **Status**: DONE (2026-06-28, commit `505d6b26`)
- **Detail**: `verify-mock-pattern` target in the Makefile enforces **zero
  tolerance**. The single remaining `testify/mock` import in
  `internal/agent/agentinternal/accessor_test.go` is the ADR-021 escape
  hatch — intentionally excluded from the CI check.

### agentinternal/ unused symbols audit

- **Status**: DONE (2026-06-28)
- **Detail**: All 3 exported symbols have active consumers:
  - `AsAgentInternal` — used in `tests/integration/agent/agent_integration_test.go`
    and `tests/integration/agent/regression_test.go`
  - `MockSessionLifecycleManager` — used in `internal/agent/service_test.go`
  - `NewBareAgent` — used in `internal/agent/agent_error_test.go`

### MockEstimator collapse into MockTokenCounter

- **Status**: REJECTED by architect (Session 5, 2026-04-19)
- **Rationale**: Preserved for cohesion. `MockEstimator` satisfies the
  `tokenEstimator` interface (single-method: `EstimateTokens`) while
  `MockTokenCounter` satisfies the broader `TokenCounter` interface.
  Collapsing would force `MockTokenCounter` consumers to depend on a wider
  interface surface. The 2 reference sites in `agent/session/context` are
  stable.
- **See**: `docs/refactor/testutil-audit.md`, Session 5 outcome notes

---

## Design Rejections

### MockEventBusFail fold-into-MockEventBus

- **Status**: REJECTED by architect (Session 4, 2026-04-19)
- **Rationale**: `MockEventBusFail` is `MockEventBus` + an injectable
  `PublishErr` field. The architect chose to preserve both as separate
  types rather than adding an error-injection field to `MockEventBus`,
  keeping the happy-path mock simple.
- **See**: `docs/refactor/testutil-audit.md`, Session 4 outcome notes

### WarmImplementations(ctx) Opt-In

- **Status**: REJECTED ("Won't Do", ADR-039, finalized 2026-06)
- **Rationale**: No consumer has demonstrated measurable UX impact from the
  5-second TTL sawtooth. Background goroutine lifecycle management adds
  complexity disproportionate to the benefit.
- **See**: `docs/adr/2026-05-lazy-implementation-index.md`, "Rejected:
  WarmImplementations(ctx) Opt-In (Won't Do)" section

### Config.load() io.Reader Extraction

- **Status**: REJECTED by architect (2026-07)
- **Rationale**: `YAMLConfigLoader` is already the filesystem adapter
  implementing `domain_config.ConfigLoader`. `configureViper` couples
  environment-variable binding to path-based file discovery; extracting an
  `io.Reader`-based `Load()` would break the watcher's mtime-based change
  detection (`watcher_test.go`) and duplicate the existing adapter
  abstraction. The `JSONSessionLoader` already demonstrates the correct
  pattern for new loaders: accept a path, return a domain object.
- **See**: `internal/infrastructure/config/config.go` (`load`, `configureViper`,
  `YAMLConfigLoader`), `internal/infrastructure/config/watcher_test.go`

---

*Last Updated: 2026-07*
