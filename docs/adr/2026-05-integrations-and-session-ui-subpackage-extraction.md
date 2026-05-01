<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-027: Continue Sub-Package Extraction — Integrations (ado/atlassian) and Session (ui/)

## Status
Accepted

## Context

ADR-026 established the sub-package extraction pattern for `session/context/`, demonstrating that extracting a cohesive concern cluster into a child package reduces file count, improves navigability, and enables independent testing — all without semantic changes.

Two remaining clusters in the codebase violated the single-responsibility thresholds established by ADR-008 ("Domain Decomposition Strategy"):

| Cluster | Files | Concern families | Threshold violation |
|---|---|---|---|
| `internal/tools/integrations/` | 25 | Azure DevOps (11), Atlassian (6), Other (8) | Three integration families in one flat package |
| `internal/agent/session/` | 14 `ui_bridge_*` files | UI Bridge actor | Still in parent after `context/` extraction; largest remaining cluster |

ADR-025 had already decomposed `ui_bridge.go` into 5 cohesive files (dispatcher, event queue, spinner, state machine, core bridge). Extraction to a sub-package was the natural next step — the file count and internal cohesion justified its own namespace.

### Why Now

| Trigger | Impact |
|---|---|
| `integrations/` hit 25 files | Navigation cost: linear scan of flat namespace to find a specific integration provider |
| `session/` still at 41 files after `context/` extraction | The `ui_bridge_*` prefix pattern is a naming convention papering over a missing package boundary |
| ADR-021 ("Test Doubles in `*test` Sub-Packages") | Established that test fixtures co-locate with their subjects; sub-packages carry their own test infrastructure |

### Scope Boundaries

This ADR covers two extractions executed in sequence on the same branch (Issue #101):

1. **Workstream A**: `integrations/ado/` and `integrations/atlassian/`
2. **Workstream B**: `session/ui/`

Both follow the same move-then-rename-then-wire pattern established by ADR-026. The two workstreams share this ADR because they share the same rationale, constraints, and acceptance criteria — only the package subject differs.

## Decision

### 1. Extract `integrations/ado/` and `integrations/atlassian/`

**Files moved into `integrations/ado/`** (5 source + 6 test):

| Old path (`integrations/`) | New path (`integrations/ado/`) | Rename rationale |
|---|---|---|
| `azure_devops.go` | `ado/azure_devops.go` | Retained — primary type file |
| `azure_devops_pipeline.go` | `ado/pipeline.go` | Drop `azure_devops_` prefix |
| `azure_devops_pipeline_logs.go` | `ado/pipeline_logs.go` | Drop `azure_devops_` prefix |
| `azure_devops_policy.go` | `ado/policy.go` | Drop `azure_devops_` prefix |
| `azure_devops_pr.go` | `ado/pr.go` | Drop `azure_devops_` prefix |
| `azure_devops_test.go` | `ado/azure_devops_test.go` | Retained |
| `azure_devops_pipeline_test.go` | `ado/pipeline_test.go` | Drop prefix |
| `azure_devops_fault_test.go` | `ado/fault_test.go` | Drop prefix |
| `azure_devops_integration_test.go` | `ado/integration_test.go` | Drop prefix |
| `azure_devops_negative_test.go` | `ado/negative_test.go` | Drop prefix |
| `log_processor_test.go` | `ado/log_processor_test.go` | Retained |

**Files moved into `integrations/atlassian/`** (3 source + 3 test):

| Old path | New path | Rename rationale |
|---|---|---|
| `atlassian_common.go` | `atlassian/provider.go` | Name reflects role: shared auth/token provider |
| `atlassian_common_test.go` | `atlassian/provider_test.go` | Mirrors source rename |
| `confluence.go` | `atlassian/confluence.go` | Retained |
| `confluence_test.go` | `atlassian/confluence_test.go` | Retained |
| `jira.go` | `atlassian/jira.go` | Retained |
| `jira_test.go` | `atlassian/jira_test.go` | Retained |

**Export decisions for `ado/`:**

| Symbol | Decision | Rationale |
|---|---|---|
| `adoManager` → `AdoManager` | Exported | Needed by `registration.go` composition root and shared parent tests |
| `newADOManager` → `NewADOManager` | Exported | Constructor for composition root |
| `adoOption` → `AdoOption` | Exported | Functional-options type exposed to `registration.go` |
| `withHTTPClient` → `WithHTTPClient` | Exported | Functional option |
| `withToken` → `WithToken` | Exported | Functional option |
| `withBaseURL` → `WithBaseURL` | Exported | Needed by parent-level fault-injection tests |
| `executeRequest` → `ExecuteRequest` | Exported | Needed by parent-level fault-injection tests |
| `checkResponseError` → `CheckResponseError` | Exported | Needed by parent-level I/O error tests |
| `baseURL` → `BaseURL` | Exported (field) | Needed by parent-level tests |
| 20 handler methods (`adoGet*` → `AdoGet*`) | Exported | All consumed by `registration.go` tool specs |

**Export decisions for `atlassian/`:**

| Symbol | Decision | Rationale |
|---|---|---|
| `atlassianProvider` → `AtlassianProvider` | Exported | Needed by parent-level fault-injection and wait-time tests |
| `newAtlassianProvider` → `NewAtlassianProvider` | Exported | Constructor |
| `email`, `token`, `baseDelay` → `Email`, `Token`, `BaseDelay` | Exported (fields) | Needed by parent-level tests |
| `confluenceManager` → `ConfluenceManager` | Exported | Needed by parent-level fault-injection tests |
| `newConfluenceManager` → `NewConfluenceManager` | Exported | Constructor for `registration.go` |
| `jiraManager` → `JiraManager` | Exported | Needed by parent-level fault-injection tests |
| `newJiraManager` → `NewJiraManager` | Exported | Constructor for `registration.go` |
| `fetchSearchPage` → `FetchSearchPage` | Exported | Needed by parent-level tests |
| `getWaitTime` → `GetWaitTime` | Exported | Needed by parent-level wait-time tests |
| `confluenceSearch`/`Read`/`Write` → `ConfluenceSearch`/`Read`/`Write` | Exported | All consumed by `registration.go` tool specs |
| `jiraSearchIssues`/`jiraGetIssue` → `JiraSearchIssues`/`JiraGetIssue` | Exported | All consumed by `registration.go` tool specs |

### 2. Extract `session/ui/`

**Files moved into `session/ui/`** (5 source + 9 test):

| Old path (`session/`) | New path (`session/ui/`) | Rename rationale |
|---|---|---|
| `ui_bridge.go` | `ui/bridge.go` | Drop `ui_` prefix; package name `ui` already scopes it |
| `ui_bridge_dispatcher.go` | `ui/dispatcher.go` | Drop prefix |
| `ui_bridge_event_queue.go` | `ui/event_queue.go` | Drop prefix |
| `ui_bridge_spinner.go` | `ui/spinner.go` | Drop prefix |
| `ui_bridge_state_machine.go` | `ui/state_machine.go` | Drop prefix |
| `ui_bridge_test.go` | `ui/bridge_test.go` | Drop prefix |
| `ui_bridge_backpressure_test.go` | `ui/backpressure_test.go` | Drop prefix |
| `ui_bridge_concurrency_test.go` | `ui/concurrency_test.go` | Drop prefix |
| `ui_bridge_consent_test.go` | `ui/consent_test.go` | Drop prefix |
| `ui_bridge_fixture_test.go` | `ui/fixture_test.go` | Drop prefix |
| `ui_bridge_idempotency_test.go` | `ui/idempotency_test.go` | Drop prefix |
| `ui_bridge_panic_test.go` | `ui/panic_test.go` | Drop prefix |
| `ui_bridge_refactor_test.go` | `ui/refactor_test.go` | Drop prefix |
| `ui_bridge_routing_test.go` | `ui/routing_test.go` | Drop prefix |

**Type rename:**

| Old Name | New Name | Rationale |
|---|---|---|
| `UIBridge` | `Bridge` | Package name `ui` already provides the `UI` namespace |
| `NewUIBridge` | `NewBridge` | Constructor mirrors type rename |

Other types (`eventDispatcher`, `eventQueue`, `spinnerCoord`, `uiStateMachine`) were already correctly named — unexported, without redundant `UI` prefix — and required no renaming.

**Export decisions for `ui/`:**

| Symbol | Decision | Rationale |
|---|---|---|
| `UIBridge` → `Bridge` | Already exported | Type rename only; `session_manager.go` references `*ui.Bridge` |
| `NewUIBridge` → `NewBridge` | Already exported | Constructor for `session_manager.go` |
| `WithBridgeThoughts`, `WithBridgeTools`, etc. | Retained as-is | Already exported; package qualifier `ui.WithBridgeThoughts` is clear |
| `withBridgeClock` → `WithBridgeClock` | Exported | Previously unexported; needed by `session_manager.go` |
| `wg` → `WG` | Exported (field) | Previously unexported; accessed by `session_manager.go` for teardown |

### Coupling Hazard Resolutions

#### 1. `registration.go` as Composition Root

`registration.go` is the natural composition root for the `integrations/` package. After extraction:

```go
// registration.go — imports both sub-packages
import (
    "github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
    "github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"
)

func registerAzureDevOps(r tools.Registry, sm ..., client ...) error {
    m := ado.NewADOManager(sm, ado.WithHTTPClient(client), ado.WithToken(token))
    // ... register 20 ado_* tools using m.AdoGet* handlers ...
}

func registerConfluence(r tools.Registry, sm ..., client ...) error {
    m, err := atlassian.NewConfluenceManager(sm, client)
    // ... register confluence_* tools using m.Confluence* handlers ...
}
```

The import direction is `integrations → integrations/ado` and `integrations → integrations/atlassian`. These are acyclic parent-child dependencies.

#### 2. `session_manager.go` as Composition Root

`session_manager.go` is the natural composition root for `session/`. After extraction:

```go
// session_manager.go — imports ui sub-package
import "github.com/gosharplite/tell-me-go/internal/agent/session/ui"

func (o *sessionManager) setupUIRendering(...) *ui.Bridge {
    bridge := ui.NewBridge(o.UIRenderer,
        ui.WithBridgeThoughts(cfg.ShowThoughts),
        ui.WithBridgeTools(cfg.ShowTools),
        // ...
    )
    return bridge
}
```

The import direction is `session → session/ui`. Acyclic; matches the `session → session/context` pattern established by ADR-026.

#### 3. Shared Test Fixtures

**`fault_injection_test.go` and `io_error_test.go`** (parent `integrations/` package) test error paths across both ADO and Atlassian sub-packages. These stayed at the parent level and import both `ado` and `atlassian`:

```go
// integrations/fault_injection_test.go
import (
    "github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
    "github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"
)
```

`mockSecurityManager`, previously shared via the now-moved `azure_devops_test.go`, was duplicated locally in `fault_injection_test.go` — it implements `domain_security.Manager` and the duplication (~30 LOC) falls well within the ADR-026 threshold for acceptable fixture duplication.

**`panic_test.go`** uses external test package `package ui_test`. It references `ui.Bridge` via qualified import and calls `ui.SyncBridge` (exported from `ui/export_test.go`) for deterministic synchronization:

```go
// session/ui/panic_test.go — package ui_test
import "github.com/gosharplite/tell-me-go/internal/agent/session/ui"

bridge := ui.NewBridge(mRenderer, ui.WithBridgeThoughts(true), ...)
ui.SyncBridge(t, bridge, mRenderer)
```

**`syncBridge` and `syncWriter`** were moved from the parent `test_helpers_test.go` into `ui/helpers_test.go`, co-locating them with their consumers. A thin re-export (`ui/export_test.go`) exposes `SyncBridge` for the external `ui_test` package.

## Consequences

### Positive

- **`integrations/` drops from 25 to 10 files** (3 remaining integration families: media, network, teams; plus `registration.go` and shared tests). Each integration family is now independently navigable.
- **`session/` drops 14 UI bridge files**. After `context/` extraction (ADR-026) and `ui/` extraction (this ADR), the parent package contains only lifecycle orchestration, configuration, and skill injection.
- **Both sub-packages are independently testable** with their own test fixtures. `go test ./integrations/ado/...` runs only Azure DevOps tests; `go test ./session/ui/...` runs only UI bridge tests.
- **Open/Closed adherence**: adding a new Atlassian integration requires touching only `integrations/atlassian/`, not the parent. Adding a new UI event handler requires touching only `session/ui/`.
- **Establishes the sub-package extraction pattern** across two distinct domains (tools/integrations and agent/session), proving the pattern's general applicability.

### Negative

- **~25 symbols exported** (were unexported) to enable composition root access from `registration.go` and `session_manager.go`. Acceptable — all were already consumed by their respective composition roots; exporting them formalizes an existing dependency rather than creating a new one.
- **One-time mechanical migration cost** on ~80 files across 8 commits. Mitigated by `git mv` (preserves history), `perl` batch renames, and the small consumer surface (~20 lines in `registration.go`, ~10 lines in `session_manager.go`).
- **Slightly deeper import paths** in tests (`ado.NewADOManager` instead of `newADOManager`, `sessionui.NewBridge` instead of `session.NewUIBridge`). Acceptable trade-off for clarity — the package qualifier provides context about which subsystem the symbol belongs to.
- **Parent-level test files** (`fault_injection_test.go`, `io_error_test.go`) now import both `ado` and `atlassian`. These are the only files in the parent that cross-reference sub-packages for testing purposes.

### Neutral

- **No behavior changes**. This is a purely structural refactor.
- **No change to public API** of `internal/agent/agent.go`, `internal/cli`, or any external consumer. Only intra-package types and imports are affected.
- **`verify_architecture` results remain clean** — the new `integrations → integrations/ado`, `integrations → integrations/atlassian`, and `session → session/ui` dependencies are all acyclic parent-child relationships.
- **No change to test coverage**. All tests move with their source files.

## Implementation Plan

Executed on GitHub Issue #101 across 8 commits:

| Commit | Workstream | Task | Files |
|---|---|---|---|
| `ff6d653f` | A1 | Extract `integrations/ado/` | 11 moved |
| `cd0c786a` | A2 | Extract `integrations/atlassian/` | 6 moved |
| `de193112` | A3 | Wire `registration.go` | 14 edited |
| `a66acced` | A4 | Fix shared test fixtures | 16 edited |
| `dee9414f` | B1 | Extract `session/ui/` | 14 moved |
| `bc004664` | B2 | Rename `UIBridge` → `Bridge` | 8 edited |
| `079948ca` | B3 | Wire `session_manager.go` | 12 edited |
| `a992008d` | B4 | Lint, tests, integration fixes | 2 edited |

Acceptance gauntlet (all passing):

| Check | Result |
|---|---|
| `go build ./...` | ✅ Full-module build |
| `go test ./internal/agent/...` | ✅ All agent packages |
| `go test ./internal/tools/integrations/...` | ✅ All three integration packages |
| `go vet ./...` | ✅ Clean |
| `golangci-lint run` | ✅ Clean |
| `verify_architecture` | ✅ Acyclic |

## References

- ADR-008 (`2026-01-domain-decomposition-strategy.md`) — package decomposition principles and thresholds
- ADR-025 (`2026-04-uibridge-decomposition.md`) — prior intra-file decomposition of `ui_bridge.go` (sets precedent for this extraction)
- ADR-026 (`2026-04-session-context-subpackage-extraction.md`) — established the move-then-rename-then-wire sub-package extraction pattern
- ADR-021 (`2026-04-test-doubles-in-pkgtest-subpackages.md`) — test fixture co-location principles
- GitHub Issue #101 — execution tracking for this ADR
- PR #98 — Workstream A (integrations/ado and integrations/atlassian)
- PR #102 — Workstream B (session/ui)
