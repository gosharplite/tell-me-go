# ADR-003: Domain Decomposition of Chatter Orchestrator

## Status
Accepted

### Implementation Status (2025)
- **Phase 1: Elimination of ChatterParams:** COMPLETED (See [ADR-004: Elimination of ChatterParams God Object](./2024-08-chatterparams-elimination.md))
- **Phase 2: Facade Pattern & Focused Services:** IN PROGRESS (ContextPreparationService, ExecutionOrchestrator, etc. are being extracted).

## Context
The current orchestration layer centered around the `Chatter` orchestrator and its corresponding `ChatterParams` struct has evolved into a "God Object." 

Specifically:
- **Dependency Bloat:** `ChatterParams` currently accumulates 14+ dependencies (including Context, ConfigLoader, LLMGateway, HistoryManager, ToolRegistry, SecurityManager, CostTracker, etc.).
- **High Coupling:** The monolithic nature of the orchestrator makes it difficult to change one part of the system without affecting others.
- **Violation of SRP:** The orchestrator is responsible for context preparation, tool execution, LLM coordination, and monitoring/cost tracking simultaneously.
- **Brittle Testing:** Unit testing the orchestrator requires mocking up to 14 distinct interfaces, making tests difficult to write, read, and maintain.
- **DI Container Complexity:** The composition root in `internal/app/container.go` has become bloated and difficult to manage due to these complex dependency chains.

## Decision
We will decompose the monolithic `Chatter` orchestrator into a **Facade pattern** supported by distinct, focused domain services.

### New Architecture
The orchestration layer will be split into the following specialized services, coordinated by a `ChatterFacade`:

1.  **`ContextPreparationService`**: Responsible for gathering and preparing the context for the LLM call (e.g., history retrieval, prompt template application).
2.  **`ExecutionOrchestrator`**: Manages the loop of LLM calls and subsequent tool executions.
3.  **`LLMCoordinatorService`**: Handles direct interactions with LLM providers, including gateway logic and provider-specific transformations.
4.  **`MonitoringService`**: A cross-cutting service for metrics collection, cost tracking, and logging.

### Architectural Rules
To ensure the long-term health and scalability of this new structure, the following rules apply:

1.  **Consumer-Defined Interfaces:** Interfaces must be defined in the package that *consumes* them (the facade or orchestration package), not where they are implemented. This enforces true decoupling and allows implementations to remain unaware of their consumers.
2.  **Interface Segregation:** Interfaces must remain small and focused. Specifically, setup and lifecycle methods (such as `RegisterTool`) must be segregated from runtime execution methods to avoid forcing clients to depend on methods they do not use.
3.  **Functional Options Pattern:** Both the `ChatterFacade` and the newly decomposed services must utilize the **Functional Options pattern** for initialization. This replaces the use of large parameter structs and provides a more flexible, extensible, and readable way to configure services.

## Consequences

### Positive
- **Improved Testability:** Unit tests for individual services will only require 3-4 mocks, making them faster to write and more resilient to change.
- **Maintainability:** Clear separation of concerns allows for easier refactoring and independent scaling of different domain areas.
- **Modularity:** The system becomes more modular, facilitating the addition of new features or replacement of existing implementations without side effects.

### Negative / Effort
- **Refactoring Overhead:** Significant effort is required to refactor the dependency injection container (`container.go`) and the existing orchestration logic.
- **Test Updates:** Numerous existing integration and unit tests will need to be updated to reflect the new service boundaries and initialization patterns.
- **Initial Complexity:** Introducing more services increases the number of internal boundaries, which requires careful documentation and clear naming conventions to manage.
