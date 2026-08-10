# Issue #1321 — Stage-1 Config-Aware Prior for the PTC (Prune-Then-Clone) Candidate

**Status:** Go/no-go resolution (analysis-only; documented prior)
**Branch:** `1321-getwindow-clone-eval` (created from `dev` at `b1818659` — merged #1319/#1322 state)
**Scope:** Stage-1 economics gate for the PTC candidate. ZERO production code changes, ZERO test changes, ZERO benchmark changes. Deliverable is this document only.
**Contract basis:** Per the ratified contract, PTC v1 savings = *cleaner-drops + pruner-drops only*. Gatekeeper-triggered summarization drops and skill-injector/gatekeeper over-cloning are explicitly **excluded** from v1.

> **Terminology.** PTC = *prune-then-clone*: a candidate optimization that would prune (drop) history content earlier in the context pipeline so that the subsequent per-`Prepare` clone work is smaller. Its v1 value proposition is entirely the *discard* it performs — if the pipeline already discards ≈ 0 under the dominant production configuration, PTC has nothing to save and needs no measurement substrate.

---

## 1. Regime enumeration

Three config regimes determine whether PTC could save anything. The pipeline construction that governs each is `Factory.BuildStandardPipeline` (`internal/agent/session/context/pipeline_factory.go:36-88`).

### R-default — `MaxHistoryTurns == 0` (the shipped default)

- **Pipeline construction:** The `HistoryPruner` is **not constructed**. `BuildStandardPipeline` gates pruner insertion on `limits.MaxHistoryTurns > 0` (`pipeline_factory.go:61`). With the default `MaxHistoryTurns = 0` (`internal/domain/config/config.go:19`; the field is explicitly documented at `config.go:264`: "MaxHistoryTurns=0 means no pruning"), the pruner is absent from the transformer list — the pipeline is `HistoryRepairer → [extras] → toolResponseCleaner → emptyMessagePruner → contentCleaner → thoughtSignaturePropagator → tokenGatekeeper → emptyTurnFilter → WarningInjector → TransientMerger → finalContextValidator` (`pipeline_factory.go:50-88`).
- **Consequence:** Pruner-drops are **structurally impossible, by construction, not by threshold**. The code path that could drop a turn (`HistoryPruner.Transform`, `internal/agent/session/context/pruner.go:34-57`) is never invoked, because no instance exists in the pipeline.

### R-configured — `MaxHistoryTurns > 0`

- **Pipeline construction:** The `HistoryPruner` is appended with a `compositePruningPolicy` (`pipeline_factory.go:61-68`):

  ```go
  windowTurns := limits.MaxHistoryTurns                 // pipeline_factory.go:38
  // precise profile: windowTurns = MaxHistoryTurns / 2, min 2  (pipeline_factory.go:42-44)
  if limits.MaxHistoryTurns > 0 {                        // pipeline_factory.go:61
      transformers = append(transformers, &HistoryPruner{
          Policy: &compositePruningPolicy{
              Policies: []PruningPolicy{
                  &SlidingWindowPolicy{MaxTurns: windowTurns}, // pipeline_factory.go:66
                  &pinningPolicy{},                             // pipeline_factory.go:67
                  &importanceRankPolicy{},                      // pipeline_factory.go:68
              },
          }, ... })
  }
  ```

  `windowTurns = MaxHistoryTurns` for the balanced profile; for the precise profile `windowTurns = MaxHistoryTurns / 2` with a floor of 2 (`pipeline_factory.go:42-44`).
- **Drop semantics:** `SlidingWindowPolicy.MarkTurns` keeps only the last `MaxTurns` turns (`pruner.go:159-189`, `startWindow := totalTurns - p.MaxTurns` at `pruner.go:170`). `compositePruningPolicy` unions the keep-marks of all three policies (`pruner.go:140-153`) — a turn survives if **any** policy retains it. Pruner-drops occur **only when** (a) session turns exceed the sliding window **and** (b) neither `pinningPolicy` (`pruner.go:234`) nor `importanceRankPolicy` (`pruner.go:192`) retains the out-of-window turn. `HistoryPruner.Transform` then reconstructs history, counting `PrunedTurns` (`pruner.go:34-57`).

### R-bench — the calibrated instruments

- **Pipeline construction:** `newBenchTurnLarge` (`internal/agent/orchestrator/engine_phases_bench_test.go:144-184` — wrapper `:144-146` + counter-parameterized core `:148-184`) reconfigures the context manager with `MaxHistoryTurns: 200` (`engine_phases_bench_test.go:166`). Its history fixture `makeLargeHistory` (`engine_phases_bench_test.go:186-220`) builds **80 entries = 40 user/model turns** (`for i := 0; i < 40; i++`, `engine_phases_bench_test.go:212`). The bench's own `newBenchTurn` variant uses `MaxHistoryTurns: 100` with a single turn (`engine_phases_bench_test.go:72,89` and `:109,119`).
- **Consequence:** `40 < 200` — session turns never exceed the sliding window, so `SlidingWindowPolicy` keeps everything (`startWindow = 40 - 200 < 0 → 0`, `pruner.go:170-172`), and no pruner-drops occur. This is the **benchmark's own 40<200 zero-discard observation**: the harness facts are primary-sourced at `engine_phases_bench_test.go:166` + `:186-220`; ADR-057 records the resulting measurements for this harness (see §3.4) and explicitly defers the perf-track follow-up to #1321 (`docs/adr/2026-08-remove-context-window-cache.md:98`).

---

## 2. PTC v1 savings sources, quantified per regime

### 2.1 Pruner-drops

| Regime | Pruner-drops |
|---|---|
| R-default | **0** — `HistoryPruner` not constructed (`pipeline_factory.go:61`). Structural zero. |
| R-configured | **0 until session turns exceed the sliding window.** Below the window, `SlidingWindowPolicy` keeps every turn (`pruner.go:163-187`) and no turn is dropped. Above the window, drops are bounded by the pinning/importance retention of out-of-window turns (`pruner.go:140-153`). |
| R-bench | **0** — 40 turns < window 200 (`engine_phases_bench_test.go:166,212`); zero-discard regime. |

**Session-length bound.** In R-configured, how far can a session grow before something else intervenes? The `tokenGatekeeper` is always present (`pipeline_factory.go:77-81`, `withMaxTokens(limits.MaxHistoryTokens)`), and it triggers auto-summarization when estimated tokens exceed **90% of `MaxTokens`** (`internal/agent/session/context/gatekeeper.go:145-146`: `tokens > int(float64(t.MaxTokens)*0.9)`). With the default budget `DefaultMaxHistoryTokens = 120000` (`config.go:20`), a session is summarization-capped at ≈108000 estimated tokens. Summarization resets/shrinks history, which resets the turn count — so even in R-configured, pruner-drops are bounded per window and the window cannot grow without bound. (Summarization drops themselves are **excluded** from PTC v1 per the contract; they only serve here as the session-length bound.)

### 2.2 Cleaner-drops

The four cleaning transformers in the standard pipeline drop **only empty/malformed content**:

| Transformer | Drops | Source |
|---|---|---|
| `contentCleaner` | empty parts (drops `p.IsEmpty()` parts; falls back to `[empty response]` only if *everything* was empty) | `internal/agent/session/context/transformers.go:264-328` |
| `toolResponseCleaner` | tool calls/responses with **empty IDs** (`isInvalidToolPart`: `FunctionCall.ID == ""` or `FunctionResponse.ID == ""`); also drops messages whose parts were entirely removed | `transformers.go:330-411` |
| `emptyMessagePruner` | messages with 0 parts | `transformers.go:412-439` |
| `emptyTurnFilter` | turns where user+model messages are all-empty (`isTurnEmpty`), preserving a trailing single message | `transformers.go:20-51` |

All four fire only on empty/malformed content — in **well-formed sessions ≈ 0**. The bench's 40-turn scenario has zero cleaner-drops: `makeLargeHistory` builds every entry as a `user`/`model` text part with a non-empty string and no function calls (`engine_phases_bench_test.go:186-220`), so no empty part, no empty ID, no 0-part message, no all-empty turn exists for any of the four to drop.

---

## 3. Dominant-config evidence

### 3.1 Defaults (the shipped configuration is `MaxHistoryTurns = 0`)

- `DefaultMaxHistoryTurns = 0` and `DefaultMaxHistoryTokens = 120000` are the domain defaults: `internal/domain/config/config.go:19-20`.
- `config.go:264` documents the semantics explicitly: "MaxHistoryTurns=0 means no pruning" (within `ValidateBounds`, which also rejects negatives at `config.go:271`).
- The YAML surface is `MAX_HISTORY_TURNS` (`config.go:151`); `Limits.MaxHistoryTurns` is the runtime carrier (`internal/domain/events/types.go:21`).

### 3.2 Config plumbing — and the negative grep result

The full chain from user YAML to pipeline construction:

1. **Load/default:** `setDefaults` in the infrastructure loader applies `domain_config.DefaultMaxHistoryTurns` when the field is unset in the user's YAML (`internal/infrastructure/config/config.go:212`); the DTO→domain mapping carries the value at `config.go:272`.
2. **Agent runtime config:** initial limits are seeded with the domain defaults (`internal/agent/agent.go:126-129`); per-turn refresh reads the watcher's limits into `runtimeConfig` (`agent.go:222-231`, `newCfg.Limits.MaxHistoryTurns = histTurns` at `:231`).
3. **Event + reconfigure:** `publishConfigUpdate` emits `events.ConfigUpdated{Limits: cfg.Limits}` (`agent.go:249-250`); `reconfigureContextManager` calls `ctxManager.Reconfigure(cfg.Limits)` (`agent.go:285-289`).
4. **Pipeline rebuild:** `Manager.Reconfigure` (`internal/agent/session/context/manager.go:93`) rebuilds via `cm.Factory.BuildStandardPipeline(limits, ...)` (`manager.go:106`; initial build at `manager.go:73`), which applies the `MaxHistoryTurns > 0` guard (`pipeline_factory.go:61`).

**Negative grep result (recorded).** A repo-wide search for `max_history_turns|MAX_HISTORY_TURNS` across all `.yaml`/`.yml`/`.toml` files returns **exactly one hit**: `docs/domain-model/tell-me-go.modelith.yaml:791` — a *prose mention* of the field name inside a domain-model scenario describing hot-reload of limits (mirrored at `docs/domain-model/tell-me-go.modelith.md:623`). It is a documentation artifact, not a config value. **No shipped product user config or example sets `MAX_HISTORY_TURNS`.**

The agent-config trees must be distinguished from product user config: `configs/*.yaml` at the repo root (`architect.yaml`, `assistant.yaml`, `coder.yaml`, `reviewer.yaml`, `tester.yaml`) and the config trees under `docs/user/dobby/ait-base/configs/*.yaml` and `docs/user/toby/ait-base/configs/*.yaml` are **tell-me-go agent configs** — they carry `MODE:`/`PERSON:` persona definitions (verified: e.g. `configs/coder.yaml` declares `MODE: "coder"` + `PERSON`), i.e. they configure agents *run by* tell-me-go, not the tell-me-go product's own history limits. None of them sets `MAX_HISTORY_TURNS` (verified: grep over `configs/`, `docs/user/dobby`, `docs/user/toby` returns 0 matches). Therefore the dominant production configuration — a user running tell-me-go with defaults or any shipped config — has `MaxHistoryTurns = 0` and lives in R-default.

### 3.3 Fixture survey (what tests exercise)

No unit/integration fixture reproduces a discard regime with the default config:

- **`internal/agent/agenttest`:** **no** `MaxHistoryTurns` fixtures exist in the test-double package (0 matches) — the doubles themselves are pruning-agnostic; pruning state comes from explicit `Reconfigure` calls in tests.
- **Integration fixtures (`tests/integration`):** `MaxHistoryTurns: 10` in `tests/integration/agent/orchestrator/turn_engine_limit_test.go:138`, `turn_engine_integration_test.go:114,193,309`, `turn_engine_loop_test.go:74,139`; asserted as `10` in `tests/integration/agent/agent_integration_test.go:81`; `MaxHistoryTurns: 20` in `tests/integration/agent/session/auto_summarize_test.go:43,71,219,312` and `tests/integration/agent/concurrency_stress_test.go:247`. These are short-session fixtures (2–20 turns) with no discard pressure, and they are test-only — they do not ship as product config.
- **CLI integration:** inline test YAML `MAX_HISTORY_TURNS: 10` at `internal/cli/chat_integration_test.go:143` (test fixture string, not a shipped config file).
- **Context unit fixtures:** `pipeline_factory_test.go:28-29` explicitly tests both poles — `{"Turn Pruning Enabled", MaxHistoryTurns: 10, expectPruner: true}` vs `{"Turn Pruning Disabled", MaxHistoryTurns: 0, expectPruner: false}` — confirming the guard behavior. `context_manager_test.go:547` uses `MaxHistoryTurns=200` with a comment recording the same zero-discard structure at unit level: "(pruner active but non-trimming: 12 turns ≪ 200)".

The only calibrated instrument that even *activates* the pruner is the benchmark harness, and it sits in the zero-discard regime (40 ≪ 200).

### 3.4 Bench observation (ADR-057)

ADR-057 (`docs/adr/2026-08-remove-context-window-cache.md`) records the calibrated `ContextRefiner/large` numbers for the R-bench harness: **after = 13355 ns/op · 16864 B/op · 242 allocs/op** (commit `c887cd0f`, table in ADR-057 "Benchmark evidence" section; before = 13246 ns/op · 16816 B/op · 242 allocs/op at `d44fc338`). These measurements were taken in the **zero-discard regime** (40 turns < window 200, §1 R-bench) — i.e. they measure clone/gatekeeper overhead with no pruner activity, which is why ADR-057 defers the write-clone isolation to the "#1321 perf track" (`ADR-057:98`) and lists #1321 as the open F2 perf follow-up (`ADR-057:120`).

---

## 4. Go/no-go resolution

**PRIOR IS DECISIVE — discard ≈ 0 under the dominant production config → PTC is GATED.**

- The dominant config is R-default (`MaxHistoryTurns = 0`, §3.1-3.2): the `HistoryPruner` is structurally absent (`pipeline_factory.go:61`), so **pruner-drops = 0 by construction** — not by threshold, not by session length.
- Cleaner-drops require empty/malformed content; in well-formed sessions all four cleaners drop nothing (§2.2) — the dominant-config expectation is ≈ 0.
- The only shipped instrument that activates the pruner (the bench) is a zero-discard regime (40 < 200, §1 R-bench), so there is **no calibrated discard signal anywhere in the repo** to motivate a measurement substrate.

**Therefore, for the PTC branch:**

- ❌ No PTC-specific engineering (no prune-then-clone implementation).
- ❌ No stage-2 measurement substrate (no collector window, no discard telemetry, no benchmark harness changes).
- ✅ The PTC branch resolves to **terminal candidate 3 (accept-with-data)**: the prior is recorded here (this document) as the data, and the branch is closed without further engineering.

**Candidate (i) is NOT part of this task and is unaffected:** it is measured-not-gated and proceeds separately, per the ratified contract. This gate applies only to the PTC (candidate with prune/clone economics), not to candidate (i).

---

## 5. Falsifiability statement

The prior is a **falsifiable claim**, recorded as:

> **Claim P1:** Under the dominant production configuration (`MaxHistoryTurns = 0`, no shipped config sets `MAX_HISTORY_TURNS`), the context pipeline discards ≈ 0 content per `Prepare`, so PTC v1 has no savings to capture.

**Evidence that would flip this prior** (any one suffices; each is checkable, and none exists in the repo today):

1. **Production config telemetry** showing a non-trivial fraction (e.g. a majority) of real sessions running with `MaxHistoryTurns > 0` **and** session turn counts exceeding the sliding window (i.e. actual pruner activation with out-of-window turns not retained by pinning/importance). This would invalidate the "dominant config is R-default" premise and establish a real R-configured discard surface.
2. **A config-stratified production sample** (across R-default and R-configured sessions) showing observed discard > 0 — i.e. `PrunedTurns > 0` metadata (`pruner.go:53-57`) or history-length reduction attributable to the four cleaners (`transformers.go:20-51,264-328,330-411,412-439`).
3. **A shipped product config or example** that sets `MAX_HISTORY_TURNS > 0` — this would directly contradict the negative grep (§3.2) and re-open R-configured as a dominant regime.
4. **Evidence of cleaner-drops in well-formed sessions** — e.g. production telemetry of empty-ID tool responses or 0-part messages arising from normal tool execution (not from crashes/interrupts), which would give the four cleaners a nonzero discard surface even in R-default.

Until one of these materializes, the correct engineering posture is: no PTC engineering, no measurement substrate — the branch resolves to candidate 3 (accept-with-data), with this document as the audit trail.

---

## Appendix A — Verification run (analysis-only proof)

The task was analysis-only; nothing changed except this document. Verified green on the branch before commit:

```bash
git branch --show-current            # 1321-getwindow-clone-eval (b1818659)
go build ./...                       # PASS
go test ./internal/agent/session/context/...   # PASS (includes TestManager_Prepare_ClonesContent, context_manager_test.go:233, untouched)
```

**Non-negotiables honored:**
- `TestManager_Prepare_ClonesContent` (`internal/agent/session/context/context_manager_test.go:233-248`) stays green **verbatim** — no test file was touched by this task.
- `docs/architect/INTENTIONAL_NON_FIXES.md` was consulted (read-only); it contains no entry bearing on PTC discard economics (its context-pipeline entries are defensive-guard/interface-stub classes for unrelated paths, e.g. `manager.go` `capBestBlock`). Not modified.
- No ADR created or modified — ADR-058 (a later task) will cite this document.
- No new dependencies, no benchmark harness changes, no Makefile changes.

**Anchor verification note:** all `file:line` citations above were re-anchored against the working tree at `b1818659` (post-#1319/#1322) and match the cited lines exactly at write time.
