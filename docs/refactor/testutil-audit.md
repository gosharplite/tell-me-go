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


## Session 4 Outcome (bulk extraction of remaining AGENT symbols)

- **Date**: 2026-04-19
- **Branch**: `refactor/testutil-extract-agent-bulk` (forked from Session 3 commit `9eeaf2b0`)
- **Symbols relocated** (15 distinct types + 1 constructor; names preserved):
  - `testutil.MockToolRegistry` + `testutil.NewMockToolRegistry` → `agenttest.MockToolRegistry` (`mock_tool_registry.go`)
  - `testutil.MockSummarizer` → `agenttest.MockSummarizer` (`mock_summarizer.go`)
  - `testutil.MockTokenCounter` → `agenttest.MockTokenCounter` (`mock_token_counter.go`) — also picked up the stray `EstimateTokens` method that lived 340 lines below the type definition
  - `testutil.MockTransformer` → `agenttest.MockTransformer` (`mock_transformer.go`)
  - `testutil.MockAgentExecutor` → `agenttest.MockAgentExecutor` (`mock_agent_executor.go`)
  - `testutil.MockEventBusFail` → `agenttest.MockEventBusFail` (`mock_event_bus_fail.go`) — preserved as-is per architect direction (rejected the audit Risk Note #6 fold-into-MockEventBus suggestion)
  - `testutil.ToolBehavior` → `agenttest.ToolBehavior` (`tool_behavior.go`)
  - `testutil.MockChatter` → `agenttest.MockChatter` (`mock_chatter.go`)
  - `testutil.MockCapturer` → `agenttest.MockCapturer` (`mock_capturer.go`)
  - `testutil.MockSessionProvider` → `agenttest.MockSessionProvider` (`mock_session_provider.go`)
  - `testutil.MockLLMClient` → `agenttest.MockLLMClient` (`mock_llm_client.go`)
  - `testutil.PanicRegistry` → `agenttest.PanicRegistry` (`panic_registry.go`)
  - `testutil.MockLogger` → `agenttest.MockLogger` (`mock_logger.go`)
  - `testutil.MockCostTracker` → `agenttest.MockCostTracker` (`mock_cost_tracker.go`)
  - `testutil.MockPruningPolicy` → `agenttest.MockPruningPolicy` (`mock_pruning_policy.go`)
- **Symbols collapsed** (Step 3 conflict resolution):
  - `testutil.MockTurnsLogger` was found to be **functionally identical** to the pre-existing `agenttest.MockTurnsLogger` (declared in `helpers.go` as a type alias for unexported `mockTurnsLogger`). The testutil version was deleted; its 5 call sites in 3 files were rewritten to `agenttest.MockTurnsLogger` (the existing one).
- **Symbols inlined** (audit INLINE bucket — handled because the alias would dangle):
  - `testutil.MockEngineCostTracker` was a one-line type alias `= MockCostTracker`. Its single call site in `tests/integration/agent/orchestrator/turn_engine_test.go:1072` was rewritten to use `agenttest.MockCostTracker` directly. The alias was deleted from testutil, completing the audit's INLINE intent for this symbol.
- **Source file gutted**: `internal/domain/testutil/agent_mocks.go` shrank from **508 → 38 LOC** (-92%). It now contains only `MockExecutor` (TOOLS bucket — slated for a future session). A header comment marks it as such for future contributors.
- **Destination grown**: `internal/agent/agenttest/` grew from 3 files to **18 files** (15 new + 3 from Session 3); 1144 LOC total.
- **Call sites updated**: **40 files**, with rough reference counts as follows (per-symbol from `grep -c` Search A reconciliation):
  - `MockTokenCounter`: 93 refs in 23 files (the densest)
  - `MockToolRegistry`: 53 refs in 12 files
  - `MockSummarizer`: 38 refs in 12 files
  - `MockLLMClient`: 27 refs in 4 files
  - `MockAgentExecutor`: 24 refs in 4 files
  - `ToolBehavior`: 24 refs in 2 files
  - `NewMockToolRegistry`: 19 refs in 4 files
  - `MockSessionProvider`: 17 refs in 5 files
  - `MockCapturer`: 15 refs in 3 files
  - `MockChatter`: 12 refs in 3 files
  - `MockCostTracker`: 10 refs in 5 files (combined with `MockEngineCostTracker`'s 1)
  - `MockLogger`: 9 refs in 2 files
  - `MockTurnsLogger`: 5 refs in 3 files
  - `MockPruningPolicy`: 4 refs in 1 file
  - `MockTransformer`: 3 refs in 3 files
  - `MockEventBusFail`: 3 refs in 2 files
  - `PanicRegistry`: 2 refs in 1 file
  - **Total: 358 references rewritten across 40 files.**
- **Tests**: PASS (61 packages — unchanged from Session 3; no new packages created).
- **Race detector**: PASS.
- **Architecture verification**: ✅ no new violations.
- **Architectural lint**: zero non-test files import `agenttest` or `agentinternal`.
- **Leaf rule preserved**: `agenttest/` does NOT import `internal/agent`. Verified via grep.

### Step-by-step batching that worked

The 14 new files in `agenttest/` were created in 4 batches of 3–5, with `go build ./internal/agent/agenttest/...` between each batch as a checkpoint. All 4 batches built first try. The bulk sed pass for 358 call-site references in 40 files completed in one shot, with `goimports -w .` handling all import bookkeeping (added new agenttest imports in some files, removed orphaned testutil imports in others).

### Lessons added (apply in Sessions 5+)

1. **Bulk extraction is now mechanical and safe.** With the leaf rule structurally enforced (Session 3) and 4 batches of mocks completed, the workflow has stabilized: read source → check leaf-safety → write per-type files → build-check per batch → bulk sed → goimports → run verification. A single AGENT-bucket session can comfortably handle 15+ symbols if the destination package is otherwise stable.

2. **`MockEngineCostTracker = MockCostTracker` taught us about dangling aliases.** When the audit lists an INLINE symbol that is a type alias to a symbol scheduled for relocation in the same session, the alias must be inlined at the same time (not deferred), otherwise it dangles. The fix is trivial — rewrite the single call site to use the underlying type directly — but it must NOT be missed. Future sessions: scan the source file for `type X = Y` aliases where `Y` is in scope.

3. **Stray methods can hide far from their type.** `MockTokenCounter.EstimateTokens` was at line 486 of `agent_mocks.go`, while the type was at line 144 — separated by 14 unrelated symbols. `get_type_info` correctly enumerated it as a method, but a naïve "copy the type definition and the contiguous methods below it" approach would have missed it. Future sessions: always cross-check `get_type_info`'s methods list against what your `replace_text` deletion captures.

4. **The `MockTurnsLogger` duplication was the audit's first surprise of this kind.** The same logical mock existed in two places (likely a result of a partial earlier copy-paste). The Step 3 conflict-detection process worked — both versions were read side-by-side, confirmed functionally identical, and the testutil version was deleted in favor of the agenttest one. Future sessions targeting the AGENT bucket should always grep the destination package for the symbol name before writing a new file.

### State of the testutil dump after Session 4

- Files remaining in `internal/domain/testutil/`: 11 (unchanged).
- **agent_mocks.go**: 508 → 38 LOC (-92%); now contains only `MockExecutor` (TOOLS bucket).
- Symbols remaining in testutil: 21 (was 36 after Session 3, lost 15 + 1 collapsed `MockTurnsLogger` + 1 collapsed `MockEngineCostTracker` = 17 net symbols — 36 - 17 + 2 already-merged equivalents = 21).
- Across the whole testutil dump:
  - **`buffer.go`**: NewSafeBuffer + SyncWriter + dead Buffer/SafeBuffer → GENERIC bucket
  - **`clock.go`**: MockClock + MockTicker → AGENT bucket (a future "small AGENT" session)
  - **`config_mocks.go`**: MockConfigLoader → AGENT bucket
  - **`event_mocks.go`**: TestEventBus, MockEventBus → EVENTS bucket
  - **`misc_mocks.go`**: MockEntropySource, MockEstimator, TestifyMockClock → AGENT bucket
  - **`security_mocks.go`**: 3 symbols → SECURITY/TOOLS bucket
  - **`ui_mocks.go`**: 2 symbols → AGENT bucket per audit Risk Note #3
- **Recommended next session (Session 5)**: bundle `clock.go` (AGENT) + `config_mocks.go` (AGENT) + `misc_mocks.go` (AGENT) + `ui_mocks.go` (AGENT) — all 4 small files, all share the `agenttest/` destination. Estimated 8 symbols, similar mechanical workflow. After that, only EVENTS, SECURITY, TOOLS, and GENERIC buckets remain.


## Session 5 Outcome (small AGENT files bulk extraction)

- **Date**: 2026-04-19
- **Branch**: `refactor/testutil-extract-agent-smallfiles` (forked from Session 4 commit `7b40b4eb`)
- **Source files deleted (4)**: `clock.go`, `config_mocks.go`, `misc_mocks.go`, `ui_mocks.go` — all of their symbols moved.
- **Symbols relocated (8 distinct types, names preserved):**
  - `testutil.MockClock` + `testutil.MockTicker` → `agenttest.MockClock`, `agenttest.MockTicker` (`mock_clock.go`, bundled per audit explicit guidance because `MockClock.NewTicker()` returns `*MockTicker`)
  - `testutil.MockConfigLoader` → `agenttest.MockConfigLoader` (`mock_config_loader.go`)
  - `testutil.MockEntropySource` → `agenttest.MockEntropySource` (`mock_entropy_source.go`)
  - `testutil.MockEstimator` → `agenttest.MockEstimator` (`mock_estimator.go`) — preserved as-is per architect direction (rejected audit Risk Note #7 deletion suggestion)
  - `testutil.TestifyMockClock` → `agenttest.TestifyMockClock` (`testify_mock_clock.go`)
  - `testutil.MockUIRenderer` → `agenttest.MockUIRenderer` (`mock_ui_renderer.go`) — AGENT bucket per audit Risk Note #3 (despite the name, all consumers are agent/session tests)
  - `testutil.MockHistoryRenderer` → `agenttest.MockHistoryRenderer` (`mock_history_renderer.go`) — same
- **New files in `agenttest/`**: 7 (1 per type, with `MockClock` + `MockTicker` sharing `mock_clock.go`).
- **Call sites updated**: 22 files, **127 references** rewritten in a single sed pass:
  - `MockUIRenderer`: 59 refs in 9 files (densest)
  - `MockClock`: 38 refs in 11 files
  - `MockHistoryRenderer`: 14 refs in 2 files
  - `MockConfigLoader`: 5 refs in 2 files
  - `MockEntropySource`: 4 refs in 2 files
  - `TestifyMockClock`: 4 refs in 2 files
  - `MockEstimator`: 2 refs in 1 file
  - `MockTicker`: 1 ref in 1 file
- **Tests**: PASS (61 packages — unchanged).
- **Race detector**: PASS.
- **Architecture verification**: ✅ no new violations.
- **Architectural lint**: zero non-test files import `agenttest` or `agentinternal`.
- **Leaf rule preserved**: `agenttest/` does NOT import `internal/agent`. Verified via grep.

### Workflow notes

- All 7 destination files were written in one batch (small symbols, low risk), with one consolidated `go build ./internal/agent/agenttest/...` checkpoint that passed first try.
- Source-file-deletion approach worked cleanly because every source file's content was 100% in scope (different from Session 4, where `agent_mocks.go` had to be rewritten-from-scratch to keep `MockExecutor` for the TOOLS bucket).
- The `inframock` aliased imports (in 3 files: `internal/ui/renderer_test.go`, `internal/tools/workspace/process_executor_test.go`, `internal/domain/events/events_test.go`) were verified to NOT reference any in-scope symbol — they all only use `inframock.NewSafeBuffer` (GENERIC bucket, future session).
- Total broken-state-window between source-file deletion and call-site rewrite: < 60 seconds.

### State of the testutil dump after Session 5

- Files remaining in `internal/domain/testutil/`: **7** (was 11):
  - `agent_mocks.go` (38 LOC, only `MockExecutor` — TOOLS bucket)
  - `buffer.go` + `buffer_test.go` (GENERIC bucket: `NewSafeBuffer`, `SyncWriter`)
  - `event_mocks.go` + `events_test.go` (EVENTS bucket: `TestEventBus`, `MockEventBus`, `CountingEventBus`+`NewCountingEventBus` INLINE)
  - `exec_test.go` (TOOLS bucket — tests `MockExecutor`)
  - `security_mocks.go` (mixed SECURITY/TOOLS bucket: `MockSecurityManager`, `MockInteractor`, `MockCommandValidator`)
- **Total `testutil` LOC: 690** (was 938 after Session 4; was 1664 baseline). Net reduction since baseline: **-58%.**
- Symbols remaining in testutil: ~13 (of original 39).
- All AGENT-bucket symbols are now relocated. The remaining buckets are: EVENTS, SECURITY, TOOLS, GENERIC.

### Recommended Session 6

Two viable bundles, architect to pick:

- **Option α (EVENTS bundle, ~5 symbols)**: Move `event_mocks.go` to a new `internal/domain/events/eventstest/` sub-package. `TestEventBus` and `MockEventBus` are the cross-cutting consumers; `CountingEventBus`+`NewCountingEventBus` get inlined into their sole consumer file (`tests/integration/agent/session/context_manager_robustness_test.go`). This establishes the EVENTS bucket destination for the first time. The `events_test.go` file (testing `TestEventBus`) follows.

- **Option β (TOOLS bundle, ~5 symbols)**: Move `MockExecutor` (+ `exec_test.go`), `MockSecurityManager`, and `MockCommandValidator` to a new `internal/tools/toolstest/` sub-package per audit Risk Note #1. Note: `MockSecurityManager` has a single consumer in `internal/agent/orchestrator` that may need to stay on the security bucket — verify in Step 2. This establishes the TOOLS bucket destination.

Suggest Option α first because (a) `event_mocks.go` is mostly self-contained and (b) it depends only on the `domain/events` package, making the leaf-rule check trivial.


## Session 6 Outcome (EVENTS bucket extraction + CountingEventBus inline)

- **Date**: 2026-04-19
- **Branch**: `refactor/testutil-extract-events` (forked from Session 5 commit `1f83b93c`)
- **Source files deleted (2)**: `event_mocks.go`, `events_test.go` — all symbols and tests moved.
- **New sub-package created**: `internal/domain/events/eventstest/` — the **first** `*test` sub-package in the **domain** layer (the third architectural ring after `infrastructure/persistence/persistencetest/` from Session 2 and `agent/agenttest/` from Session 3). The `<layer>/<pkg>/<pkg>test/` convention is now proven across all three rings.
- **Symbols relocated (2 distinct types, names preserved):**
  - `testutil.TestEventBus` → `eventstest.TestEventBus` (`test_event_bus.go`)
  - `testutil.MockEventBus` (a type alias for `TestEventBus`) → `eventstest.MockEventBus` (`mock_event_bus.go`, preserved as `type MockEventBus = TestEventBus`)
- **Symbols inlined (audit INLINE bucket)**:
  - `testutil.CountingEventBus` → `countingEventBus` (lowercased; private to the consumer file) inlined into `tests/integration/agent/session/context_manager_robustness_test.go`.
  - `testutil.NewCountingEventBus` → `newCountingEventBus` (lowercased) inlined into the same file.
  - `TestCountingEventBus` test function inlined into the same file, alongside the type. Used a custom `myCountingEvent` helper rather than reusing `myEvent`/`otherEvent` from the original `testutil/events_test.go` (those go to `eventstest_test`; not reachable from `session_test`).
- **Tests moved**: `TestTestEventBus`, `TestTestEventBus_Subscribe`, `TestTestEventBus_NoOps` (along with the unexported `myEvent` and `otherEvent` event types they use) → `internal/domain/events/eventstest/test_event_bus_test.go`. Used external test package `package eventstest_test` (the standard Go idiom for testing a public API).
- **Call sites updated**: 14 files, **~45 references** rewritten in a single sed pass:
  - `testutil.MockEventBus` → `eventstest.MockEventBus`: 32 refs in 6 files
  - `testutil.TestEventBus` → `eventstest.TestEventBus`: 13 refs in 9 files (some files had both)
- **Tests**: PASS (62 packages — was 61; +1 for the new `eventstest` package, which is the **first `*test` sub-package to ship its own test suite** instead of being `[no test files]`).
- **Race detector**: PASS.
- **Architecture verification**: ✅ no new violations.
- **Architectural lint**: zero non-test files import `eventstest`.

### Notable details

1. **Type alias preservation.** `MockEventBus = TestEventBus` was kept as an alias rather than collapsed because consumers across the codebase mix the two names (32 refs to `MockEventBus` vs 13 to `TestEventBus`). Collapsing now would force a third name to win on the rewrite — needless churn. The alias travels to `eventstest` and is documented in its own file with a "prefer TestEventBus directly" GoDoc.
2. **Package GoDoc placement.** Following the established convention from Sessions 2–4: package GoDoc lives in the FIRST file alphabetically (`mock_event_bus.go` would sort first, but conceptually `test_event_bus.go` is the primary symbol — the GoDoc went there to keep semantic primacy over alphabetical ordering, matching Session 3's `mock_gateway.go` choice).
3. **External test package convention.** This session was the first to need a `_test.go` file inside a new `*test` sub-package. Used `package eventstest_test` (external) — the Go idiomatic choice for testing a public API surface. Sets a precedent for future sessions where test-helper sub-packages need self-tests.
4. **The leaf-rule check was trivial here**, as predicted — `eventstest` lives at `internal/domain/events/eventstest/`, sibling to `events`. Domain packages are leaf-friendly by definition; no upward dependency is possible. Contrast Session 3 where `agenttest` had to extract `agentinternal` to break a cycle.

### State of the testutil dump after Session 6

- Files remaining in `internal/domain/testutil/`: **5** (was 7):
  - `agent_mocks.go` (38 LOC) — only `MockExecutor` (TOOLS bucket)
  - `buffer.go` + `buffer_test.go` (108 + 42 LOC = 150 total) — GENERIC bucket: `NewSafeBuffer`, `SyncWriter`
  - `exec_test.go` (45 LOC) — TOOLS bucket; tests `MockExecutor`
  - `security_mocks.go` (199 LOC) — mixed SECURITY/TOOLS bucket: `MockSecurityManager`, `MockInteractor`, `MockCommandValidator`
- **Total `testutil` LOC: 432** (was 690 after Session 5; baseline was 1,664). Net reduction since baseline: **−74%.**
- Symbols remaining in testutil: ~7 of original 39.
- Buckets remaining: TOOLS (3 symbols across 2 files), SECURITY (1 symbol — `MockInteractor`), GENERIC (3 symbols including 2 unexported).

### Recommended Session 7

Three viable options (architect to pick):

- **Option α (TOOLS bundle, biggest impact)**: Extract `MockExecutor` (+ `exec_test.go`) and per audit Risk Note #1, `MockSecurityManager` and `MockCommandValidator` to a new `internal/tools/toolstest/` sub-package. Note: per audit, `MockSecurityManager` has a single non-tools consumer (`internal/agent/orchestrator`) — verify in Step 2; if consumer count makes that mock truly cross-cutting, route it to the SECURITY bucket instead. Bundling here would shrink `agent_mocks.go` to deletion and fully drain `exec_test.go` and most of `security_mocks.go`. Estimated 4 symbols, ~20+ files.

- **Option β (SECURITY bundle)**: Extract `MockInteractor` to a new `internal/infrastructure/security/securitytest/` sub-package. Smaller scope (1 symbol, 3 consumer files per audit). Establishes the **fourth** `*test` sub-package shape.

- **Option γ (GENERIC bundle, leaves the dump completely empty of mocks)**: Extract `NewSafeBuffer`, `SyncWriter` (and unexport `Buffer`/`SafeBuffer`) to a new `internal/pkg/testfixtures/` package. After this, `testutil/` would contain ONLY mocks (TOOLS + SECURITY).

Suggest Option α first — it's the largest remaining bucket and `agent_mocks.go` going to zero LOC is symbolic milestone for the refactor. The SECURITY/GENERIC bundles can be a tidy pair for Session 8.


## Session 7 Outcome (TOOLS bucket extraction + SECURITY bucket re-bucketed and collapsed)

- **Date**: 2026-04-19
- **Branch**: `refactor/testutil-extract-tools-security` (forked from Session 6 commit `f8ef81a6`)
- **Source files deleted (3)**: `agent_mocks.go`, `exec_test.go`, `security_mocks.go` — all symbols and tests moved.
- **New sub-package created**: `internal/tools/toolstest/` — the **fourth** `*test` sub-package and the first one in the **tools** layer. After this session the convention is established across all four architectural rings (`infrastructure`, `agent`, `domain/events`, `tools`).
- **Symbols relocated (4 distinct types, names preserved):**
  - `testutil.MockExecutor` → `toolstest.MockExecutor` (`mock_executor.go`)
  - `testutil.MockSecurityManager` → `toolstest.MockSecurityManager` (`mock_security_manager.go`) per audit Risk Note #1
  - `testutil.MockCommandValidator` → `toolstest.MockCommandValidator` (`mock_command_validator.go`)
  - `testutil.MockInteractor` → `toolstest.MockInteractor` (`mock_interactor.go`) — **re-bucketed from SECURITY → TOOLS** (see audit revision below)
- **Tests moved**: `TestMockExecutor` → `internal/tools/toolstest/mock_executor_test.go` (external `package toolstest_test`).
- **Cross-layer test cleanup**: `internal/agent/orchestrator/engine_test.go` had a single `testutil.MockSecurityManager` call site. The naive rewrite to `toolstest.MockSecurityManager` introduced a layer violation (`agent → tools`) flagged by `verify_architecture`. Replaced with a 14-line package-local `noopSecurityManager` stub at the bottom of the same file. This stub satisfies `domain_security.Manager` with all-zero-value methods and is sufficient because the only assertion in that test is pointer equality of the stored SM. Audit doc adds Lesson #1 below.
- **Call sites updated**: 37 files, **~145 references** rewritten in a single sed pass (`testutil.(MockExecutor|MockSecurityManager|MockCommandValidator|MockInteractor) → toolstest.\1`). Per-symbol rough counts:
  - `MockSecurityManager`: ~125 refs across 33 files (the densest)
  - `MockCommandValidator`: ~17 refs in 7 files
  - `MockInteractor`: 4 refs in 2 files
  - `MockExecutor`: 1 real ref + 2 comment refs in 2 files
- **Tests**: PASS (63 packages — was 62; +1 for the new `toolstest` package, which ships its own test suite).
- **Race detector**: PASS (`go test -count=1 -race ./...`).
- **Architecture verification**: ✅ no new violations (after the noopSecurityManager remediation).
- **Architectural lint**: zero non-test files import `toolstest`.
- **Leaf rule preserved**: `toolstest/` does NOT import `internal/tools` or any of its sub-packages. Verified via grep.

### Audit revision: SECURITY bucket has zero genuine occupants

The original audit (Session 1 row at line 144) routed `MockInteractor` to the SECURITY bucket on the basis of "3 references across `infrastructure/security` and `tools/{workspace,developer}`". Step 3 conflict-detection in this session proved this row was incorrect:

- `internal/infrastructure/security/` has its **own**, package-private `MockInteractor` declared in `interaction_mock_test.go` (`package security`). All 32 in-package references inside `infrastructure/security/` (across `auditor_test.go`, `interaction_test.go`, `manager_test.go`, `policy_test.go`) resolve to **that** local type, not `testutil.MockInteractor`.
- The two types are **NOT functionally equivalent** — they differ in concurrency (`sync.Mutex` vs none), context-cancellation handling (`select { case <-ctx.Done(): }` checks vs none), `ReadSingleKey` semantics (lowercased first char vs full Answer), and the presence of a `Prompts` slice (absent vs present).
- The actual consumers of `testutil.MockInteractor` are exclusively in `internal/tools/`: `internal/tools/workspace/interaction_test.go` (3 sites) and `internal/tools/developer/dev_test.go` (1 interface-assertion line).

The SECURITY bucket therefore has **zero genuine occupants**. Per architect-style judgment matching Session 6's framing ("avoid spinning up half-empty packages"), `MockInteractor` was moved to TOOLS alongside the other three mocks rather than getting its own otherwise-empty `internal/infrastructure/security/securitytest/` sub-package. The audit row at line 144 is now superseded by this section.

### Notable details

1. **Layer violation surfaced by the rewrite, not by prior code.** The pre-Session 7 `testutil.MockSecurityManager` import in `internal/agent/orchestrator/engine_test.go` flowed through the **domain** layer (`internal/domain/testutil`), which `verify_architecture` permits from the `agent` layer. Renaming the qualifier to `toolstest` re-routed the import through the **tools** layer, which the verifier forbids from `agent`. The fix (a 14-line package-local stub) preserves test intent without the cross-layer dep. **Lesson for Session 8 and beyond**: when a relocation moves a symbol *upward in the layer stack* (here: domain → tools), every cross-layer caller must be re-evaluated; running `verify_architecture` is mandatory after each session.
2. **`agent_mocks.go` reaches LOC 0 and is deleted** — symbolic milestone. The file shrank 508 → 38 in Session 4 and is now gone. Three source files deleted in this session (matching Session 5's clean-deletion pattern).
3. **No type-aliases dangled** (Session 4 lesson #2) — none of the four moved symbols had aliases pointing at them.
4. **Two `MockInteractor` types with the same name now coexist legitimately**, one in `package security` (visible only inside `internal/infrastructure/security` tests) and one in `package toolstest` (consumed by `internal/tools/*` tests). They never appear in the same scope, so there is no ambiguity. The `mock_interactor.go` GoDoc explicitly documents the divergence so future readers don't try to fold them.

### State of the testutil dump after Session 7

- Files remaining in `internal/domain/testutil/`: **2** (was 5):
  - `buffer.go` (108 LOC) — GENERIC bucket: `Buffer`, `SafeBuffer`, `NewSafeBuffer`, `SyncWriter`
  - `buffer_test.go` (42 LOC) — GENERIC bucket: `TestSafeBuffer_Contract`
- **Total `testutil` LOC: 150** (was 432 after Session 6; baseline was 1,664). Net reduction since baseline: **−91%.**
- Symbols remaining in testutil: **4** of original 39 (down from 7 after Session 6). 2 of those 4 are unexported (`Buffer`, `SafeBuffer`) and slated to be lowercased during the Session 8 relocation.
- Bucket remaining: **GENERIC** only.
- Sub-package count: **4** `*test` sub-packages (`persistencetest`, `agenttest`, `eventstest`, `toolstest`) + **1** sibling-internal package (`agentinternal`). The convention is fully proven across the codebase.

### Recommended Session 8 (the finale)

Move `buffer.go` + `buffer_test.go` to a new `internal/pkg/testfixtures/` package per the audit's GENERIC bucket assignment. Lowercase `Buffer` → `buffer` and `SafeBuffer` → `safeBuffer` per the audit's "DEAD as a name" finding (zero external named references; reachable only via `NewSafeBuffer`). Update ~5 consumer files. After Session 8, `internal/domain/testutil/` will be deleted entirely and the refactor is complete.


## Session 8 Outcome (GENERIC bucket extraction; testutil deleted; refactor complete)

- **Date**: 2026-04-19
- **Branch**: `refactor/testutil-finale` (forked from Session 7 commit `0988663a`)
- **Source files deleted (3)**: `buffer.go`, `buffer_test.go`, **and the `internal/domain/testutil/` directory itself**. The dump is gone.
- **New sub-package created**: `internal/pkg/testfixtures/` — the **fifth** `*test`-style sub-package and the **first** in the `internal/pkg/` shared-utilities tree. It hosts test primitives that are genuinely cross-cutting (multiple unrelated consumer packages) and not specific to any single production package.
- **Symbols relocated (4 distinct symbols + 1 test):**
  - `testutil.Buffer` (interface) → `testfixtures.buffer` — **lowercased** per audit "DEAD as a name" finding (zero external named references)
  - `testutil.SafeBuffer` (struct) → `testfixtures.safeBuffer` — **lowercased**, same rationale
  - `testutil.NewSafeBuffer` (constructor) → `testfixtures.NewSafeBuffer` — kept exported (return type is now the unexported `buffer` interface, which deliberately tightens the API surface)
  - `testutil.SyncWriter` (struct) → `testfixtures.SyncWriter` — **kept exported** because consumers construct it as a struct literal (`&testfixtures.SyncWriter{Writer: ..., OnWrite: ...}`), which requires both the type and its public fields to be accessible
  - `TestSafeBuffer_Contract` test → `internal/pkg/testfixtures/safebuffer_test.go` (internal `package testfixtures` so it can reference the lowercased types)
- **Files in destination**: 3 (`safebuffer.go`, `syncwriter.go`, `safebuffer_test.go`); 150 LOC total — exactly matching the source-file LOC. No content lost; only renaming and re-distribution.
- **Call sites updated**: **6 files**, **~30 references** rewritten in two `sed` passes (one for the unaliased `testutil.X` form, one for the `inframock.X` aliased form):
  - `internal/agent/session/ui_bridge_panic_test.go`: 2 refs (unaliased)
  - `internal/agent/session/context_manager_test.go`: 2 refs (unaliased, `SyncWriter`)
  - `tests/integration/agent/session/spinner_integration_test.go`: 4 refs (unaliased; both symbols)
  - `internal/domain/events/events_test.go`: 1 ref (aliased as `inframock`)
  - `internal/tools/workspace/process_executor_test.go`: 3 refs (aliased as `inframock`)
  - `internal/ui/renderer_test.go`: **17 refs** (aliased as `inframock` — the densest single file)
- **Tests**: PASS (63 packages — same count as Session 7, even though `testfixtures` is +1 and `testutil` is −1 — the net is zero, and `testfixtures` ships its own test suite from day one).
- **Race detector**: PASS (`go test -count=1 -race ./...` — full suite, ~17s wall clock).
- **Architecture verification**: ✅ no new violations.
- **Architectural lint**: all 6 importers of `testfixtures` are `_test.go` files. Verified via `grep -rn "internal/pkg/testfixtures" --include="*.go" .` — every result row ends in `_test.go`.

### Audit-vs-reality reconciliation

The audit row for `NewSafeBuffer` listed 3 importer packages (session, ui, integration/session, with a "+1 hit in events" footnote). Reality: **6 files across 5 packages**, because the audit did not account for the **3 files using the `inframock` import alias** (flagged in the Session 5 outcome notes but never reconciled into the audit table itself). This is the third confirmation of the **±25% audit accuracy lesson** from Session 2 — and the second confirmation of the specific **alias-grep blind spot** from Session 2's lesson #1. Both `sed` passes (one per qualifier form) caught everything in a single rewrite cycle; no broken-state window exceeded ~30 seconds.

### Closing artifacts shipped

This session's deliverables exceed the strict scope of relocation, by design — the dissolution is complete and the convention now needs guard rails:

1. **ADR-021** (`docs/adr/2026-04-test-doubles-in-pkgtest-subpackages.md`) formalizes the `<pkg>/<pkg>test/` convention. Documents the rules (leaf rule, escape hatch via sibling `<pkg>internal/`, naming convention for fakes vs mocks, no production imports), the alternatives considered, and the enforcement mechanism. The ADR explicitly references this audit doc for the change history. Indexed in `docs/adr/README.md` as ADR-021.
2. **This retrospective section** captures the final-state metrics, the pattern catalog used across all 8 sessions, and the lessons consolidated for future contributors.

### Pattern catalog (consolidated from Sessions 2–8)

For any future similar refactor, the workflow that emerged is:

1. **Audit the dump** — enumerate every exported symbol with consumer counts. Reconcile two grep modes: (a) the qualifier (`testutil.X`), (b) the import path (`internal/domain/testutil`). Aliases lurk in the latter and are missed by the former.
2. **Bucket symbols by consumer locality**, not by source-file co-location. The ones the audit said belonged together because they shared a file (`agent_mocks.go` had 21 symbols!) often belonged in different destinations.
3. **For each session, pick a bucket that won't fight you on the leaf rule.** Domain-layer destinations (`eventstest`) were trivially leaf-safe. Agent-layer destinations (`agenttest`) needed prophylactic checking and Session 3 needed a `agentinternal` escape-hatch package.
4. **Write per-symbol files** in the destination, in batches of 3–5 with a `go build` checkpoint between each batch. Trivially related primitives (interface + struct + constructor) may share a file, e.g. `safebuffer.go` here.
5. **Bulk `sed` for the qualifier rewrite**, in one pass. Do not edit by hand — `sed | goimports` is faster, lower-error, and the diff is reviewable.
6. **Run `goimports -w`** (NOT `go fmt`) — it adds new imports and removes dead ones automatically. Hand-editing imports is the slowest part of the workflow.
7. **Run `verify_architecture` after every session.** Session 7 caught a layer violation that became visible only because of the rewrite. Session 8 had none.
8. **Delete source files outright when 100% of their symbols leave** (Session 5 lesson). Git rename detection preserves blame across the move.

### Lessons added (apply to any future similar refactor)

1. **Aliased imports are a tax on every refactor.** `inframock "internal/domain/testutil"` cost extra grep cycles in Sessions 2 (twice), 5 (acknowledged but deferred), and 8 (closed out). Going forward: code review should reject any import alias that names something other than what is actually being imported. An alias is acceptable to disambiguate a name collision, never to lie about the source.
2. **DEAD-as-a-name is a real category, separate from DEAD-and-deletable.** `Buffer` and `SafeBuffer` were flagged as DEAD by static dead-code tooling because no consumer named them directly. They were ALIVE through `NewSafeBuffer`. The right disposition was lowercase-not-delete, which would have been missed by either an automated dead-code-deletion bot or a literal reading of the audit row. Future audits should distinguish the two categories explicitly.
3. **Constructor return types can deliberately tighten the API.** `NewSafeBuffer` now returns an unexported `buffer` interface — consumers can call its (exported) methods but cannot type-assert to a named concrete type. This is a feature, not a bug: it forces test code to depend on behavior, not implementation. No consumer broke; the audit's risk-note prediction held.
4. **A finale session should ship more than relocation.** The convention used across 8 sessions was implicit until Session 8 wrote the ADR. Without the ADR, future contributors looking at 5 `*test` sub-packages and an empty `testutil/` slot might reaggregate. The ADR codifies the rule and the rationale; the retrospective captures the lived experience of arriving at it.

### Final state of the testutil dump (after Session 8)

- **Files in `internal/domain/testutil/`: 0.**
- **Directory `internal/domain/testutil/`: deleted.**
- **Symbols remaining in testutil: 0** (all 39 of the original symbols have been relocated, inlined, or collapsed across Sessions 2–8).
- **Refactor complete.**

### Final metrics (8 sessions, January–April 2026)

| Metric | Baseline (pre-Session 1) | After Session 8 |
|---|---|---|
| `internal/domain/testutil/` LOC | 1,664 | **0** |
| `internal/domain/testutil/` files | 12 | **0** |
| `internal/domain/testutil/` exported symbols | 39 | **0** |
| `*test` sub-packages | 0 | **5** (`persistencetest`, `agenttest`, `eventstest`, `toolstest`, `testfixtures`) |
| Sibling internal helper packages | 0 | **1** (`agentinternal`) |
| Layer violations (`verify_architecture`) | latent (1 surfaced in Session 7) | **0** |
| Total refactor sessions | — | 8 |
| Total references rewritten across all sessions | — | ~700 |
| Total files modified across all sessions | — | ~85 |
| Architectural rules enforced | informal | ADR-021 |
| Domain layer purity | violated | restored |

### Recommended follow-ups (for a future audit, not this session)

1. **Add the ADR-021 grep lint to CI.** A check that fails the build if any new file matches `internal/.*/testutil/.*\.go` would cement the convention.
2. **Audit `agentinternal/` for unused symbols** now that the AGENT bucket extraction is fully complete. Some helpers may have lost their last consumer during Sessions 4–7.
3. **Verify the audit's Risk Note #2 was correctly resolved.** `NewOSFileSystem` now exists in two places (`persistence.NewOSFileSystem` for production with retries+atomic writes; `persistencetest.NewPlainOSFileSystem` for tests, plain). If anyone introduces a third "OSFileSystem-ish" thing, that's worth catching early.
4. **Consider whether `MockEstimator` (preserved in Session 5 against the audit's collapse-suggestion) is now actually unused.** Audit Risk Note #7 flagged it as a candidate for collapse-into-`MockTokenCounter`. The architect rejected the collapse for cohesion reasons; revisit if the gap between the two has grown.
