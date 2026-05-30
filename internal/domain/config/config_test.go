// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/assert"
)

func TestResolveThinkingBudget(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Models: map[string]ModelConfig{
			"gemini-2.0-flash": {MaxThinkingBudget: 1000},
			"pro":              {MaxThinkingBudget: 5000},
		},
	}
	pData := pricing.PricingData{
		Models: map[string]pricing.ModelPricing{
			"default": {ThinkingBudget: 2000},
			"extra":   {ThinkingBudget: 10000},
		},
	}

	tests := []struct {
		model    string
		expected int
	}{
		{"gemini-2.0-flash", 1000},   // Exact match in Config
		{"gemini-2.0-pro-exp", 5000}, // Substring match ("pro") in Config
		{"extra-special", 10000},     // Substring match in PricingData
		{"unknown", 2000},            // Default in PricingData
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			t.Parallel()
			got := cfg.ResolveThinkingBudget(tt.model, pData)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestResolveContextWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		model            string
		maxHistoryTokens int
		models           map[string]ModelConfig
		expected         int
	}{
		{
			name:             "Fallback to MaxHistoryTokens",
			model:            "unknown",
			maxHistoryTokens: 100000,
			expected:         100000,
		},
		{
			name:             "Respect smaller ModelConfig limit",
			model:            "small-model",
			maxHistoryTokens: 100000,
			models: map[string]ModelConfig{
				"small-model": {ContextWindow: 50000},
			},
			expected: 50000,
		},
		{
			name:             "Respect MaxHistoryTokens if ModelConfig limit is larger",
			model:            "large-model",
			maxHistoryTokens: 100000,
			models: map[string]ModelConfig{
				"large-model": {ContextWindow: 200000},
			},
			expected: 100000,
		},
		{
			name:             "Substring match for ModelConfig",
			model:            "gemini-2.0-flash",
			maxHistoryTokens: 100000,
			models: map[string]ModelConfig{
				"flash": {ContextWindow: 60000},
			},
			expected: 60000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{
				Model:            tt.model,
				MaxHistoryTokens: tt.maxHistoryTokens,
				Models:           tt.models,
			}
			assert.Equal(t, tt.expected, cfg.ResolveContextWindow())
		})
	}
}

func TestFindBestMatch(t *testing.T) {
	t.Parallel()
	m := map[string]string{
		"gemini":           "gemini-match",
		"gemini-2.0":       "gemini-2.0-match",
		"gemini-2.0-flash": "exact-match",
	}

	isValid := func(s string) bool { return true }

	tests := []struct {
		name     string
		key      string
		expected string
		found    bool
	}{
		{
			name:     "Exact match",
			key:      "gemini-2.0-flash",
			expected: "exact-match",
			found:    true,
		},
		{
			name:     "Longest substring match",
			key:      "gemini-2.0-pro",
			expected: "gemini-2.0-match",
			found:    true,
		},
		{
			name:     "Shortest substring match",
			key:      "gemini-1.5-pro",
			expected: "gemini-match",
			found:    true,
		},
		{
			name:     "No match",
			key:      "claude-opus-4-6",
			expected: "",
			found:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, found := findBestMatch(m, tt.key, isValid)
			assert.Equal(t, tt.found, found)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDefaultPricing(t *testing.T) {
	t.Parallel()
	pData := DefaultPricing()
	assert.NotEmpty(t, pData.Models)
	assert.True(t, pData.Models["default"].ThinkingBudget >= 0)
	assert.True(t, pData.Models["gemini-3-flash-preview"].SearchQuery > 0)
}

func TestGetActiveProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		config   Config
		expected LLMProvider
	}{
		{
			name: "Fallback to legacy config when no provider selected",
			config: Config{
				URL:            "legacy-url",
				Model:          "legacy-model",
				ThinkingBudget: 1000,
				ThinkingLevel:  "high",
			},
			expected: LLMProvider{
				Type:           "gemini",
				URL:            "legacy-url",
				Model:          "legacy-model",
				ThinkingBudget: 1000,
				ThinkingLevel:  "high",
			},
		},
		{
			name: "Fallback to legacy config when selected provider not found",
			config: Config{
				SelectedProvider: "non-existent",
				URL:              "legacy-url",
				Model:            "legacy-model",
				ThinkingBudget:   1000,
				ThinkingLevel:    "high",
			},
			expected: LLMProvider{
				Type:           "gemini",
				URL:            "legacy-url",
				Model:          "legacy-model",
				ThinkingBudget: 1000,
				ThinkingLevel:  "high",
			},
		},
		{
			name: "Return selected provider configuration",
			config: Config{
				SelectedProvider: "openai-test",
				Providers: map[string]LLMProvider{
					"openai-test": {
						Type:           "openai",
						URL:            "openai-url",
						APIKey:         "test-key",
						Model:          "gpt-4",
						ThinkingBudget: 2000,
						ThinkingLevel:  "medium",
						Headers:        map[string]string{"X-Test": "value"},
					},
				},
			},
			expected: LLMProvider{
				Type:           "openai",
				URL:            "openai-url",
				APIKey:         "test-key",
				Model:          "gpt-4",
				ThinkingBudget: 2000,
				ThinkingLevel:  "medium",
				Headers:        map[string]string{"X-Test": "value"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.config.GetActiveProvider()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestConfig_GetFailoverProviders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		failoverOrder []string
		providers     map[string]LLMProvider
		expectedLen   int
		expectedTypes []string // nil means nil slice expected
	}{
		{
			name:          "empty order",
			failoverOrder: []string{},
			providers:     map[string]LLMProvider{"p1": {Type: "openai"}},
			expectedLen:   0,
			expectedTypes: nil,
		},
		{
			name:          "all found",
			failoverOrder: []string{"p1", "p2"},
			providers: map[string]LLMProvider{
				"p1": {Type: "openai"},
				"p2": {Type: "anthropic"},
			},
			expectedLen:   2,
			expectedTypes: []string{"openai", "anthropic"},
		},
		{
			name:          "some missing",
			failoverOrder: []string{"p1", "missing", "p2"},
			providers: map[string]LLMProvider{
				"p1": {Type: "openai"},
				"p2": {Type: "gemini"},
			},
			expectedLen:   2,
			expectedTypes: []string{"openai", "gemini"},
		},
		{
			name:          "all missing",
			failoverOrder: []string{"x", "y"},
			providers:     map[string]LLMProvider{"p1": {Type: "openai"}},
			expectedLen:   0,
			expectedTypes: nil,
		},
		{
			name:          "nil order",
			failoverOrder: nil,
			providers:     map[string]LLMProvider{"p1": {Type: "openai"}},
			expectedLen:   0,
			expectedTypes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{
				FailoverOrder: tt.failoverOrder,
				Providers:     tt.providers,
			}
			got := cfg.GetFailoverProviders()
			assert.Len(t, got, tt.expectedLen)
			if tt.expectedTypes == nil {
				assert.Nil(t, got)
			} else {
				for i, expectedType := range tt.expectedTypes {
					assert.Equal(t, expectedType, got[i].Type)
				}
			}
		})
	}
}

func TestDeepSeekPricingMatch(t *testing.T) {
	t.Parallel()
	pData := DefaultPricing()

	tests := []struct {
		model    string
		expected float64
	}{
		{"deepseek-reasoner", 0.028},
		{"deepseek-v3", 0.028},
		{"deepseek-ai/deepseek-v3", 0.028},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, found := findBestMatch(pData.Models, tt.model, func(p pricing.ModelPricing) bool {
				return p.Hit > 0
			})
			assert.True(t, found)
			assert.Equal(t, tt.expected, pricing.Hit)
		})
	}
}
