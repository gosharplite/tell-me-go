# ADR-075: Scalability Boundary Conditions — CLI-First State Store, Ports-Registry Headroom, Anti-Extension Restatement

**Status:** Accepted
**Date:** 2026-09
**Related:** [Issue #1470](https://github.com/gosharplite/tell-me-go/issues/1470), [ADR-064](2026-08-ports-shared-kernel-registry-gate.md), [ADR-058](2026-08-getwindow-clone-evaluation.md), [ADR-060](2026-08-toolchain-runner-injection.md), [ADR-074](2026-09-process-runner-injection.md), [ADR-039](2026-05-lazy-implementation-index.md)

## Context

The system's scalability boundary conditions are real, deliberate decisions, but they currently live scattered in prose — `docs/architect/environments/environment-management-evolution.md`, the sprawl protocol notes, ADR-064, and ADR-058 — with no single decision record. This ADR records them in one normative place so a future contributor hits an ADR-cited wall — not a surprise — before crossing any of them. There is precedent for boundary and Won't-Do records: ADR-039, ADR-058, and the "record it even if the answer is no" pattern of issue #1467.

The record's nature is intentional and stated up front: **D1–D3 are binding rulings whose re-evaluation demands a new ADR, never a silent change; D4 is reference-only and creates no new rulings.**

## Decision

### D1 — CLI-First State Store: SQLite Single-Writer (Accepted for the CLI Product)

The state store is a single-writer SQLite session DB per `TELL_ME_HOME`; concurrent writers corrupt the session DB. Because the product is CLI-first, the **sequential-only sub-agent protocol** — no parallel `tell-me-go` calls against one session directory — is the accepted operational mitigation. The sprawl corruption warning states it in the environment record at both stages:

- **Stage 4** (Persisting State Across Subshells): "Communication is strictly **sequential** — parallel sub-agent calls are explicitly avoided due to subshell variable inheritance issues."
- **Stage 5** (Session Strategy): "concurrent writes to the same remote SQLite DB will corrupt the session."

The modeled presupposition matches: the quality model's `QualityPipeline` — a sequential gate pipeline, per the `pipeline-stops-on-failure` invariant — and the `Session` entity in `docs/domain-model/tell-me-go.modelith.md` both assume one agent process per session at a time.

**Re-evaluation triggers — each demands a NEW ADR, never a silent change:** (a) a long-lived server mode; (b) multi-process concurrent access to one `TELL_ME_HOME` state DB; (c) remote/concurrent session writes.

### D2 — Ports-Registry Headroom: 8 of N ≤ 12

The ports registry — the `// # Registry` block in `internal/domain/ports/doc.go` (ADR-064) — currently holds **8 family buckets** of the **N ≤ 12** family bound enforced by `verify-ports-registry` (the gate fails if more than 12 distinct `## Family:` markers appear). The mechanics are recorded faithfully to ADR-064:

- new interface exports must fold into exactly one existing family (the audit-fold convention, ADR-064 Decision 2 — folds, never mints) unless they genuinely justify a new family;
- a 9th family consumes headroom against the N ≤ 12 bound, so the proposal must **force consolidation of an existing family first** as the alternative;
- any new non-interface export must satisfy ≥ 1 of the **5-clause Supporting admission rule** clauses (a)–(e) — (a) signature inspection (parameter/return/embedded field of a ports interface or another Supporting type); (b) const/var vocabulary; (c) exported func referencing a ports type; (d) named type implementing a ports interface; (e) cross-layer reference — or it has no bucket under the bijection (ADR-064 Decision 3).

The operative rule: **flag registry capacity and the 8/≤12 position proactively in the proposal, not at gate time** — the gate is the backstop, never the first warning.

### D3 — Anti-Extension Restatement: Import Gates Are Never Extended to `_test.go` Files

This decision restates two existing rulings and creates no new ruling. (Mapping note so the issue shorthand is not propagated: the shorthand "ADR-060 Decision 6" refers to **ADR-060 §9** — ADR-060's §6 is the drift/sentinel section and is NOT the anti-extension ruling; ADR-074's anchor is correctly **§6**.)

- **ADR-060 §9** ("Surviving test-layer construction census + anti-extension decision"): "`verify-tools-toolchain-import` must never be extended to `_test.go` files; doing so would break the 22 sites and destroy the verification the deferral depends on." The 22 surviving `toolchain.NewGoRunner(...)` test constructions — `coverage_parser_test.go` ×17, `health_test.go` ×2, `real_nonfix_catalog_test.go` ×2, `architecture_bench_test.go` ×1 — are the sanctioned **real-adapter-over-mock-executor verification surface**.
- **ADR-074 §6** ("Test-layer asymmetry"): "the `verify-tools-process-import` gate must never be extended to `_test.go` files; doing so would break the helper and the real-adapter test surface exactly as ADR-060 §9 ruled for the toolchain gate."
- **Operational precedent:** #1465 AC-6 / PR #1466 (commit `30a66e1d`) — a tools-layer gate predicate was amended under the same discipline (an exclusion removed, rationale amended; scope not extended to test files).

**Any "let's also check tests" proposal is rejected by default and must arrive as a new ADR.**

### D4 — Concurrency Envelope (Reference-Only)

These references are recorded so they are not re-litigated; this decision creates **no new rulings**. The 55 ACCEPTED complexity/coverage catalog entries in `docs/architect/INTENTIONAL_NON_FIXES.md` stand as-is and are not restated as new decisions. Named references:

- `MAX_CONCURRENT_TOOLS` (`Config.MaxConcurrentTools`) and `TOOL_TIMEOUT`/`toolTimeout` (`Config.ToolTimeoutSeconds`) bound tool concurrency and per-tool execution time;
- per-server MCP `serial` (URL-class defaults, ADR-067 §8) bounds MCP dispatch;
- the per-round `GetWindow` deep clone is **accepted with data** per ADR-058 (candidate-3 accept-with-data; B/op-sole-currency; the ownership-transfer invariant is non-negotiable).

## Consequences

**Positive:** a future contributor meets an ADR-cited wall before crossing any boundary; the D1 triggers are actionable and each mandates a new ADR; scattered prose has a single normative home.

**Negative:** none material — documentation-only; D1–D3 restate existing acceptances (restatement is the point and must not silently tighten or relax them).

**Neutral:** forward-looking boundary record; nothing here is a gap, so `INTENTIONAL_NON_FIXES.md` is intentionally untouched.

## References

- [Issue #1470](https://github.com/gosharplite/tell-me-go/issues/1470) — this ADR.
- [ADR-064](2026-08-ports-shared-kernel-registry-gate.md) — ports registry, N ≤ 12, Supporting admission rule.
- [ADR-058](2026-08-getwindow-clone-evaluation.md) — per-round `GetWindow` deep-clone acceptance (candidate 3, accept-with-data).
- [ADR-060 §9](2026-08-toolchain-runner-injection.md) — surviving test-layer construction census + anti-extension decision.
- [ADR-074 §6](2026-09-process-runner-injection.md) — test-layer asymmetry and the anti-extension ruling.
- [ADR-039](2026-05-lazy-implementation-index.md) — Won't-Do precedent.
- [ADR-067 §8](2026-08-mcp-client-architecture.md) — consent & concurrency defaults (per-server MCP `serial`).
- [Issue #1465](https://github.com/gosharplite/tell-me-go/issues/1465) — AC-6 gate-predicate amendment precedent.
- [PR #1466](https://github.com/gosharplite/tell-me-go/pull/1466) — commit `30a66e1d` (exclusion removed, rationale amended; scope not extended to test files).
- [Issue #1467](https://github.com/gosharplite/tell-me-go/issues/1467) — "record it even if the answer is no" pattern.
- `docs/architect/environments/environment-management-evolution.md` — Stage 4 (Persisting State Across Subshells) / Stage 5 (Session Strategy).
- `internal/domain/ports/doc.go` — the `// # Registry` block (8 family buckets + `## Supporting`).
