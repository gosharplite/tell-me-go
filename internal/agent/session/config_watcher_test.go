// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

type testSessionLoader struct{}

func (l *testSessionLoader) LoadSession(path string) (*config.SessionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pCfg map[string]interface{}
	if err := json.Unmarshal(data, &pCfg); err != nil {
		return nil, err
	}
	cfg := &config.SessionConfig{}
	if val, ok := pCfg["MAX_HISTORY_TOKENS"]; ok {
		cfg.MaxHistoryTokens = l.toIntPtr(val)
	}
	if val, ok := pCfg["MAX_TURNS"]; ok {
		cfg.MaxToolTurns = l.toIntPtr(val)
	}
	if val, ok := pCfg["MAX_HISTORY_TURNS"]; ok {
		cfg.MaxHistoryTurns = l.toIntPtr(val)
	}
	return cfg, nil
}

func (l *testSessionLoader) toIntPtr(val interface{}) *int {
	switch v := val.(type) {
	case float64:
		i := int(v)
		return &i
	case string:
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return &i
		}
	}
	return nil
}

func TestConfigWatcher_Refresh(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	cw := session.NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", sessionPath)

	// 1. Initial defaults
	tokens, _, _ := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("expected 100 tokens, got %d", tokens)
	}

	// 2. Create session config
	if err := os.WriteFile(sessionPath, []byte(`{"MAX_HISTORY_TOKENS": 200}`), 0644); err != nil {
		t.Fatal(err)
	}

	cw.Refresh("default")
	tokens, _, _ = cw.GetLimits()
	if tokens != 200 {
		t.Errorf("expected 200 tokens after refresh, got %d", tokens)
	}

	// 3. Update session config (ensure mtime change is detected)
	if err := os.WriteFile(sessionPath, []byte(`{"MAX_HISTORY_TOKENS": "300"}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Explicitly shift the modification time to the future to ensure detection
	futureTime := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(sessionPath, futureTime, futureTime); err != nil {
		t.Fatalf("failed to change file times: %v", err)
	}

	cw.Refresh("default")
	tokens, _, _ = cw.GetLimits()
	if tokens != 300 {
		t.Errorf("expected 300 tokens after second refresh, got %d", tokens)
	}
}

func TestConfigWatcher_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "malformed.json")

	cw := session.NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", sessionPath)

	if err := os.WriteFile(sessionPath, []byte(`{invalid}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Should not panic and should keep old values
	cw.Refresh("default")
	tokens, _, _ := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("expected 100 tokens to be preserved, got %d", tokens)
	}
}

func TestConfigWatcher_MissingFile(t *testing.T) {
	cw := session.NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", "non-existent.json")

	// Should not panic
	cw.Refresh("default")
	tokens, _, _ := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("expected 100 tokens, got %d", tokens)
	}
}

func setupConfigWatcherTest(t *testing.T) (session.ConfigWatcher, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.yaml")
	sessionPath := filepath.Join(tmpDir, "session.json")

	cw := session.NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths(mainPath, sessionPath)
	return cw, mainPath, sessionPath
}

func TestConfigWatcher_MainConfigAndPrecedence(t *testing.T) {

	t.Run("YamlLoading", testYamlLoading)
	t.Run("ModelIsolation", testModelIsolation)
	t.Run("PrecedenceRules", testPrecedenceRules)
	t.Run("DeletionRobustness", testDeletionRobustness)
}

func testYamlLoading(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*session.FileConfigWatcher)
	mockLoader := new(agenttest.MockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MAX_HISTORY_TOKENS: 500
MAX_TURNS: 5
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&config.Config{
		MaxHistoryTokens: 500,
		MaxToolTurns:     5,
	}, nil)

	cw.Refresh("default")
	tokens, toolTurns, _ := cw.GetLimits()

	if tokens != 500 {
		t.Errorf("expected 500 tokens from YAML, got %d", tokens)
	}
	if toolTurns != 5 {
		t.Errorf("expected 5 tool turns from YAML, got %d", toolTurns)
	}
}

func testModelIsolation(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*session.FileConfigWatcher)
	mockLoader := new(agenttest.MockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MODELS:
  model-a:
    CONTEXT_WINDOW: 1000
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&config.Config{
		Models: map[string]config.ModelConfig{
			"model-a": {ContextWindow: 1000},
		},
	}, nil)

	// Refresh with model-b (NOT in YAML) first to ensure it doesn't pick up model-a's values
	cw.Refresh("model-b")
	window := cw.GetContextWindow()
	if window == 1000 {
		t.Errorf("model-b should NOT have model-a's context window (1000)")
	}

	// Now refresh with model-a
	cw.Refresh("model-a")
	window = cw.GetContextWindow()
	if window != 1000 {
		t.Errorf("expected 1000 context window for model-a, got %d", window)
	}

	// Switch back to model-b and ensure it goes back to default
	cw.Refresh("model-b")
	window = cw.GetContextWindow()
	if window == 1000 {
		t.Errorf("model-b should NOT retain model-a's context window after switching back")
	}
	if window != cw.GetDefaultWindow() {
		t.Errorf("expected default context window for model-b, got %d", window)
	}
}

func testPrecedenceRules(t *testing.T) {
	fcw, mainPath, sessionPath := setupConfigWatcherTest(t)
	cw := fcw.(*session.FileConfigWatcher)
	mockLoader := new(agenttest.MockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MAX_HISTORY_TOKENS: 500
MAX_TURNS: 5
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&config.Config{
		MaxHistoryTokens: 500,
		MaxToolTurns:     5,
	}, nil)

	if err := os.WriteFile(sessionPath, []byte(`{"MAX_HISTORY_TOKENS": 999}`), 0644); err != nil {
		t.Fatal(err)
	}

	cw.Refresh("model-a")
	tokens, toolTurns, _ := cw.GetLimits()
	if tokens != 999 {
		t.Errorf("expected 999 tokens (session override), got %d", tokens)
	}
	if toolTurns != 5 {
		t.Errorf("expected 5 tool turns (from YAML), got %d", toolTurns)
	}
}

func testDeletionRobustness(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*session.FileConfigWatcher)
	mockLoader := new(agenttest.MockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MAX_HISTORY_TOKENS: 500
MAX_TURNS: 5
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&config.Config{
		MaxHistoryTokens: 500,
		MaxToolTurns:     5,
	}, nil)

	cw.Refresh("model-a")

	// Verify initial state
	tokens, toolTurns, _ := cw.GetLimits()
	if tokens != 500 || toolTurns != 5 {
		t.Fatalf("setup failed: expected (500, 5), got (%d, %d)", tokens, toolTurns)
	}

	// Remove file
	_ = os.Remove(mainPath)
	cw.Refresh("model-a")

	// Assert old values persist
	tokens, toolTurns, _ = cw.GetLimits()
	if tokens != 500 {
		t.Errorf("expected 500 tokens to persist after YAML deletion, got %d", tokens)
	}
	if toolTurns != 5 {
		t.Errorf("expected 5 tool turns to persist after YAML deletion, got %d", toolTurns)
	}
}

func TestConfigWatcher_ManualLimits(t *testing.T) {
	// Not implemented
}
