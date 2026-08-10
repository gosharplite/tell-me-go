# Issue #1321 — F1 Baseline Re-Verification + Real-Counter Instrument (Measurement Substrate)

**Status:** Recorded baseline (measurement substrate for candidate (i); ADR-058 will cite this document and ADR-057's vintages rather than restating numbers)
**Branch:** `1321-getwindow-clone-eval`
**Scope:** F1-baseline re-verification on this branch + new `large_real_counter` scenario in the calibrated instruments. ZERO production-code changes; ZERO clone-path changes (no `CloneContent`, `GetWindow`, `Manager`, or clone-path edits). The only source change is the additive benchmark-scenario change committed at `ece4137c`.

---

## 1. Vintage-tagged baseline table

Per the baseline-vintage protocol (same axes ADR-057 uses):

| Vintage axis | Value |
|---|---|
| **Clone identity** | current `CloneContent` — **code-identical to ADR-057 after-vintage `c887cd0f`** (verified: `git log c887cd0f..HEAD -- internal/domain/llm/types.go` is empty; the only intervening change touching the context pipeline is ADR-057's own cache removal at `b1818659`, which the bench harness already reflects in both vintages) |
| **Counter type (mock)** | `agenttest.MockTokenCounter` — fixed-value mock (`internal/agent/agenttest/mock_token_counter.go:13-23`, always returns `Tokens` regardless of contents) |
| **Counter type (real)** | `HeuristicTokenCounter` via `sessctx.NewHeuristicTokenCounter(nil)` — production counter (`internal/agent/session/context/token_counter.go:36-44`); nil registry is valid, `countToolDeclarationOverhead` returns 0 (`token_counter.go:107-109`) |
| **Scenario identity** | `ContextRefiner/large`, `ContextRefiner/context_cancelled`, `FullTurnCycle/happy_path`, `FullTurnCycle/with_recovery`, `FullTurnCycle/large` (mock counter); `ContextRefiner/large_real_counter` (real counter). All use the 40-turn/80-entry fixture (`internal/agent/orchestrator/engine_phases_bench_test.go:186-220`) with `MaxHistoryTurns: 200` (`:166`) — zero-discard regime (40 < 200). |
| **Commit (mock baseline)** | `b3775c76` — pristine tree before the additive bench change |
| **Commit (real counter)** | `ece4137c` — `b3775c76` + additive bench change (mock-counter variants behaviorally identical; diff +36/−2) |
| **Run date** | 2026-08-10 (UTC) |
| **Machine pins** | go `1.26.5` · `linux/amd64` · OS `Linux 6.12.94+ x86_64` · CPU `AMD EPYC 7B13` · 6 cores (`nproc`) |

**Protocol:** `go test -bench='<regex>' -benchmem -benchtime=5s -count=5 ./internal/agent/orchestrator/` (direct `go test` — `make bench` omits the orchestrator package, per ADR-057). Medians of the 5 counts reported.

---

## 2. Fresh F1 baseline (mock counter) and ADR-057 comparison verdict

Run on pristine `b3775c76`, `go test -bench='BenchmarkContextRefiner|BenchmarkFullTurnCycle' -benchmem -benchtime=5s -count=5 ./internal/agent/orchestrator/` (236.6 s).

| Scenario | Fresh median (this run) | ADR-057 after (`c887cd0f`) | Δ ns/op | B/op | allocs/op | Verdict |
|---|---|---|---|---|---|---|
| `ContextRefiner/large` | **13279** ns/op | 13355 ns/op | **−0.57%** | 16864 (identical) | 242 (identical) | within noise |
| `FullTurnCycle/happy_path` | **1616** ns/op | 1571 ns/op | **+2.9%** | 1136 (identical) | 19 (identical) | within noise |
| `FullTurnCycle/with_recovery` | **4151** ns/op | 4084 ns/op | **+1.6%** | 2072 (identical) | 38 (identical) | within noise |
| `ContextRefiner/context_cancelled` | **99.45** ns/op · 48 B/op · 1 allocs/op | (not in ADR-057 comparison table) | — | — | — | fresh-only record |
| `FullTurnCycle/large` | **1729923** ns/op · 1063265 B/op · 10255 allocs/op | (not in ADR-057 comparison table) | — | — | — | fresh-only record |

Raw 5-count ns/op samples (for median auditability):
- `ContextRefiner/large`: 13336, 13279, 13323, 13160, 13170 → median **13279**
- `ContextRefiner/context_cancelled`: 99.75, 99.45, 98.87, 99.69, 99.07 → median **99.45**
- `FullTurnCycle/happy_path`: 1623, 1615, 1616, 1620, 1599 → median **1616**
- `FullTurnCycle/with_recovery`: 4089, 4115, 4179, 4151, 4155 → median **4151**
- `FullTurnCycle/large`: 1761449, 1738232, 1729923, 1681238, 1670195 → median **1729923**

### VERDICT: (a) REPRODUCED WITHIN NOISE

For all three ADR-057-recorded comparisons, **B/op and allocs/op are bit-identical** to the recorded after-numbers, and ns/op is within ±3% (range: −0.57% to +2.9%) — machine-noise band. **ADR-057's numbers remain the decision baseline; this fresh run is corroboration, not a replacement.** No machine divergence (this is the same machine family/era as ADR-057's vintage: AMD EPYC 7B13, go 1.26.5).

### Struct-overhead observation (for ADR-058)

`ContextRefiner/large`: **16864 B/op ÷ 80 entries ≈ 210.8 ≈ 211 B/entry**. This is the **bounded ceiling** that brackets candidate (i)'s realistic win: even a perfect per-entry elimination could not recover more than ~211 B/entry, and the actual recoverable share depends on what fraction of that 211 B/entry is clone-copy overhead vs. pipeline-inherent allocations (to be decomposed by Task 3).

---

## 3. Real-counter instrument: `BenchmarkContextRefiner/large_real_counter`

**Bench-file change (additive only, committed `ece4137c`):** `newBenchTurnLarge` refactored into a wrapper (`engine_phases_bench_test.go:144-146`) + counter-parameterized core `newBenchTurnLargeWithCounter(counter llm.TokenCounter)` (`:148-184`); the existing mock-counter variants construct the identical fixture/limits/bus/clock/AddContent-suppression and are **behaviorally identical** (diff: +36/−2 lines; the −2 are the mock-counter construction moved into the core). New sub-benchmark `large_real_counter` (`:312`) runs the same 40-turn fixture through `ContextRefiner.Process` with the **production** `HeuristicTokenCounter` (`sessctx.NewHeuristicTokenCounter(nil)`).

With the real counter, each `Prepare` pays **two O(text) token walks per round**: the gatekeeper's `t.Estimator.EstimateTokens(req.History)` (`internal/agent/session/context/gatekeeper.go:126`) and the final validator's `t.Strategy.EstimateTokens(req.History)` (`internal/agent/session/context/transformers.go:60`; both delegate to `Strategy.EstimateTokens` → `counter.Count`, `strategy.go:83-84`).

**Results:**

| Run | `large_real_counter` ns/op (median) | B/op | allocs/op |
|---|---|---|---|
| Validation (1 s × 1) | 13106 | 16864 | 242 |
| **Authoritative (5 s × 5)** | **12336** (samples: 12156, 12405, 12336, 12563, 12180) | **16864** | **242** |
| Paired same-process (2 s × 3) | **13398** (samples: 13415, 13286, 13398) | 16864 | 242 |

**Same-process mock-vs-real comparison (paired run, both counters in one process):** `ContextRefiner/large` (mock) median **13239** (13093, 13438, 13239) vs `large_real_counter` median **13398** → real ≈ mock **+1.2%**, well inside the overlapping sample ranges — i.e. the two O(text) walks add a single-digit-percent cost at this fixture size (~80 entries × ~50 chars ≈ 4 KB text × 2 walks), **not** a dominant component. The authoritative real-counter run's lower median (12336) vs the mock suite run (13279) is process-level variance across separate invocations (the paired run is the correct basis for the counter delta).

**Key structural finding (recorded for Task 3 / ADR-058):** B/op (16864) and allocs/op (242) are **bit-identical between the mock counter and the production counter** across every run. The counter contributes **zero** additional allocation; the 211 B/entry struct overhead is clone/pipeline-dominated, not counter-dominated. This brackets candidate (i): the measurable win space is the per-`Prepare` clone machinery, and B/op is the right currency for it (per contract, B/op is recorded but **not** the acceptance currency — the mock-counter `large` is).

**Expected vs observed (honest divergence note):** the Architect's expectation that ns/op would be "dominated by the two O(text) token walks" is **not** borne out at this fixture size — the walks cost ≈1–2% (within noise). The dominant ns/op cost is the pipeline + per-round clone machinery, which the mock-counter `large` already measures. This does not change the substrate: the real-counter scenario remains the correct instrument for candidate (i) at larger text volumes, and its value will scale with session text size, not turn count.

---

## 4. Verification (all green)

```bash
go build ./...                                                    # PASS
go test ./internal/agent/orchestrator/...                         # PASS
go test ./internal/agent/session/context/...                      # PASS (includes TestManager_Prepare_ClonesContent — untouched, verbatim)
git diff ece4137c^ ece4137c -- internal/agent/orchestrator/engine_phases_bench_test.go   # +36/−2, purely additive
```

**Non-negotiables honored:** no production-code changes; no changes to `internal/domain/llm/types.go`, `internal/infrastructure/history/history.go`, or `internal/agent/session/context/manager.go` (verified: `git diff b3775c76..HEAD --stat` touches only the bench test file + docs); `docs/architect/INTENTIONAL_NON_FIXES.md` untouched (consult-only); no ADR created or modified; no new dependencies; no Makefile changes; `TestManager_Prepare_ClonesContent` stays green verbatim.
