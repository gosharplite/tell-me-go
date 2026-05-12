# ADR-0001: DI Composition Root — Sub-Provider Decomposition

## Status

Accepted (2026-05-12)

## Context

The `Bootstrapper` in `internal/infrastructure/di/container.go` acts as the Composition Root, wiring all
system components for a chat session. As integrations grew, `BuildSessionDependencies` became a procedural
God-method that mixed orchestration (sequencing), composition (field assignment), and lifecycle management
(lazy initialization) into a single ~80-line method.

The `sessionDeps` struct compounded this by holding 18+ data fields alongside `sync.Once`-guarded lazy
initialization for the LLM client and tool registry.

## Decision

Decompose the `Bootstrapper` into 8 specialized sub-providers, each with a single responsibility:

| Sub-Provider | File | Responsibility |
|---|---|---|
| `sessionFactory` | `session_factory.go` | Session state, security setup, rotation |
| `toolchainFactory` | `toolchain_factory.go` | Tool registry wiring and policy registration |
| `telemetryFactory` | `telemetry_factory.go` | Pricing data, cost tracking, turns logging |
| `historyFactory` | `history_factory.go` | History manager and unified provider |
| `healthFactory` | `health_factory.go` | Health checker assembly (LLM, persistence, toolchain) |
| `uiFactory` | `ui_factory.go` | UI renderer, history browser, history renderer |
| `chatFactory` | `chat_factory.go` | Agent factory and chat service |
| `suggestionFactory` | `suggestion_factory.go` | Suggestion service |

Extract lazy initialization from `sessionDeps` into standalone `LazyClient` and `LazyRegistry` types,
each with their own `sync.Once`-guarded factory.

Remove closure-based adapter wrappers from `NewBootstrapper`. Sub-factory constructors accept function
fields directly rather than closures that close over the half-constructed `Bootstrapper`.

## Consequences

### Positive
- Each sub-factory is independently testable via its concrete type (`default*Factory`)
- `BuildSessionDependencies` is a pure orchestration method: 8 sequential delegate calls, no inline wiring
- `sessionDeps` is a pure data struct with no lifecycle logic
- `LazyClient` implements `llm.ExtendedClient` directly, eliminating the `lazyLLMProxy` intermediary
- No circular dependencies exist between sub-factories
- The `cli.Bootstrapper` interface (consumer contract) is unchanged

### Negative
- 3 additional files in the `di` package (`lazy_client.go`, `lazy_registry.go`, `health_factory.go`)
- Tests that mutate `Bootstrapper` function fields (`RegisterAllTools`, `RotateSession`, etc.) after
  construction must also re-initialize the affected sub-factory, since values are captured at construction
  time rather than through closures

### Neutral
- The `Bootstrapper` still has 17 exported fields (HomeDir, SM, Version, etc.). Further decomposition
  into a `BootstrapperConfig` value object is deferred to a future ADR.

## Alternatives Considered

1. **Keep the God-object pattern.** Rejected: testing individual wiring domains required standing up
   the entire dependency graph.
2. **Use a DI framework (e.g., Wire, Dingo).** Rejected: adds code generation complexity; the
   manual approach is sufficient given the 8-factory structure.
3. **Move sub-factories into separate packages.** Rejected: the interfaces are package-private
   and tightly coupled to the `di` package's parameter types (`toolchainParams`, etc.). Exporting
   them would create unnecessary public API surface.
