## Audit Summary: `enqueueNonCritical` Contract Change (PR #230)

**Audit scope:** All production callers, test surfaces, and integration paths affected by making `enqueueNonCritical` deterministic — cancelled context always returns `context.Canceled` instead of silently succeeding via Go's randomized `select`.

**Audit date:** 2026-01

**Methodology:** 5-task structured audit. See `docs/audits/issue-232-task{1..5}.md` for full analysis per task.

---

### Call Sites Inventory

| Layer | Call Site | Location | Outcome | Reasoning |
|-------|-----------|----------|---------|-----------|
| **Subscriber** | Bridge subscriber lambda | `session_manager.go:295` | ⚠️ Behavioral drift | `context.Canceled` now deterministically returned during shutdown; downgraded to Debug |
| **Publisher 1** | `InferenceStartedEvent` | `engine_inference.go:43` | ✅ Safe | `SafePublish` deterministic `ctx.Done()` check unchanged; `_ =` ignores errors |
| **Publisher 2** | `SummarizationStartedEvent` | `summarizer.go:57` | ✅ Safe | Same; error logged but not propagated |
| **Publisher 3** | `ToolExecutionStartedEvent` | `engine_execution.go:33` | ✅ Safe | Same; `_ =` ignores errors |
| **Publisher 4** | `RetryWaitingEvent` | `engine_phases.go:146` | ✅ Safe | Same; preceded by `ctx.Err()` guard |
| **Publisher 5** | `TokenLimitReachedEvent` | `gatekeeper.go:177` | ✅ Safe | Published during context refinement; context alive |
| **Publisher 6** | `ConfigUpdated` | `agent.go:242` | ✅ Safe | `applyConfig` checks `ctx.Err()` as first operation |
| **Publisher 7** | `TraceEvent` | `engine.go:308` | ✅ Safe | Post-phase; lost on cancellation before and after PR #230 |
| **Publisher 8** | `SummarizationRequired` | `gatekeeper.go:283` | ✅ Safe | Published during context refinement; context alive |
| **Tests** | 40 tests across 10 files | `bridge_test.go`, `event_queue_test.go`, etc. | ✅ Safe | Co-delivered with PR #230; 10 tests assert new contract |
| **Integration** | 2 calls in 1 file | `spinner_integration_test.go:227,256` | ✅ Safe | Contexts alive at call time |
| **Out-of-tree** | cmd/, pkg/ | (none) | ✅ Safe | Zero callers; bridge fully encapsulated in `internal/` |
| **TurnsLogger** | Separate interface | `session_manager.go:271` | ✅ Safe | `TurnsLogger.HandleEvent` returns void — different contract |

### Behavioral Drift — Detail

**Location:** `session_manager.go:294-297` — the bridge subscriber lambda.

**Before PR #230:** When the EventBus subscriber loop's `listenCtx` was cancelled during shutdown, the subscriber lambda's `timeoutCtx` was cancelled. However, `enqueueNonCritical` used Go's randomized `select` without a context pre-check. A cancelled context could win or lose the race — the event might be silently enqueued (returning `nil`) or the context cancellation might win. The subscriber lambda rarely saw `context.Canceled`.

**After PR #230:** `enqueueNonCritical` (and `HandleEvent` itself) now check `ctx.Err()` deterministically before the non-blocking send. A cancelled context **always** returns `context.Canceled`, which hits the `logger.Warn` path in the subscriber.

**Impact:** During Ctrl+C / graceful shutdown, 0-2 Warning-level log lines may be emitted for in-flight non-critical events (primarily `InferenceStartedEvent`). During normal graceful shutdown (conversation completes fully), 0 warnings are emitted.

**Fix:** Downgrade `context.Canceled` errors from `Warn` to `Debug` at the subscriber lambda — see diff below.

### Diff Applied

```diff
--- a/internal/agent/session/session_manager.go
+++ b/internal/agent/session/session_manager.go
@@ -294,7 +294,11 @@ func (o *sessionManager) setupUIRendering(...)
 	chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
 		if err := bridge.HandleEvent(ctx, e); err != nil {
-			logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
+			if err == context.Canceled {
+				logger.Debug("Bridge event skipped: context cancelled", "event", fmt.Sprintf("%T", e))
+			} else {
+				logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
+			}
 		}
 	})
```

Uses `err == context.Canceled` (exact comparison) rather than `errors.Is` to avoid matching wrapped actor-death errors. Imports `"context"` already present (line 5).

### Shutdown Ordering (For Reference)

The two-tier cancellation cascade ensures deterministic shutdown:

1. **Tier 1 (EventBus/Agent):** `engine.Run` returns → `cancel()` fires → agent `gCtx` cancelled → EventBus `listenCtx` cancelled → subscriber loops drain channels → `EventBus.Listen` returns
2. **Tier 2 (Bridge/Session):** `chatAgent.Chat` returns → `cancel()` fires → session `gCtx` cancelled → `bridge.Listen` drains remaining events with `context.Background()` → bridge exits

Events already in the bridge's eventQueue are safely rendered. The race window is only in the EventBus subscriber channel (analyzed in Task 1).

### Test Verification

```
go test -race -count=10 ./internal/agent/session/...          → PASS (3 packages, ~10s)
go test -race -count=10 ./tests/integration/agent/session/... → PASS (13.8s)
```

0 failures, 0 flakes across 10 iterations per package. Race detector clean.

### Conclusion

The `enqueueNonCritical` contract change is **safe to accept**. One behavioral drift was identified and addressed with a targeted fix. No other call sites — production, test, or integration — depend on the pre-#230 non-deterministic behavior. The change eliminates a latent race condition in shutdown event delivery.

### Related Documents

| Document | Content |
|----------|---------|
| `docs/audits/issue-232-task1.md` | Subscriber-side shutdown analysis |
| `docs/audits/issue-232-task2.md` | Producer-side context tracing (8 publishers) |
| `docs/audits/issue-232-task3.md` | Test surface audit (40 tests, stress results) |
| `docs/audits/issue-232-task4.md` | Integration test surface + out-of-tree check |
| `docs/audits/issue-232-task5.md` | Final cross-cutting check + acceptance criteria |
