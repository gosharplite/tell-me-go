// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/security"
)

// SecurityManager encapsulates all security-related state and path validation logic.
// Deprecated: Use github.com/gosharplite/tell-me-go/internal/security directly in new code.
type SecurityManager struct {
	inner *security.SecurityManager

	// Fields kept for backward compatibility with direct access within the 'tools' package
	pricingMu           sync.Mutex
	bypassMu            sync.RWMutex
	bypassConfirmations bool
}

// NewSecurityManager initializes a new SecurityManager.
func NewSecurityManager() *SecurityManager {
	return &SecurityManager{
		inner: security.NewSecurityManager(os.Stdin),
	}
}

// SetInputReader sets the input reader for the SecurityManager.
func (sm *SecurityManager) SetInputReader(r io.Reader) {
	sm.inner.Interaction.SetReader(r)
}

// SetBypassFile sets the file where persistent bypass state is stored.
func (sm *SecurityManager) SetBypassFile(path string) {
	sm.inner.SetBypassFile(path)
}

// LoadBypassState reads the persistent bypass state from disk.
func (sm *SecurityManager) LoadBypassState() {
	sm.inner.LoadBypassState()
	sm.bypassMu.Lock()
	sm.bypassConfirmations = sm.inner.IsBypassActive()
	sm.bypassMu.Unlock()
}

// IsBypassActive returns the current state of bypass_confirmation.
func (sm *SecurityManager) IsBypassActive() bool {
	sm.bypassMu.RLock()
	defer sm.bypassMu.RUnlock()
	return sm.bypassConfirmations
}

// SaveBypassState writes the persistent bypass state to disk.
func (sm *SecurityManager) SaveBypassState(ctx context.Context) {
	sm.bypassMu.RLock()
	active := sm.bypassConfirmations
	sm.bypassMu.RUnlock()
	sm.inner.SetBypassActive(active)
	sm.inner.SaveBypassState(ctx)
}

// SetCommandsLogFile sets the path for logging executed commands.
func (sm *SecurityManager) SetCommandsLogFile(path string) {
	sm.inner.Auditor.SetLogFile(path)
}

// TerminalLock locks the terminal for exclusive access.
func (sm *SecurityManager) TerminalLock() {
	sm.inner.Interaction.TerminalLock()
}

// TerminalUnlock unlocks the terminal.
func (sm *SecurityManager) TerminalUnlock() {
	sm.inner.Interaction.TerminalUnlock()
}

// logAudit writes a two-line audit entry to the commands log file.
func (sm *SecurityManager) logAudit(label1, val1, label2, val2 string) {
	sm.inner.Auditor.LogAudit(label1, val1, label2, val2)
}

// ConfirmDestructiveAction prompts the user for confirmation before performing a destructive tool action.
func (sm *SecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return sm.inner.Interaction.ConfirmAction(ctx, action, target, detail, sm.IsBypassActive())
}

// SetSafePathsFile sets the file where persistent safe paths are stored.
func (sm *SecurityManager) SetSafePathsFile(path string) {
	sm.inner.Policy.SetConfigFile(path, true)
}

// SetReadOnlyPathsFile sets the file where persistent read-only paths are stored.
func (sm *SecurityManager) SetReadOnlyPathsFile(path string) {
	sm.inner.Policy.SetConfigFile(path, false)
}

// LoadSafePaths reads persistent safe paths from disk.
func (sm *SecurityManager) LoadSafePaths() error {
	return sm.inner.Policy.LoadPaths(true)
}

// LoadReadOnlyPaths reads persistent read-only paths from disk.
func (sm *SecurityManager) LoadReadOnlyPaths() error {
	return sm.inner.Policy.LoadPaths(false)
}

// SaveSafePaths writes persistent safe paths to disk.
func (sm *SecurityManager) SaveSafePaths(ctx context.Context) error {
	return sm.inner.Policy.SavePaths(ctx, true)
}

// SaveReadOnlyPaths writes persistent read-only paths to disk.
func (sm *SecurityManager) SaveReadOnlyPaths(ctx context.Context) error {
	return sm.inner.Policy.SavePaths(ctx, false)
}

// RegisterSafePath adds a directory or file to the list of allowed boundaries for tool access.
func (sm *SecurityManager) RegisterSafePath(path string) {
	sm.inner.Policy.RegisterPath(path, true)
}

// RegisterReadOnlyPath adds a directory or file to the list of allowed boundaries for read-only access.
func (sm *SecurityManager) RegisterReadOnlyPath(path string) {
	sm.inner.Policy.RegisterPath(path, false)
}

// GetSafePaths returns a copy of the currently registered safe paths.
func (sm *SecurityManager) GetSafePaths() []string {
	return sm.inner.Policy.GetPaths(true)
}

// GetReadOnlyPaths returns a copy of the currently registered read-only paths.
func (sm *SecurityManager) GetReadOnlyPaths() []string {
	return sm.inner.Policy.GetPaths(false)
}

// RemoveSafePath removes a path from the authorized list.
func (sm *SecurityManager) RemoveSafePath(path string) error {
	return sm.inner.Policy.RemovePath(path, true)
}

// RemoveReadOnlyPath removes a path from the read-only authorized list.
func (sm *SecurityManager) RemoveReadOnlyPath(path string) error {
	return sm.inner.Policy.RemovePath(path, false)
}

// IsPathSafe checks if a path is within the allowed boundaries (CWD, Temp, or registered Home/Config paths).
// It returns the resolved absolute path if safe, or an error otherwise.
func (sm *SecurityManager) IsPathSafe(path string) (string, error) {
	return sm.inner.Policy.ValidatePath(path, false)
}

// IsPathWritable checks if a path is within the writable boundaries (CWD, Temp, or registered safe paths).
// It does NOT include read-only paths.
// It returns the resolved absolute path if writable, or an error otherwise.
func (sm *SecurityManager) IsPathWritable(path string) (string, error) {
	return sm.inner.Policy.ValidatePath(path, true)
}

func (sm *SecurityManager) readSingleKey(ctx context.Context) (string, error) {
	return sm.inner.Interaction.ReadSingleKey(ctx)
}

func (sm *SecurityManager) readLine(ctx context.Context) (string, error) {
	return sm.inner.Interaction.ReadLine(ctx)
}
