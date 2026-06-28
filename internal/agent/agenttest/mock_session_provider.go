// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockSessionProvider is a hand-rolled stub of ports.SessionProvider.
// All methods delegate to configurable function fields. When a function
// field is nil, the method returns its natural zero value (or the shared
// SessionInfo field for GetInfo/SetInfo).
type MockSessionProvider struct {
	// SessionInfo is a convenience field for tests that just need
	// GetInfo/SetInfo to share a mutable SessionInfo value.
	// When GetInfoFn/SetInfoFn are nil, GetInfo returns this field
	// and SetInfo writes to it.
	SessionInfo ports.SessionInfo

	// Function fields — nil means use SessionInfo field or zero value
	GetTasksFn         func() ports.TaskStore
	GetSettingsFn      func() ports.KVStore
	GetInfoFn          func() ports.SessionInfo
	SetInfoFn          func(ctx context.Context, info ports.SessionInfo) error
	CloseFn            func() error
	GetHealthCheckerFn func() ports.HealthChecker
}

// Compile-time interface check.
var _ ports.SessionProvider = (*MockSessionProvider)(nil)

func (m *MockSessionProvider) GetTasks() ports.TaskStore {
	if m.GetTasksFn != nil {
		return m.GetTasksFn()
	}
	return nil
}

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
	return m.SessionInfo
}

func (m *MockSessionProvider) SetInfo(ctx context.Context, info ports.SessionInfo) error {
	if m.SetInfoFn != nil {
		return m.SetInfoFn(ctx, info)
	}
	m.SessionInfo = info
	return nil
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
