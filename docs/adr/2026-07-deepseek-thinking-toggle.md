# ADR-052: DeepSeek/Kimi Thinking Mode Toggle and user_id Isolation

## Status
Accepted (2026-07)

## Context and Problem Statement

DeepSeek models support a thinking mode (chain-of-thought reasoning) controlled by a non-standard `{"thinking":{"type":"enabled|disabled"}}` request field. The thinking toggle defaults to **enabled** for DeepSeek reasoner-class models and **disabled** for `deepseek-chat`. DeepSeek also supports a `user_id` parameter for content-safety, KVCache, and scheduling isolation.

An initial implementation (PR #1269) was reverted because of two architectural blockers:

1. **Unconditional wire-format change:** The `thinking` field was emitted for every model with `SupportsReasoningContent` (a response-side flag), overriding provider defaults with no opt-out. This forced thinking mode on `deepseek-chat` (which defaults to OFF) and risked bricking Kimi traffic if the provider rejects unknown fields.

2. **Dead code in production:** The `WithThinkingEnabled` and `WithUserID` options existed only at the adapter layer — no config fields, no factory wiring, unreachable by operators.

## Decision Drivers

1. **Preserve provider defaults:** Unconfigured deployments must see zero wire-format change. The thinking field must be omitted unless the operator explicitly sets `THINKING_ENABLED` in YAML.

2. **Decouple request-side from response-side capabilities:** The reverted implementation gated thinking toggle emission on `SupportsReasoningContent`, but Vertex MaaS DeepSeek already proves these vary independently (`RequiresVertexThinkingKwargs` exists because that transport silently ignores the standard `thinking` field).

3. **Fail-fast on misconfiguration:** Invalid `user_id` values must be rejected at startup, not surface as opaque HTTP 400s that abort the failover chain.

4. **Vertex MaaS consistency:** When `THINKING_ENABLED` is explicitly set, `chat_template_kwargs.thinking` must match — otherwise an explicit disable is silently defeated on Vertex transports.

## Considered Options

1. **Gate on `SupportsReasoningContent` (reverted approach):** Rejected. Couples a request-side extension to a response-side flag known to diverge (Vertex MaaS).

2. **Make thinking toggle DeepSeek-only, skip Kimi:** Rejected. Kimi models share the same OpenAI-compatible API surface; scoping them in now avoids a second config change later, and the `SupportsThinkingToggle` gate provides a clean kill-switch if Kimi rejects the field.

3. **Dedicated capability + tri-state config + Vertex reconciliation (chosen).**

## Decision Outcome

Chosen option: **Dedicated `SupportsThinkingToggle` capability with tri-state config and Vertex MaaS reconciliation.**

### Implementation Details

1. **Capability model (`capabilities.go`):**
   - Added `SupportsThinkingToggle bool` to `Capabilities`, resolved independently in `resolveDeepSeekFamily` and `resolveKimiFamily`.
   - Set `true` for all `deepseek-*` and `kimi-*` models. All other models default to `false` (zero value).
   - `SupportsReasoningContent` remains unchanged; it governs only response-side `reasoning_content` serialization.

2. **Config (`config.go`):**
   - `ThinkingEnabled *bool` — tri-state pointer. `nil` = unset (field omitted from wire), `true`/`false` = explicit. YAML key: `THINKING_ENABLED`.
   - `UserID string` — validated at startup: `[a-zA-Z0-9\-_]+`, max 512 characters. YAML key: `USER_ID`.

3. **Client options (`openai/client.go`):**
   - `WithThinkingEnabled(bool)` — sets both `thinkingEnabled` and `thinkingEnabledSet` sentinel. The sentinel distinguishes "explicit false" from "never called" (both have boolean value `false`).
   - `WithUserID(string)` — sets the user_id value.

4. **Request assembly (`openai/chat.go`):**
   - Thinking toggle emitted only when `SupportsThinkingToggle && thinkingEnabledSet`. When unset, the field is omitted, preserving provider defaults.
   - `user_id` emitted only when `SupportsThinkingToggle && userID != ""`.
   - `injectTransportHints` (Vertex MaaS): `chat_template_kwargs.thinking` honors the resolved toggle value. Defaults to `true` when unconfigured (backward compat). When `thinkingEnabledSet=true`, uses the configured value.

5. **Factory wiring (`factory.go`):**
   - `buildBaseClient` conditionally appends `WithThinkingEnabled` and `WithUserID` to the option slice when config values are present.

### Cross-Provider Support Matrix

| Provider | `SupportsThinkingToggle` | `thinking` field | `user_id` | Notes |
|----------|--------------------------|------------------|-----------|-------|
| `deepseek-*` | `true` | Emitted when configured | Emitted when configured | Includes Vertex MaaS via `chat_template_kwargs` |
| `kimi-*` | `true` | Emitted when configured | Emitted when configured | Acceptance not yet proven against live API |
| `gpt-*`, `o1-*`, `o3-*` | `false` | Never emitted | Never emitted | |
| Anthropic | `false` | Never emitted | Never emitted | Uses separate client path |
| Gemini | `false` | Never emitted | Never emitted | Uses separate client path |

### Kimi Risk Mitigation

The `SupportsThinkingToggle` capability is set `true` for Kimi models based on their shared OpenAI-compatible API surface. If live testing reveals that the Moonshot/Kimi API rejects the `thinking` field (HTTP 400), mitigation is a one-line change: set `supportsThinkingToggle = false` in `resolveKimiFamily`. No config migration needed — the capability gate makes this a build-time toggle.

## Consequences

- **Positive:** Operators can explicitly control DeepSeek thinking mode from YAML. Unconfigured deployments see zero wire-format change.
- **Positive:** Multi-tenant deployments gain `user_id`-based KVCache and scheduling isolation, validated at startup.
- **Positive:** Vertex MaaS thinking toggle no longer silently defeats explicit configuration.
- **Negative:** Kimi `thinking` field acceptance is unproven. Mitigated by the `SupportsThinkingToggle` kill-switch.
- **Neutral:** `thinkingEnabledSet` sentinel adds a bool field to `client`. Acceptable complexity trade-off for tri-state semantics.

## Verification

The implementation is pinned by tests in three packages:

- `internal/domain/llm/capabilities_test.go::TestResolveCapabilities` — table-driven capability matrix asserting `SupportsThinkingToggle` for all DeepSeek and Kimi model variants, and `false` for GPT/o-series.
- `internal/infrastructure/llm/openai/chat_test.go` — 10 test functions covering thinking toggle tri-state (explicit enable, explicit disable, unset-omitted), capability gating (gpt-4 ignores), user_id emission and gating, and Vertex MaaS kwargs reconciliation (explicit disable honored, unset defaults true, non-Vertex no kwargs).
- `internal/infrastructure/llm/factory_test.go` — 5 factory pass-through tests verifying `THINKING_ENABLED` (`true`, `false`, unset) and `USER_ID` (set, empty) reach the wire through the full `newClient → buildBaseClient → NewClient → SendChat` pipeline.
- `internal/domain/config/provider_validation_test.go` — existing tests continue to pass, validating the `user_id` format/length constraints are not triggered by zero-value configs.

## Related ADRs

- [**ADR-001**](2026-01-multi-llm-provider-strategy.md): Hybrid LLM Infrastructure Strategy — established the multi-provider architecture that makes per-provider capability gating necessary.
- [**ADR-023**](2026-04-reasoning-token-accounting.md): Reasoning-Token Accounting — same architectural pattern of normalising provider-specific quirks at the SDK boundary.
- [**ADR-024**](2026-04-openai-budget-field-divergence.md): OpenAI Chat-vs-Responses Budget-Field Divergence — precedent for `Capabilities` enum modelling of provider-specific wire-format decisions.

## References
- PR #1269 — initial (reverted) implementation
- PR #1270 — reinstatement with proper architecture
- DeepSeek API reference (thinking toggle): <https://api-docs.deepseek.com/guides/thinking_mode>
- DeepSeek API reference (user_id): <https://api-docs.deepseek.com/guides/per_user_limits>

---
*Last Updated: 2026-07*
