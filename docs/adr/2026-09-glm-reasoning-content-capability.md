# ADR-072: Native reasoning_content Round-Trip for Z.AI GLM Models (SupportsReasoningContent)

## Status
Accepted (2026-09)

## Context
Z.AI's GLM-5.3 and GLM-5.3-Flash are always-on reasoning models — GLM-5.3 "always operates with reasoning enabled… disabling reasoning is no longer supported", and GLM-5.3-Flash's `thinking.type` "only supports enabled; thinking cannot be disabled" — and both return reasoning traces in the `reasoning_content` field on Chat Completions responses.

The OpenAI transport's response-side parsing is capability-independent: `fromOpenAIResponse` (`internal/infrastructure/llm/openai/metrics.go`) maps a response's `reasoning_content` to an `IsThought` part for any model. GLM reasoning traces were therefore already captured into history and rendered via the `[Thinking]` path. The defect was request-side round-trip only: `ResolveCapabilities` left `SupportsReasoningContent: false` for all `glm-*` models, so on the next request `classifyParts` (`internal/infrastructure/llm/openai/client.go`) wrapped assistant thought parts in non-native `<thought>…</thought>` XML inside `content`, and `shouldIncludeReasoning` omitted `reasoning_content` from assistant messages — a wire shape Z.AI does not document. Source: PR #1450 architecture review, finding #3.

## Decision

### Decision 1: Separate exact-match allowlist for GLM reasoning classification (seeded with `glm-5.3` and `glm-5.3-flash`)
`isGLMReasoningModel(model)` returns true for exactly `glm-5.3` and `glm-5.3-flash` (and the namespaced `*/glm-5.3`, `*/glm-5.3-flash` forms), mirroring the `isKimiK3Model`/`isGLMVisionModel` exact-match pattern (ADR-071, Decision 1). The reasoning allowlist is deliberately **separate** from the vision allowlist (`isGLMVisionModel`): the two capability axes are independent — `glm-5.3` is text-only but always-reasoning, so a shared allowlist would either wrongly grant it vision or wrongly deny it reasoning. Older GLM-4.x models (`glm-4.5V`, `glm-4.7-flash`) are excluded: they ship a user-controllable thinking toggle, so `reasoning_content` is not guaranteed on the wire; they join by explicit allowlist extension when verified. This resolves the issue's boundary question: `glm-5.3` is IN.

### Decision 2: Wire contract — native `reasoning_content`, mirroring the deepseek/kimi path
With `SupportsReasoningContent: true`, GLM joins the existing native path with zero transport changes: `classifyParts` serializes thought parts into `reasoning_content` (not `<thought>` XML), and `shouldIncludeReasoning` includes `reasoning_content` on every assistant message (even when empty) — the same always-include multi-turn protocol shape already used for `deepseek-*` and `kimi-*`. Response-side parsing is unchanged (it was already capability-independent).

### Decision 3: Non-goals
`SupportsThinkingToggle` and `SupportsReasoningEffort` remain false for GLM: Z.AI documents `thinking.type` supports only `enabled`, so emitting the toggle or effort would add wire surface without control (issue out-of-scope). Video/file input and cross-provider reasoning rendering remain out of scope as in ADR-071. `FileUploadMode` and `MaxTokensField` stay `FileUploadNone` / `MaxTokensFieldLegacy`.

## Consequences

### Positive
- GLM multi-turn reasoning context round-trips in the provider's native field; the undocumented `<thought>` XML shape no longer leaks into GLM requests.
- Capability-layer-only change: the two transport gate sites (`shouldIncludeReasoning`, `classifyParts`) were already exercised by the deepseek/kimi native path, and the new transport test pins the GLM contract at CC ≤ 10 (test CC=9, helpers CC=8/4) — no catalog pin required.
- UI `[Thinking]` rendering is unaffected: response-side parsing already produced `IsThought` parts.

### Negative / Accepted Trade-offs
- Allowlist rot: a future always-reasoning GLM variant not added to `isGLMReasoningModel` keeps the `<thought>` fallback until recorded (same mitigation as ADR-071 Decision 1 — architect review on model additions).
- Every GLM assistant message now carries `reasoning_content` (possibly empty) — ~30 extra bytes per message; if Z.AI ever rejects the empty field, this mirrors a latent deepseek/kimi contract assumption rather than a GLM-specific one.

### Neutral
- `ResolveCapabilities` cyclomatic complexity rises 9 → 10 (still within the CC ≤ 10 policy threshold; noted on the CC=10 boundary watch).
- No config, domain-model, or README sample-config changes: the capability is model-string-driven.

## Alternatives Considered

1. **Reuse the vision allowlist for reasoning.** Rejected — `glm-5.3` is text-only-but-always-reasoning; the axes are not coextensive.
2. **`glm-5.3*` prefix heuristic.** Rejected — same reasoning as ADR-071 Decision 1: no reliable GLM naming convention; an unvetted future `glm-5.3-*` variant must not inherit the classification by pattern.
3. **Response-side gating changes.** Not needed — `fromOpenAIResponse` already parses `reasoning_content` capability-independently.
4. **Catalog pin for the transport test.** Rejected — a new test should be born under the CC ≤ 10 policy; decomposition keeps every function under threshold, so no architect-curated debt is warranted.

## Verification
- `internal/domain/llm/capabilities_test.go` `TestResolveCapabilities` (CC=13, catalog-pinned at line 10): rows `glm-5.3-flash`, `z.ai/glm-5.3-flash`, `glm-5.3`, `z.ai/glm-5.3` → `SupportsReasoningContent: true`; `glm-4.7-flash`, `glm-4.5V` → false.
- `internal/infrastructure/llm/openai/client_chat_test.go` `TestGLMReasoningContentRoundTrip` (+ `assertNativeReasoningMessage`, `assertJSONReasoningContract`): GLM allowlist models serialize assistant thought parts as `reasoning_content` (JSON key present, no `<thought>` XML); `glm-4.7-flash` negative control keeps the `<thought>` fallback.
- `make check-full` passes (including `verify-nonfix-catalog` and `verify-adr-index`).

## Related ADRs
- [ADR-071](2026-09-glm-53-flash-vision-capability.md) — the allowlist pattern this extends to an independent capability axis.
- [ADR-070](2026-09-deepseek-vision-capability.md) — capability-axes-decomposition precedent.
- [ADR-052](2026-07-deepseek-thinking-toggle.md) — the request-side thinking-toggle precedent, deliberately NOT extended to GLM here.
- [ADR-023](2026-04-reasoning-token-accounting.md) — response-side reasoning-token handling.

## References
- Issue #1451; PR #1450 (architecture review, finding #3).
- Z.AI GLM-5.3-Flash model page: https://docs.z.ai/guides/vlm/glm-5.3-flash.md — "thinking.type only supports enabled; thinking cannot be disabled."
- Z.AI GLM-5.3 model page: https://docs.z.ai/guides/llm/glm-5.3.md — "GLM-5.3 always operates with reasoning enabled… disabling reasoning is no longer supported."
- Z.AI Chat Completion API reference: https://docs.z.ai/api-reference/llm/chat-completion.md

---
*Last Updated: 2026-09 (issue #1451)*
