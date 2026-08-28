# ADR-073: GPT-5.x Vision via the Responses API — Full-History Image Routing, gpt-5+ Vision, Dimension Guard

## Status
Accepted (2026-09)

## Context
Issue #1448 supersedes #1447; the three locked decisions (D1/D2/D3) were ratified by the owner in #1447 comment 5449555045 and are binding — the ADR records them, it does not re-litigate them.

Pre-D1 behavior: the responses sink dropped images with a warn-and-drop (`responses_sink_non_string_content`); routing was `RequiresResponsesAPI && toolCount > 0 && hasEffort`; gpt-5.x had no `SupportsVision`.

## Decision

### D1 — Full-history sticky hasImage routing
The routing formula is exactly:

```
shouldUseResponses = RequiresResponsesAPI && (hasImage(history) || (toolCount > 0 && hasEffort))
```

- `hasImage` scans the FULL history (non-system parts with `image/*` InlineData). Full-history scan makes the decision sticky: once an image enters the history, every subsequent turn still routes to `/responses` while the image is present. Stickiness is DERIVED, never stored — the client is process-cached (`lazyClient`), so a stored latch would leak across sessions.
- The decision is computed once in `resolveAPIStrategy` and carried on the request (`chatRequest.routeResponses`, `json:"-"`) so `resolveEndpoint` is a pure read — no lossy re-derivation.
- **Reasoning omission (spec §3, REVISION 2):** on a no-effort image-forced `/responses` turn the `reasoning` field is OMITTED entirely — never `"reasoning":{}` (`req.Reasoning` set only `if strat.hasEffort`).
- **Fail-loud input-side sink contract (correction #4):** the responses sink is the only non-total path. New sentinels `errUnhandledInputBlockType` (input side NEVER suppressible — distinct from the output-side `errUnhandledBlockType`, which stays suppressible via the ADR-024 `errors.Is` guard) and `errVideoInputNotImplemented` (video on the Responses API is out of scope, issue #1447). `responsesSink` carries an `err` accumulator (`fail()` first-error-wins); on error `AddMessage` appends nothing and `toResponsesInput` returns the error — the turn aborts before any HTTP request. Translation: `string` → role-typed text block (byte-identical); `requestContentBlock` → `input_text`/`output_text` via `resolveBlockType`; `imageURLBlock` → `input_image` guarded by `if !SupportsVision { fail-loud }` (gate-agreement assertion, not a drop); `videoURLBlock` → fail-loud; default → fail-loud with `errUnhandledInputBlockType`. `standardSink` stays total with `mediaOmittedFallback`; the responses path never uses the placeholder.

### D2 — All gpt-5+ vision-capable via `isGpt5OrNewer`, boundary preserved
`resolveGPTFamily` sets `SupportsVision = isGpt5OrNewer(v)` — no allowlist, no separate threshold. **Boundary:** gpt-5.0–5.3 become vision-capable via the existing Chat Completions `image_url` path (they do NOT `RequireResponsesAPI`); only `RequiresResponsesAPI` models (gpt-5.4+) route images to `/responses`. No new capability flag exists — the routing predicate uses only `RequiresResponsesAPI` (GLM/DeepSeek/Kimi cannot be captured: false for all). `SupportsVision` union is composed in an unexported `supportsVisionFor` helper so `ResolveCapabilities` stays at the CC ≤ 10 policy threshold (T2 adjudication: Option B extraction, rejecting a CC=11 production pin).

### D3 — Dimension guard, fail-loud, `RequiresResponsesAPI`-scoped
Client-side cap 2048 px on the longest edge (drift-verified against the OpenAI images-vision guide; provider-side rejection threshold 6000 px; enforcement dimension = longest edge). Violations fail loud via the new domain-typed `MediaSizeError` (`internal/domain/llm`, fields {Kind, Mode, Cap, Actual}, pre-rendered message, `Unwrap() → llm.ErrTerminal`) — **no LLMError enum growth** (RecoveryStep's cataloged `structurally-unreachable` default arm must stay unreachable). Undecodable formats (e.g. WebP — stdlib `image` has no decoder) are SKIPPED (provider-enforced), never failed. The guard runs in `prepareMediaAssets` before any request, gated on `RequiresResponsesAPI` — covering exactly the image turns routed to `/responses` (gpt-5.4+). GLM/DeepSeek are excluded (`RequiresResponsesAPI` false — ADR-071's no-inline-guard for GLM preserved). **gpt-5.0–5.3 Chat Completions image turns are UNGUARDED by design** — see open items.

## Open Items

Deferred — user decision: NO live smoke tests were executed.

1. **gpt-5.0–5.3 tool+image on Chat Completions** — routing is deterministic (stays on `/chat/completions`, pinned by tests) but the wire acceptance of that combination is untested. Deferred, needs credentials.
2. **Omitted-reasoning wire acceptance** — the CODE contract is settled: per spec §3, a no-effort image-forced `/responses` turn omits the `reasoning` field entirely (never `"reasoning":{}`). The open item is purely external: whether the provider accepts an absent `reasoning` field on such a turn is a live-wire fact requiring credentials. Deferred; not executed per the skip-live-tests decision.

## Consequences

### Positive
- gpt-5.4+ image input works end-to-end (`input_image` blocks, media-first); gpt-5.0–5.3 images flow through the existing `image_url` chat path; byte-identical text-only wire for all families; GLM/DeepSeek/Kimi untouched; input-side sink failures abort before HTTP (no silent drops).

### Negative / Accepted Trade-offs
- gpt-5.0–5.3 chat-path images unguarded (deferred); undecodable formats skip the dimension guard (provider-enforced); `"reasoning"` omission on no-effort image turns awaits provider acceptance (open item); `historyItem.Content` widened to `[]any` (text-only JSON unchanged).

### Neutral
- `ResolveCapabilities` CC 10→8 via `supportsVisionFor` (Option B); `historyHasImage` full-history scan is O(history) per turn (bounded by context assembly).

## Alternatives Considered

1. `SupportsResponsesImages` capability flag in the routing predicate — REJECTED: the routing formula must be the spec formula verbatim (owner ratification).
2. Client-side mutable sticky latch — REJECTED: process-cached client would leak across sessions; stickiness is derived from the full-history scan.
3. Option A: ACCEPTED production pin for `ResolveCapabilities` at CC=11 — REJECTED in favor of the `supportsVisionFor` extraction (refactor-on-touch; a pin would rot as the capability table grows).
4. Warn-and-drop responses sink — REJECTED: superseded by the fail-loud input-side contract (correction #4).
5. Guard gated on a gpt-5.x-scoped capability vs `RequiresResponsesAPI` — the latter chosen: exact coverage of the responses-routed image turns, zero new capability surface, GLM/DeepSeek excluded by construction.

## Verification
- T1 `internal/domain/llm/media_size_error_test.go` — `TestNewMediaSizeError_Message`, `TestMediaSizeError_Fields`, `TestMediaSizeError_UnwrapTerminal`, `TestMediaSizeError_Classification` (message/fields/Unwrap-terminal/classification).
- T2 `internal/domain/llm/capabilities_test.go` — `TestResolveCapabilities` gpt-5.x rows (gpt-5, 5.3, 5.4, 6, 7, 7.0, 10.1 → `SupportsVision: true`; gpt-4/4.5/o1-mini stay false) + D2 boundary rows `gpt-5.0` / `gpt-5.2` (vision-capable without responses API); `TestResolveCapabilities_FileUploadMode`, `TestCapabilities_FileUploadMode_OutOfRange` (gpt-5.5 → `FileUploadNone`), `TestParseGPTVersion`.
- T3 `internal/infrastructure/llm/openai/responses_routing_test.go` (14 tests) — `TestHistoryHasImage` (7-row predicate matrix); `TestResponsesRouting_GPT54ImageForcesResponses` (image → `/responses` + `input_image`); `TestResponsesRouting_NoEffortImageTurn_OmitsReasoning` (REVISION 2 pin); `TestResponsesRouting_ImageStickyAcrossTurns`; `TestResponsesRouting_FreshHistoryTextOnly_ChatCompletions`; `TestResponsesRouting_GPT50Image_StaysChatCompletions` (D2 boundary, gpt-5.0/5.3); `TestResponsesRouting_GPT50ImageWithTools_StaysChatCompletions`; `TestResponsesRouting_GLMImage_StaysChatCompletions` (ADR-071 regression); `TestResponsesRouting_DeepSeekVision_StaysChatCompletions` (ADR-070 regression); `TestResponsesRouting_TextOnly_ByteIdentical` (exact key set + input item); `TestVision_GPT5ResponsesImagePayload`; `TestVision_GPT5ChatImagePayload`; `TestResponsesSink_ConverterFailLoud` (7-row converter fail-loud matrix); `TestResponsesRouting_FailLoudAbortsBeforeHTTP` (zero requests).
- T4 `internal/infrastructure/llm/openai/media_dimensions_test.go` — `TestImageLongestEdge_Formats` (png/jpeg/gif decoder proof + tall-edge), `TestImageLongestEdge_Undecodable`, `TestCheckResponsesImageDimensions` (dimension matrix incl. 2049 → Cap/Actual/ErrTerminal, aggregate, undecodable-skip), `TestResponsesDimensionGuard_GPT54FailsLoud` (zero requests), `TestResponsesDimensionGuard_GPT50Unguarded`.
- `make check-full` is the final gate (T6).

## Related ADRs
- [ADR-070](2026-09-deepseek-vision-capability.md) — capability-axes + FileUploadMode precedent.
- [ADR-071](2026-09-glm-53-flash-vision-capability.md) — GLM allowlist; its no-inline-guard is preserved by the D3 gate.
- [ADR-072](2026-09-glm-reasoning-content-capability.md) — capability-axis precedent; CC boundary note for ResolveCapabilities.
- [ADR-024](2026-04-openai-budget-field-divergence.md) — output-side suppressible sentinel guard, contrasted with the input-side non-suppressible contract.

## References
- OpenAI images-vision guide (responses mode): https://developers.openai.com/api/docs/guides/images-vision?api-mode=responses (drift-verified 2048/6000/longest-edge).
- Issues #1448 (this), #1447 (grill + owner ratification, comment 5449555045), #1449 (GLM vision, ADR-071), #1451 (GLM reasoning, ADR-072).
- Commits: c04db42 (MediaSizeError), db79757 (D2 capability), 69baf71 (D1 routing + sink), f53160e (D3 guard).
- Files changed: `internal/domain/llm/{media_size_error.go,capabilities.go}`, `internal/infrastructure/llm/openai/{chat.go,client.go,responses.go,media_dimensions.go,responses_routing_test.go,media_dimensions_test.go,endpoint_test.go,client_edge_test.go}`, `docs/architect/INTENTIONAL_NON_FIXES.md` (re-anchor note).

---
*Last Updated: 2026-09 (issue #1448)*
