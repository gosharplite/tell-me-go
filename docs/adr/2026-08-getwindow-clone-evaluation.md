# ADR-058: Accept the Per-Round GetWindow Deep Clone (F2 Evaluation — Candidate 3)

**Status:** Accepted
**Date:** 2026-08-10
**Related:** [Issue #1321](https://github.com/gosharplite/tell-me-go/issues/1321)

## Context

After F1 (ADR-057, `docs/adr/2026-08-remove-context-window-cache.md`), the sole remaining per-round O(window) cost in the context path is the **eager load-boundary clone** in `GetWindow`: `loadHistory` → `GetWindow(0,-1)` → per-entry `CloneContent` (`internal/agent/session/context/manager.go:140`, `internal/infrastructure/history/history.go:145-171`). This cost is **allocation-only** — measured at 16864 B/op · 242 allocs/op on the calibrated `ContextRefiner/large` instrument in the zero-discard regime (40 turns < window 200).

Issue #1321 is an **evaluation issue**: success is defined as a *documented decision*, not a landed optimization. The evaluation contract (grilled; recorded in the issue body) fixed the rules: **B/op is the sole acceptance currency**; the real-counter ns/op is a **no-regression guard**; the **full ownership-transfer invariant** (no structural sharing) is non-negotiable; a **-race gate** applies to any prototype; all measurements follow the **baseline-vintage protocol** (vintages referenced, never raw numbers restated across documents).

Two candidates were evaluated: **(i)** a cheaper per-message clone (arena-based `GetWindow`), and **PTC** (prune-then-clone). Two more — the **(ii)/(iv)** structural-sharing family — were parked pending a data-driven justification, and the collector window was stage-2-gated on the PTC prior.

## Decision

**Candidate 3 (accept-with-data) for both evaluated candidates.** The full evaluation chain is recorded in `docs/architect/1321-stage1-prior.md` (stage-1 prior) and `docs/architect/1321-f1-baseline.md` (F1 baseline, real-counter instrument, candidate-(i) measurement — §2, §3, §5). Disposition:

| Candidate | Disposition | Summary (vintages cited in Evidence) |
|---|---|---|
| **(i)** cheaper per-message clone | **REJECTED WITH DATA** | Pre-registered bar X = 4.5% B/op reduction on `ContextRefiner/large` (sub-bracket, decomposition-justified: the recoverable fraction is only ~5.1% of the 16864 B/op baseline). Measured ΔB/op = **+6.5% (regression)** → bar fails; ns/op guard passed (−31.5%); allocs 242→5. Prototype `04a51d5e` reverted (`474a63bc`); no dead code left behind. Root cause: Go size-class rounding on contiguous arena backings (+~1792 B/op) exceeds the per-entry small-alloc tax eliminated; a byte-competitive arena requires unsafe byte-packing, outside project standards. |
| **PTC** (prune-then-clone) | **GATED — terminal candidate 3** | The stage-1 prior is decisive: discard ≈ 0 under the dominant production config (`MaxHistoryTurns = 0` default → `HistoryPruner` structurally absent, `pipeline_factory.go:61`; the four cleaners drop only empty/malformed content; the calibrated bench is a zero-discard regime, 40 < 200). No PTC engineering, no stage-2 measurement substrate, **NO COLLECTOR WINDOW COMMISSIONED** (the collector is stage-2-gated and did not run). |
| **(ii)/(iv)** structural-sharing family | **PARKED, not commissioned** | Re-open condition (prior favorable AND PTC prototypes) recorded as **UNMET** (see Falsifiability/Auditability). |
| Cache re-introduction | **NOT planned** (ADR-057) | Any revisit must consume vintages, never raw numbers (ADR-057 consequences). |

## Evidence (vintage-referencing per protocol)

Full tables live in `docs/architect/1321-f1-baseline.md`; this section cites vintages and key numbers only, per the baseline-vintage protocol.

| Vintage | Identity | `ContextRefiner/large` (mock counter) | Verdict |
|---|---|---|---|
| ADR-057 after — `c887cd0f` | current `CloneContent` | 13355 ns/op · 16864 B/op · 242 allocs/op | decision baseline (recorded in ADR-057) |
| Fresh F1 — `b3775c76` (mock) / `ece4137c` (real counter) | same clone identity (verified: `git log c887cd0f..HEAD -- internal/domain/llm/types.go` empty) | 13279 ns/op · **16864 B/op** · 242 allocs/op | **reproduced within noise** (B/op and allocs bit-identical; ns/op within ±3%) |
| Post-(i) prototype — `04a51d5e` | arena `CloneContentSlice` in `GetWindow` | 9096 ns/op · **17952 B/op** · 5 allocs/op | measured; **rejected** (ΔB/op = +6.5%) |

Supporting data points (also in the baseline doc): `large_real_counter` paired baseline 13398 ns/op (mock 13239, +1.2% — the two O(text) token walks are a small cost at the 4 KB fixture); post-(i) `large_real_counter` 9133 ns/op (−31.8%, guard passed); `happy_path` 1616→1604 ns/op and `with_recovery` 4151→4120 ns/op (small-window arena, no regression); `FullTurnCycle/large` 1729923→884805 ns/op (−48.8%) with B/op 1063265→991142 (−6.8%).

## Findings (the recorded value)

1. **Struct-overhead finding — refines the issue's pre-measurement guess.** The per-round clone costs **211 B/entry** total (16864 B/op ÷ 80 entries), of which **94.9% is irreducible payload** — 200 B/entry (Content struct 96 B + Part struct 88 B + pointer slots 16 B, measured via `unsafe.Sizeof` and confirmed by `pprof -alloc_space`). The recoverable (non-payload) fraction is **≤ ~5.1% (864 B/op)** in safe Go. This **explicitly refines the issue's pre-measurement "~30–50% ceiling" estimate**: the ceiling for any standards-compliant (safe, no-unsafe) optimization is **~5%**, not 30–50%. The decomposition is in `docs/architect/1321-f1-baseline.md` §5.1.
2. **Token-walk finding (recorded; NO new issue — maintainer decision).** `HeuristicTokenCounter.countContentTokens` **writes `content.TokenCount` and never reads it** — a write-only cache (`internal/agent/session/context/token_counter.go:57-58`). The field is persisted as `token_count`, carried on clones, and not invalidated by `AppendParts`/`UpdateTurnContent`. Measured bound via the real-counter instrument: a second-walk cache-read would recover **≤ ~1%** at the 4 KB calibrated fixture (the two walks cost ~1.2% of the round, paired run), scaling with session **text volume**, not turn count. Any future reader of `TokenCount` must implement staleness discipline (invalidation on append/update paths) before relying on it.
3. **Arena trade-off finding (documented reference point).** A fresh-per-call arena consolidates allocations (242→5), cuts ns/op sharply (−31.5% `ContextRefiner/large`, −48.8% `FullTurnCycle/large`), but **increases B/op (+6.5%)** — failing the B/op-sole-currency rule. The cause is allocator size-class rounding on contiguous medium backings (80×88 B = 7040 → 8064; 80×96 B = 7680 → 8064; ≈ +1792 B/op). Any future latency-bound reconsideration should consume this vintage rather than re-measuring.

## Falsifiability / Auditability

- **Stage-1 prior flip conditions** (from `docs/architect/1321-stage1-prior.md` §5) — any one would flip the PTC gate: (1) production config telemetry showing a majority of sessions with `MaxHistoryTurns > 0` **and** session turn counts exceeding the sliding window; (2) a config-stratified production sample showing discard > 0; (3) a shipped product config/example that sets `MAX_HISTORY_TURNS > 0`; (4) cleaner-drop evidence in well-formed sessions.
- **(ii)/(iv) re-open condition: UNMET.** The structural-sharing family is parked unless a favorable prior materializes (PTC prototypes) — i.e. conditions (1)–(4) above. The full ownership-transfer invariant remains in force; no invariant relaxation was made by this evaluation.
- **Collector design (as recorded in the issue contract) — PARKED, not shipped.** If a prior flip condition materializes, the stage-2 collector would measure per-round `windowTurns`/`prunedTurns`/`survivingTurns` plus config-identity via the existing event bus; consent-qualified and disabled-by-default. It was **not commissioned** and did not run.

## Consequences

**Positive**

- **Documented decision = evaluation success.** Issue #1321 resolves to candidate 3 (accept-with-data) for both evaluated candidates, per the ratified contract.
- **Calibrated instruments ship**: the `large_real_counter` benchmark scenario (`internal/agent/orchestrator/engine_phases_bench_test.go:312`) is committed at `ece4137c`; its value scales with session text volume, and it remains the substrate for any future text-volume evaluation.
- **Decomposition and measured findings** recorded (struct overhead, token walk, arena trade-off) — the data behind the decision is auditable.
- **No invariant relaxation; no dead code**: the prototype was reverted in full; `CloneContent` and all other callers are untouched and behaviorally identical.

**Negative**

- The per-round clone allocation cost remains — **~16864 B/op at the 40-turn scenario** (sole remaining per-round O(window) allocation cost in the context path).
- The arena's ns/op win is foregone under the B/op-sole-currency rule.
- No cheap byte-level win exists in safe Go — the realistic optimization ceiling is **~5%**, not the 30–50% the issue initially hypothesized.

## Follow-ups

- **#1320** (open) — overflow residual from ADR-057; unaffected by this record.
- **#1321** (open — this issue) — **closed by this record** (candidate 3 terminal).
- Re-open conditions are recorded in the Decision section and Falsifiability/Auditability (prior flip conditions; (ii)/(iv) re-open condition currently UNMET).

## Alternatives considered

- **(i) arena-based cheaper clone** — the measured candidate; rejected for the B/op regression (+6.5%). See the disposition table and `docs/architect/1321-f1-baseline.md` §5.
- **Unsafe byte-packed arena** — would allocate one exact ~16 KB region (avoiding size-class rounding) and could beat the baseline on B/op; rejected because unsafe memory reinterpretation is outside this project's standards and would complicate the ownership-transfer proof.
- **Pooled arena with retention proof** — rejected by the contract's lifetime constraint: `Prepare`'s output outlives `Prepare` (stored in `TurnState`), there is no round-end signal without a port/orchestration change (forbidden), so only a fresh-per-call arena is compliant.
- **Per-entry micro-optimizations** — bounded by the ~5.1% recoverable ceiling measured here; not pursued as an issue.
- **PTC engineering** — gated by the decisive stage-1 prior; not pursued.
- **Structural sharing ((ii)/(iv))** — parked; the ownership-transfer invariant would be relaxed, and no data-driven justification exists (re-open condition unmet).

## References

- [#1319](https://github.com/gosharplite/tell-me-go/issues/1319) — closed/merged: ADR-057 + context window cache removal.
- [#1320](https://github.com/gosharplite/tell-me-go/issues/1320) — open: overflow residual.
- [#1321](https://github.com/gosharplite/tell-me-go/issues/1321) — open: this issue (closed by this record).
- [ADR-057](2026-08-remove-context-window-cache.md) — F1 baseline vintages and cache-removal decision.
- [docs/architect/1321-stage1-prior.md](../architect/1321-stage1-prior.md) — stage-1 PTC prior (go/no-go, falsifiability conditions).
- [docs/architect/1321-f1-baseline.md](../architect/1321-f1-baseline.md) — F1 baseline, real-counter instrument, candidate-(i) measurement and rejection (§2, §3, §5).
