## Task 4 Findings: Integration Test Surface Analysis

### HandleEvent Calls in Integration Tests

Only `spinner_integration_test.go` references the bridge or `HandleEvent` directly:

| # | Test | File:Line | Event | Context | Cancellable? | Error Checked? | Safe? |
|---|------|-----------|-------|---------|-------------|----------------|-------|
| 1 | `TestSpinner_ContextTimeout_Resilience` | `spinner_integration_test.go:227` | `InferenceStartedEvent` (non-critical) | `handlerCtx` = `context.WithTimeout(sessionCtx, 100ms)` | Yes, 100ms timeout | `_ =` | ✅ Safe |
| 2 | `TestSpinner_ContextTimeout_Resilience` | `spinner_integration_test.go:256` | `ResponseEvent` (critical) | `sessionCtx` = `context.WithCancel(context.Background())` | Only in t.Cleanup | `_ =` | ✅ Safe |

**Indirect calls via `capturedHandler`:**
| # | Test | File:Line | Event | Context | Cancellable? | Safe? |
|---|------|-----------|-------|---------|-------------|-------|
| 3 | `TestSpinner_E2E_Visibility` | `spinner_integration_test.go:*` (via `capturedHandler`) | `InferenceStartedEvent`, `ResponseEvent` | `ctx` from mock `Chat` arg → derived from `context.Background()` | No (Background never cancelled) | ✅ Safe |

### Spinner Integration Test Deep Dive

#### Test 1: `TestSpinner_E2E_Visibility`

**Context origin chain:**
```
context.Background()                           ← never cancelled
  → orch.Run(bgCtx, ...)
    → session_manager.Run → gCtx = WithCancel(bgCtx)
      → errgroup → chatAgent.Chat(gCtx, ...)
        → mock.Chat receives gCtx as ctx arg
          → capturedHandler(ctx, events.InferenceStartedEvent{})
            → bridge.HandleEvent(ctx, event)
```

**Analysis:** Every context in the chain derives from `context.Background()`, which is never cancelled. All events arrive with a live context. The `InferenceStartedEvent` (non-critical) goes through `enqueueNonCritical` with `ctx.Err() == nil` → event enqueued → spinner renders. The `ResponseEvent` (critical) clears the spinner. All assertions on stderr output (`"Thinking..."`, spinner frames `"⠋"`, `"⠙"`, ANSI clear `"\033[2K"`) depend on events being delivered — and they are.

**Verdict:** ✅ No behavioral change. Context is never cancelled during the test.

#### Test 2: `TestSpinner_ContextTimeout_Resilience`

**Context lifecycle at line 227:**

```go
// Line 208: Session context — alive for entire test duration
sessionCtx, sessionCancel := context.WithCancel(context.Background())
t.Cleanup(sessionCancel)  // cancelled only AFTER all assertions

// Line 224-225: Handler context — 100ms timeout, deferred cancel
handlerCtx, cancel := context.WithTimeout(sessionCtx, 100*time.Millisecond)
defer cancel()

// Line 227: HandleEvent called IMMEDIATELY after creating handlerCtx
_ = bridge.HandleEvent(handlerCtx, events.InferenceStartedEvent{})
```

**Timeline:**
1. `t=0`: `handlerCtx` created with 100ms deadline — `ctx.Err() == nil`
2. `t≈0`: `HandleEvent(handlerCtx, ...)` called — `ctx.Err()` check passes (nil)
3. `t≈0`: `InferenceStartedEvent` enqueued into bridge's eventQueue via `enqueueNonCritical`
4. `t≈0+`: Bridge's Listen loop picks up event, starts spinner
5. `t=100ms`: `handlerCtx` expires automatically — but spinner is already running on bridge's independent `loopCtx`
6. Test verifies spinner ticks at `t≈200ms`, `t≈400ms` — spinner still alive

**Why this is safe under PR #230:** The `ctx.Err()` check in `HandleEvent` (and `enqueueNonCritical`) runs at step 2, when the context is freshly created and definitely alive. The 100ms timeout hasn't elapsed. The event is enqueued before any cancellation. The bridge's `loopCtx` (set in `NewBridge` to `context.WithCancel(context.Background())`) is completely independent of `handlerCtx` — the spinner's lifecycle is not tied to the handler context.

**The test's semantics are preserved.** The test name says "ContextTimeout_Resilience" — it's testing that the spinner survives handler context expiry. This works identically before and after PR #230 because:
- Before: `enqueueNonCritical` didn't check context at all (always enqueued)
- After: `enqueueNonCritical` checks context, but context is alive at call time

**Context lifecycle at line 256:**
```go
_ = bridge.HandleEvent(sessionCtx, events.ResponseEvent{Content: &llm.Content{}})
```
- `sessionCtx` is alive (only cancelled in `t.Cleanup`, which hasn't run yet)
- `ResponseEvent` is critical — uses `enqueueCritical`, which also checks `ctx.Err()` but `sessionCtx` is alive
- Event delivered, spinner cleared

**Verdict:** ✅ No behavioral change. Both HandleEvent calls use contexts that are demonstrably alive at call time.

#### Test 3: `TestDispatcher_EndToEnd_ContextCancellation`

Line 244: `ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)`

This test exercises the executor dispatcher's cancellation, not the bridge. No `HandleEvent` or UI-related calls. Uses `exec.Execute(ctx, ...)` directly. Not affected by PR #230.

### Session Shutdown Patterns in Integration Tests

**Zero integration tests exercise the full session shutdown path** with the bridge subscriber. The tests either:
- Use `orch.Run(context.Background(), ...)` where the context is never cancelled (`TestSpinner_E2E_Visibility`)
- Construct a bridge directly and manage its lifecycle manually (`TestSpinner_ContextTimeout_Resilience`)
- Test executor/dispatcher internals without the bridge

No integration test creates a cancellable session context, cancels it mid-flight, and then asserts on non-critical event delivery. The shutdown race condition analyzed in Task 1 is **not exercised** by the integration test suite.

### Out-of-Tree Callers (Preview)

```
$ grep -rn 'bridge\.HandleEvent\|Bridge\.HandleEvent' --include='*.go' | grep -v _test.go | grep -v internal/agent/session/ui/

./internal/agent/session/session_manager.go:295:
    if err := bridge.HandleEvent(ctx, e); err != nil {
```

**One production caller:** The subscriber lambda in `session_manager.go:295` — already exhaustively analyzed in Task 1.

**Zero callers in `cmd/`:**

```
$ grep -rn 'HandleEvent\|enqueueNonCritical\|enqueueEvent' cmd/ --include='*.go'
(no matches)
```

**Zero callers in `pkg/`:** The `pkg/` directory is intended for public API clients; no bridge access.

### Summary

| Metric | Count |
|--------|-------|
| Integration test files touching bridge | 1 of 8 |
| `HandleEvent` calls in integration tests | 2 direct + 2 indirect via `capturedHandler` |
| Contexts alive at `HandleEvent` call time | 4 of 4 |
| Contexts already cancelled at `HandleEvent` call time | 0 of 4 |
| Tests depending on old non-deterministic behavior | 0 |
| Out-of-tree production callers of `Bridge.HandleEvent` | 1 (session_manager.go) |
| Callers in `cmd/` | 0 |
| Semantic correctness under new contract | ✅ All safe |

### Recommended Follow-ups

**None.** The integration test suite is semantically correct under the new deterministic `enqueueNonCritical` contract. The two `HandleEvent` calls in `spinner_integration_test.go` use contexts that are alive at call time, so the `ctx.Err()` check passes and events are delivered as expected.

**Observation for future work:** No integration test exercises the Ctrl+C / graceful shutdown path through the full session lifecycle (orchestrator → EventBus subscriber → bridge → spinner). This shutdown path is unit-tested (`bridge_test.go`, `event_queue_test.go`, `backpressure_test.go`) but not integration-tested. Filing a follow-up issue to add an integration test for the shutdown cascade would improve coverage of the edge case analyzed in Task 1.
