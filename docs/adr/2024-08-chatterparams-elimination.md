# ADR-004: Elimination of ChatterParams God Object

## Status
Accepted

## Context
As identified in **ADR-003: Domain Decomposition Strategy**, the `Chatter` orchestrator and its associated `ChatterParams` struct had evolved into a "God Object." 

Specifically:
- **Dependency Bloat:** `ChatterParams` accumulated 14+ disparate dependencies, including `Context`, `ConfigLoader`, `LLMGateway`, `HistoryManager`, `ToolRegistry`, `SecurityManager`, `CostTracker`, and others.
- **Violation of ISP:** Clients were forced to provide or depend on a massive structure even if they only required a subset of its capabilities.
- **Test Fragility:** Unit testing the orchestrator required mocking an excessive number of interfaces, making the test suite brittle and difficult to maintain.
- **Composition Root Complexity:** The DI container in `internal/infrastructure/di/container.go` became a complex web of manual wiring for this single large object.

## Decision
We have eliminated the `ChatterParams` struct and refactored the initialization of the `Chatter` orchestrator using a combination of focused configuration and behavior-based dependency injection.

### Key Structural Changes:
1.  **Separation of Data and Behavior:** `ChatterParams` was replaced by:
    -   `ChatterConfig`: A clean struct containing only primitives and configuration settings.
    -   `SessionDependencies`: A focused struct containing the core behavioral interfaces required for a session.
2.  **Factory Pattern for Object Graph Construction:**
    -   The deep initialization logic for the `Chatter` orchestrator was moved from the general `internal/infrastructure/di/container.go` to a specialized factory: `internal/infrastructure/factory/chatter.go`.
    -   The `Container` now delegates the complexity of building the `Chatter` object graph to this factory.
3.  **Refined Orchestrator Initialization:**
    -   The `Chatter` orchestrator now accepts structured dependencies that align with its internal components (e.g., `ContextPreparationService`, `ExecutionOrchestrator`, `MonitoringService`), adhering to the **Facade Pattern** defined in ADR-003.

## Consequences

### Positive
-   **Improved Maintainability:** The DI container is now significantly cleaner and more modular.
-   **Enhanced Testability:** Individual components of the orchestrator can be tested in isolation with minimal mocking.
-   **Strict Adherence to SRP/ISP:** Naming and structure now clearly reflect the responsibilities of each component.
-   **Scalable DI:** New dependencies can be added to specific sub-services without bloating a global parameter object.

### Negative / Effort
-   **Increased File Count:** Moving logic to specialized factories increases the number of files in the `infrastructure` layer.
-   **Initial Refactoring Cost:** Significant changes were required across the DI container, orchestrator, and test suites.
