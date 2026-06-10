// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/require"
)

// TestAgent_InitConfigFailure_Warning verifies that NewAgent now
// returns an error when initial configuration application fails,
// instead of swallowing it. This is the G8 fix — previously the
// error was swallowed and only a non-blocking StatusUpdate warning
// was emitted. Now the caller receives a proper error and the
// agent is nil.
func TestAgent_InitConfigFailure_Warning(t *testing.T) {
	t.Parallel()
	client := &agenttest.MockLLMClient{}
	h := history.NewManager(persistencetest.NewPlainOSFileSystem(), "", "")
	reg := registry.New()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	// Create a cancelled context to force applyConfig to fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// NewAgent now returns an error (instead of swallowing it and
	// emitting a warning). A nil agent means no partially-initialized
	// agent leaks to the caller.
	a, err := agent.NewAgent(client, bus, reg,
		agent.WithHistoryManager(h),
		agent.WithProviderName("test-provider"),
		agent.WithPricing("test-model", "test-mode", nil),
		agent.WithSecurityManager(sm),
		agent.WithInitContext(ctx),
	)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled),
		"expected error to wrap context.Canceled, got: %v", err)
	require.Contains(t, err.Error(), "failed to apply initial configuration")
	require.Nil(t, a, "expected nil agent when initial config fails")
}
