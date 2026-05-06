// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"os"
	"sync"
	"time"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
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

// setLimitsLocked applies non-negative limit values to the target fields.
// It is a stateless helper; the caller must hold the appropriate mutex.
func setLimitsLocked(tokens, toolTurns, historyTurns int, maxHistoryTokens, maxToolTurns, maxHistoryTurns *int) {
	if tokens >= 0 {
		*maxHistoryTokens = tokens
	}
	if toolTurns >= 0 {
		*maxToolTurns = toolTurns
	}
	if historyTurns >= 0 {
		*maxHistoryTurns = historyTurns
	}
}

// ConfigWatcher defines the interface for monitoring configuration.
type ConfigWatcher interface {
	SetPaths(main, session string)

	// Refresh re-reads configuration from the underlying source (file, env,
	// or in-memory store) using modelHint to disambiguate model-specific
	// overrides. It is invoked by (*agent).applyConfig before the fallible
	// delegate chain runs.
	//
	// Refresh is intentionally void per ADR-029 §5: it implements best-effort
	// reload semantics. A failed refresh leaves the watcher's prior state
	// intact, which is acceptable because the next chat turn will retry
	// (idempotent reload). Promoting Refresh to fallible would force every
	// caller to decide between "abort the chat" and "log and continue" —
	// a policy choice the ADR explicitly defers.
	//
	// Do NOT change this signature to return error without first amending
	// ADR-029. The fail-fast delegate chain in (*agent).applyConfig is
	// scoped to SafePublish, Engine.Reconfigure, and Manager.Reconfigure
	// only; expanding the chain is a non-trivial architectural decision.
	Refresh(model string)
	SetLimits(tokens, toolTurns, historyTurns int)
	GetLimits() (tokens, toolTurns, historyTurns int)
	ApplyLimits(l events.Limits)

	// SyncToStrategy pushes the watcher's current limits into a *sessctx.Strategy
	// so that token-budget calculations downstream see the latest configuration.
	// It is invoked by (*agent).applyConfig after Refresh and before the fallible
	// delegate chain runs.
	//
	// SyncToStrategy is intentionally void per ADR-029 §5: it performs only
	// in-memory field assignments on a Strategy that the caller already owns.
	// There is no I/O, no validation, and no observable failure mode. Promoting
	// it to fallible would add a return-value-checking burden at every call
	// site for a contract that cannot fail.
	//
	// Do NOT change this signature to return error without first amending
	// ADR-029. The fail-fast delegate chain in (*agent).applyConfig is
	// scoped to SafePublish, Engine.Reconfigure, and Manager.Reconfigure
	// only; expanding the chain is a non-trivial architectural decision.
	SyncToStrategy(cs *sessctx.Strategy)
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
	contextWindow        int
	defaultHistoryTokens int
	defaultToolTurns     int
	defaultHistoryTurns  int
	defaultWindow        int
}

// NewFileConfigWatcher creates a new fileConfigWatcher with default values.
func NewFileConfigWatcher(mainLoader config.ConfigLoader, sessionLoader config.SessionLoader, tokens, toolTurns, historyTurns int, logger ports.Logger) ConfigWatcher {
	defaultWindow := 1000000

	return &FileConfigWatcher{
		Loader:               mainLoader,
		SessionLoader:        sessionLoader,
		FS:                   realFileStat{},
		logger:               logger,
		maxHistoryTokens:     tokens,
		maxToolTurns:         toolTurns,
		maxHistoryTurns:      historyTurns,
		contextWindow:        defaultWindow,
		defaultHistoryTokens: tokens,
		defaultToolTurns:     toolTurns,
		defaultHistoryTurns:  historyTurns,
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
		cw.logSessionLoadError(err)
		return
	}
	if sessCfg == nil {
		return
	}
	cw.applySessionLimits(sessCfg)
}

// applySessionLimits applies non-nil session config overrides to the watcher's
// cached limits. The caller must hold cw.mu (write lock).
func (cw *FileConfigWatcher) applySessionLimits(cfg *config.SessionConfig) {
	if cfg.MaxHistoryTokens != nil {
		cw.maxHistoryTokens = *cfg.MaxHistoryTokens
	}
	if cfg.MaxToolTurns != nil {
		cw.maxToolTurns = *cfg.MaxToolTurns
	}
	if cfg.MaxHistoryTurns != nil {
		cw.maxHistoryTurns = *cfg.MaxHistoryTurns
	}
}

// logSessionLoadError logs a non-IsNotExist load error if a logger is configured.
// IsNotExist errors are silently ignored — a missing session config file is not a fault.
func (cw *FileConfigWatcher) logSessionLoadError(err error) {
	if !os.IsNotExist(err) && cw.logger != nil {
		cw.logger.Warn("Failed to load session config", "path", cw.sessionPath, "error", err)
	}
}

// SetLimits updates the cached limits manually.
func (cw *FileConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	setLimitsLocked(tokens, toolTurns, historyTurns, &cw.maxHistoryTokens, &cw.maxToolTurns, &cw.maxHistoryTurns)
}

// GetLimits returns the current cached limits.
func (cw *FileConfigWatcher) GetLimits() (tokens, toolTurns, historyTurns int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns
}

// ApplyLimits updates the cached limits from an events.Limits struct.
func (cw *FileConfigWatcher) ApplyLimits(l events.Limits) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	setLimitsLocked(l.MaxHistoryTokens, l.MaxToolTurns, l.MaxHistoryTurns, &cw.maxHistoryTokens, &cw.maxToolTurns, &cw.maxHistoryTurns)
}

// SyncToStrategy synchronizes the current watcher state to a ContextStrategy.
func (cw *FileConfigWatcher) SyncToStrategy(cs *sessctx.Strategy) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	if cs != nil {
		cs.SetLimits(cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns)
		cs.SetContextWindow(cw.contextWindow)
	}
}

// noOpConfigWatcher implements ConfigWatcher but performs no file operations.
type noOpConfigWatcher struct {
	mu               sync.RWMutex
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	contextWindow    int
}

// NewNoOpConfigWatcher creates a new noOpConfigWatcher with default values.
func NewNoOpConfigWatcher(tokens, toolTurns, historyTurns int) ConfigWatcher {
	return &noOpConfigWatcher{
		maxHistoryTokens: tokens,
		maxToolTurns:     toolTurns,
		maxHistoryTurns:  historyTurns,
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
	setLimitsLocked(tokens, toolTurns, historyTurns, &cw.maxHistoryTokens, &cw.maxToolTurns, &cw.maxHistoryTurns)
}

// GetLimits returns the current cached limits.
func (cw *noOpConfigWatcher) GetLimits() (tokens, toolTurns, historyTurns int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns
}

// ApplyLimits updates the cached limits from an events.Limits struct.
func (cw *noOpConfigWatcher) ApplyLimits(l events.Limits) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	setLimitsLocked(l.MaxHistoryTokens, l.MaxToolTurns, l.MaxHistoryTurns, &cw.maxHistoryTokens, &cw.maxToolTurns, &cw.maxHistoryTurns)
}

// SyncToStrategy synchronizes the current watcher state to a ContextStrategy.
func (cw *noOpConfigWatcher) SyncToStrategy(cs *sessctx.Strategy) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	if cs != nil {
		cs.SetLimits(cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns)
		cs.SetContextWindow(cw.contextWindow)
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
