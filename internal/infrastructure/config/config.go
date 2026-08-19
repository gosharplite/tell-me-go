// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/spf13/viper"
	yaml "gopkg.in/yaml.v3"
)

// isDebug reports whether the default slog logger is at Debug level.
func isDebug() bool {
	return slog.Default().Enabled(context.Background(), slog.LevelDebug)
}

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

	// Case-preserving MCP_SERVERS.*.ENV bypass (issue #1407): Viper
	// lowercases nested map keys during Unmarshal, mangling conventionally-
	// cased child environment variable names (PLUR_TOOL_PROFILE would reach
	// the stdio child as plur_tool_profile). Re-parse the raw YAML and stamp
	// the decoded Env maps byte-for-byte, rejecting case-colliding sibling
	// keys deterministically. The missing-file path is already tolerated
	// upstream (readConfigFile returns nil for os.IsNotExist), so a read
	// error here simply skips the bypass and defaults stand. The double file
	// read is an accepted startup/hot-reload-only cost.
	if raw, rerr := os.ReadFile(path); rerr == nil {
		if err := applyCasePreservingMCPServerEnv(raw, &cfg); err != nil {
			return nil, err
		}
	}

	if isDebug() {
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

	if err := cfg.ValidateBounds(); err != nil {
		return nil, err
	}

	syncLegacyFields(&cfg)

	// Domain-level provider validation. Hard errors fail the load;
	// warnings (e.g., Anthropic max_tokens below the thinking-budget
	// floor) are emitted to slog. Use a discard-backed logger when
	// none is configured so warnings don't leak to stderr in tests.
	if err := cfg.ValidateProviders(validationLogger()); err != nil {
		return nil, err
	}

	// Domain-level MCP server validation: key format, URL presence, and
	// per-server timeout bounds. Hard errors fail the load.
	if err := cfg.ValidateMCPServers(); err != nil {
		return nil, err
	}

	// Domain-level MEMORY validation: SERVER presence when ENABLED, budget
	// and flood-bound bounds, and the LEARN tier set. Hard errors fail the
	// load. This single hook covers both static load and every watcher
	// hot-reload re-parse (Refresh → Loader.Load → load); a failed load
	// leaves the watcher's prior state intact per ADR-029 §5.
	if err := cfg.ValidateMemory(); err != nil {
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

// envPrefix and envKeyReplacer are the app-owned environment-binding
// contract consumed by BOTH configureViper and the case-preserving ENV
// bypass (envNameForMCPServerLeaf). Viper applies them to resolve
// TELL_ME_* overrides; the bypass reconstructs the same names.
const envPrefix = "TELL_ME"

var envKeyReplacer = strings.NewReplacer("::", "_", "-", "_")

// envNameForMCPServerLeaf builds the TELL_ME_* name Viper's AutomaticEnv
// resolves for MCP_SERVERS.<name>.ENV.<leaf>, mirroring Viper's two steps —
// mergeWithEnvPrefix (ToUpper of prefix+"_"+path) then envKeyReplacer — from
// the app-owned declarations above. Lockstep is pinned end-to-end by
// TestLoad_MCPStdio_EnvOverrideWins.
func envNameForMCPServerLeaf(name, leaf string) string {
	key := envPrefix + "_" + strings.ToUpper("mcp_servers::"+name+"::env::"+strings.ToLower(leaf))
	return envKeyReplacer.Replace(key)
}

// scalarString renders a raw-YAML scalar as its config-string form, matching
// mapstructure's weak-typing parity: nil → "" (zero value), string → as-is,
// int/bool/float → fmt.Sprint (123 → "123", true → "true").
func scalarString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

// isScalarValue reports whether v is a YAML scalar (nil, string, number,
// bool) rather than a nested mapping or sequence. ENV values must be scalars
// — a nested structure cannot become a child environment variable.
func isScalarValue(v any) bool {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct, reflect.Func, reflect.Chan:
		return false
	default:
		return true
	}
}

// stringKeyMap converts a yaml.v3-decoded mapping node into a map[string]any,
// preserving exact key casing. yaml.v3 decodes a nested mapping as
// map[string]interface{} when every key is a string, but falls back to
// map[interface{}]interface{} when any key is non-string (e.g. an unquoted
// YAML null such as `Null:` or `~`, which parse as a nil key). Non-string
// keys cannot name configuration or environment entries, so they are dropped
// — mirroring Viper, which also cannot represent them.
func stringKeyMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]any, len(m))
		for k, val := range m {
			if s, ok := k.(string); ok {
				out[s] = val
			}
		}
		return out, true
	default:
		return nil, false
	}
}

// applyCasePreservingMCPServerEnv re-applies MCP_SERVERS.*.ENV from the raw
// YAML bytes after Viper's lossy decode, preserving ENV keys byte-for-byte
// and rejecting case-colliding sibling keys deterministically. Viper
// v1.21.0 lowercases every config map key recursively (insensitiviseMap)
// during Unmarshal, which mangles the conventionally-cased variable names
// that stdio children consume via their process environment (issue #1407).
// A Go map decoded by yaml.v3 preserves exact key casing and case-differing
// siblings, so each decoded server's Env map is rebuilt from the raw ENV
// node. Sibling keys that differ only in case are rejected with an error
// naming both raw keys; nothing is normalized or silently collapsed.
func applyCasePreservingMCPServerEnv(raw []byte, cfg *domain_config.Config) error {
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse raw config for MCP_SERVERS ENV bypass: %w", err)
	}

	// Root level: locate the MCP_SERVERS block case-insensitively. A Go map
	// preserves case-differing sibling keys (MCP_SERVERS vs mcp_servers), so
	// a count > 1 is a genuine ambiguity — reject it deterministically.
	mcpKeys := make([]string, 0, 2)
	var rawMCPServers map[string]any
	for k, v := range root {
		if strings.EqualFold(k, "MCP_SERVERS") {
			mcpKeys = append(mcpKeys, k)
			if m, ok := stringKeyMap(v); ok {
				rawMCPServers = m
			}
		}
	}
	switch len(mcpKeys) {
	case 0:
		return nil // no MCP block — nothing to preserve
	case 1:
		// proceed
	default:
		return fmt.Errorf("MCP_SERVERS key collision: raw keys %q and %q both map to the same key — rename one", mcpKeys[0], mcpKeys[1])
	}
	if rawMCPServers == nil {
		return nil // MCP_SERVERS present but not a mapping; Viper decode already failed or nothing to preserve
	}

	// Server level: iterate the DECODED server keys — never stamp a raw-cased
	// server name into cfg.MCPServers, because a raw-cased stamp on an absent
	// key would insert a phantom zero-valued server that fails validation
	// misleadingly.
	for serverName := range cfg.MCPServers {
		rawServerKeys := make([]string, 0, 2)
		var serverNode map[string]any
		for k, v := range rawMCPServers {
			if strings.EqualFold(k, serverName) {
				rawServerKeys = append(rawServerKeys, k)
				if m, ok := stringKeyMap(v); ok {
					serverNode = m
				}
			}
		}
		switch len(rawServerKeys) {
		case 0:
			continue // unreachable: the server was decoded, so it exists raw
		case 1:
			// proceed
		default:
			return fmt.Errorf("MCP_SERVERS key collision: raw keys %q and %q both map to the same key — rename one", rawServerKeys[0], rawServerKeys[1])
		}

		// ENV-container level: navigate unconditionally — do not gate on the
		// decoded Env being non-empty (Viper cannot populate Env from env
		// vars alone, so a raw ENV node with an empty decoded counterpart is
		// exactly the case this bypass exists for). A raw ENV value that is
		// not a mapping (null, scalar) leaves an empty envNode, which the
		// leaf loop below treats as an empty Env.
		envKeys := make([]string, 0, 2)
		var envNode map[string]any
		for k, v := range serverNode {
			if strings.EqualFold(k, "ENV") {
				envKeys = append(envKeys, k)
				if m, ok := stringKeyMap(v); ok {
					envNode = m
				}
			}
		}
		switch len(envKeys) {
		case 0:
			continue // no ENV node — leave the decoded Env untouched
		case 1:
			// proceed
		default:
			return fmt.Errorf("MCP_SERVERS.%s ENV key collision: raw keys %q and %q both map to the same key — rename one", serverName, envKeys[0], envKeys[1])
		}

		// Leaf level: rebuild the Env map from the raw ENV node's children.
		// The raw leaf key IS the byte-for-byte target key — the one level
		// where the raw name is the correct Go map key. Rebuilding from raw
		// also purges the phantom lowercase survivor Viper leaves when Path
		// and PATH both exist.
		env := make(map[string]string, len(envNode))
		for k, rv := range envNode {
			if !isScalarValue(rv) {
				return fmt.Errorf("MCP_SERVERS.%s.ENV.%s must be a scalar value, got map/sequence", serverName, k)
			}
			name := envNameForMCPServerLeaf(serverName, k)
			if override, ok := os.LookupEnv(name); ok && override != "" {
				env[k] = os.ExpandEnv(override) // env-wins; empty treated as unset (AllowEmptyEnv parity)
			} else {
				env[k] = os.ExpandEnv(scalarString(rv))
			}
		}
		// The struct stores values, not pointers, so assign back via the map.
		srv := cfg.MCPServers[serverName]
		srv.Env = env
		cfg.MCPServers[serverName] = srv
	}

	return nil
}

// configureViper creates a Viper instance, loads the YAML file, and binds environment variables.
func configureViper(path string) (*viper.Viper, error) {
	slog.Debug("========================================")
	slog.Debug("loading configuration file", slog.String("path", path))
	_, err := os.Stat(path)
	slog.Debug("file status", slog.String("path", path), slog.Bool("exists", err == nil))

	v := viper.NewWithOptions(viper.KeyDelimiter("::"))

	if err := readConfigFile(v, path); err != nil {
		return nil, err
	}

	// Set Environment Overrides (Standardized to TELL_ME_)
	v.SetEnvPrefix(envPrefix)
	// Replace custom delimiter and dashes so TELL_ME_PROVIDERS_GOOGLE_API_KEY maps to Providers::Google::APIKey
	v.SetEnvKeyReplacer(envKeyReplacer)
	v.AutomaticEnv()

	// Bind Legacy GOSHARP_* variables for backward compatibility
	_ = v.BindEnv("MODE", "GOSHARP_MODE", "TELL_ME_MODE")
	_ = v.BindEnv("PERSON", "GOSHARP_PERSON", "TELL_ME_PERSON")
	_ = v.BindEnv("AIMODEL", "GOSHARP_AIMODEL", "TELL_ME_AIMODEL")
	_ = v.BindEnv("AIURL", "GOSHARP_AIURL", "TELL_ME_AIURL")
	_ = v.BindEnv("WRAP_WIDTH", "TELL_ME_WRAP_WIDTH")

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

	if isDebug() {
		slog.Debug("raw content", slog.String("content", redactRawContent(string(data[:min(len(data), 1000)]))))
	}

	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(string(data))); err != nil {
		return fmt.Errorf("viper failed to read config: %w", err)
	}

	if isDebug() {
		slog.Debug("viper parsed keys")
		for _, key := range v.AllKeys() {
			// ENV/ARGS subtrees are dropped wholesale so innocuous-named
			// sub-keys (e.g. env::FOO) cannot leak values.
			if isSecretKey(key) || hasSecretAncestor(key) {
				slog.Debug("parsed entry", slog.String("key", key), slog.String("value", redactedValue))
			} else {
				slog.Debug("parsed entry", slog.String("key", key), slog.Any("value", v.Get(key)))
			}
		}
	}
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
			intOverflowHook,
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		)
	})
	if err != nil {
		return fmt.Errorf("failed to unmarshal viper config: %w", err)
	}
	return nil
}

func expandEnvHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.String {
		return data, nil
	}
	return os.ExpandEnv(data.(string)), nil
}

// intOverflowHook rejects float64→int conversions where the source value
// would overflow the target integer type or has a non-integer fractional
// part. Viper's YAML parser produces float64 for integers that exceed
// int64 range (≥20 digits); WeaklyTypedInput silently converts these via
// Go's int(float64) which saturates to math.MaxInt on overflow, bypassing
// the negative-value guard in ValidateBounds. This hook catches overflow
// before it reaches the struct decoder.
func intOverflowHook(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	if f.Kind() != reflect.Float64 {
		return data, nil
	}
	if t.Kind() != reflect.Int && t.Kind() != reflect.Int64 {
		return data, nil
	}

	v := data.(float64)
	if v != math.Trunc(v) {
		return nil, fmt.Errorf("cannot decode non-integer float64 %v into integer field", v)
	}
	if v > float64(math.MaxInt) || v < float64(math.MinInt) {
		return nil, fmt.Errorf("integer overflow: value %v exceeds int range [%d, %d]", v, math.MinInt, math.MaxInt)
	}
	return data, nil
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
	cfg.Memory = domain_config.DefaultMemoryConfig()
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
