// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/spf13/viper"
)

// YAMLConfigLoader implements domain_config.ConfigLoader.
type YAMLConfigLoader struct {
	Finder domain_config.ConfigFinder
}

// Load satisfies the domain_config.ConfigLoader interface.
func (l *YAMLConfigLoader) Load(path string) (*domain_config.Config, error) {
	if path == "" {
		if l.Finder == nil {
			return nil, fmt.Errorf("config finder not initialized")
		}
		var err error
		path, err = l.Finder.Find()
		if err != nil {
			return nil, fmt.Errorf("failed to auto-discover config: %w", err)
		}
	}
	return load(path)
}

// load reads and parses the configuration file.
func load(path string) (*domain_config.Config, error) {
	v := viper.New()

	// 1. Read the file manually to preserve ${VAR} expansion
	data, err := os.ReadFile(path)
	if err == nil {
		expanded := os.ExpandEnv(string(data))
		v.SetConfigType("yaml")
		if err := v.ReadConfig(strings.NewReader(expanded)); err != nil {
			return nil, fmt.Errorf("viper failed to read config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		// Fail on real errors (like permissions), but allow missing file (12-Factor App)
		return nil, err
	}

	// 2. Set Environment Overrides (Standardized to TELL_ME_)
	v.SetEnvPrefix("TELL_ME")
	// Replace dots and dashes so TELL_ME_PROVIDERS_GOOGLE_API_KEY maps to Providers.Google.APIKey
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// 3. Bind Legacy GOSHARP_* variables for backward compatibility
	_ = v.BindEnv("MODE", "GOSHARP_MODE", "TELL_ME_MODE")
	_ = v.BindEnv("PERSON", "GOSHARP_PERSON", "TELL_ME_PERSON")
	_ = v.BindEnv("AIMODEL", "GOSHARP_AIMODEL", "TELL_ME_AIMODEL")
	_ = v.BindEnv("AIURL", "GOSHARP_AIURL", "TELL_ME_AIURL")

	// 4. Initialize Domain Struct and Defaults
	var cfg domain_config.Config
	setDefaults(&cfg) // Populate struct defaults first so Viper only overwrites what it finds

	// 5. Unmarshal using `yaml` tags to avoid modifying domain structs
	err = v.Unmarshal(&cfg, func(c *mapstructure.DecoderConfig) {
		c.TagName = "yaml" // CRITICAL: Tell Viper to read the yaml tags
	})
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal viper config: %w", err)
	}

	// 6. Execute legacy fallback synchronization
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

type sessionDTO struct {
	MaxHistoryTokens *int `json:"MAX_HISTORY_TOKENS"`
	MaxToolTurns     *int `json:"MAX_TURNS"` // Standardized to match YAML
	LegacyToolTurns  *int `json:"MAX_TOOL_TURNS"`
	MaxHistoryTurns  *int `json:"MAX_HISTORY_TURNS"`
}

// LoadSession reads and parses a session override JSON file.
func (l *JSONSessionLoader) LoadSession(path string) (*domain_config.SessionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var dto sessionDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return nil, fmt.Errorf("parse session config: %w", err)
	}

	// Fallback for legacy key
	if dto.MaxToolTurns == nil && dto.LegacyToolTurns != nil {
		dto.MaxToolTurns = dto.LegacyToolTurns
	}

	cfg := &domain_config.SessionConfig{
		MaxHistoryTokens: dto.MaxHistoryTokens,
		MaxToolTurns:     dto.MaxToolTurns,
		MaxHistoryTurns:  dto.MaxHistoryTurns,
	}

	return cfg, nil
}
