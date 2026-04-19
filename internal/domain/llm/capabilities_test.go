// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"testing"
)

func TestResolveCapabilities(t *testing.T) {
	tests := []struct {
		name                         string
		model                        string
		baseURL                      string
		supportsReasoningEffort      bool
		requiresResponsesAPI         bool
		useDeveloperRole             bool
		maxTokensField               MaxTokensField
		isDeepSeek                   bool
		requiresVertexThinkingKwargs bool
	}{
		{
			model:                   "gpt-4",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			maxTokensField:          MaxTokensFieldLegacy,
			isDeepSeek:              false,
		},
		{
			model:                   "o1-mini",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    false,
			useDeveloperRole:        true,
			maxTokensField:          MaxTokensFieldCompletion,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-5",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    false,
			useDeveloperRole:        true,
			maxTokensField:          MaxTokensFieldCompletion,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-5.4",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    true,
			useDeveloperRole:        true,
			maxTokensField:          MaxTokensFieldOutput,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-6",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    true,
			useDeveloperRole:        true,
			maxTokensField:          MaxTokensFieldOutput,
			isDeepSeek:              false,
		},
		{
			model:                   "gpt-7",
			supportsReasoningEffort: true,
			requiresResponsesAPI:    true,
			useDeveloperRole:        true,
			maxTokensField:          MaxTokensFieldOutput,
			isDeepSeek:              false,
		},
		{
			model:                   "deepseek-reasoner",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			maxTokensField:          MaxTokensFieldLegacy,
			isDeepSeek:              true,
		},
		{
			model:                   "deepseek-ai/deepseek-r1-0528-maas",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			maxTokensField:          MaxTokensFieldLegacy,
			isDeepSeek:              true,
		},
		{
			model:                   "deepseek-r1",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			maxTokensField:          MaxTokensFieldLegacy,
			isDeepSeek:              true,
		},
		{
			model:                   "deepseek-v3",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			maxTokensField:          MaxTokensFieldLegacy,
			isDeepSeek:              true,
		},
		{
			model:                   "deepseek-ai/deepseek-v3",
			supportsReasoningEffort: false,
			requiresResponsesAPI:    false,
			useDeveloperRole:        false,
			maxTokensField:          MaxTokensFieldLegacy,
			isDeepSeek:              true,
		},
		{
			name:                         "vertex deepseek requires thinking kwargs",
			model:                        "deepseek-ai/deepseek-v3.2-maas",
			baseURL:                      "https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/endpoints/openapi",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   true,
			requiresVertexThinkingKwargs: true,
		},
		{
			name:                         "direct deepseek does not require thinking kwargs",
			model:                        "deepseek-reasoner",
			baseURL:                      "https://api.deepseek.com",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   true,
			requiresVertexThinkingKwargs: false,
		},
		{
			name:                         "non-deepseek on vertex does not require thinking kwargs",
			model:                        "gemini-3-flash-preview",
			baseURL:                      "https://aiplatform.googleapis.com/v1/projects/p/locations/global/publishers/google/models",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   false,
			requiresVertexThinkingKwargs: false,
		},
		{
			name:                         "deepseek with empty base URL does not require thinking kwargs (defensive default)",
			model:                        "deepseek-reasoner",
			baseURL:                      "",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   true,
			requiresVertexThinkingKwargs: false,
		},
	}

	for _, tt := range tests {
		testName := tt.name
		if testName == "" {
			testName = tt.model
		}
		t.Run(testName, func(t *testing.T) {
			caps := ResolveCapabilities(tt.model, tt.baseURL)
			if caps.SupportsReasoningEffort != tt.supportsReasoningEffort {
				t.Errorf("expected SupportsReasoningEffort %v, got %v", tt.supportsReasoningEffort, caps.SupportsReasoningEffort)
			}
			if caps.RequiresResponsesAPI != tt.requiresResponsesAPI {
				t.Errorf("expected RequiresResponsesAPI %v, got %v", tt.requiresResponsesAPI, caps.RequiresResponsesAPI)
			}
			if caps.UseDeveloperRole != tt.useDeveloperRole {
				t.Errorf("expected UseDeveloperRole %v, got %v", tt.useDeveloperRole, caps.UseDeveloperRole)
			}
			if caps.MaxTokensField != tt.maxTokensField {
				t.Errorf("expected MaxTokensField %d, got %d", tt.maxTokensField, caps.MaxTokensField)
			}
			if caps.IsDeepSeek != tt.isDeepSeek {
				t.Errorf("expected IsDeepSeek %v, got %v", tt.isDeepSeek, caps.IsDeepSeek)
			}
			if caps.RequiresVertexThinkingKwargs != tt.requiresVertexThinkingKwargs {
				t.Errorf("expected RequiresVertexThinkingKwargs %v, got %v", tt.requiresVertexThinkingKwargs, caps.RequiresVertexThinkingKwargs)
			}
		})
	}
}
