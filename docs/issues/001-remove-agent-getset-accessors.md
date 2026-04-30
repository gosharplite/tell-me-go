# Remove `agent` struct `Get*`/`Set*` test-only accessors

## Category
[REFACTOR] — Encapsulation / Test Hygiene

## Context
The `agent` struct in `internal/agent/agent.go` exposes **8 mutator/accessor methods** that exist solely to support tests. This is a textbook violation of "Tell, Don't Ask" and a recurrence of the pattern ADR-004 ("ChatterParams Elimination") was meant to prevent. The struct now has **23 fields and 20 methods** — the same scale of coupling that ChatterParams previously concentrated in a single options bag.

## Evidence (verified via `find_usages` AST analysis)

| Method | Production callers | Test callers |
|---|---|---|
| `SetCtxManager` | 0 | 1 (`agent_error_test.go`) |
| `SetEvents`     | 0 | 5 (1 unit, 1 lifecycle, 3 integration) |
| `SetConfigWatcher` | 0 | 3 |
| `SetTracker`    | 0 | 3 |
| `SetLogger`     | 0 | 3 |
| `SetRuntimeConfig` | 0 | 2 |
| `GetCtxManager`, `GetEvents`, `GetConfigWatcher`, `GetTracker`, `GetRuntimeConfig` | TBD — audit | TBD — audit |

**100% of `Set*` callers are tests.** This proves the methods exist as a test backdoor, not a production API.

## Current vs. Scalable

**Current:**
```go
// In tests:
a, _ := agent.NewAgent(gw, bus, reg, opts...)
a.SetEvents(mockBus)        // backdoor mutation
a.SetTracker(mockTracker)   // backdoor mutation
a.SetLogger(mockLogger)     // backdoor mutation
```

**Scalable:**
```go
// In agenttest package — builders compose options correctly
a := agenttest.NewAgentBuilder(t).
    WithEventBus(mockBus).
    WithTracker(mockTracker).
    WithLogger(mockLogger).
    Build()
```

The `agenttest` package already exists (`internal/agent/agenttest/`). It should grow a builder rather than the production type growing setters.

## Proposed Action

1. **Audit `Get*` methods** with `find_usages` to confirm none are called from production. If any are, file a sub-issue to remove that production caller first.
2. **Add `agenttest.AgentBuilder`** with chainable `With*` methods that call the existing `agent.AgentOption` functions (`WithLogger`, `WithEvents`, etc.).
3. **Migrate all test callers** of `Set*`/`Get*` to the builder.
4. **Delete the 8 `Set*`/`Get*` methods** from `agent.go`.
5. Run `go test ./... -race` and `golangci-lint run` to verify no regressions.

## Acceptance Criteria

- [ ] `find_usages` for each removed method returns zero results.
- [ ] `internal/agent/agent.go` method count drops from 20 to ≤12.
- [ ] All tests pass with `-race`.
- [ ] No new fields added to `agent` struct.
- [ ] `agenttest.AgentBuilder` documented in `agenttest/doc.go`.

## Out of Scope

- Splitting the 23-field struct into composed layers (deferred to a future issue once true coupling shape is visible after this cleanup).
- Modifying `AgentOption` signatures.

## Effort
**Small-Medium** (1–2 days). Mechanical refactor; risk concentrated in 5 integration test files.

## ADR Required
No. This issue executes encapsulation principles already established in ADR-004.

## References
- ADR-004 (`2026-01-chatterparams-elimination.md`) — same anti-pattern, prior round
- ADR-007 (`2026-02-agent-options-extraction.md`) — establishes the `AgentOption` pattern that the builder will compose
