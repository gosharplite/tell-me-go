// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

// SecurityManager coordinates path validation, user interaction, and auditing.
type SecurityManager struct {
	Policy      *PathPolicy
	Interaction *InteractionHandler
	Auditor     *Auditor

	bypassFile   string
	bypassActive bool
	bypassMu     sync.RWMutex
}

// NewSecurityManager creates a new SecurityManager.
func NewSecurityManager(input io.Reader) *SecurityManager {
	auditor := NewAuditor()
	return &SecurityManager{
		Policy:      NewPathPolicy(),
		Interaction: NewInteractionHandler(input, auditor),
		Auditor:     auditor,
	}
}

// IsPathSafe checks if a path is safe.
func (sm *SecurityManager) IsPathSafe(path string) (string, error) {
	return sm.Policy.ValidatePath(path, false)
}

// IsPathWritable checks if a path is writable.
func (sm *SecurityManager) IsPathWritable(path string) (string, error) {
	return sm.Policy.ValidatePath(path, true)
}

// ConfirmDestructiveAction prompts the user for confirmation.
func (sm *SecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return sm.Interaction.ConfirmAction(ctx, action, target, detail, sm.IsBypassActive())
}

// LogAudit writes an audit entry.
func (sm *SecurityManager) LogAudit(label1, val1, label2, val2 string) {
	sm.Auditor.LogAudit(label1, val1, label2, val2)
}

// SetBypassFile sets the file where persistent bypass state is stored.
func (sm *SecurityManager) SetBypassFile(path string) {
	sm.bypassMu.Lock()
	defer sm.bypassMu.Unlock()
	sm.bypassFile = path
}

// LoadBypassState reads the persistent bypass state from disk.
func (sm *SecurityManager) LoadBypassState() {
	sm.bypassMu.Lock()
	defer sm.bypassMu.Unlock()
	if sm.bypassFile == "" {
		return
	}
	data, err := os.ReadFile(sm.bypassFile)
	if err == nil {
		sm.bypassActive = strings.TrimSpace(string(data)) == "true"
	}
}

// IsBypassActive returns the current state of bypass_confirmation.
func (sm *SecurityManager) IsBypassActive() bool {
	sm.bypassMu.RLock()
	defer sm.bypassMu.RUnlock()
	return sm.bypassActive
}

// SaveBypassState writes the persistent bypass state to disk.
func (sm *SecurityManager) SaveBypassState(ctx context.Context) {
	sm.bypassMu.RLock()
	file := sm.bypassFile
	active := sm.bypassActive
	sm.bypassMu.RUnlock()

	if file == "" {
		return
	}
	val := "false"
	if active {
		val = "true"
	}
	_ = fsutil.AtomicWrite(ctx, file, []byte(val), 0644)
}

// SetBypassActive sets the bypass state.
func (sm *SecurityManager) SetBypassActive(active bool) {
	sm.bypassMu.Lock()
	defer sm.bypassMu.Unlock()
	sm.bypassActive = active
}

// TerminalLock locks the terminal.
func (sm *SecurityManager) TerminalLock() {
	sm.Interaction.TerminalLock()
}

// TerminalUnlock unlocks the terminal.
func (sm *SecurityManager) TerminalUnlock() {
	sm.Interaction.TerminalUnlock()
}

// ReadSingleKey reads a single key.
func (sm *SecurityManager) ReadSingleKey(ctx context.Context) (string, error) {
	return sm.Interaction.ReadSingleKey(ctx)
}

// ReadLine reads a line.
func (sm *SecurityManager) ReadLine(ctx context.Context) (string, error) {
	return sm.Interaction.ReadLine(ctx)
}

// SetInputReader sets the input reader.
func (sm *SecurityManager) SetInputReader(r io.Reader) {
	sm.Interaction.SetReader(r)
}

// RegisterSafePath registers a safe path.
func (sm *SecurityManager) RegisterSafePath(path string) {
	sm.Policy.RegisterPath(path, true)
}

// SaveSafePaths saves safe paths.
func (sm *SecurityManager) SaveSafePaths(ctx context.Context) error {
	return sm.Policy.SavePaths(ctx, true)
}

// RemoveSafePath removes a safe path.
func (sm *SecurityManager) RemoveSafePath(path string) error {
	return sm.Policy.RemovePath(path, true)
}

// SetCommandsLogFile sets the commands log file.
func (sm *SecurityManager) SetCommandsLogFile(path string) {
	sm.Auditor.SetLogFile(path)
}

// SetSafePathsFile sets the safe paths file.
func (sm *SecurityManager) SetSafePathsFile(path string) {
	sm.Policy.SetConfigFile(path, true)
}

// SetReadOnlyPathsFile sets the read-only paths file.
func (sm *SecurityManager) SetReadOnlyPathsFile(path string) {
	sm.Policy.SetConfigFile(path, false)
}

// LoadSafePaths loads safe paths.
func (sm *SecurityManager) LoadSafePaths() error {
	return sm.Policy.LoadPaths(true)
}

// LoadReadOnlyPaths loads read-only paths.
func (sm *SecurityManager) LoadReadOnlyPaths() error {
	return sm.Policy.LoadPaths(false)
}

// SaveReadOnlyPaths saves read-only paths.
func (sm *SecurityManager) SaveReadOnlyPaths(ctx context.Context) error {
	return sm.Policy.SavePaths(ctx, false)
}

// RegisterReadOnlyPath registers a read-only path.
func (sm *SecurityManager) RegisterReadOnlyPath(path string) {
	sm.Policy.RegisterPath(path, false)
}

// GetSafePaths returns safe paths.
func (sm *SecurityManager) GetSafePaths() []string {
	return sm.Policy.GetPaths(true)
}

// GetReadOnlyPaths returns read-only paths.
func (sm *SecurityManager) GetReadOnlyPaths() []string {
	return sm.Policy.GetPaths(false)
}

// RemoveReadOnlyPath removes a read-only path.
func (sm *SecurityManager) RemoveReadOnlyPath(path string) error {
	return sm.Policy.RemovePath(path, false)
}
