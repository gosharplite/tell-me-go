// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"log/slog"
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
	DefaultMaxLoopRepetitions = 5
	DefaultHTTPTimeoutSeconds = 300  // 5 minutes
	WarningRatio              = 0.78 // Ratio to trigger safety warnings before hard limits
	SystemContextBuffer       = 1000 // Reserved space for system warnings/instructions
)

// LLMProvider represents the configuration for a specific AI service provider.
type LLMProvider struct {
	Type           string `yaml:"TYPE"`            // e.g., "openai", "anthropic", "gemini"
	URL            string `yaml:"URL"`             // Base API URL
	APIKey         string `yaml:"API_KEY"`         // Secret key (supports ${VAR} expansion in next task)
	Model          string `yaml:"MODEL"`           // Default model for this provider
	ThinkingBudget int    `yaml:"THINKING_BUDGET"` // Provider-specific thinking budget
	ThinkingLevel  string `yaml:"THINKING_LEVEL"`  // Provider-specific thinking level
	// MaxTokens is the per-request output-token cap. Optional; zero means
	// "use the provider's package default". Read at startup only — changes
	// to this field require a process restart to take effect.
	MaxTokens int               `yaml:"MAX_TOKENS"`
	Headers   map[string]string `yaml:"HEADERS"` // Custom HTTP headers
}

// anthropicThinkingBudgetHeadroom mirrors the Anthropic client's
// runtime invariant: when ThinkingBudget > 0, the request's max_tokens
// must exceed thinking_budget + 1024, otherwise the client silently
// bumps it. Defining the constant here lets validation surface the
// silent bump as a warning without coupling the domain to the
// infrastructure constant.
const anthropicThinkingBudgetHeadroom = 1024

// Validate reports semantic errors and emits warnings for surprising-
// but-tolerated values on a single LLMProvider. The logger is used for
// warn-level diagnostics; it must be non-nil. Returns a non-nil error
// only for hard rejections.
//
// Hard rejections:
//   - MaxTokens < 0 — the API would reject this anyway; catch it at
//     startup with a clear message naming the provider and value.
//
// Warnings (non-fatal):
//   - Anthropic providers where MaxTokens > 0 && ThinkingBudget > 0
//     && MaxTokens < ThinkingBudget + anthropicThinkingBudgetHeadroom:
//     the Anthropic runtime will silently bump max_tokens at request
//     time, overriding the configured cap. Surfacing as a warning
//     gives operators visibility into the silent override.
func (p *LLMProvider) validate(name string, logger *slog.Logger) error {
	if p.MaxTokens < 0 {
		return fmt.Errorf("PROVIDERS.%s.MAX_TOKENS must be >= 0, got %d", name, p.MaxTokens)
	}
	if p.Type == "anthropic" && p.MaxTokens > 0 && p.ThinkingBudget > 0 &&
		p.MaxTokens < p.ThinkingBudget+anthropicThinkingBudgetHeadroom {
		logger.Warn("provider_max_tokens_below_thinking_budget_floor",
			"provider", name,
			"max_tokens", p.MaxTokens,
			"thinking_budget", p.ThinkingBudget,
			"note", "Anthropic runtime will silently bump max_tokens to thinking_budget+1024 on each request")
	}
	return nil
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
	UseTUIPrompt       bool                   `yaml:"USE_TUI_PROMPT"`       // Enable TUI prompt with suggestions
	BypassConfirmation bool                   `yaml:"BYPASS_CONFIRMATION"`
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

// ValidateProviders runs validation against every provider entry in
// Providers and returns the first error encountered. Warnings are
// emitted via the supplied logger (non-fatal). The logger must be
// non-nil; callers without a configured logger should pass a logger
// backed by a discard handler.
//
// The order of iteration is undefined (Go map iteration); operators
// should treat the first-error semantics as "any one of multiple
// invalid providers will be reported" rather than depending on which
// one surfaces first.
func (c *Config) ValidateProviders(logger *slog.Logger) error {
	for name, p := range c.Providers {
		provider := p // copy to avoid taking the address of the range variable
		if err := provider.validate(name, logger); err != nil {
			return err
		}
	}
	return nil
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

// updateBestMatch checks whether candidate key k is a valid match for target
// and, if so, replaces bestV/maxLen with it when it's longer than the current best.
func updateBestMatch[T any](k, target string, v T, isValid func(T) bool, bestV *T, found *bool, maxLen *int) {
	if k != "default" && strings.Contains(target, k) && isValid(v) {
		if !*found || len(k) > *maxLen {
			*maxLen = len(k)
			*bestV = v
			*found = true
		}
	}
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
		updateBestMatch(k, key, v, isValid, &bestV, &found, &maxLen)
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
				Hit:  0.028,
				Miss: 0.28,
				Comp: 0.42,
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
