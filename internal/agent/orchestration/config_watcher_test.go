// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
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

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", sessionPath)

	// 1. Initial defaults
	tokens, _, _, _ := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("expected 100 tokens, got %d", tokens)
	}

	// 2. Create session config
	if err := os.WriteFile(sessionPath, []byte(`{"MAX_HISTORY_TOKENS": 200}`), 0644); err != nil {
		t.Fatal(err)
	}

	cw.Refresh("default")
	tokens, _, _, _ = cw.GetLimits()
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
	tokens, _, _, _ = cw.GetLimits()
	if tokens != 300 {
		t.Errorf("expected 300 tokens after second refresh, got %d", tokens)
	}
}

func TestConfigWatcher_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "malformed.json")

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", sessionPath)

	if err := os.WriteFile(sessionPath, []byte(`{invalid}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Should not panic and should keep old values
	cw.Refresh("default")
	tokens, _, _, _ := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("expected 100 tokens to be preserved, got %d", tokens)
	}
}

func TestConfigWatcher_MissingFile(t *testing.T) {
	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", "non-existent.json")

	// Should not panic
	cw.Refresh("default")
	tokens, _, _, _ := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("expected 100 tokens, got %d", tokens)
	}
}

func setupConfigWatcherTest(t *testing.T) (ConfigWatcher, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.yaml")
	sessionPath := filepath.Join(tmpDir, "session.json")

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
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
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
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
	tokens, toolTurns, _, _ := cw.GetLimits()

	if tokens != 500 {
		t.Errorf("expected 500 tokens from YAML, got %d", tokens)
	}
	if toolTurns != 5 {
		t.Errorf("expected 5 tool turns from YAML, got %d", toolTurns)
	}
}

func testModelIsolation(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
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
	window := cw.contextWindow
	if window == 1000 {
		t.Errorf("model-b should NOT have model-a's context window (1000)")
	}

	// Now refresh with model-a
	cw.Refresh("model-a")
	window = cw.contextWindow
	if window != 1000 {
		t.Errorf("expected 1000 context window for model-a, got %d", window)
	}

	// Switch back to model-b and ensure it goes back to default
	cw.Refresh("model-b")
	window = cw.contextWindow
	if window == 1000 {
		t.Errorf("model-b should NOT retain model-a's context window after switching back")
	}
	if window != cw.defaultWindow {
		t.Errorf("expected default context window for model-b, got %d", window)
	}
}

func testPrecedenceRules(t *testing.T) {
	fcw, mainPath, sessionPath := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
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
	tokens, toolTurns, _, _ := cw.GetLimits()
	if tokens != 999 {
		t.Errorf("expected 999 tokens (session override), got %d", tokens)
	}
	if toolTurns != 5 {
		t.Errorf("expected 5 tool turns (from YAML), got %d", toolTurns)
	}
}

func testDeletionRobustness(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
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
	tokens, toolTurns, _, _ := cw.GetLimits()
	if tokens != 500 || toolTurns != 5 {
		t.Fatalf("setup failed: expected (500, 5), got (%d, %d)", tokens, toolTurns)
	}

	// Remove file
	_ = os.Remove(mainPath)
	cw.Refresh("model-a")

	// Assert old values persist
	tokens, toolTurns, _, _ = cw.GetLimits()
	if tokens != 500 {
		t.Errorf("expected 500 tokens to persist after YAML deletion, got %d", tokens)
	}
	if toolTurns != 5 {
		t.Errorf("expected 5 tool turns to persist after YAML deletion, got %d", toolTurns)
	}
}

func TestConfigWatcher_ManualLimits(t *testing.T) {
	mockSess := new(mockSessionLoader)
	cw := NewFileConfigWatcher(nil, mockSess, 100, 10, 20, nil)

	t.Run("SetLimits_Positive", func(t *testing.T) {
		cw.SetLimits(200, 15, 25)
		tokens, toolTurns, historyTurns, _ := cw.GetLimits()
		assert.Equal(t, 200, tokens)
		assert.Equal(t, 15, toolTurns)
		assert.Equal(t, 25, historyTurns)
	})

	t.Run("SetLimits_NonPositivePreserves", func(t *testing.T) {
		cw.SetLimits(200, 15, 25) // Reset to known state
		cw.SetLimits(0, -1, -5)
		tokens, toolTurns, historyTurns, _ := cw.GetLimits()
		assert.Equal(t, 200, tokens)
		assert.Equal(t, 15, toolTurns)
		assert.Equal(t, 25, historyTurns)
	})
}

func TestConfigWatcher_ApplyLimits(t *testing.T) {

	t.Run("FullUpdate", func(t *testing.T) {
		cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
		limits := events.Limits{
			MaxHistoryTokens: 500,
			MaxToolTurns:     30,
			MaxHistoryTurns:  40,
			TieredThreshold:  5000,
		}

		cw.ApplyLimits(limits)
		tokens, toolTurns, historyTurns, threshold := cw.GetLimits()
		assert.Equal(t, 500, tokens)
		assert.Equal(t, 30, toolTurns)
		assert.Equal(t, 40, historyTurns)
		assert.Equal(t, 5000, threshold)
	})

	t.Run("PartialUpdate", func(t *testing.T) {
		cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil).(*fileConfigWatcher)
		cw.tieredThreshold = 1000
		limits := events.Limits{
			MaxHistoryTokens: 0,
			MaxToolTurns:     -1,
			MaxHistoryTurns:  40,
			TieredThreshold:  0,
		}

		cw.ApplyLimits(limits)
		tokens, toolTurns, historyTurns, threshold := cw.GetLimits()
		assert.Equal(t, 100, tokens)
		assert.Equal(t, 10, toolTurns)
		assert.Equal(t, 40, historyTurns)
		assert.Equal(t, 1000, threshold)
	})
}

func TestConfigWatcher_SyncToStrategy(t *testing.T) {
	cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil).(*fileConfigWatcher)
	cw.contextWindow = 500000
	cw.tieredThreshold = 3000

	t.Run("ValidStrategy", func(t *testing.T) {
		cs := NewContextStrategy(NewHeuristicTokenCounter(nil))
		cw.SyncToStrategy(cs)
		assert.Equal(t, 500000, cs.contextWindow)
		assert.Equal(t, 3000, cs.GetTieredThreshold())
	})

	t.Run("NilStrategy", func(t *testing.T) {
		assert.NotPanics(t, func() {
			cw.SyncToStrategy(nil)
		})
	})
}

func TestConfigWatcher_GetContextWindow_Refresh(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
	cw.Loader = mockLoader

	// Default
	assert.Equal(t, 1000000, cw.contextWindow)

	// Specific model config
	yamlContent := `
MODELS:
  test-model:
    CONTEXT_WINDOW: 123456
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&config.Config{
		Models: map[string]config.ModelConfig{
			"test-model": {ContextWindow: 123456},
		},
	}, nil)

	cw.Refresh("test-model")
	assert.Equal(t, 123456, cw.contextWindow)
}

func TestConfigWatcher_SessionReadFileError(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session_dir")
	if err := os.Mkdir(sessionPath, 0755); err != nil {
		t.Fatal(err)
	}

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", sessionPath)

	// Should not panic and return early
	cw.Refresh("default")
	tokens, _, _, _ := cw.GetLimits()
	assert.Equal(t, 100, tokens)
}

func TestConfigWatcher_UpdateFromMain_NoChange(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `MAX_HISTORY_TOKENS: 500`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&config.Config{
		MaxHistoryTokens: 500,
	}, nil)

	cw.Refresh("model-a")
	tokens, _, _, _ := cw.GetLimits()
	assert.Equal(t, 500, tokens)

	// Refresh again with same model and no file change
	cw.Refresh("model-a")
}

func TestConfigWatcher_SetPaths(t *testing.T) {
	cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil).(*fileConfigWatcher)
	cw.SetPaths("main", "session")
	assert.Equal(t, "main", cw.mainPath)
	assert.Equal(t, "session", cw.sessionPath)
}

func TestConfigWatcher_SessionAllFields(t *testing.T) {
	fcw, _, sessionPath := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	content := `{"MAX_HISTORY_TOKENS": 500, "MAX_TURNS": 15, "MAX_HISTORY_TURNS": 25}`
	if err := os.WriteFile(sessionPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cw.Refresh("default")
	tokens, toolTurns, historyTurns, _ := cw.GetLimits()
	assert.Equal(t, 500, tokens)
	assert.Equal(t, 15, toolTurns)
	assert.Equal(t, 25, historyTurns)
}

func TestConfigWatcher_MalformedYAML(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
	cw.Loader = mockLoader

	if err := os.WriteFile(mainPath, []byte(`invalid: yaml: :`), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(nil, fmt.Errorf("malformed yaml"))

	cw.Refresh("default")
	// Should return early and keep defaults
	tokens, _, _, _ := cw.GetLimits()
	assert.Equal(t, 100, tokens)
}

func TestConfigWatcher_ModelConfigZeroContext(t *testing.T) {
	fcw, mainPath, _ := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	mockLoader := new(mockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MODELS:
  test-model:
    CONTEXT_WINDOW: 0
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&config.Config{
		Models: map[string]config.ModelConfig{
			"test-model": {ContextWindow: 0},
		},
	}, nil)

	cw.Refresh("test-model")
	assert.Equal(t, 1000000, cw.contextWindow)
}

func TestConfigWatcher_UpdateFromSession_NoChange(t *testing.T) {
	fcw, _, sessionPath := setupConfigWatcherTest(t)
	cw := fcw.(*fileConfigWatcher)
	if err := os.WriteFile(sessionPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	cw.Refresh("default")
	cw.Refresh("default")
}

func TestConfigWatcher_EmptyPaths(t *testing.T) {
	cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	cw.SetPaths("", "")
	cw.Refresh("default")
	tokens, _, _, _ := cw.GetLimits()
	assert.Equal(t, 100, tokens)
}
