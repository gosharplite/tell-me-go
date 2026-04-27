// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

// MockChatter is a testify-based mock of ports.Chatter. Configure
// behaviour with mock.On(...) and assert calls with mock.AssertCalled
// or mock.AssertExpectations.
type MockChatter struct {
	mock.Mock
}

func (m *MockChatter) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	args := m.Called(ctx, s, prompt)
	return args.Error(0)
}

func (m *MockChatter) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	args := m.Called(ctx, toolTurns, historyTokens, historyTurns)
	return args.Error(0)
}

func (m *MockChatter) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}

func (m *MockChatter) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
