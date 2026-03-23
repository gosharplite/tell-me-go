// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"gopkg.in/yaml.v3"
)

// YAMLConfigLoader implements domain_config.ConfigLoader.
type YAMLConfigLoader struct{}

// Load satisfies the domain_config.ConfigLoader interface.
func (l *YAMLConfigLoader) Load(path string) (*domain_config.Config, error) {
	return load(path)
}

// load reads and parses the configuration file.
func load(path string) (*domain_config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg domain_config.Config
	setDefaults(&cfg)

	expanded := os.ExpandEnv(string(data))
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal expanded yaml: %w", err)
	}

	applyEnvironmentOverrides(&cfg)
	syncLegacyFields(&cfg)

	return &cfg, nil
}

func setDefaults(cfg *domain_config.Config) {
	cfg.MaxToolTurns = domain_config.DefaultMaxToolTurns
	cfg.MaxHistoryTurns = domain_config.DefaultMaxHistoryTurns
	cfg.MaxHistoryTokens = domain_config.DefaultMaxHistoryTokens
	cfg.MaxConcurrentTools = domain_config.DefaultMaxConcurrentTools
	cfg.ToolTimeoutSeconds = domain_config.DefaultToolTimeoutSeconds
	cfg.HTTPTimeoutSeconds = domain_config.DefaultHTTPTimeoutSeconds
	cfg.ShowThoughts = true
	cfg.ShowTools = true

	if os.Getenv("TELL_ME_NO_STREAM") == "true" {
		cfg.DisableStreaming = true
	}
}

func applyEnvironmentOverrides(cfg *domain_config.Config) {
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
}

func syncLegacyFields(cfg *domain_config.Config) {
	active := cfg.GetActiveProvider()
	if cfg.Model == "" {
		cfg.Model = active.Model
	}
	if cfg.URL == "" {
		cfg.URL = active.URL
	}
	if cfg.ThinkingBudget == 0 {
		cfg.ThinkingBudget = active.ThinkingBudget
	}
	if cfg.ThinkingLevel == "" {
		cfg.ThinkingLevel = active.ThinkingLevel
	}
}

// DefaultPricing returns the hardcoded fallback pricing data.
func DefaultPricing() pricing.PricingData {
	return domain_config.DefaultPricing()
}

// JSONSessionLoader implements domain_config.SessionLoader.
type JSONSessionLoader struct{}

// LoadSession reads and parses a session override JSON file.
func (l *JSONSessionLoader) LoadSession(path string) (*domain_config.SessionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pCfg map[string]interface{}
	if err := json.Unmarshal(data, &pCfg); err != nil {
		return nil, err
	}

	cfg := &domain_config.SessionConfig{}

	// Implement the safe extraction logic
	if val, ok := pCfg["MAX_HISTORY_TOKENS"]; ok {
		if v := toInt(val); v != nil {
			cfg.MaxHistoryTokens = v
		}
	}
	if val, ok := pCfg["MAX_TURNS"]; ok {
		if v := toInt(val); v != nil {
			cfg.MaxToolTurns = v
		}
	} else if val, ok := pCfg["MAX_TOOL_TURNS"]; ok {
		if v := toInt(val); v != nil {
			cfg.MaxToolTurns = v
		}
	}
	if val, ok := pCfg["MAX_HISTORY_TURNS"]; ok {
		if v := toInt(val); v != nil {
			cfg.MaxHistoryTurns = v
		}
	}

	return cfg, nil
}

func toInt(val interface{}) *int {
	switch v := val.(type) {
	case float64:
		i := int(v)
		return &i
	case string:
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return &i
		}
	}
	return nil
}
