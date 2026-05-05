// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

// MockHealthCheckManager is a testify mock for ports.HealthCheckManager.
type MockHealthCheckManager struct {
	mock.Mock
}

func (m *MockHealthCheckManager) CheckAll(ctx context.Context) (*ports.HealthReport, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ports.HealthReport), args.Error(1)
}

func (m *MockHealthCheckManager) CheckComponent(ctx context.Context, comp ports.Component) (*ports.ComponentReport, error) {
	args := m.Called(ctx, comp)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ports.ComponentReport), args.Error(1)
}
