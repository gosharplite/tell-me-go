# Issue #1321 — F1 Baseline Re-Verification + Real-Counter Instrument (Measurement Substrate)

**Status:** Recorded baseline + candidate-(i) outcome (measurement substrate; candidate (i) measured and REJECTED — see §5; ADR-058 will cite this document and ADR-057's vintages rather than restating numbers)
**Branch:** `1321-getwindow-clone-eval`
**Scope:** F1-baseline re-verification on this branch + new `large_real_counter` scenario in the calibrated instruments + candidate-(i) prototype/equivalence/measurement/decision. ZERO production-code changes in the baseline; the (i) prototype was reverted post-measurement (tree is `ece4137c`-equivalent). The only surviving source change is the additive benchmark-scenario change committed at `ece4137c`.

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

---

## 5. Candidate (i) evaluation — arena-based GetWindow clone (prototype, measured, REJECTED)

Post-vintage record of the Task 3 candidate-(i) evaluation. ADR-058 will cite this section, ADR-057's vintages, and the two SHAs below rather than restating numbers.

### 5.1 Decomposition of the 211 B/entry ceiling (pre-registration input)

`go test -bench='BenchmarkContextRefiner/large' -benchmem -benchtime=2s -count=1 -memprofile=/tmp/mem.out ./internal/agent/orchestrator/` + `go tool pprof -alloc_space` on the pre-prototype tree (`ece4137c`):

- **Key fact verified:** in `ContextRefiner/large` the factory is nil (`newBenchTurnLargeWithCounter` → `NewManager(..., nil)`), so `cm.Pipeline` is nil (`manager.go:69-75`) and `runPipeline` short-circuits (`manager.go:152-153`). The bench's **16864 B/op / 242 allocs are ENTIRELY the GetWindow clone** — confirmed by the profile: `(*Content).clone` 48.5% + `(*Part).clone` 46.2% + `GetWindow` 4.4% of all bytes, all under `Prepare → loadHistory → GetWindow → CloneContent`.
- **Per-entry allocation model (80 entries × 1 text part):** `&Content{}` (96 B class) + `make([]*Part, 1)` (8 B) + `&Part{}` (96 B class) = 200 B/entry in 3 allocs + 1 window-level result slice (8 B/entry) → **208 B/entry in size classes**; measured 210.8 B/entry (16864/80); residual ~3 B/entry allocator noise.
- **(a) Irreducible payload floor:** the structs and pointer slots themselves — Content 96 B + Part 88 B (measured via `unsafe.Sizeof`: Content=96, Part=88, Blob=40, FunctionCall=40, FunctionResponse=40) + Parts pointer slots 8 B + result pointer slot 8 B = **200 B/entry = 16000 B/op** (94.9% of the measurement).
- **(b) Recoverable (non-payload):** **864 B/op = 10.8 B/entry = 5.1%** — the per-entry Parts-slice allocation tax + Part size-class rounding (88→96) + allocator residual across 241 separate small allocations.

### 5.2 Pre-registered bar X (stated BEFORE the measurement)

The recoverable fraction (5.1%) is **below the contract's 15% bracket** → per the contract's sub-bracket provision, X is set at the **decomposition-justified ceiling with margin**:

> **X = 4.5% B/op reduction on `ContextRefiner/large` (mock counter), ≈759 B/op off the 16864 baseline.**

Argument: the arena is projected to recover the full non-payload fraction (5.1%) by consolidating 241 small allocations into ~4 contiguous backings; X = 4.5% allows 0.6 points for allocator class behavior on the new backings. An honest sub-bracket X with this decomposition argument is the reviewable artifact; an unjustified 15% bar would not be. **ns/op guard (unchanged from contract):** post-(i) `large_real_counter` within ±3% of the paired baseline 13398; `happy_path`/`with_recovery` within ±3% of 1616/4151.

### 5.3 Prototype (commit `04a51d5e`, pre = `ece4137c`, post = `04a51d5e`)

- `internal/domain/llm/types.go` — added `CloneArena` + `NewCloneArena(capacity)` + `(*CloneArena).CloneContent` + `(*CloneArena).CloneContentSlice` (arena-backed deep clone: Content/Part/Blob/FunctionCall/FunctionResponse structs laid out in contiguous backing arrays sized exactly by a counting pass; per-entry `Parts`/`TransientParts` sub-sliced with full slice expressions so `cap == len` — append-safe, no neighbour clobbering). `CloneContent` unchanged in behavior for all other callers.
- `internal/infrastructure/history/history.go` — `GetWindow` uses a fresh per-call arena sized to the window (no pooling, no global state, no synchronization — compliant with the lifetime constraint that Prepare's output outlives Prepare).
- `internal/agent/agenttest/mock_history_manager.go` — `GetWindow` adopts the SAME arena path (non-lazy-mock mandate: the bench measures the mock, so the mock implements the real algorithm).
- `internal/domain/llm/clone_equivalence_test.go` — new equivalence suite.

### 5.4 Equivalence proof (all green at `04a51d5e`)

Six table-driven tests in `internal/domain/llm/clone_equivalence_test.go`, asserting the arena clone is **identical to `CloneContent`'s output via `reflect.DeepEqual`** (covers TokenCount, Pinned, ID, ThoughtSignature bytes — stronger than EqualContent) plus mutation isolation both directions, over the full edge corpus:

| Test | Coverage |
|---|---|
| `TestCloneArena_EquivalenceWithCloneContent` | 14-entry corpus: nil Content, nil Part in Parts, nil/empty TransientParts, nil InlineData/FunctionCall/FunctionResponse, empty Parts, empty Args/Response maps, nested maps, nested slices, mixed `[]interface{}` (string/float64/bool/nil), populated TransientParts, non-empty ThoughtSignature, pinned, populated TokenCount, multi-part; slice length/order + nil→nil mapping |
| `TestCloneArena_SingleEntryEquivalence` | entry-level `CloneContent` vs reference for text/tool-call/transient/signature entries |
| `TestCloneArena_MutationIsolation` | both directions, every corpus entry (source→clone and clone→source) |
| `TestCloneArena_AppendSafety` | append to clone[0].Parts must not clobber clone[1] or the source (full-slice-expression caps) |
| `TestCloneArena_EmptyAndNilInputs` | nil and empty input slices |
| `TestCloneArena_ThoughtSignatureAndBlobBytes` | in-place source-byte mutation must not alias into the clone |

Two real semantic bugs were caught and fixed during development: (1) empty-but-non-nil `Parts` cloned to `nil` when the window had zero parts (nil-backing slice) — fixed by always allocating the pointer backing (zerobase) when any content exists; (2) the single-entry path lacked the backing pre-sizing — fixed with a first-use exact-cap guard in `cloneParts`.

**Guardrail:** `TestManager_Prepare_ClonesContent` (`context_manager_test.go:233`) passed **verbatim** with the mock on the arena path — the guardrail now exercised the arena, as intended. **-race gate:** `go test -race ./internal/agent/session/... ./internal/infrastructure/history/... ./internal/domain/llm/...` all green at `04a51d5e`. Full suites green: `go test ./internal/domain/llm/... ./internal/infrastructure/history/... ./internal/agent/session/... ./internal/agent/orchestrator/...`.

### 5.5 Measurement (post-vintage `04a51d5e`, same machine pins as §1)

`go test -bench='BenchmarkContextRefiner|BenchmarkFullTurnCycle' -benchmem -benchtime=5s -count=5 ./internal/agent/orchestrator/` (229.9 s). Medians of 5:

| Scenario | Pre (`ece4137c`) | Post (`04a51d5e`) | Δ ns/op | Δ B/op | allocs/op |
|---|---|---|---|---|---|
| `ContextRefiner/large` (mock, **acceptance currency**) | 13279 ns/op · **16864 B/op** · 242 | 9096 ns/op · **17952 B/op** · 5 | **−31.5%** | **+1088 B/op (+6.5%)** | 242 → 5 |
| `ContextRefiner/large_real_counter` (ns/op guard) | 13398 ns/op (paired) · 16864 B/op | 9133 ns/op · 17952 B/op | **−31.8%** | +6.5% | 242 → 5 |
| `ContextRefiner/context_cancelled` | 99.45 · 48 B/op · 1 | 100.3 · 48 B/op · 1 | +0.85% | 0 | 1 |
| `FullTurnCycle/happy_path` (small-window) | 1616 · 1136 B/op · 19 | 1604 · 1136 B/op · 19 | **−0.7%** | 0 | 19 |
| `FullTurnCycle/with_recovery` (small-window) | 4151 · 2072 B/op · 38 | 4120 · 2072 B/op · 38 | **−0.7%** | 0 | 38 |
| `FullTurnCycle/large` | 1729923 · 1063265 B/op · 10255 | 884805 · 991142 B/op · 19 | **−48.8%** | −6.8% | 10255 → 19 |

Post-vintage raw samples — `ContextRefiner/large` ns/op: 8939, 9011, 9096, 9174, 9209 → median **9096**; B/op 17952 across all 5; allocs 5. `large_real_counter` ns/op: 9133, 9361, 9101, 9113, 9149 → median **9133**; B/op 17952; allocs 5. `happy_path` ns/op: 1613, 1604, 1608, 1599, 1601 → median **1604**. `with_recovery` ns/op: 4115, 4139, 4121, 4120, 4107 → median **4120**. `FullTurnCycle/large` ns/op: 865344, 884805, 865072, 927861, 929693 → median **884805**.

Isolated micro-benchmark (arena vs reference clone of 80 text entries, same process): arena **17792 B/op · 4 allocs · ≈8100 ns/op** vs reference **16704 B/op · 241 allocs · ≈11900 ns/op** — reproduces the +6.5% B/op regression and −31% ns/op win without the harness.

### 5.6 Decision: REJECTED (candidate 3 terminal — a successful evaluation)

**The B/op acceptance currency FAILS the pre-registered bar; the ns/op guard PASSES.**

- **ΔB/op = +1088 B/op = +6.5% (a regression, not a reduction) < X = 4.5%** → the decision rule fires REJECTED.
- **ns/op guard:** `large_real_counter` −31.8% vs 13398 (well within ±3%; no regression), `happy_path` −0.7%, `with_recovery` −0.7% — all pass.
- **allocs/op:** 242 → 5 (fell as predicted; data, not a gate).
- **Why B/op regressed — the falsified decomposition assumption:** the pre-registration assumed contiguous backings cost exact bytes. The measurement falsified that: Go's allocator rounds the arena's medium backings up to sparse size classes — 80×88 B = 7040 B → **8064** (+1024), 80×96 B = 7680 B → **8064** (+384), 2×640 B → 704 (+128) — a ~+1792 B/op class-rounding tax that **exceeds** the per-entry small-allocation tax the arena eliminated (pre-arena per-entry allocs were already class-exact at 96/8/96). The arena consolidates allocations (−98%), cuts ns/op (−31%), but **increases bytes (+6.5%)** — the opposite of the acceptance currency's requirement. A byte-competitive arena would require unsafe byte-packing (one exact ~16 KB allocation), which is outside this project's standards and the contract's "no structural sharing" provability requirement.
- **Post-decision tree state:** the prototype commit `04a51d5e` was **reverted** (`474a63bc`); the code tree is **exactly `ece4137c`-equivalent** (verified: `git diff ece4137c HEAD --stat` shows only docs) — no dead code left behind, the arena API and equivalence test were fully removed with the revert.

**F2 outcome:** candidate (i) REJECTED with data; candidate PTC already GATED (Task 1). F2 ends at **CANDIDATE 3 (accept-with-data) for both candidates** — a successful evaluation: the stage-1 prior (Task 1), the F1 baseline + real-counter instrument (Task 2), and this measured (i) outcome (Task 3) are the recorded data. ADR-058 (Task 4) will cite this document and ADR-057's vintages.
