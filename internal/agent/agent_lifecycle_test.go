// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgent_Initialization(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	// Use all options to increase coverage
	chatter, err := NewAgent(gw, bus, reg,
		WithSecurityManager(sm),
		WithInternalTools(),
		WithHistoryManager(&mockHistoryManager{}),
		WithSessionProvider(&agenttest.MockSessionProvider{}),
		WithSummarizer(&agenttest.MockSummarizer{}),
		WithPricing("model", "mode", map[string]pricing.ModelPricing{"model": {Hit: 1.0}}),
		WithSessionCostTracker(&agenttest.MockCostTracker{}),
		WithClock(&agenttest.MockClock{}),
		WithProviderName("provider"),
		WithLogger(slog.Default()),
	)
	require.NoError(t, err)
	assert.NotNil(t, chatter)

	// Verify internal components via export_test.go helpers
	assert.NotNil(t, CtxManagerForTest(chatter))
	assert.NotNil(t, ConfigWatcherForTest(chatter))
	assert.Equal(t, bus, EventsForTest(chatter))
}

func TestAgent_InternalAccessor(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	// Test events override via export_test.go setter
	bus2 := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	SetEventsForTest(chatter, bus2)
	assert.Equal(t, bus2, EventsForTest(chatter))

	// Test tracker override via export_test.go setter
	SetTrackerForTest(chatter, &agenttest.MockCostTracker{})

	// Test remaining InternalAccessor methods
	a := AsInternal(chatter)
	require.NotNil(t, a)
	assert.NotNil(t, a.GetTracker())

	// Test ApplyConfig
	err = a.ApplyConfig(ctx)
	assert.NoError(t, err)
}

func TestAgent_Subscribe(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	var capturedEvent events.Event
	chatter.Subscribe(func(ctx context.Context, e events.Event) {
		capturedEvent = e
	})

	err = bus.Publish(ctx, events.StatusUpdate{Message: "test"})
	require.NoError(t, err)
	assert.NotNil(t, capturedEvent)
}

func TestAgent_SetLimits(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	var capturedEvent events.Event
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if e.Type() == "ConfigUpdated" {
			capturedEvent = e
		}
	})

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	err = chatter.SetLimits(ctx, 5, 1000, 10)
	require.NoError(t, err)

	// Verify event publication
	require.NotNil(t, capturedEvent)
	cfgEvent, ok := capturedEvent.(events.ConfigUpdated)
	require.True(t, ok)
	assert.Equal(t, 5, cfgEvent.Limits.MaxToolTurns)
	assert.Equal(t, 1000, cfgEvent.Limits.MaxHistoryTokens)
	assert.Equal(t, 10, cfgEvent.Limits.MaxHistoryTurns)

	// Verify internal runtimeConfig update via export_test.go getter
	rc := RuntimeConfigForTest(chatter)
	assert.Equal(t, 5, rc.Limits.MaxToolTurns)
	assert.Equal(t, 1000, rc.Limits.MaxHistoryTokens)
	assert.Equal(t, 10, rc.Limits.MaxHistoryTurns)
}

func TestAgent_Shutdown(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()

	t.Run("Normal shutdown", func(t *testing.T) {
		sm := &mockSecurityManager{AllowAll: true}
		tl := &agenttest.MockTurnsLogger{}
		tl.On("Close").Return(nil)

		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithTurnsLogger(tl))
		require.NoError(t, err)

		err = chatter.Shutdown(ctx)
		assert.NoError(t, err)
		tl.AssertCalled(t, "Close")
	})

	t.Run("Graceful handling of nil components", func(t *testing.T) {
		// Create an agent manually with some nil components
		a := &agent{
			logger:      nil, // Should default to slog.Default()
			events:      nil,
			turnsLogger: nil,
		}

		err := a.Shutdown(ctx)
		assert.NoError(t, err)
	})

	t.Run("TurnsLogger.Close error", func(t *testing.T) {
		sm := &mockSecurityManager{AllowAll: true}
		tl := &agenttest.MockTurnsLogger{}
		tl.On("Close").Return(assert.AnError)

		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithTurnsLogger(tl))
		require.NoError(t, err)

		err = chatter.Shutdown(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), assert.AnError.Error())
	})

	t.Run("EventBus.Flush error", func(t *testing.T) {
		sm := &mockSecurityManager{AllowAll: true}
		// Use a cancelled context to force Flush to fail
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
		require.NoError(t, err)

		err = chatter.Shutdown(cancelCtx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("EventBus.Shutdown error", func(t *testing.T) {
		sm := &mockSecurityManager{AllowAll: true}
		bus := &eventstest.MockEventBus{}
		bus.SetShutdownErr(assert.AnError)

		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
		require.NoError(t, err)

		err = chatter.Shutdown(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), assert.AnError.Error())
	})
}

func TestNewAgent_Errors(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	t.Run("Missing security manager", func(t *testing.T) {
		chatter, err := NewAgent(gw, bus, reg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "security manager is required")
		assert.Nil(t, chatter)
	})

	t.Run("Internal tools registration failure", func(t *testing.T) {
		reg.SetRegisterErr(assert.AnError)
		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithInternalTools())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to register internal tools")
		assert.Nil(t, chatter)
		reg.SetRegisterErr(nil) // Reset
	})

	t.Run("Initial config apply failure", func(t *testing.T) {
		// Use a cancelled context to force applyConfig to fail
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithInitContext(cancelCtx))
		// NewAgent doesn't return error if applyConfig fails, it just emits a warning
		assert.NoError(t, err)
		assert.NotNil(t, chatter)
	})
}

func TestAgent_InternalAccessor_Remaining(t *testing.T) {
	chatter := &agent{}
	a := AsInternal(chatter)
	require.NotNil(t, a)

	cw := session.NewNoOpConfigWatcher(0, 0, 0)
	SetConfigWatcherForTest(chatter, cw)
	assert.Equal(t, cw, ConfigWatcherForTest(chatter))

	logger := slog.Default()
	SetLoggerForTest(chatter, logger)
	// getLogger is private, but we can call it via other methods if needed
	assert.Equal(t, logger, chatter.getLogger())

	rc := &runtimeConfig{Model: "test-model"}
	SetRuntimeConfigForTest(chatter, rc)
	assert.Equal(t, rc, RuntimeConfigForTest(chatter))
}

func TestAsInternal_Nil(t *testing.T) {
	assert.Nil(t, AsInternal(nil))
}

func TestAgent_GetLogger_Default(t *testing.T) {
	a := &agent{}
	assert.NotNil(t, a.getLogger())
}

func TestAgent_InitComponents_FileConfigWatcher(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	loader := &agenttest.MockConfigLoader{}
	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithLoader(loader))
	require.NoError(t, err)

	assert.NotNil(t, ConfigWatcherForTest(chatter))
	// Verify it's not the no-op one if we can, but at least we covered the branch
}

func TestAgent_Chat_AddContentError(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	hm := &mockHistoryManager{}
	hm.AddContentFunc = func(ctx context.Context, content *domain_llm.Content) error {
		return assert.AnError
	}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithHistoryManager(hm))
	require.NoError(t, err)

	err = chatter.Chat(ctx, &ports.Session{}, "hello")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize session history")
}

func TestAgent_Chat_ApplyConfigError(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	err = chatter.Chat(cancelCtx, &ports.Session{}, "hello")
	assert.Error(t, err)
}
