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

We started with 5 completeness warnings and resolved them all (0 errors, 0
warnings as of commit `eeea9aac`). But we identified **convention deviations**
that the linter doesn't catch:

| # | Issue | Fix |
|---|---|---|
| 1 | Entity names **not backtick-quoted** in most definitions, invariants, and scenario steps | Add `` `EntityName` `` throughout all prose fields |
| 2 | `SecurityManager` used as an actor/scenario-step reference but has **no entity or glossary entry** | Either add a `SecurityManager` entity or define it in the glossary |
| 3 | **Redundant bidirectional relationships**: `Turn → Session` exists but `Session → Turn (owned)` already covers it. Same for `ToolCall → Turn` vs. `Turn → ToolCall (owned)` | Remove the child→parent reverse declarations; keep only the parent side |
| 4 | `Session` has no relationship to `History`, but `History → Session` exists | Add `Session → History (owned)` from the parent side |

## Layers already modeled

| Entity | Role |
|---|---|
| `Session` | Long-running conversation context |
| `Turn` | One request–response cycle with thoughts and tool calls |
| `ToolCall` | Single tool invocation during a Turn |
| `Provider` | LLM backend (gemini/openai/deepseek/anthropic) |
| `Config` | YAML configuration with provider registry |
| `Tool` | Registered capability exposed to the LLM |
| `Skill` | Injected guidance (golang-patterns, golang-testing) |
| `SafePath` | Authorized directory boundary |
| `History` | Persisted session storage (SQLite) |
| `UserInteractor` | Security confirmation interface |

## Future work

- Fix the 4 convention deviations above
- Consider adding entities for: `Orchestrator` (currently a glossary term but
  central to all scenarios), `SecurityManager` (used in scenarios but undefined)
- Model-level gaps: pricing/cost-audit, telemetry/OTel, TUI/browse layer
