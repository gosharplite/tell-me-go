// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"context"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/stretchr/testify/require"
)

func TestAgent_InitConfigFailure_Warning(t *testing.T) {
	t.Parallel()
	client := &testutil.MockLLMClient{}
	h := history.NewManager(persistencetest.NewPlainOSFileSystem(), "", "")
	reg := registry.New()
	sm := &testutil.MockSecurityManager{AllowAll: true}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)

	// Create a cancelled context to force applyConfig to fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var warningEmitted bool
	var mu sync.Mutex
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if su, ok := e.(events.StatusUpdate); ok {
			mu.Lock()
			if su.Level == "warning" && su.Message == "failed to apply initial configuration" {
				warningEmitted = true
			}
			mu.Unlock()
		}
	})

	// New should not crash even if applyConfig fails
	a, err := agent.NewAgent(client, bus, reg,
		agent.WithHistoryManager(h),
		agent.WithProviderName("test-provider"),
		agent.WithSecurityManager(sm),
		agent.WithInitContext(ctx),
	)
	require.NoError(t, err)

	if a == nil {
		t.Fatal("New returned nil agent")
	}

	// Flush the bus to ensure we process all events
	_ = bus.Flush(context.Background())

	mu.Lock()
	emitted := warningEmitted
	mu.Unlock()

	if !emitted {
		t.Error("Expected config-failure warning event to be emitted, but it was not")
	}
}
