# ADR-061: Per-Turn Turn Lifecycle (Constructor-Is-The-Reset)

**Status:** Accepted (decision committed; implementation lands in #1327)
**Date:** 2026-08
**Related:** [Issue #1327](https://github.com/gosharplite/tell-me-go/issues/1327), [ADR-059](2026-08-overflow-signal-into-prepare.md)

## Context

The "forgot to reset" bug class. `Turn` is a long-lived mutable object reused across the turns of a multi-turn `Run`; `prepareNextTurn` (engine.go) was the **sole manual reset site**, and its reset list had grown to **nine fields** — `Index`, `CurrentTurns`, `Phase`, `RetryCount`, `RecoveryFromOverflow`, `Response`, `ToolResponse`, `HasToolCalls`, `ToolReasons`. Every new `TurnState` field is one more field that must be remembered in the reset path.

ADR-059 is the proof the discipline was already a real bug source: it had to add an explicit `RecoveryFromOverflow = false` reset (engine.go) precisely because a forgotten reset leaks state cross-turn — `Turn` is reused across turns, so a flag set during one turn's recovery would bleed into the next. The reset list only grows as `TurnState` grows; the failure mode is not "we forgot once" but "the list is unmaintainable by construction".

The 2026-08 full-repo architecture review identified this as a structural defect: a long-lived mutable `Turn` reused across turns, with `prepareNextTurn` as the sole manual reset site whose reset list had grown to nine fields; the discipline is inherently fragile — each new `TurnState` field silently widens the reset obligation, and the ADR-059 `RecoveryFromOverflow` reset (added to fix a real cross-turn leak) demonstrates the bug class already produced production bugs.

## Decision

**Option 1 — constructor-is-the-reset.** `Run` allocates a **fresh `Turn` per iteration** via `CreateTurn` (engine.go); `prepareNextTurn` and its reset list are **deleted**; every `TurnState` field starts zeroed structurally. The reset obligation disappears by construction: there is nothing to remember because there is nothing reused.

### D1 — Fresh `Turn` per iteration; stop condition evaluated pre-allocation

- `Run` (`internal/agent/orchestrator/engine.go`) allocates the first turn (`CreateTurn(0, startTime, sessionID)`) and then a fresh `Turn` per iteration (`CreateTurn(Turn.Index+1, startTime, sessionID)`); `prepareNextTurn` is deleted.
- **Critical ordering:** the stop condition is evaluated on the **completed** turn **before** the fresh allocation — `shouldStopRunning(Turn)` (`!Turn.State.HasToolCalls || Turn.Stop`) runs before `CreateTurn` — preserving the `handleLoopBreak` `HasToolCalls=true` "continue to a next generation turn" handshake. A fresh turn would read `HasToolCalls=false` and terminate the run, so the ordering is load-bearing, not incidental.
- `CreateTurn` re-reads `e.config.Load()` per allocation, so `Model`, `Mode`, `ProviderName`, and `CostTracker` refresh mid-`Run` from live config (hot-reload improvement); `StartTime` stays Run-fixed (passed unchanged from `Run`'s parameter).

### D2 — Session-scoped accumulators move to `loopDetector`

The three session-scoped accumulators — `ToolCallCount`, `RecentResponseHashes`, `HasSeenRateLimit` — move out of `TurnState` (all were `json:"-"`, so **no persistence impact**) into an unexported `loopDetector` type (middleware.go):

- `withLoopDetector` becomes an `*Engine` method; `detectLoop` / `isDuplicateResponse` become `*loopDetector` methods.
- `RecoveryStep` (engine_phases.go) reads/writes the rate-limit flag via `Turn.LoopDetector` nil-safely (`hasSeenRateLimit` / `recordRateLimit` are nil-safe, mirroring the ADR-059 nil-safe read pattern).

### D3 — Per-Run detector instances

Refinement forced by `TestTurnEngine_Concurrency_TaskCost` (tests/integration/agent/concurrency_stress_test.go:268), which runs **20 concurrent `Run`s on one Engine** and must stay race-free under `-race`: `Run` allocates a fresh detector per `Run` (`newLoopDetector()`) and installs it on every `Turn` via `Turn.LoopDetector`; the Engine field (`Engine.loopDetector`) remains the fallback for direct-`ExecuteTurn` paths and bare-`Turn` tests. This reproduces pre-T6 per-Run accumulator semantics exactly — goroutine-local, per-chat — and keeps concurrent Runs on a shared Engine isolated.

### D4 — Run-scoped cumulative `TaskCost`

The Run-scoped cumulative `TaskCost` (pre-T6: accumulated on the reused `Turn`) moves to `loopDetector.taskCost`; `withMetrics` accumulates there and publishes `Turn.State.TaskCost` per turn; bare-Turn tests (no `Turn.LoopDetector`) fall back to per-Turn accumulation.

## Consequences

**Positive**

- **Structural elimination of the reset discipline** — the "forgot to reset" bug class is gone by construction, not by vigilance.
- **Hot-reload improvement:** per-turn `Model` / `Mode` / `ProviderName` / `CostTracker` now refresh mid-`Run` from live config — the fresh `CreateTurn` re-reads `e.config.Load()`.
- `Turn.StartTime` stays Run-fixed — unchanged semantics.
- ADR-059's `RecoveryFromOverflow` **per-turn** scope is preserved structurally — a fresh `Turn` starts the flag `false` every turn.

**Negative**

- Two-turn regression tests require explicit coverage (added in `internal/agent/orchestrator/turn_lifecycle_test.go`).
- The `TaskCost` accumulator now lives on a type named `loopDetector` — it shares the per-Run lifecycle with the loop accumulators (documented on the type); the name is a historical artifact of the type's origin, not a statement about cost accounting.

**Note:** the `Turn` domain-model entity is unchanged — all moved fields were runtime-only (`json:"-"`); **no domain-model edits**.

## Alternatives considered

- **Option 2 — explicit `(*Turn).Reset()`.** Still requires remembering every runtime-only field in one method; single-owner discipline reduces but does **not** eliminate the bug class — a new `TurnState` field added without a corresponding reset line re-opens the exact ADR-059 leak. Rejected.

## References

- [#1327](https://github.com/gosharplite/tell-me-go/issues/1327) — per-turn Turn lifecycle refactor.
- [ADR-059](2026-08-overflow-signal-into-prepare.md) — the overflow-signal precedent; its `RecoveryFromOverflow` reset was the ADR-061 motivating bug, and its RESET mechanism is amended by this ADR.
- `TestTurnEngine_Concurrency_TaskCost` race constraint — `tests/integration/agent/concurrency_stress_test.go:268` (20 concurrent `Run`s on one Engine, `numConcurrent := 20`).
- Catalog pins re-anchored at `middleware.go:213` / `middleware.go:311` — `docs/architect/INTENTIONAL_NON_FIXES.md` (detectLoop `json.Marshal` entry and `injectSyntheticLoopFeedback` return-nil entry).
