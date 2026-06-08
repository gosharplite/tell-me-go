// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockHealthCheckManager is a hand-rolled mutex-guarded spy for ports.HealthCheckManager.
type MockHealthCheckManager struct {
	mu sync.Mutex

	CheckAllFunc       func(ctx context.Context) (*ports.HealthReport, error)
	CheckComponentFunc func(ctx context.Context, comp ports.Component) (*ports.ComponentReport, error)

	checkAllCalls       int
	checkComponentCalls int
	calledMethods       []string
}

var _ ports.HealthCheckManager = (*MockHealthCheckManager)(nil)

func (m *MockHealthCheckManager) CheckAll(ctx context.Context) (*ports.HealthReport, error) {
	m.mu.Lock()
	m.checkAllCalls++
	m.calledMethods = append(m.calledMethods, "CheckAll")
	fn := m.CheckAllFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx)
	}
	return nil, nil
}

func (m *MockHealthCheckManager) CheckComponent(ctx context.Context, comp ports.Component) (*ports.ComponentReport, error) {
	m.mu.Lock()
	m.checkComponentCalls++
	m.calledMethods = append(m.calledMethods, "CheckComponent")
	fn := m.CheckComponentFunc
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, comp)
	}
	return nil, nil
}

// Snapshot returns a consistent view of call counters and called method names.
// The returned slice is a copy and safe to inspect without holding the lock.
func (m *MockHealthCheckManager) Snapshot() (checkAllCalls int, checkComponentCalls int, methods []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calledMethods))
	copy(out, m.calledMethods)
	return m.checkAllCalls, m.checkComponentCalls, out
}
