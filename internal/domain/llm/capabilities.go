// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"
	"strings"
)

// Capabilities defines the feature set supported by a specific LLM model.
type Capabilities struct {
	// SupportsReasoningEffort indicates if the model supports the 'reasoning_effort' field.
	SupportsReasoningEffort bool
	// RequiresResponsesAPI indicates if the model requires the '/v1/responses' endpoint for combined tool use and reasoning.
	RequiresResponsesAPI bool
	// UseDeveloperRole indicates if the model prefers the 'developer' role over the 'system' role.
	UseDeveloperRole bool
	// UseMaxCompletionTokens indicates if the model uses 'max_completion_tokens' instead of 'max_tokens'.
	UseMaxCompletionTokens bool
	// IsDeepSeek indicates if the model follows DeepSeek-specific conventions (e.g., reasoning_content in assistant messages).
	IsDeepSeek bool
}

// ResolveCapabilities returns the capability set for a given model name.
func ResolveCapabilities(model string) Capabilities {
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

	isDeepSeek := strings.Contains(model, "deepseek-reasoner") ||
		strings.Contains(model, "deepseek-r1")

	caps.IsDeepSeek = isDeepSeek

	if isOpenAIReasoner {
		caps.UseMaxCompletionTokens = true
		caps.UseDeveloperRole = true
		caps.SupportsReasoningEffort = true
	}

	if isGpt54OrNewer(model) {
		caps.RequiresResponsesAPI = true
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
