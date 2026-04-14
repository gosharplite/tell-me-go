// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgent_Initialization(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

	// Use all options to increase coverage
	chatter, err := NewAgent(gw, bus, reg,
		WithSecurityManager(sm),
		WithInternalTools(),
		WithHistoryManager(&MockHistoryManager{}),
		WithSessionProvider(&testutil.MockSessionProvider{}),
		WithSummarizer(&testutil.MockSummarizer{}),
		WithPricing("model", "mode", map[string]pricing.ModelPricing{"model": {Hit: 1.0}}),
		WithSessionCostTracker(&testutil.MockCostTracker{}),
		WithClock(&testutil.MockClock{}),
		WithProviderName("provider"),
		WithLogger(slog.Default()),
	)
	require.NoError(t, err)
	assert.NotNil(t, chatter)

	// Use AsInternal to verify internal components
	a := AsInternal(chatter)
	require.NotNil(t, a)

	assert.NotNil(t, a.GetCtxManager())
	assert.NotNil(t, a.GetConfigWatcher())

	assert.Equal(t, bus, a.GetEvents())
}

func TestAgent_InternalAccessor(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	a := AsInternal(chatter)
	require.NotNil(t, a)

	// Test setters/getters
	bus2 := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	a.SetEvents(bus2)
	assert.Equal(t, bus2, a.GetEvents())

	a.SetTracker(&testutil.MockCostTracker{})
	assert.NotNil(t, a.GetTracker())

	// Test ApplyConfig
	err = a.ApplyConfig(ctx)
	assert.NoError(t, err)
}

func TestAgent_Subscribe(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	var capturedEvent events.Event
	chatter.Subscribe(func(ctx context.Context, e events.Event) {
		capturedEvent = e
	})

	bus.Publish(ctx, events.StatusUpdate{Message: "test"})
	assert.NotNil(t, capturedEvent)
}

func TestAgent_SetLimits(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

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

	// Verify internal runtimeConfig update
	a := AsInternal(chatter)
	rc := a.GetRuntimeConfig().(*runtimeConfig)
	assert.Equal(t, 5, rc.Limits.MaxToolTurns)
	assert.Equal(t, 1000, rc.Limits.MaxHistoryTokens)
	assert.Equal(t, 10, rc.Limits.MaxHistoryTurns)
}

func TestAgent_SetTieredThreshold(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

	var capturedEvent events.Event
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if e.Type() == "ConfigUpdated" {
			capturedEvent = e
		}
	})

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	err = chatter.SetTieredThreshold(ctx, 500)
	require.NoError(t, err)

	// Verify event publication
	require.NotNil(t, capturedEvent)
	cfgEvent, ok := capturedEvent.(events.ConfigUpdated)
	require.True(t, ok)
	assert.Equal(t, 500, cfgEvent.Limits.TieredThreshold)

	// Verify internal runtimeConfig update
	a := AsInternal(chatter)
	rc := a.GetRuntimeConfig().(*runtimeConfig)
	assert.Equal(t, 500, rc.Limits.TieredThreshold)
}

func TestAgent_Shutdown(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()

	t.Run("Normal shutdown", func(t *testing.T) {
		sm := &MockSecurityManager{AllowAll: true}
		tl := &testutil.MockTurnsLogger{}
		tl.On("Close").Return(nil)

		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithTurnsLogger(tl))
		require.NoError(t, err)

		err = chatter.Shutdown(ctx)
		assert.NoError(t, err)
		tl.AssertCalled(t, "Close")
	})

	t.Run("Graceful handling of nil components", func(t *testing.T) {
		// Create an agent manually with some nil components
		a := &Agent{
			logger:      nil, // Should default to slog.Default()
			events:      nil,
			turnsLogger: nil,
		}

		err := a.Shutdown(ctx)
		assert.NoError(t, err)
	})

	t.Run("TurnsLogger.Close error", func(t *testing.T) {
		sm := &MockSecurityManager{AllowAll: true}
		tl := &testutil.MockTurnsLogger{}
		tl.On("Close").Return(assert.AnError)

		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithTurnsLogger(tl))
		require.NoError(t, err)

		err = chatter.Shutdown(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), assert.AnError.Error())
	})

	t.Run("EventBus.Flush error", func(t *testing.T) {
		sm := &MockSecurityManager{AllowAll: true}
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
		sm := &MockSecurityManager{AllowAll: true}
		bus := &testutil.MockEventBus{}
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
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

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
	chatter := &Agent{}
	a := AsInternal(chatter)
	require.NotNil(t, a)

	cw := session.NewNoOpConfigWatcher(0, 0, 0)
	a.SetConfigWatcher(cw)
	assert.Equal(t, cw, a.GetConfigWatcher())

	logger := slog.Default()
	a.SetLogger(logger)
	// getLogger is private, but we can call it via other methods if needed
	assert.Equal(t, logger, chatter.getLogger())

	rc := &runtimeConfig{Model: "test-model"}
	a.SetRuntimeConfig(rc)
	assert.Equal(t, rc, a.GetRuntimeConfig())
}

func TestAsInternal_Nil(t *testing.T) {
	assert.Nil(t, AsInternal(nil))
}

func TestAgent_GetLogger_Default(t *testing.T) {
	a := &Agent{}
	assert.NotNil(t, a.getLogger())
}

func TestAgent_InitComponents_FileConfigWatcher(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

	loader := &testutil.MockConfigLoader{}
	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithLoader(loader))
	require.NoError(t, err)

	a := AsInternal(chatter)
	assert.NotNil(t, a.GetConfigWatcher())
	// Verify it's not the no-op one if we can, but at least we covered the branch
}

func TestAgent_Chat_AddContentError(t *testing.T) {
	ctx := context.Background()
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

	hm := &MockHistoryManager{}
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
	gw := &testutil.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := testutil.NewMockToolRegistry()
	sm := &MockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	err = chatter.Chat(cancelCtx, &ports.Session{}, "hello")
	assert.Error(t, err)
}
