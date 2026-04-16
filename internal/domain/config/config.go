// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// Default limits for history and tools.
const (
	DefaultMaxToolTurns       = 200
	DefaultMaxHistoryTurns    = 0
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
	Type           string            `yaml:"type"`            // e.g., "openai", "anthropic", "gemini"
	URL            string            `yaml:"url"`             // Base API URL
	APIKey         string            `yaml:"api_key"`         // Secret key (supports ${VAR} expansion in next task)
	Model          string            `yaml:"model"`           // Default model for this provider
	ThinkingBudget int               `yaml:"thinking_budget"` // Provider-specific thinking budget
	ThinkingLevel  string            `yaml:"thinking_level"`  // Provider-specific thinking level
	Headers        map[string]string `yaml:"headers"`         // Custom HTTP headers
}

// Config represents the application configuration loaded from a YAML file.
type Config struct {
	Mode               string                 `yaml:"mode"`
	Person             string                 `yaml:"person"`
	URL                string                 `yaml:"aiurl"`
	Model              string                 `yaml:"aimodel"`
	UseSearch          bool                   `yaml:"use_search"`
	MaxToolTurns       int                    `yaml:"max_turns"`          // Recursion limit
	MaxHistoryTurns    int                    `yaml:"max_history_turns"`  // For pruning turns
	MaxHistoryTokens   int                    `yaml:"max_history_tokens"` // For safety rollback
	ThinkingBudget     int                    `yaml:"thinking_budget"`
	ThinkingLevel      string                 `yaml:"thinking_level"`
	ShowThoughts       bool                   `yaml:"show_thoughts"`
	ShowTools          bool                   `yaml:"show_tools"`
	MaxConcurrentTools int                    `yaml:"max_concurrent_tools"` // Parallel tool execution
	ToolTimeoutSeconds int                    `yaml:"tool_timeout"`         // Single tool timeout
	HTTPTimeoutSeconds int                    `yaml:"http_timeout"`         // LLM Client timeout
	UseTUIPrompt       bool                   `yaml:"use_tui_prompt"`       // Enable TUI prompt with suggestions
	Models             map[string]ModelConfig `yaml:"models"`               // Model-specific overrides
	SelectedProvider   string                 `yaml:"selected_provider"`
	Providers          map[string]LLMProvider `yaml:"providers"`
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
	MaxThinkingBudget int                  `yaml:"max_thinking_budget"`
	ContextWindow     int                  `yaml:"context_window"`
	Pricing           pricing.ModelPricing `yaml:"pricing"`
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
		UpdatedAt: "2026-02-03T12:00:00Z", // Sync with assets/pricing.json
		Models: map[string]pricing.ModelPricing{
			"default": {
				Hit:  0.0,
				Miss: 0.0,
				Comp: 0.0,
			},
			"gpt-5": {
				Hit:  0.175,
				Miss: 1.75,
				Comp: 14.00,
			},
			"deepseek-": {
				Hit:      0.028,
				Miss:     0.28,
				Comp:     0.42,
				Thinking: 0.42,
			},
			"claude-sonnet-4-6": {
				Hit:  0.30,
				Miss: 3.00,
				Comp: 15.00,
			},
			"claude-opus-4-6": {
				Hit:  0.50,
				Miss: 5.00,
				Comp: 25.00,
			},
			"gemini-3-flash-preview": {
				Hit:            0.05,
				Miss:           0.50,
				Comp:           3.00,
				ThinkingBudget: 32768,
				SearchQuery:    0.014, // Updated from 0.035
			},
			"gemini-3-pro-preview": {
				Hit:            0.20,  // Updated from 0.3125
				Miss:           2.00,  // Updated from 1.25
				Comp:           12.00, // Updated from 5.00
				ThinkingBudget: 65536,
				SearchQuery:    0.014, // Updated from 0.035
			},
		},
	}
}

// ConfigFinder defines the interface for locating the configuration file across different environments.
type ConfigFinder interface {
	Find() (string, error)
}
