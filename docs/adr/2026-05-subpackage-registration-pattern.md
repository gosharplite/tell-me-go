<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-028: Delegate Registration Logic to Integration Sub-Packages

## Status
Accepted

## Context

ADR-027 extracted `integrations/ado/` and `integrations/atlassian/` as sub-packages, moving their domain logic (managers, types, API handlers) out of the parent `integrations/` package. However, the `registerX` functions for all six integration domains were left in the parent `registration.go` — an 824-line file with cyclomatic complexity peaking at 26 in `registerAzureDevOps` (industry ceiling: ≤10). This creates three problems:

### 1. God Object Anti-Pattern

`registration.go` contains six `registerX` functions spanning six unrelated integration domains:

| Function | Domain | Lines | Complexity |
|---|---|---|---|
| `registerMedia` | Media generation | ~45 | Low (sequential registration) |
| `registerNetwork` | HTTP/network | ~45 | Low (sequential registration) |
| `registerTeams` | Microsoft Teams | ~32 | Low (single tool) |
| `registerConfluence` | Atlassian Confluence | ~60 | Low (three tools) |
| `registerJira` | Atlassian Jira | ~40 | Low (two tools) |
| `registerAzureDevOps` | Azure DevOps | ~270+ | **26** (20 tool specs, dispatch logic) |

Each function is self-contained: it creates its own manager, declares tools, and registers them — with zero shared mutable state between registrations. The file is a God Object by aggregation, not by coupling.

### 2. Merge-Conflict Hotspot

Because all six registration functions live in the same file, any change to any integration's tool declarations touches `registration.go`. This creates a merge-conflict surface across six unrelated domains. A team member adding a Confluence tool and another adding an ADO pipeline tool both modify the same file for no architectural reason.

### 3. Violation of Open/Closed Principle

Adding a new integration (e.g., GitHub) requires touching `registration.go` directly — modifying the God Object rather than extending the system through a well-defined interface. This is the classic Open/Closed violation: the module is open for modification, not closed against it.

### Why Now

| Trigger | Impact |
|---|---|
| `registerAzureDevOps` at complexity 26 | Unmaintainable; any ADO tool change risks breaking the entire registration chain |
| Sub-packages already exist (ADR-027) | The `ado/` and `atlassian/` sub-packages are the natural homes for their registration logic |
| Issue #116 explicitly targets this decomposition | Pre-agreed scope; aligns with ADR-008 domain decomposition thresholds |

### Scope Boundaries

This ADR covers only the relocation of `registerX` functions from `registration.go` into their domain homes. It does **not** change any tool declarations, tool behavior, registration order, or error semantics.

## Decision

### 1. Each Integration Sub-Package Owns Its Own `Register` Function

**`ado.Register`** — moves `registerAzureDevOps` from `integrations/registration.go` into `integrations/ado/register.go` as an exported function:

```go
// internal/tools/integrations/ado/register.go
package ado

func Register(r tools.Registry, sm domain_security.Manager, client tools.HTTPClient) error {
    // ... current registerAzureDevOps body, unchanged ...
}
```

**`atlassian.RegisterConfluence`** and **`atlassian.RegisterJira`** — move `registerConfluence` and `registerJira` into `integrations/atlassian/register.go` as exported functions:

```go
// internal/tools/integrations/atlassian/register.go
package atlassian

func RegisterConfluence(r tools.Registry, sm domain_security.Manager, client tools.HTTPClient) error {
    // ... current registerConfluence body, unchanged ...
}

func RegisterJira(r tools.Registry, sm domain_security.Manager, client tools.HTTPClient) error {
    // ... current registerJira body, unchanged ...
}
```

### 2. Parent `registration.go` Becomes a Thin Orchestrator

After extraction, `registration.go` retains only `RegisterAll` — a ~30-line orchestrator that calls each sub-package's `Register` in fail-fast sequence:

```go
// internal/tools/integrations/registration.go
package integrations

import (
    "github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
    "github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"
    // ... other imports ...
)

func RegisterAll(r tools.Registry, fs persistence.FileSystem, sm domain_security.Manager, client llm.LLMClient, assetsDir string) error {
    if err := registerMedia(r, fs, sm, client, assetsDir); err != nil {
        return err
    }
    // network, teams — same-package calls (see §3)
    if err := registerNetwork(r, newnetworkTool(sm, nil)); err != nil {
        return err
    }
    if err := registerTeams(r, sm, nil); err != nil {
        return err
    }
    // atlassian sub-package
    if err := atlassian.RegisterConfluence(r, sm, nil); err != nil {
        return err
    }
    if err := atlassian.RegisterJira(r, sm, nil); err != nil {
        return err
    }
    // ado sub-package
    if err := ado.Register(r, sm, nil); err != nil {
        return err
    }
    return nil
}
```

Total `registration.go` size: ≤50 LOC (down from 824). Complexity: 1 (linear call chain).

### 3. Sibling Registrations Stay in Parent Package but Move to Domain Files

For integration domains that do **not** have sub-packages (media, network, teams):

- `registerMedia` moves from `registration.go` → `media.go` (alongside `mediaManager` logic)
- `registerNetwork` moves from `registration.go` → `network.go`
- `registerTeams` moves from `registration.go` → `teams.go`

These remain **unexported** (lowercase) — they are same-package functions called only by `RegisterAll`.

### 4. Export Rules

| Registration Function | Destination | Exported? | Rationale |
|---|---|---|---|
| `registerAzureDevOps` | `ado/register.go` | **Yes** (`ado.Register`) | Cross-package call from `registration.go` |
| `registerConfluence` | `atlassian/register.go` | **Yes** (`atlassian.RegisterConfluence`) | Cross-package call from `registration.go` |
| `registerJira` | `atlassian/register.go` | **Yes** (`atlassian.RegisterJira`) | Cross-package call from `registration.go` |
| `registerMedia` | `media.go` (parent) | **No** (`registerMedia`) | Same-package call; stays unexported |
| `registerNetwork` | `network.go` (parent) | **No** (`registerNetwork`) | Same-package call; stays unexported |
| `registerTeams` | `teams.go` (parent) | **No** (`registerTeams`) | Same-package call; stays unexported |

### 5. Import Direction

The import graph is strictly acyclic (parent → child):

```
integrations
  ├── integrations/ado        (⚠ imports by registration.go)
  └── integrations/atlassian  (⚠ imports by registration.go)
```

No child package imports the parent. No sibling cross-imports. This matches the pattern established by ADR-027 (`session → session/ui`, `session → session/context`).

### 6. Preserved Semantics

- **Call order**: `RegisterAll` preserves the current sequence: media → network → teams → confluence → jira → ado
- **Fail-fast**: If any `Register` returns an error, `RegisterAll` returns immediately (current behavior)
- **Graceful skip**: `RegisterConfluence` and `RegisterJira` retain their existing `return nil` on missing configuration (current behavior)
- **No behavioral changes**: Tool names, parameters, handlers, and toolkit assignments are unchanged

## Consequences

### Positive

- **`registration.go` shrinks from 824 LOC to ≤50 LOC**. No file in the `integrations/` package exceeds 300 LOC.
- **`registerAzureDevOps` complexity drops to ≤10** when extracted — the function body itself is a sequential loop of ~20 tool specs (complexity ~2) plus dispatch logic.
- **Open/Closed Principle restored**: Adding a new integration (e.g., GitHub) requires only: (1) creating `integrations/github/` with its own `Register`, (2) adding one line to `RegisterAll`. Existing registration files are untouched.
- **Merge-conflict surface eliminated**: Changes to ADO tool declarations only touch `integrations/ado/register.go`. Changes to Confluence tools only touch `integrations/atlassian/register.go`. These files are never modified simultaneously by different domains.
- **Independent testability**: `go test ./integrations/ado/...` runs only ADO registration tests. `go test ./integrations/atlassian/...` runs only Atlassian registration tests. No cross-domain test contamination.

### Negative

- **~3 symbols exported** (`ado.Register`, `atlassian.RegisterConfluence`, `atlassian.RegisterJira`). These were previously unexported functions in the parent package. Acceptable: exporting them formalizes an existing cross-package dependency that already existed (the sub-packages' managers and handlers were already exported by ADR-027 for consumption by `registration.go`).
- **One-time mechanical migration**: ~270 lines of ADO registration moved to `ado/register.go`, ~100 lines of Atlassian registration moved to `atlassian/register.go`, ~120 lines of sibling registration relocated within the parent package. Mitigated by `git mv` for new files and simple cut-paste for within-file moves.
- **Slightly deeper call chain**: `RegisterAll → ado.Register` instead of `RegisterAll → registerAzureDevOps`. Acceptable — the extra stack frame is negligible, and the package qualifier (`ado.Register`) provides namespace clarity.

### Neutral

- **No behavioral changes**. This is a purely structural refactor.
- **No public API changes** outside `internal/`. External consumers (CLI, agent) call `integrations.RegisterAll()` — its signature and behavior are unchanged.
- **`verify_architecture` remains clean** — the `integrations → integrations/ado` and `integrations → integrations/atlassian` dependencies are already established by ADR-027.
- **No change to test coverage**. Registration tests remain with their respective sub-packages or domain files.

## Implementation Plan

Executed on GitHub Issue #116 across 4 commits:

| Commit | Task | Description |
|---|---|---|
| T1 | Write ADR-028 | This document |
| T2 | Extract `registerAzureDevOps` → `ado/register.go` | Move ~270 LOC; export as `ado.Register` |
| T3 | Extract `registerConfluence` + `registerJira` → `atlassian/register.go` | Move ~100 LOC; export as `atlassian.RegisterConfluence` / `atlassian.RegisterJira` |
| T4 | Relocate sibling registrations + finalize orchestrator | Move `registerMedia` → `media.go`, `registerNetwork` → `network.go`, `registerTeams` → `teams.go`; update `RegisterAll` to call sub-package `Register` functions |

Acceptance criteria:

| Check | Target |
|---|---|
| `registration.go` LOC | ≤ 50 |
| `go build ./...` | ✅ Full-module build |
| `go test ./internal/tools/integrations/...` | ✅ All integration packages |
| `go vet ./...` | ✅ Clean |
| `golangci-lint run` | ✅ Clean |
| `verify_architecture` | ✅ Acyclic |

## References

- ADR-008 (`2026-01-domain-decomposition-strategy.md`) — package decomposition principles and thresholds
- ADR-027 (`2026-05-integrations-and-session-ui-subpackage-extraction.md`) — established the `integrations/ado/` and `integrations/atlassian/` sub-packages; left registration logic in parent as composition root
- GitHub Issue #116 — execution tracking for this ADR
