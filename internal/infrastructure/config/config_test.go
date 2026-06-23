// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

func TestDefaultPricing(t *testing.T) {
	p := DefaultPricing()
	if len(p.Models) == 0 {
		t.Error("expected non-empty pricing data")
	}

	if _, ok := p.Models["gpt-5"]; !ok {
		t.Error("expected gpt-5 to be present in default pricing")
	}
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
		return
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
		name    string
		yamlVal string // "KEY: overflow" line
		errFrag string // substring expected in the error message
	}{
		{name: "MAX_TURNS overflow", yamlVal: "MAX_TURNS: " + overflow, errFrag: "MAX_TURNS"},
		{name: "MAX_HISTORY_TURNS overflow", yamlVal: "MAX_HISTORY_TURNS: " + overflow, errFrag: "MAX_HISTORY_TURNS"},
		{name: "MAX_HISTORY_TOKENS overflow", yamlVal: "MAX_HISTORY_TOKENS: " + overflow, errFrag: "MAX_HISTORY_TOKENS"},
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
			if !strings.Contains(err.Error(), "must be >= 0") {
				t.Errorf("expected error containing \"must be >= 0\", got %q", err.Error())
			}
		})
	}
}
