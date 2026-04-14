// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
)

func TestLoad(t *testing.T) {

	t.Run("ValidConfig", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test.yaml")

		yamlContent := `
MODE: "test-mode"
PERSON: "test-person"
AIMODEL: "test-model"
AIURL: "http://test.url"
`
		if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := load(configPath)
		if err != nil {
			t.Fatalf("load() failed: %v", err)
		}

		if cfg.Mode != "test-mode" {
			t.Errorf("expected Mode 'test-mode', got '%s'", cfg.Mode)
		}
		if cfg.Model != "test-model" {
			t.Errorf("expected Model 'test-model', got '%s'", cfg.Model)
		}
		// Verify defaults
		if cfg.MaxToolTurns != 200 {
			t.Errorf("expected default MaxToolTurns 200, got %d", cfg.MaxToolTurns)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		cfg, err := load("non-existent.yaml")
		if err != nil {
			t.Errorf("expected no error for non-existent file, got %v", err)
		}
		if cfg == nil {
			t.Fatal("expected config to be initialized even without file")
		}
		// Verify defaults are set
		if cfg.MaxToolTurns != 200 {
			t.Errorf("expected default MaxToolTurns 200, got %d", cfg.MaxToolTurns)
		}
	})

	t.Run("InvalidYAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "invalid.yaml")
		if err := os.WriteFile(configPath, []byte(": invalid"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := load(configPath)
		if err == nil {
			t.Error("expected error for invalid YAML, got nil")
		}
	})
}

func TestLoad_EnvOverride(t *testing.T) {
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
