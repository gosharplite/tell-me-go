<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-043: Plugin/Extension System for Tool Discovery via Auto-Registration

## Status
Accepted

## Context

ADR-028 reduced `registration.go` from 824 lines to ~50 lines by delegating registration logic to integration sub-packages. Each sub-package (`ado/`, `atlassian/`) owns its `Register` function, and same-package registrations (media, network, teams) moved to their domain files. However, `RegisterAll` in `registration.go` still required manual wiring: every new integration needed both an import statement and a hardcoded call added to the orchestrator. The module was still open for modification.

### The Remaining Open/Closed Violation

After ADR-028, adding a new integration (e.g., GitHub, Kubernetes) required touching two places in `registration.go`:

```go
// Step 1: Add import
import "github.com/gosharplite/tell-me-go/internal/tools/integrations/github"

// Step 2: Add call to RegisterAll
func RegisterAll(...) error {
    // ... existing registrations ...
    if err := github.Register(r, sm, nil); err != nil {
        return err
    }
    return nil
}
```

This is the classic Open/Closed Principle violation: the `integrations` package was open for modification, not closed against it. Each new integration forced a change to the orchestrator's source code, creating a merge-conflict hotspot and increasing the cognitive load of `RegisterAll`.

### Goal

Adding a new integration should require **zero changes** to `registration.go`. The orchestrator should discover all available plugins automatically, with no hardcoded call sequence. The module should be closed for modification but open for extension — new integrations extend the system simply by existing in their sub-package.

### Technical Constraints

- **Circular imports**: `integrations` imports `ado` and `atlassian` (for their blank-import side effects). If `ado` or `atlassian` imported `integrations` to access `RegisterAll` or shared types, a cycle would form: `integrations → ado → integrations`.
- **Registration order**: The existing call sequence (media → network → teams → confluence → jira → ado) must be preserved. While tool registration is order-independent, deterministic ordering aids debugging and test reproducibility.
- **Test isolation**: The global plugin registry must not leak state between test runs. A cleanup mechanism is required.

## Decision

### 1. Plugin Interface

Define a `plugin.Plugin` interface in a new `internal/tools/integrations/plugin/` sub-package. The interface has two methods:

- `Name() string` — returns a unique identifier for the plugin, used for duplicate detection
- `Register(r tools.Registry, deps plugin.PluginDependencies) error` — wires the plugin's tools into the tool registry using the provided dependencies

```go
// internal/tools/integrations/plugin/plugin.go
package plugin

type Plugin interface {
    Name() string
    Register(r tools.Registry, deps PluginDependencies) error
}
```

Each integration implements this interface with a private struct. For example, the ADO plugin:

```go
// internal/tools/integrations/ado/plugin.go
package ado

import "github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"

type adoPlugin struct{}

func NewPlugin() plugin.Plugin { return &adoPlugin{} }

func (adoPlugin) Name() string { return "azure-devops" }

func (adoPlugin) Register(r tools.Registry, deps plugin.PluginDependencies) error {
    return Register(r, deps.SecurityMgr, deps.HTTPClient)
}
```

The existing `ado.Register`, `atlassian.RegisterConfluence`, and `atlassian.RegisterJira` functions remain exported and unchanged — the `Plugin` interface wraps them, adapting their signatures to the uniform `PluginDependencies` contract.

### 2. PluginDependencies Struct

`PluginDependencies` carries all shared dependencies that plugins may need during registration. `HTTPClient` is separated from `LLMClient` because 4 out of 5 plugins (network, teams, confluence/jira, ado) need HTTP capabilities but none of them call LLM APIs:

```go
type PluginDependencies struct {
    FileSystem  persistence.FileSystem
    SecurityMgr domain_security.Manager
    LLMClient   llm.LLMClient
    HTTPClient  tools.HTTPClient
    AssetsDir   string
}
```

All fields are optional at the type level — plugins handle nil values gracefully by skipping optional functionality or substituting safe defaults. The zero value of `PluginDependencies` is usable (all fields nil/empty).

### 3. Global Plugin Registry

Three functions manage a thread-safe, copy-on-read global registry:

```go
var (
    mu      sync.Mutex
    plugins []Plugin
)

func Register(p Plugin) error   // adds plugin; rejects nil and duplicates
func All() []Plugin              // returns a defensive copy
func Reset()                     // clears all plugins (test-only)
```

- **`Register`**: Thread-safe via `sync.Mutex`. Rejects nil plugins and duplicate names.
- **`All`**: Returns a copy of the internal slice so callers cannot mutate the registry. Safe for concurrent use with `Register`.
- **`Reset`**: Clears the registry. Intended for test cleanup only (`t.Cleanup(plugin.Reset)`).

### 4. Auto-Registration via `init()`

Each integration sub-package calls `plugin.Register(NewPlugin())` in its `init()` function. This is the mechanism that makes `RegisterAll` a pure loop — the orchestrator no longer needs to know which plugins exist:

**Same-package plugins** (media, network, teams) register in filename lexical order (`media.go` → `network.go` → `teams.go`), determined by Go's `init()` execution within a package:

```go
// internal/tools/integrations/media.go
func init() {
    plugin.Register(&mediaPlugin{})
}
```

**Sub-package plugins** (ado, atlassian) register via import order of their blank imports in `registration.go`:

```go
// internal/tools/integrations/registration.go
import (
    _ "github.com/gosharplite/tell-me-go/internal/tools/integrations/ado"
    _ "github.com/gosharplite/tell-me-go/internal/tools/integrations/atlassian"
)
```

The combined registration order is: azure-devops → atlassian → media → network → teams. This is because Go initializes imported packages before the importing package: `registration.go` blank-imports `ado` and `atlassian`, so their `init()` runs first. Then same-package `init()` functions run in filename lexical order: `media.go` → `network.go` → `teams.go`. The atlassian plugin internally registers Confluence first, then Jira.

### 5. RegisterAll Becomes a Loop

`RegisterAll` iterates over `plugin.All()` instead of maintaining a hardcoded call sequence:

```go
func RegisterAll(r tools.Registry, fs persistence.FileSystem, sm domain_security.Manager, client llm.LLMClient, assetsDir string) error {
    deps := plugin.PluginDependencies{
        FileSystem:  fs,
        SecurityMgr: sm,
        LLMClient:   client,
        AssetsDir:   assetsDir,
    }

    for _, p := range plugin.All() {
        if err := p.Register(r, deps); err != nil {
            return fmt.Errorf("%s: %w", p.Name(), err)
        }
    }

    return nil
}
```

Adding a new integration now requires only:
1. Creating a sub-package with a `plugin.Plugin` implementation
2. Adding one blank import to `registration.go`

The `RegisterAll` function body never changes. `registration.go` shrinks further from ~50 LOC to ~30 LOC (essentially just the loop and its imports).

### 6. Circular Import Avoidance

The `plugin/` sub-package is the critical architectural element that breaks what would otherwise be a `integrations ↔ ado` cycle:

```
integrations ──blank import──→ integrations/ado ──import──→ integrations/plugin ──import──→ domain packages
    │                              │                                 │
    └────────import───────────────┘                                 └── (no integrations import)
```

Both `integrations` and `ado`/`atlassian` import `plugin`. Nobody imports `integrations` from a sub-package. The import graph remains acyclic:

```
integrations → ado         (blank import, init() side effect)
integrations → atlassian   (blank import, init() side effect)
integrations → plugin      (for Plugin, PluginDependencies, All)
ado → plugin               (for Plugin, PluginDependencies, Register)
atlassian → plugin         (for Plugin, PluginDependencies, Register)
plugin → domain packages   (for tools.Registry, llm.LLMClient, etc.)
```

## Consequences

### Positive

| Consequence | Detail |
|---|---|
| **Open/Closed Principle restored** | New integration = new sub-package + one blank import. `RegisterAll` never changes. |
| **`registration.go` shrinks further** | From ~50 LOC post-ADR-028 to ~30 LOC (a pure loop). Complexity: 1. |
| **Plugin interface enables future dynamic loading** | The `plugin.Plugin` contract is a natural extension point for YAML-based or plugin-directory-based discovery, without architectural changes. |
| **`verify_architecture` remains acyclic** | `plugin/` acts as a shared leaf dependency. Neither `ado` nor `atlassian` imports `integrations`. |
| **Test isolation via `Reset()`** | Each test can call `t.Cleanup(plugin.Reset)` to start with a clean registry. The copy-on-read `All()` prevents accidental mutation of internal state. |
| **Duplicate detection** | `Register` rejects duplicate plugin names, catching misconfigured init() ordering or accidental double-imports at startup. |

### Negative

| Consequence | Detail |
|---|---|
| **`init()` ordering is implicit** | Registration order depends on Go's `init()` semantics (imports first, then same-package in filename lexical order). This is documented as a constraint but not enforced by the compiler. A developer moving `media.go` → `zmedia.go` would silently change registration order. Mitigation: the registration order is not semantically significant — tools are idempotent and independent — but deterministic ordering aids debugging. |
| **Global plugin registry is test-hostile without `Reset()`** | Shared mutable global state violates test isolation. Mitigated by `Reset()` and `t.Cleanup` conventions. The `PluginDependencies` zero value is usable so tests can construct minimal plugins without real infrastructure. |
| **~5 new exported symbols** | `plugin.Plugin`, `plugin.PluginDependencies`, `plugin.Register`, `plugin.All`, `plugin.Reset` are new public API surface. Acceptable: they form a cohesive, minimal interface for a well-defined extension point. |

### Neutral

| Consequence | Detail |
|---|---|
| **No behavioral changes to any tool** | Tool names, parameters, handlers, toolkit assignments, and error semantics are unchanged. This is a purely structural refactor. |
| **`RegisterAll` signature unchanged** | External consumers (CLI, agent) call `integrations.RegisterAll(r, fs, sm, client, assetsDir)` — its signature and error semantics are identical to post-ADR-028. |
| **Existing exported functions preserved** | `ado.Register`, `atlassian.RegisterConfluence`, `atlassian.RegisterJira` remain exported with unchanged signatures. The `Plugin` interface wraps them; it does not replace them. Direct callers of these functions are unaffected. |
| **No change to test coverage** | Registration tests remain with their respective sub-packages. Plugin registry tests live in `plugin/plugin_test.go`. |

## Implementation Plan

Executed on GitHub Issue #1135 across 4 commits:

| Commit | Task | Description |
|---|---|---|
| T1 | Write ADR-043 | This document |
| T2 | Create `plugin/` package with interface, registry, and tests | `plugin.go` (~70 LOC) + `plugin_test.go` (~200 LOC) |
| T3 | Implement `Plugin` in each integration | `ado/plugin.go`, `atlassian/plugin.go`, `media.go` (mediaPlugin), `network.go` (networkPlugin), `teams.go` (teamsPlugin) |
| T4 | Simplify `RegisterAll` to a loop | Replace hardcoded call sequence with `for _, p := range plugin.All()`; ~30 LOC final |

Acceptance criteria:

| Check | Target |
|---|---|
| `registration.go` LOC | ≤ 30 |
| `go build ./...` | ✅ Full-module build |
| `go test ./internal/tools/integrations/...` | ✅ All integration packages |
| `go vet ./...` | ✅ Clean |
| `golangci-lint run ./internal/tools/integrations/...` | ✅ Clean |
| `verify_architecture` | ✅ Acyclic |

## References

- ADR-028: "Delegate Registration Logic to Integration Sub-Packages" — established the pattern of sub-packages owning their registration logic; reduced `registration.go` from 824 to ~50 LOC
- ADR-027: "Integrations and Session UI Sub-Package Extraction" — established the `integrations/ado/` and `integrations/atlassian/` sub-packages
- GitHub Issue #1135 — execution tracking for this ADR
