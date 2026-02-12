// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"os"
	"strings"
	"sync"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// SecurityManager coordinates path validation, user interaction, and auditing.
type SecurityManager struct {
	policy       *pathPolicy
	interaction  *interactionHandler
	Auditor      AuditLogger
	domainPolicy *domain.Policy
	safety       *domain.SafetyService

	bypassFile   string
	bypassActive bool
	bypassMu     sync.RWMutex
}

// NewSecurityManager creates a new SecurityManager.
func NewSecurityManager(interactor domain.UserInteractor) *SecurityManager {
	if interactor == nil {
		interactor = &NoOpInteractor{}
	}
	auditor := NewAuditor()
	auditor.SetInteractor(interactor)
	policy := domain.DefaultPolicy()
	return &SecurityManager{
		policy:       newPathPolicy(),
		interaction:  newInteractionHandler(interactor, auditor),
		Auditor:      auditor,
		domainPolicy: policy,
		safety:       domain.NewSafetyService(policy),
	}
}

// GetPolicy returns the domain security policy.
func (sm *SecurityManager) GetPolicy() *domain.Policy {
	return sm.domainPolicy
}

// SetPolicy sets the domain security policy.
func (sm *SecurityManager) SetPolicy(p *domain.Policy) {
	sm.domainPolicy = p
	sm.safety = domain.NewSafetyService(p)
}

// IsPathSafe checks if a path is safe.
func (sm *SecurityManager) IsPathSafe(path string) (string, error) {
	return sm.policy.ValidatePath(path, false)
}

// IsPathWritable checks if a path is writable.
func (sm *SecurityManager) IsPathWritable(path string) (string, error) {
	return sm.policy.ValidatePath(path, true)
}

// ConfirmDestructiveAction prompts the user for confirmation.
func (sm *SecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return sm.interaction.ConfirmAction(ctx, action, target, detail, sm.IsBypassActive())
}

// Authorize prompts the user for authorization of a specific command or action.
func (sm *SecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	if sm.IsBypassActive() {
		return true, nil
	}
	if isSafe {
		return true, nil
	}
	return sm.interaction.ConfirmAction(ctx, "Execute "+label, detail, reason, false)
}

// LogAudit writes an audit entry.
func (sm *SecurityManager) LogAudit(label1, val1, label2, val2 string) {
	sm.Auditor.LogAudit(label1, val1, label2, val2)
}

// Warn prints a security warning.
func (sm *SecurityManager) Warn(message string) {
	sm.interaction.interactor.Warn(message)
}

// SetInteractor updates the user interactor.
func (sm *SecurityManager) SetInteractor(interactor domain.UserInteractor) {
	sm.interaction.SetInteractor(interactor)
	sm.Auditor.SetInteractor(interactor)
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
	_ = storage.AtomicWrite(ctx, file, []byte(val), 0644)
}

// SetBypassActive sets the bypass state.
func (sm *SecurityManager) SetBypassActive(active bool) {
	sm.bypassMu.Lock()
	defer sm.bypassMu.Unlock()
	sm.bypassActive = active
}

// IsCommandAllowed checks if a base command is allowed for execution.
func (sm *SecurityManager) IsCommandAllowed(command string) bool {
	return sm.domainPolicy.IsCommandAllowed(command)
}

// TerminalLock locks the terminal.
func (sm *SecurityManager) TerminalLock() {
	sm.interaction.TerminalLock()
}

// TerminalUnlock unlocks the terminal.
func (sm *SecurityManager) TerminalUnlock() {
	sm.interaction.TerminalUnlock()
}

// ReadSingleKey reads a single key.
func (sm *SecurityManager) ReadSingleKey(ctx context.Context) (string, error) {
	return sm.interaction.ReadSingleKey(ctx)
}

// ReadLine reads a line.
func (sm *SecurityManager) ReadLine(ctx context.Context) (string, error) {
	return sm.interaction.ReadLine(ctx)
}

// RegisterSafePath registers a safe path.
func (sm *SecurityManager) RegisterSafePath(path string) {
	sm.policy.RegisterPath(path, true)
}

// SaveSafePaths saves safe paths.
func (sm *SecurityManager) SaveSafePaths(ctx context.Context) error {
	return sm.policy.SavePaths(ctx, true)
}

// RemoveSafePath removes a safe path.
func (sm *SecurityManager) RemoveSafePath(path string) error {
	return sm.policy.RemovePath(path, true)
}

// SetCommandsLogFile sets the commands log file.
func (sm *SecurityManager) SetCommandsLogFile(path string) {
	sm.Auditor.SetLogFile(path)
}

// SetSafePathsFile sets the safe paths file.
func (sm *SecurityManager) SetSafePathsFile(path string) {
	sm.policy.SetConfigFile(path, true)
}

// SetReadOnlyPathsFile sets the read-only paths file.
func (sm *SecurityManager) SetReadOnlyPathsFile(path string) {
	sm.policy.SetConfigFile(path, false)
}

// LoadSafePaths loads safe paths.
func (sm *SecurityManager) LoadSafePaths() error {
	return sm.policy.LoadPaths(true)
}

// LoadReadOnlyPaths loads read-only paths.
func (sm *SecurityManager) LoadReadOnlyPaths() error {
	return sm.policy.LoadPaths(false)
}

// SaveReadOnlyPaths saves read-only paths.
func (sm *SecurityManager) SaveReadOnlyPaths(ctx context.Context) error {
	return sm.policy.SavePaths(ctx, false)
}

// RegisterReadOnlyPath registers a read-only path.
func (sm *SecurityManager) RegisterReadOnlyPath(path string) {
	sm.policy.RegisterPath(path, false)
}

// GetSafePaths returns safe paths.
func (sm *SecurityManager) GetSafePaths() []string {
	return sm.policy.GetPaths(true)
}

// GetReadOnlyPaths returns read-only paths.
func (sm *SecurityManager) GetReadOnlyPaths() []string {
	return sm.policy.GetPaths(false)
}

// RemoveReadOnlyPath removes a read-only path.
func (sm *SecurityManager) RemoveReadOnlyPath(path string) error {
	return sm.policy.RemovePath(path, false)
}

// GetSafetyService returns the domain safety service.
func (sm *SecurityManager) GetSafetyService() *domain.SafetyService {
	return sm.safety
}

// GetInteractor returns the user interactor.
func (sm *SecurityManager) GetInteractor() domain.UserInteractor {
	return sm.interaction.interactor
}
