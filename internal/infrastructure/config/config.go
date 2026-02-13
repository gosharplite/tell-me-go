// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"gopkg.in/yaml.v3"
)

// Re-export constants for compatibility
const (
	DefaultMaxToolTurns     = domain_config.DefaultMaxToolTurns
	DefaultMaxHistoryTurns  = domain_config.DefaultMaxHistoryTurns
	DefaultMaxHistoryTokens = domain_config.DefaultMaxHistoryTokens
	DefaultTieredThreshold    = domain_config.DefaultTieredThreshold
	WarningRatio              = domain_config.WarningRatio
	SystemContextBuffer       = domain_config.SystemContextBuffer
	DefaultMaxLoopRepetitions = domain_config.DefaultMaxLoopRepetitions
)

// Config is an alias for domain_config.Config
type Config = domain_config.Config

// ModelConfig is an alias for domain_config.ModelConfig
type ModelConfig = domain_config.ModelConfig

// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	// Set defaults
	cfg.MaxToolTurns = domain_config.DefaultMaxToolTurns
	cfg.MaxHistoryTurns = domain_config.DefaultMaxHistoryTurns
	cfg.MaxHistoryTokens = domain_config.DefaultMaxHistoryTokens
	cfg.MaxConcurrentTools = domain_config.DefaultMaxConcurrentTools
	cfg.ToolTimeoutSeconds = domain_config.DefaultToolTimeoutSeconds
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
			},
			"flash": {
				Hit:             0.025,
				Miss:            0.10,
				Comp:            0.40,
				TieredThreshold: 0,
				TieredMiss:      0.20,
				TieredComp:      0.80,
			},
			"pro": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 0,
				TieredMiss:      2.50,
				TieredComp:      15.00,
			},
			"default": {
				Hit:             0.125,
				Miss:            1.25,
				Comp:            10.00,
				TieredThreshold: 0,
				TieredMiss:      2.50,
				TieredComp:      15.00,
			},
		},
		ThinkingBudgets: map[string]int{
			"gemini-3-flash-preview": 32768,
			"gemini-3-pro-preview":   65536,
		},
		SearchQuery: 0.035,
	}
}
