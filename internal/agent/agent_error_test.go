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
)

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
	a := agent.NewAgentInternal()
	agent.SetEventsForTest(a, bus)
	agent.SetCtxManagerForTest(a, &session.ContextManager{
		History: hm,
	})

	sess := &ports.Session{StartTime: time.Now()}
	err := a.Chat(ctx, sess, "hello")

	if err == nil {
		t.Fatal("Expected error due to config failure/context cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error from applyConfig, got: %v", err)
	}
}
