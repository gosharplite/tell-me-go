# ADR-059: Overflow Signal into Prepare (Effective-Budget Reduction on Recovery)

**Status:** Accepted (decision committed; implementation lands in #1320)
**Date:** 2026-08-10
**Related:** [Issue #1320](https://github.com/gosharplite/tell-me-go/issues/1320)

## Context

Post-F1 (ADR-057, `docs/adr/2026-08-remove-context-window-cache.md`), `Prepare` is estimate-driven: the gatekeeper summarises only when its local estimate exceeds the threshold. The provider-vs-estimator divergence — estimation imprecision, multimodal `InlineData` blob bytes not counted by the estimator, and a provider window smaller than the configured `ContextWindow` — means the re-assembled window is identical to the one that just overflowed. With the single-refinement policy (`RetryCount == 1`), the second overflow kills the turn.

The current flow: `RecoveryStep` (`internal/agent/orchestrator/engine_phases.go`, `LLMErrorContextOverflow` branch, `RetryCount == 0` → `NextPhase: PhaseRefining`) → `ContextRefiner.Process` → `Turn.CtxManager.Prepare(ctx, Turn.Index)` (`engine_phases.go:58` — the **only** production `Prepare` caller) → `TokenGatekeeper.Transform` (`internal/agent/session/context/gatekeeper.go`: Stage 1 `handleSafetyPressure` 90% threshold, Stage 2 `validateHardLimits` vs. `hardLimitPolicy{MaxTokens, SystemContextBuffer=1000}.effectiveLimit()`).

Two failure cases must be handled:

- **Case A** — pass 1 did **not** summarise (estimate under the 90% threshold, provider overflowed anyway): a threshold tweak *could* in principle trigger pass-1 summarization, but only by distorting the normal-turn budget for every turn, not just recovery.
- **Case B** — pass 1 **did** summarise and persisted, but the provider still overflowed: the reloaded (summarised) history sits far below any threshold, so a threshold tweak alone could **never** trigger a second summarization. Only a forced summarization can shrink the window further.

Because the single-refinement policy means the recovery `Prepare` is the **last chance** to shrink the window, it must succeed in shrinking even when pass 1 already summarised.

## Decision

**Overflow signal into `Prepare` / effective-budget reduction on recovery** — the "Option C" from #1317 implemented as a deliberate feature, explicitly **NOT** a cache patch (ADR-057 finality). The gatekeeper learns that the current `Prepare` is a recovery from a provider context overflow and responds with two levers: forced recovery summarization and a reduced hard limit.

### D1 — The signal and its flow (end-to-end)

- `TurnState` (`internal/agent/orchestrator/engine_types.go`) gains `RecoveryFromOverflow bool` (`json:"-"`).
- `RecoveryStep.Process` (`internal/agent/orchestrator/engine_phases.go`, `LLMErrorContextOverflow` branch, `RetryCount == 0`): set `Turn.State.RecoveryFromOverflow = true` immediately before returning `NextPhase: PhaseRefining`. The `RetryCount >= 1` fall-through (turn dies on second overflow) is unchanged.
- `ContextRefiner.Process`: pass the signal via a new variadic option — `Turn.CtxManager.Prepare(ctx, Turn.Index, sessctx.WithOverflowRecovery())` when the flag is set, else the existing bare call. This requires adding the `internal/agent/session/context` import to `engine_phases.go`. The bare call form stays source-compatible (all existing callers/tests compile unchanged).
- `Manager.Prepare` (`internal/agent/session/context/manager.go`) gains `opts ...PrepareOption` where `type PrepareOption func(*ContextRequest)` and `WithOverflowRecovery() PrepareOption` sets a new `ContextRequest.RecoveryFromOverflow bool` field (`contracts.go`, documented). The signal never leaves the request scope — **NO** Manager-level state, no persistence, no new config keys (the recovery margin is derived from the existing `SystemContextBuffer`).
- **RESET (amended 2026-08, ADR-061/issue #1327):** per ADR-061, `prepareNextTurn` (`internal/agent/orchestrator/engine.go`) no longer exists — the reset is **structural**: `Run` allocates a fresh `Turn` per iteration, so `Turn.State.RecoveryFromOverflow` starts `false` each turn. Signal behavior unchanged. (Original: `prepareNextTurn`, where `RetryCount` is already reset, set `Turn.State.RecoveryFromOverflow = false` — preventing cross-turn leakage since `Turn` was reused across turns.)

### D2 — The effective-budget mechanism (two levers, both owned by the gatekeeper)

- **LEVER 1 — FORCED RECOVERY SUMMARIZATION (Stage 1).** In `handleSafetyPressure`, trigger summarization when `req.RecoveryFromOverflow` is true EVEN IF the estimate is under the 90% threshold: `overThreshold || req.RecoveryFromOverflow`. Reason string passed to `SummarizationRequired`: "recovery from provider context overflow: forcing summarization to shrink the window" (distinct from the 90% pressure reason). All existing guards inherited unchanged: `MaintenanceBlocked`/history-<10 degrade to "tokens unchanged" (no error), `SummarizationAttempted` prevents double-summarization within the recovery run, blocking errors still abort. This handles BOTH failure cases: Case A (pass 1 didn't summarise — estimate under threshold) and Case B (pass 1 did summarise and persisted, but the provider still overflowed — the reloaded history is under any threshold, so a threshold tweak alone could never trigger; forced summarization can).
- **LEVER 2 — REDUCED HARD LIMIT (Stage 2).** `hardLimitPolicy` (`gatekeeper.go`) gains a `Recovery bool` field; `effectiveLimit()` doubles the reservation on recovery via a named constant `recoveryBufferMultiplier = 2` (documented: "the recovery margin reserves the system buffer a second time — an honest demand for a strictly smaller window when the estimator has demonstrably under-counted"). Result: recovery effective limit = `MaxTokens − 2 × min(10% MaxTokens, SystemContextBuffer)` (e.g. 20000 → 18000). Defense-in-depth: if forced summarization was impossible (all pinned / too short) or insufficient, the recovery `Prepare` fails fast with `ErrContextLimitExceeded` at the tighter ceiling instead of burning an inference call that will die anyway. A successful shrink still must clear the reduced ceiling before inference proceeds.
- **UNCHANGED (explicitly):** normal (non-recovery) `Prepare` behavior is byte-identical — threshold, hard limit, events, and the `hardLimitPolicy` math for `Recovery == false` all untouched; `finalContextValidator` (`internal/agent/session/context/transformers.go`) keeps its absolute-ceiling check (the recovery budget is owned exclusively by the gatekeeper — one policy owner); `WarningInjector`, `TransientMerger`, pruner all untouched; `RecoveryStep`'s single-refinement policy untouched; no cache tokens anywhere (`make verify-no-context-window-cache` stays green).

### D3 — Why the alternatives were rejected

- **A) status quo** — the bug this issue exists to fix.
- **B) overflow-aware cache invalidation** — rejected by ADR-057's own alternatives (Option C there) and by cache-finality.
- **C) threshold-lowering only** — insufficient for Case B (post-summarization estimate sits far below any threshold).
- **D) provider-reported usage** — not available/standardized across providers, no domain port exists, out of scope (noted as future enhancement only).
- **E) persistent recovery state on the Manager** — rejected: cross-turn mutable state on a shared component, concurrency risk; the signal belongs to the Turn lifecycle where `RetryCount` already lives.

### D4 — Related-record requirements to cite

- The F2 token-walk finding (ADR-058: write-only `TokenCount`, ≤1% at the calibrated fixture) as the perf non-concern for an extra summarization path.
- The #1320-authored multimodal requirement: if #1321's lazy-hydration/COW candidates land, the estimator must still count multimodal payloads accurately — the recovery signal is a safety net, not a substitute for estimation accuracy.
- `docs/architect/INTENTIONAL_NON_FIXES.md` explicitly NOT a candidate (the issue's Reachability section).
- The config workaround (`MAX_HISTORY_TOKENS`/`ContextWindow`) remains the operator lever, which is why no new config keys are added.

### D5 — Acceptance checklist (as implemented; enforced by grep + tests)

Presence greps (implemented reality — 6 production files, 8 code matches for the signal):

- `RecoveryFromOverflow` — `engine_types.go` (field), `engine_phases.go` ×2 (`RecoveryStep` set + `ContextRefiner` read), `engine.go` (`prepareNextTurn` reset), `contracts.go` (`ContextRequest` field), `manager.go` (`WithOverflowRecovery` setter), `gatekeeper.go` ×2 (Lever 1 + Lever 2 reads).
- `WithOverflowRecovery` — `manager.go` (definition) + `engine_phases.go` (call site).
- `PrepareOption` — `manager.go` (type + variadic param) + `engine_phases.go` (`[]sessctx.PrepareOption` — the pinned `ContextRefiner` snippet).
- `ContextRequest.RecoveryFromOverflow` — `contracts.go`.
- `recoveryBufferMultiplier` — `gatekeeper.go` (const + use).

Implementation notes:

- Lever 2 reads the signal nil-safely as `req != nil && req.RecoveryFromOverflow` — required because the pre-existing `TestTokenGatekeeper_ValidateHardLimits` invokes `validateHardLimits` with a nil request (nil → `Recovery == false` → byte-identical normal-path behavior).
- Doc comments in `internal/agent/session/context/` must not contain the word "persisted" (ADR-057 Tier-2 die-grep in `make verify-no-context-window-cache`); use "stored".

Absence of new cache tokens (existing `make verify-no-context-window-cache` covers).

Test expectations (landed in Task 3):

- `internal/agent/session/context/gatekeeper_recovery_test.go` — `TestTokenGatekeeper_RecoveryFromOverflow_ForcesSummarization` (recovery with under-threshold estimate summarises; normal path unaffected), `TestTokenGatekeeper_RecoveryFromOverflow_TooShortHistoryDegradesGracefully` (too-short history degrades to "tokens unchanged"), `TestTokenGatekeeper_RecoveryFromOverflow_ReducedHardLimitFailsFast` (recovery fails fast at the reduced ceiling; exactly one summarizer call).
- `internal/agent/session/context/context_manager_recovery_test.go` — `TestManager_Prepare_RecoveryFromOverflow_ForcesSummarization` (Manager-level, Q4-c shape: fixed `MockTokenCounter` under the 90% threshold on the first `Prepare`, forced summarization on the second with `WithOverflowRecovery()`).
- `internal/agent/orchestrator/recovery_signal_test.go` — `TestRecoveryStep_ContextOverflow_SetsRecoveryFromOverflow`, `TestPrepareNextTurn_ResetsRecoveryFromOverflow`, `TestContextRefiner_PassesRecoverySignalToPrepare` (orchestrator wiring end-to-end).

## Consequences

**Positive**

- The turn survives a provider context overflow via a strictly-smaller window: forced recovery summarization shrinks the window even when pass 1 already summarised (Case B) or never summarised (Case A).
- Fail-fast on irrecoverable divergence: if no summarizable block exists (all pinned / too short) or summarization is insufficient, the recovery `Prepare` fails with `ErrContextLimitExceeded` at the tighter ceiling instead of burning an inference call that will die anyway.
- Defense-in-depth via two independent levers (forced summarization + reduced hard limit), both owned by the single policy owner (the gatekeeper) — no new Manager state, no persistence, no new config keys.
- Normal (non-recovery) `Prepare` behavior is byte-identical — zero regression surface for the common path.

**Negative**

- A second LLM-backed summarizer call plus a second persist on the recovery path — the cost model ADR-057 already accepted (one extra summarization per turn at most, bounded by `RecoveryStep`'s single-refinement policy).
- If no summarizable block exists, the turn still dies on the second overflow — unchanged failure semantics; the recovery signal does not create summarization material where none exists.
- The recovery margin is derived from the existing `SystemContextBuffer` — the operator lever (`MAX_HISTORY_TOKENS`/`ContextWindow`) remains the way to influence its size.

## Alternatives considered

- **A — status quo.** Rejected: the bug this issue exists to fix.
- **B — overflow-aware cache invalidation.** Rejected: ADR-057's own alternatives (Option C there) and cache-finality — the cache is gone and not coming back.
- **C — threshold-lowering only.** Rejected: insufficient for Case B — after pass 1 summarised and persisted, the reloaded history sits far below any threshold, so a threshold tweak alone can never trigger a second summarization.
- **D — provider-reported usage.** Rejected: not available/standardized across providers, no domain port exists, out of scope; noted as a future enhancement only.
- **E — persistent recovery state on the Manager.** Rejected: cross-turn mutable state on a shared component is a concurrency risk; the signal belongs to the Turn lifecycle where `RetryCount` already lives.

## References

- [#1317](https://github.com/gosharplite/tell-me-go/issues/1317) — closed; Option C source.
- [#1319](https://github.com/gosharplite/tell-me-go/issues/1319) — closed/merged: ADR-057 + context window cache removal.
- [#1320](https://github.com/gosharplite/tell-me-go/issues/1320) — open: this issue.
- [#1321](https://github.com/gosharplite/tell-me-go/issues/1321) — open: F2 evaluation (candidate 3 terminal, ADR-058).
- [ADR-057](2026-08-remove-context-window-cache.md) — F1: cache removal; overflow residual tracked here.
- [ADR-058](2026-08-getwindow-clone-evaluation.md) — F2: token-walk finding (write-only `TokenCount`, ≤1% at the calibrated fixture).
- [docs/architect/1321-f1-baseline.md](../architect/1321-f1-baseline.md) — F1 baseline, real-counter instrument.
