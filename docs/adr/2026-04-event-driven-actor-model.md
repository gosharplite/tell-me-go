# ADR: Migrate Orchestrator to Event-Driven Actor Model

## Status
Accepted

## Context
The previous orchestrator implementation relied on a synchronous, mutex-heavy execution model for managing state and coordinating between the orchestration layer and the UI transport. This approach had significant limitations:
- **Blocking Operations:** Heavy reliance on mutexes created contention and thread-blocking, particularly on hot paths.
- **Deadlock Vulnerabilities:** Synchronous calls between subsystems increased the risk of systemic deadlocks, especially when components experienced delays, errors, or crashed unexpectedly.
- **Coupling:** The execution logic was tightly coupled with UI rendering and transport, making it difficult to test in isolation and scale independently.

As the system grew in complexity with concurrent tool execution and streaming responses, a non-blocking, asynchronous approach became necessary to ensure resilience and maintainability.

## Decision
We have migrated the core orchestrator to an asynchronous, event-driven actor model using the `uiBridge` component. This architecture introduces the following structural changes:

1. **`uiBridge` Actor Model:** The orchestrator now uses an isolated goroutine (the "actor") to process state changes sequentially via an `eventCh`. This completely eliminates the need for complex mutex locking around UI state.
2. **`CircuitBreakerPipeline`:** We introduced a resilient pipeline to wrap external, risky, or long-running calls, providing failure threshold monitoring and preventing cascading failures across the system.
3. **Decoupled Event Processing:** Domain events are now routed asynchronously. The execution engine produces events, and the `uiBridge` consumes them, effectively decoupling the execution logic from the UI transport layer.

## Consequences (Trade-offs)

### Positive
- **Lock-Free Concurrency:** State is mutated only within the single `uiBridge` actor loop, eliminating race conditions and mutex contention.
- **Better Separation of Concerns:** Execution logic is completely decoupled from UI rendering. The orchestrator merely emits events.
- **Resilience:** The `CircuitBreakerPipeline` isolates component failures.

### Negative
- **Increased Complexity:** Asynchronous flows are inherently harder to trace, debug, and reason about than synchronous code.
- **Testing Challenges:** Unit tests must now rely on channel synchronization and eventual consistency rather than immediate sequential assertions.
- **Flow Control Overhead:** Bounded channels require careful management of backpressure to avoid dropping critical events or blocking producers.

## Mitigations (Crucial)

To address the complexities of asynchronous flow control and explicitly prevent deadlocks, we implemented a robust liveness-monitoring and backpressure pattern:

1. **Backpressure via Bounded Channels:** The `eventCh` applies natural backpressure. If the consumer (`uiBridge`) falls behind, producers will block on `enqueueEvent` rather than exhausting memory.
2. **Deadlock Prevention (Liveness Signaling):** To prevent publishers from blocking indefinitely if the consumer actor dies, `enqueueEvent` monitors a dedicated `loopCtx` (`<-b.loopCtx.Done()`). 
3. **Guaranteed Cancellation Broadcast:** Inside the `uiBridge.loop` worker, we enforce a strict `defer b.loopCancel()` at the absolute top of the method block. This guarantees that whether the actor exits gracefully or crashes via a panic, the context is immediately canceled. The cancellation signal is instantly broadcast to all blocked producers, allowing them to fast-fail and preventing the entire system from hanging.