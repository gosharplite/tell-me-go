// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"encoding/json"
	"fmt"
	"io"
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
	v, err := configureViper(path)
	if err != nil {
		return nil, err
	}

	var cfg domain_config.Config
	setDefaults(&cfg)

	if err := unmarshalConfig(v, &cfg); err != nil {
		return nil, err
	}

	debugLogModels(&cfg)
	syncLegacyFields(&cfg)

	// Domain-level provider validation. Hard errors fail the load;
	// warnings (e.g., Anthropic max_tokens below the thinking-budget
	// floor) are emitted to slog. Use a discard-backed logger when
	// none is configured so warnings don't leak to stderr in tests.
	if err := cfg.ValidateProviders(validationLogger()); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validationLogger returns the logger used for domain-level provider
// validation warnings. In debug mode it forwards to the default
// slog logger; otherwise it discards warn output to keep test output
// quiet. Hard errors are returned via the error path regardless.
func validationLogger() *slog.Logger {
	if isDebug() {
		return slog.Default()
	}
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// configureViper creates a Viper instance, loads the YAML file, and binds environment variables.
func configureViper(path string) (*viper.Viper, error) {
	debugLogFileStatus(path)

	v := viper.NewWithOptions(viper.KeyDelimiter("::"))

	if err := readConfigFile(v, path); err != nil {
		return nil, err
	}

	// Set Environment Overrides (Standardized to TELL_ME_)
	v.SetEnvPrefix("TELL_ME")
	// Replace custom delimiter and dashes so TELL_ME_PROVIDERS_GOOGLE_API_KEY maps to Providers::Google::APIKey
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_", "-", "_"))
	v.AutomaticEnv()

	// Bind Legacy GOSHARP_* variables for backward compatibility
	_ = v.BindEnv("MODE", "GOSHARP_MODE", "TELL_ME_MODE")
	_ = v.BindEnv("PERSON", "GOSHARP_PERSON", "TELL_ME_PERSON")
	_ = v.BindEnv("AIMODEL", "GOSHARP_AIMODEL", "TELL_ME_AIMODEL")
	_ = v.BindEnv("AIURL", "GOSHARP_AIURL", "TELL_ME_AIURL")

	return v, nil
}

// readConfigFile reads the YAML file into Viper, allowing missing files but failing on real errors.
func readConfigFile(v *viper.Viper, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Allow missing file (12-Factor App)
		}
		return err
	}

	debugLogRawContent(path, data)

	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return fmt.Errorf("viper failed to read config: %w", err)
	}

	debugLogViperKeys(v)
	return nil
}

// unmarshalConfig decodes the Viper config into the domain struct using yaml tags.
func unmarshalConfig(v *viper.Viper, cfg *domain_config.Config) error {
	err := v.Unmarshal(cfg, func(c *mapstructure.DecoderConfig) {
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
		return fmt.Errorf("failed to unmarshal viper config: %w", err)
	}
	return nil
}

// isDebug returns true when TELL_ME_DEBUG=1 is set.
func isDebug() bool {
	return os.Getenv("TELL_ME_DEBUG") == "1"
}

// debugLogFileStatus logs file existence information when debug mode is enabled.
func debugLogFileStatus(path string) {
	if !isDebug() {
		return
	}
	slog.Debug("========================================")
	slog.Debug("loading configuration file", slog.String("path", path))
	_, err := os.Stat(path)
	slog.Debug("file status", slog.String("path", path), slog.Bool("exists", err == nil))
}

// debugLogRawContent logs truncated raw file content when debug mode is enabled.
func debugLogRawContent(path string, data []byte) {
	if !isDebug() {
		return
	}
	slog.Debug("raw content", slog.String("content", string(data[:min(len(data), 1000)])))
}

// debugLogViperKeys logs all keys parsed by Viper when debug mode is enabled.
func debugLogViperKeys(v *viper.Viper) {
	if !isDebug() {
		return
	}
	slog.Debug("viper parsed keys")
	for _, key := range v.AllKeys() {
		slog.Debug("parsed entry", slog.String("key", key), slog.Any("value", v.Get(key)))
	}
}

// debugLogModels logs model configuration details after unmarshaling when debug mode is enabled.
func debugLogModels(cfg *domain_config.Config) {
	if !isDebug() {
		return
	}
	slog.Debug("cfg.Models count", slog.Int("count", len(cfg.Models)))
	for k, v := range cfg.Models {
		slog.Debug("model detail",
			slog.String("model", k),
			slog.Int("context_window", v.ContextWindow),
			slog.Float64("pricing_comp", v.Pricing.Comp),
			slog.Float64("pricing_hit", v.Pricing.Hit),
			slog.Float64("pricing_miss", v.Pricing.Miss))
	}
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
