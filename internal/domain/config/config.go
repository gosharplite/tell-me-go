// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// Default limits for history and tools.
const (
	DefaultMaxToolTurns       = 10
	DefaultMaxHistoryTurns    = 20
	DefaultMaxHistoryTokens   = 120000
	DefaultMaxConcurrentTools = 5
	DefaultToolTimeoutSeconds = 30
	DefaultTieredThreshold    = 0
	DefaultMaxLoopRepetitions = 5
	DefaultHTTPTimeoutSeconds = 300  // 5 minutes
	WarningRatio              = 0.78 // ~100k for 128k cliff
	SystemContextBuffer       = 1000 // Reserved space for system warnings/instructions
)

// LLMProvider represents the configuration for a specific AI service provider.
type LLMProvider struct {
	Type           string            `yaml:"TYPE"`            // e.g., "openai", "anthropic", "gemini"
	URL            string            `yaml:"URL"`             // Base API URL
	APIKey         string            `yaml:"API_KEY"`         // Secret key (supports ${VAR} expansion in next task)
	Model          string            `yaml:"MODEL"`           // Default model for this provider
	ThinkingBudget int               `yaml:"THINKING_BUDGET"` // Provider-specific thinking budget
	ThinkingLevel  string            `yaml:"THINKING_LEVEL"`  // Provider-specific thinking level
	Headers        map[string]string `yaml:"HEADERS"`         // Custom HTTP headers
}

// Config represents the application configuration loaded from a YAML file.
type Config struct {
	Mode               string                 `yaml:"MODE"`
	Person             string                 `yaml:"PERSON"`
	URL                string                 `yaml:"AIURL"`
	Model              string                 `yaml:"AIMODEL"`
	UseSearch          bool                   `yaml:"USE_SEARCH"`
	MaxToolTurns       int                    `yaml:"MAX_TURNS"`          // Recursion limit
	MaxHistoryTurns    int                    `yaml:"MAX_HISTORY_TURNS"`  // For pruning turns
	MaxHistoryTokens   int                    `yaml:"MAX_HISTORY_TOKENS"` // For safety rollback
	ThinkingBudget     int                    `yaml:"THINKING_BUDGET"`
	ThinkingLevel      string                 `yaml:"THINKING_LEVEL"`
	ShowThoughts       bool                   `yaml:"SHOW_THOUGHTS"`
	ShowTools          bool                   `yaml:"SHOW_TOOLS"`
	MaxConcurrentTools int                    `yaml:"MAX_CONCURRENT_TOOLS"` // Parallel tool execution
	ToolTimeoutSeconds int                    `yaml:"TOOL_TIMEOUT"`         // Single tool timeout
	HTTPTimeoutSeconds int                    `yaml:"HTTP_TIMEOUT"`         // LLM Client timeout
	DisableStreaming   bool                   `yaml:"DISABLE_STREAMING"`
	Models             map[string]ModelConfig `yaml:"MODELS"` // Model-specific overrides
	SelectedProvider   string                 `yaml:"SELECTED_PROVIDER"`
	Providers          map[string]LLMProvider `yaml:"PROVIDERS"`
}

// GetActiveProvider returns the configuration for the selected provider.
// If SELECTED_PROVIDER is empty or not found, it should synthesize a provider
// using the legacy top-level fields (URL, Model, etc.) for backward compatibility.
func (c *Config) GetActiveProvider() LLMProvider {
	if p, ok := c.Providers[c.SelectedProvider]; ok {
		return p
	}
	// Fallback to legacy flat config
	return LLMProvider{
		Type:           "gemini", // Default legacy type
		URL:            c.URL,
		Model:          c.Model,
		ThinkingBudget: c.ThinkingBudget,
		ThinkingLevel:  c.ThinkingLevel,
	}
}

// ModelConfig defines capabilities and limits for a specific model.
type ModelConfig struct {
	MaxThinkingBudget int                  `yaml:"MAX_THINKING_BUDGET"`
	ContextWindow     int                  `yaml:"CONTEXT_WINDOW"`
	Pricing           pricing.ModelPricing `yaml:"PRICING"`
}

// ResolveThinkingBudget returns the best matching thinking budget for the model.
func (c *Config) ResolveThinkingBudget(model string, pricingData pricing.PricingData) int {
	// 1. Try Config overrides
	if mCfg, ok := findBestMatch(c.Models, model, func(m ModelConfig) bool {
		return m.MaxThinkingBudget > 0
	}); ok {
		return mCfg.MaxThinkingBudget
	}

	// 2. Try Pricing defaults (encapsulated in ModelPricing)
	return pricingData.GetModelPricing(model).ThinkingBudget
}

// ResolveContextWindow returns the appropriate context window limit.
func (c *Config) ResolveContextWindow() int {
	maxTokens := c.MaxHistoryTokens
	if mCfg, ok := findBestMatch(c.Models, c.Model, func(m ModelConfig) bool {
		return m.ContextWindow > 0
	}); ok {
		if maxTokens > mCfg.ContextWindow {
			return mCfg.ContextWindow
		}
		return maxTokens
	}
	return maxTokens
}

// ResolveTieredThreshold returns the tiered cost threshold for the model.
func (c *Config) ResolveTieredThreshold(pData pricing.PricingData) int {
	if mPricing, ok := findBestMatch(pData.Models, c.Model, func(p pricing.ModelPricing) bool {
		return p.TieredThreshold > 0
	}); ok {
		return int(mPricing.TieredThreshold)
	}
	return DefaultTieredThreshold
}

// findBestMatch encapsulates the priority matching logic: exact match first, then substring match.
func findBestMatch[T any](m map[string]T, key string, isValid func(T) bool) (T, bool) {
	if val, ok := m[key]; ok && isValid(val) {
		return val, true
	}

	var bestV T
	var found bool
	var maxLen int

	for k, v := range m {
		if k != "default" && strings.Contains(key, k) && isValid(v) {
			if !found || len(k) > maxLen {
				maxLen = len(k)
				bestV = v
				found = true
			}
		}
	}

	if found {
		return bestV, true
	}

	var zero T
	return zero, false
}

// DefaultPricing returns the hardcoded fallback pricing data.
func DefaultPricing() pricing.PricingData {
	return pricing.PricingData{
		UpdatedAt: "Hardcoded Fallback",
		Models: map[string]pricing.ModelPricing{
			"gemini-3-flash-preview": {
				Hit:             0.05,
				Miss:            0.50,
				Comp:            3.00,
				TieredThreshold: 0,
				ThinkingBudget:  32768,
			},
			"gemini-3-pro-preview": {
				Hit:             0.3125,
				Miss:            1.25,
				Comp:            5.00,
				TieredThreshold: 0,
				ThinkingBudget:  65536,
			},
			"flash": {
				Hit:             0.025,
				Miss:            0.10,
				Comp:            0.40,
				TieredThreshold: 0,
				TieredMiss:      0.20,
				TieredComp:      0.80,
				ThinkingBudget:  0,
			},
			"pro": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 0,
				TieredMiss:      2.50,
				TieredComp:      15.00,
				ThinkingBudget:  0,
			},
			"default": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 0,
				TieredMiss:      2.50,
				TieredComp:      15.00,
				ThinkingBudget:  0,
			},
		},
		SearchQuery: 0.035,
	}
}
