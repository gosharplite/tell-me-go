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
