## Task 1 Findings

### Shutdown Ordering

The system uses a **two-tier context cancellation cascade** during shutdown:

**Tier 1 — EventBus/Agent (inner):**
When `engine.Run` returns (either successfully or with context-cancelled error):

1. `defer cancel()` fires in `agent.Chat` → agent-level `gCtx` cancelled
2. This cascades through `errgroup.WithContext` chain: agent gCtx → Chat errgroup ctx → StartTelemetry errgroup ctx → EventBus `listenCtx`
3. EventBus subscriber loops see `listenCtx.Done()` → **drain** subscriber channels (events in `w.ch` are decremented and dropped, NOT delivered to subscribers)
4. `EventBus.Listen` returns → `StartTelemetry` returns → `chatAgent.Chat` returns

**Tier 2 — Bridge/Session (outer):**
After `chatAgent.Chat` returns:

5. `defer cancel()` fires in `session_manager.Run` → session `gCtx` cancelled
6. `bridge.Listen(gCtx)` sees cancellation → drains bridge eventQueue with `context.Background()` (events already in bridge queue ARE safely rendered)
7. `g.Wait()` returns → deferred cleanup: `chatAgent.Shutdown` → `EventBus.Flush` + `EventBus.Shutdown`

**Critical race window:** The EventBus subscriber loops shut down BEFORE the bridge. Events that are still in the EventBus subscriber channel (`w.ch`, capacity 1024) when `listenCtx` is cancelled face a Go `select` race:
- ~50%: event received from `w.ch` → `timeoutCtx` derived from (now cancelled) `listenCtx` → subscriber lambda receives cancelled context → `bridge.HandleEvent` returns `context.Canceled` → **Warning logged**
- ~50%: `ctx.Done()` case wins → `drain(w)` → event silently dropped

**Graceful shutdown:** Zero events in `w.ch` (engine completed all phases, all events consumed). No warnings.

**Ctrl+C mid-conversation:** 1-2 events in `w.ch` (typically `InferenceStartedEvent` ± one more). 0-2 potential warnings.

### Warn Log Impact

| Event Type | Critical? | During Graceful Shutdown | During Ctrl+C | Acceptable? |
|---|---|---|---|---|
| `InferenceStartedEvent` | No | 0 | 0-1 (race) | Yes — negligible |
| `SummarizationStartedEvent` | No | 0 | 0 (rarely mid-summarization) | Yes |
| `ToolExecutionStartedEvent` | No | 0 | 0 (rarely mid-tool-exec) | Yes |
| `RetryWaitingEvent` | No | 0 | 0 (rarely mid-retry-backoff) | Yes |
| `TokenLimitReachedEvent` | No | 0 | 0 (guard phase, fast) | Yes |
| `ConfigUpdated` | No | 0 | 0 | Yes |
| `TraceEvent` | No | 0 | 0 | Yes |
| `SummarizationRequired` | No | 0 | 0 | Yes |

All non-critical events are "progress indicator" style — they start spinners or log informational state. They are emitted within engine phases that complete **before** `engine.Run` returns. In graceful shutdown, all phases complete normally and events are consumed. In Ctrl+C, at most 1-2 events are in-flight in the subscriber channel.

**Conclusion: The Warn noise is negligible — 0 lines during graceful shutdown, 0-2 lines during forced cancellation.** This is not a noise problem.

### Dropped UI Signals

**No non-critical events are expected to render during shutdown.** All non-critical events are spinner-start signals:

- `InferenceStartedEvent` → "Thinking..." spinner
- `SummarizationStartedEvent` → "Compressing context..." spinner
- `ToolExecutionStartedEvent` → "Executing tools..." spinner
- `RetryWaitingEvent` → "Retrying in..." spinner

The **critical** `ResponseEvent` — which clears the spinner — is published via `context.WithoutCancel` in `InvokeModel` (line 52 of `engine_inference.go`). This ensures it reaches the EventBus even when the caller context is cancelled. It is processed **before** `engine.Run` returns, so it arrives while the EventBus subscriber loop is still alive. The spinner-clear signal is **not** affected by this change.

The bridge's `Listen` loop calls `drainRemainingEvents` with `context.Background()` when its context is cancelled, so any events already in the bridge's own queue are rendered during shutdown regardless.

### Current Error Handling at Subscriber

Two production subscribers exist in `session_manager.go`:

**Line 270 — TurnsLogger (not affected):**
```go
chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
    turnsLogger.HandleEvent(ctx, e)  // error silently ignored
})
```

**Line 294 — Bridge (the one affected):**
```go
chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
    if err := bridge.HandleEvent(ctx, e); err != nil {
        logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
    }
})
```

There is **no filtering** for `context.Canceled`. Every non-nil error — including the expected shutdown signal `context.Canceled` — is logged at Warn level.

### Recommendation

**(b) Downgrade to Debug for `context.Canceled` errors**

Rationale:
- `context.Canceled` during shutdown is **expected behavior**, not a warning-worthy condition
- The Warn count is trivially small (0-2 during Ctrl+C), so noise is not the primary motivator; **semantic correctness** is
- Option (a) — "accept as correct surfacing" — is defensible but sends the wrong signal: developers monitoring Warn logs should not see expected-shutdown events
- Option (c) — "silently drop" — hides the error completely, which would mask a real bug if `context.Canceled` were returned outside of shutdown
- Debug-level logging preserves observability for troubleshooting without polluting Warn-level monitoring

### Diff (for option b)

```diff
--- a/internal/agent/session/session_manager.go
+++ b/internal/agent/session/session_manager.go
@@ -291,10 +291,14 @@ func (o *sessionManager) setupUIRendering(ctx context.Context, chatAgent ports.C
 		ui.WithBridgeClock(o.Clock),
 	)
 	chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
 		if err := bridge.HandleEvent(ctx, e); err != nil {
-			logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
+			if errors.Is(err, context.Canceled) {
+				logger.Debug("Bridge event skipped: context cancelled", "event", fmt.Sprintf("%T", e))
+			} else {
+				logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
+			}
 		}
 	})
 	return bridge
 }
```

**Additional note:** The `errors` and `context` imports are already present in `session_manager.go` (confirmed at lines 4-7).
