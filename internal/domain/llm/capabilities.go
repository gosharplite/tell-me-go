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
	// IsDeepSeek indicates if the model follows DeepSeek-specific conventions (e.g., reasoning_content in assistant messages).
	IsDeepSeek bool
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

// ResolveCapabilities returns the capability set for a given model name and
// provider base URL. The base URL is required for transport-conditional
// capabilities such as RequiresVertexThinkingKwargs. Pass an empty string
// if the URL is not available; transport-conditional capabilities will
// default to false.
func ResolveCapabilities(model, baseURL string) Capabilities {
	var caps Capabilities

	// OpenAI Reasoner detection
	isOpenAIReasoner := strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3")

	if strings.HasPrefix(model, "gpt-") {
		version := strings.TrimPrefix(model, "gpt-")
		var major int
		n, _ := fmt.Sscanf(version, "%d", &major)
		if n >= 1 && major >= 5 {
			isOpenAIReasoner = true
		}
	}

	isDeepSeek := strings.Contains(model, "deepseek-")

	caps.IsDeepSeek = isDeepSeek

	if isDeepSeek && strings.Contains(baseURL, "aiplatform.googleapis.com") {
		caps.RequiresVertexThinkingKwargs = true
	}

	if isOpenAIReasoner {
		caps.UseDeveloperRole = true
		caps.SupportsReasoningEffort = true
	}

	if isGpt54OrNewer(model) {
		caps.RequiresResponsesAPI = true
	}

	// Resolve MaxTokensField. The three values are mutually exclusive:
	//   RequiresResponsesAPI true → Output    (gpt-5.4+ on /responses)
	//   isOpenAIReasoner    true  → Completion (o-series, gpt-5.0..5.3)
	//   otherwise                 → Legacy    (DeepSeek, plain gpt-4)
	switch {
	case caps.RequiresResponsesAPI:
		caps.MaxTokensField = MaxTokensFieldOutput
	case isOpenAIReasoner:
		caps.MaxTokensField = MaxTokensFieldCompletion
	default:
		caps.MaxTokensField = MaxTokensFieldLegacy
	}

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
