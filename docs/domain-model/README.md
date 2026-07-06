# Domain Model for tell-me-go

This folder contains the canonical domain model for tell-me-go, built with
[modelith](https://github.com/stacklok/modelith).

> [!IMPORTANT]
> **AI agents (including tell-me-go itself): before doing any work in this
> repository, you MUST read all three domain-model skill files in full:**
>
> - [`skills/domain-model-author/SKILL.md`](skills/domain-model-author/SKILL.md)
> - [`skills/domain-model-context/SKILL.md`](skills/domain-model-context/SKILL.md)
> - [`skills/domain-model-lint/SKILL.md`](skills/domain-model-lint/SKILL.md)
>
> These define the canonical vocabulary, conventions, and rules for working
> with the domain model. Reasoning without them will produce names and
> structures inconsistent with the model. At minimum, load
> `domain-model-context` before any coding task that touches product concepts.

## Files

| File | Role |
|---|---|
| `tell-me-go.modelith.yaml` | Canonical source — you edit this |
| `tell-me-go.modelith.md`   | Generated Markdown + Mermaid ER diagram — never edit by hand |

## Quick commands

```sh
make modelith-lint     # validate the YAML
make modelith-render   # regenerate the .md from the .yaml
make modelith-check    # CI gate: fail if the committed .md is stale
```

`modelith-check` is wired into `make check` and `make check-full`.

## What we learned about modelith

### The intended workflow is AI-assisted, not hand-written

Modelith ships three Claude Code skills (see `plugin/skills/` in the modelith
repo):

1. **`domain-model-author`** — builds/updates the YAML by conversation
2. **`domain-model-context`** — loads the model into a coding session so the
   agent reasons in the team's vocabulary
3. **`domain-model-lint`** — read-only review; explains findings prioritized as
   structural → semantic → completeness

The `domain-model-author` skill follows a **3-pass build order**:

| Pass | What | Goal |
|---|---|---|
| 1 — Skeleton | Name every entity, write a crisp 2–4 sentence `definition`, declare `relationships` and `cardinality` | This is the minimum useful model; renders to an ER diagram already |
| 2 — Behavior | Add `invariants` and `scenarios` that exercise every entity | Invariants are where the real behavior lives |
| 3 — Refinement | Fill in `attributes`, `enums`, `actions`, `glossary`, `ownership`, `role`/`note` | Only where they add clarity — not a checklist to exhaust |

Core philosophy from the skill: *"Your value is in the questions, not the
typing."* Naming is a commitment. Fuzziness is a signal. Push on invariants.

### Conventions

- **Entity keys** are PascalCase (`Project`, not `project`).
- **Backtick entity names** in freeform text (definitions, notes, invariants,
  scenario steps): `` `Project` ``. In structured fields that already imply an
  entity (`actors`, relationship `entity:`, entity keys), do **not** backtick.
- **`cardinality`** is one of `1:1`, `1:n`, `n:1`, `n:n`.
- **`ownership`** is `owned` (composition — can't exist independently) or
  `referenced` (association — independent entity).
- **Prefer declaring a relationship once**, from the parent/owner side.
  Declaring from both sides is allowed but redundant.
- **Action/scenario `actors`** must be defined entities or glossary terms.
- **Glossary** defines non-entity vocabulary (roles like `Owner`, states).
- **`invariants`** use `{id, statement}`. The `id` is lowercase kebab-case,
  unique across the model. Invariants live under the entity they govern, or in
  the top-level `invariants` list for cross-entity rules. Both share one id
  namespace.
- **Scenario `invariants_touched`** references invariant ids (not statements).
  A dangling id is a lint **error**.

### Lint layers

1. **Structural (error)** — violates the JSON Schema: wrong type, missing
   required field, bad cardinality. Must fix.
2. **Semantic (error or warning)** — dangling references, duplicate ids,
   reciprocal cardinalities that don't match. Errors block regardless of flags.
3. **Completeness (warning)** — entity with no invariants, entity no scenario
   exercises, glossary term never referenced, enum no attribute uses. Promoted
   to errors with `--completeness error`.

### How our model is doing

0 errors, 0 warnings. All modelith-skill conventions are followed:

- **Entity names backtick-quoted** in all definitions, invariants, notes, and scenario steps.
- **`SecurityManager`** added to the glossary as a defined actor.
- **Redundant child→parent relationships removed** (`Turn→Session`, `ToolCall→Turn`); only the parent side declares the relationship.
- **`Session→History`** relationship added from the parent side.

One additional lesson: **backtick is a reserved YAML indicator**. A plain scalar
value starting with `` ` `` will fail YAML parsing. Always double-quote values
that begin with a backtick: `"`Tool` invocations..."`.

## How modelith helps tell-me-go

The domain model is not passive documentation — it is an active tool for
improving the codebase. Six concrete applications:

### 1. Gap audit — model vs. code

Compare every entity and enum in the model against the codebase. Does `Context`
have a corresponding struct? Does `LLMError` exist as a type? The first audit
([issue #1192](https://github.com/gosharplite/tell-me-go/issues/1192)) found
six structural gaps — all now resolved:

| # | Gap | Severity | Status | Resolution |
|---|---|---|---|---|
| 1 | `Context` — no struct; scattered across 7+ types | High | **RESOLVED** (2026-07) | Decomposition is intentional. Model YAML updated to document the cooperating types (`HistoryPruner`, `TokenGatekeeper`, `pinningPolicy`, etc.) that compose the Context pipeline. |
| 2 | `Pricing` — `contextWindow` separated from rates | Medium | **RESOLVED** (2026-07) | `ModelPricing` struct (`pricing/pricing.go:33`) includes `ContextWindow`. `ModelConfig` (`config/config.go:226`) carries both `ContextWindow` and `Pricing.ModelPricing` together. |
| 3 | `LLMError` — no classification type | Medium | **RESOLVED** (2026-07) | `llm.LLMError` type alias exists at `llm/llmerror.go:14`, mapping to the five enum values (rate_limited, context_overflow, auth_failure, server_error, timeout). |
| 4 | `SafePath` — no dedicated type; handled procedurally | Medium | **RESOLVED** (2026-07) | `security.SafePath` struct at `security/safepath.go:21` with `Path`, `Mode` (`SafePathMode`), and `AuthorizedAt` fields. |
| 5 | `ToolCall` — only an event, not a domain value object | Low | **RESOLVED** (2026-07) | `tools.ToolCall` struct at `tools/types.go:65` with `ToolName`, `Arguments`, `Result`, `Duration`, and `Status` fields. |
| 6 | `bypassConfirmation` on wrong entity — fixed in model | — | **RESOLVED** (2026-07) | Corrected in model YAML: `bypassConfirmation` now lives on `Config`, matching the code (`Config.BypassConfirmation`). |
| 7 | `Turn` incorrectly placed in Glossary | — | **RESOLVED** (2026-07) | Removed `Turn` from glossary since it's defined as an entity; replaced with `Chatter` to document the interface boundary. |

**All 7 original gaps are closed.** Re-run `make modelith-drift` and `make modelith-layers` periodically to catch new drift.

Run this audit periodically: `grep` each entity name in `internal/domain/` and
verify a matching type exists.

### 2. Reverse audit — code vs. model

The inverse direction: scan `internal/` for packages and types with no
corresponding entity in the model. A full audit (2026-07) found all
`internal/domain/` sub-packages are correctly classified:

| Package | Contains | Classification |
|---|---|---|
| `events/` | EventBus, event types, pub/sub | Infrastructure — intentionally omitted per model design decisions |
| `telemetry/` | Telemetry types | Infrastructure — intentionally omitted |
| `persistence/` | `FileSystem`, `File`, `Paths` | Port interfaces and config-derived types, not domain entities |
| `ports/` | Repository interfaces, `Task` DTO | Hexagonal port interfaces — not domain entities by definition |
| `services/` | `taskService` | Application-layer service, not a domain entity |
| `security/` | `Policy`, `SafetyService` | Supporting types for glossary role `SecurityManager` |

**No gaps found.** The model and code are aligned on what belongs in the domain
layer. Run `make modelith-drift` periodically to catch new drift.

### 3. Scenario → test coverage mapping

Every scenario is an integration test waiting to be written. Map them to
existing or missing coverage:

| Scenario | Maps to | Test exists? |
|---|---|---|
| Basic question-answer turn | Engine happy path | `TestTurnEngine_ExecutionStep_NoToolCalls` |
| Cost auditing per turn | MetricsTracker + CostCalculator | `TestCostCalculator_Calculate` |
| Skill injection into context | Orchestrator Skill Injection | `TestSkillInjector_Transform` |
| Tool-augmented turn | Executor dispatch | `TestRunExecutionPlan_HappyPath_SuccessfulBatchesReturnNil` |
| Hallucination loop detection | `loopDetector` middleware | `TestLoopDetector_Scenarios` |
| Context overflow and summarisation | `TokenGatekeeper` + `pinningPolicy` | `TestTokenGatekeeper_ValidateHardLimits` |
| Provider error classification and failover | `RecoveryStep` + `DefaultRetryPolicy` | `TestDefaultRetryPolicy_ShouldRetry` |
| Session undo and retry | History rollback + re-prompt | `TestSessionManager_Rollback` |
| Path authorization | SecurityManager `SafePath` validation | `TestCheckBoundary_ErrorPaths` |

### 4. Invariant audit

The model declares 15 invariants. Each has been traced to its code enforcer
(audit completed 2026-07). All are enforced or structurally guaranteed:

| # | Invariant ID | Enforced By | Status |
|---|---|---|---|
| 1 | `session-max-turns` | `engine_phases.go:24` / `engine_execution.go:49` — checks `CtxManager.GetLimits().MaxToolTurns` | ✅ ENFORCED |
| 2 | `context-within-budget` | `TokenGatekeeper` at `gatekeeper.go:24` — validates hard limits, triggers summarization | ✅ ENFORCED |
| 3 | `context-pinned-preserved` | `pinningPolicy` at `pruner.go:234` — marks pinned turns; `TokenGatekeeper` respects pins | ✅ ENFORCED |
| 4 | `tool-timeout` | `executor.go:202` — `WithToolTimeout` option; fed by `Config.ToolTimeoutSeconds` | ✅ ENFORCED |
| 5 | `history-persisted-after-turn` | `engine.go:358` — `emergencySave()` called at end of each turn phase loop | ✅ ENFORCED |
| 6 | `turn-belongs-to-one-session` | Structural — `Turn` composed within `Session`, not independently referenceable | ✅ STRUCTURAL |
| 7 | `provider-unique-name` | `config.go:158` — `validateProviderUniqueness()` checks map keys before accepting config | ✅ ENFORCED |
| 8 | `config-valid-provider` | `config.go:126` — `validateSelectedProvider()` verifies key exists in registry | ✅ ENFORCED |
| 9 | `pricing-unique-model` | `map[string]ModelPricing` — Go maps structurally prevent duplicates; `ValidateUniqueModels()` anchor at `pricing.go:58` | ✅ STRUCTURAL |
| 10 | `tool-unique-name` | `registry.go:63` — `RegisterToToolkitWithOptions` checks `r.entries[def.Name]`; duplicates update existing entry | ✅ ENFORCED |
| 11 | `skill-unique-name` | `file_repo.go:36` — `hasSkillName()` checks cache; duplicates trigger `slog.Warn` and skip | ⚠️ SOFT |
| 12 | `safepath-absolute` | `manager.go:148` — `RegisterSafePath` calls `filepath.Clean()` + `filepath.Abs()`; also enforced in `policy.go:93` | ✅ ENFORCED |
| 13 | `task-non-empty-content` | `task_service.go:90` — `AddTask` returns error if `content == ""` | ✅ ENFORCED |
| 14 | `bypass-suppresses-prompts` | `manager.go:96` — `Confirm()` checks `IsBypassActive()` first, returns true without calling `ui.Confirm()` | ✅ ENFORCED |
| 15 | `deterministic-cost-audit` | `metrics_tracker.go:223` — `AccumulateAndReturn` calculates cost via `CostCalculator` immediately after each turn | ✅ ENFORCED |

**14 of 15 invariants are fully enforced.** The one soft spot is `skill-unique-name`:
duplicate skill names log a warning and skip rather than failing startup. This
is documented in [INTENTIONAL_NON_FIXES.md](../../architect/INTENTIONAL_NON_FIXES.md).

### 5. PR review automation

A CI check could, for each pull request:
- Scan new/changed exported identifiers against the model's canonical names
- Flag code that introduces an entity-like concept without a model update
- Flag code that renames a modeled concept (e.g. `Session` → `Conversation`)

This keeps the model and code from drifting apart silently.

### 6. Onboarding that stays honest

New contributors open `tell-me-go.modelith.md`, see the Mermaid ER diagram,
read 11 entity definitions, trace 10 scenarios. They understand the system's
nouns and rules before reading a single line of Go. Because `modelith-check`
runs in CI, the diagram is regenerated from source on every commit — it
**cannot rot**, unlike hand-maintained architecture docs.

## Layers already modeled

| Layer | Entity | Role |
|---|---|---|
| Core loop | `Session` | Long-running conversation context |
| | `Context` | In-flight prompt payload assembled before each `Turn` |
| | `Turn` | One request–response cycle with thoughts and tool calls |
| | `ToolCall` | Single tool invocation during a Turn |
| LLM backend | `Provider` | LLM backend (gemini/openai/deepseek/anthropic) |
| | `Config` | YAML configuration with provider registry |
| | `Pricing` | Per-model cost rates (HIT/MISS/COMP per million tokens) |
| Agent capabilities | `Tool` | Registered capability exposed to the LLM |
| | `Skill` | Injected guidance (golang-patterns, golang-testing) |
| Safety | `SafePath` | Authorized directory boundary |
| | `UserInteractor` | Security confirmation interface |
| Persistence | `History` | Persisted session storage (SQLite) |

Glossary roles (not entities — they have no persisted state):

| Term | Role |
|---|---|
| `Orchestrator` | Drives the session loop, dispatches tools, manages turns |
| `SecurityManager` | Validates tool requests against SafePath, delegates to UserInteractor |
| `Thought` | Provider-agnostic reasoning block (text, tool-call, chain-of-thought) |
| `Chatter` | Conversation interface between the Orchestrator and Provider gateway |

## Design decisions

- **`Orchestrator` and `SecurityManager` are glossary terms, not entities.**
  They are behavioral roles with no persisted state of their own — making them
  entities would require inventing attributes and invariants that don't exist
  in the code.
- **`bypassConfirmation` lives on `Config`, not `UserInteractor`.** The code
  places the bypass flag on `PolicyEvaluator`, which reads from configuration.
  `UserInteractor` is a pure interface (`Confirm`, `Warn`, `Prompt`, `ReadLine`)
  and does not carry state.
- **`Pricing` is an entity, not just Config attributes.** Each model variant has
  a structured cost profile (context window + three rate tiers). The cost-audit
  scenario demonstrates the lookup: Turn token counts → Pricing rates → USD
  cost → accumulated on Session.
- **`Context` is distinct from `History`.** `History` is persisted (SQLite);
  `Context` is the runtime prompt payload assembled before each Turn. It has
  its own invariants: must fit within the model's context window, and pinned
  Turns are never summarised.
- **`LLMError` and `ToolCategory` are typed enums.** Error classification
  drives retry/failover decisions (rate_limited → backoff, auth_failure →
  abort, context_overflow → summarise). Tool categories group capabilities
  (workspace, analysis, integration, system).
- **Telemetry (OpenTelemetry) and TUI (bubbletea)** are intentionally omitted.
  They are infrastructure/presentation concerns, not domain concepts.


*Last Updated*: 2026-07