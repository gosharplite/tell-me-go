// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	// 1. Setup isolated environment
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

	// 2. Execution
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// 3. Verification
	if cfg.Mode != "test-mode" {
		t.Errorf("expected Mode 'test-mode', got '%s'", cfg.Mode)
	}
	if cfg.Model != "test-model" {
		t.Errorf("expected Model 'test-model', got '%s'", cfg.Model)
	}
}
