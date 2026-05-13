# ADR-032: Test-Hook Policy for Unexported Concrete Types

## Status
Accepted

## Context

PR [#245](https://github.com/gosharplite/tell-me-go/pull/245) surfaced a disagreement about
`beforeBlockingSendHook` — a nil-in-production `func()` field on the unexported `eventQueue` struct,
used for deterministic test synchronization. The reviewer flagged it as a Clean Architecture
violation; the architect assessed it as a pragmatic Go pattern. The debate (fully documented in
[PR #245 comments](https://github.com/gosharplite/tell-me-go/pull/245#issuecomment-4402652177))
revealed there is **no documented policy** on when test hooks embedded in production structs are
acceptable vs. when interface mocking should be used.

This creates:

1. **Inconsistent reviews** — the same pattern may be approved or rejected based on reviewer.
2. **Precedent risk** — without clear criteria, hooks proliferate unchecked, creating God Objects
   that accumulate testing artifacts.
3. **ADR-011 ambiguity** — ADR-011 §3 Rule 2 mandates "Positive Synchronization via Mock
   Callbacks" but does not address unexported concrete types that have no interface boundary
   to mock.

## Decision

### Decision Criteria

A nil-in-production `func()` test hook is **acceptable** when **all** of the following are true:

| # | Criterion | Rationale |
|---|-----------|-----------|
| 1 | **Unexported concrete type** | The type is not part of any package's public API. No interface boundary exists that could be mocked. |
| 2 | **No interface to mock** | The synchronization point is on an unexported method of a concrete type. `testify/mock` is impossible because there is no interface to implement. |
| 3 | **`time.Sleep` is the only alternative** | Without the hook, the test must use arbitrary sleeps — violating ADR-011 Rule 1 and introducing flakiness. |
| 4 | **Nil `func()` — zero overhead** | The hook field is a single function pointer. The production path executes one nil-check branch. Zero allocations. |
| 5 | **Single synchronization point** | The hook observes one well-defined state transition, not broad behavioral verification. Broad verification belongs with interface mocks. |
| 6 | **Documented** | The field carries a `// Test hook: nil in production` comment and a reference to this ADR. |
| 7 | **Must not change production behavior** | The hook produces zero observable side effects in production when nil. The only permitted exception is fault-injection for exercising `recover()` blocks, where a `panic()` is the only viable mechanism to trigger the defer/recover path. Hooks that inject faults must be clearly suffixed `Panic` and reference this ADR. |

A hook that violates any criterion **must** use an alternative mechanism:

- **Exported type or interface exists** → Interface mock (ADR-011 Rule 2).
- **Broad behavioral verification** → Interface mock.
- **Multiple hooks accumulating on one struct** → Re-evaluate the struct's responsibilities; consider decomposition.
- **Hook has side effects beyond observation** → Forbidden, with one exception: fault-injection hooks targeting `recover()` blocks are permitted when no other mechanism can trigger the recovery path. Such hooks must be suffixed `Panic` and documented in the registry.

### Rationale

The decisive factor is the **interface boundary limitation**. ADR-011 Rule 2 requires
`testify/mock`'s `.Run()` callbacks, but `.Run()` requires an interface to mock. When the target
is an unexported concrete type with no interface, the mock pathway is simply unavailable.

The Go standard library uses the same pattern sparingly: `net/http` maintains unexported test
hooks (e.g., `testHookServerServe`) for internal synchronization points that are unreachable
through the public API. This ADR applies the same discipline: permitted, documented, and tracked.

### Registry Requirement

Every authorized test hook **must** be registered in Appendix A of this ADR. Adding a hook
requires amending this ADR — making proliferation visible and forcing a conscious architectural
decision each time.

### Fault-Injection Exception

ADR-032 §Decision Criteria Rule 4 requires hooks to be "observation-only" — they must not alter
program flow or inject side effects. However, Go's `recover()` mechanism can only be exercised by
an actual `panic()`. There is no interface-based alternative: a mock that returns an error does
not trigger `recover()`, and refactoring the `defer/recover` block into an interface defeats the
purpose of testing the recovery logic itself.

A narrow exception is therefore granted: **fault-injection hooks that call `panic()` are permitted
when and only when:**

1. The hook targets a specific `defer/recover()` block that cannot be tested by any other means.
2. The hook field name is suffixed with `Panic` (e.g., `testEmitHeartbeatsPanic`).
3. The hook is nil in production and set only in dedicated panic-recovery tests.
4. The test validates both that the panic was caught (no goroutine leak) and that the recovery
   logic executed (e.g., the error was logged).

This exception does not extend to hooks that inject other side effects (e.g., mutating shared
state, modifying function arguments, altering control flow beyond `panic()`). Fault-injection
hooks remain subject to all other ADR-032 criteria, including the registry requirement.

## Consequences

- **Positive**: Eliminates inconsistent review outcomes for test hooks.
- **Positive**: Contains God Object risk through mandatory registry and strict criteria.
- **Positive**: Preserves ADR-011's zero-`time.Sleep` guarantee for all synchronization scenarios,
  including those unreachable through interface mocking.
- **Negative**: Slight process overhead — each new hook requires an ADR amendment.
  Accepted because new hooks should be rare.

## Appendix A — Authorized Test Hook Registry

| # | Struct | Field | File | Purpose | Since |
|---|--------|-------|------|---------|-------|
| 1 | `eventQueue` | `beforeBlockingSendHook` | `internal/agent/session/ui/event_queue.go:31` | Observe goroutine entry into blocking `select` in `enqueueCritical` | 2026-05 |
| 2 | `InternalTools` | `testEmitHeartbeatsPanic` | `internal/agent/session/internal_tools.go:22` | Fault-injection: inject a panic on the next ticker firing to verify graceful recovery in `emitHeartbeats` | 2026-05 |
| 3 | `InternalTools` | `testEmitHeartbeatsTickHook` | `internal/agent/session/internal_tools.go:23` | Observe ticker firings for deterministic coordination in heartbeat channel-state tests | 2026-05 |
| 4 | `indexer` | `testComputeImplementationsHook` | `internal/tools/analysis/index.go:72` | Count `computeImplementations` invocations for singleflight coalescing verification; nil in production | 2026-05 |
