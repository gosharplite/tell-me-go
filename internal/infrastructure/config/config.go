// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
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
	// Only show debug output if TELL_ME_DEBUG=1
	if os.Getenv("TELL_ME_DEBUG") == "1" {
		slog.Debug("========================================")
		slog.Debug("loading configuration file", slog.String("path", path))
		
		fileExists := func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		}
		slog.Debug("file status", slog.String("path", path), slog.Bool("exists", fileExists(path)))
	}

	v := viper.NewWithOptions(viper.KeyDelimiter("::"))

	// 1. Read the file content
	data, err := os.ReadFile(path)
	if err == nil {
		if os.Getenv("TELL_ME_DEBUG") == "1" {
			slog.Debug("raw content", slog.String("content", string(data[:min(len(data), 1000)])))
		}
		
		v.SetConfigType("yaml")
		if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
			return nil, fmt.Errorf("viper failed to read config: %w", err)
		}
		
		// Debug: Show what Viper parsed
		if os.Getenv("TELL_ME_DEBUG") == "1" {
			slog.Debug("viper parsed keys")
			for _, key := range v.AllKeys() {
				slog.Debug("parsed entry", slog.String("key", key), slog.Any("value", v.Get(key)))
			}
		}
	} else if !os.IsNotExist(err) {
		// Fail on real errors (like permissions), but allow missing file (12-Factor App)
		return nil, err
	}

	// 2. Set Environment Overrides (Standardized to TELL_ME_)
	v.SetEnvPrefix("TELL_ME")
	// Replace custom delimiter and dashes so TELL_ME_PROVIDERS_GOOGLE_API_KEY maps to Providers::Google::APIKey
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_", "-", "_"))
	v.AutomaticEnv()

	// 3. Bind Legacy GOSHARP_* variables for backward compatibility
	_ = v.BindEnv("MODE", "GOSHARP_MODE", "TELL_ME_MODE")
	_ = v.BindEnv("PERSON", "GOSHARP_PERSON", "TELL_ME_PERSON")
	_ = v.BindEnv("AIMODEL", "GOSHARP_AIMODEL", "TELL_ME_AIMODEL")
	_ = v.BindEnv("AIURL", "GOSHARP_AIURL", "TELL_ME_AIURL")

	// 4. Initialize Domain Struct and Defaults
	var cfg domain_config.Config
	setDefaults(&cfg) // Populate struct defaults first so Viper only overwrites what it finds

	// 5. Unmarshal using `yaml` tags to ensure consistency between YAML files and Viper internal lowercasing
	err = v.Unmarshal(&cfg, func(c *mapstructure.DecoderConfig) {
		c.TagName = "yaml"
		c.WeaklyTypedInput = true
		c.Squash = true
		c.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			expandEnvHook,
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal viper config: %w", err)
	}

	// Debug the Models field after unmarshaling
	if os.Getenv("TELL_ME_DEBUG") == "1" {
		slog.Debug("cfg.Models count", slog.Int("count", len(cfg.Models)))
		for k, v := range cfg.Models {
			slog.Debug("model detail",
				slog.String("model", k),
				slog.Int("context_window", v.ContextWindow),
				slog.Float64("pricing_comp", v.Pricing.Comp),
				slog.Float64("pricing_hit", v.Pricing.Hit),
				slog.Float64("pricing_miss", v.Pricing.Miss),
				slog.Float64("pricing_thinking", v.Pricing.Thinking))
		}
	}

	// 6. Execute legacy fallback synchronization
	syncLegacyFields(&cfg)

	return &cfg, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func expandEnvHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}
	return os.ExpandEnv(data.(string)), nil
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
