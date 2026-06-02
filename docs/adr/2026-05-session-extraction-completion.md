# ADR-040: Complete Session Subpackage Extraction (config_watcher, skill_injector)

**Date**: 2026-05-08
**Status**: Accepted
**Deciders**: Architecture review (Issue #260)
**References**: ADR-025, ADR-026, ADR-029

## Context

During Phase 6 (Domain Decomposition, ADR-026), core context-preparation
components were extracted from `internal/agent/session/` into
`internal/agent/session/context/`. Two components were explicitly deferred:

| Component | Reason for deferral |
|---|---|
| `config_watcher.go` | File-watching logic interfaces with both domain config and session context strategy |
| `skill_injector.go` | Depends on domain/skills interfaces; was deemed lower priority than the context pipeline |

With the context sub-package now stable, these deferred components create an
unnecessary coupling between core session lifecycle and cross-cutting concerns.

## Decision

### skill_injector.go → `internal/agent/skills/`

The skill injector implements `ports.ContextTransformer` — it is an agent-layer
pipeline component. It has zero dependencies on `session/` internals. Its
dependencies are purely domain-level (`domain/llm`, `domain/ports`, `domain/skills`).

**Target**: `internal/agent/skills/` — a new package co-located with the agent
layer, semantically grouped with the agent's context transformation pipeline.

### config_watcher.go → interface in `domain/config/`, implementations in `infrastructure/config/`

The `ConfigWatcher` interface defined a `SyncToStrategy(*sessctx.Strategy)` method
that coupled the abstraction to `internal/agent/session/context/`. This prevented
extraction to a lower layer.

**Solution**:
1. **Interface** (`domain/config/watcher.go`): Replaced `SyncToStrategy` with
   `GetContextWindow() int`. The interface now exposes only data accessors —
   the sync logic belongs to the caller (agent layer), which already holds
   both the watcher and the strategy.
2. **Implementations** (`infrastructure/config/watcher.go`): `FileConfigWatcher`
   and `noOpConfigWatcher` moved to infrastructure. Both implement the domain
   interface with zero agent-layer dependencies.

### SyncToStrategy inlining

The sync logic moved into `agent.go`'s `prepareRuntimeConfig()`:

```go
if a.strategy != nil {
    tokens, toolTurns, histTurns := a.configWatcher.GetLimits()
    a.strategy.SetLimits(tokens, toolTurns, histTurns)
    a.strategy.SetContextWindow(a.configWatcher.GetContextWindow())
}
```

Per ADR-029, `prepareRuntimeConfig` is the sole pre-reconfigure entry point;
inlining the sync here is consistent with its role as the configuration
assembly point.

## Consequences

### Positive
- `internal/agent/session/` reduced by 2 files + 2 test files (4 total)
- `ConfigWatcher` interface is now a proper domain port with no agent-layer
  coupling
- `skill_injector` is independently testable in its own package
- Infrastructure config implementations have zero imports from `internal/agent/`

### Negative
- `GetLimits()` is called twice in `prepareRuntimeConfig()` (once for the
  `runtimeConfig` struct, once for the strategy sync). This is acceptable:
  both calls hit a `sync.RWMutex`-protected cache and are effectively free.

### Neutral
- Three new packages created: `internal/agent/skills/`,
  `internal/domain/config/watcher.go` (single file),
  `internal/infrastructure/config/watcher.go` (single file)

## Package Dependency Graph (post-extraction)

```
domain/config/watcher.go          ← ConfigWatcher interface (no agent deps)
         ↑
infrastructure/config/watcher.go  ← FileConfigWatcher, noOpConfigWatcher
         ↑
agent/agent.go                     ← consumer, owns sync logic
agent/skills/injector.go           ← ContextTransformer impl
```

## Migration Notes

- Test files moved with their implementations — no test logic changed
- `agentinternal/accessor.go` updated to use `domain_config.ConfigWatcher`
- All integration tests updated to use new constructor paths
- `session/context/doc.go` updated to reflect new locations
