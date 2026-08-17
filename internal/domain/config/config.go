// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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

// userIDRegex validates the DeepSeek user_id format.
// Must match [a-zA-Z0-9\-_]+ per DeepSeek API spec.
var userIDRegex = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// APIFamily identifies the wire-protocol family of an LLM provider.
// It is the compile-time-safe representation of how to communicate
// with a provider, as opposed to LLMProvider.Type which is the
// user-facing label string (e.g., "kimi", "deepseek").
//
// There are exactly three API families. The set is intended to be
// exhaustive — adding a fourth should be a deliberate, grep-able change
// at every switch site. Enable the 'exhaustive' linter for this type
// to make it a compile-time error.
type APIFamily string

const (
	APIOpenAI    APIFamily = "openai"    // OpenAI-compatible: openai, deepseek, kimi
	APIAnthropic APIFamily = "anthropic" // Anthropic Messages API
	APIGemini    APIFamily = "gemini"    // Google Gemini / Vertex AI
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

	// ThinkingEnabled controls the DeepSeek/Kimi thinking-mode toggle.
	// Tri-state: nil = omit the field from the wire (preserve provider
	// default); true = thinking enabled; false = thinking disabled.
	// Only emitted for providers with SupportsThinkingToggle capability.
	ThinkingEnabled *bool `yaml:"THINKING_ENABLED"`

	// UserID is the DeepSeek user_id for content safety, KVCache, and
	// scheduling isolation. Must match [a-zA-Z0-9\-_]+ with max length
	// 512. Do not include PII. Emitted only when non-empty for providers
	// with SupportsThinkingToggle capability.
	UserID string `yaml:"USER_ID"`
}

// Family returns the wire-protocol family for this provider,
// derived from its Type label. Unknown types default to APIGemini
// for backward compatibility with the existing factory switch.
func (p *LLMProvider) Family() APIFamily {
	switch p.Type {
	case "openai", "deepseek", "kimi":
		return APIOpenAI
	case "anthropic":
		return APIAnthropic
	case "google", "gemini", "":
		return APIGemini
	default:
		return APIGemini
	}
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

	// user_id validation: enforce format + length constraint at startup
	// rather than letting invalid values surface as runtime HTTP 400s.
	if p.UserID != "" {
		if len(p.UserID) > 512 {
			return fmt.Errorf("PROVIDERS.%s.USER_ID must be <= 512 characters, got %d", name, len(p.UserID))
		}
		if !userIDRegex.MatchString(p.UserID) {
			return fmt.Errorf("PROVIDERS.%s.USER_ID must match [a-zA-Z0-9\\-_]+, got %q", name, p.UserID)
		}
	}

	if p.Family() == APIAnthropic && p.MaxTokens > 0 && p.ThinkingBudget > 0 &&
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
	Mode               string `yaml:"MODE"`
	Person             string `yaml:"PERSON"`
	URL                string `yaml:"AIURL"`
	Model              string `yaml:"AIMODEL"`
	UseSearch          bool   `yaml:"USE_SEARCH"`
	MaxToolTurns       int    `yaml:"MAX_TURNS"`          // Recursion limit
	MaxHistoryTurns    int    `yaml:"MAX_HISTORY_TURNS"`  // For pruning turns
	MaxHistoryTokens   int    `yaml:"MAX_HISTORY_TOKENS"` // For safety rollback
	ThinkingBudget     int    `yaml:"THINKING_BUDGET"`
	ThinkingLevel      string `yaml:"THINKING_LEVEL"`
	ShowThoughts       bool   `yaml:"SHOW_THOUGHTS"`
	ShowTools          bool   `yaml:"SHOW_TOOLS"`
	MaxConcurrentTools int    `yaml:"MAX_CONCURRENT_TOOLS"` // Parallel tool execution
	ToolTimeoutSeconds int    `yaml:"TOOL_TIMEOUT"`         // Single tool timeout
	HTTPTimeoutSeconds int    `yaml:"HTTP_TIMEOUT"`         // LLM Client timeout
	UseTUIPrompt       bool   `yaml:"USE_TUI_PROMPT"`       // Enable TUI prompt with suggestions
	// WrapWidth is the column width for word-wrapping non-TUI markdown
	// output. Zero means "use glamour's built-in default (80)". Read at
	// session start only — changes take effect from the next session.
	WrapWidth          int                        `yaml:"WRAP_WIDTH"`
	BypassConfirmation bool                       `yaml:"BYPASS_CONFIRMATION"`
	Models             map[string]ModelConfig     `yaml:"MODELS"` // Model-specific overrides
	SelectedProvider   string                     `yaml:"SELECTED_PROVIDER"`
	Providers          map[string]LLMProvider     `yaml:"PROVIDERS"`
	MCPServers         map[string]MCPServerConfig `yaml:"MCP_SERVERS"`
	FailoverOrder      []string                   `yaml:"FAILOVER_ORDER"`
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

// validateSelectedProvider ensures SelectedProvider (when set) references
// a key that exists in the Providers registry. An empty SelectedProvider
// is valid — it means "use legacy flat config."
func (c *Config) validateSelectedProvider() error {
	if c.SelectedProvider == "" {
		return nil
	}
	if _, ok := c.Providers[c.SelectedProvider]; !ok {
		return fmt.Errorf("SELECTED_PROVIDER %q is not a key in PROVIDERS (available: %s)",
			c.SelectedProvider, strings.Join(c.providerKeys(), ", "))
	}
	return nil
}

// providerKeys returns a sorted list of provider names for error messages.
func (c *Config) providerKeys() []string {
	keys := make([]string, 0, len(c.Providers))
	for k := range c.Providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateProviderUniqueness is a named validation anchor for the
// provider-unique-name invariant: each Provider in the registry has
// a unique name.
//
// The invariant is structurally enforced by Go's map semantics — a
// map[string]LLMProvider cannot contain duplicate keys. This method
// exists so that future code which assembles the provider registry
// from multiple sources (e.g., merging config files) has an obvious
// place to add explicit duplicate detection.
//
// In the current single-file architecture, this always returns nil.
func (c *Config) validateProviderUniqueness() error {
	// Structurally enforced by map[string]LLMProvider.
	// If providers are ever assembled from multiple files, add a
	// duplicate-key check here before the merge.
	return nil
}

// ValidateProviders runs validation against every provider entry in
// Providers and returns the first error encountered. Warnings are
// emitted via the supplied logger (non-fatal). The logger must be
// non-nil; callers without a configured logger should pass a logger
// backed by a discard handler.
//
// SelectedProvider is validated first, then provider uniqueness —
// before per-provider checks — so the operator sees config-level
// misconfiguration before any individual provider errors.
//
// The order of per-provider iteration is undefined (Go map iteration);
// operators should treat the first-error semantics as "any one of
// multiple invalid providers will be reported" rather than depending
// on which one surfaces first.
func (c *Config) ValidateProviders(logger *slog.Logger) error {
	if err := c.validateSelectedProvider(); err != nil {
		return err
	}
	if err := c.validateProviderUniqueness(); err != nil {
		return err
	}
	for name, p := range c.Providers {
		provider := p // copy to avoid taking the address of the range variable
		if err := provider.validate(name, logger); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMCPServers validates every MCP server entry in MCPServers and
// returns the first error encountered. Server keys must match
// mcpServerKeyRegex (lowercase alphanumeric and hyphens); each server's
// URL and Timeout are then validated via MCPServerConfig.validate.
//
// The order of per-server iteration is undefined (Go map iteration);
// operators should treat first-error semantics as "any one of multiple
// invalid servers will be reported".
func (c *Config) ValidateMCPServers() error {
	for name, server := range c.MCPServers {
		if !mcpServerKeyRegex.MatchString(name) {
			return fmt.Errorf("MCP_SERVERS key %q is invalid: must match ^[a-z0-9-]+$ (lowercase alphanumeric and hyphens)", name)
		}
		if err := server.validate(name); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBounds checks every non-negative int field on Config and returns
// an error for any negative value. Viper's WeaklyTypedInput can silently
// produce negative values from integer overflow, so this is the guard.
// Zero is valid for all fields (e.g. MaxHistoryTurns=0 means no pruning,
// ThinkingBudget=0 means use the provider default).
func (c *Config) ValidateBounds() error {
	if c.MaxToolTurns < 0 {
		return fmt.Errorf("MAX_TURNS must be >= 0, got %d", c.MaxToolTurns)
	}
	if c.MaxHistoryTurns < 0 {
		return fmt.Errorf("MAX_HISTORY_TURNS must be >= 0, got %d", c.MaxHistoryTurns)
	}
	if c.MaxHistoryTokens < 0 {
		return fmt.Errorf("MAX_HISTORY_TOKENS must be >= 0, got %d", c.MaxHistoryTokens)
	}
	if c.ThinkingBudget < 0 {
		return fmt.Errorf("THINKING_BUDGET must be >= 0, got %d", c.ThinkingBudget)
	}
	if c.MaxConcurrentTools < 0 {
		return fmt.Errorf("MAX_CONCURRENT_TOOLS must be >= 0, got %d", c.MaxConcurrentTools)
	}
	if c.ToolTimeoutSeconds < 0 {
		return fmt.Errorf("TOOL_TIMEOUT must be >= 0, got %d", c.ToolTimeoutSeconds)
	}
	if c.HTTPTimeoutSeconds < 0 {
		return fmt.Errorf("HTTP_TIMEOUT must be >= 0, got %d", c.HTTPTimeoutSeconds)
	}
	if c.WrapWidth < 0 {
		return fmt.Errorf("WRAP_WIDTH must be >= 0, got %d", c.WrapWidth)
	}
	return nil
}

// ModelConfig defines capabilities and limits for a specific model.
type ModelConfig struct {
	MaxThinkingBudget int                  `yaml:"MAX_THINKING_BUDGET"`
	ContextWindow     int                  `yaml:"CONTEXT_WINDOW"`
	Pricing           pricing.ModelPricing `yaml:"PRICING"`
}

// GetFailoverProviders returns the LLMProvider values from c.Providers in the
// order specified by c.FailoverOrder. Providers named in FailoverOrder that
// are not present in the Providers map are silently skipped. Returns nil when
// FailoverOrder is empty or nil.
func (c *Config) GetFailoverProviders() []LLMProvider {
	if len(c.FailoverOrder) == 0 {
		return nil
	}
	result := make([]LLMProvider, 0, len(c.FailoverOrder))
	for _, name := range c.FailoverOrder {
		if p, ok := c.Providers[name]; ok {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
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

// resolveContextWindowFromPricing returns the context window for the active
// model, checking Config.Models first, then falling back to the PricingData
// table. If neither specifies a context window, returns MaxHistoryTokens.
func (c *Config) resolveContextWindowFromPricing(pd pricing.PricingData) int {
	// 1. Config.Models override (highest priority)
	if mCfg, ok := findBestMatch(c.Models, c.Model, func(m ModelConfig) bool {
		return m.ContextWindow > 0
	}); ok {
		return mCfg.ContextWindow
	}

	// 2. Pricing data (new unified source)
	mp := pd.GetModelPricing(c.Model)
	if mp.ContextWindow > 0 {
		return mp.ContextWindow
	}

	// 3. Fallback to config-level limit
	return c.MaxHistoryTokens
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
		UpdatedAt: "2026-07-18T12:00:00Z",
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
			"kimi-": {
				Hit:  0.3,
				Miss: 3.0,
				Comp: 15.0,
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
				SearchQuery:    0.014,   // Updated from 0.035
				ContextWindow:  1048576, // 1M token context window
			},
			"gemini-3-pro-preview": {
				Hit:            0.20,  // Updated from 0.3125
				Miss:           2.00,  // Updated from 1.25
				Comp:           12.00, // Updated from 5.00
				ThinkingBudget: 65536,
				SearchQuery:    0.014,   // Updated from 0.035
				ContextWindow:  2097152, // 2M token context window
			},
		},
	}
}

// ConfigFinder defines the interface for locating the configuration file across different environments.
type ConfigFinder interface {
	Find() (string, error)
}
