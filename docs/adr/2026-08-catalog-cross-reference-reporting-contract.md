# ADR-054: Catalog Cross-Reference Contract for Coverage & Complexity Reports

**Status:** Accepted
**Date:** 2026-08-06
**Related:** [PR #1293](https://github.com/gosharplite/tell-me-go/pull/1293), `docs/architect/INTENTIONAL_NON_FIXES.md`

## Context

The analysis tooling (`get_detailed_coverage`, `get_code_health`) previously reported every uncovered block and every over-threshold complexity function as an actionable gap. This produced a persistent noise floor: the same ACCEPTED Intentional Non-Fix entries (delegation wrappers, unreachable `json.Marshal` branches, interface stubs) were re-flagged on every audit, and "high priority" did not mean "needs work."

Commit `73155701` introduced a cross-reference between the reports and the architect-curated `INTENTIONAL_NON_FIXES.md` catalog. This ADR records the resulting output contract, which is a user-visible change in what the health tooling reports.

## Decision

### 1. Cataloged = excluded from actionable counts

An uncovered block or over-threshold function whose location falls within an ACCEPTED catalog entry's references is **cataloged**: it is excluded from the High/Medium priority counts and the actionable alert list, and rendered in a separate `[CATALOGED GAPS (ACCEPTED)]` / `Cataloged (ACCEPTED) over threshold` section so the reader can verify the acceptance rationale.

- "High priority (Architectural)" and "Medium priority (Technical Debt)" now mean **not cataloged** — i.e. genuinely actionable.
- A catalog load failure degrades gracefully: every gap is treated as actionable (logged via `slog.Warn`), so the tool never silently hides gaps when the catalog is unreadable.

### 2. Text vs. JSON asymmetry is intentional

- The **text** report (`get_detailed_coverage`) excludes cataloged blocks from actionable counts and renders them in the dedicated section.
- The **JSON** renderer (`get_detailed_coverage` with a JSON consumer) is additive only: it tags each block with `catalog_title` but does **not** filter. Machine consumers may filter on the field themselves; removing data from the machine-readable output would be a breaking contract change with no migration path.

This asymmetry is deliberate and must not be "fixed" by filtering the JSON output without a new ADR.

### 3. Status semantics

`checkComplexity` returns `GOOD` when there is nothing actionable — including when every over-threshold function is cataloged (ACCEPTED). `"0 Alerts"` is never emitted; `recommendComplexity` only fires on statuses containing `Alerts`, `ERROR`, or `TIMEOUT`, so a cataloged-only result cannot produce a self-contradictory "Refactor high-complexity functions" recommendation.

## Consequences

- Audits are faster and more honest: the noise floor is gone, and reviewers see only uncataloged gaps.
- The catalog becomes load-bearing: stale or drifting references (see the Drift Policy in `INTENTIONAL_NON_FIXES.md`) can misclassify a gap. Re-verification on PRs touching referenced files is mandatory.
- JSON consumers must handle the new optional `catalog_title` field (additive, `omitempty`).

## Related

- `docs/architect/INTENTIONAL_NON_FIXES.md` — Acceptance Classes glossary and Drift Policy
- Quality domain model `AcceptanceClass` enum — `docs/domain-model/quality.modelith.yaml`
