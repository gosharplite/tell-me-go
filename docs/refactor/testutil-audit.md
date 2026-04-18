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

| Symbol | Kind | Bucket | Status | Importers (count) | Importing Packages |
|---|---|---|---|---|---|
| `NewOSFileSystem` | func | `persistencetest` sub-package (renamed `NewPlainOSFileSystem`) | **RELOCATED in Session 2** | 5 | internal/tools (tools_test.go), internal/tools/workspace (many _test.go files), internal/tools/analysis (health_test.go, manager_test.go), tests/integration/agent (many), tests/integration/agent/orchestrator/* and tests/integration/agent/session/* (history.NewManager calls) |

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

## Session 2 Outcome (NewOSFileSystem extraction)

- **Date**: 2026-04-18
- **Branch**: `refactor/testutil-extract-plainosfs` (renamed from `refactor/testutil-promote-osfs` after the Step 3 halt — see below)
- **Symbol relocated and renamed**: `testutil.NewOSFileSystem` → `persistencetest.NewPlainOSFileSystem`
- **Destination file**: `internal/infrastructure/persistence/persistencetest/plain_os_fs.go` (new package)
- **Call sites updated**: 103 (across 23 test files; pre-edit grep estimated ~100, the audit's earlier "~80" figure was an undercount because it did not include aliased imports)
- **Files deleted**: `internal/domain/testutil/persistence_mocks.go`
- **Files created**: `internal/infrastructure/persistence/persistencetest/plain_os_fs.go`
- **Tests**: PASS (60 packages — was 59, +1 for the new `persistencetest` package which has no tests yet)
- **Race detector**: PASS (`go test -count=1 -race ./...`)
- **Architecture verification**: ✅ no new violations
- **Architectural lint (Step 9)**: all 23 importers of `persistencetest` are `_test.go` files. Convention enforced: production code never imports `persistencetest`.

### Why the rename and destination differ from the original Session 2 plan

The original Session 2 spec instructed promoting the function into the production `internal/infrastructure/persistence` package as `persistence.NewOSFileSystem`. The Step 3 collision guard halted that approach because **a function with that exact name and signature already exists in production** (`internal/infrastructure/persistence/os_fs.go:190`) but with **different semantics**:

| Aspect | Production `persistence.NewOSFileSystem` | Test helper (now `persistencetest.NewPlainOSFileSystem`) |
|---|---|---|
| Concrete type | `&domainFS{fs: &OSFileSystem{}}` (two-layer) | `&plainOSFS{}` (single-layer, was `mockOSFS`) |
| Retry on transient errors | ✅ via `fsRetry` wrapper | ❌ none — surface errors immediately |
| `WriteFile` semantics | Routed through `AtomicWrite` (write-temp-then-rename) | Plain `os.WriteFile` (non-atomic) |
| `Walk` | Checks `ctx.Err()` per entry | Ignores ctx (faster, simpler) |

The architect's verdict was Option B with refinement: **keep both, name them honestly, package them honestly**. The test helper now lives in a sibling sub-package following the stdlib `httptest`/`iotest` convention, with a name (`Plain*`) that actively signals what it omits. This establishes the `<layer>/<pkg>/<pkg>test/` pattern as the convention for the rest of the testutil dissolution.

### Notes / surprises encountered

1. **Two files used a misleading import alias** that masked the relocation: `internal/tools/analysis/manager_test.go` and `internal/tools/workspace/search_bench_test.go` aliased `internal/domain/testutil` AS `infrapersistence` — the alias actively *advertised* the architectural smell that motivated this refactor (a test was pretending to import the production persistence package while actually importing the testutil package). The bulk `sed` pass missed these because the source qualifier was `infrapersistence.NewOSFileSystem`, not `testutil.NewOSFileSystem`. Found and fixed during `go vet`. **Lesson for Session 3+**: always grep the *import path* (`tell-me-go/internal/domain/testutil`), not just the qualifier (`testutil.`), when enumerating relocations.

2. **Five other files** (`tests/integration/agent/concurrency_stress_test.go`, `internal/ui/history_test.go`, `internal/infrastructure/history/store_test.go`, `internal/infrastructure/history/history_test.go`, `internal/infrastructure/history/migration_test.go`) also use the `infrapersistence` alias, but they alias the **real** `internal/infrastructure/persistence` package and call the **production** `NewOSFileSystem`. Those are correct as-is and out of scope.

3. **`goimports` did most of the import-management work automatically** — it added the new `persistencetest` import to all 21 bulk-edited files and removed the `testutil` import from the 2 files where it became unused (`turn_engine_loop_test.go`, `auto_summarize_test.go`). Manual import editing was needed only on the 2 misleading-alias files.

4. **The audit's "~80 call sites" estimate was 23% low** (actual: 103). Acceptable for an audit, but Session 3+ estimates for the bigger AGENT/TOOLS buckets should be regenerated with `grep -c` rather than read off the audit.

### State of the testutil dump after Session 2

- Files remaining in `internal/domain/testutil/`: 11 (was 12)
- Symbols remaining: 38 of original 39 exported symbols
- Next-easiest candidate (architect to confirm Session 3 scope): a single AGENT-bucket symbol with no naming collision, e.g. `MockGateway` or `MockHistoryManager`.


## Session 3 Outcome (MockGateway + MockHistoryManager extraction, with collateral fix)

- **Date**: 2026-04-19
- **Branch**: `refactor/testutil-extract-agent-gateway-history`
- **Symbols relocated** (names preserved, no rename):
  - `testutil.MockGateway` → `agenttest.MockGateway` (file: `internal/agent/agenttest/mock_gateway.go`)
  - `testutil.MockHistoryManager` → `agenttest.MockHistoryManager` (file: `internal/agent/agenttest/mock_history_manager.go`)
- **Collateral relocations** (forced by import-cycle resolution — see Lessons):
  - `agenttest.AgentInternal` → `agentinternal.AgentInternal`
  - `agenttest.AsAgentInternal` → `agentinternal.AsAgentInternal`
  - `agenttest.MockSessionLifecycleManager` (and its unexported backing type `mockSessionLifecycleManager`) → `agentinternal.MockSessionLifecycleManager`
- **New package created**: `internal/agent/agentinternal/` (a regular non-`*test` sibling package). Holds exactly the test helpers that legitimately need to import `internal/agent` (the parent production package).
- **Source file shrunk**: `internal/domain/testutil/agent_mocks.go` lost 2 types and ~150 LOC; 17 other symbols remain for later sessions.
- **Helper file restructured**: `internal/agent/agenttest/helpers.go` lost 3 symbols and 2 imports (`internal/agent`, `internal/domain/config`). It is now layer-correct: `agenttest` depends only on `internal/domain/*`, never on `internal/agent`.
- **Call sites updated**:
  - 23 test files for `MockGateway`/`MockHistoryManager` (158 references)
  - 4 test files for `AgentInternal`/`AsAgentInternal`/`MockSessionLifecycleManager`
  - **27 files total** in this session
- **Tests**: PASS (61 packages — was 60; +1 for new `agentinternal` package).
- **Race detector**: PASS (`go test -count=1 -race ./...`).
- **Architecture verification**: ✅ no new violations; the latent `agenttest → internal/agent` upward dependency is GONE.
- **Architectural lint**: zero non-test production files import `agenttest` or `agentinternal` (verified via grep).

### Why the collateral relocation was unavoidable

Step 10 of the original Session 3 spec failed with an **import cycle**:

```
internal/agent (test compile)
  → internal/agent/agenttest (via *_test.go importing agenttest.MockGateway)
  → internal/agent (via the pre-existing helpers.go using agent.InternalAccessor / agent.AsInternal / agent.CapturerInteractor)
```

The cycle affected 9 internal-package test files (those declared `package agent`, `package orchestrator`, `package session` rather than the external `*_test` variants). Files using external `*_test` packages were unaffected.

Diagnosis revealed three symbols in `agenttest/helpers.go` had typed dependencies on `internal/agent` symbols:
1. `AgentInternal` (embeds `agent.InternalAccessor`)
2. `AsAgentInternal` (calls `agent.AsInternal`)
3. `mockSessionLifecycleManager.BuildSessionDependencies` (parameter typed `agent.CapturerInteractor`, a *distinct* interface declared in `internal/agent/chat.go` — not a type alias)

The architect's first remediation proposal (Option E) targeted only #1 and #2. Investigation revealed #3 also kept the `internal/agent` import alive in `helpers.go`. The signature of `BuildSessionDependencies` cannot be widened to `any` because the mock must satisfy the production `internal/agent.SessionLifecycleManager` interface, which is itself defined in `internal/agent` and uses `CapturerInteractor` directly. The choice was therefore between (a) extracting the production interface to `domain/ports` (out of scope) or (b) moving #3 along with #1 and #2. Option (b) was selected as Option E-extended.

### Lessons added (apply in Sessions 4+)

1. **"Destination directory is empty" is an audit assumption that may be wrong.** The audit reported `internal/agent/agenttest/` as empty; it actually contained a 280-line `helpers.go` with hidden upward dependencies. Future sessions targeting a partly-populated destination MUST `read_files` on every existing `.go` file there during Step 1 and inspect the import block for upward dependencies into the parent package.

2. **A `*test` sub-package may not import its parent production package** — this is the architectural rule the architect established this session. Test-only helpers that legitimately need parent-package symbols belong in a *sibling* regular package (e.g. `agentinternal/`), not the `*test` package. Apply the same rule prophylactically when creating future `*test` packages.

3. **`mock.Mock`-based mock signatures must exactly match the production interface they satisfy.** Cannot widen parameter types to `any` to break dependencies. If the production interface itself imports a parent-package type, the mock cannot escape that dependency without first refactoring the production interface (out of scope for testutil dissolution).

4. **The "~6 consumer packages" estimate per AGENT-bucket symbol was decent for `MockGateway` (8 files) but underestimated `MockHistoryManager` (16 files).** Audit estimates remain ±25% as flagged in Session 2.

5. **`grep -rl` substring matches can deceive.** Two files (`spinner_integration_test.go`, `internal_tools_test.go`) were initially missed in my MockGateway-specific grep but appeared in the union grep. Reconciliation between Search A (qualifier-based) and Search B (import-path-based) caught them.

### State of the testutil dump after Session 3

- Files remaining in `internal/domain/testutil/`: 11 (unchanged from Session 2 — `agent_mocks.go` shrank but did not disappear).
- Symbols remaining: 36 of original 39 exported symbols (was 38 after Session 2, lost MockGateway + MockHistoryManager).
- New shared sibling package: `internal/agent/agentinternal/` (3 symbols).
- Next-easiest candidates per the audit's AGENT bucket: `MockToolRegistry`/`NewMockToolRegistry` (~8 consumers), or `MockSummarizer`/`MockTokenCounter`/`MockTransformer` family. Architect to confirm Session 4 scope.
