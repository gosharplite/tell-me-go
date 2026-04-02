# ADR-013: Asynchronous Event-Driven Orchestration

## Status
Accepted

## Context
The Orchestrator had grown into a "God Object," heavily reliant on `sync.RWMutex` for state management, which created severe lock contention. Furthermore, the `EventBus` dispatched telemetry synchronously, risking Orchestrator stalls if a subscriber (like a DB or UI) hung. The persistence layer suffered from N+1 query patterns, and goroutine boundaries lacked formal panic recovery, making the agent fragile under load.

## Decision
We refactored the core execution engine to an asynchronous, lock-free model:
1. **State Management:** Replaced mutexes with a channel-based fan-in Aggregator (Reducer) loop and `atomic.Pointer` for configurations.
2. **Telemetry:** Transitioned the `EventBus` to use isolated subscriber channels with non-blocking sends and load shedding (dropping events if buffers fill).
3. **Persistence:** Enforced pure domain models and implemented DTO mapping with bulk SQL inserts to eliminate N+1 roundtrips.
4. **Resiliency:** Enforced panic-recovery middleware on all goroutine boundaries and implemented context-driven graceful shutdown sequences.

## Consequences
* **Positive:** Massive scalability improvements, zero lock contention in the hot path, robust fault tolerance, and strict adherence to Hexagonal Architecture boundaries.
* **Negative:** Increased cognitive load when tracing asynchronous execution paths and telemetry drops under extreme backpressure.