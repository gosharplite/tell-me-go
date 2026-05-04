<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-029: Fallible `Reconfigure` Delegate Chain in Agent Hot-Reload

## Status
Accepted

## Decision Date
2026-05-04

## Context

`(*agent).applyConfig` (`internal/agent/agent.go:158`) is the central hot-reload entrypoint for runtime configuration changes (provider switch, context-window resize, tool toggle, history-limit changes). It is invoked from three call sites:

1. `NewAgent` — initial configuration application during construction.
2. `(*agent).SetLimits` — runtime limit updates from the TUI / API surface.
3. `(*agent).Chat` — pre-flight configuration refresh before each chat turn.

`applyConfig` itself is at **100% test coverage** — the gap raised in issue #137 (0% on the exported wrapper) was the symptom, not the disease. The architectural defect is that two of the three delegates downstream of `applyConfig` have **`void` return signatures** and therefore cannot signal failure to the caller:

```go
// internal/agent/orchestrator/engine.go:120
func (e *Engine) Reconfigure(cfg RuntimeConfig, tracker domain_pricing.CostTracker) {
    // ... no error return ...
}

// internal/agent/session/context/manager.go:81
func (cm *Manager) Reconfigure(limits events.Limits) {
    // ... no error return ...
}
```

The delegate chain inside `applyConfig` looks like this:

```text
applyConfig(ctx)
 ├── ctx.Err()                           → error  (testable)
 ├── configWatcher.Refresh(model)        → void   ⚠ swallowed
 ├── configWatcher.GetLimits()           → values (no error path)
 ├── configWatcher.SyncToStrategy()      → void   ⚠ swallowed
 ├── events.SafePublish(ConfigUpdated)   → error  (testable; ErrBusNotInitialized swallowed by design)
 ├── engine.Reconfigure(...)             → void   ⚠ swallowed  ← ARCHITECTURAL BLOCKER
 └── ctxManager.Reconfigure(...)         → void   ⚠ swallowed  ← ARCHITECTURAL BLOCKER
```

### The Concrete Problem

A misconfigured runtime change to either the orchestrator engine (e.g., invalid provider/model combination, malformed pricing override) or the context manager (e.g., zero/negative limits that violate invariants) currently has **no observable failure surface**. The agent reports success, the configuration silently fails to apply, and configuration drift persists for the entire session lifetime.

The blast radius is global: every provider switch, every `SetLimits` call, every chat turn pre-flight passes through this fallible-but-silent chain.

### Why a `void` Signature Was Acceptable Originally

`Reconfigure` was conceived as a **state-mutation method** during the actor-model migration (ADR-018) — a parallel to `Run`/`Stop` lifecycle calls. At the time, all reconfiguration was assumed to be infallible: the agent had already validated the config upstream, and the delegate's job was simply to absorb the new state atomically. This assumption no longer holds because:

1. **Provider plug-ins** (ADR-001) introduced provider-specific validation that can only happen at the engine level (e.g., model availability for a given provider).
2. **Pricing overrides** (`ModelPricing`) introduced per-model invariants (non-negative rates, currency consistency) that must be re-validated on every reconfigure.
3. **Bounded contexts** (ADR-006) introduced limit invariants on the context manager (e.g., `MaxHistoryTokens >= MaxHistoryTurns × MinTokensPerTurn`).

Each of these is a real failure mode that today is silently swallowed.

### Why Now

| Trigger | Impact |
|---|---|
| Issue #137 surfaced the `ApplyConfig` 0% coverage symptom | Investigation revealed the underlying `void`-signature defect |
| ADR-022 / `agentinternal` bridge | Tests can now reach the delegate boundary; the test scaffolding to verify error propagation already exists |
| The "delegate returns error → propagated" scenario (proposed in #137) | Structurally untestable today — this ADR unblocks it |

### Scope Boundaries

This ADR covers **only** the signature change of `Engine.Reconfigure` and `Manager.Reconfigure` (and the corresponding error-propagation logic in `(*agent).applyConfig`). It does **not**:

- Introduce new validation rules — those are tracked separately and added incrementally per defect.
- Change the semantics of `configWatcher.Refresh` / `SyncToStrategy` — these remain `void` (rationale below).
- Modify the public `ports.Chatter` interface or the exported `(*agent).ApplyConfig` wrapper signature.

## Decision

### 1. `Engine.Reconfigure` Returns `error`

```go
// internal/agent/orchestrator/engine.go
func (e *Engine) Reconfigure(cfg RuntimeConfig, tracker domain_pricing.CostTracker) error {
    if err := cfg.Validate(); err != nil {
        return fmt.Errorf("engine reconfigure: invalid runtime config: %w", err)
    }
    // ... existing atomic state swap ...
    return nil
}
```

A `RuntimeConfig.Validate()` method is added and called as the first step. Initial validation rules are minimal (non-empty `ProviderName` when overriding; non-negative pricing rates) and grow per defect.

### 2. `Manager.Reconfigure` Returns `error`

```go
// internal/agent/session/context/manager.go
func (cm *Manager) Reconfigure(limits events.Limits) error {
    if err := limits.Validate(); err != nil {
        return fmt.Errorf("ctx manager reconfigure: invalid limits: %w", err)
    }
    // ... existing pipeline mutation ...
    return nil
}
```

`events.Limits.Validate()` enforces: all limits ≥ 0; if `MaxHistoryTokens > 0` and `MaxHistoryTurns > 0`, the ratio must be sane (concrete bound deferred to implementation, but no zero-division traps).

### 3. `applyConfig` Adopts Fail-Fast Semantics with Explicit Order

```go
func (a *agent) applyConfig(ctx context.Context) error {
    if err := ctx.Err(); err != nil {
        return err
    }

    // ... existing watcher refresh + limits read + atomic store ...

    if err := events.SafePublish(ctx, a.events, events.ConfigUpdated{Limits: newCfg.Limits}); err != nil {
        if !errors.Is(err, events.ErrBusNotInitialized) {
            a.getLogger().Error("event_publish_failed", "event_type", "ConfigUpdated", "error", err)
            return err
        }
    }

    if a.engine != nil {
        if err := a.engine.Reconfigure(orchestrator.RuntimeConfig{...}, tracker); err != nil {
            a.getLogger().Error("engine_reconfigure_failed", "error", err)
            return fmt.Errorf("engine reconfigure: %w", err)
        }
    }

    if a.ctxManager != nil {
        if err := a.ctxManager.Reconfigure(newCfg.Limits); err != nil {
            a.getLogger().Error("ctx_manager_reconfigure_failed", "error", err)
            return fmt.Errorf("ctx manager reconfigure: %w", err)
        }
    }
    return nil
}
```

Order is **fixed and documented**: `events.SafePublish` → `engine.Reconfigure` → `ctxManager.Reconfigure`. Rationale: subscribers should see the intent (`ConfigUpdated`) before any state mutates; engine state is owned by the orchestrator (most expensive to roll back); context manager is the leaf consumer (cheapest to leave inconsistent if a downstream stage failed).

### 4. Failure Semantics: Fail-Fast, No Rollback

When any delegate returns an error, `applyConfig` stops immediately and returns the wrapped error. **No rollback** is attempted because:

- The atomic `a.config.Store(&newCfg)` happens **before** the delegate calls — so the canonical "intended" config is already published.
- The first delegate (`SafePublish`) has already broadcast `ConfigUpdated` to subscribers; partial rollback would require a compensating `ConfigReverted` event that does not exist today.
- Reconfigure operations are **idempotent**: callers that observe an error can call `applyConfig` again with a corrected config to converge. This matches the actor-model contract (ADR-018) and avoids the complexity of two-phase commit.

The trade-off is that a partial reconfigure leaves the system in a transient inconsistent state until the next successful `applyConfig`. This is acceptable because:

| Stage failed | Visible inconsistency | Recovery |
|---|---|---|
| `SafePublish` | UI/telemetry subscribers miss the event; engine + ctx unchanged | Caller retries; next event publish reconciles |
| `engine.Reconfigure` | Engine on old config; ctx manager not yet touched | Caller retries; engine re-validates |
| `ctxManager.Reconfigure` | Engine on new config; ctx manager on old limits | Caller retries; ctx manager re-validates |

In all three cases, the **persistent runtime config** (`a.config`) reflects the intended new state, so subsequent reads (e.g., metrics, debug dumps) show the correct target. Operators can detect drift by diffing `a.config.Load()` against the live engine/ctx state if needed (future ADR if drift detection becomes necessary).

### 5. `configWatcher.Refresh` and `SyncToStrategy` Stay `void`

These are retained as `void` because:

- `Refresh` is a **best-effort cache reload** — failing to re-read a config file should fall back to the last-known-good in-memory values, which is the existing behavior. Surfacing this as an error would force callers to choose between "fail the whole reconfigure" (worse than current) or "log and ignore" (identical to current).
- `SyncToStrategy` is a **pure in-memory copy** with no failure modes.

This is documented in the godoc of each function and called out as an explicit non-decision.

### 6. Public API Stability

| Surface | Change |
|---|---|
| `ports.Chatter.SetLimits` | **Unchanged** (already returns `error`) |
| `(*agent).ApplyConfig` (exported wrapper, line 348) | **Unchanged** (already returns `error`) |
| `agentinternal.AgentInternal.ApplyConfig` | **Unchanged** (already returns `error`) |
| `Engine.Reconfigure` | **Breaking** for any direct caller in `internal/` |
| `Manager.Reconfigure` | **Breaking** for any direct caller in `internal/` |

The breaking changes are confined to `internal/` packages. No external consumer (CLI, integrations, public API) is affected.

### 7. Caller Updates

Direct callers of `Engine.Reconfigure` and `Manager.Reconfigure` outside of `applyConfig` must check the returned error. A pre-implementation grep is required to enumerate them; current expectation based on inspection is that the only production caller is `applyConfig` itself, with secondary callers in `*_test.go` files (acceptable to ignore the error in tests where the input is known-valid, with a `_ =` assignment and a comment).

## Consequences

### Positive

- **The "delegate returns error → propagated" test scenario from issue #137 becomes feasible.** A mock orchestrator/manager can return a synthetic error and the caller can assert it propagates through `applyConfig` and out through `ApplyConfig`/`SetLimits`.
- **Silent misconfiguration is eliminated.** Invalid pricing, missing provider, or malformed limits surface as a returned error at the call site (CLI prompt, TUI status bar, API response) instead of vanishing.
- **Operator observability improves.** Failures emit structured logs (`engine_reconfigure_failed`, `ctx_manager_reconfigure_failed`) tagged with the failing stage, which simplifies post-mortem analysis.
- **Future validation rules have a home.** Adding a new invariant to `RuntimeConfig` or `events.Limits` is a localized change in `Validate()` — no new plumbing required.

### Negative

- **Breaking signature change inside `internal/`.** Every direct caller of `Engine.Reconfigure` / `Manager.Reconfigure` must be updated. Mitigated by the fact that direct callers are few (primarily `applyConfig` and tests) and the compiler enforces the migration.
- **No rollback on partial failure.** Operators must understand that a failed `applyConfig` may leave engine and ctx manager temporarily desynchronized. Mitigated by the idempotent retry contract and the fact that the canonical `a.config` always reflects the intended state.
- **Validation logic must be designed conservatively.** Over-strict validation could reject configs that worked before this ADR — an unintended regression. Mitigated by starting with the minimum viable rule set (non-empty/non-negative checks) and adding stricter rules only in response to real defects, each with its own tracking issue.

### Neutral

- **No change to the actor-model contract** established in ADR-018. `Reconfigure` remains a single-shot state-mutation method; it just now reports whether the mutation succeeded.
- **No change to event semantics.** `ConfigUpdated` is still published before the delegate mutations — subscribers see the intent regardless of downstream success. A future ADR may introduce `ConfigApplyFailed` if observability requirements grow.
- **No coverage target change.** `applyConfig` is already at 100%; the new error branches will be exercised by table-driven tests that grow alongside the new `Validate()` rules.

## Implementation Plan

To be executed under the follow-up GitHub issue (Issue B in the #137 review thread):

| Task | Description |
|---|---|
| T1 | Land this ADR (status: Proposed → Accepted on review) |
| T2 | Add `RuntimeConfig.Validate()` and `events.Limits.Validate()` with minimal initial rules |
| T3 | Change `Engine.Reconfigure` signature to return `error`; update internal callers |
| T4 | Change `Manager.Reconfigure` signature to return `error`; update internal callers |
| T5 | Update `(*agent).applyConfig` to check and propagate both errors with wrapped context |
| T6 | Add table-driven tests covering: valid config, engine validation failure, ctx manager validation failure, ordered fail-fast (engine error → ctx manager not called) |
| T7 | Update godoc on `Refresh` / `SyncToStrategy` to document why they remain `void` |

Acceptance criteria:

| Check | Target |
|---|---|
| `go build ./...` | ✅ Full-module build |
| `go test ./internal/agent/...` | ✅ All tests pass |
| `go vet ./...` | ✅ Clean |
| `golangci-lint run` | ✅ Clean |
| Coverage of new error branches in `applyConfig` | ≥ 90% |
| `verify_architecture` | ✅ No new cycles |

## References

- ADR-001 — Hybrid LLM Infrastructure Strategy (provider plug-in model that introduced provider-specific reconfigure validation)
- ADR-006 — History Log Compaction and Bounded Contexts (origin of the limit invariants on `events.Limits`)
- ADR-018 — Migrate Orchestrator to Event-Driven Actor Model (established `Reconfigure` as a state-mutation lifecycle method)
- ADR-022 — Test-Only Access via `agentinternal` Bridge (provides the test-side scaffolding required to verify the new error propagation)
- GitHub Issue #137 — original architectural review that surfaced the defect
- GitHub Issue #140 — Issue A: trivial coverage of the exported `ApplyConfig` wrapper
