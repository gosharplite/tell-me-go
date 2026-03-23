// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
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
		_, err := load("non-existent.yaml")
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
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
	t.Setenv("TELL_ME_NO_STREAM", "true")

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
	if !cfg.DisableStreaming {
		t.Error("expected DisableStreaming to be true")
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

func TestJSONSessionLoader_LoadSession(t *testing.T) {
	loader := &JSONSessionLoader{}

	t.Run("ValidAllFields", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "session.json")
		content := `{"MAX_HISTORY_TOKENS": 500, "MAX_TURNS": 15, "MAX_HISTORY_TURNS": 25}`
		_ = os.WriteFile(path, []byte(content), 0644)

		cfg, err := loader.LoadSession(path)
		if err != nil {
			t.Fatalf("LoadSession failed: %v", err)
		}

		if cfg.MaxHistoryTokens == nil || *cfg.MaxHistoryTokens != 500 {
			t.Errorf("expected 500 tokens, got %v", cfg.MaxHistoryTokens)
		}
		if cfg.MaxToolTurns == nil || *cfg.MaxToolTurns != 15 {
			t.Errorf("expected 15 tool turns, got %v", cfg.MaxToolTurns)
		}
		if cfg.MaxHistoryTurns == nil || *cfg.MaxHistoryTurns != 25 {
			t.Errorf("expected 25 history turns, got %v", cfg.MaxHistoryTurns)
		}
	})

	t.Run("EmptyJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "empty.json")
		_ = os.WriteFile(path, []byte("{}"), 0644)

		cfg, err := loader.LoadSession(path)
		if err != nil {
			t.Fatalf("LoadSession failed: %v", err)
		}

		if cfg.MaxHistoryTokens != nil || cfg.MaxToolTurns != nil || cfg.MaxHistoryTurns != nil {
			t.Error("expected all fields to be nil for empty JSON")
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "invalid.json")
		_ = os.WriteFile(path, []byte("{invalid}"), 0644)

		_, err := loader.LoadSession(path)
		if err == nil {
			t.Error("expected error for invalid JSON, got nil")
		}
	})

	t.Run("FileNotFound", func(t *testing.T) {
		_, err := loader.LoadSession("non-existent.json")
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})
}
