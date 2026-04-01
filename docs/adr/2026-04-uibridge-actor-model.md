# ADR-011: Refactor UI Bridge to Actor Model for Thread-Safe Asynchronous Rendering

## Status
Accepted

## Context
The `uiBridge` component is responsible for translating domain events (e.g., `InferenceStartedEvent`, `ToolCallEvent`) into UI updates (e.g., starting/stopping spinners, rendering text). 

Prior to this refactor, the `uiBridge` suffered from several issues:
1. **Data Races**: It used a `sync.Mutex` to protect shared state, but the asynchronous nature of the `EventBus` and the complex state transitions (inference vs. tools vs. response) made it difficult to prevent races.
2. **Flaky Tests**: Tests relied on `time.Sleep` to wait for asynchronous rendering side-effects, leading to unpredictable failures in CI environments.
3. **Complex Lifecycle**: Stopping spinners and cleaning up resources on unexpected errors was error-prone and lacked a formal teardown mechanism, risking goroutine leaks.

## Decision
We have refactored `uiBridge` to use the **Actor Model** and enforced a deterministic testing strategy.

### 1. Actor Pattern Implementation
*   **Sequential Processing**: The `uiBridge` now maintains a private `loop()` goroutine that reads events from a mailbox channel (`eventCh`). All state mutations happen exclusively within this goroutine.
*   **No Mutexes**: The use of `sync.Mutex` within `uiBridge` is strictly forbidden. State variables like `isRendering`, `activePhase`, and `isWaitingForConsent` are private to the actor loop.
*   **Non-Blocking Submission**: `handleEvent` sends events to the channel without blocking the caller (up to the channel capacity), ensuring the domain logic remains responsive.

### 2. Lifecycle Management
*   **Graceful Teardown**: The component implements a `Cleanup()` method.
*   **Resource Safety**: `Cleanup()` closes a `done` channel to signal the loop to exit and uses a `sync.WaitGroup` to wait for the goroutine to terminate.
*   **Idempotency**: `Cleanup()` uses `sync.Once` to ensure it can be called safely multiple times (e.g., in a `defer` block and an error path).

### 3. Deterministic Testing Rules
To ensure 100% reliability and zero flakiness, the following rules are enforced for all tests interacting with `uiBridge`:

*   **Rule 1: No `time.Sleep`**: The use of arbitrary sleeps for synchronization is strictly prohibited.
*   **Rule 2: Positive Synchronization via Mock Callbacks**: When asserting that a UI action occurred, use `testify/mock`'s `.Run()` method to signal a channel.
    ```go
    done := make(chan struct{})
    mRenderer.On("LogTurnStatus", mock.Anything).Run(func(args mock.Arguments) {
        close(done)
    })
    // ... wait for <-done with timeout
    ```
*   **Rule 3: Negative Synchronization via Sentinel Events**: When asserting that an action did *not* occur (e.g., `AssertNotCalled`), use the `syncBridge()` helper. This helper sends a sentinel domain event (e.g. `SystemMessageEvent`) through the bridge and waits for its corresponding mock call, guaranteeing that all previous events in the queue have been handled.
    ```go
    bridge.handleEvent(ctx, someEvent)
    syncBridge(t, bridge, mRenderer) // Flushes the queue using a sentinel
    mRenderer.AssertNotCalled(t, "ForbiddenAction")
    ```

## Consequences
*   **Positive**: Elimination of data races in UI rendering logic.
*   **Positive**: Deterministic, high-speed test suite with zero flakiness.
*   **Positive**: Formal lifecycle management prevents goroutine leaks.
*   **Positive**: Improved code readability by centralizing state transitions in a single `switch` statement within the loop.
*   **Negative**: Introduction of a small amount of boilerplate for event synchronization in tests.
