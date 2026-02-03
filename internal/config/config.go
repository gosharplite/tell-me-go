// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/pricing"
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

	return &cfg, nil
}

// ResolveThinkingBudget returns the best matching thinking budget for the model.
func (c *Config) ResolveThinkingBudget(model string, pricingData pricing.PricingData) int {
	// 1. Check for exact model match in config
	if mCfg, ok := c.Models[model]; ok && mCfg.MaxThinkingBudget > 0 {
		return mCfg.MaxThinkingBudget
	}
	// 2. Check for substring matches in config
	for k, v := range c.Models {
		if k != "default" && strings.Contains(model, k) && v.MaxThinkingBudget > 0 {
			return v.MaxThinkingBudget
		}
	}
	// 3. Fallback to Pricing data
	if val, ok := pricingData.ThinkingBudgets[model]; ok {
		return val
	}
	for k, v := range pricingData.ThinkingBudgets {
		if k != "default" && strings.Contains(model, k) {
			return v
		}
	}
	// 4. Ultimate fallback
	return pricingData.ThinkingBudgets["default"]
}
