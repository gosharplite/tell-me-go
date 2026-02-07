// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/pricing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

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

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}

		if cfg.Mode != "test-mode" {
			t.Errorf("expected Mode 'test-mode', got '%s'", cfg.Mode)
		}
		if cfg.Model != "test-model" {
			t.Errorf("expected Model 'test-model', got '%s'", cfg.Model)
		}
		// Verify defaults
		if cfg.MaxToolTurns != 10 {
			t.Errorf("expected default MaxToolTurns 10, got %d", cfg.MaxToolTurns)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		_, err := Load("non-existent.yaml")
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

		_, err := Load(configPath)
		if err == nil {
			t.Error("expected error for invalid YAML, got nil")
		}
	})
}

func TestLoad_EnvOverride(t *testing.T) {
	os.Setenv("GOSHARP_MODE", "env-mode")
	defer os.Unsetenv("GOSHARP_MODE")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_env.yaml")
	_ = os.WriteFile(configPath, []byte("MODE: yaml-mode"), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Mode != "env-mode" {
		t.Errorf("expected Mode 'env-mode' (from ENV), got '%s'", cfg.Mode)
	}
}

func TestLoad_MoreEnvOverrides(t *testing.T) {
	os.Setenv("GOSHARP_PERSON", "env-person")
	os.Setenv("GOSHARP_AIMODEL", "env-model")
	os.Setenv("GOSHARP_AIURL", "env-url")
	os.Setenv("TELL_ME_NO_STREAM", "true")
	defer func() {
		os.Unsetenv("GOSHARP_PERSON")
		os.Unsetenv("GOSHARP_AIMODEL")
		os.Unsetenv("GOSHARP_AIURL")
		os.Unsetenv("TELL_ME_NO_STREAM")
	}()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_env_more.yaml")
	_ = os.WriteFile(configPath, []byte("PERSON: yaml-person"), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
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

func TestResolveThinkingBudget(t *testing.T) {
	cfg := &Config{
		Models: map[string]ModelConfig{
			"gemini-2.0-flash": {MaxThinkingBudget: 1000},
			"pro":              {MaxThinkingBudget: 5000},
		},
	}
	pricingData := pricing.PricingData{
		ThinkingBudgets: map[string]int{
			"default": 2000,
			"extra":   10000,
		},
	}

	tests := []struct {
		model    string
		expected int
	}{
		{"gemini-2.0-flash", 1000},   // Exact match
		{"gemini-2.0-pro-exp", 5000}, // Substring match ("pro")
		{"extra-special", 10000},     // Pricing match
		{"unknown", 2000},            // Default
	}

	for _, tt := range tests {
		got := cfg.ResolveThinkingBudget(tt.model, pricingData)
		if got != tt.expected {
			t.Errorf("model %s: expected %d, got %d", tt.model, tt.expected, got)
		}
	}
}
