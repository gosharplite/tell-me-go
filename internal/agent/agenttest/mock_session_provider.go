// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockSessionProvider is a hand-rolled stub of ports.SessionProvider.
// GetTasks always returns nil; all other methods delegate to configurable
// function fields. When a function field is nil, the method returns its
// natural zero value.
type MockSessionProvider struct {
	// Function fields — nil means return zero value
	GetSettingsFn      func() ports.KVStore
	GetInfoFn          func() ports.SessionInfo
	SetInfoFn          func(info ports.SessionInfo)
	CloseFn            func() error
	GetHealthCheckerFn func() ports.HealthChecker
}

// Compile-time interface check.
var _ ports.SessionProvider = (*MockSessionProvider)(nil)

// GetTasks returns nil. This method is not tracked.
func (m *MockSessionProvider) GetTasks() ports.TaskStore { return nil }

func (m *MockSessionProvider) GetSettings() ports.KVStore {
	if m.GetSettingsFn != nil {
		return m.GetSettingsFn()
	}
	return nil
}

func (m *MockSessionProvider) GetInfo() ports.SessionInfo {
	if m.GetInfoFn != nil {
		return m.GetInfoFn()
	}
	return ports.SessionInfo{}
}

func (m *MockSessionProvider) SetInfo(info ports.SessionInfo) {
	if m.SetInfoFn != nil {
		m.SetInfoFn(info)
	}
}

func (m *MockSessionProvider) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

func (m *MockSessionProvider) GetHealthChecker() ports.HealthChecker {
	if m.GetHealthCheckerFn != nil {
		return m.GetHealthCheckerFn()
	}
	return nil
}
