<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-025: Decompose UIBridge God Object into Composable Sub-Components

## Status
Proposed

## Context
The `UIBridge` in `internal/agent/session/ui_bridge.go` has grown to ~420 LOC and now handles 5 distinct responsibilities within a single struct of 20+ fields:

1. **State machine transitions** — `transition(next uiState)`, `is(state uiState)`, and the `uiState` enum.
2. **Spinner lifecycle** — `activePhase` tracking, `startSpinnerForPhase()`, `stopActiveSpinner()`, `resumeActiveSpinner()`, and `transitionSpinner()`.
3. **Event queue management** — channel initialisation (capacity 100), `enqueueEvent` with critical-vs-shed backpressure policy, `CloseInput`, drain semantics in `drainRemainingEvents`, and the `isClosed` atomic flag.
4. **Event dispatching** — a monolithic 12-case type-switch in `processEvent` with a cyclomatic complexity of 10+.
5. **Actor lifecycle** — `Listen` goroutine loop, `Cleanup` with timeout-based teardown, `WaitStarted` synchronisation, and `HandleEvent` public entry point.

The `processEvent` method violates the Open/Closed Principle: adding a new event type requires modifying the switch block and risks accidentally affecting unrelated branches. Furthermore, the mixing of all concerns in a single struct makes unit-testing individual responsibilities difficult — tests must construct the entire `UIBridge` with all its dependencies even when only exercising spinner transitions or queue backpressure.

## Decision
We will decompose `UIBridge` into 5 components held together by composition, with the original struct becoming a thin façade. The actor model (ADR-011) and event-driven architecture (ADR-018) are preserved; this refactoring only changes the internal organisation of the actor.

### Component Map

| Component | File | Responsibility |
|---|---|---|
| `UIBridge` (façade) | `ui_bridge.go` | ~80 LOC. Instantiates and wires the 4 sub-components via constructor injection. Exposes the unchanged public API: `HandleEvent`, `Listen`, `Cleanup`, `WaitStarted`, `CloseInput`. Holds the actor loop goroutine and delegates `processEvent` to `eventDispatcher.dispatch(ctx, e)`. |
| `eventQueue` | `ui_bridge_event_queue.go` | Channel initialisation (capacity 100), `enqueueEvent` with critical-vs-shed backpressure policy, `CloseInput`, drain semantics, `isClosed` check. Receives `loopCtx` for liveness signalling. |
| `uiStateMachine` | `ui_bridge_state_machine.go` | `transition(next uiState)`, `is(state uiState)` query, `stopActiveSpinner()` side-effect delegation. All state mutation is confined to the actor goroutine; no external locking. |
| `spinnerCoord` | `ui_bridge_spinner.go` | `activePhase` tracking, `startSpinnerForPhase()`, `stopActiveSpinner()`, `resumeActiveSpinner()`, `transitionSpinner()`. Delegates spinner stop to `uiStateMachine.stopActiveSpinner()` for consistency. |
| `eventDispatcher` | `ui_bridge_dispatcher.go` | Replaces the monolithic `switch` in `processEvent` with a handler-registration map: `map[reflect.Type]func(ctx context.Context, e events.Event)`. Each handler method (`handleTurnStatus`, `handleSpinnerEvent`, `handleResponse`, `handleToolEvents`, `handleSystemMessage`, `handleUsageMetrics`, `handleTurnStarted`, `handleConsentStarted`, `handleConsentFinished`) registers itself via an `init()`-style registration function called during construction. |

### Design Rules

- **Constructor injection**: All sub-components receive their dependencies (`renderer`, `logger`, `clock`) via explicit constructor parameters. No package-level globals.
- **State ownership**: Shared state (`state`, `stopSpinner`) lives exclusively in `uiStateMachine`. Other components access it through method calls — e.g. `spinnerCoord` calls `sm.stopActiveSpinner()` rather than touching `sm.stopSpinner` directly.
- **Façade delegation**: `UIBridge.Listen` calls `dispatcher.dispatch(ctx, e)` instead of `processEvent(ctx, e)`. `UIBridge.HandleEvent` calls `queue.enqueueEvent(ctx, e)`.
- **Package-level helpers preserved**: `isCriticalEvent` and `getSpinnerInfo` remain as pure functions at package scope — they have no state and benefit from being shared across components.
- **Actor model preserved**: All state mutation still happens exclusively inside the `Listen` goroutine. No mutexes are reintroduced.

### Handler Registration Pattern

```go
// In ui_bridge_dispatcher.go
type eventHandler func(ctx context.Context, e events.Event)

type eventDispatcher struct {
    handlers map[reflect.Type]eventHandler
    // injected dependencies...
}

func newEventDispatcher(renderer ports.UIRenderer, logger ports.Logger, sm *uiStateMachine, sc *spinnerCoord) *eventDispatcher {
    d := &eventDispatcher{
        handlers: make(map[reflect.Type]eventHandler),
    }
    d.register(events.TurnStatusEvent{}, d.handleTurnStatus)
    d.register(events.InferenceStartedEvent{}, d.handleSpinnerEvent)
    d.register(events.SummarizationStartedEvent{}, d.handleSpinnerEvent)
    d.register(events.ToolExecutionStartedEvent{}, d.handleSpinnerEvent)
    d.register(events.RetryWaitingEvent{}, d.handleSpinnerEvent)
    d.register(events.ConsentStartedEvent{}, d.handleConsentStarted)
    d.register(events.ConsentFinishedEvent{}, d.handleConsentFinished)
    d.register(events.ResponseEvent{}, d.handleResponse)
    d.register(events.UsageMetricsEvent{}, d.handleUsageMetrics)
    d.register(events.ToolCallEvent{}, d.handleToolEvents)
    d.register(events.ToolResultEvent{}, d.handleToolEvents)
    d.register(events.TurnStarted{}, d.handleTurnStarted)
    d.register(events.SystemMessageEvent{}, d.handleSystemMessage)
    d.register(events.StatusUpdate{}, d.handleSystemMessage)
    return d
}

func (d *eventDispatcher) register(e events.Event, h eventHandler) {
    d.handlers[reflect.TypeOf(e)] = h
}

func (d *eventDispatcher) dispatch(ctx context.Context, e events.Event) {
    if h, ok := d.handlers[reflect.TypeOf(e)]; ok {
        h(ctx, e)
    }
}
```

## Consequences

### Positive
- Each sub-component is independently testable with mock dependencies (e.g. `eventQueue` can be tested by injecting events and asserting drain behaviour without involving the renderer or state machine).
- `processEvent`/dispatch cyclomatic complexity drops from 10+ to ~3 (a single map lookup + call).
- New event types can be added by writing a handler method and registering it — no changes to the dispatcher core, satisfying the Open/Closed Principle.
- ~420 LOC monolithic file becomes ~5 files each under ~120 LOC, improving navigability.
- The public API of `UIBridge` (`HandleEvent`, `Listen`, `Cleanup`, `CloseInput`, `WaitStarted`) remains unchanged — all callers are unaffected.

### Negative
- More files and indirection: a developer must trace through the façade to find the actual handler implementation.
- The handler registration map uses `reflect.Type`, which is marginally slower than a compiled switch statement. This overhead is negligible in an actor loop processing human-scale UI events (tens per second, not millions).
- Risk of registration-order bugs if two handlers accidentally register for the same `reflect.Type` — the second registration silently overwrites the first. Mitigation: the registration map is populated in a single constructor function, making conflicts visible during code review.

### Neutral
- The actor model, backpressure strategy, and teardown contract from ADR-011 are preserved without modification.
- The event-driven orchestration architecture from ADR-018 is unchanged.
- Callers (`session.go`, orchestration layer) require zero changes.
