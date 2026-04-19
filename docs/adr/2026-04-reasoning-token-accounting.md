# ADR-023: Reasoning-Token Accounting for OpenAI-Compatible Providers

## Status
Accepted (2026-04)

## Context
LLM providers differ in how they report token usage when a model emits chain-of-thought (CoT) reasoning. The internal `llm.Metrics` struct is populated by every provider client (OpenAI, Gemini, Anthropic) and consumed by a single pricing layer that is intentionally provider-agnostic. The semantics of its two output-side fields (`ResponseTokens`, `ThinkingTokens`) must therefore be uniform across all providers — but the underlying APIs are not.

| Provider family | API field for reasoning | Relationship to "output" total |
|---|---|---|
| **OpenAI-compatible** (OpenAI gpt-5/o-series, DeepSeek reasoner, future Mistral/Together AI reasoners) | `usage.completion_tokens_details.reasoning_tokens` | **Subset** of `usage.completion_tokens` |
| **Google Gemini** | `usage_metadata.thoughts_token_count` | **Disjoint** from `usage_metadata.candidates_token_count` |
| **Anthropic Claude** | (no separate reasoning field; thinking is interleaved with content blocks) | N/A |

Verified empirically against the live DeepSeek API on 2025-12-XX:

```json
{
  "prompt_tokens": 16,
  "completion_tokens": 190,
  "total_tokens": 206,
  "completion_tokens_details": { "reasoning_tokens": 147 }
}
```

The arithmetic `total_tokens (206) == prompt_tokens (16) + completion_tokens (190)` proves `reasoning_tokens` is **contained within** `completion_tokens`. If it were additive, `total_tokens` would be 353.

The pricing layer (`internal/domain/pricing/pricing.go::Calculate`) treats the two fields as **disjoint** and sums them into the output cost:

```go
OutputCost = ResponseTokens × Comp + ThinkingTokens × Thinking
```

This formula is correct **only if the two fields are disjoint**. Prior to commit `87550005`, the OpenAI-compatible client wrote raw `completion_tokens` (which already includes reasoning) into `ResponseTokens`, causing reasoning tokens to be billed twice — up to ~2× overcharge on reasoning-heavy turns, and a structurally wrong "O:" (output total) in the UI.

## Decision
**Normalise to the disjoint-quantity convention at each provider's SDK boundary.** Each provider client (`openai`, `gemini`, `anthropic`) is responsible for converting its native usage representation into the canonical internal form:

```
INVARIANT: For all providers,
    ResponseTokens  := final-content output tokens only (excludes reasoning)
    ThinkingTokens  := reasoning/CoT tokens only
    ResponseTokens + ThinkingTokens == total billable output tokens
```

For OpenAI-compatible providers specifically, this requires subtracting `reasoning_tokens` from `completion_tokens` in `calculateFinalMetrics`. For Gemini, no transformation is needed (the API already reports them disjoint). For Anthropic, the question is moot until a future Claude variant exposes an explicit reasoning-token field.

## Consequences

### Positive
- Pricing arithmetic is uniform across providers — no per-provider branches in `pricing.go`.
- `O:` (output total) in the UI accurately reflects the user's actual output spend.
- The same fix automatically covers any future OpenAI-compatible reasoning model (gpt-5.x, o-series successors, third-party providers using OpenAI's schema).

### Negative
- The transformation is **non-obvious** — a maintainer reading only `client.go` may be tempted to "simplify" by removing the subtraction. Mitigation: inline comment in `calculateFinalMetrics` cites this ADR by number.
- New provider integrations must explicitly verify which convention their API follows. Mitigation: this ADR documents the empirical test (curl + arithmetic check on `total_tokens`) used to determine the convention.

### Neutral
- A defensive guard `thinkingTokens > 0 && contentTokens >= thinkingTokens` protects against malformed responses where `reasoning_tokens > completion_tokens` — should never occur, but underflow in `int32` arithmetic would silently corrupt billing.

## Alternatives Considered

1. **Sum the fields in `pricing.go` instead of subtracting in `client.go`**
   *Rejected*: would require per-provider conditionals in the pricing layer, violating Separation of Concerns. Pricing should be agnostic to provider quirks.

2. **Store raw `completion_tokens` in `ResponseTokens` and document the overlap**
   *Rejected*: every downstream consumer (UI, ledger, dashboards, `O:` totals) would need to know about the overlap and deduct accordingly. Single point of truth at the SDK boundary is safer.

3. **Add a third field `ReasoningContainedInResponse bool` to `llm.Metrics`**
   *Rejected*: leaks provider-specific schema into the domain model. Violates the Hexagonal boundary.

## Verification
The invariant is pinned by tests in `internal/infrastructure/llm/openai/client_test.go`:
- `TestDeepSeek_ReasoningTokens_DisjointFromContent` — happy path with realistic fixture (prompt=16, completion=190, reasoning=147 → ResponseTokens=43, ThinkingTokens=147).
- `TestDeepSeek_NoReasoning_PassesThrough` — non-reasoning models (e.g. `deepseek-chat`) unaffected; ResponseTokens equals raw completion_tokens.
- `TestDeepSeek_MalformedReasoning_GuardsAgainstNegative` — defensive underflow guard preserves completion_tokens when `reasoning_tokens > completion_tokens`.

## Related ADRs
- **ADR-001**: Hybrid LLM Infrastructure Strategy — established the multi-provider architecture where uniform metric semantics are critical.
- **ADR-015**: Cross-Platform Timing Guarantees — adjacent precedent for normalising provider quirks at the SDK boundary rather than downstream.

## References
- Bug-fix commit: `87550005` — `fix(openai): subtract reasoning_tokens from completion_tokens`
- DeepSeek API docs: <https://api-docs.deepseek.com/quick_start/pricing>
- DeepSeek thinking-mode docs: <https://api-docs.deepseek.com/guides/thinking_mode>
- OpenAI usage object reference (for `completion_tokens_details.reasoning_tokens`): <https://platform.openai.com/docs/api-reference/chat/object>
- Modified file: `internal/infrastructure/llm/openai/client.go::calculateFinalMetrics`
- Pricing consumer: `internal/domain/pricing/pricing.go::Calculate`

---
*Last Updated: 2026-04*
