<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-026: Decompose `agent/session` Package via `context/` Sub-Package Extraction

## Status
Accepted

## Context

The `internal/agent/session` package has grown into the largest single package in the codebase by LOC and file count. Concrete measurements (taken at the time of writing):

| Metric | Value |
|---|---|
| Total files | 55 (29 production + 26 test) |
| Total LOC | 12,978 |
| Production LOC | ~3,930 |
| Largest production file | `context_manager.go` (543 LOC) |
| Second largest | `context_transformers.go` (480 LOC) |

The package mixes **at least five distinct, cohesive concerns** behind a single `package session` declaration:

| Concern | Files | Production LOC | Cohesion Marker |
|---|---|---|---|
| **Context preparation pipeline** | `context_manager.go`, `context_pipeline.go`, `context_strategy.go`, `context_transformers.go`, `pruner.go`, `gatekeeper.go`, `token_counter.go`, `pipeline_factory.go`, `injector.go` (WarningInjector) | ~2,247 | All implement or coordinate `ports.ContextTransformer` execution |
| **UI Bridge (actor)** | `ui_bridge.go`, `ui_bridge_dispatcher.go`, `ui_bridge_event_queue.go`, `ui_bridge_spinner.go`, `ui_bridge_state_machine.go` | ~757 | Decomposed per ADR-025 — already internally cohesive |
| **Session lifecycle** | `session_manager.go`, `interfaces.go`, `doc.go` | ~431 | Orchestrates the other four concerns |
| **Configuration watching** | `config_watcher.go` | ~301 | File-system-bound, distinct dependency surface |
| **Skill injection** | `skill_injector.go` | ~108 | Bridges `domain/skills` into the context pipeline |
| **Internal tool registration** | `internal_tools.go` | ~163 | Registers chat-history-management tools |

ADR-008 ("Domain Decomposition Strategy") established that packages exceeding ~3,000 production LOC or mixing more than three orthogonal concerns must be decomposed along concern boundaries. `agent/session` violates both thresholds.

ADR-025 ("UIBridge Decomposition") addressed the largest internal monolith inside this package by splitting `ui_bridge.go` into 5 cohesive files. That refactor was scoped to a single file family because cross-package extraction was not yet justified — the file count was not yet pathological. With the package now at 55 files, **the next decomposition step must be to extract sub-packages**, not to further fragment files inside the existing flat namespace.

### Why `context/` First

The Context Preparation concern is the largest, the most internally cohesive, and the most independently testable group:

- **57% of the package's production code** (2,247 / 3,930 LOC) belongs to it.
- All files in the group depend on **`ports.ContextTransformer`** as their primary contract.
- The group has a single internal entry point (`ContextManager.Prepare`) and a single internal builder (`PipelineFactory.BuildStandardPipeline`).
- External consumers are limited to **two files**: `agent.go` and `session_manager.go`. No transitive consumers.
- The group has its own test fixtures (`test_helpers_test.go`, `context_manager_*_test.go`, `coverage_expansion_test.go`) that already conceptually scope to context-pipeline behaviour.

Other candidate groups (UIBridge, Skills, Config-Watcher) are smaller, more entangled with `session_manager.go`, or already structurally decomposed. Extracting them is deferred to follow-up ADRs.

### Risks Identified

A pre-flight audit revealed three coupling hazards that the design must address:

1. **`internal_tools.go` depends on `ContextManager`.** It calls `NewInternalTools(cm *ContextManager)` and `RegisterInternal(r tools.ToolRegistrar, cm *ContextManager)`. After extraction, this file must either move with `context/` or import it.
2. **`PipelineFactory` constructs `skillInjector`, `WarningInjector`, `TokenGatekeeper`, `HistoryPruner`.** All four must live in the same package as the factory, or the factory must be split.
3. **`export_test.go` uses unexported types** (`sessionManager`, `UIBridge`). Cross-package tests cannot reach them. Fixtures shared between `session/` and `session/context/` must move to a neutral location or be re-exported via `_test.go`-only helpers in each package.

## Decision

We will extract a new sub-package `internal/agent/session/context/` containing the Context Preparation concern. The parent `session/` package retains lifecycle, UIBridge, configuration watching, skill injection, and internal-tools registration. The decomposition is **structural, not semantic** — no behaviour changes.

### Files Moved into `session/context/`

| Old path (`session/*`) | New path (`session/context/*`) | Rename |
|---|---|---|
| `context_manager.go` | `context/manager.go` | `ContextManager` → `Manager` |
| `context_pipeline.go` | `context/pipeline.go` | `contextPipeline` → `pipeline` (unchanged — already lowercase) |
| `context_strategy.go` | `context/strategy.go` | `ContextStrategy` → `Strategy` |
| `context_transformers.go` | `context/transformers.go` | unchanged (already split into multiple types) |
| `pruner.go` | `context/pruner.go` | unchanged |
| `gatekeeper.go` | `context/gatekeeper.go` | unchanged |
| `token_counter.go` | `context/token_counter.go` | unchanged |
| `pipeline_factory.go` | `context/pipeline_factory.go` | `PipelineFactory` → `Factory` |
| `injector.go` (WarningInjector) | `context/warning_injector.go` | filename clarified |

Plus their `*_test.go` siblings (10 files), preserving test coverage in the new package.

### Files Staying in `session/`

`session_manager.go`, `interfaces.go`, `doc.go`, `ui_bridge*.go` (5 files), `config_watcher.go`, `skill_injector.go`, `internal_tools.go`, `export_test.go`, plus their tests.

**Rationale for keeping each:**

- `session_manager.go`, `interfaces.go`, `doc.go` — orchestration and package contract.
- `ui_bridge*.go` — already internally decomposed per ADR-025; not a context concern.
- `config_watcher.go` — independent file-system concern; different lifecycle (deferred to a future ADR).
- `skill_injector.go` — depends on `domain/skills` and `domain/ports`; thin adapter rather than context logic.
- `internal_tools.go` — registers chat-history-management tools; lives at the boundary between `tools.Registry` and `ContextManager`. Needs special handling (see below).

### Coupling Hazard Resolutions

#### 1. `internal_tools.go` and `ContextManager`

**Decision:** `internal_tools.go` stays in `session/` and imports `session/context`. The reasoning:

- It is fundamentally a **tool registration adapter** that happens to need a `ContextManager` reference, not a context-pipeline component.
- Moving it into `context/` would force `context/` to import `domain/tools`, polluting the context layer with tool-registration concerns.
- The import direction `session → session/context` is acyclic and matches the parent-child dependency convention.

After the move:

```go
// session/internal_tools.go
import sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"

func NewInternalTools(cm *sessctx.Manager) *InternalTools { ... }
func RegisterInternal(r tools.ToolRegistrar, cm *sessctx.Manager) error { ... }
```

#### 2. `PipelineFactory` Construction Surface

**Decision:** `PipelineFactory` (renamed `Factory`) moves with the context group and **continues to construct all transformer types in scope**: `TokenGatekeeper`, `HistoryPruner`, `WarningInjector`, plus the inline transformers from `context_transformers.go`.

`skillInjector` is the **one exception** — it stays in `session/` and is injected into the factory as a pre-built `ports.ContextTransformer`. Updated factory signature:

```go
// session/context/pipeline_factory.go
func (f *Factory) BuildStandardPipeline(
    limits events.Limits,
    extras ...ports.ContextTransformer,  // session/ injects skillInjector here
) *pipeline { ... }
```

This preserves the factory's internal construction logic while breaking the `context → domain/skills` import that would otherwise be created.

#### 3. Test Fixture Sharing

**Decision:** Shared test fixtures stay duplicated where minimal, and are promoted to `internal/pkg/testfixtures` only when the duplication burden exceeds ~50 LOC.

`export_test.go` stays in `session/` and continues to expose `sessionManager` and `UIBridge` internals. The Context Preparation tests already use their own fixtures (`test_helpers_test.go`, `context_manager_*_test.go`) and do not depend on `export_test.go` exports. Verified by inspection of `context_manager_test.go`'s import surface.

### Renaming Convention

Per Go style, the package qualifier replaces the type-name prefix:

- `session.ContextManager` → `context.Manager`
- `session.ContextStrategy` → `context.Strategy`
- `session.PipelineFactory` → `context.Factory`

The renames are mechanical and applied via `rename_symbol`, not regex. Functions like `NewContextManager` become `NewManager` to maintain `package.NewType` symmetry. **No behaviour changes.**

### Consumer Update Surface

Verified by inspection of import graph and `find_usages`:

| Consumer | Lines requiring update | Update type |
|---|---|---|
| `internal/agent/agent.go` | ~5 | Type renames + import |
| `internal/agent/session/session_manager.go` | ~8 | Type renames + import |
| `internal/agent/session/internal_tools.go` | ~3 | Type renames + import |
| `internal/agent/session/skill_injector.go` | ~2 | Implements interface from `context/` |

No other production files in the codebase import the affected types (verified via `get_package_graph`).

## Consequences

### Positive

- **`session/` production file count drops from 29 to ~20** — restoring navigability.
- **`session/context/` becomes independently testable** with a clear `Manager.Prepare` entry point. New transformers can be added without touching the parent package.
- **Open/Closed adherence**: adding context transformers requires modifying only `context/`, not `session/`.
- **Establishes a sub-package decomposition pattern** for follow-up extractions (`session/uibridge/`, `session/skills/`, `session/configwatch/`) without requiring further ADRs — they will reference this one.
- **Reduces blast radius for context-pipeline changes** — currently a one-line change in a transformer triggers `go test ./internal/agent/session/...` covering 26 test files. After the split, only `context/` tests run.

### Negative

- **One-time mechanical migration cost** on ~10 production files plus their tests. Mitigated by AST-based `rename_symbol` and the small consumer surface (~18 lines).
- **Slightly deeper import paths** for context types in tests (`sessctx.Manager` instead of `session.ContextManager`). Acceptable trade-off for clarity.
- **`PipelineFactory.BuildStandardPipeline` signature gains a variadic `extras` parameter** to receive `skillInjector` from the parent. This is a breaking change to the factory API, but the only caller is `session_manager.go` — bounded blast radius.
- **`internal_tools.go` becomes the only file in `session/` that imports `session/context`** — a slight asymmetry. Acceptable; alternative (moving it into `context/`) creates worse coupling.

### Neutral

- **No change to public API** of `internal/agent/agent.go` or `internal/cli`. Only intra-package types are renamed.
- **No change to test coverage**. All tests move with their files.
- **No change to runtime behaviour**. This is a purely structural refactor.
- **`verify_architecture` results remain clean** — the new `session → session/context` dependency is acyclic.

## Implementation Plan

Execution is deferred to the operational runbook attached to GitHub Issue #88. The runbook codifies:

1. Pre-flight verification of the import graph state.
2. ADR-026 acceptance commit (this file).
3. Move-then-rename sequence (one file pair per commit for bisect-friendliness).
4. Consumer updates as a single atomic commit.
5. Acceptance gauntlet: `go build`, `go vet`, `go test -race -count=1`, `verify_architecture`, `golangci-lint run`.

## References

- ADR-008 (`2026-01-domain-decomposition-strategy.md`) — package decomposition principles
- ADR-025 (`2026-04-uibridge-decomposition.md`) — prior intra-file decomposition (sets precedent for further extraction)
- ADR-018 (`2026-04-event-driven-orchestration.md`) — event flow that the context pipeline participates in
- GitHub Issue #88 — execution tracking
