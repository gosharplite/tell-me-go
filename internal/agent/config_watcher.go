// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/config"
)

// ConfigWatcher monitors configuration files for changes and caches values.
type ConfigWatcher struct {
	mu                   sync.RWMutex
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

// NewConfigWatcher creates a new ConfigWatcher with default values.
func NewConfigWatcher(tokens, toolTurns, historyTurns int) *ConfigWatcher {
	defaultThreshold := config.DefaultTieredThreshold
	defaultWindow := 1000000
	if dp := config.DefaultPricing(); dp.Models != nil {
		if m, ok := dp.Models["default"]; ok {
			if m.TieredThreshold > 0 {
				defaultThreshold = int(m.TieredThreshold)
			}
		}
	}

	return &ConfigWatcher{
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
func (cw *ConfigWatcher) SetPaths(main, session string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.mainPath = main
	cw.sessionPath = session
}

// Refresh checks for file changes and updates cached values if necessary.
func (cw *ConfigWatcher) Refresh(model string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	changed := cw.updateFromMain(model)
	cw.updateFromSession(changed)
}

func (cw *ConfigWatcher) updateFromMain(model string) bool {
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

	cfg, err := config.Load(cw.mainPath)
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

func (cw *ConfigWatcher) updateFromSession(forceUpdate bool) {
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

func (cw *ConfigWatcher) loadSessionConfig() {
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
	if val, ok := pCfg["MAX_TOOL_TURNS"]; ok {
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
func (cw *ConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {
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

func (cw *ConfigWatcher) SetTieredThreshold(threshold int) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if threshold > 0 {
		cw.tieredThreshold = threshold
	}
}

// GetLimits returns the current cached limits.
func (cw *ConfigWatcher) GetLimits() (tokens, toolTurns, historyTurns int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns
}

func (cw *ConfigWatcher) GetTieredThreshold() int {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.tieredThreshold
}

func (cw *ConfigWatcher) GetContextWindow() int {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.contextWindow
}
