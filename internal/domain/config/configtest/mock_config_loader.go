// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package configtest

import (
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

// MockConfigLoader is a hand-rolled spy for config.ConfigLoader.
// Set LoadFunc to override behavior; when nil, Load returns (nil, nil).
type MockConfigLoader struct {
	mu sync.Mutex

	// LoadFunc is invoked by Load when non-nil.
	LoadFunc func(path string) (*config.Config, error)

	// Spy state.
	calledLoad   int
	lastLoadPath string
}

// Snapshot returns a race-safe copy of all call counts.
func (m *MockConfigLoader) Snapshot() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]int{
		"Load": m.calledLoad,
	}
}

func (m *MockConfigLoader) Load(path string) (*config.Config, error) {
	m.mu.Lock()
	m.calledLoad++
	m.lastLoadPath = path
	fn := m.LoadFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(path)
	}
	return nil, nil
}
