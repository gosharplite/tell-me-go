// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agentinternal"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
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
	eventstest.CleanupBus(t, bus)

	// Construct a deliberately bare agent: no engine, no executor, no
	// configWatcher. We only set the two fields applyConfig and Chat
	// touch on the path under test, so cancelling ctx forces applyConfig
	// to return context.Canceled — the assertion below.
	a := agentinternal.NewBareAgent()
	a.SetEventsForTest(bus)
	a.SetCtxManagerForTest(&sessctx.Manager{
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
