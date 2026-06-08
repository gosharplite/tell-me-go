// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockSessionProvider is a hand-rolled, mutex-guarded spy of ports.SessionProvider.
// GetTasks always returns nil; all other methods delegate to configurable
// function fields. When a function field is nil, the method returns its
// natural zero value. Spy counters and call-order tracking enable
// assertion without testify/mock.
type MockSessionProvider struct {
	mu sync.Mutex

	// Function fields — nil means return zero value
	GetSettingsFn      func() ports.KVStore
	GetInfoFn          func() ports.SessionInfo
	SetInfoFn          func(info ports.SessionInfo)
	CloseFn            func() error
	GetHealthCheckerFn func() ports.HealthChecker

	// Spy counters
	getSettingsCalls      int
	getInfoCalls          int
	setInfoCalls          int
	closeCalls            int
	getHealthCheckerCalls int
	calledMethods         []string
}

// Compile-time interface check.
var _ ports.SessionProvider = (*MockSessionProvider)(nil)

// GetTasks returns nil. This method is not tracked by spy counters.
func (m *MockSessionProvider) GetTasks() ports.TaskStore { return nil }

func (m *MockSessionProvider) GetSettings() ports.KVStore {
	m.mu.Lock()
	m.getSettingsCalls++
	m.calledMethods = append(m.calledMethods, "GetSettings")
	fn := m.GetSettingsFn
	m.mu.Unlock()

	if fn != nil {
		return fn()
	}
	return nil
}

func (m *MockSessionProvider) GetInfo() ports.SessionInfo {
	m.mu.Lock()
	m.getInfoCalls++
	m.calledMethods = append(m.calledMethods, "GetInfo")
	fn := m.GetInfoFn
	m.mu.Unlock()

	if fn != nil {
		return fn()
	}
	return ports.SessionInfo{}
}

func (m *MockSessionProvider) SetInfo(info ports.SessionInfo) {
	m.mu.Lock()
	m.setInfoCalls++
	m.calledMethods = append(m.calledMethods, "SetInfo")
	fn := m.SetInfoFn
	m.mu.Unlock()

	if fn != nil {
		fn(info)
	}
}

func (m *MockSessionProvider) Close() error {
	m.mu.Lock()
	m.closeCalls++
	m.calledMethods = append(m.calledMethods, "Close")
	fn := m.CloseFn
	m.mu.Unlock()

	if fn != nil {
		return fn()
	}
	return nil
}

func (m *MockSessionProvider) GetHealthChecker() ports.HealthChecker {
	m.mu.Lock()
	m.getHealthCheckerCalls++
	m.calledMethods = append(m.calledMethods, "GetHealthChecker")
	fn := m.GetHealthCheckerFn
	m.mu.Unlock()

	if fn != nil {
		return fn()
	}
	return nil
}

// Snapshot returns a point-in-time copy of all spy counters and the
// ordered list of called method names. It is safe to call concurrently.
func (m *MockSessionProvider) Snapshot() (
	getSettingsCalls, getInfoCalls, setInfoCalls, closeCalls, getHealthCheckerCalls int,
	methods []string,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calledMethods))
	copy(out, m.calledMethods)
	return m.getSettingsCalls, m.getInfoCalls, m.setInfoCalls, m.closeCalls, m.getHealthCheckerCalls, out
}
