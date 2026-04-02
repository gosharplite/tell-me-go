# ADR-014: Event-Driven Orchestration and Circuit Breaker Pipeline

## Status
Accepted

## Context
The previous execution loop relied on a synchronous model with blocking calls. This approach had significant limitations, particularly on hot paths where thread-blocking could occur, making the system vulnerable to deadlocks and cascading failures when individual components experienced delays or errors. As the application scaled, this synchronous model proved inadequate for handling the complex, concurrent nature of our execution requirements without risking systemic failure.

## Decision
We have implemented an asynchronous, event-driven orchestration pipeline to replace the synchronous execution loop. This new architecture introduces the following core components:

1. **`SimpleEventBus`**: Acts as the central nervous system, routing events between decoupled components. Instead of components invoking one another directly, they now communicate by publishing and reacting to events through this bus.
2. **`uiBridge` State Machines**: Manages UI state asynchronously, responding to domain events without blocking execution.
3. **`CircuitBreakerPipeline`**: Wraps external or risky calls, monitoring for failure thresholds and tripping to prevent cascading failures.

## Consequences

### Positive
- **Resilience:** Circuit breakers prevent cascading failures by isolating component timeouts and errors.
- **Scalability and Bounded Memory Usage:** Bounded channels enable explicit load-shedding. Dropping visual events when queues are full prevents Out-Of-Memory (OOM) errors under heavy load.
- **Improved UI Responsiveness:** Asynchronous event handling prevents the UI from blocking during long-running background tasks.

### Negative
- **Complexity:** Asynchronous state machines make debugging and testing more complex. It requires us to heavily rely on strict channel synchronization (rather than wall-clock time) to ensure deterministic testing.
