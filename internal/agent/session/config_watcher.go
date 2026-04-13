// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"os"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// FileStat defines the interface for file status checks.
type FileStat interface {
	Stat(name string) (os.FileInfo, error)
}

type realFileStat struct{}

func (s realFileStat) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }

// ConfigWatcher defines the interface for monitoring configuration.
type ConfigWatcher interface {
	SetPaths(main, session string)
	Refresh(model string)
	SetLimits(tokens, toolTurns, historyTurns int)
	GetLimits() (tokens, toolTurns, historyTurns, threshold int)
	ApplyLimits(l events.Limits)
	SyncToStrategy(cs *ContextStrategy)
}

// fileConfigWatcher monitors configuration files for changes and caches values.
type FileConfigWatcher struct {
	mu                   sync.RWMutex
	Loader               config.ConfigLoader
	SessionLoader        config.SessionLoader
	FS                   FileStat
	logger               ports.Logger
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

// NewFileConfigWatcher creates a new fileConfigWatcher with default values.
func NewFileConfigWatcher(mainLoader config.ConfigLoader, sessionLoader config.SessionLoader, tokens, toolTurns, historyTurns int, logger ports.Logger) ConfigWatcher {
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
		Loader:               mainLoader,
		SessionLoader:        sessionLoader,
		FS:                   realFileStat{},
		logger:               logger,
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

	info, err := cw.FS.Stat(cw.mainPath)
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

	info, err := cw.FS.Stat(cw.sessionPath)
	if err != nil {
		return
	}

	if info.ModTime().After(cw.lastSessionMod) || forceUpdate {
		cw.lastSessionMod = info.ModTime()
		cw.loadSessionConfig()
	}
}

func (cw *FileConfigWatcher) loadSessionConfig() {
	if cw.SessionLoader == nil {
		return
	}
	sessCfg, err := cw.SessionLoader.LoadSession(cw.sessionPath)
	if err != nil {
		if !os.IsNotExist(err) && cw.logger != nil {
			cw.logger.Warn("Failed to load session config", "path", cw.sessionPath, "error", err)
		}
		return
	}
	if sessCfg == nil {
		return
	}
	if sessCfg.MaxHistoryTokens != nil {
		cw.maxHistoryTokens = *sessCfg.MaxHistoryTokens
	}
	if sessCfg.MaxToolTurns != nil {
		cw.maxToolTurns = *sessCfg.MaxToolTurns
	}
	if sessCfg.MaxHistoryTurns != nil {
		cw.maxHistoryTurns = *sessCfg.MaxHistoryTurns
	}
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
		cs.SetContextWindow(cw.contextWindow)
		cs.SetTieredThreshold(cw.tieredThreshold)
	}
}

// noOpConfigWatcher implements ConfigWatcher but performs no file operations.
type noOpConfigWatcher struct {
	mu               sync.RWMutex
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	tieredThreshold  int
	contextWindow    int
}

// NewNoOpConfigWatcher creates a new noOpConfigWatcher with default values.
func NewNoOpConfigWatcher(tokens, toolTurns, historyTurns int) ConfigWatcher {
	defaultThreshold := config.DefaultTieredThreshold
	if dp := config.DefaultPricing(); dp.Models != nil {
		if m, ok := dp.Models["default"]; ok {
			if m.TieredThreshold > 0 {
				defaultThreshold = int(m.TieredThreshold)
			}
		}
	}

	return &noOpConfigWatcher{
		maxHistoryTokens: tokens,
		maxToolTurns:     toolTurns,
		maxHistoryTurns:  historyTurns,
		tieredThreshold:  defaultThreshold,
		contextWindow:    1000000, // Matches defaultWindow in NewFileConfigWatcher
	}
}

// SetPaths is a no-op.
func (cw *noOpConfigWatcher) SetPaths(main, session string) {}

// Refresh is a no-op.
func (cw *noOpConfigWatcher) Refresh(model string) {}

// SetLimits updates the cached limits manually.
func (cw *noOpConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {
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
func (cw *noOpConfigWatcher) GetLimits() (tokens, toolTurns, historyTurns, threshold int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns, cw.tieredThreshold
}

// ApplyLimits updates the cached limits from an events.Limits struct.
func (cw *noOpConfigWatcher) ApplyLimits(l events.Limits) {
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
func (cw *noOpConfigWatcher) SyncToStrategy(cs *ContextStrategy) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	if cs != nil {
		cs.SetLimits(cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns)
		cs.SetContextWindow(cw.contextWindow)
		cs.SetTieredThreshold(cw.tieredThreshold)
	}
}

func (cw *FileConfigWatcher) GetContextWindow() int {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.contextWindow
}

func (cw *FileConfigWatcher) GetDefaultWindow() int {
	return cw.defaultWindow
}
