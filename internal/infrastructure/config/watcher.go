// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"sync"
	"time"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
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

// resolveContextWindow determines the context window for the given model.
// It returns the model-specific override if one exists with a positive value;
// otherwise it returns the provided default window.
func resolveContextWindow(cfg *domain_config.Config, model string, defaultWindow int) int {
	if mCfg, ok := cfg.Models[model]; ok && mCfg.ContextWindow > 0 {
		return mCfg.ContextWindow
	}
	return defaultWindow
}

// reloadSnapshot captures the mutable comparison state needed to decide
// whether to reload files. It is populated under RLock (Phase 1) so that
// all blocking I/O (Phase 2) can run without holding any mutex.
type reloadSnapshot struct {
	mainPath       string
	sessionPath    string
	lastMainMod    time.Time
	lastSessionMod time.Time
	lastModel      string
}

// fileConfigWatcher monitors configuration files for changes and caches values.
type fileConfigWatcher struct {
	mu                   sync.RWMutex
	Loader               domain_config.ConfigLoader
	SessionLoader        domain_config.SessionLoader
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
func NewFileConfigWatcher(mainLoader domain_config.ConfigLoader, sessionLoader domain_config.SessionLoader, tokens, toolTurns, historyTurns int, logger ports.Logger) domain_config.ConfigWatcher {
	defaultWindow := 1000000

	return &fileConfigWatcher{
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
func (cw *fileConfigWatcher) SetPaths(main, session string) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.mainPath = main
	cw.sessionPath = session
}

// snapshotReloadState returns a stack-local copy of the watcher's mutable
// comparison fields. Caller must NOT hold any lock; this method acquires
// and releases RLock internally.
func (cw *fileConfigWatcher) snapshotReloadState() reloadSnapshot {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return reloadSnapshot{
		mainPath:       cw.mainPath,
		sessionPath:    cw.sessionPath,
		lastMainMod:    cw.lastMainMod,
		lastSessionMod: cw.lastSessionMod,
		lastModel:      cw.lastModel,
	}
}

// Refresh checks for file changes and updates cached values if necessary.
// It uses three phases to avoid holding the write lock across disk I/O:
//  1. RLock snapshot of comparison state
//  2. All blocking I/O without any lock
//  3. Write lock only for in-memory mutations
func (cw *fileConfigWatcher) Refresh(model string) {
	// Phase 1: Snapshot mutable comparison state under RLock.
	snap := cw.snapshotReloadState()

	// Phase 2: All blocking I/O. No lock held.
	// Safe because Loader/SessionLoader/FS are immutable after construction.
	newMainCfg, mainInfo, mainChanged := cw.loadMainOutsideLock(snap, model)
	newSessCfg, sessInfo, sessChanged := cw.loadSessionOutsideLock(snap, mainChanged)

	// Phase 3: Apply mutations to in-memory cache only. No I/O.
	// Guard each mutation with a ModTime comparison to prevent
	// stale overwrites: if a concurrent Refresh already applied
	// a newer version, we must not overwrite it with older data.
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if mainChanged && !mainInfo.ModTime().Before(cw.lastMainMod) {
		cw.applyMainConfig(newMainCfg, mainInfo, model)
	}
	if sessChanged && !sessInfo.ModTime().Before(cw.lastSessionMod) {
		cw.applySessionLimits(newSessCfg)
		cw.lastSessionMod = sessInfo.ModTime()
	}
}

// loadMainOutsideLock performs all blocking I/O needed to reload the main
// configuration file. It must NOT hold any mutex. It returns the parsed
// config, the os.FileInfo from Stat, and whether the file was actually
// reloaded. If no reload is needed, it returns (nil, nil, false).
func (cw *fileConfigWatcher) loadMainOutsideLock(snap reloadSnapshot, model string) (*domain_config.Config, os.FileInfo, bool) {
	ok, info := cw.shouldReloadMain(snap, model)
	if !ok {
		return nil, nil, false
	}

	cfg, err := cw.Loader.Load(snap.mainPath)
	if err != nil {
		return nil, nil, false
	}

	return cfg, info, true
}

// shouldReloadMain checks whether the main configuration file needs to be
// re-read using a snapshot captured under RLock. It returns (false, nil) when
// the reload can be skipped because the path is empty, the file cannot be
// stat'd, neither the modification time nor the model have changed, or the
// Loader is nil. Otherwise it returns (true, info) where info is the
// os.FileInfo from Stat.
func (cw *fileConfigWatcher) shouldReloadMain(snap reloadSnapshot, model string) (bool, os.FileInfo) {
	if snap.mainPath == "" {
		return false, nil
	}

	info, err := cw.FS.Stat(snap.mainPath)
	if err != nil {
		return false, nil
	}

	if !info.ModTime().After(snap.lastMainMod) && model == snap.lastModel {
		return false, nil
	}

	if cw.Loader == nil {
		return false, nil
	}

	return true, info
}

// loadSessionOutsideLock performs all blocking I/O needed to reload the
// session configuration file. It must NOT hold any mutex. It returns the
// parsed session config, the os.FileInfo from Stat, and whether the file
// was actually reloaded. If no reload is needed, it returns (nil, nil, false).
func (cw *fileConfigWatcher) loadSessionOutsideLock(snap reloadSnapshot, forceUpdate bool) (*domain_config.SessionConfig, os.FileInfo, bool) {
	if snap.sessionPath == "" {
		return nil, nil, false
	}

	info, err := cw.FS.Stat(snap.sessionPath)
	if err != nil {
		return nil, nil, false
	}

	if !info.ModTime().After(snap.lastSessionMod) && !forceUpdate {
		return nil, nil, false
	}

	if cw.SessionLoader == nil {
		return nil, nil, false
	}

	sessCfg, err := cw.SessionLoader.LoadSession(snap.sessionPath)
	if err != nil {
		cw.logSessionLoadErrorNoLock(err, snap.sessionPath)
		return nil, nil, false
	}
	if sessCfg == nil {
		return nil, nil, false
	}

	return sessCfg, info, true
}

// applyMainConfig updates the watcher's cached state from a successfully
// loaded main configuration. The caller must hold cw.mu (write lock).
func (cw *fileConfigWatcher) applyMainConfig(cfg *domain_config.Config, info os.FileInfo, model string) {
	cw.lastMainMod = info.ModTime()
	cw.lastModel = model

	cw.maxHistoryTokens = cfg.MaxHistoryTokens
	cw.maxToolTurns = cfg.MaxToolTurns
	cw.maxHistoryTurns = cfg.MaxHistoryTurns

	cw.contextWindow = resolveContextWindow(cfg, model, cw.defaultWindow)
}

// applySessionLimits applies non-nil session config overrides to the watcher's
// cached limits. The caller must hold cw.mu (write lock).
func (cw *fileConfigWatcher) applySessionLimits(cfg *domain_config.SessionConfig) {
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

// logSessionLoadErrorNoLock logs a session load error without reading
// cw.sessionPath from the watcher. It is safe to call outside the mutex.
func (cw *fileConfigWatcher) logSessionLoadErrorNoLock(err error, sessionPath string) {
	if !os.IsNotExist(err) && cw.logger != nil {
		cw.logger.Warn("Failed to load session config", "path", sessionPath, "error", err)
	}
}

// SetLimits updates the cached limits manually.
func (cw *fileConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	setLimitsLocked(tokens, toolTurns, historyTurns, &cw.maxHistoryTokens, &cw.maxToolTurns, &cw.maxHistoryTurns)
}

// GetLimits returns the current cached limits.
func (cw *fileConfigWatcher) GetLimits() (tokens, toolTurns, historyTurns int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns
}

// ApplyLimits updates the cached limits from an events.Limits struct.
func (cw *fileConfigWatcher) ApplyLimits(l events.Limits) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	setLimitsLocked(l.MaxHistoryTokens, l.MaxToolTurns, l.MaxHistoryTurns, &cw.maxHistoryTokens, &cw.maxToolTurns, &cw.maxHistoryTurns)
}

// GetContextWindow returns the current cached context window.
func (cw *fileConfigWatcher) GetContextWindow() int {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.contextWindow
}

// getDefaultWindow returns the default context window value.
func (cw *fileConfigWatcher) getDefaultWindow() int {
	return cw.defaultWindow
}
