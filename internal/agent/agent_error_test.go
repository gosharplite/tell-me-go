// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
)

// allowAllSM is a minimal security.Manager that permits everything.
type allowAllSM struct{}

func (allowAllSM) IsPathSafe(string) (string, error) { return "", nil }
func (allowAllSM) IsPathWritable(string) (string, error) {
	return "", nil
}
func (allowAllSM) Authorize(context.Context, string, string, string, bool) (bool, error) {
	return true, nil
}
func (allowAllSM) LogAudit(string, ...any)                   {}
func (allowAllSM) TerminalLock()                              {}
func (allowAllSM) TerminalUnlock()                            {}
func (allowAllSM) Prompt(string)                              {}
func (allowAllSM) Warn(string)                                {}
func (allowAllSM) Confirm(context.Context, string) (bool, error) { return true, nil }
func (allowAllSM) ReadLine(context.Context) (string, error)   { return "", nil }
func (allowAllSM) IsCommandAllowed(string) bool               { return true }
func (allowAllSM) IsBypassActive() bool                       { return false }
func (allowAllSM) Close() error                               { return nil }

var _ security.Manager = allowAllSM{}

func TestAgent_ConfigFailure(t *testing.T) {
	t.Parallel()
	// Create a context that we can cancel

	ctx, cancel := context.WithCancel(context.Background())

	hm := &agenttest.MockHistoryManager{
		AddContentFunc: func(c context.Context, content *llm.Content) error {
			// Cancel context right after AddContent succeeds so applyConfig fails
			cancel()
			return nil
		},
	}

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)

	a := agent.NewAgentBuilder(t).
		WithGateway(&agenttest.MockGateway{}).
		WithEventBus(bus).
		WithRegistry(agenttest.NewMockToolRegistry()).
		WithSecurityManager(allowAllSM{}).
		WithHistoryManager(hm).
		WithCtxManager(&session.ContextManager{History: hm}).
		Build()

	sess := &ports.Session{StartTime: time.Now()}
	err := a.Chat(ctx, sess, "hello")

	if err == nil {
		t.Fatal("Expected error due to config failure/context cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error from applyConfig, got: %v", err)
	}
}
