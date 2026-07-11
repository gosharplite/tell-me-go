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

### context/manager.go — groupTurns error in capBestBlock

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `groupTurns` only fails on nil content, nil parts, or empty roles.
  The sub-slice `contents[best.startMsg:best.endMsg]` passed to `groupTurns` inside
  `capBestBlock` comes from a history window that was already loaded and validated
  by `loadHistory()` via `validateHistoryBoundaries`. No mutation occurs on the
  slice between validation and this call site. Structurally unreachable — same
  acceptance class as `json.Marshal` on all-string structs in
  `global_prompt_tracker.go`.
- **See**: `internal/agent/session/context/manager.go`
  (architect-acceptance comment at the `groupTurns` call site in `capBestBlock`)

### context/manager.go — capBestBlock error propagation in checkWindowSize

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The `capBestBlock` error path in `checkWindowSize` propagates the
  error from `groupTurns` inside `capBestBlock`. Since `groupTurns` on an
  already-validated sub-slice is structurally unreachable (see previous entry),
  this error-handling branch is equally unreachable. Both gaps share the same root
  cause and acceptance rationale.
- **See**: `internal/agent/session/context/manager.go`
  (architect-acceptance comment at the `capBestBlock` call site in `checkWindowSize`)

### domain/config/config.go — validateProviderUniqueness always returns nil

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `validateProviderUniqueness` is a named validation anchor that
  always returns `nil` — the provider-unique-name invariant is structurally
  enforced by Go's `map[string]LLMProvider` semantics (a map cannot contain
  duplicate keys). The method exists as a future hook point if providers are
  ever assembled from multiple config files. The existing
  `TestConfig_ValidateProviderUniqueness` test exercises the happy path; the
  error-return branch flagged by coverage tools does not exist in the current
  single-file architecture. Structurally unreachable — same acceptance class
  as `json.Marshal` on all-string structs in `global_prompt_tracker.go`.
- **See**: `internal/domain/config/config.go:183-185`

### agent/agenttest/helpers.go — 0% coverage on interface-satisfying stubs (15 methods)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The following methods on test doubles in `agenttest/helpers.go`
  are no-op stubs that exist solely to satisfy interface contracts for tests.
  They are intentionally empty (or return zero values) and are never called
  with real logic paths — only as compile-time interface satisfaction:
  `RenderResponse`, `LogTurnStatus`, `LogSystemMessage`, `LogUsage`,
  `LogToolCall`, `LogToolResult`, `RenderHealthReport`, `SetUseColor`,
  `SetForceSpinner`, `Render`, `GetSkillRepository`, `Subscribe`,
  `WaitStarted`, `Warn`, `Prompt`.
  Testing a mock's no-op method would test the mock itself, not production
  behavior — same acceptance class as `mockFileSystem.Chmod` and
  `domainFS.Chmod` already documented below.
  The `agenttest/` package is already excluded from coverage metrics by the
  Makefile `test-coverage` target.
- **See**: `internal/agent/agenttest/helpers.go`

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

## Coverage Gaps (ACCEPTED — 2026-07 Batch Triage)

### Defensive guards, platform-specific, and fault-injection gaps (12 sites)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: After closing all real coverage gaps (batch 1-3), the remaining
  44 medium-priority gaps were triaged. Of these, 32 were already documented
  (above entries), mocks/stubs, or trivial getters. The remaining 12 fall into
  three established acceptance classes:
  - **Defensive nil/empty guards** on internal pipeline state: `propagateNamedInterfaceAssertionUsages`,
    `propagateInitUsages`, `propagateTransitiveExternalUsage`, `setupCommand`,
    `newPipelineCmd` — callers never produce nil/empty inputs.
  - **Platform-specific branches**: `resolveAndValidateOutputPath` (Windows separator),
    `NormalizeKey` (Windows volume prefix) — not executable on Linux.
  - **Fault-injection required**: `sqlite_store.Update`/`Delete` (`RowsAffected` driver error),
    `state.SetInfo` (`AtomicWrite` disk fault), `getConcurrencyLimit` (`runtime.NumCPU() < 1`
    structurally unreachable), `processWatcherEvents` (goroutine timing).
- **See**: Inline architect-acceptance comments at each gap site:
  `internal/tools/analysis/propagate_named_interface_assertions.go:62`,
  `internal/tools/analysis/propagate_transitive.go:32`,
  `internal/tools/analysis/propagate_transitive.go:128`,
  `internal/tools/workspace/process_executor.go:118`,
  `internal/tools/workspace/process_executor.go:356`,
  `internal/tools/workspace/pipeline.go:56`,
  `internal/tools/analysis/complexity.go:125`,
  `internal/infrastructure/persistence/sqlite_store.go:200`,
  `internal/infrastructure/persistence/sqlite_store.go:218`,
  `internal/infrastructure/persistence/state.go:56`,
  `internal/ui/tui/browser.go:141`,
  `internal/pkg/filepathutil/normalize.go:61`

### services/task_service.go — AppendTask delegation wrapper

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `AppendTask` is a pure delegation wrapper that calls
  `s.store.Append(ctx, task)`. The store's `Append` error path is already
  covered by `TestTaskService_AddTask_WriteError` (which calls `AddTask` →
  `store.Append`). Testing a pass-through method provides no value — same
  acceptance class as `domainFS.Chmod`, `mockFileSystem.Chmod`,
  `plainOSFS.Chmod`, and `OSFileSystem.Chmod`.
- **See**: `internal/domain/services/task_service.go:95-97`

### skills/file_repo.go — soft enforcement of skill-unique-name

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `fileSkillRepository.hasSkillName()` at `file_repo.go:36` uses
  `slog.Warn` + skip rather than a hard error for duplicate skill names during
  loading. This is intentional: skill files are user-authored Markdown; a typo
  that creates a duplicate name should not prevent the agent from starting.
  The warning is visible in logs and actionable by the operator. The
  `skill-unique-name` invariant in the domain model is informational rather
  than a safety constraint — unlike `tool-unique-name` (where duplicate
  registration would cause nondeterministic dispatch).
- **See**: `internal/infrastructure/skills/file_repo.go:36` (hasSkillName check),
  `docs/domain-model/README.md` (invariant audit, row 11)

---

## Coverage Gaps (ACCEPTED — 2026-07 skills.sh Batch Triage)

### orchestrator/middleware.go — json.Marshal error in detectLoop

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `json.Marshal(&sanitized)` on `*llm.Content` cannot fail — all fields
  are simple types (string, `[]*Part`, etc.). The `sanitized` value is a shallow
  copy of `state.Response` with `ID` set to `""`. No field implements
  `json.Marshaler` with error-return semantics. Structurally unreachable — same
  acceptance class as `json.Marshal` on all-string structs in
  `global_prompt_tracker.go`.
- **See**: `internal/agent/orchestrator/middleware.go:164-166`

### skills.sh integration — structurally unreachable and fault-injection gaps (17 sites)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Following the established acceptance classes from the 2026-07
  batch triage, the remaining gaps in the skills.sh integration package
  (`internal/tools/integrations/skillssh/`) fall into these categories:

  **Structurally unreachable (8 sites)**:
  - `searchGitHubAPI`: `http.NewRequestWithContext` error — URL constructed from
    `url.QueryEscape`, unreachable for GET.
  - `searchGitHubAPI`: `io.ReadAll` error — requires transport-level corruption.
    Same class as pidlock/process_executor acceptances.
  - `searchGitHubAPI`: `limit > 10` guard — `per_page=10` makes this defensive
    against API changes.
  - `fetchSkillMeta`: `http.NewRequestWithContext`, `client.Do`, `StatusCode`,
    `io.ReadAll` (×4) — best-effort function; all errors return `("", "")`.
  - `findSkillDir`: `info.Name() != "SKILL.md"` skip branch — tested implicitly
    by `TestRemoveSkill_RemovesSkillShSkill`.
  - `findSkillDir`: `return nil` after Walk — already covered by
    `TestRemoveSkill_NotFound`.
  - `findRepoRoot`: `return skillDir` fallback — defensive guard, structurally
    unreachable.

  **Filesystem fault injection (4 sites)**:
  - `InstallSkill`: `os.MkdirAll` error — same class as `pidlock/pidlock.go`.
  - `RemoveSkill`: `os.RemoveAll` error — same class.
  - `findSkillDir`: `filepath.Walk` walkErr — unreachable on test-controlled
    temp dirs.
  - `findSkillDir`: `os.ReadFile` error — race condition between Walk and file
    deletion.

  **Integration-level (2 sites)**:
  - `NewSkillManager` — tested via `newTestMgr` (unexported constructor).
  - `RegisterSkillsShTools` + all registration branches — tested at integration
    level via DI container.

  **Other (3 sites)**:
  - `searchGitHubAPI`: `client == nil` → `http.DefaultClient` fallback —
    requires nil client injection; tests always provide a mock client.
  - `SearchSkills`: `name == ""` and `desc == ""` formatting branches — tested
    implicitly via `TestSearchSkills_ResultsParsed`.
  - `validateSkillRemovable`: `repo.GetAll` error — already covered by the
    `ListSkills` repo-error test pattern.

- **See**: `internal/tools/integrations/skillssh/manager_impl.go`,
  `internal/tools/integrations/skillssh/tools.go`

---

## Test Complexity (ACCEPTED)

### tests/e2e/history_flags_test.go — TestHistoryNavigation_CompleteWorkflow (complexity 15)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Sequential E2E workflow where steps 2 ("go back") and 3 ("retry")
  mutate shared history state and depend on prior steps. Splitting into subtests
  would force duplicating the expensive `newHistoryNavEnv` setup (mock server +
  3 sequential CLI invocations + history reconciliation per subtest). The
  function's own doc-comment explicitly documents this design decision: "These
  are not independent subtests because steps 2 and 3 mutate shared state and
  step 3 depends on step 2's rollback." The 15 cyclomatic complexity points are
  test boilerplate assertions, not branching business logic. Same acceptance
  class as the existing structural concerns — test infrastructure where the cost
  of refactoring outweighs the maintainability benefit.
- **See**: `tests/e2e/history_flags_test.go:142`

---

*Last Updated: 2026-07 (skills.sh triage + agenttest stubs + test complexity + middleware loop-detection gap)*
