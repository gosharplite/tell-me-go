# ADR-066: Infrastructure→Application Dependency Inversion — Six-Type Port Relocation, internal/app Construction Seam, and the di → ui Carve-Out (TD-1, M-scope)

**Status:** Accepted
**Date:** 2026-08
**Related:** [Issue #1364](https://github.com/gosharplite/tell-me-go/issues/1364) (supersedes [#1363](https://github.com/gosharplite/tell-me-go/issues/1363), which superseded [#1362](https://github.com/gosharplite/tell-me-go/issues/1362)), [ADR-056](2026-08-contract-home-and-transitive-closure-gate.md), [ADR-062](2026-08-encoding-relocation-and-tools-infrastructure-gate.md), [ADR-063](2026-08-sop-documentation-governance.md), [ADR-054](2026-08-catalog-cross-reference-reporting-contract.md), [ADR-064](2026-08-ports-shared-kernel-registry-gate.md), [ADR-065](2026-08-infra-seam-contract-leaf-rule.md)

## Context

TD-1 (M-scoped): the infrastructure layer imports application-layer packages —
`internal/infrastructure/*` production files import `internal/agent` and
`internal/application/*` — a dependency inversion across the Clean Architecture
boundary. This is a structural/layering-clarity defect only (`[TECHNICAL DEBT]`):
the code compiles, tests pass, and `make check-full` was green before this work.

Two adversarial grill rounds produced the corrected plan (verdict: **proceed
with changes — execute as corrected**); the #1363 round transcript at
<https://gist.github.com/gosharplite/c6daca67fb97aa7c4c52404cbba0dc66> is
**binding context** for this record. The grill proved the prior plan (#1363) as
written **failed its own acceptance criterion 1 on rows 2, 3, and 5**; this
record reflects the corrected, executable plan.

### Edge inventory (verified exhaustive)

**7 import lines / 4 targets / 3 application families.** Rows 1–5 are in
criterion-1 scope (removed by relocation); rows 6–7 are ADR-carved (kept,
documented — Decision 3).

| # | Line | Target | Family | Fate |
|---|------|--------|--------|------|
| 1 | `factory/chatter.go:13` | `internal/agent` | agent | closed — file move to `internal/app` (C2+C3) |
| 2 | `di/chat_factory.go:9` | `internal/agent` | agent | closed — last reference (constructor call) dies (C2+C3) |
| 3 | `di/container.go:13` | `internal/agent` | agent | closed — all three refs become `ports` (C1) |
| 4 | `di/toolchain_factory.go:11` | `internal/agent` | agent | closed — `Capturer` → `ports.CapturerInteractor` (C1) |
| 5 | `di/suggestion_factory.go:11` | `internal/application/suggestions` | application | closed — file deleted, delegation to `internal/app` (C2+C3) |
| 6 | `di/ui_factory.go:16` | `internal/ui` | ui | **kept** — ADR-carved per ADR-062 D5 |
| 7 | `di/ui_factory.go:17` | `internal/ui/tui` | ui | **kept** — ADR-carved per ADR-062 D5 |

The governance hole: `familyOf()` returns `""` outside `internal/domain/`, so the
transitive gate **cannot express an infra→application edge** as a whitelist row
(ADR-056's own Context). An infra→application edge is therefore either removed
by construction or adjudicated by a new ADR — there is no third option.

## Decision

### 1. The M execution

The M-scope plan ships as three commits, gates green at every boundary:

- **C1 — six-type port relocation (closes rows 3 + 4).** `SessionLifecycleManager`
  (agent/service.go), `CapturerInteractor` (agent/chat.go), `ChatService`
  (interface only), `ChatCommand` (required by `ChatService.ProcessMessage`),
  **`ChatServiceConfig`** (mandatory — the config is *named* at a di
  construction site, so keeping it in agent fails criterion 1), and
  `LogFileOpener` (referenced by `ChatServiceConfig.LogOpener`; also has a
  type-aware implementer in `infra/persistence`) move to
  `internal/domain/ports` under the ADR-056 domain-home criterion (their
  consumers span non-importable layers; `di` references count as consumers).
  Registry placement per ADR-064: 4 interfaces into existing families
  (`ChatService`, `SessionLifecycleManager` → Agent Lifecycle;
  `CapturerInteractor` → User Interaction; `LogFileOpener` → Persistence &
  State) + 2 Supporting entries (`ChatCommand`, `ChatServiceConfig`); family
  count stays 8 ≤ N≤12; bijection holds. The concrete `*chatService` stays in
  agent (it calls `agent/session.Run`); `agent.NewChatService(cfg
  ports.ChatServiceConfig) ports.ChatService` becomes the constructor
  signature.
- **C2+C3 — `internal/app` birth + factory slim (closes rows 1, 2, 5, atomic).**
  `internal/app` is created hosting the construction seams that di (and, for
  the old factory, the factory package) must reach: `NewChatter` (moved from
  the deleted `internal/infrastructure/factory/chatter.go` with its
  `ChatterComposer` contract), `NewChatService` (`app.NewChatService(ports.
  ChatServiceConfig{...})` — the seam through which di reaches the concrete
  `*chatService` without naming `internal/agent`), and `BuildSuggestionService`
  (suggestion construction: `suggestions.NewMultiSourceSuggestionService`,
  `history.NewGlobalPromptTracker`, `history.NewNoOpTracker`). `di/
  suggestion_factory.go` is deleted; `Bootstrapper.GetSuggestionService`
  delegates to app. `internal/infrastructure/factory` is slimmed to
  `health_manager.go` + tests only — `di/health_factory.go` keeps calling
  `factory.NewHealthCheckManager` (a legal infra→infra lateral edge;
  unexported `*defaultHealthCheckManager` does not move). The `chatFactory`
  seam uses the **file-stays variant** (Decision 10).
- **The five closed edges** (rows 1–5) and the ADR-carved `di → ui` rows 6–7
  are the complete fate of the verified-exhaustive inventory above.

### 2. The declined X

The full X-scope — relocating di's five infra factories (`sessionFactory`,
`toolchainFactory`, `telemetryFactory`, `historyFactory`, `healthFactory`) and
the `ui` rows 6–7 — was **rejected**: churn vs. benefit for a 2-line `ui` edge.
The M-scope was selected because the five in-scope rows are the application-layer
reach; the `ui` rows are infrastructure-adjacent adapters bound at the
composition root and are governed instead by the ADR-062 Decision 5
Gate-Exception Contract (Decision 3). Precedent: ADR-062 D5 (exception requires
a new ADR + whitelist edit, same change) and ADR-056 (di references count as
consumers — a realignment always includes unwiring the DI composition root).

### 3. The `di → ui` carve-out (ADR-062 Decision 5 Gate-Exception Contract)

Rows 6–7 (`di/ui_factory.go` → `internal/ui` and `internal/ui/tui`) are an
edge that is neither a dependency-free utility nor an injectable adapter: the
`uiFactory` binding is a composition-root construction of UI adapters, kept in
di. Per ADR-062 D5, the exception is **this ADR** (architect-sanctioned,
ADR-cited) — the whitelist edit for the carve-out ships in the same change as
the relocation work. **Criterion-1 scope statement: only the agent and
application families are in scope for removal; the `ui` family is explicitly
out of scope and carved out.** No `uiFactory` / `suggestionFactory` relocation
of rows 6–7 — note the *suggestion service construction* (row 5) does move; the
*suggestionFactory seam* rows are 6–7.

### 4. Criterion-2 escape-hatch invocation

The residual **dual construction** is the recorded escape-hatch use:
`uiFactory` lives in di (it constructs `ports.UIRenderer`, `ports.HistoryBrowser`),
while the suggestion/chat/chatter construction moved to `internal/app`. The
architecture therefore has two construction sites for application-layer
objects — di (UI adapters) and app (chatter/chat-service/suggestions) — and
this record acknowledges that split as deliberate for the M-scope, with the
full unification deferred (the declined X, Decision 2).

### 5. `internal/app` born `layerUnknown`

The new package is born **`layerUnknown`** — the ADR-063 known-gap class
(`familyOf()` and the layer classifier return `""`/unknown outside their mapped
families), **deliberately accepted** for the M-scope. The X-option — adding an
`app` classifier mapping so the layer gate recognizes the package — is recorded
as the **rejected alternative**: no `classifyInternal` change and no
`exitInternalLayerNames` change ship with this work (explicitly out of scope in
the issue). `internal/app`'s governance rests on the transitive-gate whitelist
row added at its birth (a pure-bloat payer; the STRICT gate fails from the
moment app exists without it), not on layer-gate classification.

### 6. Two-sense "composition root" disambiguation

The term "composition root" has two distinct meanings that collided in the
governance vocabulary; this ADR fixes the collision:

- **Layer-exemption sense**: the architecture-gate composition-root carve-out
  that exempts di/factory from the layer rules (`isCompositionRoot` /
  `isExitCompositionRoot` — the exit query's composition-root exclusion).
- **Infra-lateral sense**: the `## infra-root:` whitelist meaning — di
  composes infra adapters laterally (the infra packages di may bind).

The whitelist comment now cites this disambiguation verbatim: `# infra-lateral
composition: di composes infra adapters; no application-layer imports
(ADR-066)`. The `## infra-root: di` entry itself is unchanged (all 11 targets,
including `factory`), comment-only edit.

### 7. Exit-query ledger record

`exitStayRationales` gains **one new stay**: `ChatService` ("di signature —
chatFactory/GetChatService — full criterion cross-layer", added in C1;
`TestVerifyPortsRegistryStayKeyLiveness` length assertion 6→7 in the same
commit). The **six existing entries go dormant at C1** — `Capturer`,
`UIRenderer`, `HistoryRenderer`, `HistoryBrowser`, `HistoryEditor`,
`SessionFinalizer` — each gained ports-internal usages from the relocated
declarations (the moved interfaces now reference them within
`internal/domain/ports`). **Reversibility rationale**: dormancy is not
permanence — removing a dormant entry is an **owner decision**, not part of
this issue; each entry remains the historical record of an ADR-056
full-criterion stay. The issue's original "No exitStayRationales changes"
statement is **explicitly superseded** by this record.

### 8. Coverage-pin-vs-partition reading of the coordination rule

The catalog coordination rule ("any legitimate catalog change … edits the
partition test in the same PR") applies to **complexity pins**, which
`verify-nonfix-catalog` mechanically verifies against the complexity
partition in `real_nonfix_catalog_test.go`. The three re-anchors shipped with
this work are **coverage pins** — `container.go` → 196–199 (C1), →
194–197 (C2+C3), and `chatter.go:107-121` → its new `app` home (C2+C3) — and
coverage pins are prose policy per ADR-054 consequences (the coverage matcher
has no name axis to verify against). **One note covers all three re-anchors**:
`real_nonfix_catalog_test.go` is complexity-only, so the coordination rule's
partition-test clause **does not apply** to these coverage re-anchors — no
partition-test edits were required or made (each re-anchor still landed in the
same commit as its line-shifting edit, per the drift coordination rule).

### 9. Phantom fixture-sync note

`transitive_gate_test.go`'s `pinnedWhitelist` fixture is a **decoupled
parser-format pin**: it exercises the whitelist *parser* (five const-parsing
tests, including its 2-infra-root assertion) and has **no live-file coupling**
to `docs/architect/TRANSITIVE_IMPORT_WHITELIST.md`. It must **not** be
"synced" when the whitelist ledger changes — future implementers must not
waste a commit on it.

### 10. Rejected options

- **Stay removal** — the six dormant `exitStayRationales` entries are kept
  (Decision 7); removal is an owner decision, not part of this issue.
- **Mock relocation** — `mockSessionLifecycleManager` stays in
  `agentinternal` (config-family whitelist constraint: its
  `BuildSessionDependencies` signature carries `*domain_config.Config`, and
  agenttest's 8-family whitelist admits no config; the stale mock doc comment
  is corrected in C1, not the mock's home).
- **Fully-parameterized wrapper** — rejected for the `chatFactory` seam;
  parameterizing away the agent dependency entirely was not selected.
- **Wholesale `defaultChatFactory` move** — rejected: the wholesale move is
  non-compiling (`defaultChatFactory` holds the unexported `uiFactory`
  interface). The **file-stays variant** was selected: the seam file stays in
  di with per-line type swaps to `ports`.

## Consequences

**Positive:** the governance hole — `familyOf()` cannot express
infra→application edges, so no whitelist row can adjudicate them — is closed
by construction: relocation removes rows 1–5, and the ADR-062 D5 carve-out
documents rows 6–7. The `grep` acceptance criterion (no
`internal/infrastructure/*` production import of `internal/agent` or
`internal/application/*`) is satisfied and outranks any implementation report.
`internal/app` gives the composition root an application-layer-free
construction seam; `internal/infrastructure/factory` is slimmed to
ports-only health management.

**Negative:** the residual `di → app → agent` application-layer reach — di
reaches the concrete `*chatService` through `app.NewChatService`, and app
imports agent for `NewChatter`/`NewChatService` — is an accepted cost of the
file-stays variant and the M-scope (the full X would remove it). The six
dormant exit-query ledger entries remain as the historical record, each
gaining ports-internal usages from the relocated declarations. The
`layerUnknown` classification of `internal/app` is a recorded, deliberately
accepted known-gap (ADR-063 class) with no classifier change in M.

## Addendum: Issue #1369 — Adoption of the X-Option and internal/app Layer Classification (2026-08)

Issue #1369 completed the architecture transition initiated by ADR-066 by decoupling `internal/app` and adopting the Decision 5 X-option:
1. **Elimination of Infrastructure Dependencies**: `internal/app` was completely decoupled from `internal/infrastructure/*`. Concrete adapter constructions (`infra_llm.NewSummarizer`, `infra_config.NewFileConfigWatcher`, `history.NewGlobalPromptTracker`, `infra_persistence.NewDomainFS`, `telemetry.RegisterTraceSubscriber`) were relocated to the composition root in `internal/infrastructure/di`. Read-only path registration for `docs/skills` and `.skills` was centralized in `di.defaultSessionFactory.setupSecurity`.
2. **Adoption of the Decision 5 X-Option**: `internal/app` is now formally classified as `layerApplication` in `internal/tools/analysis/architecture.go`. The Clean Architecture rule ("Application layer must not depend on Infrastructure") is now actively evaluated and satisfied for `internal/app`. (Note: "The declined X" of Decision 2 — relocating di's five infra factories and the `di → ui` carve-out — remains untouched and preserved).
3. **Exit Query Realignment & Stays**: `internal/app` was added to `exitInternalLayerNames`. `ChatterComposer` and `SuggestionService` were recorded as documented cross-layer stays in `exitStayRationales` under ADR-056 Decision 1, and the stay-key liveness count assertion was updated from 7 to 9.

- **Amended 2026-09 (#1468):** naming ruling — `internal/app` is the single application-layer namespace; application services live as `app/` sub-packages (`internal/app/suggestions`); the `internal/application/*` namespace is retired. Historical table row 5 above is preserved verbatim. See [Issue #1468](https://github.com/gosharplite/tell-me-go/issues/1468).
