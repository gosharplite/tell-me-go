// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockChatter is a hand-rolled test double for ports.Chatter.
// Override function fields (ChatFn, SetLimitsFn, SubscribeFn,
// ShutdownFn) to script behaviour. All methods record invocation
// counts and names accessible via Snapshot().
type MockChatter struct {
	mu              sync.Mutex
	calledChat      int
	calledSetLimits int
	calledSubscribe int
	calledShutdown  int
	calledMethods   []string

	// Function fields — set before test to script behaviour.
	ChatFn      func(ctx context.Context, s *ports.Session, prompt string) error
	SetLimitsFn func(ctx context.Context, toolTurns, historyTokens, historyTurns int) error
	SubscribeFn func(sub func(context.Context, events.Event))
	ShutdownFn  func(ctx context.Context) error
}

// Snapshot returns a point-in-time view of invocation counters and
// method call order. The returned slice is a defensive copy safe for
// inspection without holding the mutex.
func (m *MockChatter) Snapshot() (chat, setLimits, subscribe, shutdown int, methods []string) {
	m.mu.Lock()
	chat = m.calledChat
	setLimits = m.calledSetLimits
	subscribe = m.calledSubscribe
	shutdown = m.calledShutdown
	methods = make([]string, len(m.calledMethods))
	copy(methods, m.calledMethods)
	m.mu.Unlock()
	return
}

// Chat executes a single conversation turn. When ChatFn is nil the
// default is success (nil error).
func (m *MockChatter) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	m.mu.Lock()
	m.calledChat++
	m.calledMethods = append(m.calledMethods, "Chat")
	m.mu.Unlock()

	if m.ChatFn != nil {
		return m.ChatFn(ctx, s, prompt)
	}
	return nil
}

// SetLimits configures tool-turn, history-token, and history-turn
// budgets. When SetLimitsFn is nil the default is success (nil error).
func (m *MockChatter) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	m.mu.Lock()
	m.calledSetLimits++
	m.calledMethods = append(m.calledMethods, "SetLimits")
	m.mu.Unlock()

	if m.SetLimitsFn != nil {
		return m.SetLimitsFn(ctx, toolTurns, historyTokens, historyTurns)
	}
	return nil
}

// Subscribe registers an event subscriber. The subscriber callback is
// forwarded to SubscribeFn when set; otherwise it is silently discarded.
// Tests that need to capture the subscriber must set SubscribeFn.
func (m *MockChatter) Subscribe(sub func(context.Context, events.Event)) {
	m.mu.Lock()
	m.calledSubscribe++
	m.calledMethods = append(m.calledMethods, "Subscribe")
	m.mu.Unlock()

	if m.SubscribeFn != nil {
		m.SubscribeFn(sub)
	}
}

// Shutdown initiates graceful shutdown. When ShutdownFn is nil the
// default is success (nil error).
func (m *MockChatter) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.calledShutdown++
	m.calledMethods = append(m.calledMethods, "Shutdown")
	m.mu.Unlock()

	if m.ShutdownFn != nil {
		return m.ShutdownFn(ctx)
	}
	return nil
}
