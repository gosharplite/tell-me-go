// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
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

// stubFileInfo implements os.FileInfo for testing.
type stubFileInfo struct{ modTime time.Time }

func (s stubFileInfo) Name() string       { return "stub" }
func (s stubFileInfo) Size() int64        { return 0 }
func (s stubFileInfo) Mode() os.FileMode  { return 0 }
func (s stubFileInfo) ModTime() time.Time { return s.modTime }
func (s stubFileInfo) IsDir() bool        { return false }
func (s stubFileInfo) Sys() interface{}   { return nil }

// stubFileStat implements session.FileStat for testing.
type stubFileStat struct {
	statErr error
	modTime time.Time
}

func (s stubFileStat) Stat(name string) (os.FileInfo, error) {
	if s.statErr != nil {
		return nil, s.statErr
	}
	return stubFileInfo{modTime: s.modTime}, nil
}

// stubConfigLoader implements config.ConfigLoader for testing.
type stubConfigLoader struct {
	loadErr error
	cfg     *config.Config
}

func (l stubConfigLoader) Load(path string) (*config.Config, error) {
	if l.loadErr != nil {
		return nil, l.loadErr
	}
	if l.cfg != nil {
		return l.cfg, nil
	}
	return &config.Config{
		MaxHistoryTokens: 10000,
		MaxToolTurns:     20,
		MaxHistoryTurns:  50,
	}, nil
}

func TestFileConfigWatcher_Refresh_LoadError(t *testing.T) {
	// Setup: Create FileConfigWatcher with a future mod time (triggers Stat) and a loader that errors.
	fcw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	cw := fcw.(*session.FileConfigWatcher)

	cw.FS = stubFileStat{modTime: time.Now().Add(time.Hour)}
	cw.Loader = stubConfigLoader{loadErr: errors.New("parse error")}
	cw.SetPaths("/fake/main.yaml", "")

	// Action: Refresh should call Load, get error, keep original limits.
	cw.Refresh("gpt-5")

	// Assert: Limits unchanged from defaults.
	tokens, toolTurns, historyTurns := cw.GetLimits()
	assert.Equal(t, 100, tokens, "tokens should remain at default")
	assert.Equal(t, 10, toolTurns, "toolTurns should remain at default")
	assert.Equal(t, 20, historyTurns, "historyTurns should remain at default")
}

func TestFileConfigWatcher_Refresh_StatError(t *testing.T) {
	// Setup: Create FileConfigWatcher with Stat returning os.ErrNotExist.
	fcw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	cw := fcw.(*session.FileConfigWatcher)

	cw.FS = stubFileStat{statErr: os.ErrNotExist}
	cw.SetPaths("/fake/main.yaml", "")

	// Action: Refresh should call Stat, get error, return false immediately (no Load call).
	cw.Refresh("gpt-5")

	// Assert: Limits unchanged from defaults.
	tokens, toolTurns, historyTurns := cw.GetLimits()
	assert.Equal(t, 100, tokens, "tokens should remain at default")
	assert.Equal(t, 10, toolTurns, "toolTurns should remain at default")
	assert.Equal(t, 20, historyTurns, "historyTurns should remain at default")
}

func TestConfigWatcher_LoadSessionConfig_ReadError(t *testing.T) {
	// Tests the Warn log path in loadSessionConfig when LoadSession returns
	// a non-IsNotExist error AND the logger is non-nil.
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	// Create a file that exists (so FS.Stat succeeds) but will cause LoadSession to fail.
	if err := os.WriteFile(sessionPath, []byte(`valid json but loader errors`), 0644); err != nil {
		t.Fatal(err)
	}

	var buf testfixtures.SyncWriter
	testLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cw := session.NewFileConfigWatcher(nil, &errorSessionLoader{err: errors.New("permission denied")}, 100, 10, 20, testLogger)
	fcw := cw.(*session.FileConfigWatcher)

	// Override FS to return a future mod time so updateFromSession triggers loadSessionConfig.
	fcw.FS = stubFileStat{modTime: time.Now().Add(time.Hour)}
	fcw.SetPaths("", sessionPath)

	// Trigger the load
	fcw.Refresh("default")

	// Assert the Warn was logged
	output := buf.String()
	assert.Contains(t, output, "Failed to load session config")
	assert.Contains(t, output, "permission denied")

	// Limits should remain at defaults
	tokens, _, _ := cw.GetLimits()
	assert.Equal(t, 100, tokens)
}

func TestConfigWatcher_LoadSessionConfig_NilLogger(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	// Create a readable file — Stat will succeed.
	if err := os.WriteFile(sessionPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// logger=nil intentionally — this is the branch under test.
	cw := session.NewFileConfigWatcher(nil, &errorSessionLoader{err: errors.New("permission denied")}, 100, 10, 20, nil)
	fcw := cw.(*session.FileConfigWatcher)

	// Ensure Stat returns a future mod time so updateFromSession triggers loadSessionConfig.
	fcw.FS = stubFileStat{modTime: time.Now().Add(time.Hour)}
	fcw.SetPaths("", sessionPath)

	// Must not panic.
	fcw.Refresh("default")

	// Limits must remain at constructor defaults.
	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 || toolTurns != 10 || historyTurns != 20 {
		t.Errorf("limits changed unexpectedly: got (%d, %d, %d), want (100, 10, 20)", tokens, toolTurns, historyTurns)
	}
}

func TestConfigWatcher_LoadSessionConfig_NilSessionLoader(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	if err := os.WriteFile(sessionPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// sessionLoader=nil
	cw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	fcw := cw.(*session.FileConfigWatcher)

	fcw.FS = stubFileStat{modTime: time.Now().Add(time.Hour)}
	fcw.SetPaths("", sessionPath)

	// Must not panic.
	fcw.Refresh("default")

	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 || toolTurns != 10 || historyTurns != 20 {
		t.Errorf("limits changed unexpectedly: got (%d, %d, %d), want (100, 10, 20)", tokens, toolTurns, historyTurns)
	}
}

func TestConfigWatcher_LoadSessionConfig_NilSessionConfig(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	if err := os.WriteFile(sessionPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// nilSessionLoader returns (nil, nil) — session config is absent
	cw := session.NewFileConfigWatcher(nil, &nilSessionLoader{}, 100, 10, 20, nil)
	fcw := cw.(*session.FileConfigWatcher)

	fcw.FS = stubFileStat{modTime: time.Now().Add(time.Hour)}
	fcw.SetPaths("", sessionPath)

	fcw.Refresh("default")

	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 || toolTurns != 10 || historyTurns != 20 {
		t.Errorf("limits changed unexpectedly: got (%d, %d, %d), want (100, 10, 20)", tokens, toolTurns, historyTurns)
	}
}

func TestConfigWatcher_LoadSessionConfig_MissingFields(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	// Valid JSON but no recognized limit keys — all fields will be nil.
	if err := os.WriteFile(sessionPath, []byte(`{"unrelated": "value"}`), 0644); err != nil {
		t.Fatal(err)
	}

	cw := session.NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	fcw := cw.(*session.FileConfigWatcher)

	fcw.FS = stubFileStat{modTime: time.Now().Add(time.Hour)}
	fcw.SetPaths("", sessionPath)

	fcw.Refresh("default")

	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("MaxHistoryTokens should remain at default 100, got %d", tokens)
	}
	if toolTurns != 10 {
		t.Errorf("MaxToolTurns should remain at default 10, got %d", toolTurns)
	}
	if historyTurns != 20 {
		t.Errorf("MaxHistoryTurns should remain at default 20, got %d", historyTurns)
	}
}

func TestConfigWatcher_LoadSessionConfig_AllFields(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	// Session config with all three limit keys set.
	if err := os.WriteFile(sessionPath, []byte(`{"MAX_HISTORY_TOKENS": 500, "MAX_TURNS": 30, "MAX_HISTORY_TURNS": 40}`), 0644); err != nil {
		t.Fatal(err)
	}

	cw := session.NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	fcw := cw.(*session.FileConfigWatcher)

	fcw.FS = stubFileStat{modTime: time.Now().Add(time.Hour)}
	fcw.SetPaths("", sessionPath)

	fcw.Refresh("default")

	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 500 {
		t.Errorf("MaxHistoryTokens = %d, want 500", tokens)
	}
	if toolTurns != 30 {
		t.Errorf("MaxToolTurns = %d, want 30", toolTurns)
	}
	if historyTurns != 40 {
		t.Errorf("MaxHistoryTurns = %d, want 40", historyTurns)
	}
}

func TestFileConfigWatcher_SetLimits(t *testing.T) {
	tests := []struct {
		name                       string
		tokens, toolTurns, histTurns int
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", 200, 5, 10, 200, 5, 10},
		{"zero tokens no-op", 0, 5, 10, 100, 5, 10},
		{"negative tokens no-op", -1, 5, 10, 100, 5, 10},
		{"mixed zero/positive", 200, 0, 10, 200, 10, 10},
		{"all zero no-op", 0, 0, 0, 100, 10, 20},
		{"partial update", 0, 0, 50, 100, 10, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
			fcw := cw.(*session.FileConfigWatcher)

			fcw.SetLimits(tt.tokens, tt.toolTurns, tt.histTurns)
			tokens, toolTurns, histTurns := cw.GetLimits()

			if tokens != tt.wantTokens || toolTurns != tt.wantToolTurns || histTurns != tt.wantHistTurns {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)",
					tokens, toolTurns, histTurns,
					tt.wantTokens, tt.wantToolTurns, tt.wantHistTurns)
			}
		})
	}
}

func TestFileConfigWatcher_ApplyLimits(t *testing.T) {
	tests := []struct {
		name                       string
		limits                     events.Limits
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", events.Limits{MaxHistoryTokens: 200, MaxToolTurns: 5, MaxHistoryTurns: 10}, 200, 5, 10},
		{"zero tokens no-op", events.Limits{MaxHistoryTokens: 0, MaxToolTurns: 5, MaxHistoryTurns: 10}, 100, 5, 10},
		{"negative tokens no-op", events.Limits{MaxHistoryTokens: -1, MaxToolTurns: 5, MaxHistoryTurns: 10}, 100, 5, 10},
		{"zero-value Limits", events.Limits{}, 100, 10, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
			fcw := cw.(*session.FileConfigWatcher)

			fcw.ApplyLimits(tt.limits)
			tokens, toolTurns, histTurns := cw.GetLimits()

			if tokens != tt.wantTokens || toolTurns != tt.wantToolTurns || histTurns != tt.wantHistTurns {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)",
					tokens, toolTurns, histTurns,
					tt.wantTokens, tt.wantToolTurns, tt.wantHistTurns)
			}
		})
	}
}

func TestFileConfigWatcher_SyncToStrategy(t *testing.T) {
	t.Run("pushes limits", func(t *testing.T) {
		cw := session.NewFileConfigWatcher(nil, nil, 500, 10, 20, nil)
		fcw := cw.(*session.FileConfigWatcher)

		fcw.SetLimits(500, 10, 20)
		strategy := sessctx.NewStrategy(nil)
		fcw.SyncToStrategy(strategy)

		if strategy.GetMaxHistoryTokens() != 500 {
			t.Errorf("expected 500, got %d", strategy.GetMaxHistoryTokens())
		}
		if strategy.GetMaxToolTurns() != 10 {
			t.Errorf("expected 10, got %d", strategy.GetMaxToolTurns())
		}
	})

	t.Run("pushes context window", func(t *testing.T) {
		cw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
		fcw := cw.(*session.FileConfigWatcher)

		strategy := sessctx.NewStrategy(nil)
		fcw.SyncToStrategy(strategy)

		if strategy.GetContextWindow() != 1000000 {
			t.Errorf("expected 1000000, got %d", strategy.GetContextWindow())
		}
	})

	t.Run("nil strategy no-op", func(t *testing.T) {
		cw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
		fcw := cw.(*session.FileConfigWatcher)

		// Must not panic
		fcw.SyncToStrategy(nil)
	})
}

func TestFileConfigWatcher_SyncToStrategy_Divergence(t *testing.T) {
	cw := session.NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	fcw := cw.(*session.FileConfigWatcher)

	strategy := sessctx.NewStrategy(nil)

	// Sync initial values
	fcw.SyncToStrategy(strategy)
	if strategy.GetMaxHistoryTokens() != 100 {
		t.Fatalf("initial sync: expected 100, got %d", strategy.GetMaxHistoryTokens())
	}

	// Change watcher WITHOUT syncing
	fcw.SetLimits(999, 50, 60)

	// Strategy must still reflect OLD values — proof of divergence
	if strategy.GetMaxHistoryTokens() != 100 {
		t.Errorf("strategy should retain old value 100 before re-sync, got %d", strategy.GetMaxHistoryTokens())
	}

	// Re-sync — strategy must now reflect NEW values
	fcw.SyncToStrategy(strategy)
	if strategy.GetMaxHistoryTokens() != 999 {
		t.Errorf("after re-sync: expected 999, got %d", strategy.GetMaxHistoryTokens())
	}
	if strategy.GetMaxToolTurns() != 50 {
		t.Errorf("after re-sync: expected 50, got %d", strategy.GetMaxToolTurns())
	}
}

// errorSessionLoader returns a fixed error from LoadSession.
type errorSessionLoader struct {
	err error
}

func (l *errorSessionLoader) LoadSession(path string) (*config.SessionConfig, error) {
	return nil, l.err
}

// nilSessionLoader returns (nil, nil) to simulate a missing session config.
type nilSessionLoader struct{}

func (l *nilSessionLoader) LoadSession(path string) (*config.SessionConfig, error) {
	return nil, nil
}
