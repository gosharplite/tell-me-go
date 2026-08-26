# Intentional Non-Fixes

Items evaluated by the architect and deliberately NOT pursued.
Any AI agent recommending these should consult the rationale below.

## Acceptance Classes

Every entry carries an acceptance class — the triage classification that
justifies why the issue is deliberately left unaddressed. These class names
are also referenced by code comments (e.g. "structurally unreachable — see
the `structurally-unreachable` acceptance class") and are the authoritative
definition of each term. Mirrors the `AcceptanceClass` enum in the quality
domain model (`docs/domain-model/quality.modelith.yaml`).

| Class | Definition |
| --- | --- |
| `structurally-unreachable` | The branch cannot fire in correct code — e.g. `json.Marshal` on an all-string struct, or `StdoutPipe` before `Start`. |
| `delegation-wrapper` | A thin pass-through that would only re-test the underlying implementation — e.g. `domainFS.Chmod`, `AppendTask`, `RealClock`. |
| `platform-specific` | A branch only executable on a specific OS — e.g. Windows path separators, Darwin kernel observation. |
| `fault-injection-required` | Triggering the branch requires filesystem or driver fault injection disproportionate to the test value. |
| `defensive-guard` | A nil or empty guard on internal pipeline state that callers never produce. |
| `interface-stub` | A no-op method that exists solely to satisfy an interface contract — e.g. `auth.Invalidate`, `NoOpEventBus`. |
| `test-complexity` | Sequential state-mutation or dispatch-heavy test where splitting would duplicate expensive setup. |
| `dispatch-structural` | Cyclomatic complexity from type-switch or branch count, not branching business logic — e.g. `handleDomainEvent` (CC=12). |
| `soft-enforcement` | An invariant deliberately enforced as a warning rather than a hard failure — e.g. `skill-unique-name`. |

## Drift Policy

Catalog entries pin code locations with `file:line` or `file:start-end`
references. These references are verified at record time but can drift as
code above them is edited. The analysis tooling matches cataloged gaps by
*interval overlap* and accepts *bare-basename* references; both are
deliberate trade-offs for a curated catalog, but a shifted range can quietly
catalog a new gap no one reviewed. Policy:

- **Re-verify on every PR** that touches a file referenced by the catalog:
  confirm each entry's ranges still point at the intended symbol, and update
  the entry (or record a new one) when they do not.
- **Never rely on overlap-matching as review** — it is an advisory aid, not a
  substitute for re-anchoring the reference.
- **CC values in complexity entries are re-measured** whenever the referenced
  function changes; drift is recorded in the entry's rationale (see the
  2026-08 CC drift corrections for the pattern).
- **Enforcement boundary**: complexity pins: mechanically verified by `verify-nonfix-catalog` on over-threshold functions; under-threshold pins are enforced at threshold-crossing, by construction; coverage pins: prose policy per ADR-054 consequences — the coverage matcher has no name axis to verify against.
- **Coordination rule**: any legitimate catalog change (add/remove/re-anchor/CC re-verify) edits the partition test in the same PR.

---

## Coverage Gaps (ACCEPTED)

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

### ado/pipeline_crud.go — buildVariablesUpdatePayload error return

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The error return of `json.Marshal(existingDef)` in
  `buildVariablesUpdatePayload` is structurally unreachable: the map is
  produced by `json.Decode` into `interface{}` plus manual insertion of
  JSON-safe primitives (string, bool, *bool), so `json.Marshal` cannot fail.
  Note: the inline comment at `pipeline_crud.go:272-274` cites "ADR-037",
  but ADR-037 is the `agentinternal` test-bridge decision and says nothing
  about `json.Marshal` — that citation is stale and should be read as
  referring to this catalog's `json.Marshal` acceptance class instead.
  Same acceptance class as the cataloged `json.Marshal` entries
  (`global_prompt_tracker.go` Append/writeCompactedData,
  `orchestrator/middleware.go` detectLoop).
- **See**: `internal/tools/integrations/ado/pipeline_crud.go:272-275`
  (inline comment + `return json.Marshal(existingDef)`), and the `if err != nil`
  branch at `pipeline_crud.go:308-310` that consumes this unreachable error

### history/history.go — complementPred and contentHasFunctionCall at 0% → RESOLVED

- **Status**: RESOLVED (2026-07, `TestHistoryManager_SetPinned_WithFunctionCall` + `TestHistoryManager_SetPinned_ViaModelID`)
- **Coverage after fix**: `complementPred` **100%** (was 0%), `contentHasFunctionCall` **100%** (was 0%),
  `contentHasFunctionResponse` **100%** (was 75%), `validateTurnPair` **78.9%** (was 52.6%),
  package **97.6%** (was 96.1% at `origin/dev`).
- **What was covered**: Two tests close all gaps in the `turnPairRules` chain:
  - `TestHistoryManager_SetPinned_WithFunctionCall`: FunctionCall→FunctionResponse turn with
    two subtests — forward rule (pin via model ID, rule 0) and backward rule (pin via user ID, rule 1).
    This covers `complementPred` at 100% and `contentHasFunctionResponse` at 100%.
  - `TestHistoryManager_SetPinned_ViaModelID`: plain user→model text turn, pin via **model** ID.
    This drives rule 0's pred (`contentHasFunctionCall`) to return false on a non-FC model message,
    triggering fallthrough to rule 3. Closes the `contentHasFunctionCall` false-return gap (75% → 100%).
- **What remains uncovered**: `validateTurnPair` four error-return branches: "no following
  message" (`:270`), "no preceding message" (`:275`), "unexpected partner role" (`:282`),
  and "mismatched partner content" (`:289`). Requires edge-case malformed turn sequences
  (OOB indices, role mismatches, FC/non-FR pairings). Low ROI.
- **See**: `internal/infrastructure/history/history_test.go`
  (`TestHistoryManager_SetPinned_WithFunctionCall`),
  `history.go:301` (complementPred), `history.go:333` (contentHasFunctionCall)

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
  acceptance class as platform-specific / filesystem fault-injection gaps —
  "require filesystem fault injection... not worth the test complexity for a
  process-lock helper."
- **See**: `internal/infrastructure/persistence/os_fs.go:50-53`

### security/command_validator.go — HasBareNewline delegation wrapper at 0%

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `HasBareNewline` is a thin delegation wrapper that calls
  `hasBareNewline(normalize(cmd))`. It satisfies `domain.CommandValidator.HasBareNewline`
  and is called by three production callers in `tools/workspace/shell.go`
  (lines 162, 431, 558). Tests exercise the unexported `hasBareNewline` helper
  directly (`command_validator_test.go:900`) rather than through the method
  wrapper. Same acceptance class as `domainFS.Chmod`, `OSFileSystem.Chmod`,
  and `plainOSFS.Chmod` — testing a delegation pass-through provides no value
  when the underlying helper is already covered.
- **See**: `internal/infrastructure/security/command_validator.go:119-121`,
  `internal/tools/workspace/shell.go:162,431,558`

### tools/workspace/process_executor.go — StdoutPipe error

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `os/exec.Cmd.StdoutPipe()` only returns an error when called after
  `cmd.Start()` or when called more than once on the same command. In
  `prepareCommand`, `StdoutPipe()` is called immediately after constructing
  the `exec.Cmd` and before `Start()`, making this error path structurally
  unreachable in correct code. Testing it would require deliberately
  violating the `os/exec` contract (calling `StdoutPipe` after `Start`),
  which is a programming error, not a recoverable runtime condition.
  Re-anchored 2026-09 (#1431): lines drifted 143-145 → 151-153 as the
  setupCommand Env-handling block grew above the StdoutPipe call.
- **See**: `internal/tools/workspace/process_executor.go:152-154`

### tools/workspace/process_executor.go — StderrPipe error

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Same rationale as `StdoutPipe` above: `os/exec.Cmd.StderrPipe()`
  only fails when called after `cmd.Start()` or called twice. In
  `prepareCommand`, it is called before `Start()` and exactly once, making
  this error path structurally unreachable.
  Re-anchored 2026-09 (#1431): lines drifted 147-149 → 155-157 as the
  setupCommand Env-handling block grew above the StderrPipe call.
- **See**: `internal/tools/workspace/process_executor.go:156-158`

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
- **See**: `internal/agent/session/context/manager.go:526-528`
  (architect-acceptance comment at the `groupTurns` call site in `capBestBlock`)

### context/manager.go — capBestBlock error propagation in checkWindowSize

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The `capBestBlock` error path in `checkWindowSize` propagates the
  error from `groupTurns` inside `capBestBlock`. Since `groupTurns` on an
  already-validated sub-slice is structurally unreachable (see previous entry),
  this error-handling branch is equally unreachable. Both gaps share the same root
  cause and acceptance rationale.
- **See**: `internal/agent/session/context/manager.go:571-573`
  (architect-acceptance comment at the `capBestBlock` call site in `checkWindowSize`)

### agent/session/context/manager.go — capBestBlock non-capped return

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The final return in `capBestBlock` (`manager.go:542`) is reached
  when `best.count >= minViable` but `groupTurns` returns ≤1 turn for the
  sub-slice. With the default `contiguousUnpinnedSelector` (MinViableBlock=2),
  `best.count >= 2` and the sub-slice spans ≥2 turns, so `groupTurns` always
  returns ≥2 turns. This return is a defensive fallthrough for degenerate
  message sequences. Same acceptance class as defensive nil/empty guards on
  internal pipeline state (2026-07 Batch Triage). Re-anchored 2026-09: the
  return drifted from line 582 to 590 as the acceptance comment block grew.
  Re-anchored 2026-08 (#1320): the return drifted from line 590 to 542 as the
  ADR-057 cache-removal deletions and the #1320 additions shifted the file;
  re-verified against the live source.
- **See**: `internal/agent/session/context/manager.go:542`

### agent/session/session_manager.go — tuiCleanup and setupUIRendering TUI branches

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Both the `tuiCleanup` deferred call in `Run()` and the
  `tuiOutput` branch in `setupUIRendering` require a ProgressRenderer and full
  TUI integration to exercise. The branches themselves are simple nil-checks
  with no branching business logic (call a cleanup function; return nil for
  the bridge). Testing them requires TUI integration infrastructure
  disproportionate to the value. Same acceptance class as the integration-level
  branches in the 2026-07 skills.sh Batch Triage. These ranges also cover the
  final-TurnStatus subscription in `Run()` and the `renderPostTUISummary` path
  in `teardownSession` — the TUI summary/subscription path added with the TUI
  auto-close summary feature — the same acceptance class (TUI integration
  requiring a ProgressRenderer + full TUI harness).
- **See**: `internal/agent/session/session_manager.go:137-144,203-210,334-337`

### agent/session/ui/dispatcher.go — ToolOutputStreamEvent case in handleSystemMessage

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The `ToolOutputStreamEvent` case in `handleSystemMessage`'s type
  switch is structurally unreachable via normal dispatch — `ToolOutputStreamEvent`
  is registered to `handleToolOutputStream`, not `handleSystemMessage`. The case
  exists as a defensive fallthrough for a same-handler dispatch pattern.
  Same acceptance class as defensive guards on internal pipeline state
  (2026-07 Batch Triage).
- **See**: `internal/agent/session/ui/dispatcher.go:204-205`

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
- **See**: `internal/domain/config/config.go:224-229` (definition), `:251-253` (call-site error branch, covered by this entry)

### domain/events/events.go — NoOpEventBus no-op stubs at 0%

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `NoOpEventBus.Publish`, `Subscribe`, `Shutdown`, `Flush`,
  `Listen`, and `WaitStarted` are no-op stub methods required to satisfy the
  `EventBus` interface contract. `NoOpEventBus` is used as a safe default
  (`return nil` semantics) when no real EventBus is needed (e.g., tests,
  uninitialized paths). Go's coverage instrumentation does not count empty
  method bodies as covered. Same acceptance class as `agenttest/helpers.go`
  interface-satisfying stubs, `auth.Invalidate` no-ops, and `watcher_noop.go`
  stubs already documented below.
- **See**: `internal/domain/events/events.go:65-70`

### domain/llm/types.go — NewID delegation wrapper

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `NewID()` is a pure delegation to `uuid.New().String()`. Testing it
  would test the third-party uuid library, not project code. Same acceptance
  class as `RealClock` delegation wrappers in `pkg/clock/clock.go` and
  `mockFileSystem.Chmod`.
- **See**: `internal/domain/llm/types.go:234-236`

### domain/ports/repository.go — Task struct field accessors (GetID, GetStatus, GetCreatedAt)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `Task.GetID()`, `Task.GetStatus()`, and `Task.GetCreatedAt()` are
  struct field accessors that exist solely to satisfy the `FilterableItem`
  interface constraint. They contain no branches and no business logic.
  Testing them would test Go struct field access. Same acceptance class as
  the interface-satisfying stubs in `agenttest/helpers.go`.
- **See**: `internal/domain/ports/repository.go:108,112,116`

### agent/orchestrator/engine_phases.go — default case in RecoveryStep switch

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `ClassifyLLMError` returns exactly one of five known categories
  (RateLimited, ContextOverflow, AuthFailure, ServerError, Timeout) — the switch
  in `RecoveryStep.Process` covers all five explicitly. The `default:` case is
  a defensive guard that is structurally unreachable. Same acceptance class as
  `json.Marshal` on all-string structs in `global_prompt_tracker.go`.
- **See**: `internal/agent/orchestrator/engine_phases.go:177`

### agent/orchestrator/middleware.go — return nil in injectSyntheticLoopFeedback

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The `return nil` path in `injectSyntheticLoopFeedback` is reached
  only when `HasToolCalls` is true but `Response` has no `FunctionCall` parts —
  inconsistent internal state that does not occur in normal operation
  (`HasToolCalls` is derived from the presence of `FunctionCall` parts).
  Defensive guard — same acceptance class as defensive nil/empty guards on
  internal pipeline state (2026-07 Batch Triage).
- **See**: `internal/agent/orchestrator/middleware.go:302` (re-anchored 2026-08, #1327 T5: unused reset() method deleted)

### agent/agenttest/helpers.go — 0% coverage on interface-satisfying stubs (16 methods)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The following methods on test doubles in `agenttest/helpers.go`
  are no-op stubs that exist solely to satisfy interface contracts for tests.
  They are intentionally empty (or return zero values) and are never called
  with real logic paths — only as compile-time interface satisfaction:
  `RenderResponse`, `LogTurnStatus`, `LogSystemMessage`, `LogUsage`,
  `LogToolCall`, `LogToolResult`, `RenderHealthReport`, `SetUseColor`,
  `SetForceSpinner`, `Render`, `GetSkillRepository`, `Subscribe`,
  `WaitStarted`, `Warn`, `Prompt`, `SetWordWrap`.
  Testing a mock's no-op method would test the mock itself, not production
  behavior — same acceptance class as `mockFileSystem.Chmod` and
  `domainFS.Chmod` already documented below.
  The `agenttest/` package is already excluded from coverage metrics by the
  Makefile `test-coverage` target.
- **See**: `internal/agent/agenttest/helpers.go:33-34,35,36,37-38,39-40,41-42,43,44,45,48,58-59,315-317,356,360,390,391`
  (Re-anchored 2026-09 (#1433): the previous See ref was file-only
  (internal/agent/agenttest/helpers.go), which the coverage matcher drops —
  interval-overlap matching requires a line spec — so all 16 methods surfaced
  as uncataloged gaps in detailed-coverage reports. Re-anchored to per-method
  line refs mirroring the 2026-08 "agent/agenttest + clitest/eventstest"
  entry's comma-list pattern; partition-test rows added in the same commit
  per the Drift Policy coordination rule.)

### agent/agenttest + clitest/eventstest — interface-satisfying mock stubs at 0%

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The following mock methods exist solely to satisfy interface
  contracts (CostTracker.Warmup, HistoryManager.GetLastModelTurn/GetModelTurn/
  UpdateTurnContent, Logger.ExecutionCompletedLate, UIRenderer.UpdateSpinnerStatus,
  EventBus.Subscribe/WaitStarted, Gateway/LLMClient.ExtractDocument,
  clock.Ticker.Stop, clitest mocks) — no unit test calls them because tests
  exercise the real implementations via DI or integration tests. Testing a
  mock's stub would test the mock itself (circular, no value) — same acceptance
  class as agenttest/helpers.go interface-satisfying stubs (already documented
  above) and auth.Invalidate no-ops.
- **See**: `internal/agent/agenttest/mock_cost_tracker.go:44`,
  `internal/agent/agenttest/mock_history_manager.go:139,152,158`,
  `internal/agent/agenttest/mock_logger.go:20`,
  `internal/agent/agenttest/mock_ui_renderer.go:289`,
  `internal/domain/events/eventstest/test_event_bus.go:109`,
  `internal/agent/agenttest/mock_event_bus_fail.go:22,26`,
  `internal/agent/agenttest/mock_gateway.go:66`,
  `internal/agent/agenttest/mock_llm_client.go:91`,
  `internal/agent/agenttest/mock_clock.go:93`,
  `internal/cli/clitest/mock_bootstrapper.go:225`,
  `internal/cli/clitest/mock_chat_service.go:128,143`

### infrastructure/auth/auth.go — four Invalidate() no-op stubs at 0%

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `APIKeyAuth.Invalidate`, `BearerAuth.Invalidate`,
  `AnthropicAuth.Invalidate`, and `noOpAuth.Invalidate` are empty no-op
  method bodies required to satisfy the `Authenticator` interface contract.
  Go's coverage instrumentation does not count empty method bodies as
  covered. These are called by production code via the interface and
  exercised by `TestOtherAuthenticators`, `TestNoOpAuth`, and
  `TestAuthInvalidate_Additional`, but the coverage tool reports 0%.
  Same acceptance class as `agenttest/helpers.go` interface-satisfying
  stubs and `mockFileSystem.Chmod`.
- **See**: `internal/infrastructure/auth/auth.go` (package-level coverage note
  at top of file), lines 177, 192, 206, 309

### domain/config/watcher_noop.go — SetPaths and Refresh no-op stubs at 0%

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `noOpConfigWatcher.SetPaths` and `noOpConfigWatcher.Refresh`
  are empty no-op method bodies required to satisfy the `ConfigWatcher`
  interface contract. `noOpConfigWatcher` is used as a fallback when no
  watcher is injected (e.g., bare construction for tests). Go's coverage
  instrumentation does not count empty method bodies as covered. Same
  acceptance class as `auth.Invalidate` no-ops and `agenttest/helpers.go`
  interface-satisfying stubs.
- **See**: `internal/domain/config/watcher_noop.go:33-34`

### pkg/clock/clock.go — 36.8% coverage on interfaces + fakes + delegation wrappers

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `internal/pkg/clock` contains only:
  - Two interfaces (`Clock`, `Ticker`) — no executable code.
  - `RealClock` — thin delegation wrappers around `time.Now()`, `time.Sleep()`,
    `time.After()`, `time.Since()`, `time.NewTicker()`. Testing these would
    test the Go standard library, not project code. Same acceptance class as
    `domainFS.Chmod`, `mockFileSystem.Chmod`, and `plainOSFS.Chmod`.
  - `FakeClock` and `FakeTicker` — test-only fakes with deterministic behavior
    for use by other packages' tests. Testing a test double is circular and
    provides no value. Same acceptance class as `mockFileSystem.Chmod`.
  - `realTicker` — unexported adapter struct wrapping `*time.Ticker` for
    interface satisfaction. No business logic.
  The package exists to enable deterministic time control in tests across the
  project. Every line of code is either an interface definition, a stdlib
  delegation, or a test fake. There is no business logic to cover.
- **See**: `internal/pkg/clock/clock.go`

### ui/tui/progress/renderer.go — glamour render error paths

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The two glamour error paths in `makeMarkdownRenderer` —
  `glamour.NewTermRenderer` (line 81) and `cachedRenderer.Render` (line 89) —
  return the original unrendered markdown text as a cosmetic degrade. Glamour
  only fails on invalid options or internal library errors, not on valid
  markdown input. These are cosmetic fallbacks (raw markdown instead of
  ANSI-rendered text in the TUI) and do not affect correctness. Structurally
  unreachable in normal operation — same acceptance class as `json.Marshal`
  on all-string structs in `global_prompt_tracker.go`.
- **See**: `internal/ui/tui/progress/renderer.go:81-83, 88-90`

### ui/tui/progress/model.go — renderMarkdownAsync goroutine body at 13.3%

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `renderMarkdownAsync` constructs a `tea.Cmd` closure that
  calls `glamour.NewTermRenderer` and `tr.Render` inside a goroutine managed
  by the Bubble Tea runtime. Unit tests only exercise the closure-creation
  line (13.3%); the goroutine body — including both glamour error fallback
  paths that return plaintext on render failure — never executes
  synchronously in test code. These are the same cosmetic fallback paths
  already documented for `renderer.go` (plaintext instead of ANSI-rendered
  text). Testing the goroutine body would require a full TUI integration
  harness with `tea.NewProgram` to trigger `mdRenderCompleteMsg`. Same
  acceptance class as `glamour render error paths` in `renderer.go`:
  structurally unreachable in normal operation, cosmetic degrade only.
  Re-anchored 2026-09 (#1431): lines drifted 746 → 669-676 as the
  #1419-style render-path additions grew the file above the function.
- **See**: `internal/ui/tui/progress/model.go:669-676` (`renderMarkdownAsync`)

### ui/renderer.go — SetWordWrap renderer rebuild error branch

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The `if err != nil` branch in `stdUIRenderer.SetWordWrap` after
  `NewMarkdownRenderer` is structurally unreachable: the renderer is rebuilt
  with only `WithStandardStyle`, `WithEmoji`, and an optional `WithWordWrap`
  option, none of which can fail. On the impossible error the previous
  renderer is kept rather than degrading (consistent with ADR-007). Same
  acceptance class as the cataloged glamour error paths in
  `ui/tui/progress/renderer.go` (structurally unreachable, cosmetic degrade
  only). Forcing coverage would require changing SetWordWrap's signature to
  accept a failing option — disproportionate to the value.
- **See**: `internal/ui/renderer.go:258-273` (SetWordWrap)

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
  test-double sub-packages (agenttest/, orchestratortest/, configtest/,
  analysistest/, clitest/, eventstest/, persistencetest/, toolstest/). The
  directory name `testing/` clearly signals its
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

### Split internal/agent façade (chatService vs lifecycle)

- **Status**: REJECTED by architect (2026-08, issue #1299 architecture grilling)
- **Rationale**: The "façade mixing four responsibilities" premise is false —
  `internal/agent/agent.go` is one cohesive runtime owner (25 fields;
  init/config/Chat/Shutdown/drift form one operational surface on one stateful
  object), `chatService` is already type-separated with zero references to the
  `*agent` type, and `agent → session/context` is documented ADR-026 intent, not
  a bypass — all four candidate implementations of "route through session" fail.
  A package move to `internal/application/chat` is mechanically coherent but is
  metric-relabeling (fan-in is 8 production files vs domain/ports at 102),
  creates an application→agent/subpackage half-state, and double-churns the
  same consumers if followed by the ports split. Every cited metric was wrong
  or normal ("14 imports" is agent.go's fan-out and unremarkable vs
  container.go's 18; max cyclomatic complexity = 8; the test bridge at 11
  methods is within ADR-037's envelope, trigger ≥13). The one real finding:
  16 of 25 fields are init-only scaffolding (the `executor` field is
  write-only) — ADR-007's accepted cost, now quantified; clarity-only, worth a
  comment fix, not a refactor. The concrete `*sessctx.Manager` type coupling
  (four holders: agent, orchestrator Engine, orchestrator Turn, and
  session.InternalTools — note `TurnState.Metadata` at engine_types.go:170 is a
  Metadata holder, not a Manager holder) is agent-layer and not cross-layer:
  under the cross-layer criterion (ADR-056) it belongs at its consumer and, by the
  clarity-only standard, it is an accepted-cost entry, not a refactor and not a
  ports task.
- **See**: `internal/agent/agent.go:39,44,104-149`, `internal/agent/service.go`,
  `internal/agent/internal_bridge.go`, `internal/agent/session/context/doc.go:9`,
  `internal/agent/orchestrator/engine.go:16,38,65,169`,
  `internal/agent/orchestrator/engine_types.go:170`,
  `internal/agent/session/internal_tools.go:30,37,178`,
  `internal/infrastructure/factory/chatter_test.go:29,32`,
  `internal/agent/agentinternal/doc.go`,
  `docs/adr/2026-04-session-context-subpackage-extraction.md` (ADR-026)

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
  Re-anchored 2026-09: sqlite_store RowsAffected refs 200/218 → 207-209/224-226 (actual gap lines); state.go:56 ref dropped (AtomicWrite branch covered).
- **See**: Inline architect-acceptance comments at each gap site:
  `internal/tools/analysis/propagate_named_interface_assertions.go:62`,
  `internal/tools/analysis/propagate_transitive.go:32`,
  `internal/tools/analysis/propagate_transitive.go:128`,
  `internal/tools/workspace/process_executor.go:118`,
  `internal/tools/workspace/process_executor.go:356`,
  `internal/tools/workspace/pipeline.go:56`,
  `internal/tools/analysis/complexity.go:125`,
  `internal/infrastructure/persistence/sqlite_store.go:207-209,224-226`,
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
- **See**: `internal/domain/services/task_service.go:101-103`

### agent/service.go — EditLastTurn delegation wrapper

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `chatService.EditLastTurn` is a pure delegation wrapper that
  calls `s.HistoryEditor.Edit(ctx, hManager)`. The underlying `HistoryEditor.Edit`
  method is exercised by its own tests; testing a pass-through method provides
  no value. Same acceptance class as `AppendTask`, `domainFS.Chmod`,
  `mockFileSystem.Chmod`, `plainOSFS.Chmod`, and `HasBareNewline` — all
  delegation wrappers already documented in this file.
- **See**: `internal/agent/service.go:189-191`

### persistence/state.go — NewSessionStateFromEnv thin entry point at 0%

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `NewSessionStateFromEnv` is a thin production entry point that
  reads `os.Getenv("STORAGE_TYPE")` with a fallback to `"sqlite"`, then
  delegates to `newSessionState`. The `newSessionState` function is well-tested
  directly; `NewSessionStateFromEnv` is used at exactly one call site
  (`di/config.go:55`) as the default factory in `DefaultBootstrapperConfig`.
  Testing it would require `os.Setenv`/`os.Unsetenv` manipulation and a real
  SQLite database for the non-"memory" path — disproportionate to the value
  of covering a 2-line env-reader + delegation wrapper. Same acceptance class
  as `AppendTask` and `domainFS.Chmod`.
- **See**: `internal/infrastructure/persistence/state.go:208-214`,
  `internal/infrastructure/di/config.go:55`

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
- **See**: `internal/agent/orchestrator/middleware.go:205-210` (re-anchored 2026-08, #1327 T5: unused reset() method deleted)

### skills.sh integration — structurally unreachable and fault-injection gaps (17 sites)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Following the established acceptance classes from the 2026-07
  batch triage, the remaining gaps in the skills.sh integration package
  (`internal/tools/integrations/skillssh/`) fall into these categories:

  **Structurally unreachable (8 sites)**:
  - `searchGitHubAPI`: `http.NewRequestWithContext` error — URL constructed from
    `url.QueryEscape`, unreachable for GET.
  - `searchGitHubAPI`: `io.ReadAll` error — requires transport-level corruption.
    Same class as the process_executor acceptance.
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
  - `InstallSkill`: `os.MkdirAll` error — same class as filesystem
    fault-injection gaps.
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

- **See**: `internal/tools/integrations/skillssh/manager_impl.go:50-52,60-62,75-77,102-104,129-131,141-146,206-208,312-312,330-332,342-344,360-362,366-368,371-373,381-381,397-397`, `internal/tools/integrations/skillssh/search.go:78-80,83-85,88-90,93-95`, `internal/tools/integrations/skillssh/tools.go:23-25,27-32,35-50,53-62,65-81,84-100,102-102`

---

## Coverage Gaps (ACCEPTED — 2026-09 catalog hygiene)

### llm/failover.go — FailoverGateway.ExtractDocument delegation wrapper

- **Status**: ACCEPTED (2026-09)
- **Rationale**: one-line delegation to clients[0].Client.ExtractDocument; testing a pass-through provides no value — same acceptance class as AppendTask / domainFS.Chmod (delegation-wrapper).
- **See**: `internal/infrastructure/llm/failover.go:131-133`

### llm/resilient_client.go — resilientClient.ExtractDocument delegation wrapper

- **Status**: ACCEPTED (2026-09)
- **Rationale**: one-line delegation to the underlying client; testing a pass-through provides no value — same acceptance class as FailoverGateway.ExtractDocument (delegation-wrapper).
- **See**: `internal/infrastructure/llm/resilient_client.go:106-108`

### llm/provider_health.go — getPingEndpoint panic and buildRequest error branch (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: getPingEndpoint's switch is exhaustive over the 3-value APIFamily (//exhaustive:enforce) — the trailing panic is unreachable; consequently buildRequest's `if err != nil` branch consuming getPingEndpoint's (never-returned) error is equally unreachable. Same acceptance class as llm/factory.go createAuthenticator default-case (structurally-unreachable).
- **See**: `internal/infrastructure/llm/provider_health.go:238`, and the `if err != nil` branch at `provider_health.go:126-129`

---

## Test Complexity (ACCEPTED)

### internal/infrastructure/llm/factory_test.go — assertMissingKeysResult (CC=13)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Table-driven test helper asserting per-key error results
  across multiple missing-key scenarios (multiple API response shapes).
  CC comes from case enumeration and assertion boilerplate, not branching
  business logic. Same acceptance class as `TestHydrateMediaAssets` (CC=13) —
  assertion boilerplate across a coverage matrix. Re-anchored 2026-08 (#1350 item 4): line drifted 211→212 as the llm→auth inversion added a ports import to factory_test.go. Re-anchored 2026-09: line drifted 212→214 as two kimi rows were added to `TestCreateAuthenticator_Strategies` (2026-09 factory seams triage).
- **See**: `internal/infrastructure/llm/factory_test.go:214`

### internal/agent/orchestrator/engine_phases_test.go — TestRecoveryStep_EmptyResponse_RetriesUpToLimit (CC=13)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Sequential test driving the empty-response retry branch to
  its retry limit; steps mutate shared mock state across iterations. CC is
  assertion boilerplate, not branching business logic. Same acceptance
  class as its production counterpart `(*RecoveryStep).Process` (CC=12,
  already ACCEPTED) — sequential state-mutation test where splitting would
  duplicate setup. CC increased from 12→13 (re-verified 2026-08) after a
  coverage subtest was added to exercise the RecoveryStep 2s fallback delay —
  still the same acceptance class (test-complexity, assertion boilerplate).
- **See**: `internal/agent/orchestrator/engine_phases_test.go:532`

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
- **See**: `tests/e2e/history_flags_test.go:161`

### cmd/tell-me-go/main.go — buildApp os.Getwd error path (non-Linux)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: The `wd = "."` fallback when `os.Getwd()` fails is only
  reachable on Linux via the chdir-into-deleted-directory technique used by
  `TestBuildApp_GetwdError`. On macOS and Windows, the kernel caches the
  working directory path, making the error structurally unreachable.
  Same acceptance class as platform-specific branches.
  Pin added 2026-09 (#1431): the gap at main.go:184-186 surfaced as
  uncataloged because the See ref was file-only; the coverage matcher
  requires a line spec.
- **See**: `cmd/tell-me-go/main.go:184-186` (`buildApp`, `os.Getwd` error branch),
  `cmd/tell-me-go/main_test.go` (`TestBuildApp_GetwdError`)

### agent/agent.go — configRefreshHook.BeforeTurn/AfterTurn no-op stubs

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `configRefreshHook` implements `orchestrator.TurnHook` to
  inject config hot-reload at phase transitions. `BeforeTurn` and `AfterTurn`
  are empty because the hook only needs `OnPhaseTransition`. The empty methods
  exist solely to satisfy the interface contract. Same acceptance class as
  the `agenttest/helpers.go` interface-satisfying stubs.
- **See**: `internal/agent/agent.go:383-384`

### agent/session/internal_tools.go — prodHeartbeatHooks.onTick() no-op at 0%

- **Status**: ACCEPTED (2026-07)
- **Rationale**: `prodHeartbeatHooks.onTick()` is a production no-op stub
  that satisfies the `heartbeatHooks` interface. The `emitHeartbeats` method
  calls `t.hooks.onTick()` on each ticker cycle; in production the hook is
  a no-op, while tests inject a mock to observe ticker events. Go's coverage
  instrumentation does not count empty method bodies as covered. Same
  acceptance class as `auth.Invalidate` no-ops and `agenttest/helpers.go`
  interface-satisfying stubs.
- **See**: `internal/agent/session/internal_tools.go:23-26`

### internal/infrastructure/mcp/client_test.go — TestBasicAuthTransport (CC=11)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Table-driven transport test pinning the Basic auth header contract across three subtest closures (header set with exact base64, no-header when username empty, no-header when token empty), each building its own roundTripFunc assertion closure. CC comes from subtest enumeration and assertion boilerplate, not branching business logic. Same acceptance class as `TestMediaBlocks` (CC=11) — assertion boilerplate across a coverage matrix. Re-anchored 2026-09: 645→646 (+1 import shift in client_test.go).
- **See**: `internal/infrastructure/mcp/client_test.go:646`

### internal/infrastructure/di/mcp_factory_test.go — TestMCPFactory_ConcurrentBuild (CC=11)

- **Status**: ACCEPTED (2026-08, #1396)
- **Rationale**: Channel-gated concurrency proof for the factory's phase-2 construction: per-server fake calls block on release channels while a started-channel deadline select proves all N constructions entered before any completed. CC comes from harness/assertion boilerplate (deadline select, started-counter check, per-server dep assertions), not branching business logic. Splitting would duplicate the channel-gated harness. Same acceptance class as `TestGetModelTurn` (CC=16) and `TestFakeToolchainRunner_PresetValues` (CC=28). Re-anchored 2026-09: 683→684 (+1 import shift in mcp_factory_test.go).
- **See**: `internal/infrastructure/di/mcp_factory_test.go:684`

### internal/infrastructure/di/mcp_factory_test.go — TestMCPFactory_ConcurrentBuild_FailingServerSkips (CC=15)

- **Status**: ACCEPTED (2026-08, #1396)
- **Rationale**: Same channel-gated harness as `TestMCPFactory_ConcurrentBuild` plus sequential setup and per-server assertions over N servers (failing server absent, others present, exactly one `mcp_client_init_failed` log). CC is harness/assertion boilerplate, not branching business logic. Splitting would duplicate the expensive harness. Same acceptance class as `TestFakeToolchainRunner_PresetValues` (CC=28). Re-anchored 2026-09: 743→744 (+1 import shift in mcp_factory_test.go).
- **See**: `internal/infrastructure/di/mcp_factory_test.go:744`

### internal/infrastructure/mcp/stdio_client_integration_test.go — TestStdio_RoundTrip (CC=11)

- **Status**: ACCEPTED (2026-08, #1396)
- **Rationale**: Sequential integration test over a real child process (list tools → echo round-trip → stderr-capture poll → stderr tool call). CC is multi-step assertion boilerplate and the deadline-bounded stderr poll, not branching business logic. Splitting would duplicate the expensive child-process setup per subtest. Same acceptance class as `TestGetModelTurn` (CC=16) and `TestHistoryNavigation_CompleteWorkflow` (CC=15).
- **See**: `internal/infrastructure/mcp/stdio_client_integration_test.go:112`

### internal/infrastructure/mcp/stdio_client_integration_test.go — TestStdio_LauncherTreePassThrough (CC=13)

- **Status**: ACCEPTED (2026-08, #1396)
- **Rationale**: Sequential launcher test (spawn → stderr poll for pid → ListTools → Close → deadline-bounded liveness poll with `syscall.Kill`). CC is the poll/assertion sequence, not branching business logic. Splitting would duplicate the child-process/launcher setup. Same acceptance class as `TestHistoryNavigation_CompleteWorkflow` (CC=15) — sequential workflow where steps depend on prior state. Re-anchored 2026-09 (#1407): TestStdio_EnvCasePreserved inserted above drifted the See: from 216 to 262 (CC unchanged at 13).
- **See**: `internal/infrastructure/mcp/stdio_client_integration_test.go:262`

### internal/infrastructure/mcp/stdio_client_integration_test.go — TestStdio_ChildDeathMidSession (CC=13)

- **Status**: ACCEPTED (2026-08, #1396)
- **Rationale**: Sequential death-mid-session integration test over a real child process (die call → race-tolerant death-indication assertion → deadline-bounded poll loop that deterministically closes the async-reap window by waiting for the sticky child-exit error → post-sticky pointer-equality assertions). CC comes from the poll/branch structure (the poll's if/break, the deadline guard, and the two sticky assertions), not branching business logic — and the poll is the test's synchronization mechanism, not refactorable: splitting it would both duplicate the expensive child-process setup and destroy the deterministic reaper synchronization it provides. Same acceptance class as `TestStdio_RoundTrip` (CC=11) and `TestStdio_LauncherTreePassThrough` (CC=13) in the same file. CC re-verified 2026-09: 11→13 (ListTools sticky-error extension). Re-anchored 2026-09 (#1407): TestStdio_EnvCasePreserved inserted above drifted the See: from 278 to 324 (CC unchanged at 13).
- **See**: `internal/infrastructure/mcp/stdio_client_integration_test.go:324`

### internal/infrastructure/mcp/stdio_client_test.go — TestResolveCommand (CC=13)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: Table-driven resolution-contract test pinning the COMMAND resolution rules (separator-bearing passthrough, bare-name LookPath, empty-command failure, nonexistent-bare-command annotation). CC comes from case enumeration and assertion boilerplate, not branching business logic. CC rose 10→13 (2026-09) when two not-found subtests were added — still the same acceptance class as `TestGetModelTurn` (CC=16) and `TestMediaBlocks` (CC=11): assertion boilerplate across a coverage matrix.
- **See**: `internal/infrastructure/mcp/stdio_client_test.go:25`

### internal/agent/memory/hook_test.go — TestHookBatch (CC=22)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Sequential state-mutation test over the batch tier:
  AfterTurn appends to the shared ring buffer (no MCP call); FlushSession
  drains under lock, deletes the map entry, and calls plur_learn_batch
  outside the lock; a second flush is a no-op. Steps mutate shared hook
  state and depend on prior steps — splitting would duplicate setup. Same
  acceptance class as `TestGetModelTurn` (CC=16) — test-complexity, not
  branching business logic. Re-anchored 2026-09 (#1410): line drifted
  209→245 as the T2 branch-(i)/(ii)/(iii) tests and the #1410 writeStats/
  payload-contract assertions were appended above; CC re-measured 14→20 —
  the added per-key payload assertions and skip-at-append/flush-contract
  subtests grew the branch count, still the same test-complexity class.
  Re-anchored 2026-09 (#1412): line drifted 245→247 and CC re-measured
  20→22 as the T2 retain-on-failure/drop-count subtests grew the file
  above and inside the test, still the same test-complexity class.
- **See**: `internal/agent/memory/hook_test.go:247`

### internal/agent/memory/hook_test.go — TestHookFull (CC=21)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Sequential full-tier test: frame-match learns, sha256
  dedupe, flood bound, and non-frame suppression — each subtest mutates the
  hook's per-session learn state built up by prior steps. CC is assertion
  boilerplate across dependent steps. Same acceptance class as
  `TestGetModelTurn` (CC=16). Re-anchored 2026-09 (#1410): line drifted
  273→354 as the T2 branch tests and the #1410 payload-contract assertions
  were appended above; CC re-measured 15→21 — the added no-agent/no-
  session_id payload assertions and dead-tool/scope subtests grew the branch
  count, still the same test-complexity class. Re-anchored 2026-09 (#1412):
  line drifted 354→359 as the T2 retain-on-failure/drop-count subtests grew
  the file above it; CC re-verified 21 (unchanged).
- **See**: `internal/agent/memory/hook_test.go:359`

### internal/agent/memory/hook_test.go — TestHookCaptureBranchI (CC=13)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Two-subtest wire-contract test over branch (i) (Response
  present): with and without an error annotation, asserting the `plur_capture`
  payload is exactly `{summary, agent, session_id}` with the text-presence
  discriminator (text wins over the error annotation) and that
  `text`/`error`/`prompt` keys are absent (issue #1410). CC comes from
  per-key assertion boilerplate (summary/agent/session_id checks + the
  absent-key loop in each subtest), not branching business logic. Same
  acceptance class as `TestHookBatch` (CC=20) — test-complexity, assertion
  boilerplate across a coverage matrix. Grew over the CC=10 threshold in
  2026-09 (#1410) when the exact-payload assertions were added (T2).
  Re-anchored 2026-09 (#1412): line drifted 59→61 as the T2 retain-on-
  failure/drop-count subtests grew the file above it; CC re-verified 13
  (unchanged).
- **See**: `internal/agent/memory/hook_test.go:61`

### internal/agent/memory/injector_test.go — TestInjectorEnabledInsert (CC=22)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Table-driven injector test across the enabled-path insertion
  behaviors — prepend when no system Content, append to an existing system
  Content, replace an existing marker Part, at most one memory block — plus
  trim and warning surfaces. CC comes from case enumeration and assertion
  boilerplate, not branching business logic. Same acceptance class as
  `TestHydrateMediaAssets` (CC=13) — assertion boilerplate across a coverage
  matrix. Re-anchored 2026-09 (memory round 2): line drifted 108→109 as the
  TASK-E helper tests (metadataIDs/lastUserText/joinTextParts) added the
  reflect import above it; CC re-verified 22 (unchanged).
- **See**: `internal/agent/memory/injector_test.go:109`

### internal/app/chatter_test.go — TestNewChatter_MemoryClientLookup (CC=12)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Subtest enumeration over the memory-lookup seam
  (configured → client returned, skipped → nil-degrade, empty → not
  called) with per-subtest setup — assertion boilerplate across a coverage
  matrix. CC is subtest enumeration, not branching business logic. Same
  acceptance class as `TestGetModelTurn` (CC=16).
- **See**: `internal/app/chatter_test.go:226`

### tests/e2e/testdata/fakeplur/main.go — newServer (CC=13)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Testdata helper constructor for the fake plur MCP server:
  config branching for store/seed loading and the tool dispatch table
  setup — structural setup branching in a testdata helper binary, not
  business logic. Same acceptance class as `createPrecisionWorkspace`
  (CC=12, test-fixture builder). Re-anchored 2026-09 (#1410): line drifted
  104→142 as the reject-tools/required-param plumbing grew the file above
  it (T5 measurement); CC re-verified 13 (unchanged). Re-anchored 2026-09
  (#1412): line drifted 142→179 as the T1 delayFile/delayTool server fields
  and FAKE_PLUR_DELAY_* env wiring grew the file above it; CC re-verified
  13 (unchanged).
- **See**: `tests/e2e/testdata/fakeplur/main.go:179`

### tests/e2e/testdata/fakeplur/main.go — (*server).dispatchTool (CC=14)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Testdata helper dispatch for the fake plur MCP server:
  reject-map guard, the T1 delay guard (`delayFor` re-read per call,
  issue #1412 — sleeps and returns isError so the store is never mutated
  by a call the client already abandoned), required-param presence/empty-
  value loop, then the 6-case tool switch (inject/recall, learn, capture,
  learn_batch, status, default). CC is dispatch-structural (switch/branch
  count + guard loop), not branching business logic — same class as
  `handleDomainEvent` (CC=12) and `newServer` (CC=13) in the same testdata
  helper binary. Grew over the CC=10 threshold in 2026-09 (#1410) when the
  reject-tools guard, the required-param empty-value rejection, and the
  `plur_learn_batch` case were added (T5). Re-anchored 2026-09 (#1412):
  line drifted 381→419 as the T1 delayFile/delayTool plumbing grew the file
  above it; CC re-measured 13→14 — the delay guard adds one decision point,
  still the same dispatch-structural class.
- **See**: `tests/e2e/testdata/fakeplur/main.go:419`

### tests/e2e/memory_live_test.go — TestLivePlurCapturePersists (CC=12)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Live E2E persistence leg (behind `-tags=e2e_live`, never in
  `make check-full` — ADR-068 §8): precondition probes (npx present,
  handshake OK) skip the leg, then a sequential CLI run (capture tier) and a
  fresh-process after-read assert the captured marker round-trips through
  the real server's store. CC is precondition/assertion boilerplate across
  the sequential live-server workflow, not branching business logic. Same
  acceptance class as `TestHistoryNavigation_CompleteWorkflow` (CC=15) —
  sequential workflow where steps depend on prior state; splitting would
  duplicate the expensive live-store setup. New 2026-09 (#1410, T7).
- **See**: `tests/e2e/memory_live_test.go:285`

### tests/e2e/e2e_test.go — newestSourceMTime (CC=13)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: Test helper computing the newest ModTime among the CLI
  binary's inputs (non-test `.go` files + go.mod/go.sum under root) for
  build-staleness checks: a WalkDir closure with `.git`/`vendor`/`testdata`
  dir-skip guards and per-file mtime comparison. CC is structural
  walk-guard branching in test infrastructure, not business logic. Same
  acceptance class as `createPrecisionWorkspace` (CC=12) — test-fixture
  helper where the branching is traversal guards. New 2026-09 (#1410, T7 —
  added for the live-leg rebuild check).
- **See**: `tests/e2e/e2e_test.go:79`

---

## Complexity Alerts (ACCEPTED)

### ui/tui/progress/model.go — handleDomainEvent (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: This is a 10-case type-switch dispatching domain events in a
  Bubble Tea `Update` handler. Each case is a clean one-liner delegation to a
  dedicated handler method:

  ```go
  case events.ToolCallEvent:
      return m, m.handleToolCallEvent(e)
  case events.ToolResultEvent:
      return m, m.handleToolResultEvent(e)
  // ... 8 more identical patterns
  ```

  The cyclomatic complexity is inherent to the type-switch dispatch pattern —
  each case adds 1 to CC. There is zero branching business logic within the
  switch body. Refactoring into a `map[reflect.Type]eventHandler` registry
  (mirroring what `eventDispatcher` already does in
  `internal/agent/session/ui/dispatcher.go`) would trade one well-understood
  Go pattern for another with no reduction in cognitive complexity. The code
  is already clean and well-structured.
- **See**: `internal/ui/tui/progress/model.go:339`

### agent/session/ui/dispatcher.go — handleToolEvents (CC=10)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: This is a two-case type-switch (`ToolCallEvent` /
  `ToolResultEvent`) where each case contains a duplicate `turnStartTime` guard
  pattern:

  ```go
  // When turn-time tracking is active, skip stop/resume to preserve elapsed-time counter.
  if !d.spinner.turnStartTime.IsZero() {
      d.renderer.LogToolCall(...)
      return
  }
  d.spinner.stopActiveSpinner()
  d.renderer.LogToolCall(...)
  if d.spinner.resumeActiveSpinner(...) { ... }
  ```

  The duplication is intentional and documented by inline comments: when
  `turnStartTime` is active, the spinner must not be stopped mid-turn.
  Extracting the pattern into a helper would obscure the distinct renderer
  method called per case (`LogToolCall` vs `LogToolResult`), and the two
  cases have slightly different guard clauses (nil-check vs empty-name-check).
  The complexity is structural, not cognitive — the same pattern appears in
  `handleUsageMetrics`, `handleTurnStatus`, `handleResponse`, and
  `handleSystemMessage`, each with identical comment documentation.
- **See**: `internal/agent/session/ui/dispatcher.go:147`

### Acceptance Rationale (Shared)

Both functions follow the same acceptance class as
`TestHistoryNavigation_CompleteWorkflow` (CC=15, already documented in the
Test Complexity section above): cyclomatic complexity driven by dispatch/branch
count, not by branching business logic. The cost of refactoring (extracting
dispatch registries, collapsing spinner-guard patterns) outweighs the
maintainability benefit. The code is already clean, well-commented, and easy
to reason about.

### ui/renderer_spinner_test.go — TestStartSpinnerLifecycle (CC=17)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Sequential lifecycle test with 4 subtests ("starts, ticks multiple frames,
  and stops cleanly", "stop is idempotent", "spinner does not run when not in
  terminal context", "context cancellation stops spinner and clears indicator").
  Steps are not independent — each subtest builds on the spinner lifecycle
  (start → tick → stop). The CC=17 comes from assertion boilerplate
  (`t.Fatalf`, `t.Errorf`, `runtime.Gosched` poll loops), not branching
  business logic. Splitting would duplicate the expensive setup
  (`MockClockWithTicker` + `SetForceSpinner`) per subtest. Same acceptance
  class as `TestHistoryNavigation_CompleteWorkflow` (CC=15, already documented
  in the Test Complexity section above).
- **See**: `internal/ui/renderer_spinner_test.go:176`

### tools/analysis/index_snapshot_test.go — (*indexer).snapshot (CC=14)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Production method defined in a `_test.go` file (package `analysis`,
  not `analysis_test`) because it needs access to unexported indexer fields
  (`idx.mu`, `idx.pkgs`, `idx.symbolsByPath`, `idx.usagesByName`,
  `idx.implsCache`). It deep-copies the indexer's complete internal state into
  an `indexSnapshot` struct for test-fixture generation. CC increased from 11→14
  since acceptance (re-verified 2026-08) — still a test-fixture builder. The CC=14
  comes from 5 deep-copy loops (`fileToPkg`, `decls`, `implsCopy`, `symbolsCopy`,
  `usagesCopy`) + 3 guards (`modulePath` discovery, `CompiledGoFiles` dedup,
  `collectDeclarations` filter). Only called from `fixture_gen_test.go:58` —
  pure test infrastructure. Splitting the copy loops into helpers would
  fragment a coherent snapshot operation for cosmetic CC reduction with no
  maintainability benefit. Same acceptance class as `createPrecisionWorkspace`
  (CC=12, test-fixture builder) and the type-switch dispatch entries in this
  section.
- **See**: `internal/tools/analysis/index_snapshot_test.go:112`,
  `internal/tools/analysis/fixture_gen_test.go:58`
---

### ui/tui/progress/model.go — (*model).Update (CC=14)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: This is the standard Bubble Tea `Update` method — a type-switch
  dispatching 8 message types (`KeyMsg`, `WindowSizeMsg`, `spinnerTickMsg`,
  `mdRenderCompleteMsg`, `channelClosedMsg`, `error`, `domainEventMsg`, `default`),
  each delegating to a dedicated handler. The CC is driven by the type-switch
  (+8) + 3 guard conditions (`sessionComplete` check, `showMetrics` sampling,
  `renderGeneration` staleness). Each case is a clean one-liner delegation.
  Extracting guards into helper methods would fragment a well-understood dispatch
  function with no cognitive benefit. Same acceptance class as `handleDomainEvent`
  (type-switch dispatch pattern where CC is structural, not from branching
  business logic).
- **See**: `internal/ui/tui/progress/model.go:252`

### ui/tui/progress/renderer.go — (*renderer).makeSubscriber (CC=11)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: This is a 3-case type-switch (`TurnStarted`/`TurnStatusEvent`,
  `ResponseEvent`, `default`) where each case follows an identical
  timer-guarded channel-send pattern with different timeout values (5s for
  control-plane, 100ms for display-plane, 50ms for best-effort). The CC is
  driven by the type-switch (+3) + `select` multi-case statements (+3 per
  branch). Each branch is structurally identical and self-documenting via
  inline comments. Extracting a `sendWithDeadline` helper would add indirection
  for a 3-line pattern with no cognitive benefit. Same acceptance class as
  `handleDomainEvent` (structural dispatch CC, not branching business logic).
- **See**: `internal/ui/tui/progress/renderer.go:48`

### ui/tui/history_editor.go — (*HistoryEditor).Edit (CC=10)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Sequential TUI orchestration with error handling at each step:
  initLogger guard, GetLastModelTurn error, part-aggregation loop (`for range`
  + `if IsThought` + `else if Text`), `p.Run()` error, type-assertion guard,
  and `WasAborted()` check. Every branch is a single-line error return or
  delegation. The CC is structural (error guards + one aggregation loop), not
  branching business logic. Extracting the 3-line part-aggregation loop into a
  helper would fragment a coherent sequential function for cosmetic CC reduction
  with no cognitive benefit. Same acceptance class as `handleDomainEvent`
  (type-switch dispatch), `handleToolEvents` (spinner-guard pattern), and
  `(*model).Update` (Bubble Tea dispatch).
- **See**: `internal/ui/tui/history_editor.go:39`

### ui/history.go — renderHistory (CC=12 — increased from 11)

- **Status**: ACCEPTED (2026-08, PR #1294 — WRAP_WIDTH -l history width)
- **Rationale**: Sequential history renderer with structural guards only:
  empty-history early return, `n > total` clamp, `GetWindow` error branch,
  `Raw`/`CustomRenderer` option branches (markdown renderer selection), and
  per-part nil guards inside the two loops. Every branch is a single-line
  guard, error return, or renderer assignment — zero branching business logic.
  The CC is driven by the guard count (+8) and the two iteration loops (+2),
  not by decision complexity. Extracting guards into helpers would fragment a
  coherent sequential render for cosmetic CC reduction with no cognitive
  benefit. Same acceptance class as `(*HistoryEditor).Edit` (CC=10, sequential
  orchestration with error guards) and the other structural-dispatch entries
  in this section. Note: the shared `NewMarkdownRenderer` helper (2026-08)
  already removed the duplicated glamour construction from this function.
  CC increased from 11→12 (2026-08, PR #1294) due to the single-line
  `WrapWidth > 0` guard added to the markdown-renderer selection branch —
  the same structural-guard acceptance class as the rest of this function.
- **See**: `internal/ui/history.go:26`

### internal/domain/config/mcp_config.go — (*MCPServerConfig).validate (CC=20) → RESOLVED

- **Status**: RESOLVED (2026-09, refactored below CC=10 threshold)
- **Complexity after fix**: `validateTransportShape` CC=5, `validateCommandFields` CC=5, `validateAuthTransportConflict` CC=4, `validateExecutionLimits` CC=2, `validateAuthMode` CC=3, `validateCredentials` CC=6; `validate` orchestrator CC=6 (was 20).
- **What was refactored**: Extracted the six check groups into private sub-validators called by a thin `validate(name)` orchestrator in the documented most-specific-error-wins order; the six-mode auth switch retained verbatim as a grouped case (no set-lookup).
- **See**: `internal/domain/config/mcp_config.go` (`validate`, `validateTransportShape`, `validateCommandFields`, `validateAuthTransportConflict`, `validateExecutionLimits`, `validateAuthMode`, `validateCredentials`)

### ui/tui/browser.go — (*rootBrowserModel).handleActionKeys (CC=15) → RESOLVED

- **Status**: RESOLVED (2026-09, refactored below CC=10 threshold)
- **Complexity after fix**: `handleActionKeys` CC=7 (was 15), `handleEditKey` CC=9.
- **What was refactored**: Extracted the "e" (edit) key action handling logic into `(*rootBrowserModel).handleEditKey()`.
- **See**: `internal/ui/tui/browser.go` (`handleActionKeys`, `handleEditKey`)

### ui/tui/browser.go — (*rootBrowserModel).handleEditorMsg at 0% → RESOLVED

- **Status**: RESOLVED (2026-09, `TestBrowserHandleEditorMsg_*` suite)
- **Coverage after fix**: `handleEditorMsg` **100%** (was 0%).
- **What was covered**: Sub-model message forwarding, abort on Esc and Ctrl+C, save on Ctrl+S (updating DTO, clearing cache, handling errors and out-of-bounds selection), and View delegation.
- **See**: `internal/ui/tui/browser.go` (`handleEditorMsg`),
  `internal/ui/tui/browser_test.go` (`TestBrowserHandleEditorMsg_*`)

### tools/workspace/shell.go — (*windowsShellWrapper).isPowerShellIndicator (CC=10)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Windows-only helper that detects PowerShell usage indicators
  (bare newlines, PowerShell aliases, Verb-Noun cmdlet patterns, PS-specific
  substrings like `$env:`, `$(`, `select-string`) to decide between
  `powershell`/`pwsh` vs `cmd.exe` shell wrappers. The 10 CC points are
  structural guards (nil-check, alias lookup, hyphen-index bounds, prefix
  validation, loop over substring indicators) — zero branching business logic.
  Not exercisable on Linux dev/CI. Same acceptance class as platform-specific
  branches (already ACCEPTED in Coverage Gaps above) and the type-switch
  dispatch entries in this section.
- **See**: `internal/tools/workspace/shell.go:152`

### CC=9 production cohort — threshold policy (2026-07)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: All non-test production functions at CC=9 were triaged and
  found to be structural — the cyclomatic complexity comes from error guards,
  switch dispatch, and validation checks, not from branching business logic.
  CC < 10 is acceptable by policy. This policy covers approximately 21–23
  functions across `internal/tools/analysis`, `internal/tools/workspace`,
  `internal/infrastructure/...`, `internal/agent/session/...`, and
  `internal/domain/config/...`. Individual entries are added only where
  rationale is non-obvious (see `RecoveryStep.Process` below).
- **See**: Complexity metrics tool output for `./internal/...` (CC ≥ 9 filter,
  excluding `_test.go`, benchmarks, `toolstest` mocks)

### CC=10 boundary watch list (2026-09, issue #1431)

- **Status**: ACCEPTED (2026-09, advisory — no gate change)
- **Rationale**: Seven production functions sit exactly at the policy threshold
  (CC=10). CC ≤ 10 is acceptable by policy (complexity-threshold-policy), so
  no gate change is made; `verify-nonfix-catalog` does not pin them
  (under-threshold pins are enforced at threshold-crossing, by construction).
  **Refactor-on-touch rule**: the next modification to any listed function
  must decompose proactively rather than creep past the CC=10 policy
  threshold — one added decision point makes it an over-threshold alert
  requiring either a refactor or a new ACCEPTED entry. `(*mcpFactory).Build`
  is the highest-risk (DI hot path, already split once in #1396).
- **Watch list** (all CC=10, verified 2026-09):
  `(*mcpFactory).Build` — internal/infrastructure/di/mcp_factory.go:98;
  `resolveServerToken` — internal/infrastructure/di/mcp_factory.go:216;
  `mediaUploadPurpose` — internal/infrastructure/llm/openai/client.go:706;
  `convertObject` — internal/tools/integrations/mcp/schema.go:74;
  `load` — internal/infrastructure/config/config.go:49;
  `redactRawContent` — internal/infrastructure/config/redact.go:173;
  `clauseAFixpoint` — internal/tools/analysis/ports_registry.go:606.
- **See**: complexity metrics tool output for `./internal/...` (CC == 10
  filter, excluding `_test.go`, benchmarks, `toolstest` mocks)

### agent/orchestrator/engine_phases.go — (*RecoveryStep).Process (CC=9)

- **Status**: [SUPERSEDED — see CC=12 entry below] ACCEPTED (2026-07)
- **Rationale**: 5-case classify-switch (`RateLimited`, `ContextOverflow`,
  `AuthFailure`, `ServerError`, `Timeout`) with error guards on each branch.
  Every branch is a single-line delegation or return. The CC is inherent to
  the exhaustive switch dispatch — each case adds 1 CC. The `default:` case
  is already individually ACCEPTED in Coverage Gaps above as structurally
  unreachable (`ClassifyLLMError` returns exactly five known categories).
- **See**: `internal/agent/orchestrator/engine_phases.go:101`

### agent/orchestrator/engine_phases.go — (*RecoveryStep).Process (CC=12 — increased from 9)

- **Status**: ACCEPTED (2026-07, PR #1283 — empty response retry feature)
- **Rationale**: CC increased from 9→12 due to the empty-response retry branch
  (3 structural branches: `errors.Is(..., errEmptyResponse)`, `RetryCount <
  maxEmptyResponseRetries`, `policy.ShouldRetry`/fallback delay). Each branch
  is a single-line delegation or return. Same acceptance class as
  `handleDomainEvent` (CC=12) and `(*model).Update` (CC=14) — structural
  dispatch where CC is switch/branch count, not branching business logic.
- **See**: `internal/agent/orchestrator/engine_phases.go:129`

### internal/agent/memory/hook.go — (*plurHook).AfterTurn (CC=22) → RESOLVED

- **Status**: RESOLVED (2026-09, refactored below CC=10 threshold)
- **Complexity after fix**: `AfterTurn` CC=7 (was 22); helpers `isDuplicateTurn` CC=3, `clientUnavailable` CC=3, `fetchLastModelTurn` CC=5, `buildEpisode` CC=8, `dispatchTier` CC=4.
- **What was refactored**: extracted the turn-scoped dedupe into `isDuplicateTurn`, the nil-client runtime guard into the parameterized `clientUnavailable(phase string)` (AfterTurn phase "learn", FlushSession phase "learn_batch"), the three-way episode classification into `buildEpisode` (branch iii → `fetchLastModelTurn`), and the LEARN-tier dispatch into the mandatory `dispatchTier` shape — an inline tier switch would hold AfterTurn at CC=10 with zero margin.
- **See**: `internal/agent/memory/hook.go` (`AfterTurn`, `isDuplicateTurn`, `clientUnavailable`, `fetchLastModelTurn`, `buildEpisode`, `dispatchTier`)

### internal/agent/memory/hook.go — (*plurHook).maybeLearn (CC=11) → RESOLVED

- **Status**: RESOLVED (2026-09, refactored below CC=10 threshold)
- **Complexity after fix**: `maybeLearn` CC=8 (was 11); helper `claimLearnSlot` CC=5.
- **What was refactored**: extracted the per-session exact-match sha256 dedupe + MAX_LEARNS_PER_SESSION flood bound into `claimLearnSlot` (single lock; both counters updated only after both checks pass); the frame gate, scope presence, write-lock acquire (ADR-068 §4) and err-branch logger stay inline in `maybeLearn`.
- **See**: `internal/agent/memory/hook.go` (`maybeLearn`, `claimLearnSlot`)

### internal/agent/memory/hook.go — (*plurHook).FlushSession (CC=18) → RESOLVED

- **Status**: RESOLVED (2026-09, refactored below CC=10 threshold)
- **Complexity after fix**: `FlushSession` CC=7 (was 18); helpers `claimEpisodes` CC=5, `buildEngramPayload` CC=3, `restoreOnFailure` CC=3, `finalizeOnSuccess` CC=3, `effectiveScope` CC=2.
- **What was refactored**: extracted the claim (snapshot AND remove under one lock, issue #1412) plus the master-switch drain-and-drop gate (issue #1414) into `claimEpisodes`; the engrams mapping loop into the pure `buildEngramPayload`; the failure-path restore + `retained`/`dropped` reporting into `restoreOnFailure` (re-creates the map entry when a concurrent flush deleted it); the success-path entry deletion / drop-counter reset into `finalizeOnSuccess`; the per-engram scope read into `effectiveScope`. The nil-client drain-and-drop delete and the engrams-empty belt-and-suspenders guard stay at the orchestrator level (re-anchored, never deleted).
- **See**: `internal/agent/memory/hook.go` (`FlushSession`, `claimEpisodes`, `buildEngramPayload`, `restoreOnFailure`, `finalizeOnSuccess`, `effectiveScope`)

### internal/agent/memory/injector.go — (*plurInjector).Transform (CC=18) → RESOLVED

- **Status**: RESOLVED (2026-09, refactored below CC=10 threshold)
- **Complexity after fix**: `Transform` CC=8 (was 18); helpers `fetchEngramPayload` CC=6, `applyMemoryTransformation` CC=5, `observeInjection` CC=4, `resolveUserPrompt` CC=1.
- **What was refactored**: Extracted the four strip sites per the ownership contract (disabled-strip+persist stays in Transform; transport-error and result.Error strips move into fetchEngramPayload; maxBody<0 strip-without-insert moves into applyMemoryTransformation; success observability moves to observeInjection) into a new injector_pipeline.go; guard chain cfg-nil → disabled → nil-client preserved.
- **See**: `internal/agent/memory/injector.go` (`Transform`), `internal/agent/memory/injector_pipeline.go`

### internal/agent/memory/injector.go — metadataIDs (CC=11)

- **Status**: ACCEPTED (2026-09, issue #1404)
- **Rationale**: 4-case type-switch + guards normalizing the
  `Metadata["ids"]` value into a string slice — dispatch-structural, same
  class as `handleDomainEvent` (CC=12). Each case is a single-line
  conversion or return; the CC is inherent to the type-switch dispatch
  pattern, not branching business logic. Re-anchored 2026-09 (#1410): line
  drifted 226→237 as the #1410 Transform strip branch grew the file above
  it; CC re-verified 11 (unchanged). Re-anchored 2026-09 (#1419): line
  drifted 237→177 as the Transform decomposition shrank the file above it;
  CC re-verified 11 (unchanged).
- **See**: `internal/agent/memory/injector.go:177`

---

## Test Complexity (ACCEPTED — 2026-07 Supplemental)

### internal/infrastructure/llm/openai/files_test.go — TestPrepareMediaAssets_KimiURL_UploadsVideo (CC=16)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Sequential integration test verifying Kimi-specific video upload pipeline with multiple API response shapes (success, auth failure, server error, retry). Each case mutates shared mock state. Splitting would duplicate the expensive mock server setup. Same acceptance class as `TestHistoryNavigation_CompleteWorkflow`. Re-anchored 2026-08 (#1350 item 4): line drifted 379→378 as the llm→auth inversion swapped the auth import for ports.
- **See**: `internal/infrastructure/llm/openai/files_test.go:379`

### internal/infrastructure/llm/openai/client_vision_test.go — TestHydrateMediaAssets (CC=13)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Table-driven test covering 8 distinct media hydration scenarios (image URL, base64, video, mixed, error paths). CC comes from assertion boilerplate across cases, not branching business logic. Splitting would fragment a coherent coverage matrix. Re-anchored 2026-09 (#1429 T2): line drifted 243→244 as the TestMediaBlocks coverage row was added.
- **See**: `internal/infrastructure/llm/openai/client_vision_test.go:244`

### internal/infrastructure/llm/openai/client_vision_test.go — TestVision_KimiImagePayload (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Provider-specific payload validation test with multiple assertion paths for Kimi's non-standard image format. CC is assertion boilerplate, not branching logic. Same acceptance class as `TestHydrateMediaAssets`. Re-anchored 2026-09 (#1429 T2): line drifted 128→129 as the TestMediaBlocks coverage row was added.
- **See**: `internal/infrastructure/llm/openai/client_vision_test.go:129`

### internal/infrastructure/llm/openai/client_vision_test.go — TestMediaBlocks (CC=11)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Table-driven test covering media block construction across image, video, and mixed content types. CC comes from case enumeration, not branching logic. Same file, same acceptance class as the other vision tests above.
- **See**: `internal/infrastructure/llm/openai/client_vision_test.go:18`

### internal/infrastructure/history/history_test.go — TestUpdateTurnContent_ClearText (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Sequential state-mutation test verifying text clearing and thought preservation through multiple update cycles. Steps are not independent — each mutates shared history state. Splitting would duplicate setup. Same acceptance class as `TestHistoryNavigation_CompleteWorkflow`.
- **See**: `internal/infrastructure/history/history_test.go:1275` (re-anchored 2026-09, +1 import shift in history_test.go)

### internal/infrastructure/history/history_test.go — TestUpdateTurnContent_AddTextWhenNone (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Same sequential state-mutation pattern as `TestUpdateTurnContent_ClearText`, verifying the complementary code path. CC is assertion boilerplate across dependent steps.
- **See**: `internal/infrastructure/history/history_test.go:1362` (re-anchored 2026-09, +1 import shift in history_test.go)

### internal/domain/llm/capabilities_test.go — TestResolveCapabilities (CC=13)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Table-driven combinatorial test covering provider × model × feature capability resolution. CC increased from 12→13 since acceptance (re-verified 2026-08) — still the same acceptance class. CC comes from the 10+ test cases in the table, each a simple assertion. Splitting the table into subtests would obscure the cross-provider comparison intent.
- **See**: `internal/domain/llm/capabilities_test.go:10`

### internal/tools/analysis/precision_test.go — TestDeadCodeAnalyzer_Precision (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: End-to-end precision test verifying dead code analysis across a multi-package fixture workspace. CC comes from per-package assertion blocks. Splitting would require duplicating the expensive `createPrecisionWorkspace` fixture (itself CC=12 and already ACCEPTED).
- **See**: `internal/tools/analysis/precision_test.go:177`

### internal/tools/analysis/precision_test.go — createPrecisionWorkspace (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Test-fixture builder that constructs a multi-package Go workspace with specific symbol structures for dead code analysis. Already referenced as accepted in the `(*indexer).snapshot` (CC=11) entry. CC comes from file creation boilerplate, not branching logic.
- **See**: `internal/tools/analysis/precision_test.go:118`

### internal/tools/analysis/index_fixture_test.go — TestFixtureIndexer_ConstructAndHarvest (CC=13)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: End-to-end fixture test constructing an indexer, loading packages, and harvesting results. CC increased from 11→13 since acceptance (re-verified 2026-08) — still the same acceptance class. CC comes from setup + assertion blocks across the three-phase test (construct → index → harvest). Splitting would duplicate the fixture construction.
- **See**: `internal/tools/analysis/index_fixture_test.go:158`

### internal/tools/workspace/process_executor_stream_test.go — TestRunCommand_NonExitErrorWaitPath (CC=11)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Sequential test exercising the command executor's error-handling paths for non-exit errors (signal, wait failure, context cancellation). Steps build on shared process state. CC is assertion boilerplate across error paths.
- **See**: `internal/tools/workspace/process_executor_stream_test.go:817`

### internal/infrastructure/history/history_test.go — TestGetModelTurn (CC=16)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Table-driven test with 5 subtests covering valid index, OOB negative, OOB pos, non-model role, and deep-copy isolation for `GetModelTurn`. Each subtest contains its own setup (temp dir, `NewManager`, `AddContent`). CC comes from subtest enumeration and assertion boilerplate, not branching business logic. Same acceptance class as `TestUpdateTurnContent_ClearText` (CC=12) and `TestUpdateTurnContent_AddTextWhenNone` (CC=12) in the same file.
- **See**: `internal/infrastructure/history/history_test.go:1446` (re-anchored 2026-09, +1 import shift in history_test.go)

### internal/infrastructure/history/history_test.go — TestHistoryManager_SetPinned_WithFunctionCall (CC=21)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Two-subtest test with a per-subtest `setup` helper that creates a FunctionCall→FunctionResponse turn. The CC=21 is entirely from error-checking boilerplate: `t.Fatalf` on every `AddContent` and `GetWindow` call in setup, plus pin/unpin assertions in each subtest. All branches are guard clauses, not business logic. Same acceptance class as `TestGetModelTurn` (CC=16) and `TestUpdateTurnContent_ClearText` (CC=12).
- **See**: `internal/infrastructure/history/history_test.go:392` (re-anchored 2026-09, +1 import shift in history_test.go)

### internal/infrastructure/history/history_test.go — TestHistoryManager_SetPinned_ViaModelID (CC=12)

- **Status**: ACCEPTED (2026-07)
- **Rationale**: Single-scenario test pinning via model message ID in a plain user→model text turn to close the `contentHasFunctionCall` false-return gap. CC=12 is entirely error-checking boilerplate: `t.Fatalf` on `AddContent`, `GetWindow`, and `SetPinned` calls, plus pin/unpin assertions. Same acceptance class as `TestGetModelTurn` (CC=16) and `TestHistoryManager_SetPinned_WithFunctionCall` (CC=21).
- **See**: `internal/infrastructure/history/history_test.go:494` (re-anchored 2026-09, +1 import shift in history_test.go)

### internal/tools/toolstest/fake_toolchain_runner_test.go — TestFakeToolchainRunner_PresetValues (CC=28)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Table-driven test with 12 subtests (one per ToolchainRunner method) asserting preset-Func returns, call-log recording, and last-Call identity via the assertCallLog helper. CC=28 comes from subtest enumeration and assertion boilerplate, not branching business logic — each subtest is a one-liner closure. Same acceptance class as `TestGetModelTurn` (CC=16) and `TestHistoryManager_SetPinned_WithFunctionCall` (CC=21).
- **See**: `internal/tools/toolstest/fake_toolchain_runner_test.go:36`

### internal/tools/toolstest/fake_toolchain_runner_test.go — TestFakeToolchainRunner_ZeroDefaults (CC=38)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: Table-driven test with 12 subtests (one per ToolchainRunner method) asserting exact zero-value defaults and call-log recording on an unset-Func fake. CC=38 comes from subtest enumeration and assertion boilerplate, not branching business logic. Same acceptance class as `TestFakeToolchainRunner_PresetValues` (CC=28) and `TestHistoryManager_SetPinned_WithFunctionCall` (CC=21).
- **See**: `internal/tools/toolstest/fake_toolchain_runner_test.go:249`

---

## Coverage Gaps (ACCEPTED — 2026-08 pricing-file removal follow-up)

### telemetry/system_metrics_shared.go — runtime metric kind mismatch (errRuntimeMetricKindMismatch)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The `return 0, errRuntimeMetricKindMismatch` branch in the runtime-metrics converter (`system_metrics_shared.go:47`) is a defensive guard against a runtime metric reporting a Kind other than `KindFloat64`. Its companion test `TestGetRuntimeCPUStats_KindFloat64Always` asserts that every consumed runtime metric is `KindFloat64` on the current Go version — the mismatch branch is structurally unreachable today and exists only to guard against future Go runtime changes. Same acceptance class as defensive guards on internal state (2026-07 Batch Triage).
- **See**: `internal/infrastructure/telemetry/system_metrics_shared.go:47`

### di/container.go — skills repository init error paths

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The remaining error branch of `infra_skills.NewFileSkillRepository(skillsDir)` (`container.go:194-197`) requires filesystem fault injection (unreadable skills directory). The sibling `infra_skills.NewSkillsShRepository(skillsShDir)` error branch (`container.go:200-203`) is covered since 2026-09 by `TestBuildSharedSkillRepo_FileRepoFallback` — the only path to the `return fileRepo` fall-through at `container.go:211` passes through it, so it is exercised as a side effect (coverage edge-case batch). Both degrade gracefully — `slog.Warn` + continue without skills, and `slog.Debug` + nil repo — and the happy paths are covered by container tests. Same acceptance class as the filesystem fault-injection gaps in the 2026-07 Batch Triage.
- **See**: `internal/infrastructure/di/container.go:194-197` (file-repo error branch only; skills.sh branch 200-203 covered — see rationale)

---

## Coverage Gaps (ACCEPTED — 2026-08 #1302 emergencySave double-append)

### agent/orchestrator/engine_phases.go — ghost-response path (structurally unreachable)

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The `IsTransient` branch in `PersistenceStep.Process` is structurally
  unreachable for history-store errors — store errors are raw filesystem errors,
  never transient (`IsTransient` matches only `ErrTransient`/`ErrRateLimit`,
  gateway.go:36). The ghost-response path (partial success + hypothetically
  transient store error → recovery re-infers and appends a fresh response while
  the old stays) is therefore NOT reachable today. Same acceptance class as
  defensive guards on internal pipeline state (2026-07 Batch Triage).
- **Note**: The Sync-wrinkle disk-duplicate (store.go:252-283 — write succeeds,
  deferred `Sync` fails) is accepted as out-of-scope for #1302; the durable fix
  belongs in the history store package (fault-injection-required class — fsync
  failure requires real I/O faults).
- **See**: `internal/agent/orchestrator/engine_phases.go:84-97` (architect-acceptance comment + `IsTransient` classification)

---

## Coverage Gaps (ACCEPTED — 2026-08 coverage/complexity hygiene)

### domain/ports/logger.go — NoOpLogger and NoOpTurnsLogger no-op stubs at 0%

- **Status**: ACCEPTED (2026-08)
- **Rationale**: `NoOpLogger.Error/Warn/Info/Debug` (empty bodies) and `NoOpTurnsLogger.HandleEvent`/`Listen`/`Close` exist solely to satisfy the `Logger`/`TurnsLogger` interface contracts. All are already exercised by `logger_test.go` (`TestNoOpLogger`, `TestNoOpTurnsLogger_HandleEvent`, `TestNoOpTurnsLogger_Listen`, `TestNoOpTurnsLogger_Close`, `TestNoOpTurnsLogger_Listen_DeadlineExceeded`); Go's coverage instrumentation does not count empty method bodies as covered (blocks report `count=1` with zero statements). Same acceptance class as the cataloged `NoOpEventBus` and `auth.Invalidate` entries — `interface-stub`.
- **See**: `internal/domain/ports/logger.go:36-39,62-64`, `internal/domain/ports/logger_test.go`

### infrastructure/security/policy.go — BypassConfirmation success path and confirmAction error branch

- **Status**: ACCEPTED (2026-08)
- **Rationale**: The entire success path of `BypassConfirmation` (`SetBypassActive(true)` + `kv.Set("bypass_confirmation","true")` + `Warn` + success return, `policy.go:300-308`) and the `confirmAction` error branch (`policy.go:293-295`) are structurally unreachable: `confirmAction` never returns an error, and it returns `true` only when `sm.IsBypassActive()` is already true — which short-circuits at the entry check (`policy.go:287`). Both checks read the same `bypassActive` field (`manager.go:22,90-100`) under the same `TerminalLock`, so no interleaving exists. The tool can only ever report "already enabled" or auto-decline; bypass is actually enabled via `di/session_factory.go:82-86`. A skipped test already documents the kv.Set path (`policy_test.go:596`, `t.Skip("[UNREACHABLE] ...")`). Same acceptance class as `json.Marshal` on all-string structs — `structurally-unreachable`.
- **See**: `internal/infrastructure/security/policy.go:283-308`, `internal/infrastructure/security/policy_test.go:596`

---

## Coverage Gaps (ACCEPTED — 2026-09 factory seams triage)

### llm/factory.go — default case in createAuthenticator

- **Status**: ACCEPTED (2026-09)
- **Rationale**: The `default:` arm of `createAuthenticator`'s `Family()` switch is structurally unreachable — `Family()` returns exactly one of `APIOpenAI`, `APIAnthropic`, or `APIGemini` (totality enforced by `//exhaustive:enforce`), and all three are handled explicitly. The guard exists only to fail loudly on programmer error. Same acceptance class as `agent/orchestrator/engine_phases.go — default case in RecoveryStep switch`.
- **See**: `internal/infrastructure/llm/factory.go:220-223`


### Test Complexity (ACCEPTED — 2026-09 issue #1378 MCP empty-type schema tests)

### llm/gemini/tools_test.go — TestToSDKSchema_EmptyType_OmitsTypeKey (CC=19)

- **Status**: ACCEPTED (2026-09, issue #1378)
- **Rationale**: Table-driven test pinning the empty-Type → no-wire-type-key contract for Gemini (Go-value pin, wire-omission pin via marshal/unmarshal, description/enum preservation, and the TYPE_UNSPECIFIED-never-set guard per grill correction #3). CC comes from subtest enumeration and assertion boilerplate, not branching business logic. Splitting would fragment a coherent coverage matrix. Same acceptance class as `TestFakeToolchainRunner_PresetValues` (CC=28) and `TestGetModelTurn` (CC=16).
- **See**: `internal/infrastructure/llm/gemini/tools_test.go:20`

### llm/openai/tools_test.go — TestToOpenAISchema_EmptyTypeOmitsKey (CC=11)

- **Status**: ACCEPTED (2026-09, issue #1378)
- **Rationale**: Table-driven test pinning the OpenAI `omitempty` wire fix: an empty-Type node must marshal with no `"type"` key. CC comes from assertion boilerplate (map-unmarshal key-absence, `"type":""` substring guard, description presence check) across subtest cases, not branching business logic. Same acceptance class as `TestFakeToolchainRunner_PresetValues` (CC=28).
- **See**: `internal/infrastructure/llm/openai/tools_test.go:21`

### llm/openai/tools_test.go — TestToOpenAITools_EmptyTypeNested_OmitsEmptyTypeKey (CC=16)

- **Status**: ACCEPTED (2026-09, issue #1378)
- **Rationale**: End-to-end regression test for the issue's exact failure shape (the `run_secret_scanning`-equivalent declaration through `toOpenAITools`): sequential assertions over the marshalled payload — no `"type":""` anywhere, root keeps `"type":"object"`, nested `files` node keeps description without a type key, `required` unpruned. CC is multi-assertion wire-check boilerplate, not branching business logic. Same acceptance class as `TestPrepareMediaAssets_KimiURL_UploadsVideo` (CC=16).
- **See**: `internal/infrastructure/llm/openai/tools_test.go:124`

### tools/integrations/mcp/schema_test.go — TestConvertSchema_NestedUnrepresentable_BecomesUntypedAny (CC=12)

- **Status**: ACCEPTED (2026-09, issue #1378)
- **Rationale**: Table-driven test with 6 cases (the `run_secret_scanning` fixture, nested `anyOf`, nested union, nested `"null"`, nested absent type, enum-only node) pinning the converter-level untyped-ANY contract and unpruned `required`. CC comes from case enumeration and per-case assertion blocks, not branching business logic. Same acceptance class as `TestHydrateMediaAssets` (CC=13).
- **See**: `internal/tools/integrations/mcp/schema_test.go:241`
---

## Coverage Gaps (ACCEPTED — 2026-09 di triage batch)

### di/mcp_factory.go — resolveServerToken default case

- **Status**: ACCEPTED (2026-09)
- **Rationale**: the inline comment documents it: unknown auth modes are rejected by config validation before Build runs, so the default treats them defensively as "none". Defensive guard on internal pipeline state — same acceptance class as the 2026-07 Batch Triage defensive nil/empty guards. Re-anchored 2026-08 (#1389): lines drifted 148-151 → 157-160 as the basic-auth DI resolution added the username parameter to the newClient closure. Re-anchored 2026-08 (#1396): lines drifted 157-160 → 223-227 as the stdio factory refactor replaced the newClient seam with newClientFor and split Build into a two-phase pre-pass + concurrent construction. Re-anchored 2026-09 (#1431): lines drifted 223-227 → 246-249 as the two-phase Build pre-pass comment block grew the file above the switch.
- **See**: `internal/infrastructure/di/mcp_factory.go:246-249`

### di/mcp_factory.go — gh auth token resolver

- **Status**: ACCEPTED (2026-09)
- **Rationale**: the tokenResolver closure shells out to `gh auth token` via exec.Command at the composition root; triggering the error requires `gh` to be missing or failing mid-resolution — environment fault injection disproportionate to the value. Same acceptance class as the 2026-07 fault-injection gaps. Re-anchored 2026-08 (#1396): lines drifted 60-65 → 67-70 as the stdio factory refactor replaced the newClient seam with newClientFor. Extended 2026-09: See widened to 66-71 — the success path (:71) is equally environment-dependent. Re-anchored 2026-09 (#1431): lines drifted 66-71 → 73-78 as the two-phase Build pre-pass grew the constructor above the closure.
- **See**: `internal/infrastructure/di/mcp_factory.go:73-78`

### di/toolchain_factory.go — registerSkillsShTools error

- **Status**: ACCEPTED (2026-09)
- **Rationale**: the branch requires a SkillRepo failure (unreadable skills directory) — same filesystem fault-injection class as the cataloged di/container.go skills repository init error paths entry. Re-anchored 2026-09 (#1431): lines drifted 124-126 → 127-129 as the skillssh registration call-site comment block grew the file above the branch.
- **See**: `internal/infrastructure/di/toolchain_factory.go:127-129`

---

## Coverage Gaps (ACCEPTED — 2026-09 #1387 OpenAI/Kimi file-upload adapter)

### llm/openai/files.go — buildFileUploadBody multipart error branches (structurally unreachable)

- **Status**: ACCEPTED (2026-09, issue #1387)
- **Rationale**: The four error branches in `buildFileUploadBody` — `WriteField` (`files.go:38-40`), `CreateFormFile` (`files.go:46-48`), `fw.Write` (`files.go:51-53`), and `w.Close` (`files.go:57-59`) — cannot fire in correct code: the multipart writer writes into a `*bytes.Buffer` sink, and `bytes.Buffer.Write` never returns an error, so all four APIs funnel into a sink that cannot fail. The `if err != nil` guards exist only for interface contract compliance. Same acceptance class as the cataloged `json.Marshal` entries in `global_prompt_tracker.go` and `orchestrator/middleware.go` detectLoop (`structurally-unreachable`). An architect-acceptance comment sits above each if-statement.
- **See**: `internal/infrastructure/llm/openai/files.go:38-40,46-48,51-53,57-59`

### llm/openai/files.go — uploadFile buildFileUploadBody error propagation (root-caused by Entry A)

- **Status**: ACCEPTED (2026-09, issue #1387)
- **Rationale**: `uploadFile`'s `if err != nil { return "", err }` branch after `buildFileUploadBody` (`files.go:77-79`) propagates the (never-returned) error from the four multipart branches cataloged in Entry A above. Since Entry A's branches are structurally unreachable, this propagation branch is equally unreachable — the same root cause. Same acceptance class as the cataloged `llm/provider_health.go` buildRequest error branch (structurally-unreachable, root-caused by getPingEndpoint's exhaustive switch).
- **See**: `internal/infrastructure/llm/openai/files.go:77-79`

## Coverage Gaps (ACCEPTED — 2026-09 redact hygiene)

### infrastructure/config/redact.go — normalizeKeyToken dead branches (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: The two normalization branches in `normalizeKeyToken` — `strings.TrimPrefix(s, "- ")` (redact.go:52) and `if i := strings.Index(s, ":"); i >= 0 { s = s[:i] }` (redact.go:53-55) — are structurally unreachable through its only call site (`redactRawContent`, redact.go:199): the caller strips any leading "- " sequence prefix via `stripSequencePrefix` (redact.go:185) before the key is extracted, and the argument `rest[:colonIdx]` (redact.go:199) is the slice before the *first* colon of the line, which by construction can never contain ':'. The branches are defensive normalizers for hypothetical callers that do not exist. Same acceptance class as the cataloged `json.Marshal` entries on all-string structs (`structurally-unreachable`).
- **See**: `internal/infrastructure/config/redact.go:53-55` (normalizeKeyToken dead branches; flagged gap 53-55), sole call site `redact.go:199`

## Coverage Gaps (ACCEPTED — 2026-09 triage completion)

### mcp/client.go + stdio_client.go — marshalInputSchema json.Marshal error + propagations (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `marshalInputSchema` json.Marshals the SDK-provided `InputSchema`, which is wire-decoded JSON (`map[string]any` / `RawMessage`) — `json.Marshal` cannot fail on it. The two ListTools propagation branches (`client.go:120-122`, `stdio_client.go:174-176`) root-cause to the same impossible error. Same acceptance class as the cataloged `json.Marshal` entries on all-string structs (`structurally-unreachable`). Re-anchored 2026-09 (#1431): stdio_client.go propagation ref drifted 162-165 → 174-176 as the handshake-timeout normalization block grew the file above ListTools.
- **See**: `internal/infrastructure/mcp/client.go:258-260` (marshalInputSchema body), `:120-122` (Client.ListTools propagation), `internal/infrastructure/mcp/stdio_client.go:174-176` (StdioClient.ListTools propagation)

### mcp/stdio_client.go — StdoutPipe/StdinPipe errors (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: Both pipes are obtained exactly once, before `Start()` (the code comments: "Both pipes must be obtained before Start; any error here leaves nothing to clean up"). `os/exec` `StdoutPipe`/`StdinPipe` only error after `Start` or on a double call — structurally unreachable in correct code. Same acceptance class as the cataloged `process_executor.go` StdoutPipe/StderrPipe entries (`structurally-unreachable`).
- **See**: `internal/infrastructure/mcp/stdio_client.go:82-85,87-90`

### mcp/stdio_client.go — resolveCommand generic LookPath error (defensive guard)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `exec.LookPath` returns `ErrNotFound` for missing commands (covered — the friendly annotated branch at `stdio_client.go:355-357`); the remaining non-ErrNotFound error branch (`:358`) requires environment/path-length edge cases (e.g. ENAMETOOLONG) that callers never hit. Same acceptance class as defensive guards on internal pipeline state (2026-07 Batch Triage). Re-anchored 2026-09 (#1431): ref drifted 347 → 358 as the resolveCommand annotated-error block grew above the generic return.
- **See**: `internal/infrastructure/mcp/stdio_client.go:358`

### mcp/stdio_client.go — Close session.Close error branch (reachable-but-racy; timing fault-injection)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: Empirically proven REACHABLE, not dead: on a child-death flow, if `Close()`'s `session.Close()` runs before the SDK read-loop teardown finishes, it returns "file already closed" and `closeErr` is set (isolated `-count=1` run covers `251-253`; full-suite runs race with teardown → nondeterministic count). Deterministically covering it would require controlling SDK teardown timing — timing fault-injection disproportionate to the value. Same acceptance class as the cataloged `processWatcherEvents` goroutine-timing entry (2026-07 Batch Triage). Re-anchored 2026-09 (#1431): ref drifted 240-242 → 251-253 as the Close reaper-join comment block grew above the session.Close call.
- **See**: `internal/infrastructure/mcp/stdio_client.go:251-253`

### history/global_prompt_tracker.go — prepareCompactedEntries failure branch (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `writeCompactedData` cannot return false when writing to `bytes.Buffer` — `json.Marshal` on the all-string `promptEntry` never fails and `bytes.Buffer.Write` never errors. The inline architect-acceptance comment at `global_prompt_tracker.go:416-419` already documents this. Same acceptance class as the cataloged Append/writeCompactedData entries in the same file (`structurally-unreachable`).
- **See**: `internal/infrastructure/history/global_prompt_tracker.go:420-422`

### security/policy.go — confirmAction error branches at five tool call sites (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `confirmAction` never returns an error (its own error return is already cataloged as structurally unreachable at `policy.go:293-295`). The five call-site `if err != nil { return tools.ToolResult{}, err }` branches — Write (`:95-98`), Remove (`:134-137`), Read (`:192-195`), RemoveRead (`:231-234`), SessionUpdate (`:343-346`) — propagate a never-returned error; same root cause.
- **See**: `internal/infrastructure/security/policy.go:95-98,134-137,192-195,231-234,343-346`

### security/manager.go — filepath.Abs error branches in RegisterSafePath/RegisterReadOnlyPath (defensive guard)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `filepath.Abs` fails only when the current working directory is unavailable (deleted-cwd, Linux-only technique — same acceptance class as the cataloged `cmd/tell-me-go` buildApp `os.Getwd` entry). Caller-supplied paths resolve normally. Defensive guard.
- **See**: `internal/infrastructure/security/manager.go:136-139,161-164`

### persistence/state.go — SetInfo json.MarshalIndent error (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: The cloned `SessionInfo` holds only string/map fields — `json.MarshalIndent` cannot fail. The inline architect-acceptance comment at `state.go:59-63` already documents this (and the AtomicWrite fault-injection sibling). Same acceptance class as the cataloged `json.Marshal` entries (`structurally-unreachable`).
- **See**: `internal/infrastructure/persistence/state.go:65-68`

### telemetry/metrics.go — appendSummaryToLog json.Marshal(summary) (structurally unreachable)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `summary` is `llm.Metrics` — all primitive fields (string/int32/int/float64/bool). `json.Marshal` cannot fail. Same acceptance class as the cataloged `json.Marshal` entries (`structurally-unreachable`).
- **See**: `internal/infrastructure/telemetry/metrics.go:176-178`

### di/toolchain_factory.go — registerSkillsShTools execRunner closure (fault-injection required)

- **Status**: ACCEPTED (2026-09)
- **Rationale**: The `execRunner` closure (real `osexec.CommandContext(...).CombinedOutput()`) is composition-root glue passed to `NewSkillManager`; the skillssh exec paths are exercised with the fake runner in the skillssh package tests. Driving the real toolchain path requires a live git/go environment — integration-level fault injection disproportionate to the value. Same acceptance class as the cataloged registerSkillsShTools error entry in the same file. Re-anchored 2026-09 (#1431): lines drifted 145-147 → 154-156 as the skillssh registration block grew the file above the closure.
- **See**: `internal/infrastructure/di/toolchain_factory.go:154-156`

## Coverage Gaps (ACCEPTED — memory integration interface stubs)

### agent/memory/hook.go — plurHook.BeforeTurn/OnPhaseTransition no-op stubs

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `BeforeTurn` and `OnPhaseTransition` are no-op method bodies
  that exist solely to satisfy the `orchestrator.TurnHook` interface contract
  (compiled-time asserted at `hook.go:330`). Go's coverage instrumentation
  does not count empty bodies as covered (blocks report count=1 with zero
  statements). Same acceptance class as the cataloged
  `configRefreshHook.BeforeTurn/AfterTurn` no-op stubs (`agent/agent.go`) and
  `prodHeartbeatHooks.onTick()` (`agent/session/internal_tools.go`) —
  `interface-stub`.
- **See**: `internal/agent/memory/hook.go:89-93` (BeforeTurn + OnPhaseTransition)

## Coverage Gaps (ACCEPTED — memory integration fault injection)

### agent/memory/flock_unix.go — acquireWriteLock non-EWOULDBLOCK flock error branch

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `syscall.Flock` returns a non-EWOULDBLOCK/EAGAIN error only
  under environment fault injection (filesystem without flock support,
  kernel lock-table exhaustion ENOLCK, or signal interruption EINTR) — none
  deterministically triggerable on the CI filesystem without a production
  seam (an fd is opened and flocked entirely inside `acquireWriteLock`, so
  no test hook exists to inject a bad fd). Fail-open: callers log and
  proceed unlocked (ADR-068 §2). Same acceptance class as the cataloged
  `sqlite_store` Update/Delete `RowsAffected` driver-error and
  `processWatcherEvents` goroutine-timing entries — `fault-injection-required`.
- **See**: `internal/agent/memory/flock_unix.go:52-53`

### agent/memory/hook.go — FlushSession engrams-empty belt-and-suspenders guard

- **Status**: ACCEPTED (2026-09)
- **Rationale**: `if len(engrams) == 0 { return }` (hook.go:241-243) is
  structurally unreachable: episodes are mapped 1:1 into engrams
  (`Statement: ep.Text` — no filter in the mapping loop), skip-at-append
  guarantees every buffered episode has non-empty Text
  (buffer.go:67-69), and the empty-drain `len(episodes) == 0` already
  returned in `claimEpisodes` (hook.go:456-459). The guard is a defensive
  belt-and-suspenders against a future filter being added to the mapping
  loop, kept at the orchestrator level after the build call. Same acceptance
  class as the defensive nil/empty guards on internal pipeline state
  (2026-07 Batch Triage) — `defensive-guard`. Re-anchored 2026-09 (#1419):
  line drifted 361→241 as the hook decomposition shrank FlushSession; guard
  kept at orchestrator level after the build call.
- **See**: `internal/agent/memory/hook.go:241-243`

## Coverage Gaps (ACCEPTED — 2026-09 #1431 medium-gap triage batch)

### Defensive nil/empty guards on internal pipeline state (53 sites)

- **Status**: ACCEPTED (2026-09, issue #1431)
- **Rationale**: nil/empty guards on internal state that callers never produce —
  nil-dependency guards at composition boundaries (`sessionDeps.GetMCPClient`
  toolchain nil check), nil-receiver fail-open guards on value methods, empty-
  collection early returns, and config-scope fail-open fallbacks. Same
  acceptance class as the cataloged defensive nil/empty guards (2026-07 Batch
  Triage). Architect-acceptance comments at representative sites.
- **See**: `internal/infrastructure/di/container.go:284-286`,
  `internal/infrastructure/persistence/state.go:178-180`,
  `internal/infrastructure/security/paths.go:97-99,121-123,143-145,261-263,283-285,329-331,344-346`,
  `internal/infrastructure/security/policy.go:90-92,129-131,187-189,226-228`,
  `internal/tools/analysis/index.go:31-33,156-158`,
  `internal/tools/analysis/index_impl.go:177-179`,
  `internal/tools/analysis/dependency.go:203-205`,
  `internal/tools/analysis/ports_registry.go:24-26,26-28,174-176,215-217,734-736`,
  `internal/tools/analysis/transitive_gate.go:529-531,533-535,552-554,555-557,577-579,632-634,659-661,661-663,664-665,665-667,669-671,673-675,685-687,687-689`,
  `internal/tools/analysis/types.go:426-428`,
  `internal/tools/workspace/process_executor.go:82-84,131-133`,
  `internal/tools/workspace/pipeline.go:32-34,34-36,41-42,42-42,44-46`,
  `internal/tools/workspace/reader.go:301-303,305-307,331-333,335-337`,
  `internal/tools/workspace/shell.go:236-238,305-307,310-312,318-320,394-396`

### Structurally unreachable branches (5 sites)

- **Status**: ACCEPTED (2026-09, issue #1431)
- **Rationale**: branches that cannot fire in correct code — exhaustive-switch
  defaults, json.Marshal-style sinks, and dead normalization paths whose only
  callers pre-normalize their input. Same acceptance class as the cataloged
  json.Marshal entries (`global_prompt_tracker.go`, `middleware.go` detectLoop).
  Architect-acceptance comments at representative sites.
- **See**: `internal/infrastructure/config/config.go:232-234,258-260,287-289`,
  `internal/tools/analysis/imports.go:50-52`,
  `internal/tools/integrations/ado/pipeline_crud.go:313-315`

### Fault-injection-required branches (55 sites)

- **Status**: ACCEPTED (2026-09, issue #1431)
- **Rationale**: triggering the branch requires filesystem, driver, environment,
  or goroutine-timing fault injection disproportionate to the test value —
  subprocess/toolchain invocation failures, driver-level DB errors, fsync/disk
  faults. Same acceptance class as the 2026-07 fault-injection gaps.
  Architect-acceptance comments at representative sites.
- **See**: `internal/infrastructure/mcp/stdio_client.go:123-125`,
  `internal/infrastructure/telemetry/metrics.go:190-192`,
  `internal/tools/analysis/dead_code.go:179-181`,
  `internal/tools/analysis/index_impl.go:161-163`,
  `internal/tools/analysis/ports_registry.go:29-30,30-32`,
  `internal/tools/analysis/transitive_gate.go:690-691,691-693`,
  `internal/tools/analysis/types.go:423-425`,
  `internal/tools/workspace/process_executor.go:90-92,98-100,107-109,241-243,246-248,256-260,265-267,269-269,472-474,474-476,477-479,484-486,486-488,489-491`,
  `internal/tools/workspace/pipeline.go:122-124,161-164,174-175,175-177,185-185`,
  `internal/tools/workspace/proc_posix.go:35-37,45-47,47-47,47-49,52-54`,
  `internal/tools/workspace/reader.go:310-312,344-345`,
  `internal/tools/workspace/shell.go:259-271,273-273,273-274,274-276,277-277,364-376,378-378,378-379,379-381,382-382,466-485,495-495,538-540,540-541`,
  `internal/ui/tui/browser.go:155-156`,
  `internal/ui/tui/history_editor.go:50-51,51-53,67-69,72-74,75-77`

### Interface-stub no-op methods (16 sites)

- **Status**: ACCEPTED (2026-09, issue #1431)
- **Rationale**: empty method bodies that exist solely to satisfy interface
  contracts; Go coverage does not count empty bodies as covered. Same
  acceptance class as `NoOpEventBus`, `auth.Invalidate`, `NoOpLogger`
  (`interface-stub`). Architect-acceptance comments at representative sites.
- **See**: `internal/agent/agenttest/helpers.go:48-48,50-50,240-246,246-248,249-249`,
  `internal/agent/agenttest/mock_history_manager.go:141-142,145-147,147-148,148-150,154-155,160-161,164-164`,
  `internal/agent/agenttest/mock_ui_renderer.go:264-271,271-273,296-298`,
  `internal/tools/analysis/analysistest/mock_index.go:61-63`

### Delegation wrappers (2 sites)

- **Status**: ACCEPTED (2026-09, issue #1431)
- **Rationale**: thin pass-throughs that would only re-test the underlying
  implementation — the underlying method is covered directly. Same acceptance
  class as `AppendTask`, `domainFS.Chmod`, `FailoverGateway.ExtractDocument`
  (`delegation-wrapper`). Architect-acceptance comments at representative sites.
- **See**: `internal/tools/workspace/process_executor.go:496-498`,
  `internal/ui/tui/history_editor.go:31-37`

### Platform-specific branches (9 sites)

- **Status**: ACCEPTED (2026-09, issue #1431)
- **Rationale**: branches only executable on a specific OS — Windows path
  separators/volume prefixes, Darwin kernel observation. Not executable on
  Linux dev/CI. Same acceptance class as the cataloged platform-specific
  entries (`isPowerShellIndicator`, `NormalizeKey`). Architect-acceptance
  comments at representative sites.
- **See**: `internal/infrastructure/telemetry/system_metrics_darwin_cgo.go:38-41,41-44,45-45,67-69,76-78,87-89`,
  `internal/tools/workspace/process_executor.go:368-370,370-372,378-378`

*Last Updated: 2026-09 (ADR-053: get_cost_summary tool, DailyCost metric, and global cost ledger removed — issue #1291; coverage/complexity hygiene: event-type table test, shared markdown renderer, accepted mock stubs; catalog additions: ado/pipeline_crud.go json.Marshal unreachable entry, assertMissingKeysResult (CC=13), TestRecoveryStep_EmptyResponse_RetriesUpToLimit (CC=12), CC drift re-verification for (*indexer).snapshot (14), TestResolveCapabilities (13), TestFixtureIndexer_ConstructAndHarvest (13), design rejection: split internal/agent façade — issue #1299; #1302: emergencySave ghost-response guard entry — engine_phases.go; #1300: #1299 entry criterion renamed di-touch → cross-layer per ADR-056; coverage hygiene: NoOpLogger + BypassConfirmation entries — 2026-08; test-complexity catalog additions: TestFakeToolchainRunner_PresetValues (CC=28) and TestFakeToolchainRunner_ZeroDefaults (CC=38) — issue #1325 toolstest fake; #1327: middleware.go catalog pins re-anchored to :213/:311 (per-turn Turn lifecycle refactor); catalog re-anchor: di/container.go skills.sh error branch (203-206) covered — See narrowed to :197-200; catalog re-anchor: history_test.go pins +1 (T1 import shift); catalog re-anchor: middleware.go detectLoop + session_manager.go TUI entry (line drift from renderPostTUISummary feature); catalog re-anchor: factory_test.go + files_test.go complexity pins (issue #1350 item 4, llm→auth import shift); catalog re-anchor: files_test.go complexity pin 378→379 + auth.go Invalidate no-op stub lines (issue #1358 auth/contract realignment); 2026-09 factory seams triage: coverage pin for llm/factory.go createAuthenticator default-case; 2026-09 catalog hygiene: coverage pins for llm/failover.go ExtractDocument delegation wrapper + llm/provider_health.go getPingEndpoint panic and buildRequest error branch (structurally unreachable); re-anchored NewID (222-224 → 234-236) and Task accessors (105,108,111 → 112,116); coverage test for BuildSuggestionService tracker fallback; 2026-09 #1378: four test-complexity catalog additions (TestToSDKSchema_EmptyType_OmitsTypeKey CC=19, TestToOpenAISchema_EmptyTypeOmitsKey CC=11, TestToOpenAITools_EmptyTypeNested_OmitsEmptyTypeKey CC=16, TestConvertSchema_NestedUnrepresentable_BecomesUntypedAny CC=12) — MCP empty-type schema tests; #1387: buildFileUploadBody multipart + propagation coverage-pin catalog entries (Entry A/B); redact hygiene: normalizeKeyToken dead-branch coverage entry (structurally-unreachable); 2026-09 triage completion: ten coverage entries (mcp marshalInputSchema/pipes/resolveCommand/Close-race, global_prompt_tracker prepare, policy confirmAction call-sites, manager Abs guards, state SetInfo marshal, telemetry summary marshal, toolchain execRunner); re-anchors (sqlite RowsAffected 200/218 → 207-209/224-226, state.go:56 dropped, gh token resolver 66-70 → 66-71); partition-test rows (17); complexity-drift repair: TestBasicAuthTransport 645→646, TestMCPFactory_ConcurrentBuild 683→684, ..._FailingServerSkips 743→744 (import-shift re-anchors), TestStdio_ChildDeathMidSession CC 11→13, TestResolveCommand 10→13 new entry (stdio_client_test.go:25); memory integration: hook.go BeforeTurn/OnPhaseTransition interface-stub entry + TestFirstNonNil/TestAcquireWriteLockUserHomeDirError coverage — partition-test row (1); memory integration round 2: truncateToBytes/AfterTurn-empty-text/acquireWriteLock call-site guard/metadataIDs/lastUserText/joinTextParts/FlushSession edge coverage (harness fix: newTestMemoryHome pre-creates ~/.plur) + flock_unix.go non-EWOULDBLOCK (fault-injection-required) and FlushSession engrams-empty (defensive-guard) catalog entries — partition-test rows (2); #1431: medium-gap triage batch (140 sites across six classes: defensive 53, structurally-unreachable 5, fault-injection 55, interface-stub 16, delegation 2, platform 9) + TurnTrace.AddWarnings / startWalker walk-error coverage fixes + six coverage-pin re-anchors (StdoutPipe 152-154, StderrPipe 156-158, renderMarkdownAsync 669-676, stdio_client ListTools 174-176, Close 251-253, resolveCommand 358) + partition-test rows (6), CC=10 boundary watch list))
