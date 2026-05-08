<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-031: Caller Cancellation Priority in Dual-Context Enqueue

## Status
Accepted

## Decision Date
2026-05-06

## Context

The UI Bridge actor model (ADR-011) introduced a dual-context architecture in `eventQueue`: every enqueue operation carries both the caller's context and the actor's lifecycle context (`loopCtx`). Two methods implement backpressure-aware event delivery:

- `enqueueCritical` — blocks until the event is sent, the caller cancels, or the actor dies.
- `enqueueNonCritical` — attempts a non-blocking send; sheds the event if the queue is full.

Both methods must decide, before attempting a send, which of the two possible cancellation/termination signals to check first. Without a documented priority, the order of these checks was ambiguous across implementations and subject to drift under refactoring or new contributors. If the actor death check were inadvertently placed before the caller cancellation check, a cancelled caller whose context was already done could be blocked until the actor context was also cancelled — introducing unnecessary latency and violating the caller's explicit intent.

## Decision

Adopt a **strict two-tier priority** enforced by an identical pre-guard pattern in both `enqueueCritical` and `enqueueNonCritical`:

```go
// 1. Caller cancellation (highest priority)
if err := ctx.Err(); err != nil {
    return err
}
// 2. Actor death
if err := eq.loopCtx.Err(); err != nil {
    return eq.wrapActorDead(err)
}
// 3. Only then attempt the send
```

### Priority Hierarchy

| Tier | Signal | Priority | Rationale |
|------|--------|----------|-----------|
| 1 | Caller cancellation (`ctx.Err()`) | **Highest** | Caller cancellation is explicit caller intent — they are done and want an immediate answer. Honoring it first prevents wasted work and delivers the fastest error path. |
| 2 | Actor death (`loopCtx.Err()`) | Second | Terminal liveness check — prevents the caller from blocking forever on a dead consumer. Wrapped with `ErrActorDead` for unambiguous `errors.Is` classification. |
| 3 | Send attempt (`ch <- e` / `select`) | Last | Only proceeds if both contexts are alive. |

### Implementation Locations

Both methods in `internal/agent/session/ui/event_queue.go` implement this pattern identically:

| Method | Line | Comment |
|--------|------|---------|
| `enqueueCritical` | 74 | `// Strict priority: caller cancellation (ADR-031)` |
| `enqueueNonCritical` | 95 | `// Strict priority: caller cancellation (ADR-031)` |

### Error Semantics

The priority ordering guarantees predictable error semantics:

- **Caller cancels**: Always returns `context.Canceled` or `context.DeadlineExceeded`, regardless of actor state. The caller's intent is always honored first.
- **Actor dies before caller calls**: Returns `ErrActorDead` (wrapping `context.Canceled` from the loop context). The caller never reaches the first check because the actor was already dead.
- **Actor dies during blocking send** (`enqueueCritical` only): Returns `ErrActorDead` from the `select` branch. The caller was alive at the pre-guard but the actor died during the send window.
- **Both alive**: Event is successfully delivered (or shed for non-critical when the queue is full).

## Consequences

### Positive

- **Deterministic error semantics.** Callers always receive `context.Canceled`/`context.DeadlineExceeded` when they cancel, never `ErrActorDead` (unless the actor was already dead before the call).
- **Fastest error path.** A cancelled caller exits immediately — no wasted work, no unnecessary blocking on a dead consumer check that cannot help them.
- **Identical pattern across both methods.** The pre-guard is byte-identical between `enqueueCritical` and `enqueueNonCritical`, eliminating cognitive overhead and making the contract trivially verifiable.
- **No ambiguity for future contributors.** The comment `// Strict priority: caller cancellation (ADR-031)` in both methods points to this document, so the rationale is always discoverable.

### Negative

- **Two context-check branches before every send.** The overhead is two nil-checked interface method calls (`.Err()` on context) per enqueue — negligible at the UI Bridge's event throughput.
- **Non-obvious to new readers.** The dual-context priority is subtle; without this ADR, a contributor might reorder the checks during a refactor and accidentally invert the priority. Mitigated by the in-code comments referencing this ADR.

### Neutral

- **No API surface change.** The method signatures, error types, and return semantics are unchanged from their introduction in ADR-011. This ADR documents the existing implementation's invariant; it does not change behavior.
- **No runtime impact.** The pre-guard order was already correct in the code; this ADR makes the contract explicit and durable against future change.

## References

- ADR-011 — Refactor UI Bridge to Actor Model for Thread-Safe Asynchronous Rendering (introduced the dual-context `eventQueue` architecture)
- ADR-032 — Test-Hook Policy for Unexported Concrete Types (documents `beforeBlockingSendHook`, the test hook that gates on the pre-guard in `enqueueCritical`)
- `internal/agent/session/ui/event_queue.go:74` — `enqueueCritical` pre-guard implementation
- `internal/agent/session/ui/event_queue.go:95` — `enqueueNonCritical` pre-guard implementation
