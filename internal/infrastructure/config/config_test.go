// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
)

type loadTestCase struct {
	name          string
	fileContent   string
	useValidPath  bool
	wantErr       bool
	wantMode      string
	wantModel     string
	wantToolTurns int
}

func assertLoadConfig(t *testing.T, cfg *domain_config.Config, tt loadTestCase) {
	t.Helper()
	if tt.wantMode != "" && cfg.Mode != tt.wantMode {
		t.Errorf("expected Mode '%s', got '%s'", tt.wantMode, cfg.Mode)
	}
	if tt.wantModel != "" && cfg.Model != tt.wantModel {
		t.Errorf("expected Model '%s', got '%s'", tt.wantModel, cfg.Model)
	}
	if cfg.MaxToolTurns != tt.wantToolTurns {
		t.Errorf("expected default MaxToolTurns %d, got %d", tt.wantToolTurns, cfg.MaxToolTurns)
	}
}

func TestLoad(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "") // neutralize ambient env pollution

	tests := []loadTestCase{
		{
			name:          "ValidConfig",
			fileContent:   "MODE: \"test-mode\"\nPERSON: \"test-person\"\nAIMODEL: \"test-model\"\nAIURL: \"http://test.url\"",
			useValidPath:  true,
			wantErr:       false,
			wantMode:      "test-mode",
			wantModel:     "test-model",
			wantToolTurns: 200,
		},
		{
			name:          "NonExistentFile",
			useValidPath:  false,
			wantErr:       false,
			wantToolTurns: 200,
		},
		{
			name:         "InvalidYAML",
			fileContent:  ": invalid",
			useValidPath: true,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "non-existent.yaml"
			if tt.useValidPath {
				tmpDir := t.TempDir()
				path = filepath.Join(tmpDir, "test.yaml")
				if err := os.WriteFile(path, []byte(tt.fileContent), 0644); err != nil {
					t.Fatalf("failed to write test config: %v", err)
				}
			}

			cfg, err := load(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("load() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if cfg == nil {
				t.Fatal("expected config to be initialized")
			}

			assertLoadConfig(t, cfg, tt)
		})
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "") // neutralize ambient env pollution
	t.Setenv("GOSHARP_MODE", "env-mode")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_env.yaml")
	_ = os.WriteFile(configPath, []byte("MODE: yaml-mode"), 0644)

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	if cfg.Mode != "env-mode" {
		t.Errorf("expected Mode 'env-mode' (from ENV), got '%s'", cfg.Mode)
	}
}

func TestLoad_MoreEnvOverrides(t *testing.T) {
	t.Setenv("GOSHARP_PERSON", "env-person")
	t.Setenv("GOSHARP_AIMODEL", "env-model")
	t.Setenv("GOSHARP_AIURL", "env-url")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_env_more.yaml")
	_ = os.WriteFile(configPath, []byte("PERSON: yaml-person"), 0644)

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	if cfg.Person != "env-person" {
		t.Errorf("expected Person 'env-person', got '%s'", cfg.Person)
	}
	if cfg.Model != "env-model" {
		t.Errorf("expected Model 'env-model', got '%s'", cfg.Model)
	}
	if cfg.URL != "env-url" {
		t.Errorf("expected URL 'env-url', got '%s'", cfg.URL)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution
	t.Setenv("MOCK_SECRET", "xyz123")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_expansion.yaml")

	yamlContent := `
SELECTED_PROVIDER: "work-openai"
PROVIDERS:
  work-openai:
    TYPE: "openai"
    API_KEY: "${MOCK_SECRET}"
    MODEL: "gpt-4"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	provider := cfg.Providers["work-openai"]
	if provider.APIKey != "xyz123" {
		t.Errorf("expected APIKey 'xyz123', got '%s'", provider.APIKey)
	}
}

func TestLoad_SyncActiveProvider(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_sync.yaml")

	yamlContent := `
SELECTED_PROVIDER: "google"
PROVIDERS:
  google:
    TYPE: "google"
    MODEL: "gemini-3-flash"
    URL: "https://google.com/ai"
    THINKING_BUDGET: 4096
    THINKING_LEVEL: "MEDIUM"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	if cfg.Model != "gemini-3-flash" {
		t.Errorf("expected legacy Model to be synced to 'gemini-3-flash', got '%s'", cfg.Model)
	}
	if cfg.URL != "https://google.com/ai" {
		t.Errorf("expected legacy URL to be synced to 'https://google.com/ai', got '%s'", cfg.URL)
	}
	if cfg.ThinkingBudget != 4096 {
		t.Errorf("expected legacy ThinkingBudget to be synced to 4096, got %d", cfg.ThinkingBudget)
	}
	if cfg.ThinkingLevel != "MEDIUM" {
		t.Errorf("expected legacy ThinkingLevel to be synced to 'MEDIUM', got '%s'", cfg.ThinkingLevel)
	}
}

type sessionTestCase struct {
	name              string
	fileContent       string
	setupFile         bool
	wantErr           bool
	wantHistoryTokens *int
	wantToolTurns     *int
	wantHistoryTurns  *int
	expectAllNil      bool
}

func TestJSONSessionLoader_LoadSession(t *testing.T) {
	i500 := 500
	i15 := 15
	i25 := 25
	i10 := 10

	tests := []sessionTestCase{
		{
			name:              "ValidAllFields",
			fileContent:       `{"MAX_HISTORY_TOKENS": 500, "MAX_TURNS": 15, "MAX_HISTORY_TURNS": 25}`,
			setupFile:         true,
			wantErr:           false,
			wantHistoryTokens: &i500,
			wantToolTurns:     &i15,
			wantHistoryTurns:  &i25,
		},
		{
			name:         "EmptyJSON",
			fileContent:  `{}`,
			setupFile:    true,
			wantErr:      false,
			expectAllNil: true,
		},
		{
			name:        "InvalidJSON",
			fileContent: `{invalid}`,
			setupFile:   true,
			wantErr:     true,
		},
		{
			name:      "FileNotFound",
			setupFile: false,
			wantErr:   true,
		},
		{
			name:          "LegacyToolTurns",
			fileContent:   `{"MAX_TOOL_TURNS": 10}`,
			setupFile:     true,
			wantErr:       false,
			wantToolTurns: &i10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &JSONSessionLoader{}
			path := "non-existent.json"

			if tt.setupFile {
				tmpDir := t.TempDir()
				path = filepath.Join(tmpDir, "session.json")
				if err := os.WriteFile(path, []byte(tt.fileContent), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
			}

			cfg, err := loader.LoadSession(path)
			assertSessionState(t, cfg, err, tt)
		})
	}
}

func checkIntPtr(t *testing.T, fieldName string, expected, actual *int) {
	t.Helper()
	if expected == nil {
		return
	}
	if actual == nil || *actual != *expected {
		t.Errorf("expected %s %d, got %v", fieldName, *expected, actual)
	}
}

func assertSessionState(t *testing.T, got *domain_config.SessionConfig, err error, tt sessionTestCase) {
	t.Helper()
	if (err != nil) != tt.wantErr {
		t.Fatalf("LoadSession() error = %v, wantErr %v", err, tt.wantErr)
	}

	if tt.wantErr || got == nil {
		return
	}

	if tt.expectAllNil {
		if got.MaxHistoryTokens != nil || got.MaxToolTurns != nil || got.MaxHistoryTurns != nil {
			t.Error("expected all fields to be nil for empty JSON")
		}
		return
	}

	checkIntPtr(t, "history tokens", tt.wantHistoryTokens, got.MaxHistoryTokens)
	checkIntPtr(t, "tool turns", tt.wantToolTurns, got.MaxToolTurns)
	checkIntPtr(t, "history turns", tt.wantHistoryTurns, got.MaxHistoryTurns)
}

func TestLoad_TELL_ME_EnvOverrides(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "tell-me-mode")
	t.Setenv("TELL_ME_PROVIDERS_GOOGLE_MODEL", "tell-me-model")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_tellme.yaml")

	yamlContent := `
MODE: "yaml-mode"
PROVIDERS:
  google:
    MODEL: "yaml-model"
`
	_ = os.WriteFile(configPath, []byte(yamlContent), 0644)

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	if cfg.Mode != "tell-me-mode" {
		t.Errorf("expected Mode 'tell-me-mode', got '%s'", cfg.Mode)
	}
	// Note: Viper automatic mapping to nested map keys from environment variables
	// can be tricky. Let's see if this works.
	if cfg.Providers["google"].Model != "tell-me-model" {
		t.Errorf("expected Provider Google Model 'tell-me-model', got '%s'", cfg.Providers["google"].Model)
	}
}

type mockFinder struct {
	path   string
	err    error
	called bool
}

func (f *mockFinder) Find() (string, error) {
	f.called = true
	return f.path, f.err
}

func TestYAMLConfigLoader_Load_AutoDiscovery(t *testing.T) {
	t.Run("CallsFinderOnEmptyPath", func(t *testing.T) {
		finder := &mockFinder{path: "discovered.yaml"}
		loader := &YAMLConfigLoader{Finder: finder}

		cfg, err := loader.Load("")
		if err != nil {
			t.Fatalf("Load(\"\") failed: %v", err)
		}

		if !finder.called {
			t.Error("expected Finder.Find() to be called")
		}
		if cfg == nil {
			t.Fatal("expected config to be initialized")
		}
	})

	t.Run("FinderError", func(t *testing.T) {
		expectedErr := fmt.Errorf("find failed")
		finder := &mockFinder{err: expectedErr}
		loader := &YAMLConfigLoader{Finder: finder}

		_, err := loader.Load("")
		if err == nil || !strings.Contains(err.Error(), "find failed") {
			t.Errorf("expected error containing 'find failed', got %v", err)
		}
	})

	t.Run("FinderNotInitialized", func(t *testing.T) {
		loader := &YAMLConfigLoader{}
		_, err := loader.Load("")
		if err == nil || !strings.Contains(err.Error(), "config finder not initialized") {
			t.Errorf("expected error 'config finder not initialized', got %v", err)
		}
	})
}

func TestLoad_ModelWithDots(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_dots.yaml")
	yamlContent := `
MODELS:
  "deepseek-ai/deepseek-v3.2-maas":
    CONTEXT_WINDOW: 163840
    PRICING:
      COMP: 5.40
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	modelName := "deepseek-ai/deepseek-v3.2-maas"
	if _, ok := cfg.Models[modelName]; !ok {
		t.Errorf("expected model '%s' to be present, got keys: %v", modelName, cfg.Models)
	}
}

func TestLoad_WithDebugEnabled(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "") // neutralize ambient env pollution
	t.Setenv("TELL_ME_DEBUG", "1")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "debug_test.yaml")

	yamlContent := `
MODE: "debug-mode"
MODELS:
  "debug-model":
    CONTEXT_WINDOW: 1000
    PRICING:
      COMP: 1.0
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() with debug=1 failed: %v", err)
	}

	if cfg.Mode != "debug-mode" {
		t.Errorf("expected Mode 'debug-mode', got '%s'", cfg.Mode)
	}
}

// TestLoad_ProviderMaxTokens_RoundTrip pins that PROVIDERS.<name>.MAX_TOKENS
// round-trips correctly from YAML through Viper + mapstructure into
// the domain LLMProvider.MaxTokens field, and that the loader rejects
// negative values via the domain Validate path.
//
// FAILURE MEANING: If round-trip breaks, operators cannot configure
// MAX_TOKENS without recompiling — the entire purpose of Task H.
// If negative values are accepted, the API later returns a generic
// 400 with no breadcrumb back to the YAML field.
func TestLoad_ProviderMaxTokens_RoundTrip(t *testing.T) {
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tests := []struct {
		name    string
		yamlVal string // text for the MAX_TOKENS line; empty omits the field
		wantErr bool
		wantVal int
		errFrag string
	}{
		{name: "positive value round-trips", yamlVal: "MAX_TOKENS: 16384", wantVal: 16384},
		{name: "explicit zero round-trips as zero", yamlVal: "MAX_TOKENS: 0", wantVal: 0},
		{name: "field omitted defaults to zero", yamlVal: "", wantVal: 0},
		{name: "negative value rejected", yamlVal: "MAX_TOKENS: -1", wantErr: true, errFrag: "MAX_TOKENS"},
		{name: "large value accepted", yamlVal: "MAX_TOKENS: 65000", wantVal: 65000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "test_max_tokens.yaml")
			yamlContent := `
SELECTED_PROVIDER: "claude"
PROVIDERS:
  claude:
    TYPE: "anthropic"
    MODEL: "claude-opus-4-6"
    API_KEY: "test"
    ` + tt.yamlVal + `
`
			if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := load(configPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), tt.errFrag) {
					t.Errorf("expected error containing %q, got %q", tt.errFrag, err.Error())
				}
				return
			}
			if got := cfg.Providers["claude"].MaxTokens; got != tt.wantVal {
				t.Errorf("MaxTokens = %d; want %d", got, tt.wantVal)
			}
		})
	}
}

// TestReadConfigFile_NonNotExistError exercises the os.ReadFile error path
// that is NOT os.ErrNotExist (line 131 in config.go). When os.ReadFile fails
// with a permission error, path-is-directory error, etc., readConfigFile must
// return the raw error and NOT silently swallow it as a missing file.
func TestReadConfigFile_NonNotExistError(t *testing.T) {
	tmpDir := t.TempDir()
	dirPath := filepath.Join(tmpDir, "config-dir")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}

	v := viper.New()
	err := readConfigFile(v, dirPath)
	if err == nil {
		t.Fatal("expected error when reading a directory as a config file, got nil")
	}
	if os.IsNotExist(err) {
		t.Errorf("expected a non-IsNotExist error, but got IsNotExist: %v", err)
	}
}

// TestReadConfigFile_WithDebugEnabled exercises the two isDebug() branches
// inside readConfigFile (lines 134–136 and 143–147). The default slog logger
// runs at Info level so those blocks are normally skipped during tests.  We
// temporarily promote the default logger to Debug (writing to io.Discard to
// keep output quiet) and restore the original logger via t.Cleanup.
func TestReadConfigFile_WithDebugEnabled(t *testing.T) {
	// Save and restore default logger
	originalLogger := slog.Default()
	debugLogger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(debugLogger)
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "debug-config.yaml")
	if err := os.WriteFile(configPath, []byte("MODE: debug-test\n"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	v := viper.New()
	if err := readConfigFile(v, configPath); err != nil {
		t.Fatalf("readConfigFile() with debug enabled failed: %v", err)
	}
}

// TestValidationLogger_WhenDebug exercises the isDebug() true branch of
// validationLogger (lines 90-92). When the default slog logger is at Debug
// level, validationLogger must return slog.Default() — not the discard logger.
func TestValidationLogger_WhenDebug(t *testing.T) {
	originalLogger := slog.Default()
	debugLogger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(debugLogger)
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	got := validationLogger()
	if got == nil {
		t.Fatal("validationLogger() returned nil")
	}
	if got != slog.Default() {
		t.Error("expected validationLogger() to return slog.Default() when debug is enabled")
	}
}

// TestLoad_UnmarshalError exercises the unmarshalConfig error path (lines
// 164-166) and load's propagation of it (lines 57-59). Viper+mapstructure
// with WeaklyTypedInput silently coerces most type mismatches, but a scalar
// string where a map is expected (MODELS: "not-a-map") reliably triggers a
// decode failure.
func TestLoad_UnmarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad-types.yaml")
	yamlContent := `
MODE: "test"
MODELS: "this-is-a-string-not-a-map"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := load(configPath)
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal viper config") {
		t.Errorf("expected error wrapping 'failed to unmarshal viper config', got: %v", err)
	}
}

// TestLoad_ModelDebugLogging exercises the isDebug() block in load that
// iterates cfg.Models (lines 61-70). The default slog logger runs at Info
// level so the block is normally skipped during tests. We temporarily
// promote the logger to Debug (writing to io.Discard) and supply a config
// with at least one model entry so both the count log and the per-model
// iteration body are exercised.
func TestLoad_ModelDebugLogging(t *testing.T) {
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	originalLogger := slog.Default()
	debugLogger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(debugLogger)
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "debug-models.yaml")
	yamlContent := `
MODE: "debug-test"
SELECTED_PROVIDER: "openai"
PROVIDERS:
  openai:
    TYPE: "openai"
    MODEL: "gpt-5"
    API_KEY: "sk-test"
MODELS:
  "gpt-5":
    CONTEXT_WINDOW: 128000
    PRICING:
      COMP: 5.00
      HIT: 2.50
      MISS: 0.00
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() with debug+models failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	m, ok := cfg.Models["gpt-5"]
	if !ok {
		t.Fatal("expected model 'gpt-5' in config")
	}
	if m.ContextWindow != 128000 {
		t.Errorf("expected ContextWindow 128000, got %d", m.ContextWindow)
	}
}

// TestLoad_ProviderMaxTokens_EnvOverride pins that the Viper env-binding
// applies to the new MAX_TOKENS field. Mirrors TestLoad_TELL_ME_EnvOverrides
// for the THINKING_BUDGET-adjacent precedent.
func TestLoad_ProviderMaxTokens_EnvOverride(t *testing.T) {
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution
	t.Setenv("TELL_ME_PROVIDERS_GOOGLE_MAX_TOKENS", "8000")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_env_max_tokens.yaml")
	yamlContent := `
SELECTED_PROVIDER: "google"
PROVIDERS:
  google:
    TYPE: "gemini"
    MAX_TOKENS: 32000
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	if got := cfg.Providers["google"].MaxTokens; got != 8000 {
		t.Errorf("expected env override MAX_TOKENS=8000, got %d", got)
	}
}

// TestLoad_IntegerOverflowIsDetected verifies that load() rejects
// integer-overflow values for every overflow-vulnerable int field.
// Viper+mapstructure with WeaklyTypedInput silently wraps a 20-digit
// integer past the int64 ceiling to a negative value; ValidateBounds()
// then rejects the negative value with a "must be >= 0" error naming
// the offending YAML field.
func TestLoad_IntegerOverflowIsDetected(t *testing.T) {
	// 99999999999999999999 is 20 digits — well beyond int64 max (9223372036854775807)
	const overflow = "99999999999999999999"

	tests := []struct {
		name     string
		yamlVal  string // "KEY: overflow" line
		errFrag  string // substring expected in the error message
		errFrag2 string // optional second error fragment; only checked when non-empty
	}{
		{name: "MAX_TURNS overflow", yamlVal: "MAX_TURNS: " + overflow, errFrag: "integer overflow", errFrag2: "exceeds int range"},
		{name: "MAX_HISTORY_TURNS overflow", yamlVal: "MAX_HISTORY_TURNS: " + overflow, errFrag: "integer overflow", errFrag2: "exceeds int range"},
		{name: "MAX_HISTORY_TOKENS overflow", yamlVal: "MAX_HISTORY_TOKENS: " + overflow, errFrag: "integer overflow", errFrag2: "exceeds int range"},
		{name: "MAX_TURNS non-integer float64", yamlVal: "MAX_TURNS: 1.5", errFrag: "cannot decode non-integer float64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELL_ME_MODE", "") // neutralize ambient env pollution

			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "test_overflow.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yamlVal+"\n"), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := load(configPath)
			if err == nil {
				t.Fatal("expected error for integer overflow, got nil")
			}
			if !strings.Contains(err.Error(), tt.errFrag) {
				t.Errorf("expected error containing %q, got %q", tt.errFrag, err.Error())
			}
			if tt.errFrag2 != "" && !strings.Contains(err.Error(), tt.errFrag2) {
				t.Errorf("expected error containing %q, got %q", tt.errFrag2, err.Error())
			}
		})
	}
}

// TestLoad_IntOverflowHook_HappyPath covers the final return in
// intOverflowHook (config.go:212): an integral, in-range float64 YAML
// value flowing into an int field passes the hook's guards and decodes
// successfully. The error paths are covered by
// TestLoad_IntegerOverflowIsDetected.
func TestLoad_IntOverflowHook_HappyPath(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "") // neutralize ambient env pollution

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.yaml")
	content := "MAX_TURNS: 5.0\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(path)
	if err != nil {
		t.Fatalf("load() unexpected error: %v", err)
	}
	if cfg.MaxToolTurns != 5 {
		t.Fatalf("MaxToolTurns = %d, want 5", cfg.MaxToolTurns)
	}
}

// TestLoad_ValidateBoundsError verifies that load() propagates ValidateBounds
// errors for values that pass intOverflowHook (e.g., negative integers within
// float64 range) but fail domain-level bounds checks. This covers the error
// propagation path at config.go:74-76.
func TestLoad_ValidateBoundsError(t *testing.T) {
	tests := []struct {
		name    string
		yamlVal string // a single top-level int field line
		errFrag string // substring expected in the error
	}{
		{name: "MAX_TURNS negative", yamlVal: "MAX_TURNS: -1", errFrag: "MAX_TURNS must be >= 0"},
		{name: "MAX_HISTORY_TURNS negative", yamlVal: "MAX_HISTORY_TURNS: -1", errFrag: "MAX_HISTORY_TURNS must be >= 0"},
		{name: "MAX_HISTORY_TOKENS negative", yamlVal: "MAX_HISTORY_TOKENS: -1", errFrag: "MAX_HISTORY_TOKENS must be >= 0"},
		{name: "MAX_CONCURRENT_TOOLS negative", yamlVal: "MAX_CONCURRENT_TOOLS: -1", errFrag: "MAX_CONCURRENT_TOOLS must be >= 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TELL_ME_MODE", "") // neutralize ambient env pollution

			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "test_bounds.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yamlVal+"\n"), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := load(configPath)
			if err == nil {
				t.Fatal("expected ValidateBounds error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errFrag) {
				t.Errorf("expected error containing %q, got %q", tt.errFrag, err.Error())
			}
		})
	}
}

// TestLoad_ValidateMCPServersError verifies that load() propagates
// ValidateMCPServers errors (config.go:89-91) for invalid MCP_SERVERS
// entries: a server key that violates ^[a-z0-9-]{1,24}$ (uppercase and
// underscore), and a valid key whose server fails
// (*MCPServerConfig).validate (empty URL).
func TestLoad_ValidateMCPServersError(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "") // neutralize ambient env pollution

	tests := []struct {
		name        string
		fileContent string
		wantErrSub  string
	}{
		{
			name: "InvalidKeyFormat",
			fileContent: "MCP_SERVERS:\n" +
				"  \"Bad_Key\":\n" + // uppercase + underscore violates ^[a-z0-9-]{1,24}$
				"    URL: \"https://example.com/mcp\"\n",
			wantErrSub: "MCP_SERVERS key",
		},
		{
			name: "ValidKeyEmptyURL",
			fileContent: "MCP_SERVERS:\n" +
				"  \"good-key\":\n" + // valid key; empty URL fails (*MCPServerConfig).validate
				"    URL: \"\"\n",
			wantErrSub: "URL must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "test.yaml")
			if err := os.WriteFile(path, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := load(path)
			if err == nil {
				t.Fatalf("load() expected error containing %q, got nil", tt.wantErrSub)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("load() error = %q, want substring %q", err.Error(), tt.wantErrSub)
			}
		})
	}
}

// TestLoad_MemoryConfig_RoundTripAndValidation pins the MEMORY section
// round-trip through Viper + mapstructure into domain Config.Memory and the
// loader's invocation of ValidateMemory (config.go). A full MEMORY block
// parses into the expected MemoryConfig; a bad LEARN tier and ENABLED with
// an explicitly empty SERVER both fail the load with MEMORY.-prefixed
// messages.
//
// DEVATION (Architect adjudication, T3): the spec's "ENABLED: true with no
// SERVER fails Load" cannot hold — setDefaults seeds Server "plur", and
// mapstructure merges absent keys, so an absent SERVER keeps the seeded
// default (ADR-068 §5's "install once" default; the *dynamic* stage — server
// missing from MCP_SERVERS — handles real absence, ADR-068 §5 two-stage
// fallback). The static check fires on an explicitly empty SERVER, which is
// what the third case pins; the second case documents that omitting SERVER
// entirely yields the seeded default.
func TestLoad_MemoryConfig_RoundTripAndValidation(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tests := []struct {
		name        string
		fileContent string
		want        *domain_config.MemoryConfig
		wantErr     bool
		errFrag     string
	}{
		{
			name: "full MEMORY block round-trips",
			fileContent: "MEMORY:\n" +
				"  ENABLED: true\n" +
				"  SERVER: plur\n" +
				"  INJECT_BUDGET: 5000\n" +
				"  LEARN: full\n" +
				"  SCOPE: team-x\n" +
				"  MAX_LEARNS_PER_SESSION: 5\n",
			want: &domain_config.MemoryConfig{
				Enabled:             true,
				Server:              "plur",
				InjectBudget:        5000,
				LearnTier:           domain_config.MemoryLearnFull,
				Scope:               "team-x",
				MaxLearnsPerSession: 5,
			},
		},
		{
			name: "invalid LEARN tier rejected",
			fileContent: "MEMORY:\n" +
				"  ENABLED: true\n" +
				"  SERVER: plur\n" +
				"  LEARN: weekly\n",
			wantErr: true,
			errFrag: `MEMORY.LEARN must be one of "off", "capture", "batch", "full"`,
		},
		{
			name: "ENABLED with explicit empty SERVER rejected",
			fileContent: "MEMORY:\n" +
				"  ENABLED: true\n" +
				"  SERVER: \"\"\n",
			wantErr: true,
			errFrag: "MEMORY.SERVER must not be empty when ENABLED is true",
		},
		{
			name: "ENABLED without SERVER keeps the seeded plur default",
			fileContent: "MEMORY:\n" +
				"  ENABLED: true\n",
			want: &domain_config.MemoryConfig{
				Enabled:             true,
				Server:              "plur",
				InjectBudget:        2000,
				LearnTier:           domain_config.MemoryLearnBatch,
				MaxLearnsPerSession: 3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "test_memory.yaml")
			if err := os.WriteFile(path, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("load() expected error containing %q, got nil", tt.errFrag)
				}
				if !strings.Contains(err.Error(), tt.errFrag) {
					t.Fatalf("load() error = %q, want substring %q", err.Error(), tt.errFrag)
				}
				return
			}
			if err != nil {
				t.Fatalf("load() failed: %v", err)
			}
			if !reflect.DeepEqual(cfg.Memory, *tt.want) {
				t.Errorf("Memory = %+v; want %+v", cfg.Memory, *tt.want)
			}
		})
	}
}

// TestLoad_WrapWidth_RoundTrip pins that the top-level WRAP_WIDTH field
// round-trips correctly from YAML through Viper + mapstructure into the
// domain Config.WrapWidth field, and that the loader rejects negative
// values via the domain ValidateBounds path. Zero is the correct default
// when the field is omitted.
func TestLoad_WrapWidth_RoundTrip(t *testing.T) {
	t.Setenv("TELL_ME_WRAP_WIDTH", "")        // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tests := []struct {
		name    string
		yamlVal string // text for the WRAP_WIDTH line; empty omits the field
		wantErr bool
		wantVal int
		errFrag string
	}{
		{name: "positive value round-trips", yamlVal: "WRAP_WIDTH: 120", wantVal: 120},
		{name: "explicit zero round-trips as zero", yamlVal: "WRAP_WIDTH: 0", wantVal: 0},
		{name: "field omitted defaults to zero", yamlVal: "", wantVal: 0},
		{name: "negative value rejected", yamlVal: "WRAP_WIDTH: -1", wantErr: true, errFrag: "WRAP_WIDTH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "test_wrap_width.yaml")
			// WRAP_WIDTH is a top-level key (unlike provider-scoped
			// MAX_TOKENS), so the injected line must not be indented.
			yamlContent := `
SELECTED_PROVIDER: "claude"
PROVIDERS:
  claude:
    TYPE: "anthropic"
    MODEL: "claude-opus-4-6"
    API_KEY: "test"
` + tt.yamlVal + `
`
			if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := load(configPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), tt.errFrag) {
					t.Errorf("expected error containing %q, got %q", tt.errFrag, err.Error())
				}
				return
			}
			if got := cfg.WrapWidth; got != tt.wantVal {
				t.Errorf("WrapWidth = %d; want %d", got, tt.wantVal)
			}
		})
	}
}

// TestLoad_WrapWidth_EnvOverride pins that TELL_ME_WRAP_WIDTH takes
// precedence over the WRAP_WIDTH value in the YAML file.
func TestLoad_WrapWidth_EnvOverride(t *testing.T) {
	t.Setenv("TELL_ME_WRAP_WIDTH", "200")
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_env_wrap_width.yaml")
	yamlContent := `
SELECTED_PROVIDER: "claude"
PROVIDERS:
  claude:
    TYPE: "anthropic"
    MODEL: "claude-opus-4-6"
    API_KEY: "test"
WRAP_WIDTH: 120
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	if got := cfg.WrapWidth; got != 200 {
		t.Errorf("expected env override WRAP_WIDTH=200, got %d", got)
	}
}

// TestLoad_WrapWidth_EnvOnly is the critical test: TELL_ME_WRAP_WIDTH set
// in the environment MUST take effect even when WRAP_WIDTH is absent from
// the YAML file entirely. This only works because configureViper registers
// the key with BindEnv — AutomaticEnv alone cannot surface a key that
// Unmarshal never asks for (viper.AllKeys() omits it).
func TestLoad_WrapWidth_EnvOnly(t *testing.T) {
	t.Setenv("TELL_ME_WRAP_WIDTH", "150")
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_env_only_wrap_width.yaml")
	yamlContent := `
SELECTED_PROVIDER: "claude"
PROVIDERS:
  claude:
    TYPE: "anthropic"
    MODEL: "claude-opus-4-6"
    API_KEY: "test"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	if got := cfg.WrapWidth; got != 150 {
		t.Errorf("expected env-only WRAP_WIDTH=150, got %d", got)
	}
}

// captureDebugLogs promotes the default slog logger to Debug writing into a
// buffer (restored via t.Cleanup) so tests can assert on emitted diagnostics.
func captureDebugLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &buf
}

// TestLoad_ParsedDump_EnvOverriddenValues is the liveness probe (issue #1393
// acceptance criterion 1). The parsed-key debug dump runs inside
// readConfigFile, which configureViper calls BEFORE wiring env overrides
// (SetEnvPrefix + AutomaticEnv), so TODAY the dump serializes YAML-file
// values, not TELL_ME_* overrides; the liveness probe therefore asserts the
// YAML URL value appears. DEVATION (Architect adjudication, T1): the
// env-overridden URL (env-url) cannot appear while the dump stays inside
// readConfigFile; if a future refactor moves the dump after env wiring, the
// env-super-secret assertion below becomes the critical leak guard — both
// wiring orders are covered by this test. The env vars remain set so the
// scenario the issue describes stays exercised.
func TestLoad_ParsedDump_EnvOverriddenValues(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution
	t.Setenv("TELL_ME_MCP_SERVERS_ATLASSIAN_TOKEN", "env-super-secret")
	t.Setenv("TELL_ME_MCP_SERVERS_ATLASSIAN_URL", "env-url")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_redaction.yaml")
	yamlContent := `
SELECTED_PROVIDER: "test-provider"
PROVIDERS:
  test-provider:
    TYPE: "openai"
    MODEL: "gpt-4o"
    URL: "https://api.openai.com/v1"
    API_KEY: "yaml-api-key"
    MAX_TOKENS: 16384
MCP_SERVERS:
  atlassian:
    URL: "https://example.com/mcp"
    AUTH: "bearer"
    TOKEN: "yaml-token"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	buf := captureDebugLogs(t)
	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	output := buf.String()
	if strings.Contains(output, "env-super-secret") {
		t.Error("debug output leaked the env-overridden TOKEN value (env-super-secret)")
	}
	if !strings.Contains(output, "https://example.com/mcp") {
		t.Error("debug output missing the parsed URL value; liveness probe failed (dump not emitted or value redacted)")
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Error("debug output missing the [REDACTED] placeholder")
	}
	if strings.Contains(output, "yaml-token") {
		t.Error("debug output leaked the raw YAML TOKEN value (yaml-token)")
	}
	if strings.Contains(output, "yaml-api-key") {
		t.Error("debug output leaked the raw YAML API_KEY value (yaml-api-key)")
	}
	if !strings.Contains(output, "16384") {
		t.Error("debug output missing the non-secret MAX_TOKENS value (16384)")
	}
}

// TestRawContentDump_InvalidYAML_ColonlessSecretRedacted exercises issue
// #1393 acceptance criterion 2's invalid-YAML class: a colonless secret line
// (which viper rejects) must still be redacted in the raw-content debug dump
// before the parse error surfaces.
func TestRawContentDump_InvalidYAML_ColonlessSecretRedacted(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_invalid_colonless.yaml")
	yamlContent := "MODE: \"test\"\nTOKEN sk-123\n"
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	buf := captureDebugLogs(t)
	_, err := load(configPath)
	if err == nil {
		t.Fatal("expected load() to reject the colonless YAML line")
	}

	output := buf.String()
	if strings.Contains(output, "sk-123") {
		t.Error("debug output leaked the colonless secret value (sk-123)")
	}
	if !strings.Contains(output, "TOKEN [REDACTED]") {
		t.Error("debug output missing the colonless redaction (TOKEN [REDACTED])")
	}
}

// TestLoad_ParsedDump_MCPStdioRedaction pins the #1396 parsed-dump redaction
// contract for MCP stdio servers: neither the raw-content dump nor the
// parsed-key dump may log an ARGS element or an ENV value. The parsed dump
// must additionally drop innocuous-named ENV sub-keys (env::FOO) via
// hasSecretAncestor, since their leaf alone is not deny-listed.
func TestLoad_ParsedDump_MCPStdioRedaction(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_stdio_redaction.yaml")
	yamlContent := `
MCP_SERVERS:
  fs:
    COMMAND: "uvx"
    ARGS: ["--token", "sk-1234"]
    ENV:
      FOO: "sk-5678"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	buf := captureDebugLogs(t)
	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	output := buf.String()
	if strings.Contains(output, "sk-1234") {
		t.Error("debug output leaked an ARGS element value (sk-1234)")
	}
	if strings.Contains(output, "sk-5678") {
		t.Error("debug output leaked an ENV value (sk-5678)")
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Error("debug output missing the [REDACTED] placeholder")
	}
	if !strings.Contains(output, "mcp_servers::fs::env") {
		t.Error("debug output missing the parsed env key; parsed dump not emitted")
	}
}

// TestLoad_MCPStdio_EnvExpansion pins the issue §1 claim: ${VAR} expansion
// must apply to the new stdio fields — the COMMAND string, each ARGS slice
// element, and each ENV map value. expandEnvHook fires per string element /
// value through mapstructure's decode hook pipeline; if it ever stops firing
// for []string elements or map[string]string values, this test fails and the
// hook needs a change (the values are compared against the expanded forms).
func TestLoad_MCPStdio_EnvExpansion(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")                    // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "")       // neutralize ambient env pollution
	t.Setenv("TELL_ME_MCP_SERVERS_FS_ENV_PATH", "") // neutralize the ENV leaf under test
	t.Setenv("UVX_PATH", "/venv/bin")
	t.Setenv("ARG_VAL", "expanded-arg")
	t.Setenv("CUSTOM_PATH", "/custom/bin")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_stdio_expansion.yaml")
	yamlContent := `
MCP_SERVERS:
  fs:
    COMMAND: "${UVX_PATH}/uvx"
    ARGS: ["--flag=${ARG_VAL}"]
    ENV:
      PATH: "${CUSTOM_PATH}"
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	fs := cfg.MCPServers["fs"]
	if fs.Command != "/venv/bin/uvx" {
		t.Errorf("COMMAND not expanded: got %q, want %q", fs.Command, "/venv/bin/uvx")
	}
	if len(fs.Args) != 1 || fs.Args[0] != "--flag=expanded-arg" {
		t.Errorf("ARGS element not expanded: got %v, want [--flag=expanded-arg]", fs.Args)
	}
	// The case-preserving ENV bypass (issue #1407) re-applies ENV from the
	// raw YAML byte-for-byte, so the key casing is now an invariant: the
	// expanded value must be reachable at Env["PATH"] directly.
	if got := fs.Env["PATH"]; got != "/custom/bin" {
		t.Errorf("ENV value not expanded: Env[PATH] = %q, want %q", got, "/custom/bin")
	}
}

// TestLoad_MCPStdio_EnvKeyCasePreserved pins issue #1407: ENV map keys for
// stdio MCP servers must survive config load byte-for-byte, because the
// spawned child process consumes them as case-sensitive environment variable
// names. Viper v1.21.0 lowercases every nested map key during Unmarshal; the
// case-preserving bypass re-applies ENV from the raw YAML after the decode.
// PLUR_TOOL_PROFILE must stay exactly as written, Path and PATH must coexist
// as distinct keys, a null value must surface as the zero string, and no
// phantom lowercase key (path) may appear.
//
// DEVIATION (documented for Architect review): the task's YAML block wrote
// the null leaf as an unquoted `Null:` — but in YAML, unquoted Null is the
// null scalar, so yaml.v3 parses it as a nil KEY (not the string "Null"),
// which cannot name an environment variable and is dropped (Viper drops it
// too). The key is quoted ("Null":) to preserve the task's own assertions —
// a key literally named "Null" whose null VALUE surfaces as "".
func TestLoad_MCPStdio_EnvKeyCasePreserved(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")                                 // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "")                    // neutralize ambient env pollution
	t.Setenv("TELL_ME_MCP_SERVERS_FS_ENV_PLUR_TOOL_PROFILE", "") // neutralize the leaf under test
	t.Setenv("TELL_ME_MCP_SERVERS_FS_ENV_PATH", "")              // neutralizes both Path and PATH (same env name)
	t.Setenv("TELL_ME_MCP_SERVERS_FS_ENV_NULL", "")              // neutralize the leaf under test

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_mcp_env_case.yaml")
	yamlContent := `
MCP_SERVERS:
  fs:
    COMMAND: "uvx"
    ENV:
      PLUR_TOOL_PROFILE: "full"
      Path: "a"
      PATH: "b"
      "Null":
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	fs := cfg.MCPServers["fs"]
	if got := fs.Env["PLUR_TOOL_PROFILE"]; got != "full" {
		t.Errorf("Env[PLUR_TOOL_PROFILE] = %q, want %q", got, "full")
	}
	if got := fs.Env["Path"]; got != "a" {
		t.Errorf("Env[Path] = %q, want %q", got, "a")
	}
	if got := fs.Env["PATH"]; got != "b" {
		t.Errorf("Env[PATH] = %q, want %q", got, "b")
	}
	if got := fs.Env["Null"]; got != "" {
		t.Errorf("Env[Null] = %q, want empty string", got)
	}
	if len(fs.Env) != 4 {
		t.Errorf("len(Env) = %d, want 4 (no phantom lowercase keys); got %v", len(fs.Env), fs.Env)
	}
}

// TestLoad_MCPStdio_EnvOverrideWins pins the bypass's env-wins contract for
// MCP_SERVERS.*.ENV leaves: a non-empty TELL_ME_MCP_SERVERS_*_ENV_* override
// beats the YAML value, an empty override is treated as unset (AllowEmptyEnv
// parity — the file value stands), and a case-colliding override applies to
// every casing of the name because the reconstructed env name lowercases the
// leaf before uppercasing the full path.
func TestLoad_MCPStdio_EnvOverrideWins(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	writeConfig := func(t *testing.T, envYAML string) string {
		t.Helper()
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test_mcp_env_override.yaml")
		yamlContent := "MCP_SERVERS:\n  fs:\n    COMMAND: \"uvx\"\n    ENV:\n" + envYAML
		if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}
		return configPath
	}

	t.Run("non-empty override wins over YAML", func(t *testing.T) {
		t.Setenv("TELL_ME_MCP_SERVERS_FS_ENV_FOO", "env")
		cfg, err := load(writeConfig(t, "      FOO: \"yaml\"\n"))
		if err != nil {
			t.Fatalf("load() failed: %v", err)
		}
		if got := cfg.MCPServers["fs"].Env["FOO"]; got != "env" {
			t.Errorf("Env[FOO] = %q, want %q", got, "env")
		}
	})

	t.Run("empty override treated as unset", func(t *testing.T) {
		t.Setenv("TELL_ME_MCP_SERVERS_FS_ENV_FOO", "")
		cfg, err := load(writeConfig(t, "      FOO: \"yaml\"\n"))
		if err != nil {
			t.Fatalf("load() failed: %v", err)
		}
		if got := cfg.MCPServers["fs"].Env["FOO"]; got != "yaml" {
			t.Errorf("Env[FOO] = %q, want %q", got, "yaml")
		}
	})

	t.Run("case-collision override applies to all casings", func(t *testing.T) {
		t.Setenv("TELL_ME_MCP_SERVERS_FS_ENV_PATH", "env")
		cfg, err := load(writeConfig(t, "      Path: \"a\"\n      PATH: \"b\"\n"))
		if err != nil {
			t.Fatalf("load() failed: %v", err)
		}
		env := cfg.MCPServers["fs"].Env
		if got := env["Path"]; got != "env" {
			t.Errorf("Env[Path] = %q, want %q", got, "env")
		}
		if got := env["PATH"]; got != "env" {
			t.Errorf("Env[PATH] = %q, want %q", got, "env")
		}
	})
}

// TestLoad_MCPStdio_StructuralCollisionRejected pins that the bypass rejects
// case-colliding sibling keys deterministically. Viper collapses such keys
// with a random iteration-order winner; the bypass reads the raw YAML, which
// preserves both keys, so the rejection is deterministic and names both raw
// keys verbatim in the error text.
func TestLoad_MCPStdio_StructuralCollisionRejected(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tests := []struct {
		name        string
		fileContent string
		wantKeys    []string // both raw keys must appear in the error text
	}{
		{
			name:        "server-key collision",
			fileContent: "MCP_SERVERS:\n  Plur:\n    URL: \"https://a\"\n  plur:\n    URL: \"https://b\"\n",
			wantKeys:    []string{"Plur", "plur"},
		},
		{
			name:        "ENV-key collision",
			fileContent: "MCP_SERVERS:\n  fs:\n    COMMAND: \"uvx\"\n    ENV:\n      A: \"1\"\n    env:\n      B: \"2\"\n",
			wantKeys:    []string{"ENV", "env"},
		},
		{
			name: "top-level collision",
			fileContent: "MCP_SERVERS:\n  fs:\n    COMMAND: \"uvx\"\n" +
				"mcp_servers:\n  fs:\n    COMMAND: \"uvx\"\n",
			wantKeys: []string{"MCP_SERVERS", "mcp_servers"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "test_collision.yaml")
			if err := os.WriteFile(path, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := load(path)
			if err == nil {
				t.Fatalf("load() expected error naming %v, got nil", tt.wantKeys)
			}
			for _, key := range tt.wantKeys {
				if !strings.Contains(err.Error(), key) {
					t.Errorf("load() error = %q, want substring %q", err.Error(), key)
				}
			}
		})
	}
}

// TestLoad_MCPStdio_EnvNonScalarRejected pins that a nested map or sequence
// as an ENV value fails the load. mapstructure rejects the non-scalar during
// Unmarshal — before the bypass runs — so only err != nil is asserted, never
// a message, which would over-pin the decoder's wording.
func TestLoad_MCPStdio_EnvNonScalarRejected(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")              // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "") // neutralize ambient env pollution

	tests := []struct {
		name     string
		envValue string
	}{
		{name: "nested map", envValue: "FOO:\n        a: 1"},
		{name: "sequence", envValue: "FOO: [1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "test_non_scalar.yaml")
			fileContent := "MCP_SERVERS:\n  fs:\n    COMMAND: \"uvx\"\n    ENV:\n      " + tt.envValue + "\n"
			if err := os.WriteFile(path, []byte(fileContent), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := load(path)
			if err == nil {
				t.Fatal("load() expected error for non-scalar ENV value, got nil")
			}
		})
	}
}

// TestLoad_MCPStdio_ServerKeyNormalized pins the no-over-reject contract: a
// raw server key with non-lowercase casing (Plur) decodes to the normalized
// lowercase key (plur), its ENV values are preserved, and the bypass never
// stamps the raw-cased name into the decoded map (which would insert a
// phantom zero-valued server that fails validation misleadingly).
//
// DEVIATION (documented for Architect review): the task's spec put ENV under
// a URL-only server, but domain validation (mcp_config.go, #1396) rejects
// ARGS/DIR/ENV without COMMAND — "loads successfully" is impossible with URL
// + ENV. COMMAND is used instead so the server is a valid stdio server; the
// no-over-reject and ENV-preservation assertions are unchanged.
func TestLoad_MCPStdio_ServerKeyNormalized(t *testing.T) {
	t.Setenv("TELL_ME_MODE", "")                   // neutralize ambient env pollution
	t.Setenv("TELL_ME_SELECTED_PROVIDER", "")      // neutralize ambient env pollution
	t.Setenv("TELL_ME_MCP_SERVERS_PLUR_ENV_A", "") // neutralize the leaf under test

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_mcp_server_normalized.yaml")
	yamlContent := `
MCP_SERVERS:
  Plur:
    COMMAND: "uvx"
    ENV:
      A: 1
`
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := load(configPath)
	if err != nil {
		t.Fatalf("load() failed: %v", err)
	}

	srv, ok := cfg.MCPServers["plur"]
	if !ok {
		t.Fatalf("expected decoded server key %q, got %v", "plur", cfg.MCPServers)
	}
	if got := srv.Env["A"]; got != "1" {
		t.Errorf("Env[A] = %q, want %q", got, "1")
	}
	if _, ok := cfg.MCPServers["Plur"]; ok {
		t.Error("raw-cased server key 'Plur' must not be stamped into the decoded map")
	}
}
