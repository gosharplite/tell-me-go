// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

// MockSessionProvider is a testify-based mock of ports.SessionProvider.
// GetTasks always returns nil; configure other accessors with
// mock.On(...). nil-returning conversions are handled defensively so
// that tests can return untyped nils via Return(nil).
type MockSessionProvider struct {
	mock.Mock
}

func (m *MockSessionProvider) GetTasks() ports.TaskStore { return nil }
func (m *MockSessionProvider) GetSettings() ports.KVStore {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.KVStore)
}
func (m *MockSessionProvider) GetInfo() ports.SessionInfo {
	args := m.Called()
	return args.Get(0).(ports.SessionInfo)
}
func (m *MockSessionProvider) SetInfo(info ports.SessionInfo) {
	m.Called(info)
}
func (m *MockSessionProvider) Close() error {
	args := m.Called()
	return args.Error(0)
}
func (m *MockSessionProvider) GetHealthChecker() ports.HealthChecker {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.HealthChecker)
}
