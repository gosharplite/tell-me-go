# ADR-057: Remove the Context Window Cache

**Status:** Accepted
**Date:** 2026-08-10
**Related:** [Issue #1319](https://github.com/gosharplite/tell-me-go/issues/1319)

## Context

`Manager` maintains an in-memory context window cache (`cachedVersion`, `cachedWindow`, `cachedMetadata`) that is written by `updateCache` after every pipeline run and served by `tryCache` → `getCachedView` at the top of `Prepare`. The cache is keyed on the manager's internal `version` counter, which is bumped by `Reconfigure`, `AddContent`, `SetPipeline`, `SetContents` (via the persist closure in `executePipeline`), and `finalizeSummarization`.

The cache is *not* dead in production — it hits on same-turn re-`Prepare`. The orchestrator's `RecoveryStep` re-enters `PhaseRefining` (which calls `Prepare` again) with no intervening version bump in three cases:

1. **Context overflow** (`LLMErrorContextOverflow`): on the first overflow (`RetryCount == 0`) it bumps `RetryCount` to 1 and returns `NextPhase: PhaseRefining`.
2. **Transient retry** (`attemptRetry`): increments `RetryCount` and returns `NextPhase: PhaseRefining`.
3. **Empty-response retry** (`attemptRetry`): same path.

In all three, the second `Prepare` finds `cachedVersion == cm.version` and returns the cached window — no `loadHistory`, no pipeline, no write clone. This is the *only* realistic hit path.

An overflow hit is **harmful** in the sub-case where re-assembly would differ: if the post-pass-1 estimate is still above the gatekeeper threshold (90% of `MaxTokens`), a cache-free re-`Prepare` would summarise again and shrink the window further; the cache hit instead returns the stale pass-1 window unchanged, and with `RetryCount == 1` (the single-refinement policy) the turn dies on the second overflow. The pure estimate/provider-divergence sub-case — where a cache-free re-`Prepare` deterministically reproduces the same window — is tracked separately in #1320.

Cross-turn, the cache is a guaranteed miss: every turn appends content via `AddContent`, bumping `version` and invalidating the cache before the next `Prepare`. The miss still pays a per-round O(window) write clone (`updateCache` → `cloneContentSlice` + `ContextMetadata.clone()`) on every `Prepare`, on every turn, for the lifetime of the session.

This ADR supersedes the premise of #1317 (closed): #1317 proposed removing the cache on the assumption that it was a guaranteed-miss dead path. #1319 corrects that premise — the cache is live on same-turn re-`Prepare`, and the removal decision must resolve the harmful overflow sub-case explicitly rather than treating the cache as inert.

## Decision

Delete the context window cache (Option A). `Prepare` always rebuilds: `loadHistory` + pipeline. The version counter remains solely as the concurrency guard for the persist closure; `cloneContentSlice` survives only as a test helper.

1. **Delete the cache fields and sync plumbing**: `cachedVersion`/`cachedWindow`/`cachedMetadata`, `tryCache`, `getCachedView`, `updateCache`, `commitToCache`, `ContextMetadata.clone()`, and the `persisted` plumbing (`persisted` return threading through `Prepare`/`runPipeline`/`commitToCache`).
2. **Keep the version counter and the persist-closure bump**: `cm.version++` inside `executePipeline`'s persist closure (adjacent to `History.SetContents`) survives — it remains the guard against a concurrent `Reconfigure`/`AddContent` during a slow summarization.
3. **Keep `cloneContentSlice` as a test-only helper**: the two tests exercising deep-clone semantics (`finalizeSummarization`, `transformers`) keep it.
4. **De-prewarm the benchmark harness**: `prewarmCache` and the `cache_hit` sub-benchmarks are removed from the orchestrator bench die-list; the die-list itself stays in the orchestrator package.
5. **Enforcement**: the acceptance checklist below is enforced by `make verify-no-context-window-cache`, wired into `make check`/`make check-full` after `verify-adr-index`.

### Acceptance checklist (machine-checkable; run from the repo root)

**Tier 1 — repo-wide die greps (expect 0 matches):**

```bash
grep -rnE 'cachedVersion|cachedWindow|cachedMetadata|tryCache|getCachedView|commitToCache|prewarmCache|versionBumpingTransformer' --include='*.go' --exclude-dir=.git .
```

**Tier 2 — component-scoped die greps (expect 0 matches each):**

```bash
grep -rnE '\bupdateCache\b' internal/agent/session/context/
grep -rn 'func (m *ContextMetadata) clone' internal/agent/session/context/
grep -rnE '\bcache_hit\b' internal/agent/orchestrator/
grep -rnw 'persisted' internal/agent/session/context/
```

**Survival — absence (expect 0 matches):**

```bash
grep -rn 'cloneContentSlice' internal/agent/session/context/ --include='*.go' | grep -v '_test\.go'
```

**Survival — presence:**

```bash
grep -rln 'cloneContentSlice' internal/agent/session/context/ --include='*_test.go'
#   expect exactly: context_manager_finalize_summarization_test.go, context_transformers_test.go

grep -c 'cm\.version++' internal/agent/session/context/manager.go
#   expect 6 (merge-time check; drift-prone)

awk '/func \(cm \*Manager\) executePipeline/,/^}/' internal/agent/session/context/manager.go | grep -c 'cm\.version++'
#   expect 1 — the persist-closure bump survives inside executePipeline
```

**Build/test:**

```bash
go build ./...
go test ./internal/agent/session/context/...
go test ./internal/agent/orchestrator/...
```

The last is required because the bench die-list lives in the orchestrator package.

**Benchmark evidence** — harness pins: go version, OS/arch, CPU model, both commit SHAs (before/after), `-benchmem`, `-benchtime=5s`, `-count=5`; median reported.

```bash
go test -bench='BenchmarkFullTurnCycle' -benchmem -benchtime=5s -count=5 ./internal/agent/orchestrator/
go test -bench='BenchmarkContextRefiner' -benchmem -benchtime=5s -count=5 ./internal/agent/orchestrator/
```

Note: both benchmarks live in the orchestrator package (`engine_phases_bench_test.go`); `make bench`'s `BENCH_PKGS` omits it, which is why direct `go test` invocation is required.

| Comparison | Before | After | Interpretation |
|---|---|---|---|
| `FullTurnCycle/happy_path` | prewarmed — 1930 ns/op · 1456 B/op · 24 allocs/op | de-prewarmed — 1571 ns/op · 1136 B/op · 19 allocs/op | no regression — faster after (1930→1571 ns/op, −22% B/op); the prewarmed cache never produced steady-state hits — PersistenceStep's AddContent bumps the version each iteration, so every Prepare was a miss that paid the updateCache write-clone; the deletion removes that per-iteration write-clone (the isolation this comparison was expected to show) |
| `FullTurnCycle/with_recovery` | prewarmed — 4544 ns/op · 2392 B/op · 43 allocs/op | de-prewarmed — 4084 ns/op · 2072 B/op · 38 allocs/op | no regression — faster after (4544→4084 ns/op, −13% B/op); same mechanism — per-iteration cache miss + updateCache write-clone (invalidated by PersistenceStep's version bump) removed by the deletion |
| `ContextRefiner/large` | 13246 ns/op · 16816 B/op · 242 allocs/op | 13355 ns/op · 16864 B/op · 242 allocs/op | neutral within noise (ns/op +0.8%, B/op +48 B, allocs identical at 242); the bench self-prewarms (iteration-1 miss populates the cache, steady state hits), so before-large measured the cache-hit read-clone, not the write-clone — both states pay one O(window) clone per iteration; the write-clone removal is not isolated by this harness |

**VINTAGE:** clone implementation identity — `current CloneContent` for both before and after (no cheaper-clone optimization landed in this PR); token counter — `agenttest.MockTokenCounter` (fixed-value mock); scenario identity — the three named comparisons; before — `d44fc338` (2026-08-10, code identical to `b0f1aeac`); after — `c887cd0f` (2026-08-10).

Future bench tweak (out of scope — #1321 perf track): a version-bump-forcing harness would isolate the write-clone per-iteration in ContextRefiner/large.

## Consequences

**Positive**

- Removes the per-round O(window) write clone (`updateCache` → `cloneContentSlice` + `ContextMetadata.clone()`) from every `Prepare`.
- Removes the stale-window correctness footgun on same-turn re-`Prepare` — the harmful overflow sub-case now gets the corrected, re-summarised window.
- Deletes the cache-sync code paths: `tryCache`/`getCachedView`/`updateCache`/`commitToCache`, `ContextMetadata.clone()`, and the `persisted` plumbing.
- Simpler `Manager` invariants — fewer version-sync surfaces (the cache's "must match version" invariant disappears; only the persist-closure guard remains).

**Negative**

- Same-turn retry/empty-response re-`Prepare` now pays a full rebuild — `loadHistory` + pipeline — bounded by `MaxHistoryTokens`, and CPU-only in the common case (persisted summarised history stays under the gatekeeper threshold; no summariser double-call).
- Overflow sub-case (the harmful one this ADR resolves): the re-`Prepare` re-invokes the summarizer — a **second LLM-backed call** — and re-persists (a second `SetContents` and version bump). This is the intended correction, bounded to at most one re-`Prepare` per turn (two Prepares, two persists, two version bumps) enforced by `RecoveryStep`'s single-refinement policy (`RetryCount == 1`; a second overflow falls through to retry/failure). Pre-deletion contrast: the cached path paid one write clone and skipped the second summarizer call — but returned the stale pass-1 window, and with `RetryCount == 1` the turn died.

## Follow-ups

Live-tracker status (resolves to live trackers):

| Tracker | Status | Scope |
|---|---|---|
| #1321 | open | F2 — perf-only follow-up, sequenced after this issue; provenance closed #1318 |
| #1320 | open | Overflow residual — this PR resolves only the harmful overflow sub-case; the pure estimate/provider-divergence sub-case remains |

Re-introducing a cache is NOT planned. Any revisit must (a) be evaluated against the post-F2 cost model — the eager load-boundary clone changes the calculus, and it is not the pre-deletion per-round model — and (b) require production hit-rate data; the only realistic hit path was same-turn re-`Prepare`, which is precisely the path this ADR removes.

## Alternatives considered

- **B — incremental O(Δ) cache.** Rejected: illusory. The whole-window pruner/gatekeeper make delta replay semantically risky — the pipeline consumes and rewrites the full window, so an incremental cache would have to re-derive the very deltas the cache was meant to save.
- **C — keep the cache + overflow-aware invalidation.** Rejected: a permanent cross-layer footgun-by-contract. The invalidation rule would have to live in the orchestrator (which knows `RecoveryStep` semantics) while the cache lives in the session context manager; any future recovery-path change would silently re-arm the stale-window bug.

## References

- [#1317](https://github.com/gosharplite/tell-me-go/issues/1317) — closed; superseded by #1319.
- [#1318](https://github.com/gosharplite/tell-me-go/issues/1318) — closed; F2 folded into #1321.
- [#1320](https://github.com/gosharplite/tell-me-go/issues/1320) — open.
- [#1321](https://github.com/gosharplite/tell-me-go/issues/1321) — open.
