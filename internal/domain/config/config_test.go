// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestResolveContextWindowFromPricing(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Model:            "gemini-3-flash-preview",
		MaxHistoryTokens: 120000,
	}

	pd := pricing.PricingData{
		Models: map[string]pricing.ModelPricing{
			"gemini-3-flash-preview": {
				ContextWindow: 1048576,
			},
		},
	}

	got := cfg.resolveContextWindowFromPricing(pd)
	if got != 1048576 {
		t.Errorf("expected 1048576 from pricing, got %d", got)
	}
}

func TestResolveContextWindowFromPricing_ConfigOverride(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Model:            "gemini-3-flash-preview",
		MaxHistoryTokens: 120000,
		Models: map[string]ModelConfig{
			"gemini-3-flash-preview": {ContextWindow: 50000},
		},
	}

	pd := pricing.PricingData{
		Models: map[string]pricing.ModelPricing{
			"gemini-3-flash-preview": {
				ContextWindow: 1048576,
			},
		},
	}

	got := cfg.resolveContextWindowFromPricing(pd)
	if got != 50000 {
		t.Errorf("expected 50000 from Config override, got %d", got)
	}
}

func TestResolveContextWindowFromPricing_Fallback(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Model:            "unknown-model",
		MaxHistoryTokens: 120000,
	}

	pd := pricing.PricingData{
		Models: map[string]pricing.ModelPricing{
			"default": {ContextWindow: 0},
		},
	}

	got := cfg.resolveContextWindowFromPricing(pd)
	if got != 120000 {
		t.Errorf("expected fallback 120000, got %d", got)
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
		{
			name:     "Empty config (no legacy fields, no providers)",
			config:   Config{},
			expected: LLMProvider{Type: "gemini"},
		},
		{
			name: "Selected provider not found with empty legacy fields",
			config: Config{
				SelectedProvider: "missing",
			},
			expected: LLMProvider{Type: "gemini"},
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

// TestLLMProvider_Validate_EdgeCases pins the boundary values around the
// Anthropic thinking-budget warning condition. The threshold is
// ThinkingBudget + anthropicThinkingBudgetHeadroom (1024).
// For ThinkingBudget=500, the threshold is 1524:
//
//	MaxTokens < 1524  → warning (runtime will silently bump)
//	MaxTokens >= 1524 → no warning
//
// Edge cases include zero MaxTokens, zero ThinkingBudget, non-Anthropic
// providers, and the hard-rejection of negative MaxTokens.
func TestLLMProvider_Validate_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		maxTokens      int
		thinkingBudget int
		providerType   string
		expectError    bool
		expectWarning  bool
	}{
		{
			name:           "max_tokens_below_threshold_triggers_warning",
			maxTokens:      1000,
			thinkingBudget: 500,
			providerType:   "anthropic",
			expectError:    false,
			expectWarning:  true,
		},
		{
			name:           "max_tokens_exactly_at_threshold_no_warning",
			maxTokens:      1524,
			thinkingBudget: 500,
			providerType:   "anthropic",
			expectError:    false,
			expectWarning:  false,
		},
		{
			name:           "max_tokens_above_threshold_no_warning",
			maxTokens:      2000,
			thinkingBudget: 500,
			providerType:   "anthropic",
			expectError:    false,
			expectWarning:  false,
		},
		{
			name:           "max_tokens_zero_with_positive_budget_no_warning",
			maxTokens:      0,
			thinkingBudget: 500,
			providerType:   "anthropic",
			expectError:    false,
			expectWarning:  false,
		},
		{
			name:           "negative_max_tokens_rejected",
			maxTokens:      -1,
			thinkingBudget: 500,
			providerType:   "anthropic",
			expectError:    true,
			expectWarning:  false,
		},
		{
			name:           "non_anthropic_provider_no_warning",
			maxTokens:      500,
			thinkingBudget: 1000,
			providerType:   "openai",
			expectError:    false,
			expectWarning:  false,
		},
		{
			name:           "zero_thinking_budget_no_warning",
			maxTokens:      1000,
			thinkingBudget: 0,
			providerType:   "anthropic",
			expectError:    false,
			expectWarning:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger, buf := newWarnBuffer()
			p := &LLMProvider{
				Type:           tt.providerType,
				MaxTokens:      tt.maxTokens,
				ThinkingBudget: tt.thinkingBudget,
			}

			err := p.validate("test-provider", logger)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			logged := buf.String()
			if tt.expectWarning {
				assert.Contains(t, logged, "provider_max_tokens_below_thinking_budget_floor")
			} else {
				assert.NotContains(t, logged, "provider_max_tokens_below_thinking_budget_floor")
			}
		})
	}
}

// TestLLMProvider_Validate_UserID pins the startup validation contract
// for PROVIDERS.<name>.USER_ID. Valid values pass; too-long, bad
// characters, and unicode are hard-rejected with actionable messages.
func TestLLMProvider_Validate_UserID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		{"empty allowed", "", false},
		{"valid simple", "tenant-42", false},
		{"valid with dash and underscore", "tenant_a-b", false},
		{"exactly 512 chars accepted", strings.Repeat("a", 512), false},
		{"513 chars rejected", strings.Repeat("a", 513), true},
		{"space rejected", "tenant 42", true},
		{"special char at rejected", "tenant@42", true},
		{"unicode rejected", "tenant-\u2122", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &LLMProvider{UserID: tt.userID}
			err := p.validate("test-provider", slog.New(slog.NewTextHandler(io.Discard, nil)))
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfig_ValidateBounds pins the contract for Config.ValidateBounds(): every
// non-negative int field is validated, zero is always accepted, and the function
// short-circuits on the first invalid field found. Viper's WeaklyTypedInput can
// silently produce negative values from integer overflow, so this guard is
// critical for startup-time diagnosis.
func TestConfig_ValidateSelectedProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		selectedProvider string
		providers        map[string]LLMProvider
		expectError      bool
		errorContains    string
	}{
		{
			name:             "empty string is valid (legacy mode)",
			selectedProvider: "",
			providers:        map[string]LLMProvider{"gemini": {Type: "gemini"}},
			expectError:      false,
		},
		{
			name:             "valid key",
			selectedProvider: "gemini",
			providers:        map[string]LLMProvider{"gemini": {Type: "gemini"}},
			expectError:      false,
		},
		{
			name:             "missing key",
			selectedProvider: "nonexistent",
			providers:        map[string]LLMProvider{"gemini": {Type: "gemini"}},
			expectError:      true,
			errorContains:    "nonexistent",
		},
		{
			name:             "missing key with empty providers",
			selectedProvider: "any",
			providers:        map[string]LLMProvider{},
			expectError:      true,
			errorContains:    "any",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{
				SelectedProvider: tt.selectedProvider,
				Providers:        tt.providers,
			}
			err := cfg.validateSelectedProvider()
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Contains(t, err.Error(), "SELECTED_PROVIDER")
				assert.Contains(t, err.Error(), "PROVIDERS")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_providerKeys(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: map[string]LLMProvider{
			"zzz": {Type: "gemini"},
			"aaa": {Type: "openai"},
			"mmm": {Type: "anthropic"},
		},
	}
	keys := cfg.providerKeys()
	assert.Equal(t, []string{"aaa", "mmm", "zzz"}, keys, "providerKeys must return sorted keys")
}

func TestConfig_providerKeys_Empty(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	keys := cfg.providerKeys()
	assert.Empty(t, keys)
}

func TestConfig_ValidateProviderUniqueness(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Providers: map[string]LLMProvider{
			"gemini": {Type: "gemini"},
			"openai": {Type: "openai"},
		},
	}
	if err := cfg.validateProviderUniqueness(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestConfig_ValidateProviders_SelectedProviderFirst verifies that
// validateSelectedProvider runs before per-provider checks — the operator
// sees the config-level error before individual provider errors.
func TestConfig_ValidateProviders_SelectedProviderFirst(t *testing.T) {
	t.Parallel()
	logger, _ := newWarnBuffer()
	cfg := &Config{
		SelectedProvider: "nonexistent",
		Providers: map[string]LLMProvider{
			"gemini": {Type: "gemini", MaxTokens: -99}, // would also fail per-provider check
		},
	}
	err := cfg.ValidateProviders(logger)
	require.Error(t, err)
	// Must contain the SelectedProvider error, NOT the per-provider MaxTokens error
	assert.Contains(t, err.Error(), "SELECTED_PROVIDER")
	assert.Contains(t, err.Error(), "nonexistent")
	assert.NotContains(t, err.Error(), "MAX_TOKENS",
		"SelectedProvider error must surface before per-provider MaxTokens error")
}

// TestConfig_ValidateBounds pins the contract for Config.ValidateBounds(): every
// non-negative int field is validated, zero is always accepted, and the function
// short-circuits on the first invalid field found. Viper's WeaklyTypedInput can
// silently produce negative values from integer overflow, so this guard is
// critical for startup-time diagnosis.
func TestConfig_ValidateBounds(t *testing.T) {

	tests := []struct {
		name          string
		config        Config
		expectError   bool
		errorContains []string // substrings expected in the error message
	}{
		{
			name:        "all valid (defaults / zero value)",
			config:      Config{},
			expectError: false,
		},
		{
			name:          "MAX_TURNS negative",
			config:        Config{MaxToolTurns: -1},
			expectError:   true,
			errorContains: []string{"MAX_TURNS", "-1"},
		},
		{
			name:          "MAX_HISTORY_TURNS negative",
			config:        Config{MaxHistoryTurns: -1},
			expectError:   true,
			errorContains: []string{"MAX_HISTORY_TURNS", "-1"},
		},
		{
			name:          "MAX_HISTORY_TOKENS negative",
			config:        Config{MaxHistoryTokens: -1},
			expectError:   true,
			errorContains: []string{"MAX_HISTORY_TOKENS", "-1"},
		},
		{
			name:          "THINKING_BUDGET negative",
			config:        Config{ThinkingBudget: -1},
			expectError:   true,
			errorContains: []string{"THINKING_BUDGET", "-1"},
		},
		{
			name:          "MAX_CONCURRENT_TOOLS negative",
			config:        Config{MaxConcurrentTools: -1},
			expectError:   true,
			errorContains: []string{"MAX_CONCURRENT_TOOLS", "-1"},
		},
		{
			name:          "TOOL_TIMEOUT negative",
			config:        Config{ToolTimeoutSeconds: -1},
			expectError:   true,
			errorContains: []string{"TOOL_TIMEOUT", "-1"},
		},
		{
			name:          "HTTP_TIMEOUT negative",
			config:        Config{HTTPTimeoutSeconds: -1},
			expectError:   true,
			errorContains: []string{"HTTP_TIMEOUT", "-1"},
		},
		{
			name: "zero values all valid (explicit)",
			config: Config{
				MaxToolTurns:       0,
				MaxHistoryTurns:    0,
				MaxHistoryTokens:   0,
				ThinkingBudget:     0,
				MaxConcurrentTools: 0,
				ToolTimeoutSeconds: 0,
				HTTPTimeoutSeconds: 0,
			},
			expectError: false,
		},
		{
			name: "first of multiple negatives short-circuits on MAX_TURNS",
			config: Config{
				MaxToolTurns:    -5,
				MaxHistoryTurns: -3, // should not be reached
			},
			expectError:   true,
			errorContains: []string{"MAX_TURNS", "-5"},
		},
		{
			name:          "large overflow value",
			config:        Config{MaxToolTurns: -1593095861524629081},
			expectError:   true,
			errorContains: []string{"MAX_TURNS", "-1593095861524629081"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.ValidateBounds()
			if tt.expectError {
				require.Error(t, err)
				for _, sub := range tt.errorContains {
					assert.Contains(t, err.Error(), sub)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLLMProvider_Family(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  string
		want APIFamily
	}{
		// OpenAI-compatible labels
		{"openai", "openai", APIOpenAI},
		{"deepseek", "deepseek", APIOpenAI},
		{"kimi", "kimi", APIOpenAI},
		// Anthropic
		{"anthropic", "anthropic", APIAnthropic},
		// Gemini family
		{"gemini", "gemini", APIGemini},
		{"google", "google", APIGemini},
		// Edge cases
		{"empty string falls to Gemini", "", APIGemini},
		{"unknown type falls to Gemini", "bogus", APIGemini},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := LLMProvider{Type: tt.typ}
			got := p.Family()
			if got != tt.want {
				t.Errorf("LLMProvider{Type: %q}.Family() = %q; want %q",
					tt.typ, got, tt.want)
			}
		})
	}
}
