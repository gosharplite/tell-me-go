# ADR-024: OpenAI Chat-vs-Responses Budget-Field Divergence

## Status
Accepted (2026-04)

## Context
OpenAI's HTTP API exposes the same conceptual parameter — the per-request output-token budget — under three different JSON field names depending on endpoint and model generation. Sending the wrong field name yields HTTP 400 `unsupported_parameter`; the request cannot defensively include all three because OpenAI rejects unrecognised fields.

| Endpoint | Model family | Wire field |
|---|---|---|
| `/v1/chat/completions` | gpt-3.5, gpt-4 (legacy) | `max_tokens` |
| `/v1/chat/completions` | o-series, gpt-5.0..5.3 | `max_completion_tokens` |
| `/v1/responses` | gpt-5.4+ with tools and `reasoning_effort` | `max_output_tokens` |

DeepSeek's OpenAI-compatible endpoint adds a fourth dimension: it always uses `max_tokens` regardless of model, so the choice of wire field depends on the triple `(model, endpoint, provider)`. The endpoint itself is derived from `(model, has_tools, has_reasoning_effort)`. The provider is derived from `baseURL`. None of these are observable from the model name alone.

### The Bug That Forced the Decision
In commit `82e763f1` (2026-03-29), Responses API support was added by reusing the existing `chatRequest` struct, which carried only `MaxCompletionTokens`. The defect was latent — no in-tree configuration triggered all three Responses-API routing preconditions (gpt-5.4+ model, tools loaded, `reasoning_effort` header) simultaneously — until 2026-04-20, when a user combined `MODEL: "gpt-5.4"` with tools and `HEADERS.reasoning_effort: "high"`. The result was a production HTTP 400:

```
Unsupported parameter: 'max_completion_tokens'. In the Responses API,
this parameter has moved to 'max_output_tokens'.
```

The deeper issue: the `Capabilities` struct exposed `UseMaxCompletionTokens bool`, which for `gpt-5.4` resolved to `true` while the wire format actually required `max_output_tokens`. The boolean was a convention-driven hint, not a contract. The same kind of two-coupled-booleans hidden coupling that ADR-022 explicitly cautions against in a different context.

The runtime fix landed in `8753a662` (added a third field, `MaxOutputTokens`, and a switch keyed on `useResponsesAPI`-first). The structural fix landed in `d4345ea2` (replaced the boolean with an enum). This ADR captures the reasoning behind both.

## Decision
**Model the per-request output-token budget choice as a `MaxTokensField` enum on the domain `Capabilities` struct, with three values that mirror the three wire-format fields:**

```go
type MaxTokensField int

const (
    MaxTokensFieldLegacy     MaxTokensField = iota // → "max_tokens"
    MaxTokensFieldCompletion                       // → "max_completion_tokens"
    MaxTokensFieldOutput                           // → "max_output_tokens"
)
```

`ResolveCapabilities(model, baseURL)` selects the value by an explicit precedence rule:

```
RequiresResponsesAPI true → Output     (gpt-5.4+; static-default for /responses)
isOpenAIReasoner    true  → Completion (o-series, gpt-5.0..5.3)
otherwise                 → Legacy     (DeepSeek, plain gpt-4)
```

The OpenAI transport layer dispatches via `switch` on this enum, with **one explicit override** retained at the call site:

```go
field := c.capabilities.MaxTokensField
if useResponsesAPI {
    field = llm.MaxTokensFieldOutput
}
switch field { ... }
```

The override is the **one piece of dual-state coupling** the design intentionally retains. It is not a smell; it is the honest expression of a real fact: the static `Capabilities` value describes the model's *default* serialisation, whereas the per-request `useResponsesAPI` predicate (driven by `RequiresResponsesAPI && len(toolDecls) > 0 && hasEffort`) describes the *actual* serialisation for *this* request. The same `gpt-5.4` model uses `max_completion_tokens` on `/chat/completions` and `max_output_tokens` on `/responses`. Collapsing both into a single static enum value would require either materialising a distinct capability set per request (heavy) or denying the model its non-`/responses` code path (incorrect). Four lines of override at the call site is the cheapest accurate model.

## Consequences

### Positive
- **Invalid combinations are unrepresentable.** A `MaxTokensField` value names exactly one of the three valid wire fields. There is no longer a "neither bool set, no field on the wire, silent 4096-truncation" failure mode (the gpt-4-class semantic change documented in `d4345ea2`).
- **The intentional gpt-4-class behaviour change** — those models now correctly send `max_tokens` with the resolved budget instead of relying on OpenAI's silent default — closes the same silent-truncation hole that `5031162c` addressed for Anthropic. ADR-022's "fail loud, never silently truncate" principle now extends to plain Chat Completions calls as well.
- **Future provider additions** (e.g., a hypothetical fourth wire field) extend the enum, not a Cartesian product of booleans. The compiler enforces exhaustive handling at every `switch` site that does not have a `default` case.
- **The fix is symmetric with the architectural pattern in ADR-023**: normalise provider-specific quirks at the SDK boundary, expose a single canonical model upward.

### Negative / Accepted Trade-offs
- **The endpoint-override branch in the OpenAI client must be remembered** when modifying budget-field logic. Mitigated by the inline comment introduced in `d4345ea2` and by this ADR.
- **A future divergence between `/chat/completions` and `/responses` on a non-budget field** (e.g., a new tool-call format that diverges in non-trivial ways) will reopen the question of whether to split `chatRequest` into two structs. This ADR does not preclude that — it explicitly defers it to "Alternative 2" below as still-valid future option.
- **The DeepSeek path now flows through `MaxTokensFieldLegacy`**, which is shared with plain gpt-4. A future regression that broke `Legacy` semantics would now affect two providers simultaneously rather than just one. Mitigated by the contract test `TestOpenAI_WithMaxTokens_DeepSeek_PopulatesMaxTokensField` and the table-driven `TestResolveCapabilities_MaxTokensField` which pin both routes independently.

### Neutral
- `RequiresResponsesAPI` is retained on `Capabilities` and continues to drive endpoint routing in `resolveEndpoint`. It is now read at exactly two distinct call sites: routing (where it is the source of truth) and the budget-field override branch (where it is read indirectly via `useResponsesAPI`). This dual use was previously implicit; it is now visible in the code.

## Alternatives Considered

1. **Three coupled booleans** (`UseMaxTokens`, `UseMaxCompletionTokens`, `UseMaxOutputTokens`).
   *Rejected.* Invalid combinations (e.g., two flags `true` simultaneously) remain representable. This is the same class of defect that produced the original bug — the type system would not protect against a future model resolution rule that mistakenly set two flags. The enum makes such an error impossible by construction.

2. **Two distinct request structs** (`chatCompletionsRequest` and `responsesRequest`) with separate marshalling paths.
   *Rejected as out of proportion to current divergence.* The two endpoints share approximately 90% of their fields; the wire-format budget-field name is one of only three meaningful divergences (the others being `messages`/`input` and `reasoning_effort`/`reasoning.effort`, both already handled by `omitempty` on a single struct). Doubling the serialisation surface for a three-line dispatch saves no bug class that the enum does not already eliminate. Explicitly noted as a deferred option if future divergence grows past a threshold where the single-struct approach starts requiring more `if/else` branches than fields.

3. **Polymorphic `BudgetField` interface with an `Apply(req *chatRequest)` method** per concrete type.
   *Rejected as over-engineered.* Three concrete cases that change on a multi-year cadence do not justify an interface layer. Adds indirection without adding extensibility that any caller will exercise. The enum's exhaustive `switch` is the right Go-idiomatic shape; an interface here would be Java muscle memory.

4. **Move the `MaxTokensField` decision out of `Capabilities` and into the OpenAI client itself**, computing it inline from `model + endpoint`.
   *Rejected.* Capabilities is the domain layer's single source of truth for per-model behaviour. Moving wire-format knowledge into a transport-layer function would scatter the model classification logic across two layers and require duplicating the reasoner-detection rule (`gpt-5+`, `o1*`, `o3*`) at the call site. The current placement keeps `internal/domain/llm` as the only file that needs updating when a new model is added.

## Verification
The invariant is pinned by three test files:

- `internal/domain/llm/capabilities_maxtokens_test.go::TestResolveCapabilities_MaxTokensField` — table-driven enum-value pin for representative models from each tier (`gpt-4`, `gpt-5`, `gpt-5.4`, `gpt-6`, `o1-mini`, `o3`, `deepseek-reasoner`, `deepseek-ai/deepseek-v3.2-maas`).
- `internal/domain/llm/capabilities_test.go::TestResolveCapabilities` — full capability matrix including the enum, asserting the no-cross-contamination invariant against every supported model.
- `internal/infrastructure/llm/openai/responses_maxtokens_test.go::TestOpenAI_ResponsesAPI_UsesMaxOutputTokens` — end-to-end contract test that captures the request body sent to a mock `/responses` server and asserts `max_output_tokens` is present while `max_completion_tokens` and `max_tokens` are absent. This is the test that would have caught the original `82e763f1` defect at commit time had it existed then.

The four pre-existing tests in `internal/infrastructure/llm/openai/maxtokens_test.go` continue to pass without modification, confirming the refactor is behaviourally inert for `/chat/completions` traffic:

- `TestOpenAI_DefaultMaxTokens_IsGenerous`
- `TestOpenAI_WithMaxTokens_Override`
- `TestOpenAI_WithMaxTokens_ZeroFallsBackToThinkingBudget`
- `TestOpenAI_WithMaxTokens_ZeroAndNoThinkingBudget_FallsBackToDefault`
- `TestOpenAI_WithMaxTokens_DeepSeek_PopulatesMaxTokensField`

## Related ADRs
- [**ADR-001**](2026-01-multi-llm-provider-strategy.md): Hybrid LLM Infrastructure Strategy — established the multi-provider architecture in which the wire-format-divergence problem arises.
- [**ADR-022**](2026-04-tool-result-error-convention.md): Tool-Result Error Convention — adjacent precedent for "fail loud at the boundary, never silently truncate or coerce". The gpt-4-class semantic change in `d4345ea2` extends the same principle to budget-field omission.
- [**ADR-023**](2026-04-reasoning-token-accounting.md): Reasoning-Token Accounting — same architectural pattern of normalising provider-specific quirks at the SDK boundary rather than downstream.

## References
- Defect-introducing commit: `82e763f1` — `feat(openai): implement centralized capabilities and Responses API support`
- Related prior-art commit: `5031162c` — `llm(anthropic,openai): fix silent tool-call truncation at max_tokens`
- Runtime-fix commit (Task 1): `8753a662` — `fix(openai): send max_output_tokens for Responses API endpoint`
- Structural-fix commit (Task 2): `d4345ea2` — `refactor(llm): replace UseMaxCompletionTokens bool with MaxTokensField enum`
- OpenAI Responses API reference (for `max_output_tokens`): <https://platform.openai.com/docs/api-reference/responses>
- OpenAI Chat Completions reference (for `max_tokens`, `max_completion_tokens`): <https://platform.openai.com/docs/api-reference/chat/create>
- DeepSeek API reference (for `max_tokens`): <https://api-docs.deepseek.com/api/create-chat-completion>
- Modified files (Tasks 1 & 2):
  - `internal/domain/llm/capabilities.go` — enum declaration and `ResolveCapabilities`.
  - `internal/infrastructure/llm/openai/client.go` — `chatRequest.MaxOutputTokens` field and `prepareChatRequest` switch.

---
*Last Updated: 2026-04*
