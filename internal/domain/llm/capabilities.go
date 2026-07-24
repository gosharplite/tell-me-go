// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"
	"strings"
)

// MaxTokensField identifies which JSON field name the model's transport
// requires for the per-request output-token budget. The three fields
// are mutually exclusive at the wire level — sending the wrong one
// yields an HTTP 400 from the provider.
//
// See ADR-024 (forthcoming) for the historical rationale: OpenAI
// renamed the field across two API generations (Chat Completions →
// Responses), and DeepSeek retained the original name. Modeling this
// as an enum rather than coupled booleans makes invalid combinations
// unrepresentable.
type MaxTokensField int

const (
	// MaxTokensFieldLegacy → JSON key "max_tokens".
	// Used by: DeepSeek (all variants), legacy OpenAI Chat Completions
	// models that predate the o-series (e.g., gpt-4, gpt-3.5).
	MaxTokensFieldLegacy MaxTokensField = iota

	// MaxTokensFieldCompletion → JSON key "max_completion_tokens".
	// Used by: OpenAI o-series and gpt-5.0..gpt-5.3 on /chat/completions.
	MaxTokensFieldCompletion

	// MaxTokensFieldOutput → JSON key "max_output_tokens".
	// Used by: OpenAI gpt-5.4+ when routed to /responses (i.e., when
	// tools are present and reasoning_effort is set).
	MaxTokensFieldOutput
)

// Capabilities defines the feature set supported by a specific LLM model.
type Capabilities struct {
	// SupportsReasoningEffort indicates if the model supports the 'reasoning_effort' field.
	SupportsReasoningEffort bool
	// RequiresResponsesAPI indicates if the model requires the '/v1/responses' endpoint for combined tool use and reasoning.
	RequiresResponsesAPI bool
	// UseDeveloperRole indicates if the model prefers the 'developer' role over the 'system' role.
	UseDeveloperRole bool
	// MaxTokensField names the wire-format field used for the per-request
	// output-token budget. Replaces the earlier UseMaxCompletionTokens
	// boolean so that the three mutually exclusive choices are
	// represented as three mutually exclusive enum values rather than
	// as a coupled boolean plus an implicit precedence rule. See
	// MaxTokensField for the per-value semantics.
	MaxTokensField MaxTokensField
	// IsDeepSeek indicates if the model is a DeepSeek variant.
	// Prefer SupportsReasoningContent for reasoning_content serialization
	// decisions; IsDeepSeek is retained for classification, diagnostics,
	// and test assertions. It has no production code gating on it directly
	// as of the SupportsReasoningContent generalization.
	IsDeepSeek bool
	// SupportsReasoningContent indicates the model uses the reasoning_content
	// field on assistant messages for reasoning/thinking traces. When true,
	// thought parts are serialized into reasoning_content rather than being
	// wrapped in <thought> XML tags, and reasoning_content is always included
	// on assistant messages (even when empty) to satisfy the provider's
	// multi-turn protocol.
	//
	// Set for all models known to use reasoning_content natively:
	// deepseek-* and kimi-* model families.
	SupportsReasoningContent bool
	// SupportsVision indicates the model natively understands images via
	// image_url content parts in the messages array. When true, InlineData
	// parts are serialized as base64 image_url blocks rather than being
	// silently dropped.
	//
	// Set for: kimi-* models (K3, K2.7, K2.6).
	SupportsVision bool
	// SupportsVideo indicates the model natively understands video via
	// video_url content parts in the messages array. When true, InlineData
	// parts with video MIME types are uploaded with purpose="video" and
	// serialized as video_url blocks.
	//
	// Set for: kimi-* models (K3, K2.7, K2.6).
	SupportsVideo bool
	// SupportsFileUpload indicates the model supports file uploads via a
	// provider-specific file API (e.g., Kimi / Moonshot). When true,
	// InlineData parts with image or video MIME types are uploaded as
	// files before the chat request and referenced via provider-specific
	// URL schemes (e.g., ms://) in the message payload.
	//
	// Set for: kimi-* model family.
	SupportsFileUpload bool
	// RequiresVertexThinkingKwargs indicates that the transport silently
	// disables DeepSeek thinking mode unless the non-standard parameter
	// chat_template_kwargs.thinking=true is included in the request body.
	// Set when the model is a DeepSeek variant served via Vertex AI MaaS.
	// Verified empirically against Vertex deepseek-ai/deepseek-v3.2-maas
	// on 2025-12-04: without this flag completion_tokens=56 and no
	// reasoning_content; with it completion_tokens=203 and
	// reasoning_content present.
	RequiresVertexThinkingKwargs bool
}

// gptVersion holds the parsed major.minor components of a gpt- model name.
type gptVersion struct {
	major int
	minor int
	ok    bool // true if the model name had a parseable gpt- prefix
}

// parseGPTVersion extracts the major and minor version from a gpt-X or
// gpt-X.Y model name. The ok field is false for non-gpt models.
func parseGPTVersion(model string) gptVersion {
	if !strings.HasPrefix(model, "gpt-") {
		return gptVersion{}
	}
	version := strings.TrimPrefix(model, "gpt-")
	var major, minor int
	n, _ := fmt.Sscanf(version, "%d.%d", &major, &minor)
	if n >= 1 {
		return gptVersion{major: major, minor: minor, ok: true}
	}
	return gptVersion{}
}

// isGpt5OrNewer returns true for any gpt-5.x or later model.
func isGpt5OrNewer(v gptVersion) bool {
	return v.ok && v.major >= 5
}

// resolveTokenField picks the correct MaxTokensField enum value given
// the requireResponses flag and whether the model is a reasoner.
func resolveTokenField(requireResponses, isReasoner bool) MaxTokensField {
	switch {
	case requireResponses:
		return MaxTokensFieldOutput
	case isReasoner:
		return MaxTokensFieldCompletion
	default:
		return MaxTokensFieldLegacy
	}
}

// isDeepSeekModel returns true for DeepSeek model IDs, including
// namespaced variants like deepseek-ai/deepseek-v3.2-maas.
func isDeepSeekModel(model string) bool {
	return strings.Contains(model, "deepseek-")
}

// isKimiModel returns true for Kimi model IDs, including
// namespaced variants like moonshotai/kimi-k3.
func isKimiModel(model string) bool {
	return strings.Contains(model, "kimi-")
}

// isKimiK3Model returns true for Kimi K3 model IDs, including
// namespaced variants like moonshotai/kimi-k3.
func isKimiK3Model(model string) bool {
	return model == "kimi-k3" || strings.HasSuffix(model, "/kimi-k3")
}

// resolveGPTFamily derives GPT and o-series capabilities from the model string.
func resolveGPTFamily(model string) (isReasoner bool, requireResponses bool) {
	v := parseGPTVersion(model)
	isReasoner = strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") ||
		isGpt5OrNewer(v)
	requireResponses = isGpt54OrNewer(model)
	return
}

// resolveDeepSeekFamily derives DeepSeek capabilities from the model string and
// base URL. The base URL is used only for RequiresVertexThinkingKwargs detection.
func resolveDeepSeekFamily(model, baseURL string) (isDeepSeek, supportsReasoningContent, requiresVertexThinkingKwargs bool) {
	isDeepSeek = isDeepSeekModel(model)
	supportsReasoningContent = isDeepSeek
	if isDeepSeek && strings.Contains(baseURL, "aiplatform.googleapis.com") {
		requiresVertexThinkingKwargs = true
	}
	return
}

// resolveKimiFamily derives Kimi capabilities from the model string.
func resolveKimiFamily(model string) (supportsReasoningContent, supportsVision, supportsVideo, supportsFileUpload, supportsReasoningEffort, isKimiK3 bool) {
	if !isKimiModel(model) {
		return
	}
	supportsReasoningContent = true
	supportsVision = true
	supportsVideo = true
	supportsFileUpload = true
	if isKimiK3Model(model) {
		supportsReasoningEffort = true
		isKimiK3 = true
	}
	return
}

// ResolveCapabilities returns the capability set for a given model name and
// provider base URL. The base URL is required for transport-conditional
// capabilities such as RequiresVertexThinkingKwargs. Pass an empty string
// if the URL is not available; transport-conditional capabilities will
// default to false.
func ResolveCapabilities(model, baseURL string) Capabilities {
	isReasoner, requireResponses := resolveGPTFamily(model)
	isDeepSeek, dsReasoningContent, vertexThinkingKwargs := resolveDeepSeekFamily(model, baseURL)
	kReasoningContent, supportsVision, supportsVideo, supportsFileUpload, kReasoningEffort, isKimiK3 := resolveKimiFamily(model)

	caps := Capabilities{
		SupportsReasoningEffort:      isReasoner || kReasoningEffort,
		RequiresResponsesAPI:         requireResponses,
		UseDeveloperRole:             isReasoner,
		IsDeepSeek:                   isDeepSeek,
		SupportsReasoningContent:     dsReasoningContent || kReasoningContent,
		SupportsVision:               supportsVision,
		SupportsVideo:                supportsVideo,
		SupportsFileUpload:           supportsFileUpload,
		RequiresVertexThinkingKwargs: vertexThinkingKwargs,
	}

	isCompletionTokensModel := isReasoner || isKimiK3
	caps.MaxTokensField = resolveTokenField(requireResponses, isCompletionTokensModel)

	return caps
}

func isGpt54OrNewer(model string) bool {
	if !strings.HasPrefix(model, "gpt-") {
		return false
	}
	version := strings.TrimPrefix(model, "gpt-")
	var major, minor int
	n, _ := fmt.Sscanf(version, "%d.%d", &major, &minor)
	if n >= 2 {
		if major > 5 {
			return true
		}
		if major == 5 && minor >= 4 {
			return true
		}
	} else if n == 1 {
		if major > 5 {
			return true
		}
	}
	return false
}
