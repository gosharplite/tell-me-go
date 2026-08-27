# ADR-070: DeepSeek Vision Capability and FileUploadMode Decomposition

## Status
Accepted (2026-09)

## Context
DeepSeek released an experimental vision model, `deepseek-v4-flash-vision-exp`, which accepts image input over the standard OpenAI-compatible Chat Completions endpoint (`https://api.deepseek.com/chat/completions`). The rolling aliases `deepseek-v4-flash` and `deepseek-v4-pro` remain text/reasoning models and return HTTP 400 (`"This model does not support image"`) when sent image content. Non-vision DeepSeek models must therefore never serialize image parts.

Two latent defects in the OpenAI transport blocked DeepSeek vision support:

1. **Kimi/DeepSeek file-API wire divergence.** `parseFileUploadResponse` validated that the upload response's `status` field was `"ok"` or `"ready"`. DeepSeek's `POST /files` response omits `status` entirely (`{id, object:"file", bytes, created_at, filename, purpose}`), so DeepSeek uploads failed with `upload status "" (expected ok or ready)`. Kimi requires `purpose` ∈ {`image`, `video`, `file-extract`} and references uploads as `ms://{file_id}` URLs inside `image_url` blocks; DeepSeek requires `purpose=user_data` and references uploads as `{"type":"file","file_id":"file-api-..."}` content blocks.
2. **Vision/video capability coupling.** `mediaBlocks` emitted `video_url` blocks whenever `SupportsVision || SupportsVideo` was true. A vision-only model (DeepSeek: `SupportsVision: true, SupportsVideo: false`) would serialize `video/*` parts to `video_url`, triggering an upstream 400.

DeepSeek's official documentation (pinned reference gist, commit `3d4d61e`) specifies: supported formats JPEG/PNG/GIF/WebP (server-sniffed from binary header); images in user messages only; inline base64 via `image_url` blocks; images > 32 MiB (≤ 64 MiB) via the Files API with `purpose=user_data`; aggregate inline request-body cap 48 MiB; token accounting ≤ 384 tokens/image (images scaled down to ≈800×800 or up to 384×384) billed alongside text tokens.

Predecessor issue #1427 was closed as superseded by this work.

## Decision

### Decision 1: Suffix-based DeepSeek vision capability resolution
`isDeepSeekVisionModel(model)` returns `strings.Contains(model, "vision")`. `resolveDeepSeekFamily` sets `SupportsVision = isDeepSeek && isDeepSeekVisionModel(model)`; `SupportsVideo` remains false for all DeepSeek models. This keeps non-vision DeepSeek models (flash/pro/reasoner) vision-free by construction — no per-model allow-list to rot.

### Decision 2: `FileUploadMode` enum replaces the `SupportsFileUpload` boolean
Following the ADR-024 `MaxTokensField` precedent (enums over coupled booleans make invalid combinations unrepresentable), `Capabilities.SupportsFileUpload bool` becomes `FileUploadMode FileUploadMode` with values `FileUploadNone`, `FileUploadKimi`, `FileUploadDeepSeek`. The enum is the single dispatch dimension for the transport's upload/parse behavior; the zero value (`FileUploadNone`) is the safe default for all non-uploading providers.

### Decision 3: Vision/video decoupling in the OpenAI transport
`mediaBlocks(parts, ta, caps)` emits `image_url`/`file` blocks only when `SupportsVision` is true and `video_url` blocks only when `SupportsVideo` is true. Video parts on vision-only models are dropped, not proxied or error-coupled. Mode-aware block creation: DeepSeek bound parts → `file` blocks; Kimi bound parts → `ms://` `image_url`/`video_url` blocks; unbound parts → base64 data-URI `image_url` blocks.

**Addendum (issue #1439):** when capability filtering drops *all* media parts from a user message that carries no text, `buildMessageContent` now replaces the would-be-empty content with a truth-telling placeholder string via `mediaOmittedFallback` — `"(video content omitted — this model does not support video input)"` for `video/*` on `!SupportsVideo` models, `"(image content omitted — this model does not support image input)"` for `image/*` on `!SupportsVision` models, and a generic `"(media content omitted — this model does not support this media type)"` fallback. The OpenAI-compatible endpoint rejects both a JSON `null` `content` (a typed-nil slice assigned to `interface{}` survives `omitempty`) and an omitted `content` key (an empty string is stripped by `omitempty`) with HTTP 400; the placeholder keeps `content` a valid non-empty string, preserves user-role message presence for strict role alternation, and leaks no transport capability into the domain layer.

### Decision 4: Turn-scoped Files API lifecycle with automated cleanup
Uploaded files are bound in `turnAssets`, referenced via `file_id` blocks for the duration of the turn, and deleted via `DELETE /files/{id}` by `turnAssets.release`, deferred in `SendChat` on every exit path (success and error). Cleanup uses a detached context with a bounded timeout so it proceeds even when the caller's context is at deadline. Size guards fail loud before any request: single images > 64 MiB error with `image exceeds 64 MiB upload limit: %d bytes`; aggregate base64 size of inline images > 48 MiB errors with `aggregate inline image size exceeds 48 MiB limit`.

### Decision 5: Non-goals and deferred features
1. **Anthropic-compatible endpoint**: `internal/infrastructure/llm/anthropic` drops all `InlineData` parts across all models; image support over Anthropic requires separate transport architecture.
2. **Persistent cross-request file reuse**: reusing `file_id` across turns requires domain `Part` changes to persist remote references; turn-scoped ephemeral uploads satisfy all immediate needs.
3. **External URL mode**: the domain `Part` represents local blobs (`InlineData`) or local asset IDs; external URL downloading is deferred.
4. **Content-based MIME sniffing**: `extractMediaParts` relies on declared MIME types; DeepSeek's server-side sniffing fallback is deferred.
5. **Image detail parameter** (`low`/`high`/`original`/`auto`): omitted for v1 (`auto` is the server default).
6. **Assistant/system image parts**: DeepSeek accepts images in user messages only; the client does not filter by role — server-side rejection is the enforcement (known limitation).

## Consequences

### Positive
- DeepSeek vision works over the existing OpenAI-compatible path with inline base64 for ≤ 32 MiB images and turn-scoped Files API uploads for larger images, with no Kimi regression.
- Invalid combinations are unrepresentable: a provider either has no upload path, Kimi semantics, or DeepSeek semantics — no coupled boolean matrix.
- Vision-only models can no longer emit `video_url`; the decoupling prevents a whole class of upstream-400 defects for any future vision-only model.
- Size guards fail loud before network I/O, preventing silent truncation or 4xx/5xx from oversized payloads.

### Negative / Accepted Trade-offs
- Video parts on vision-only models are silently dropped (no proxy-to-image fallback). The dropping never produces an empty user message on the wire: `buildMessageContent` substitutes a truth-telling placeholder via `mediaOmittedFallback` (issue #1439), so `content` remains a valid non-empty string — the media itself is still not proxied or error-coupled.
- Uploaded files leak on process crash mid-turn (turn-scoped cleanup is best-effort; no reaper).
- The suffix-based vision detection (`strings.Contains(model, "vision")`) is a heuristic; a future non-vision model whose ID contains "vision" would be misclassified (mitigated by the `-exp` experimental naming and architect review on model additions).
- The `FileUploadMode` enum adds a third dispatch dimension to the transport; each new mode must be handled in `parseFileUploadResponse`, `uploadMediaParts`/`mediaUploadPurpose`, and `mediaBlockFor`.

### Neutral
- `isDeepSeekModel` continues to gate reasoning-content and thinking-toggle behavior; vision classification is additive, not a reclassification.
- `extractDocument` (the Kimi file-extract pipeline) inherits the mode via `uploadFile`; non-Kimi clients calling it now fail loudly (`file upload not supported`).

## Alternatives Considered

1. **Per-model vision allow-list.** Rejected — rots as models are added; the suffix heuristic keeps classification colocated with the existing DeepSeek family resolution.
2. **Two booleans (`SupportsFileUpload` + a DeepSeek-file flag).** Rejected — the ADR-024 lesson: coupled booleans make invalid combinations representable (e.g., both true with ambiguous semantics).
3. **Role-filtered image emission (user messages only, client-side).** Deferred — server-side enforcement is authoritative; client-side filtering would require threading role context into `mediaBlocks` for marginal benefit.
4. **Upload-all-then-reference (Kimi semantics for DeepSeek).** Rejected — DeepSeek's inline path for ≤ 32 MiB images avoids upload latency and API quotas; the size-threshold split is a wire-level requirement.

## Verification
- `internal/domain/llm/capabilities_test.go`: `TestResolveCapabilities` rows for `deepseek-v4-flash-vision-exp` / `deepseek-v4-flash` / `deepseek-v4-pro`; `TestResolveCapabilities_FileUploadMode`; `TestCapabilities_FileUploadMode_OutOfRange`.
- `internal/infrastructure/llm/openai/client_vision_test.go`: `TestVision_DeepSeekImagePayload`, `TestVision_DeepSeekFileBlockPayload`, and the `TestMediaBlocks` vision-without-video / image-without-vision rows.
- `internal/infrastructure/llm/openai/files_test.go`: `TestUploadFile_DeepSeekResponse`, `TestPrepareMediaAssets_DeepSeek_SmallImageStaysInline`, `TestPrepareMediaAssets_DeepSeek_OversizedUploads`, `TestPrepareMediaAssets_DeepSeek_Over64MiB_Errors`, `TestPrepareMediaAssets_DeepSeek_AggregateBody_Errors`, `TestUploadFile_DeepSeekMissingID`, `TestUploadFile_DeepSeekWrongObject`, `TestUploadFile_NoneMode_Unsupported`, plus the migrated Kimi suite (unchanged behavior).
- All Kimi vision/video/file-upload tests remain green; `make check-full` passes (including `verify-nonfix-catalog` and `verify-adr-index`).

## Related ADRs
- [ADR-024](2026-04-openai-budget-field-divergence.md) — enums-over-booleans precedent for `MaxTokensField`; `FileUploadMode` follows the same shape.
- [ADR-052](2026-07-deepseek-thinking-toggle.md) — DeepSeek request-side classification precedent.
- [ADR-001](2026-01-multi-llm-provider-strategy.md) — the multi-provider abstraction in which this wire divergence arises.

## References
- DeepSeek vision documentation snapshot (pinned): <https://gist.github.com/gosharplite/b723baa51271546edabd68ed8c988788> (commit `3d4d61e`).
- Predecessor issue #1427 (superseded).
- Issue #1439 (media-only content fallback addendum).
- Files changed: `internal/domain/llm/capabilities.go`, `internal/infrastructure/llm/openai/{client.go,files.go}`, `internal/infrastructure/llm/openai/{client_vision_test.go,files_test.go,client_edge_test.go}`, `configs/butler.yaml`, `README.md`, `docs/user/niffler/ait-base/engineers/configs/butler.yaml`.

---
*Last Updated: 2026-09 (issue #1439 addendum)*
