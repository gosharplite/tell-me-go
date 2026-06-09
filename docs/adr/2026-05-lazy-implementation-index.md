# ADR-039: Lazy Implementation Index — eager→lazy contract change for `computeImplementations`

## Status
Accepted (2026-05)

## Context
PR [#357](https://github.com/gosharplite/tell-me-go/pull/357) moved `computeImplementations` from eager execution (inside `Refresh()` → `updateState()`) to lazy execution (deferred to first `GetImplementations()` call). The rationale was a race-detector timeout in `make test-race`: `computeImplementations` performs O(N×M) `go/types.Implements` calls across all types and all interfaces in the module, and the race detector amplifies per-operation cost by ~10×, causing the `internal/tools/analysis` test suite to exceed the 10-minute timeout.

The `symbolIndex` interface previously guaranteed that after `Refresh()` returns, **all** index data — symbols, usages, and implementations — is queryable in O(1). That invariant no longer holds for implementation data.

## Decision
**Defer `computeImplementations` out of the `Refresh()` hot path and into a lazy, singleflight-guarded gate inside `GetImplementations()`.**

### Mechanism

1. `Refresh()` → `updateState()` sets `idx.implementations = nil` (invalidation), not recomputation.
2. `GetImplementations()` checks `idx.implementations == nil`. If nil, it calls `computeImplementationsLazy()`.
3. `computeImplementationsLazy()` uses `singleflight.DoChan` to deduplicate concurrent callers — exactly one O(N×M) computation runs regardless of how many goroutines hit the gate simultaneously.
4. A **method-count pre-filter** (added in [`b5cd4f3`](https://github.com/gosharplite/tell-me-go/commit/b5cd4f3)) skips `types.Implements` when a concrete type has fewer methods than the interface, reducing the constant factor.

### New Contract

| Operation | Before PR #357 | After PR #357 |
|---|---|---|
| `Refresh()` latency | O(N×M) — includes `computeImplementations` | O(N) — symbols + usages only |
| `GetImplementations()` latency | O(1) map lookup | O(1) (cached) **or** O(N×M) + singleflight wait (first call after TTL expiry) |
| `Refresh()` post-condition | All data hot | Symbols + usages hot; implementations invalidated |
| Race detector (`make test-race`) | TIMEOUT on analysis tests | PASS (67/67) |

### 5-Second TTL Sawtooth

The index has a 5-second TTL (`refreshTTL` in `index.go:63`). After TTL expiry, the next `Refresh()` invalidates implementations. The subsequent `GetImplementations()` call pays the full O(N×M) cost, creating a sawtooth latency profile:

```
Latency
  ▲
  │     ╱╲        ╱╲
  │    ╱  ╲      ╱  ╲      ← First GetImplementations() after TTL expiry
  │   ╱    ╲    ╱    ╲
  │  ╱      ╲  ╱      ╲    ← Subsequent calls: O(1) map lookup
  └┴─────────┴─────────┴──→ Time
     5s        5s
```

For CLI tooling (single invocation), the sawtooth is irrelevant — `GetImplementations` is called at most a few times per run. For long-running agent sessions, the first consumer after each 5s window pays the cost.

## Consequences

### Positive
- `make test-race` passes on all 67 packages — no more timeout.
- No behavioral change to production code paths. The same data is returned; only *when* it is computed changes.
- `singleflight` prevents thundering-herd on TTL expiry — concurrent callers share one computation.
- Method-count pre-filter reduces the O(N×M) constant factor for modules with many small interfaces.

### Negative
- The `Refresh()` post-condition invariant is weakened. A future index consumer that assumes implementations are hot after `Refresh()` will encounter unexpected latency.
- The sawtooth profile is non-obvious. A maintainer investigating slow `GetImplementations` calls may not immediately connect it to the 5s TTL + lazy invalidation.
- First-call latency after TTL expiry is unpredictable — proportional to (types × interfaces) in the module.

### Neutral
- Only 3 call sites consume `GetImplementations`: `dead_code.go:276` (`processImplementations`), `dead_code.go:323` (`propagateUsageToImplementations`), and `sequence.go:581` (`tryRecurse`). All are internal to `internal/tools/analysis`. No external API surface is affected.

## Alternatives Considered

1. **Keep eager computation, skip `-race` on analysis tests**
   *Rejected*: defeats the purpose of CI race detection. The `-race` flag catches real data races; disabling it to avoid a timeout would mask genuine bugs.

2. **Increase the TTL beyond 5s to reduce sawtooth frequency**
   *Rejected*: the 5s TTL serves correctness (stale index is worse than slow index). Increasing TTL trades correctness for latency. A separate ADR should decide TTL policy if needed.

3. **Pre-compute implementations in a background goroutine after `Refresh()`**
   *Rejected*: see `WarmImplementations(ctx)` below.

## Rejected: `WarmImplementations(ctx)` Opt-In (Won't Do)

A `WarmImplementations(ctx context.Context)` method was considered for the `symbolIndex` interface to warm the implementation cache for interactive consumers. This is rejected because:

- No consumer has demonstrated measurable UX impact from the 5-second TTL sawtooth.
- Background goroutine lifecycle management adds complexity disproportionate to the benefit.
- The 5-second TTL is conservative; if "first call after TTL" latency ever becomes a UX issue, the simpler fix is to increase TTL or add a use-count-based invalidation policy rather than introducing a new API surface.

This decision was finalized 2026-06 after a project-wide architecture review confirmed no pending work items require this capability.

## Related ADRs
- **ADR-037**: Test-Only Access via `agentinternal` Bridge — same "fail loud, never silently degrade" principle that motivated this change (race detector must pass).
- **ADR-021**: Test Doubles in `*test` Sub-Packages — PR #357 injected `mockDeadCodeAnalyzer` in `health_test.go` following this convention.

## References
- PR [#357](https://github.com/gosharplite/tell-me-go/pull/357): fix: resolve goroutine stalls in analysis tests under `-race`
- Commit [`0210e2a`](https://github.com/gosharplite/tell-me-go/commit/0210e2a): Defer `computeImplementations` out of `Refresh` hot path
- Commit [`b5cd4f3`](https://github.com/gosharplite/tell-me-go/commit/b5cd4f3): Method-count pre-filter
- Issue [#358](https://github.com/gosharplite/tell-me-go/issues/358): This ADR request
- Modified files:
  - `internal/tools/analysis/index.go` — `updateState()` sets `implementations = nil`
  - `internal/tools/analysis/index_impl.go` — `computeImplementationsLazy()` with `singleflight`
- Consumers:
  - `internal/tools/analysis/dead_code.go:276` — `processImplementations()`
  - `internal/tools/analysis/dead_code.go:323` — `propagateUsageToImplementations()`
  - `internal/tools/analysis/sequence.go:581` — `tryRecurse()`

---
*Last Updated: 2026-05*
