# ADR-007: Extract Agent Configuration via Functional Options

**Status:** Accepted  
**Date:** 2026-02-14

## Context

The `agent` struct in `internal/agent` acts as the core orchestration facade for the system. Over time, it developed "God Object" and "Fat Constructor" anti-patterns:

1.  **Fat Constructor:** The `New()` function grew to accept over 7 positional arguments, making it difficult to maintain and prone to breaking changes whenever a new dependency was introduced.
2.  **State Conflation:** The `agent` struct held approximately 14 fields. Many of these (e.g., `registry`, `sm` [SecurityManager], `summarizer`, and `registerInternal`) were only utilized during the object graph assembly (wiring) phase.
3.  **Resource Inefficiency:** Storing build-time dependencies on the runtime struct violated the Single Responsibility Principle (SRP) and resulted in a larger-than-necessary memory footprint for every agent instance.

## Decision

We have refactored the initialization of the `agent` package to use the idiomatic Go Functional Options pattern.

1.  **Introduction of `agentConfig`:** An unexported `agentConfig` struct was created to hold all initialization-only dependencies and configuration parameters.
2.  **Functional Options API:** We defined the `Option` type as `type Option func(*agentConfig)` and implemented exported functions (e.g., `WithRegistry`, `WithSecurityManager`) to populate the configuration.
3.  **Runtime Facade:** The `agent` struct was reduced to a thin runtime facade. It now only retains fields that are strictly necessary for its operation after initialization (e.g., `engine`, `ctxManager`, `events`, `tracker`).
4.  **Constructor Simplification:** The `New()` constructor signature was changed to accept only mandatory positional arguments (the LLM gateway and event bus) followed by a variadic list of options: `New(client, bus, providerName, opts ...Option)`.

## Consequences

### Positive

*   **Single Responsibility Principle (SRP):** There is now a clear distinction between the "Assembly" phase (wiring dependencies) and the "Runtime" phase (executing chat orchestration).
*   **Reduced Memory Footprint:** The `agent` struct is slimmer, as it no longer retains pointers to services only needed during setup.
*   **API Stability:** New optional dependencies can be added via new functional options without introducing breaking changes to the `New()` constructor signature.
*   **Flexible Testing:** Tests can granularly provide only the dependencies they need for a specific scenario, improving the maintainability of the test suite.

### Negative

*   **Increased Boilerplate:** The introduction of `options.go` adds a small amount of initial boilerplate code to define the options and the config struct.
