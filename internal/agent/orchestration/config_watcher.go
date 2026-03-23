// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// ConfigWatcher defines the interface for monitoring configuration.
type ConfigWatcher interface {
	SetPaths(main, session string)
	Refresh(model string)
	SetLimits(tokens, toolTurns, historyTurns int)
	GetLimits() (tokens, toolTurns, historyTurns, threshold int)
	ApplyLimits(l events.Limits)
	SyncToStrategy(cs *ContextStrategy)
}

// FileConfigWatcher monitors configuration files for changes and caches values.
type FileConfigWatcher struct {
	mu                   sync.RWMutex
	Loader               config.ConfigLoader
	mainPath             string
	sessionPath          string
	lastMainMod          time.Time
	lastSessionMod       time.Time
	lastModel            string
	maxHistoryTokens     int
	maxToolTurns         int
	maxHistoryTurns      int
	tieredThreshold      int
	contextWindow        int
	defaultHistoryTokens int
	defaultToolTurns     int
	defaultHistoryTurns  int
	defaultThreshold     int
	defaultWindow        int
}

// NewFileConfigWatcher creates a new FileConfigWatcher with default values.
func NewFileConfigWatcher(loader config.ConfigLoader, tokens, toolTurns, historyTurns int) *FileConfigWatcher {
	defaultThreshold := config.DefaultTieredThreshold
	defaultWindow := 1000000
	if dp := config.DefaultPricing(); dp.Models != nil {
		if m, ok := dp.Models["default"]; ok {
			if m.TieredThreshold > 0 {
				defaultThreshold = int(m.TieredThreshold)
			}
		}
	}

	return &FileConfigWatcher{
		Loader:               loader,
		maxHistoryTokens:     tokens,
		maxToolTurns:         toolTurns,
		maxHistoryTurns:      historyTurns,
		tieredThreshold:      defaultThreshold,
		contextWindow:        defaultWindow,
		defaultHistoryTokens: tokens,
		defaultToolTurns:     toolTurns,
		defaultHistoryTurns:  historyTurns,
		defaultThreshold:     defaultThreshold,
		defaultWindow:        defaultWindow,
	}
}

// SetPaths sets the configuration file paths.
func (cw *FileConfigWatcher) SetPaths(main, session string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.mainPath = main
	cw.sessionPath = session
}

// Refresh checks for file changes and updates cached values if necessary.
func (cw *FileConfigWatcher) Refresh(model string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	changed := cw.updateFromMain(model)
	cw.updateFromSession(changed)
}

func (cw *FileConfigWatcher) updateFromMain(model string) bool {
	if cw.mainPath == "" {
		return false
	}

	info, err := os.Stat(cw.mainPath)
	if err != nil {
		return false
	}

	if !info.ModTime().After(cw.lastMainMod) && model == cw.lastModel {
		return false
	}

	if cw.Loader == nil {
		return false
	}

	cfg, err := cw.Loader.Load(cw.mainPath)
	if err != nil {
		return false
	}

	cw.lastMainMod = info.ModTime()
	cw.lastModel = model

	cw.maxHistoryTokens = cfg.MaxHistoryTokens
	cw.maxToolTurns = cfg.MaxToolTurns
	cw.maxHistoryTurns = cfg.MaxHistoryTurns

	// Update context window from model config if available, otherwise reset to default
	cw.contextWindow = cw.defaultWindow
	if mCfg, ok := cfg.Models[model]; ok && mCfg.ContextWindow > 0 {
		cw.contextWindow = mCfg.ContextWindow
	}

	return true
}

func (cw *FileConfigWatcher) updateFromSession(forceUpdate bool) {
	if cw.sessionPath == "" {
		return
	}

	info, err := os.Stat(cw.sessionPath)
	if err != nil {
		return
	}

	if info.ModTime().After(cw.lastSessionMod) || forceUpdate {
		cw.lastSessionMod = info.ModTime()
		cw.loadSessionConfig()
	}
}

func (cw *FileConfigWatcher) loadSessionConfig() {
	data, err := os.ReadFile(cw.sessionPath)
	if err != nil {
		return
	}

	var pCfg map[string]interface{}
	if err := json.Unmarshal(data, &pCfg); err != nil {
		return
	}

	if val, ok := pCfg["MAX_HISTORY_TOKENS"]; ok {
		cw.maxHistoryTokens = toInt(val, cw.maxHistoryTokens)
	}
	if val, ok := pCfg["MAX_TURNS"]; ok {
		cw.maxToolTurns = toInt(val, cw.maxToolTurns)
	} else if val, ok := pCfg["MAX_TOOL_TURNS"]; ok {
		cw.maxToolTurns = toInt(val, cw.maxToolTurns)
	}
	if val, ok := pCfg["MAX_HISTORY_TURNS"]; ok {
		cw.maxHistoryTurns = toInt(val, cw.maxHistoryTurns)
	}
}

func toInt(val interface{}, defaultVal int) int {
	switch v := val.(type) {
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return i
		}
	}
	return defaultVal
}

// SetLimits updates the cached limits manually.
func (cw *FileConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if tokens > 0 {
		cw.maxHistoryTokens = tokens
	}
	if toolTurns > 0 {
		cw.maxToolTurns = toolTurns
	}
	if historyTurns > 0 {
		cw.maxHistoryTurns = historyTurns
	}
}

// GetLimits returns the current cached limits.
func (cw *FileConfigWatcher) GetLimits() (tokens, toolTurns, historyTurns, threshold int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns, cw.tieredThreshold
}

// ApplyLimits updates the cached limits from an events.Limits struct.
func (cw *FileConfigWatcher) ApplyLimits(l events.Limits) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if l.MaxHistoryTokens > 0 {
		cw.maxHistoryTokens = l.MaxHistoryTokens
	}
	if l.MaxToolTurns > 0 {
		cw.maxToolTurns = l.MaxToolTurns
	}
	if l.MaxHistoryTurns > 0 {
		cw.maxHistoryTurns = l.MaxHistoryTurns
	}
	if l.TieredThreshold > 0 {
		cw.tieredThreshold = l.TieredThreshold
	}
}

// SyncToStrategy synchronizes the current watcher state to a ContextStrategy.
func (cw *FileConfigWatcher) SyncToStrategy(cs *ContextStrategy) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	if cs != nil {
		cs.SetLimits(cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns)
		cs.setContextWindow(cw.contextWindow)
		cs.setTieredThreshold(cw.tieredThreshold)
	}
}

// NoOpConfigWatcher implements ConfigWatcher but performs no file operations.
type NoOpConfigWatcher struct {
	mu               sync.RWMutex
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	tieredThreshold  int
	contextWindow    int
}

// NewNoOpConfigWatcher creates a new NoOpConfigWatcher with default values.
func NewNoOpConfigWatcher(tokens, toolTurns, historyTurns int) *NoOpConfigWatcher {
	defaultThreshold := config.DefaultTieredThreshold
	if dp := config.DefaultPricing(); dp.Models != nil {
		if m, ok := dp.Models["default"]; ok {
			if m.TieredThreshold > 0 {
				defaultThreshold = int(m.TieredThreshold)
			}
		}
	}

	return &NoOpConfigWatcher{
		maxHistoryTokens: tokens,
		maxToolTurns:     toolTurns,
		maxHistoryTurns:  historyTurns,
		tieredThreshold:  defaultThreshold,
		contextWindow:    1000000, // Matches defaultWindow in NewFileConfigWatcher
	}
}

// SetPaths is a no-op.
func (cw *NoOpConfigWatcher) SetPaths(main, session string) {}

// Refresh is a no-op.
func (cw *NoOpConfigWatcher) Refresh(model string) {}

// SetLimits updates the cached limits manually.
func (cw *NoOpConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if tokens > 0 {
		cw.maxHistoryTokens = tokens
	}
	if toolTurns > 0 {
		cw.maxToolTurns = toolTurns
	}
	if historyTurns > 0 {
		cw.maxHistoryTurns = historyTurns
	}
}

// GetLimits returns the current cached limits.
func (cw *NoOpConfigWatcher) GetLimits() (tokens, toolTurns, historyTurns, threshold int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns, cw.tieredThreshold
}

// ApplyLimits updates the cached limits from an events.Limits struct.
func (cw *NoOpConfigWatcher) ApplyLimits(l events.Limits) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if l.MaxHistoryTokens > 0 {
		cw.maxHistoryTokens = l.MaxHistoryTokens
	}
	if l.MaxToolTurns > 0 {
		cw.maxToolTurns = l.MaxToolTurns
	}
	if l.MaxHistoryTurns > 0 {
		cw.maxHistoryTurns = l.MaxHistoryTurns
	}
	if l.TieredThreshold > 0 {
		cw.tieredThreshold = l.TieredThreshold
	}
}

// SyncToStrategy synchronizes the current watcher state to a ContextStrategy.
func (cw *NoOpConfigWatcher) SyncToStrategy(cs *ContextStrategy) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	if cs != nil {
		cs.SetLimits(cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns)
		cs.setContextWindow(cw.contextWindow)
		cs.setTieredThreshold(cw.tieredThreshold)
	}
}
