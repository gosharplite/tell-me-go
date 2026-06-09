// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockChatter is a hand-rolled test double for ports.Chatter.
// Set *Func fields (ChatFn, SetLimitsFn, SubscribeFn, ShutdownFn)
// to script behaviour. When a *Func field is nil, the method
// returns a sensible zero-value default (nil error / no-op).
type MockChatter struct {
	// Function fields — set before test to script behaviour.
	ChatFn      func(ctx context.Context, s *ports.Session, prompt string) error
	SetLimitsFn func(ctx context.Context, toolTurns, historyTokens, historyTurns int) error
	SubscribeFn func(sub func(context.Context, events.Event))
	ShutdownFn  func(ctx context.Context) error
}

// Chat executes a single conversation turn. When ChatFn is nil the
// default is success (nil error).
func (m *MockChatter) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	if m.ChatFn != nil {
		return m.ChatFn(ctx, s, prompt)
	}
	return nil
}

// SetLimits configures tool-turn, history-token, and history-turn
// budgets. When SetLimitsFn is nil the default is success (nil error).
func (m *MockChatter) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	if m.SetLimitsFn != nil {
		return m.SetLimitsFn(ctx, toolTurns, historyTokens, historyTurns)
	}
	return nil
}

// Subscribe registers an event subscriber. The subscriber callback is
// forwarded to SubscribeFn when set; otherwise it is silently discarded.
// Tests that need to capture the subscriber must set SubscribeFn.
func (m *MockChatter) Subscribe(sub func(context.Context, events.Event)) {
	if m.SubscribeFn != nil {
		m.SubscribeFn(sub)
	}
}

// Shutdown initiates graceful shutdown. When ShutdownFn is nil the
// default is success (nil error).
func (m *MockChatter) Shutdown(ctx context.Context) error {
	if m.ShutdownFn != nil {
		return m.ShutdownFn(ctx)
	}
	return nil
}
