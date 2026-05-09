// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

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
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
)

type testSessionLoader struct{}

func (l *testSessionLoader) LoadSession(path string) (*domain_config.SessionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pCfg map[string]interface{}
	if err := json.Unmarshal(data, &pCfg); err != nil {
		return nil, err
	}
	cfg := &domain_config.SessionConfig{}
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

// sleepingFileStat simulates slow disk I/O by sleeping before returning.
type sleepingFileStat struct {
	delay   time.Duration
	modTime time.Time
}

func (s sleepingFileStat) Stat(name string) (os.FileInfo, error) {
	time.Sleep(s.delay)
	return stubFileInfo{modTime: s.modTime}, nil
}

// sleepingConfigLoader simulates slow config loading.
type sleepingConfigLoader struct {
	delay time.Duration
}

func (l sleepingConfigLoader) Load(path string) (*domain_config.Config, error) {
	time.Sleep(l.delay)
	return &domain_config.Config{
		MaxHistoryTokens: 500,
		MaxToolTurns:     10,
		MaxHistoryTurns:  20,
	}, nil
}

// TestFileConfigWatcher_ConcurrentRefreshAndRead proves that GetLimits
// is NOT blocked by Refresh's disk I/O. A slow FS (200ms Stat) must not
// delay concurrent readers beyond ~50ms. This test FAILS on the old
// lock-across-I/O design and PASSES after the three-phase refactor.
func TestFileConfigWatcher_ConcurrentRefreshAndRead(t *testing.T) {
	fcw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	cw := fcw.(*FileConfigWatcher)

	// Simulate slow disk: 200ms Stat + 100ms Load = 300ms total I/O.
	futureTime := time.Now().Add(time.Hour)
	cw.FS = sleepingFileStat{delay: 200 * time.Millisecond, modTime: futureTime}
	cw.Loader = sleepingConfigLoader{delay: 100 * time.Millisecond}
	cw.SetPaths("/fake/main.yaml", "")

	// Barrier to synchronize goroutines.
	start := make(chan struct{})

	// Spawn Refresh in a goroutine. It will spend ~300ms in Phase 2 I/O.
	go func() {
		<-start
		cw.Refresh("gpt-5")
	}()

	// Let Refresh enter Phase 2 I/O, then measure GetLimits latency.
	close(start)
	time.Sleep(50 * time.Millisecond) // Refresh is now mid-I/O.

	// Measure: GetLimits must NOT block behind Refresh's disk I/O.
	before := time.Now()
	cw.GetLimits()
	elapsed := time.Since(before)

	if elapsed >= 100*time.Millisecond {
		t.Fatalf("GetLimits blocked for %v, expected < 100ms (Refresh I/O takes 300ms)", elapsed)
	}
}

func TestConfigWatcher_Refresh(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "session.json")

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
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

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
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
	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	cw.SetPaths("", "non-existent.json")

	// Should not panic
	cw.Refresh("default")
	tokens, _, _ := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("expected 100 tokens, got %d", tokens)
	}
}

func setupConfigWatcherTest(t *testing.T) (domain_config.ConfigWatcher, string, string) {
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
	cw := fcw.(*FileConfigWatcher)
	mockLoader := new(agenttest.MockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MAX_HISTORY_TOKENS: 500
MAX_TURNS: 5
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&domain_config.Config{
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
	cw := fcw.(*FileConfigWatcher)
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

	mockLoader.On("Load", mainPath).Return(&domain_config.Config{
		Models: map[string]domain_config.ModelConfig{
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
	cw := fcw.(*FileConfigWatcher)
	mockLoader := new(agenttest.MockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MAX_HISTORY_TOKENS: 500
MAX_TURNS: 5
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&domain_config.Config{
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
	cw := fcw.(*FileConfigWatcher)
	mockLoader := new(agenttest.MockConfigLoader)
	cw.Loader = mockLoader

	yamlContent := `
MAX_HISTORY_TOKENS: 500
MAX_TURNS: 5
`
	if err := os.WriteFile(mainPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	mockLoader.On("Load", mainPath).Return(&domain_config.Config{
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

// stubConfigLoader implements domain_config.ConfigLoader for testing.
type stubConfigLoader struct {
	loadErr error
	cfg     *domain_config.Config
}

func (l stubConfigLoader) Load(path string) (*domain_config.Config, error) {
	if l.loadErr != nil {
		return nil, l.loadErr
	}
	if l.cfg != nil {
		return l.cfg, nil
	}
	return &domain_config.Config{
		MaxHistoryTokens: 10000,
		MaxToolTurns:     20,
		MaxHistoryTurns:  50,
	}, nil
}

func TestFileConfigWatcher_Refresh_LoadError(t *testing.T) {
	// Setup: Create FileConfigWatcher with a future mod time (triggers Stat) and a loader that errors.
	fcw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	cw := fcw.(*FileConfigWatcher)

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
	fcw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	cw := fcw.(*FileConfigWatcher)

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

	cw := NewFileConfigWatcher(nil, &errorSessionLoader{err: errors.New("permission denied")}, 100, 10, 20, testLogger)
	fcw := cw.(*FileConfigWatcher)

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
	cw := NewFileConfigWatcher(nil, &errorSessionLoader{err: errors.New("permission denied")}, 100, 10, 20, nil)
	fcw := cw.(*FileConfigWatcher)

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
	cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
	fcw := cw.(*FileConfigWatcher)

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
	cw := NewFileConfigWatcher(nil, &nilSessionLoader{}, 100, 10, 20, nil)
	fcw := cw.(*FileConfigWatcher)

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

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	fcw := cw.(*FileConfigWatcher)

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

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	fcw := cw.(*FileConfigWatcher)

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
		name                                     string
		tokens, toolTurns, histTurns             int
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", 200, 5, 10, 200, 5, 10},
		{"zero tokens accepted", 0, 5, 10, 0, 5, 10},
		{"negative tokens ignored", -1, 5, 10, 100, 5, 10},
		{"mixed zero/positive", 200, 0, 10, 200, 0, 10},
		{"all zero accepted", 0, 0, 0, 0, 0, 0},
		{"partial update", 0, 0, 50, 0, 0, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
			fcw := cw.(*FileConfigWatcher)

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
		name                                     string
		limits                                   events.Limits
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", events.Limits{MaxHistoryTokens: 200, MaxToolTurns: 5, MaxHistoryTurns: 10}, 200, 5, 10},
		{"zero tokens accepted", events.Limits{MaxHistoryTokens: 0, MaxToolTurns: 5, MaxHistoryTurns: 10}, 0, 5, 10},
		{"negative tokens ignored", events.Limits{MaxHistoryTokens: -1, MaxToolTurns: 5, MaxHistoryTurns: 10}, 100, 5, 10},
		{"zero-value Limits accepted", events.Limits{}, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := NewFileConfigWatcher(nil, nil, 100, 10, 20, nil)
			fcw := cw.(*FileConfigWatcher)

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

func TestFileConfigWatcher_ForceUpdateSession(t *testing.T) {
	tmpDir := t.TempDir()
	mainPath := filepath.Join(tmpDir, "main.yaml")
	sessionPath := filepath.Join(tmpDir, "session.json")

	// Create a session file with initial limits.
	if err := os.WriteFile(sessionPath, []byte(`{"MAX_HISTORY_TOKENS": 500}`), 0644); err != nil {
		t.Fatal(err)
	}

	cw := NewFileConfigWatcher(nil, &testSessionLoader{}, 100, 10, 20, nil)
	fcw := cw.(*FileConfigWatcher)

	// FS returns a fixed PAST modTime for both main and session files.
	pastTime := time.Now().Add(-1 * time.Hour)
	fcw.FS = stubFileStat{modTime: pastTime}
	fcw.Loader = stubConfigLoader{} // returns default config — triggers changed=true on first call
	fcw.SetPaths(mainPath, sessionPath)

	// --- First Refresh ---
	// updateFromMain: pastTime > zero-value → true → changed=true
	// updateFromSession(true): forceUpdate=true bypasses mtime check → loads session config
	fcw.Refresh("gpt-5")

	tokens, _, _ := fcw.GetLimits()
	if tokens != 500 {
		t.Fatalf("first refresh (forceUpdate): expected tokens=500 from session config, got %d", tokens)
	}

	// --- Modify session file WITHOUT advancing mtime ---
	// FS still returns the same pastTime. The mtime check: pastTime.After(pastTime) = false.
	if err := os.WriteFile(sessionPath, []byte(`{"MAX_HISTORY_TOKENS": 999}`), 0644); err != nil {
		t.Fatal(err)
	}

	// --- Second Refresh ---
	// updateFromMain: pastTime.After(pastTime)=false AND model=="gpt-5"==lastModel → changed=false
	// updateFromSession(false): pastTime.After(pastTime)=false, forceUpdate=false → does NOT reload
	fcw.Refresh("gpt-5")

	tokens, _, _ = fcw.GetLimits()
	if tokens != 500 {
		t.Errorf("second refresh (no forceUpdate): expected tokens=500 (old value preserved by mtime gate), got %d", tokens)
	}

	// --- Third Refresh with different model ---
	// updateFromMain: model changed → changed=true
	// updateFromSession(true): forceUpdate=true bypasses mtime check → loads updated session config
	fcw.Refresh("gpt-4")

	tokens, _, _ = fcw.GetLimits()
	if tokens != 999 {
		t.Errorf("third refresh (forceUpdate via model switch): expected tokens=999 from updated session config, got %d", tokens)
	}
}

func TestNoOpConfigWatcher_ConstructorAndGetLimits(t *testing.T) {
	cw := NewNoOpConfigWatcher(100, 10, 20)

	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("tokens = %d, want 100", tokens)
	}
	if toolTurns != 10 {
		t.Errorf("toolTurns = %d, want 10", toolTurns)
	}
	if historyTurns != 20 {
		t.Errorf("historyTurns = %d, want 20", historyTurns)
	}
}

func TestNoOpConfigWatcher_SetPathsAndRefresh(t *testing.T) {
	cw := NewNoOpConfigWatcher(100, 10, 20)

	// SetPaths should not panic
	cw.SetPaths("/some/main.yaml", "/some/session.json")

	// Refresh should not panic
	cw.Refresh("gpt-5")

	// Limits must be unchanged
	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 || toolTurns != 10 || historyTurns != 20 {
		t.Errorf("limits changed after no-ops: got (%d, %d, %d), want (100, 10, 20)", tokens, toolTurns, historyTurns)
	}
}

func TestNoOpConfigWatcher_SetLimits(t *testing.T) {
	tests := []struct {
		name                                     string
		tokens, toolTurns, histTurns             int
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", 200, 5, 10, 200, 5, 10},
		{"zero tokens accepted", 0, 5, 10, 0, 5, 10},
		{"negative tokens ignored", -1, 5, 10, 100, 5, 10},
		{"mixed zero/positive", 200, 0, 10, 200, 0, 10},
		{"all zero accepted", 0, 0, 0, 0, 0, 0},
		{"partial update", 0, 0, 50, 0, 0, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := NewNoOpConfigWatcher(100, 10, 20)

			cw.SetLimits(tt.tokens, tt.toolTurns, tt.histTurns)
			tokens, toolTurns, histTurns := cw.GetLimits()

			if tokens != tt.wantTokens || toolTurns != tt.wantToolTurns || histTurns != tt.wantHistTurns {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)",
					tokens, toolTurns, histTurns,
					tt.wantTokens, tt.wantToolTurns, tt.wantHistTurns)
			}
		})
	}
}

func TestNoOpConfigWatcher_ApplyLimits(t *testing.T) {
	tests := []struct {
		name                                     string
		limits                                   events.Limits
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", events.Limits{MaxHistoryTokens: 200, MaxToolTurns: 5, MaxHistoryTurns: 10}, 200, 5, 10},
		{"zero tokens accepted", events.Limits{MaxHistoryTokens: 0, MaxToolTurns: 5, MaxHistoryTurns: 10}, 0, 5, 10},
		{"negative tokens ignored", events.Limits{MaxHistoryTokens: -1, MaxToolTurns: 5, MaxHistoryTurns: 10}, 100, 5, 10},
		{"zero-value Limits accepted", events.Limits{}, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := NewNoOpConfigWatcher(100, 10, 20)

			cw.ApplyLimits(tt.limits)
			tokens, toolTurns, histTurns := cw.GetLimits()

			if tokens != tt.wantTokens || toolTurns != tt.wantToolTurns || histTurns != tt.wantHistTurns {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)",
					tokens, toolTurns, histTurns,
					tt.wantTokens, tt.wantToolTurns, tt.wantHistTurns)
			}
		})
	}
}

func TestNoOpConfigWatcher_RaceDetector(t *testing.T) {
	cw := NewNoOpConfigWatcher(100, 10, 20)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(v int) {
			for j := 0; j < 100; j++ {
				cw.SetLimits(v, v, v)
				_, _, _ = cw.GetLimits()
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// errorSessionLoader returns a fixed error from LoadSession.
type errorSessionLoader struct {
	err error
}

func (l *errorSessionLoader) LoadSession(path string) (*domain_config.SessionConfig, error) {
	return nil, l.err
}

// nilSessionLoader returns (nil, nil) to simulate a missing session config.
type nilSessionLoader struct{}

func (l *nilSessionLoader) LoadSession(path string) (*domain_config.SessionConfig, error) {
	return nil, nil
}
