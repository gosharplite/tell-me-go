# ADR-056: Domain-Home Criterion for Cross-Layer Contracts; Transitive-Closure Gate; Context-Family Extraction

**Status:** Accepted
**Date:** 2026-08-08
**Related:** [Issue #1300](https://github.com/gosharplite/tell-me-go/issues/1300), [ADR-003](2026-01-domain-decomposition-strategy.md), [ADR-026](2026-04-session-context-subpackage-extraction.md), [ADR-040](2026-05-session-extraction-completion.md), [Issue #1299](https://github.com/gosharplite/tell-me-go/issues/1299), [Issue #1297](https://github.com/gosharplite/tell-me-go/issues/1297)

## Context

`internal/domain/ports` is the project's largest cross-layer contract hub. This ADR settles three architectural questions raised by the ports-hygiene grilling: where a cross-layer contract *should* live, how to enforce the *transitive closure* of the architecture gate, and what to do with the context-pipeline family stranded in `ports`.

### The retired premise

The original framing — "~50 types aggregating 8 sub-domains" — was metric-churn. Measured values:

- `internal/domain/ports` actually contains **70 types**.
- The transitive closure of `internal/domain/ports` spans **9 domain families**: config, events, llm, persistence, pricing, security, skills, telemetry, tools. The 9th family (telemetry) enters via `domain/events/types.go:13` → `internal/domain/telemetry`.
- **223 measured importers**: 103 prod + 102 test inside `internal/` + 18 in `tests/` (the issue's 222 was pre-merge).
- The ripple claim died twice: co-occurrence is 51% (113/222 importers use types from 2+ families) with a tail of 16 importers — not the near-total cross-family coupling the premise implied.

### The correct diagnosis

`internal/domain/ports` is a **structurally-required cross-layer contract home with unenforced hygiene and an unresolved convention conflict**. The hub exists because the layer rules (application↛infrastructure, infrastructure↛application) force a domain home for DI-touched contracts; it is not accidental. The conflict is with the project's oldest interface convention, [ADR-003](2026-01-domain-decomposition-strategy.md) Rule #1, verbatim:

> Consumer-Defined Interfaces: Interfaces must be defined in the package that consumes them (the facade or orchestration package), not where they are implemented.

Rule #1 predates the layered architecture and conflicts with it: the layer rules pull contracts into a domain home, while Rule #1 pulls them to the consumer — and neither rule is enforced against the other.

### The phantom audit

The documentation-to-code audit found drift in both directions:

- **4 documented-but-nonexistent types**: `SessionDependencies`, `LLMDependencyProvider`, `PersistenceDependencyProvider`, `InfrastructureDependencyProvider` are listed in `ports/doc.go` but do not exist in the package.
- **Omissions**: e.g., `ChatterComposer` exists at `ports/session.go:141` but is undocumented in `ports/doc.go`.

### Pair-overlap table

Measured pair overlaps among the family consumer sets:

| Pair | Overlap |
|------|---------|
| browser ⊂ history | full containment — every browser-family consumer is also a history-family consumer |
| history ∩ repository | 10 importers |
| history ∩ logger | 14 importers |

### ADR-026 misattribution corrected

[ADR-026](2026-04-session-context-subpackage-extraction.md):44-45 claims:

> External consumers are limited to two files: `agent.go` and `session_manager.go`. No transitive consumers.

That claim is **false today**. The actual consumer set of the context-pipeline contract is: agent, orchestrator (`engine.go`, `engine_types.go`), skills, executor, agenttest, and the session tests.

### di/doc.go Maintainer Guidance

`internal/infrastructure/di/doc.go` Maintainer Guidance step 1, verbatim:

> Define the domain interface in internal/domain/ports/ if it does not already exist.

This is the **opposite direction** of the exit criterion below: the DI composition root is instructed to place every new dependency contract in `ports` regardless of consumer locality — precisely how the hub grows unexamined.

### The ADR-040 precedent

[ADR-040](2026-05-session-extraction-completion.md) already established the pattern this ADR codifies: the `ConfigWatcher` interface moved to `internal/domain/config/` and its implementations to `internal/infrastructure/config/` — a contract whose consumers and implementers are single-layer, living in the layer's own package.

## Decision

### 1. Cross-layer criterion: a seam is domain-home iff its consumers span non-importable layers

- A seam's home is the domain layer **iff its consumers span non-importable layers** (e.g., both application and infrastructure, which cannot import each other). Otherwise the seam does not need a domain home.
- **Exit criterion:** when the full set of consumers *and* implementers of a seam is single-layer, the seam moves to that layer's package — applying [ADR-003](2026-01-domain-decomposition-strategy.md) Rule #1 and codifying the [ADR-040](2026-05-session-extraction-completion.md) precedent.
- **`di` references count as consumers.** The exit fires when the full set *including di* is single-layer; realignment therefore always includes unwiring the DI composition root, and the `di/doc.go` Maintainer Guidance step 1 (see Context) is superseded for moved seams.
- **Fluid membership.** Ports membership is recomputed from the criterion, not frozen. The realignment strike recorded **3 stay-rationales** — seams proposed for realignment and struck down because they are cross-layer (they fail the exit test): **`SuggestionProvider`** (consumers `cli`+`di`, implementers `di`), **`SystemMetricsProvider`** (consumers `cli`+`di`, implementers `di`/`telemetry`), and **`HistoryRenderer`** (consumers `cli`+`di`, implementers `di`/`telemetry`/`ui`). Each stays in `internal/domain/ports` because its consumer set spans non-importable layers under the strict Decision-1 criterion (the full set including di is multi-layer).
- **Exit-query tension (recorded, deferred).** The exit query wired into `make dead-code` (composition-root-excluded lens) surfaces **`HistoryRenderer` among 7 application-layer exit candidates** — excluding `di`/`factory` leaves a single-layer set. The two lenses genuinely diverge; which of the 7 candidates (incl. `HistoryRenderer`) are realignment-worthy vs accepted-cost stays is adjudicated in the gate-measured follow-up, not resolved here.
- **Derived-constant hub.** The ports whitelist used by the closure gate is the *derived* 9-family constant of the transitive closure — generated from the measurement, not hand-maintained.

### 2. Transitive-closure gate: consumer-level whitelists, default-strict, report-only until ratified

The current architecture gate measures **direct imports only**; the transitive closure of `internal/domain/ports` is unenforced, which is how closure grows silently.

- **Gate v1** is a consumer-level whitelist: an architect-curated file following the `INTENTIONAL_NON_FIXES.md` pattern, listing approved closure edges.
- **Default-strict posture** with a **separate report-only flag** (distinct from the existing `-strict-arch` flag): in default mode the gate fails on any unlisted closure edge; the report-only flag emits the same findings without failing.
- The report **separates "decision required" rows from "approved constant" rows**: the **26 payers** estimate is a **hypothesis** — the report is authoritative — while the **~196 self-justifying importers** are approved constants needing no per-importer review.
- **`events→telemetry` is the first recorded whitelist decision.**
- Family-level whitelists are deferred — no schema exists for them yet.
- The **architect owns whitelist maintenance**.

### 3. Context-family extraction (ships now)

The context-pipeline family leaves `internal/domain/ports`:

- **4 pipeline types move** from `internal/domain/ports/session.go` to `internal/agent/session/context`: `ContextTransformer`, `ContextRequest`, `ContextMetadata`, `PruningPolicy`.
- **`ResultStrategy` becomes a local unexported interface** in `internal/agent/executor/strategy.go` — its true consumer, with zero references in the context package.
- **Both aliases are dissolved**: `type Metadata = ports.ContextMetadata` (`pipeline.go:20`) **and** `type request = ports.ContextRequest` (`pipeline.go:23`, missed in round 1).
- **One new agent-layer edge is recorded as unavoidable**: `skills→sessctx` — `ContextRequest` is in `Transform`'s signature, so no consumer of `ContextTransformer` can avoid the edge. The predicted `agenttest→sessctx` edge did **not** materialize: the agenttest transformer double was relocated into the context package's own test files (`mock_transformer_test.go`), because agenttest importing `context` created a Go test import cycle (12 context internal-test files import agenttest → `context_test → agenttest → context`). The "unavoidable" prediction was therefore wrong for agenttest: the edge was avoidable by localizing the test double to its consumer per ADR-021 test-double locality. Note the consequence: the canonical-mock-home rule applies to *ports-family* doubles; the context family is no longer a ports family.
- **Corrected consumer set recorded**: ADR-026's "two files" claim is false (see Context).
- **`TurnState.Metadata` naming**: `engine_types.go:170` declares `TurnState.Metadata *sessctx.Metadata`; `Turn` has no `Metadata` field — the #1299 catalog conflation fix.

## Consequences

**Positive:**

- The hub shrinks by **5 types** (4 moved + `ResultStrategy` localized).
- The `executor→ports` **ResultStrategy reference** vanishes; executor still imports `ports` for `Logger`/`NoOpLogger` (decorators.go). The hub's executor surface shrinks to observability types only.
- Hygiene is enforced: the exit criterion, the derived 9-family whitelist, and the closure gate make ports membership a decided, reviewable fact rather than an accumulation.
- Accidental closure growth becomes **decided policy** — every new closure edge is a recorded whitelist decision owned by the architect.

**Negative:**

- **One new agent-layer edge** (`skills→sessctx`) is recorded — unavoidable while `ContextRequest` is in `Transform`'s signature. The predicted `agenttest→sessctx` edge was avoided by localizing the transformer test double to the context package (`mock_transformer_test.go`), per ADR-021 test-double locality.
- **26-payer decisions pending**: the "decision required" rows of the report are hypotheses until the report is run and ratified.
- The gate is **report-only until ratified** — it does not yet fail the build in default mode.
- **Canonical-mock-home rule**: `agenttest` remains the single canonical home for ports-family test doubles; consumers must not re-implement canonical doubles.

**Related work:**

- [Issue #1299](https://github.com/gosharplite/tell-me-go/issues/1299): the `*sessctx.Manager` coupling is an accepted cost — it is agent-layer and satisfies the cross-layer criterion.
- [Issue #1297](https://github.com/gosharplite/tell-me-go/issues/1297): merged; orthogonal to the closure ledger.

### Post-ratification record (2026-08, follow-up)

- **Closure semantics ratified**: merged (prod+test) closure is canonical. The gate
  classifies each package's compiled footprint (test binaries compile prod+test
  together); test-edge excess is explicitly annotated in whitelist rationales.
  V2: report annotates prod vs test-edge excess; family-level whitelists collapse
  the P1/P2/P5 patterns.
- **Gate ratification**: 39/39 decision-required rows accepted (whitelist additions
  with per-row rationales); 0 splits — every excess traced to the recorded
  events→telemetry decision, domain-internal hub coupling (llm↔events↔tools),
  infra→domain inversion reach, or test-double coupling. Gate flipped to strict.
- **Exit-query adjudication (7 candidates)**: 1 realign (SessionConfig →
  internal/agent/session — the only candidate whose full consumer+implementer set
  including di is single-layer) / 6 keep. The six stays are full-criterion
  cross-layer: Capturer (di signature), HistoryBrowser/HistoryEditor/UIRenderer
  (di uiFactory binding), SessionFinalizer (di-implemented sessionDeps),
  HistoryRenderer (di + telemetry — recorded stay confirmed; the query's
  composition-root-excluded single-layer status is realignment-eligibility, not an
  exit). Realignment-strike stay-rationales enumerated above.
- **SessionConfig realignment** tracked as follow-up work (not a non-fix).

### SessionConfig realignment completed (2026-08)

**SessionConfig realignment completed** — interface moved to `internal/agent/session` per ADR-003 Rule #1; `ports.SessionConfig` deleted; move is edge-neutral for the transitive gate (no whitelist change).
