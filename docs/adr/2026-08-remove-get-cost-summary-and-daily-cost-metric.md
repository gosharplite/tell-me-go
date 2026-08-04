# ADR-053: Remove `get_cost_summary` Tool and DailyCost Metric — Deprecate the Global Cost Ledger

**Status:** Accepted
**Date:** 2026-08-04
**Related:** [Issue #1291](https://github.com/gosharplite/tell-me-go/issues/1291)

## Context

The agent exposed two cost-analysis surfaces built on a single global cost ledger (`global_costs.json`):

1. The `get_cost_summary` agent tool — aggregated the ledger into a date/model summary table.
2. The `DailyCost` value in the post-turn status line (`╰─⠿ Ready ($turn $task $session $daily …)`) — computed by `CostTracker.GetDailyCost()`, which read the same ledger.

The ledger had exactly two readers (`getCostSummary`, `GetDailyCost`) and exactly two writers (`estimate_cost` tool handler, `RecordSessionCost` at session end — both via `EstimateCost(shouldRecord=true)`). Ledger recovery was triggered from the two read/write entry points only.

Problems motivating removal:

- **Synchronous blocking I/O on the hot path.** `GetDailyCost` performed a ledger file read under `t.mu` + `ledgerMu` on every final turn-status publish, and unconditionally on UTC-8 date rollover — blocking turn completion on disk I/O.
- **A "get" tool with a write side effect.** `get_cost_summary` called `recordCostSilently` (a ledger write) before reading, and triggered background ledger recovery — surprising behavior for a read-only-sounding query tool.
- **The daily metric overlaps with session cost.** The 4th value aggregated *other sessions'* cost for the current UTC-8 date on top of the current session's cost (already shown as the 3rd value). Users found this cross-session number noisy and requested its removal.
- **Unused complexity.** The ledger subsystem (recovery crawler, pidlock, retention policy, merge/upsert, session-ID derivation) existed solely to serve the two surfaces above; with both removed it became write-only dead weight.

## Decision

Remove `get_cost_summary` and the `DailyCost` metric together, then delete the global cost ledger subsystem and make `estimate_cost` a pure read-only estimator.

Specifically:

1. **Remove the `get_cost_summary` tool**: registration in `RegisterMetrics`, the `costSummaryArgs` struct, `recordCostSilently`, `metrics_summary.go` in full, and the `policy.go` whitelist entry.
2. **Remove the `DailyCost` metric**: drop `GetDailyCost` from the `CostTracker` interface, stop populating `TurnStatus.DailyCost` in the status-reporter middleware, and render three costs (`TurnCost TaskCost SessionCost`) in the plain renderer, the async turns logger, and the TUI progress model.
3. **Remove the ledger**: delete `ledger.go` and the recording half of `metrics_cost.go` (`recordCost`, `recordCostIfNeeded`, `updateLedgerHistory`, `loadHistory`, `triggerLedgerRecovery`, retention), the `kvStore` parameter of `RegisterMetrics` (used only for retention days), the `pidlock` package (existed only to lock the ledger), and all ledger-only tests.
4. **Make `estimate_cost` read-only**: it continues to report the per-SKU cost from the tokens log + pricing, but no longer writes `global_costs.json`.
5. **Keep** per-turn cost accumulation (`CostTracker.AccumulateAndReturn`) — this enforces the `deterministic-cost-audit` domain invariant and is independent of the ledger — and `RecordSessionCost`'s log-summary append (`appendSummaryToLog`).

## Consequences

**Positive**

- Removes a synchronous ledger read from the turn-completion hot path.
- Eliminates a "get" tool with a hidden write side effect and background recovery.
- Deletes a large dead subsystem: `ledger.go`, `metrics_summary.go`, most of `metrics_cost.go`, the recovery crawler, pidlock usage, retention policy, `sessionCostRecord`, and ~20 test files — a net reduction in maintenance surface.
- Simplifies the `Ready` line to three unambiguous per-session cost values.

**Negative**

- **Loses cross-session cost visibility.** The ability to answer "how much did I spend across all sessions today/this month, grouped by date or model" is removed. Users wanting this must reconstruct it from `tokens.log` summaries (`IsSummary` lines).
- **Behavioral change to the status line format** — ripples through golden tests and any scripts that grep the `Ready` line (the README documented the 4-field format).
- **`estimate_cost` loses its historical record** — it becomes a point-in-time estimate of the current session only; no ledger history accumulates.
- **Docs churn**: README metrics example, `docs/sop/technical/economic_awareness.md` (Self-Healing Ledger section), and the ADR index updated in the same change.

## Alternatives considered

- **Keep the ledger, remove only the tool.** Rejected: the `DailyCost` metric remained the ledger's second reader, so nothing was simplified and the hot-path read stayed.
- **Keep the tool, remove only the metric.** Rejected: the tool kept its write side effect and the ledger remained alive.
- **Retain the ledger as write-only for future analytics.** Rejected on YAGNI grounds: no consumer or roadmap item exists; dead writes with recovery/retention machinery are worse than no writes. The tokens log is the durable audit trail.
- **CQRS-style split (read model over tokens.log).** Deferred: if cross-session reporting is ever needed again, it should be rebuilt as a pure query over `tokens.log` (already parsed by `resolveUsageForSummary`), not a separate ledger file.
