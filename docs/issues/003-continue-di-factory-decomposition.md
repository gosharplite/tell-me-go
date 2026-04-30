# Continue `infrastructure/di` Bootstrapper decomposition

## Category
[REFACTOR] — Continue In-Flight Decomposition

## Context
The `Bootstrapper` in `internal/infrastructure/di/container.go` is being progressively decomposed into focused factories. Three factories already exist:

- `session_factory.go` — `defaultSessionFactory.BuildSession`
- `telemetry_factory.go` — `defaultTelemetryFactory.BuildTelemetry`
- `toolchain_factory.go` — `defaultToolchainFactory.BuildRegistry`

However, **5 distinct concerns remain inlined as `Bootstrapper` methods**, leaving the container at ~430 LOC with 24 internal package imports. This issue completes the decomposition by extracting the remaining concerns into matching factory files, following the established pattern.

## Evidence

`Bootstrapper` currently exposes these methods that mix construction concerns:

| Method | Concern | Target File |
|---|---|---|
| `GetUIRenderer` | UI construction | `ui_factory.go` (NEW) |
| `GetHistoryRenderer` | UI construction | `ui_factory.go` (NEW) |
| `GetHistoryBrowser` (+ `tuiHistoryBrowser` type) | UI construction | `ui_factory.go` (NEW) |
| `GetHistoryManager` | History wiring | `history_factory.go` (NEW) |
| `GetUnifiedHistoryProvider` | History wiring | `history_factory.go` (NEW) |
| `buildHistoryManager` | History wiring | `history_factory.go` (NEW) |
| `GetChatService` | Chat composition | `chat_factory.go` (NEW) |
| `GetAgentFactory` | Chat composition | `chat_factory.go` (NEW) |
| `GetSuggestionService` | Application service wiring | `suggestion_factory.go` (NEW) |

After this refactor, `container.go` should hold only:
- `Bootstrapper` struct definition
- `NewBootstrapper` constructor (composition root for all factories)
- `BuildSessionDependencies` (the orchestration entry point)
- `FinalizeSession` (the orchestration teardown)
- `sessionDeps` struct + getters (data holder, no construction logic)
- `lazyLLMProxy` (lazy initialization helper)

Pure construction logic moves out.

## Current vs. Scalable

**Current `container.go` skeleton (430 LOC):**
```go
func (b *Bootstrapper) BuildSessionDependencies(...)
func (b *Bootstrapper) buildHistoryManager(...)         // history concern
func (b *Bootstrapper) GetAgentFactory()                // chat concern
func (b *Bootstrapper) FinalizeSession(...)
func (b *Bootstrapper) GetHistoryManager(...)           // history concern
func (b *Bootstrapper) GetUnifiedHistoryProvider(...)   // history concern
func (b *Bootstrapper) GetSuggestionService(...)        // app concern
func (b *Bootstrapper) GetUIRenderer()                  // ui concern
func (b *Bootstrapper) GetHistoryRenderer()             // ui concern
func (b *Bootstrapper) GetHistoryBrowser()              // ui concern
func (b *Bootstrapper) GetChatService()                 // chat concern
type tuiHistoryBrowser struct{...}                      // ui concern
```

**Scalable:**
```go
// container.go (~150 LOC)
type Bootstrapper struct {
    sessionFactory   sessionFactory
    telemetryFactory telemetryFactory
    toolchainFactory toolchainFactory
    historyFactory   historyFactory   // NEW
    uiFactory        uiFactory         // NEW
    chatFactory      chatFactory       // NEW
    suggestionFactory suggestionFactory // NEW
    // ... shared deps
}

func (b *Bootstrapper) GetUIRenderer() ports.UIRenderer {
    return b.uiFactory.UIRenderer()  // delegate
}
```

This matches the existing pattern (`session_factory.go`, `telemetry_factory.go`, `toolchain_factory.go`) — no new architectural concept introduced.

## Proposed Action

1. **Extract `history_factory.go`** — easiest first step, only 3 methods, well-isolated.
2. **Extract `ui_factory.go`** — moves the `tuiHistoryBrowser` private type along with it.
3. **Extract `chat_factory.go`** — `GetAgentFactory` and `GetChatService` together.
4. **Extract `suggestion_factory.go`** — single method, trivial.
5. After each extraction: run tests, run `verify_architecture`, commit separately.
6. Update `NewBootstrapper` to wire the new factories (composition root pattern).

## Acceptance Criteria

- [ ] `container.go` LOC drops from ~430 to ≤200.
- [ ] `Bootstrapper` struct delegates all `Get*` methods to factories (one-line bodies).
- [ ] Internal package import count of `container.go` drops below 15.
- [ ] All `*_factory.go` files follow the same naming convention as the 3 existing ones.
- [ ] All existing tests in `container_test.go`, `factory_error_test.go`, `lazy_test.go` pass unchanged.
- [ ] `verify_architecture` clean.
- [ ] No new public API surface added; `cli.Bootstrapper` interface unchanged.

## Out of Scope

- Refactoring `BuildSessionDependencies` or `FinalizeSession` (these are orchestration, not construction).
- Changing the `lazyLLMProxy` mechanism.
- Splitting `sessionDeps` (it is a coherent data holder).
- Introducing a DI library (Wire, Fx). Manual factories remain the chosen approach.

## Effort
**Small** (2–3 days). Pattern is established; mechanical extraction with strong test safety net (existing test file already has 1000+ lines of coverage).

## ADR Required
No. This continues the trajectory established by `session_factory.go`, `telemetry_factory.go`, and `toolchain_factory.go` — no new architectural decision.

## References
- Existing pattern in `internal/infrastructure/di/session_factory.go`, `telemetry_factory.go`, `toolchain_factory.go`
