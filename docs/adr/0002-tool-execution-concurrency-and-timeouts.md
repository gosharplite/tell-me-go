# ADR-002: Standardize Tool Execution Concurrency, Timeouts, and Context Propagation

## Status
Accepted

## Context
The previous `ToolExecutor` implementation suffered from several architectural flaws:
1. **Unbounded Timeouts:** Tools could run indefinitely, leading to resource exhaustion.
2. **Mutable Configuration:** Tool options were mutable at runtime, risking race conditions.
3. **Goroutine Leaks:** Lack of strict `context.Context` propagation meant tools could continue running as "zombie goroutines" even after the main execution was cancelled or timed out.
4. **Deadlocks:** A global lock for all tool executions caused contention and deadlocks when "Serial" tools (which require exclusive access) were mixed with parallel tool reads.

## Decision
We have overhauled the tool execution engine to enforce the following 4 core pillars:
1. **Bounded Timeouts via Immutable Functional Options:** Timeouts are now strictly enforced using immutable options (e.g., `WithLongRunningTimeout`). Once an executor is configured, its timeout policies cannot be changed, ensuring predictable behavior.
2. **Mandatory Context Propagation:** All tools must now accept a `context.Context`. **Rule:** *All long-running tools MUST actively poll `ctx.Done()` during blocking I/O or heavy computation loops to ensure prompt termination upon cancellation.*
3. **Barrier Pattern via Task Batching:** The engine implements a Barrier Pattern using **Task Batching and WaitGroups** rather than an RWMutex.
    * Consecutive parallel tools are grouped into a batch and executed concurrently, waiting on a `sync.WaitGroup`.
    * Serial tools (mutators) force a batch break and run in complete isolation in their own sequential batch, preventing state corruption and deadlocks.
4. **Structured Observability:** Every tool execution now emits structured logs capturing execution duration, success/failure status, and timeout events. This provides clear visibility into system performance and bottleneck identification.

## Consequences
- **Positive:** Elimination of zombie goroutines and associated OOM/resource leak risks.
- **Positive:** Significant reduction in deadlock potential through sequential batch isolation.
- **Positive:** Improved system stability and predictability under high concurrency.
- **Positive:** Better auditability of tool performance via structured logging.
- **Negative:** Slightly more boilerplate for tool authors, who must now explicitly handle context cancellation.
- **Negative:** Increased complexity in the `ToolExecutor` internal logic to manage batch orchestration and timeout timers.
