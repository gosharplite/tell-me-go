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
		supportsReasoningContent     bool
		supportsThinkingToggle       bool
		supportsVision               bool
		supportsVideo                bool
		requiresVertexThinkingKwargs bool
	}{
		{
			model:                    "gpt-4",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			model:                    "o1-mini",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     false,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldCompletion,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			model:                    "gpt-5",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     false,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldCompletion,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			name:                     "gpt-5.3 does not require responses API (minor < 4)",
			model:                    "gpt-5.3",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     false,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldCompletion,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			name:                     "gpt-4.5 does not require responses API (major < 5)",
			model:                    "gpt-4.5",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			model:                    "gpt-5.4",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     true,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldOutput,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			model:                    "gpt-6",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     true,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldOutput,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			model:                    "gpt-7",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     true,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldOutput,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			name:                     "gpt-7.0 requires responses API (major>5, n>=2)",
			model:                    "gpt-7.0",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     true,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldOutput,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			name:                     "gpt-10.1 requires responses API (major>5, n>=2, two-digit major)",
			model:                    "gpt-10.1",
			supportsReasoningEffort:  true,
			requiresResponsesAPI:     true,
			useDeveloperRole:         true,
			maxTokensField:           MaxTokensFieldOutput,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			model:                    "deepseek-reasoner",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           false,
		},
		{
			model:                    "deepseek-ai/deepseek-r1-0528-maas",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           false,
		},
		{
			model:                    "deepseek-r1",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           false,
		},
		{
			model:                    "deepseek-v3",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           false,
		},
		{
			model:                    "deepseek-ai/deepseek-v3",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           false,
		},
		{
			name:                         "vertex deepseek requires thinking kwargs",
			model:                        "deepseek-ai/deepseek-v3.2-maas",
			baseURL:                      "https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/endpoints/openapi",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   true,
			supportsReasoningContent:     true,
			supportsThinkingToggle:       true,
			supportsVision:               false,
			requiresVertexThinkingKwargs: true,
		},
		{
			name:                         "direct deepseek does not require thinking kwargs",
			model:                        "deepseek-reasoner",
			baseURL:                      "https://api.deepseek.com",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   true,
			supportsReasoningContent:     true,
			supportsThinkingToggle:       true,
			supportsVision:               false,
			requiresVertexThinkingKwargs: false,
		},
		{
			name:                         "non-deepseek on vertex does not require thinking kwargs",
			model:                        "gemini-3-flash-preview",
			baseURL:                      "https://aiplatform.googleapis.com/v1/projects/p/locations/global/publishers/google/models",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   false,
			supportsReasoningContent:     false,
			supportsVision:               false,
			requiresVertexThinkingKwargs: false,
		},
		{
			name:                         "deepseek with empty base URL does not require thinking kwargs (defensive default)",
			model:                        "deepseek-reasoner",
			baseURL:                      "",
			maxTokensField:               MaxTokensFieldLegacy,
			isDeepSeek:                   true,
			supportsReasoningContent:     true,
			supportsThinkingToggle:       true,
			supportsVision:               false,
			requiresVertexThinkingKwargs: false,
		},
		{
			name:                     "kimi-k3",
			model:                    "kimi-k3",
			baseURL:                  "",
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsReasoningEffort:  true,
			supportsVision:           true,
			supportsVideo:            true,
			maxTokensField:           MaxTokensFieldCompletion,
		},
		{
			name:                     "kimi-k3 namespaced (moonshotai/kimi-k3)",
			model:                    "moonshotai/kimi-k3",
			baseURL:                  "",
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsReasoningEffort:  true,
			supportsVision:           true,
			supportsVideo:            true,
			maxTokensField:           MaxTokensFieldCompletion,
		},
		{
			name:                     "kimi-k2.7-code",
			model:                    "kimi-k2.7-code",
			baseURL:                  "",
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsReasoningEffort:  false,
			supportsVision:           true,
			supportsVideo:            true,
			maxTokensField:           MaxTokensFieldLegacy,
		},
		{
			name:                     "kimi-k2.6",
			model:                    "kimi-k2.6",
			baseURL:                  "",
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsReasoningEffort:  false,
			supportsVision:           true,
			supportsVideo:            true,
			maxTokensField:           MaxTokensFieldLegacy,
		},
		{
			model:                    "deepseek-v4-flash-vision-exp",
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           true,
			supportsVideo:            false,
		},
		{
			model:                    "deepseek-v4-flash",
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           false,
			supportsVideo:            false,
		},
		{
			model:                    "deepseek-v4-pro",
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               true,
			supportsReasoningContent: true,
			supportsThinkingToggle:   true,
			supportsVision:           false,
			supportsVideo:            false,
		},
		{
			name:                     "glm-5.3-flash is native multimodal + always-reasoning (Z.AI)",
			model:                    "glm-5.3-flash",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: true,
			supportsVision:           true,
		},
		{
			name:                     "glm-5.3-flash namespaced (z.ai/glm-5.3-flash)",
			model:                    "z.ai/glm-5.3-flash",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: true,
			supportsVision:           true,
		},
		{
			name:                     "glm-5.3 is text-only but always-reasoning (Z.AI docs)",
			model:                    "glm-5.3",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: true,
			supportsVision:           false,
		},
		{
			name:                     "glm-4.7-flash is text-only (allowlist, not suffix heuristic)",
			model:                    "glm-4.7-flash",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			name:                     "glm-4.5V not in allowlist (V-suffix heuristic rejected)",
			model:                    "glm-4.5V",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: false,
			supportsVision:           false,
		},
		{
			name:                     "glm-5.3 namespaced (z.ai/glm-5.3) is always-reasoning",
			model:                    "z.ai/glm-5.3",
			supportsReasoningEffort:  false,
			requiresResponsesAPI:     false,
			useDeveloperRole:         false,
			maxTokensField:           MaxTokensFieldLegacy,
			isDeepSeek:               false,
			supportsReasoningContent: true,
			supportsVision:           false,
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
			if caps.SupportsReasoningContent != tt.supportsReasoningContent {
				t.Errorf("expected SupportsReasoningContent %v, got %v", tt.supportsReasoningContent, caps.SupportsReasoningContent)
			}
			if caps.SupportsThinkingToggle != tt.supportsThinkingToggle {
				t.Errorf("expected SupportsThinkingToggle %v, got %v", tt.supportsThinkingToggle, caps.SupportsThinkingToggle)
			}
			if caps.SupportsVision != tt.supportsVision {
				t.Errorf("expected SupportsVision %v, got %v", tt.supportsVision, caps.SupportsVision)
			}
			if caps.SupportsVideo != tt.supportsVideo {
				t.Errorf("expected SupportsVideo %v, got %v", tt.supportsVideo, caps.SupportsVideo)
			}
			if caps.RequiresVertexThinkingKwargs != tt.requiresVertexThinkingKwargs {
				t.Errorf("expected RequiresVertexThinkingKwargs %v, got %v", tt.requiresVertexThinkingKwargs, caps.RequiresVertexThinkingKwargs)
			}
		})
	}
}

func TestParseGPTVersion(t *testing.T) {
	tests := []struct {
		model string
		want  gptVersion
	}{
		{"gpt-4", gptVersion{major: 4, minor: 0, ok: true}},
		{"gpt-5", gptVersion{major: 5, minor: 0, ok: true}},
		{"gpt-5.4", gptVersion{major: 5, minor: 4, ok: true}},
		{"gpt-5.3", gptVersion{major: 5, minor: 3, ok: true}},
		{"gpt-4.5", gptVersion{major: 4, minor: 5, ok: true}},
		{"gpt-6", gptVersion{major: 6, minor: 0, ok: true}},
		{"gpt-6.1", gptVersion{major: 6, minor: 1, ok: true}},
		{"gpt-4o", gptVersion{major: 4, minor: 0, ok: true}}, // Sscanf stops at 'o'
		{"gpt-", gptVersion{ok: false}},                      // prefix present, nothing parseable → n==0
		{"gpt-abc", gptVersion{ok: false}},                   // prefix present, non-numeric → n==0
		{"deepseek-v3", gptVersion{ok: false}},
		{"o1", gptVersion{ok: false}},
		{"claude-3", gptVersion{ok: false}},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := parseGPTVersion(tt.model)
			if got != tt.want {
				t.Errorf("parseGPTVersion(%q) = %+v; want %+v", tt.model, got, tt.want)
			}
		})
	}
}

func TestResolveCapabilities_SupportsVideo_TrueForKimiModels(t *testing.T) {
	kimiModels := []string{"kimi-k3", "kimi-k2.7-code", "kimi-k2.6"}
	for _, model := range kimiModels {
		t.Run(model, func(t *testing.T) {
			caps := ResolveCapabilities(model, "")
			if !caps.SupportsVideo {
				t.Errorf("expected SupportsVideo=true for %s, got false", model)
			}
		})
	}
}

func TestResolveCapabilities_FileUploadMode(t *testing.T) {
	tests := []struct {
		model string
		want  FileUploadMode
	}{
		{"kimi-k3", FileUploadKimi},
		{"kimi-k2.7-code", FileUploadKimi},
		{"kimi-k2.6", FileUploadKimi},
		{"deepseek-v4-flash-vision-exp", FileUploadDeepSeek},
		{"deepseek-v4-flash", FileUploadNone},
		{"deepseek-v4-pro", FileUploadNone},
		{"deepseek-reasoner", FileUploadNone},
		{"gpt-5.5", FileUploadNone},
		{"glm-5.3-flash", FileUploadNone},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := ResolveCapabilities(tt.model, "").FileUploadMode; got != tt.want {
				t.Errorf("ResolveCapabilities(%q).FileUploadMode = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestCapabilities_FileUploadMode_OutOfRange(t *testing.T) {
	models := []string{
		"gpt-5.5", "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp",
		"deepseek-reasoner", "kimi-k3", "kimi-k2.7-code", "kimi-k2.6",
		"claude-opus-4-7", "gemini-3-flash-preview",
		"glm-5.3-flash",
	}
	for _, m := range models {
		t.Run(m, func(t *testing.T) {
			mode := ResolveCapabilities(m, "").FileUploadMode
			if mode < FileUploadNone || mode > FileUploadDeepSeek {
				t.Errorf("model %s: FileUploadMode out of range: %d", m, mode)
			}
		})
	}
}
