# Domain Model for tell-me-go

This folder contains the canonical domain model for tell-me-go, built with
[modelith](https://github.com/stacklok/modelith).

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
| `Turn` | One request–response cycle |

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
