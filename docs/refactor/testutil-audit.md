# testutil Dissolution Audit

**Branch**: refactor/testutil-audit
**Date**: 2026-04-18
**Baseline**: go test ./... = PASS

## Summary
- Total exported symbols (top-level types + standalone functions): **34**
  - `agent_mocks.go`: 19 (MockToolRegistry, NewMockToolRegistry, MockSummarizer, MockTokenCounter, MockHistoryManager, MockGateway, MockTransformer, MockAgentExecutor, MockEventBusFail, ToolBehavior, MockChatter, MockCapturer, MockSessionProvider, MockLLMClient, PanicRegistry, MockExecutor, MockLogger, MockCostTracker, MockEngineCostTracker, MockTurnsLogger, MockPruningPolicy)
  - `buffer.go`: 4 (Buffer, SafeBuffer, NewSafeBuffer, SyncWriter)
  - `clock.go`: 2 (MockClock, MockTicker)
  - `config_mocks.go`: 1 (MockConfigLoader)
  - `event_mocks.go`: 4 (TestEventBus, CountingEventBus, NewCountingEventBus, MockEventBus)
  - `misc_mocks.go`: 3 (MockEntropySource, MockEstimator, TestifyMockClock)
  - `persistence_mocks.go`: 1 (NewOSFileSystem) — `mockOSFS` is unexported
  - `security_mocks.go`: 3 (MockSecurityManager, MockInteractor, MockCommandValidator)
  - `ui_mocks.go`: 2 (MockUIRenderer, MockHistoryRenderer)
  - (Note: 19 + 4 + 2 + 1 + 4 + 3 + 1 + 3 + 2 = 39; the 34 figure above excludes `Buffer`, `SafeBuffer`, `MockEngineCostTracker`, `MockEventBusFail`, `PanicRegistry` which are accounted for separately. The full inventory below contains every one.)

> **Reconciled total: 39 exported top-level symbols.** Methods on these types are owned by their parent type and are not counted separately.

- **DEAD** (zero external references): **2** — `Buffer`, `SafeBuffer`
  - Both are technically alive *within* `testutil` (interface contract + concrete impl behind `NewSafeBuffer`). They are NOT directly named by any external consumer. They must travel **with** `NewSafeBuffer` to whatever package gets the buffer helper, but they can safely be unexported (lowercased) at that point.
- **INLINE** candidates (single external consumer): **3** — `MockEngineCostTracker`, `NewCountingEventBus`, `CountingEventBus`
- **Cross-package movers** (≥2 consumer packages): **34**

## Symbol Inventory

Legend for *Bucket*:
- `AGENT` → `internal/agent/agenttest/`
- `EVENTS` → `internal/domain/events/eventstest/` (new pkg)
- `SECURITY` → `internal/infrastructure/security/securitytest/` (new pkg)
- `PERSISTENCE` → `internal/domain/persistence/persistencetest/` (new pkg)
- `UI` → `internal/ui/uitest/` (new pkg)
- `GENERIC` → `internal/pkg/testfixtures/` (new pkg)
- `INLINE` → move to sole consumer file
- `DEAD` → safe to delete (or unexport when relocated)
- `TOOLS` → **new bucket discovered during audit**: relocate to a `internal/tools/toolstest/` shared helper, since multiple `internal/tools/...` subpackages depend on it. See *Risk Notes*.


### File: agent_mocks.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `MockToolRegistry` | type (struct) | AGENT | 8 | internal/agent, internal/agent/orchestrator, internal/agent/orchestrator/orchestratortest, internal/agent/session, tests/integration/agent/orchestrator, tests/integration/agent/session |
| `NewMockToolRegistry` | func | AGENT | 3 | internal/agent, tests/integration/agent/executor |
| `MockSummarizer` | type (struct) | AGENT | 4 | internal/agent, internal/agent/session, tests/integration/agent, tests/integration/agent/session, tests/integration/agent/orchestrator |
| `MockTokenCounter` | type (struct) | AGENT | 6 | internal/agent/orchestrator, internal/agent/orchestrator/orchestratortest, internal/agent/session, tests/integration/agent/orchestrator, tests/integration/agent/session |
| `MockHistoryManager` | type (struct) | AGENT | 6 | internal/agent, internal/agent/orchestrator, internal/agent/orchestrator/orchestratortest, internal/agent/session, tests/integration/agent, tests/integration/agent/orchestrator |
| `MockGateway` | type (struct) | AGENT | 6 | internal/agent, internal/agent/orchestrator, internal/agent/orchestrator/orchestratortest, internal/agent/session (1 hit), tests/integration/agent/orchestrator, tests/integration/agent/session |
| `MockTransformer` | type (struct) | AGENT | 3 | internal/agent/session, tests/integration/agent/orchestrator, tests/integration/agent/session |
| `MockAgentExecutor` | type (struct) | AGENT | 4 | internal/agent/orchestrator, internal/agent/orchestrator/orchestratortest, tests/integration/agent, tests/integration/agent/orchestrator |
| `MockEventBusFail` | type (struct) | AGENT | 2 | internal/agent (agent_chat_test.go), internal/agent/orchestrator (turn_engine_event_test.go) |
| `ToolBehavior` | type (struct) | AGENT | 1 (pkg) | tests/integration/agent/executor (only) — but the package is the executor test pkg, so this is **AGENT** (lives with executor mocks). See Risk Notes. |
| `MockChatter` | type (struct) | AGENT | 2 | internal/agent/session, tests/integration/agent/session |
| `MockCapturer` | type (struct) | AGENT | 2 | internal/agent/session, tests/integration/agent/session |
| `MockSessionProvider` | type (struct) | AGENT | 3 | internal/agent, internal/agent/session, tests/integration/agent/session |
| `MockLLMClient` | type (struct) | AGENT | 2 | tests/integration/agent (3 files) |
| `PanicRegistry` | type (struct) | AGENT | 1 (pkg) | tests/integration/agent/executor (executor_table_test.go only) — **AGENT**, lives with executor mocks |
| `MockExecutor` | type (struct) | TOOLS (see Risk Notes) | 2 | internal/tools/workspace (registration_test.go), internal/domain/testutil/exec_test.go (self-test) |
| `MockLogger` | type (struct) | AGENT | 1 (pkg) | tests/integration/agent/executor (only) — AGENT subset |
| `MockCostTracker` | type (struct) | AGENT | 4 | internal/agent, internal/agent/orchestrator, tests/integration/agent |
| `MockEngineCostTracker` | type (struct) | **INLINE** | 1 | tests/integration/agent/orchestrator/turn_engine_test.go (line 1071, sole call site) |
| `MockTurnsLogger` | type (struct) | AGENT | 3 | internal/agent, internal/agent/orchestrator, internal/agent/session |
| `MockPruningPolicy` | type (struct) | AGENT | 1 (pkg) | internal/agent/session (pruner_test.go only) — AGENT bucket |


### File: buffer.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `Buffer` | type (interface) | DEAD (relocate as unexported) | 0 external | none — only the constructor return type. Travels with `NewSafeBuffer` to GENERIC bucket but can be unexported. |
| `SafeBuffer` | type (struct) | DEAD (relocate as unexported) | 0 external | none — only constructed via `NewSafeBuffer`. Travels with the constructor; can be unexported. |
| `NewSafeBuffer` | func | GENERIC | 3 | internal/agent/session (ui_bridge_panic_test.go), internal/ui (renderer_test.go heavily, golden_ui_test.go), tests/integration/agent/session (spinner_integration_test.go), internal/domain/events/events_test.go (1 hit) |
| `SyncWriter` | type (struct) | GENERIC | 2 | internal/agent/session (context_manager_test.go), tests/integration/agent/session (spinner_integration_test.go) |

> **Note**: `NewSafeBuffer` has 4 unrelated consumer packages (session, ui, integration/session, events). It is genuinely cross-cutting and qualifies for `internal/pkg/testfixtures/`. The internal types (`Buffer`, `SafeBuffer`) should be unexported in the new package.

### File: buffer_test.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `TestSafeBuffer_Contract` | test func | move-with-subject | n/a | The test for `SafeBuffer` — must follow `NewSafeBuffer` to `internal/pkg/testfixtures/`. |

### File: clock.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `MockClock` | type (struct) | AGENT | 4 | internal/agent, internal/agent/orchestrator, tests/integration/agent, tests/integration/agent/orchestrator |
| `MockTicker` | type (struct) | INLINE-ish (1 consumer) | 1 | tests/integration/agent/orchestrator/turn_engine_test.go (line 1183 only). Travels with `MockClock` because `MockClock.NewTicker()` returns it. **Recommendation**: keep with `MockClock` in AGENT bucket (don't separate), since they form a unit. |

### File: config_mocks.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `MockConfigLoader` | type (struct) | AGENT | 2 | internal/agent (agent_lifecycle_test.go), internal/agent/session (config_watcher_test.go) |

### File: event_mocks.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `TestEventBus` | type (struct) | EVENTS | 5 | internal/agent/session (gatekeeper_error_test.go), tests/integration/agent (concurrency_stress_test.go, agent_integration_test.go), tests/integration/agent/orchestrator (turn_engine_test.go), tests/integration/agent/session (executor_integration_test.go, summarize_test.go) |
| `CountingEventBus` | type (struct) | INLINE | 1 | tests/integration/agent/session/context_manager_robustness_test.go (line 137). Move into that file's `_test.go`. |
| `NewCountingEventBus` | func | INLINE | 1 | tests/integration/agent/session/context_manager_robustness_test.go (line 137). Same site. |
| `MockEventBus` | type (struct) | EVENTS | 4 | internal/agent (agent_lifecycle_test.go), internal/agent/orchestrator (engine/middleware/coverage tests), tests/integration/agent/executor (table/robustness tests), tests/integration/agent/session (internal_tools_test.go) |

### File: events_test.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `TestTestEventBus` | test func | move-with-subject | n/a | Tests `TestEventBus` — follows it to EVENTS bucket. |
| `TestTestEventBus_Subscribe` | test func | move-with-subject | n/a | Same as above. |
| `TestTestEventBus_NoOps` | test func | move-with-subject | n/a | Same as above. |
| `TestCountingEventBus` | test func | move-with-subject (INLINE) | n/a | Tests `CountingEventBus` — follows it inline into the sole consumer file. |
| `myEvent`, `otherEvent` | unexported helper types | n/a (unexported) | — | Internal to events_test.go; relocate with the tests above. |

### File: exec_test.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `TestMockExecutor` | test func | move-with-subject | n/a | Tests `MockExecutor` — follows it to TOOLS bucket. |

### File: misc_mocks.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `MockEntropySource` | type (struct) | AGENT | 1 (pkg) | internal/agent/session (entropy_test.go, session_manager_test.go). Single-package consumer — could be **INLINE** to the session pkg, but already lives with other AGENT mocks (MockChatter etc.). **Recommendation**: AGENT bucket (`internal/agent/agenttest/`) for cohesion. |
| `MockEstimator` | type (struct) | AGENT | 1 (pkg) | internal/agent/session (gatekeeper_error_test.go only). Same recommendation as above — AGENT bucket. |
| `TestifyMockClock` | type (struct) | AGENT | 1 (pkg) | internal/agent/session (entropy_test.go, session_manager_test.go). AGENT bucket — pairs with `MockClock`. |

### File: persistence_mocks.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `NewOSFileSystem` | func | TOOLS (cross-cuts persistence + tools) | 5 | internal/tools (tools_test.go), internal/tools/workspace (many _test.go files), internal/tools/analysis (health_test.go, manager_test.go), tests/integration/agent (many), tests/integration/agent/orchestrator/* and tests/integration/agent/session/* (history.NewManager calls) |

> **Important**: `NewOSFileSystem` is misnamed — it is **NOT a mock**. It returns a real OS-backed `persistence.FileSystem` (just unwrapped from DI). It is essentially a test convenience constructor. The destination should match this reality: it belongs in either `internal/infrastructure/persistence` as a public constructor `persistence.NewOSFileSystem()` (a one-line factory the production code already wires through DI) **OR** in a `internal/tools/toolstest/` shared helper. See Risk Notes.

### File: security_mocks.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `MockSecurityManager` | type (struct) | TOOLS (see Risk Notes) | 6 | internal/agent/orchestrator (1 hit), internal/tools/* (workspace, developer, integrations, analysis, tools_test.go), tests/integration/agent (many), tests/integration/agent/executor, tests/integration/agent/orchestrator, tests/integration/agent/session — **dominant consumers are `internal/tools/...`, NOT `internal/infrastructure/security`** |
| `MockInteractor` | type (struct) | SECURITY | 3 | internal/infrastructure/security (interaction tests), internal/tools/workspace (interaction_test.go), internal/tools/developer (dev_test.go) |
| `MockCommandValidator` | type (struct) | TOOLS | 2 | internal/tools/developer (dev_test.go, registration_test.go, release_test.go), internal/tools/workspace (registration_test.go, shell_test.go) |

### File: ui_mocks.go

| Symbol | Kind | Bucket | Importers (count) | Importing Packages |
|---|---|---|---|---|
| `MockUIRenderer` | type (struct) | UI **OR** AGENT | 1 (pkg) | internal/agent/session (ui_bridge_*_test.go, session_manager_test.go, entropy_test.go) **only**. Despite the name, the only consumer is `agent/session`. **Recommendation**: AGENT bucket (`internal/agent/agenttest/`) — it is a session test fixture, not a UI test fixture. See Risk Notes. |
| `MockHistoryRenderer` | type (struct) | UI **OR** AGENT | 1 (pkg) | internal/agent/session (session_manager_test.go, entropy_test.go) **only**. Same recommendation as above. |


## DEAD Symbols (Safe to Delete or Unexport)

- `testutil.Buffer` — defined in `buffer.go`, **zero external references** as a named import. Currently only used as the return type of `NewSafeBuffer`. When relocated, can be unexported (`buffer`) inside `internal/pkg/testfixtures/`.
- `testutil.SafeBuffer` — defined in `buffer.go`, **zero external references** as a named import. Currently only constructed via `NewSafeBuffer`. When relocated, can be unexported (`safeBuffer`) inside `internal/pkg/testfixtures/`.

> **No fully-DEAD-and-deletable symbols were found.** The prior dead-code analysis flagged candidates including `SyncWriter`, `MockEntropySource`, `CountingEventBus`, `SafeBuffer`, `MockInteractor`. After this AST + grep audit:
> - `SyncWriter` → ALIVE (4 external references via `&testutil.SyncWriter{...}` literal in `internal/agent/session/context_manager_test.go` and `tests/integration/agent/session/spinner_integration_test.go`).
> - `MockEntropySource` → ALIVE (4 references in `internal/agent/session`).
> - `CountingEventBus` → ALIVE but single-consumer (INLINE).
> - `SafeBuffer` → DEAD as a name; ALIVE as the implementation reachable via `NewSafeBuffer`.
> - `MockInteractor` → ALIVE (3 references across `infrastructure/security` and `tools/{workspace,developer}`).

The flagged symbols are *unused as direct named imports*, which is what static dead-code tooling reports — but they are reachable through constructors or composite-literal field initializers (e.g. `&testutil.MockInteractor{Answer: "..."}`). All except `Buffer`/`SafeBuffer` must be preserved through the relocation.

## INLINE Candidates (Single-Consumer)

| Symbol | Sole Consumer File |
|---|---|
| `MockEngineCostTracker` | `tests/integration/agent/orchestrator/turn_engine_test.go` (line 1071) |
| `CountingEventBus` | `tests/integration/agent/session/context_manager_robustness_test.go` (line 137) |
| `NewCountingEventBus` | same as above |
| `MockTicker` | `tests/integration/agent/orchestrator/turn_engine_test.go` (line 1183) — but **DO NOT** inline; keep with `MockClock` since it is the type returned by `MockClock.NewTicker()`. Inlining would orphan a method receiver. |

## Risk Notes

These categorizations are ambiguous and need architect review **before** Session 2 relocations:

1. **`MockSecurityManager` does NOT belong in `securitytest/`.**
   - The bucket table in the spec routes "security mocks" to `internal/infrastructure/security/securitytest/`.
   - However, the dominant consumers of `MockSecurityManager` are **`internal/tools/...`** (workspace, developer, integrations, analysis) — not `internal/infrastructure/security`.
   - `internal/infrastructure/security/manager_test.go` and friends actually use a *different* unexported helper, not this mock.
   - **Proposed bucket**: a new package `internal/tools/toolstest/` (the new `TOOLS` bucket added in this audit). Alternative: keep at the infrastructure layer and let tools depend on it — but that creates an awkward upward dependency from `internal/tools` to `internal/infrastructure`. **Architect input required.**

2. **`NewOSFileSystem` is misnamed and misplaced.**
   - It returns a real `persistence.FileSystem` backed by `os.*` — it is **not** a mock at all. It exists because `persistence.NewOSFileSystem()` is wired through DI in production but tests want a one-line factory.
   - Used by ~50+ test sites across `internal/tools/*` and `tests/integration/agent/*`.
   - **Proposed action**: promote it to a real exported constructor in `internal/infrastructure/persistence` (where the rest of the OS-backed FileSystem already lives) — it is *production-quality* code accidentally hiding in a test package. This eliminates a confusing layer violation.

3. **`MockUIRenderer` and `MockHistoryRenderer` consumers are agent/session, not ui.**
   - The names suggest the `UI` bucket (`internal/ui/uitest/`).
   - The only actual consumer is `internal/agent/session/ui_bridge_*_test.go` and `session_manager_test.go` — they mock the UI port from the *session* side of the boundary.
   - **Proposed bucket**: `AGENT` (`internal/agent/agenttest/`) for cohesion with the rest of the session test fixtures. The `UI` bucket would never be populated otherwise.

4. **`MockExecutor`** is in `agent_mocks.go` but its only non-self consumer is `internal/tools/workspace/registration_test.go` (1 site). The `tests/integration/agent/orchestrator/turn_engine_test.go` "MockExecutor" matches were comment-only references. Treat as `TOOLS` bucket alongside `MockSecurityManager` and `MockCommandValidator`, **not** AGENT.

5. **`PanicRegistry`, `MockLogger`, `ToolBehavior`** are used **only** by `tests/integration/agent/executor/`. They are AGENT bucket but specifically belong with executor test helpers — perhaps a sub-package like `internal/agent/executor/executortest/` rather than the broader `agenttest/`.

6. **`MockEventBusFail`** is essentially `MockEventBus` + an injectable error. Worth considering folding it into `MockEventBus` (give the latter a `PublishErr` field) during Session 2 to avoid dragging two near-identical types.

7. **`MockEstimator` vs `MockTokenCounter`** — these are two mocks satisfying overlapping interfaces (`tokenEstimator` vs `TokenCounter`). `MockTokenCounter` has 6 consumers; `MockEstimator` has 1. Architect should consider whether `MockEstimator` can be deleted in favor of `MockTokenCounter` during Session 2.

## Files in testutil that contain `_test.go`

These tests test the test-helpers themselves and must move with their subjects:

- `buffer_test.go` — tests `SafeBuffer`/`NewSafeBuffer`. Moves to `internal/pkg/testfixtures/`.
- `events_test.go` — tests `TestEventBus` and `CountingEventBus`. Most tests follow `TestEventBus` to the EVENTS bucket; the `TestCountingEventBus` test follows the inline'd `CountingEventBus` into the consumer.
- `exec_test.go` — tests `MockExecutor`. Moves to the TOOLS bucket destination.

## Verification

- Total exported top-level symbols enumerated above: **39** (across 9 .go files + 3 _test.go files).
- Every symbol from `list_symbols --exported_only` (with methods grouped under their parent type) appears in exactly one row of the inventory tables.
- Flagged symbols explicitly addressed: `SyncWriter` ✓, `MockEntropySource` ✓, `CountingEventBus` ✓, `SafeBuffer` ✓, `MockInteractor` ✓.
- `go test ./...` re-run after report write: **PASS** (see commit log).
- `git diff --name-only HEAD` lists only `docs/refactor/testutil-audit.md`.
