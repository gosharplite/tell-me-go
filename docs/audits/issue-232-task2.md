## Task 2 Findings: Producer-Side Analysis

### Publisher Inventory

| # | Event Type | Location (file:line) | Context Source | Cancellable? | Error Checked? | Risk |
|---|---|---|---|---|---|---|
| 1 | `InferenceStartedEvent` | `engine_inference.go:43` | `ctx` (caller param) | Yes | No (`_ =`) | ✅ Safe |
| 2 | `SummarizationStartedEvent` | `summarizer.go:57` | `ctx` (caller param) | Yes | Yes (logged, not returned) | ✅ Safe |
| 3 | `ToolExecutionStartedEvent` | `engine_execution.go:33` | `ctx` (caller param) | Yes | No (`_ =`) | ✅ Safe |
| 4 | `RetryWaitingEvent` | `engine_phases.go:146` | `ctx` (caller param) | Yes | No (`_ =`) | ✅ Safe |
| 5 | `TokenLimitReachedEvent` | `gatekeeper.go:177` | `ctx` (caller param) | Yes | Yes (logged) | ✅ Safe |
| 6 | `ConfigUpdated` | `agent.go:242` | `ctx` (caller param) | Yes | Yes (returned) | ✅ Safe |
| 7 | `TraceEvent` | `engine.go:308` | `ctx` (OTel span ctx) | Yes | Yes (logged) | ✅ Safe |
| 8 | `SummarizationRequired` | `gatekeeper.go:283` | `ctx` (caller param) | Yes | Yes (returned) | ✅ Safe |

**Note:** `ErrorEvent` and `LogEvent` do not exist as concrete types in the codebase. What the issue likely refers to are `SystemMessageEvent` with `Level: "error"` or `Level: "info"` — but `SystemMessageEvent` IS classified as critical in `isCriticalEvent` (bridge.go:277). These are **not** affected by the `enqueueNonCritical` change.

### Detailed Analysis Per Publisher

#### Publisher 1: `InferenceStartedEvent` in `InvokeModel`

```go
// engine_inference.go:43
func (p *InferenceStep) InvokeModel(ctx context.Context, Turn *Turn) (...) {
    _ = events.SafePublish(ctx, Turn.Events, events.InferenceStartedEvent{Model: Turn.Model})
    //                                    ^^^ caller's ctx
    defer func() {
        stopCtx := context.WithoutCancel(ctx)  // ResponseEvent uses WithoutCancel
        events.SafePublish(stopCtx, Turn.Events, events.ResponseEvent{...})
    }()
    respContent, metrics, err = Turn.Gateway.Generate(ctx, ...)
    // ...
}
```

- **Context origin:** The `ctx` is the engine's execution context, derived from `agent.Chat.gCtx` → `engine.Run` → `ExecuteTurn` → `runPhaseLoop` → `executePhase` → `InferenceStep.Process` → `InvokeModel`. This is the active turn's OTel span context.
- **Timing relative to shutdown:** Published at the START of `InvokeModel`, BEFORE the blocking `Generate` call. At publish time, `ctx` is always alive (the function just started). If the context becomes cancelled later (e.g., during `Generate`), the event was already published to the EventBus.
- **Asymmetry confirmed:** `InferenceStartedEvent` uses the caller's `ctx` (cancellable), while `ResponseEvent` uses `context.WithoutCancel(ctx)` (detached). This is intentional — the `InferenceStartedEvent` starts the spinner and is fire-and-forget, while the `ResponseEvent` MUST clear the spinner even on timeout.
- **Error visibility:** `_ = events.SafePublish(...)` — error is silently ignored. If `SafePublish` fails (e.g., bus not initialized), no one notices. This is by design for non-critical events.
- **Risk verdict:** **Safe.** `SafePublish` has a deterministic `ctx.Done()` check (non-blocking select, not random). If `ctx` is cancelled, the event is dropped at the `SafePublish`/`Publish` level — this was true before and after PR #230. The `enqueueNonCritical` change at the bridge level is irrelevant for publisher-side context cancellation.

#### Publisher 2: `SummarizationStartedEvent` in `Summarize`

```go
// summarizer.go:57
func (s *summarizer) Summarize(ctx context.Context, subset []*llm.Content, focus string) (...) {
    if s.events != nil {
        if err := events.SafePublish(ctx, s.events, events.SummarizationStartedEvent{}); err != nil {
            if !errors.Is(err, events.ErrBusNotInitialized) {
                s.logger.Error("event_publish_failed", ...)
            }
        }
    }
    // ... then blocking summarization call
}
```

- **Context origin:** `ctx` comes from either `TokenGatekeeper.autoSummarize` (automatic) or `Manager.SummarizeRange` (manual). Both derive from the engine's execution chain.
- **Timing:** Published BEFORE the blocking `s.gateway.Generate(ctx, summarizerInput, ...)` call. Context is alive at publish time.
- **Error visibility:** Errors are logged at Error level (except `ErrBusNotInitialized` which is silently skipped). This was the same before PR #230.
- **Risk verdict:** **Safe.** Same reasoning as Publisher 1 — deterministic `SafePublish` check prevents events with cancelled contexts from reaching the EventBus.

#### Publisher 3: `ToolExecutionStartedEvent` in `publishPreExecution`

```go
// engine_execution.go:33
func (p *ExecutionStep) publishPreExecution(ctx context.Context, Turn *Turn) {
    // ...
    _ = events.SafePublish(ctx, Turn.Events, events.ToolExecutionStartedEvent{ToolNames: names})
}

func (p *ExecutionStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
    // ...
    p.publishPreExecution(ctx, Turn)
    toolResponse, err := Turn.Executor.Execute(ctx, Turn.State.Response, ...)
    // ...
}
```

- **Context origin:** `ctx` from `ExecutionStep.Process`, which is the engine's execution context.
- **Timing:** Published BEFORE `Turn.Executor.Execute(ctx, ...)`. Context is alive.
- **Error visibility:** `_ =` — silently ignored by design (fire-and-forget).
- **Risk verdict:** **Safe.**

#### Publisher 4: `RetryWaitingEvent` in `attemptRetry`

```go
// engine_phases.go:146
func (p *RecoveryStep) attemptRetry(ctx context.Context, Turn *Turn, delay time.Duration) (...) {
    // ...
    _ = events.SafePublish(ctx, Turn.Events, events.RetryWaitingEvent{Duration: delay})

    // Coverage: defensive guard
    if err := ctx.Err(); err != nil {
        return ProcessResult{}, err
    }

    select {
    case <-ctx.Done():
        return ProcessResult{}, ctx.Err()
    case <-Turn.Clock.After(delay):
    }
    // ...
}
```

- **Context origin:** `ctx` from `RecoveryStep.attemptRetry`, which is called from `RecoveryStep.Process`, which is within the engine's execution chain.
- **Timing:** Published BEFORE the retry backoff delay. Context check happens AFTER publish — but the context is alive at publish time (otherwise the prior error path would have been taken).
- **Error visibility:** `_ =` — fire-and-forget. The comment "Publish RetryWaitingEvent to show the spinner during the backoff delay" confirms this is purely cosmetic.
- **Risk verdict:** **Safe.** The context guard on line 149 (`if err := ctx.Err()`) catches cancellation between event publish and the select, preventing a cancelled context from proceeding further.

#### Publisher 5: `TokenLimitReachedEvent` in `publishHardLimitEvents`

```go
// gatekeeper.go:177
func (t *TokenGatekeeper) publishHardLimitEvents(ctx context.Context, tokens, effectiveLimit int) {
    e1 := events.TokenLimitReachedEvent{Tokens: tokens, MaxLimit: t.MaxTokens}
    if err := events.SafePublish(ctx, t.Events, e1); err != nil {
        if !errors.Is(err, events.ErrBusNotInitialized) {
            t.getLogger().Error("event_publish_failed", ...)
        }
    }
    // Also publishes SystemMessageEvent (critical)
}
```

- **Context origin:** `ctx` from `TokenGatekeeper.validateHardLimits`, called during `ContextRefiner.Process` → `Manager.Prepare` pipeline. This is the engine's execution context within a turn.
- **Timing:** Published during the Guard/Refinement phase (before LLM inference begins). Context is alive.
- **Error visibility:** Errors logged at Error level. Event loss is visible in logs.
- **Risk verdict:** **Safe.**

#### Publisher 6: `ConfigUpdated` in `publishConfigUpdate`

```go
// agent.go:242
func (a *agent) publishConfigUpdate(ctx context.Context, cfg *runtimeConfig) error {
    if err := events.SafePublish(ctx, a.events, events.ConfigUpdated{Limits: cfg.Limits}); err != nil {
        if !errors.Is(err, events.ErrBusNotInitialized) {
            a.getLogger().Error("event_publish_failed", ...)
            return err  // SHORT-CIRCUITS the applyConfig chain per ADR-029
        }
    }
    return nil
}
```

- **Context origin:** `ctx` from `agent.applyConfig`, called from `agent.Chat` at the beginning of each conversation and each turn.
- **Timing:** `applyConfig` checks `ctx.Err()` as its FIRST operation (agent.go:218). If context is cancelled, it returns immediately without publishing.
- **Error visibility:** Errors ARE returned to the caller, short-circuiting the `applyConfig` delegate chain per ADR-029. This is the **only** non-critical publisher that propagates errors to the caller.
- **Risk verdict:** **Safe.** The pre-check `ctx.Err()` guarantees a live context. The error propagation is by design (ADR-029 fail-fast).

#### Publisher 7: `TraceEvent` in `ExecuteTurn`

```go
// engine.go:308
func (e *Engine) ExecuteTurn(parentCtx context.Context, Turn *Turn) error {
    ctx, span := otel.Tracer("agent").Start(parentCtx, "agent.Turn")
    defer span.End()
    // ...
    err := e.runPhaseLoop(ctxWithTrace, Turn)  // <-- ALL phases run here

    e.finalizeTurnTrace(trace, err)
    if err := events.SafePublish(ctx, e.events, events.TraceEvent{Trace: trace}); err != nil {
        //                                                          ^^^ OTel span ctx
        if !errors.Is(err, events.ErrBusNotInitialized) {
            e.getLogger().Error("event_publish_failed", ...)
        }
    }
    // ...
}
```

- **Context origin:** `ctx` is the OTel span context from `otel.Tracer("agent").Start(parentCtx, ...)`. `parentCtx` is the agent's `gCtx` from `agent.Chat`.
- **Timing:** Published AFTER `runPhaseLoop` returns. **This is the only non-critical publisher that fires after all phase processing is complete.** If `runPhaseLoop` returned due to context cancellation (Ctrl+C), `ctx` IS cancelled at this point.
- **Error visibility:** Errors logged at Error level. A cancelled context produces: `"publish failure for event TraceEvent: context canceled"` at Error level.
- **Behavior before PR #230:** When `ctx` was cancelled, `SafePublish` → `Publish` → non-blocking `ctx.Done()` check → `context.Canceled` returned → `SafePublish` wrapped it → Error logged. **Exactly the same.** The deterministic `ctx.Done()` check in `Publish` always caught this. PR #230's change to `enqueueNonCritical` is irrelevant here — the event never reaches the bridge when the publisher context is cancelled.
- **Risk verdict:** **Safe.** No behavioral change. The `TraceEvent` was already lost during Ctrl+C shutdown (logged as Error), and this remains the case.

#### Publisher 8: `SummarizationRequired` in `publishSummarizationEvent`

```go
// gatekeeper.go:283
func (t *TokenGatekeeper) publishSummarizationEvent(ctx context.Context, tokens, limit int, reason string) error {
    evt := events.SummarizationRequired{Tokens: tokens, MaxLimit: limit, Reason: reason}
    if err := events.SafePublish(ctx, t.Events, evt); err != nil {
        if !errors.Is(err, events.ErrBusNotInitialized) {
            t.getLogger().Error("event_publish_failed", ...)
            return err  // Propagated to triggerSummarization, which may halt pipeline
        }
    }
    return nil
}
```

- **Context origin:** `ctx` from `TokenGatekeeper.handleSafetyPressure` → `triggerSummarization`, called during `ContextRefiner.Process` pipeline. Engine execution context.
- **Timing:** Published during context refinement (before LLM inference). Context is alive.
- **Error visibility:** Errors returned to `triggerSummarization`, which checks `isBlockingError`. A publish failure does NOT block summarization (it's not a blocking error type).
- **Risk verdict:** **Safe.**

### Defer and Goroutine Analysis

**Defer blocks:** The only defer that publishes events is `InvokeModel` (engine_inference.go:45-54), which publishes `ResponseEvent` (CRITICAL). No non-critical events are published in defer blocks.

**Goroutines:** Tool execution goroutines (`parallelWorker`, `executeToolSafe`) do not directly publish events — they return results through channels. The `handlePanic` method publishes `SystemMessageEvent` (CRITICAL). No non-critical events are published from goroutines.

**Heartbeat goroutines** (`startHeartbeat` in dependency.go, `startGoDocHeartbeat` in info.go) publish only `SystemMessageEvent` (CRITICAL).

### Key Architectural Insight

The `SafePublish` → `Publish` path has a **deterministic** context cancellation check:

```go
// event_bus_pubsub.go
func (b *SimpleEventBus) Publish(ctx context.Context, event Event) error {
    select {
    case <-ctx.Done():          // Non-blocking: if ctx cancelled, ALWAYS returns error
        return ctx.Err()
    default:
    }
    // ...
}
```

This means a cancelled publisher context **deterministically prevents** the event from entering the EventBus. There is no random `select` race at this level. PR #230's change to `enqueueNonCritical` (which replaced a random select with a deterministic check) is at a different layer — the bridge subscriber. The two layers are:

```
Publisher ctx → SafePublish (deterministic check) → EventBus.Publish (deterministic check)
  → subscriber loop (random select: w.ch vs listenCtx.Done)       <-- race here
    → subscriber lambda (timeoutCtx) → bridge.HandleEvent
      → enqueueNonCritical (NOW deterministic, was random select)  <-- PR #230 change
```

The publisher-side context cancellation is already caught deterministically at layer 1. The PR #230 change adds determinism at layer 3 (bridge). The only remaining random select is at layer 2 (EventBus subscriber loop), which was analyzed in Task 1.

### Summary

- **Total publishers found:** 8
- **Safe (no behavioral change):** 8
- **Behavioral drift (follow-up ticket needed):** 0
- **Hot-fix needed:** 0

### Recommended Follow-ups

**None required at the publisher level.** All publishers emit non-critical events during normal execution flow with live contexts. The `SafePublish` → `Publish` path already deterministically blocks events with cancelled publisher contexts (non-blocking `ctx.Done()` check). PR #230's change to `enqueueNonCritical` does not introduce any new publisher-side failure modes.

The only follow-up item is from Task 1 (subscriber-side): downgrade `context.Canceled` to Debug at the subscriber lambda in `session_manager.go:296`.
