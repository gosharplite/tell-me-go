# 2026-04 Accept Dual-Write for Telemetry and UI Events

## Status
Accepted

## Context
In our system architecture, we have decoupled state persistence (e.g., history management and configurations via repositories/DB) from asynchronous event publishing (via `EventBus`). This separation introduces a classical distributed systems vulnerability: the **Dual-Write Problem**. 

For example, during an agent's execution turn or orchestration execution, state is updated and saved to the database, followed immediately by publishing an event (e.g., `events.SafePublish(...)`). If the application crashes or the power fails exactly between the database transaction committing and the event being published, the state changes persist, but the downstream event subscribers (like the UI or telemetry collectors) are never notified.

Mitigating this issue with strict consistency would require implementing a Transactional Outbox pattern. This means that instead of publishing directly to the event bus, the system would insert events into an `outbox_events` table within the same SQL transaction as the state update, then rely on a background relay process to read those records and publish them to the event bus.

## Decision
We will **accept the dual-write risk** for all currently implemented events, including Telemetry, Tracing, and UI events (e.g., `TraceEvent`, `TurnStarted`, `SystemMessageEvent`, `ToolExecutionStartedEvent`, `ToolResultEvent`).

These events are classified as **non-critical/lossy**. Implementing a Transactional Outbox would introduce significant latency and complexity to the critical path, negatively impacting system throughput and maintainability. The trade-off is acceptable because dropping a UI update or missing a single telemetry metric during a hard crash does not corrupt domain state or compromise core business logic.

## Consequences
- **Positive:** High throughput and low latency on the critical path. The system architecture remains simple and performant without the overhead of an outbox table, outbox cleanup logic, and a background relay process.
- **Negative:** If the agent crashes exactly after a state commit but before event publication, the UI might hang in a loading state or miss a final status update. Telemetry data might have a slight discrepancy compared to the actual database state.
- **Constraint:** Any future **Critical Domain Events** (e.g., billing triggers, distributed saga steps, critical state transitions relying on eventual consistency) **MUST NOT** use this pattern. If such events are introduced, we must revisit this decision and implement a strict consistency mechanism (such as a Transactional Outbox).
