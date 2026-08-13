# ADR-064: Domain/Ports Shared-Kernel Registry Gate and Split Deferral (D1=DEFER)

**Status:** Accepted
**Date:** 2026-08
**Related:** [Issue #1339](https://github.com/gosharplite/tell-me-go/issues/1339), [Issue #1341](https://github.com/gosharplite/tell-me-go/issues/1341), [Issue #1342](https://github.com/gosharplite/tell-me-go/issues/1342), [Issue #1343](https://github.com/gosharplite/tell-me-go/issues/1343), [Issue #1344](https://github.com/gosharplite/tell-me-go/issues/1344), [ADR-056](2026-08-contract-home-and-transitive-closure-gate.md), [ADR-062](2026-08-encoding-relocation-and-tools-infrastructure-gate.md)

## Context

Finding F1 (#1339): `internal/domain/ports` is a widening shared kernel. Two adversarial grill rounds (40 questions) verified the live state: **46 interface exports** and **31 non-interface exports** (17 named types + 7 consts + 3 sentinel-error vars + 4 funcs, after the Phase 1 metrics-func relocation) across **8 families**. The shared kernel grows because every cross-layer contract funnels into `ports` — ADR-056's domain-home criterion forces it — with no gate enforcing membership hygiene.

Phase 1 of this issue already shipped the clean pair extraction: `FormatMetricsLine`/`FormatFinalLine` relocated to `internal/pkg/metricsfmt` (the only two exports whose parameter and return types were all non-ports types), plus its transitive-gate whitelist entry (`Dom = {events, llm, telemetry, tools}`, `directDom = {events, llm}`).

The split itself (D1) is **deferred**. The prior framing "edge-neutral / zero residual risk" was corrected by the second grill round: the split would fail the strict transitive-closure gate on the first `make check-full` because the merged closure of any new sub-package is a new tracked consumer. This ADR records the deferral, the enforcement gate that pins the roster today, and the pinned contracts that bound the eventual PR-B mechanical verdict.

## Decision

### 1. D1 = DEFER — the ports split is deferred, not rejected

The physical split (relocating families into sub-packages under `ports/`) is deferred. **D2** = the eventual sub-package shape; **D3** = the alias-transition migration mechanism; **D4** = enforcement by the `verify-ports-registry` gate (the ADR-056 exit query remains a layer lens, not an enforcement instrument).

### 2. The scan-driven registry and its gate

The membership roster is authored as a `// # Registry` block in `internal/domain/ports/doc.go` (8 `## Family:` buckets + `## Supporting`), enumerated from the live indexer (`isPortsPackagePath` + `isInterfaceType` + `symType`), never hand-transcribed. The new `verify-ports-registry` arch-tagged gate (Makefile `check`/`check-full`, standalone — not a `test:` prerequisite) enforces:

- **Grammar**: `// ## Family: <Name>` markers, `// ## Supporting`, one name per bullet; blank `//` separators (gofmt) skipped; duplicate family names, duplicate symbols, and non-conforming directives are hard parse errors (mirroring `parseConsumerHeading`).
- **Bijection**: no phantom (every listed name resolves to a live declaration), no omission (every export in exactly one bucket), no duplicate symbol; interface exports → exactly one family, non-interface exports → `Supporting` (classification via `symMeta.isInterfaceType`, never judgment). Universe: `symType ∈ {Type, Function, Constant, Variable}`; `Method` and `Unknown` out of scope in both directions.
- **Stay-key liveness**: every `exitStayRationales` key must name a declared ports interface (closes the silent 6→5 deletion gap).
- **N ≤ 12**: the gate counts distinct family markers and fails if > 12.

The audit folds, never mints: the 7 formerly family-less interfaces found natural homes (`HistoryBrowser`/`HistoryEditor` → Conversation Persistence; `ProgressRenderer`/`EventSubscriber` → User Interaction; `SessionFinalizer` → Agent Lifecycle; `ClientFactory` → Agent Lifecycle; `FilterableItem` → Persistence & State); `SuggestionProvider` was de-duplicated (Suggestions only); the former "Infrastructure Wiring" family was folded (`HistoryManagerProvider` → Persistence & State, `SuggestionProvider` → Suggestions), yielding N = 8.

### 3. Supporting admission rule (five clauses)

A non-interface export stays in `Supporting` iff it satisfies ≥1 clause:

- **(a)** parameter/return/embedded field of a ports interface or another Supporting type — signature inspection (not `GetUsages`), walking `*types.Struct` fields and `*types.Signature` params/results, ports-only scope (terminate at non-ports types), clause-interleaved seed set.
- **(b)** const/var vocabulary — default + bijection; the only expulsion vector is a `func`.
- **(c)** exported func decl/type whose signature references ≥1 ports type.
- **(d)** named type whose method set implements a ports interface (`GetImplementations`).
- **(e)** cross-layer reference — usage sites span non-importable layers, di-included (`GetUsages` + `classifyExitLayer`, no `isExitCompositionRoot` gate).

Regression fixtures: `ChatterFactory` (clause e), `ClientFactoryFunc` (clause d + non-ports termination), `CaptureOptions`/`ChatterConfig` (func-type signature branch of (a)), `NoOpLogger`/`NoOpTurnsLogger` (clause d).

### 4. `isExitCompositionRoot` is exit-query-only

The composition-root exclusion is a property of the ADR-056 Decision 1 exit query alone. Every other consumer of the indexer's usage/implementation data — clause (e) here, and PR-B's physical consumers below — is **di-included by default**.

### 5. Physical predicates for the deferred split (per-candidate, over the partition's packages, post-split)

- **(i)** single-package share ≥ 60% (median ≤ 1 is a derived corollary, not an independent conjunct).
- **(ii)** consumers with ≥ 4 packages ≤ 15%.
- **(iii)** largest package's consumer share ≤ 50% — shape-dependent (the union of merged families' sets; this is what rejects coarse shapes).

"≤ 50%" is an *observed* statistic, not a contract.

### 6. Scoring, enumeration, and physical consumer

- **Scoring**: `R(Π) = Σ_C (m − |packages(C)|) = Σ_P (|U| − |consumers(P)|)`, unweighted, total, monotone in fineness.
- **Enumeration**: Bell(N) partitions of the N audited families; exactly-Bell(N) fixed-point assertion (before prune); sound post-construction prune ("prune iff a single predicate is provably violated by a monotone bound"); lexicographic-minimum tiebreak over tied admissible candidates.
- **Physical consumer** (for the matrix): `GetUsages(X) ∪ GetImplementations(X.method)`, merged prod+test, package granularity, di-included.

### 7. Terminal-state contract

PR-B (#1344) either ratifies the deferral (data appendix to this ADR) or upgrades to accept via **ADR-065** — a new ADR, never an amendment. Permanent deferral is a valid terminal state.

### 8. Evidence hygiene

#1340's family table is superseded historical context; the 114 > 97 overlap survives as a taxonomy-independent floor; the ADR-056 49% proxy is a lower bound on physical predicate (i).

## Consequences

**Positive:** the shared kernel's membership is pinned and mechanically verified — every export's family/Supporting placement and the N≤12 bound are a decided, reviewable fact; phantom/omission/misclassification drift fails `make check-full`; the metrics-func pair is already extracted; the deferral is recorded with a bounded upgrade path.

**Negative:** the shared kernel itself is not yet split (it remains the largest cross-layer hub); the split's transitive-gate cost (a new tracked consumer per sub-package) is deferred to PR-B's matrix, not eliminated; clause (e) is a safety net that currently admits no live entry (every Supporting export is admitted by clauses (a)–(d)).

**Related work:** [Issue #1344](https://github.com/gosharplite/tell-me-go/issues/1344) (PR-B — matrix tooling + mechanical verdict, own lifecycle; permanent deferral valid).
