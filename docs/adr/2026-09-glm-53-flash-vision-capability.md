# ADR-071: GLM-5.3-Flash Vision Capability via OpenAI-Compatible image_url Blocks

## Status
Accepted (2026-09)

## Context
Z.AI's `glm-5.3-flash` (served via an OpenAI-compatible endpoint at `https://api.z.ai/api/paas/v4`, provider type `openai`) is natively multimodal — images, videos, and files — but tell-me-go dropped image parts for it: `ResolveCapabilities` never set `SupportsVision` for any `glm-*` model (only `kimi-*` and `deepseek-*-vision*` models got it), so the OpenAI transport's `hasSupportedMedia`/`mediaBlockFor` gates dropped the parts and `mediaOmittedFallback` substituted the placeholder "(image content omitted — this model does not support image input)".

Two facts shaped the classification design:

1. **There is no reliable naming convention across GLM generations.** Older vision models use a `V` suffix (`glm-4.5V`, `glm-4.6V`, `glm-4.6V-FlashX`); text models use bare `-flash` (`glm-4.7-flash` is text-only); and the first native multimodal GLM-5 model (`glm-5.3-flash`) has **no `V` marker at all**. A suffix/substring heuristic (`-flash`, `V`, `vision`) misclassifies either `glm-4.7-flash` or `glm-5.3-flash`.
2. **The wire format is standard Chat Completions.** Z.AI documents image input as `{"type":"image_url","image_url":{"url": ...}}` content blocks where `url` accepts a Base64 Data URL — the exact block shape the OpenAI transport already emits for vision-capable models via `mediaBlockFor`/`resolveURL` (base64 data URI for unbound parts, `FileUploadMode: FileUploadNone`). No Responses API, no new block types, no upload API.

## Decision

### Decision 1: Explicit allowlist for GLM vision classification (seeded with `glm-5.3-flash`)
`isGLMVisionModel(model)` returns true for exactly `glm-5.3-flash` (and the namespaced `*/glm-5.3-flash` form), mirroring the `isKimiK3Model` exact-match pattern. `resolveGLMFamily` sets only `SupportsVision`; `SupportsVideo` stays false, `FileUploadMode` stays `FileUploadNone`, and `MaxTokensField` stays `MaxTokensFieldLegacy` (correct for Z.AI's Chat Completions protocol). The allowlist is extended as Z.AI ships more multimodal GLM variants; a naming heuristic was rejected because `glm-4.7-flash` (text) and `glm-5.3-flash` (multimodal) are indistinguishable by pattern.

### Decision 2: Inline Base64 data URLs (`FileUploadMode: FileUploadNone`)
Images are sent inline as `data:<mime>;base64,...` inside `image_url.url` — the path Z.AI documents and the transport already implements for OpenAI-family vision models. No turn-scoped Files API lifecycle (the DeepSeek `FileUploadDeepSeek` upload/delete machinery) is used.

### Decision 3: No new size guard for the GLM inline path
The OpenAI-family inline path has no byte/dimension guard today (`maxInlineMediaBytes`/`maxRequestBodyBytes` are consulted only by `checkDeepSeekMediaSizes` under `FileUploadMode == FileUploadDeepSeek`). For v1 this stays: Z.AI publishes no inline-image size limit, so inventing one would be a guess; oversized images are rejected by the provider (a recoverable 4xx). The gap is documented here; a transport-sanity byte ceiling reusing `maxRequestBodyBytes` remains a candidate if Z.AI publishes a limit.

## Consequences

### Positive
- Image input works on `zai-flash` (`glm-5.3-flash`) with a capability-layer-only change — zero transport modification.
- Text-only turns are byte-identical to before; `glm-5.3` and `glm-4.7-flash` remain text-only (`SupportsVision: false`), pinned by tests.
- The allowlist is honest about the absence of a GLM naming convention and rot-resistant for a family with a handful of models.

### Negative / Accepted Trade-offs
- Allowlist rot: a future multimodal GLM variant not added to the allowlist is silently text-only until recorded. Mitigated by the D1 rationale and architect review on model additions.
- No inline size guard: an oversized image fails at the provider (4xx) rather than client-side (Decision 3).
- Out of scope: video input (`video_url`), file input (`file_url`), and `reasoning_effort`/`thinking` control for GLM (the existing config already works; Z.AI documents `thinking.type` supports only `enabled`).

### Neutral
- The GLM resolver is additive — no existing family's capability output changes.
- `SupportsVideo` and `FileUploadMode` remain kimi/deepseek-scoped; GLM video is a separate follow-up (one extra capability flag + test).

## Alternatives Considered

1. **Naming heuristic (`glm-` prefix ∧ suffix rule).** Rejected — `glm-4.7-flash` (text) vs `glm-5.3-flash` (multimodal) are indistinguishable by pattern; the `V`-suffix convention does not extend to GLM-5.
2. **Transport-sanity byte ceiling (Decision 3 Option B).** Deferred — Z.AI publishes no inline-image size limit; a client-side guess adds surface without provider backing.

## Verification
- `internal/domain/llm/capabilities_test.go`: `TestResolveCapabilities` rows for `glm-5.3-flash` (true), namespaced `z.ai/glm-5.3-flash` (true), `glm-5.3` (false), `glm-4.7-flash` (false), `glm-4.5V` (false); `TestResolveCapabilities_FileUploadMode` row (`glm-5.3-flash` → `FileUploadNone`); `TestCapabilities_FileUploadMode_OutOfRange` row.
- `internal/infrastructure/llm/openai/client_vision_test.go`: `TestVision_GLMImagePayload` — a `glm-5.3-flash` client serializes a user image as `{"type":"image_url","image_url":{"url":"data:image/png;base64,..."}}` with the text block, and no `file`/`file_id`/`ms://` blocks.
- `make check-full` passes (including `verify-nonfix-catalog` and `verify-adr-index`).

## Related ADRs
- [ADR-070](2026-09-deepseek-vision-capability.md) — the vision-classification + `FileUploadMode` decomposition precedent this decision extends.
- [ADR-024](2026-04-openai-budget-field-divergence.md) — enums-over-booleans precedent; `FileUploadMode`/`MaxTokensField` are reused, not extended.

## References
- Z.AI GLM-5.3-Flash model page: https://docs.z.ai/guides/vlm/glm-5.3-flash.md
- Z.AI Chat Completion API reference: https://docs.z.ai/api-reference/llm/chat-completion.md
- Z.AI GLM-5.3 (text-only) page: https://docs.z.ai/guides/llm/glm-5.3.md
- Issue #1449.
- ADR-070 files-changed precedent: `internal/domain/llm/capabilities.go`, `internal/infrastructure/llm/openai/{client.go,files.go}`, test files, `configs/butler.yaml`, `README.md`.

---
*Last Updated: 2026-09 (issue #1449)*
