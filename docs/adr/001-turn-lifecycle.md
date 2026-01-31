# ADR 001: Turn Lifecycle Orchestration

## Status
Accepted

## Context
The `TurnEngine.Run` method was a "God Method" that managed the entire lifecycle of a multi-turn conversation, including orchestration logic, UI streaming, logging, and persistence. This made it difficult to test, maintain, and extend.

## Decision
We refactored `TurnEngine` to apply Separation of Concerns by:
1. Extracting the turn lifecycle into a private `executeTurn` method.
2. Introducing a `TurnState` struct to carry data between phases.
3. Decoupling side effects (UI, Logging, Persistence) using a `TurnHooks` struct.
4. Explicitly labeling the phases: **Think** (LLM Generation), **Observe** (Persistence), and **Act** (Tool Execution).

## Consequences
- **Readability**: The `Run` loop is now high-level and easy to follow.
- **Testability**: Individual phases and the atomic `executeTurn` can be tested in isolation.
- **Modularity**: UI and Logging logic are completely separated from the orchestration engine.
- **Error Handling**: Errors are consistently wrapped and propagated, improving system reliability.
