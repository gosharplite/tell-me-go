// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"testing"
)

func TestResolveCapabilities(t *testing.T) {
	tests := []struct {
		model                   string
		supportsReasoningEffort bool
		requiresResponsesAPI    bool
		useDeveloperRole        bool
		useMaxCompletionTokens  bool
		isDeepSeek              bool
	}{
		{
			model:                   "gpt-4",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			useMaxCompletionTokens:  false,
			isDeepSeek:              false,
		},
		{
			model:                   "o1-mini",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    false,
			useDeveloperRole:        true,
			useMaxCompletionTokens:  true,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-5",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    false,
			useDeveloperRole:        true,
			useMaxCompletionTokens:  true,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-5.4",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    true,
			useDeveloperRole:        true,
			useMaxCompletionTokens:  true,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-6",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    true,
			useDeveloperRole:        true,
			useMaxCompletionTokens:  true,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-7",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    true,
			useDeveloperRole:        true,
			useMaxCompletionTokens:  true,
			isDeepSeek:              false,
		},
		{
			model:                   "deepseek-reasoner",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			useMaxCompletionTokens:  false,
			isDeepSeek:              true,
		},
		{
			model:                   "deepseek-ai/deepseek-r1-0528-maas",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			useMaxCompletionTokens:  false,
			isDeepSeek:              true,
		},
		{
			model:                   "deepseek-r1",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			useMaxCompletionTokens:  false,
			isDeepSeek:              true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			caps := ResolveCapabilities(tt.model)
			if caps.SupportsReasoningEffort != tt.supportsReasoningEffort {
				t.Errorf("expected SupportsReasoningEffort %v, got %v", tt.supportsReasoningEffort, caps.SupportsReasoningEffort)
			}
			if caps.RequiresResponsesAPI != tt.requiresResponsesAPI {
				t.Errorf("expected RequiresResponsesAPI %v, got %v", tt.requiresResponsesAPI, caps.RequiresResponsesAPI)
			}
			if caps.UseDeveloperRole != tt.useDeveloperRole {
				t.Errorf("expected UseDeveloperRole %v, got %v", tt.useDeveloperRole, caps.UseDeveloperRole)
			}
			if caps.UseMaxCompletionTokens != tt.useMaxCompletionTokens {
				t.Errorf("expected UseMaxCompletionTokens %v, got %v", tt.useMaxCompletionTokens, caps.UseMaxCompletionTokens)
			}
			if caps.IsDeepSeek != tt.isDeepSeek {
				t.Errorf("expected IsDeepSeek %v, got %v", tt.isDeepSeek, caps.IsDeepSeek)
			}
		})
	}
}
