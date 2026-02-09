// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"gopkg.in/yaml.v3"
)

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
	DisableStreaming   bool                   `yaml:"DISABLE_STREAMING"`
	Models             map[string]ModelConfig `yaml:"MODELS"` // Model-specific overrides
}

// ModelConfig defines capabilities and limits for a specific model.
type ModelConfig struct {
	MaxThinkingBudget int                  `yaml:"MAX_THINKING_BUDGET"`
	ContextWindow     int                  `yaml:"CONTEXT_WINDOW"`
	Pricing           pricing.ModelPricing `yaml:"PRICING"`
}

// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	// Set defaults
	cfg.MaxToolTurns = DefaultMaxToolTurns
	cfg.MaxHistoryTurns = DefaultMaxHistoryTurns
	cfg.MaxHistoryTokens = DefaultMaxHistoryTokens
	cfg.MaxConcurrentTools = DefaultMaxConcurrentTools
	cfg.ToolTimeoutSeconds = DefaultToolTimeoutSeconds
	cfg.ShowThoughts = true
	cfg.ShowTools = true

	if os.Getenv("TELL_ME_NO_STREAM") == "true" {
		cfg.DisableStreaming = true
	}

	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode yaml: %w", err)
	}

	// Environment overrides
	if val := os.Getenv("GOSHARP_MODE"); val != "" {
		cfg.Mode = val
	}
	if val := os.Getenv("GOSHARP_PERSON"); val != "" {
		cfg.Person = val
	}
	if val := os.Getenv("GOSHARP_AIMODEL"); val != "" {
		cfg.Model = val
	}
	if val := os.Getenv("GOSHARP_AIURL"); val != "" {
		cfg.URL = val
	}

	return &cfg, nil
}

// ResolveThinkingBudget returns the best matching thinking budget for the model.
func (c *Config) ResolveThinkingBudget(model string, pricingData pricing.PricingData) int {
	// 1. Try Config overrides
	if mCfg, ok := findBestMatch(c.Models, model, func(m ModelConfig) bool {
		return m.MaxThinkingBudget > 0
	}); ok {
		return mCfg.MaxThinkingBudget
	}

	// 2. Try Pricing defaults
	if budget, ok := findBestMatch(pricingData.ThinkingBudgets, model, func(int) bool {
		return true
	}); ok {
		return budget
	}

	// 3. Ultimate fallback
	return pricingData.ThinkingBudgets["default"]
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
