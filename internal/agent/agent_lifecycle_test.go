// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asAgent is a same-package shorthand that converts a ports.Chatter back
// to *agent for direct field access. Cleaner than litter the test body
// with type assertions, and only legal because this file is package agent.
func asAgent(t *testing.T, c ports.Chatter) *agent {
	t.Helper()
	a, ok := c.(*agent)
	require.True(t, ok, "ports.Chatter is not the production *agent type")
	return a
}

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

	// Same-package: read internal state via direct field access.
	a := asAgent(t, chatter)
	assert.NotNil(t, a.ctxManager)
	assert.NotNil(t, a.configWatcher)
	assert.Equal(t, bus, a.events)
}

func TestAgent_InternalState_MutationAndReadback(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm),
		WithProviderName("test-provider"), WithPricing("test-model", "test-mode", nil))
	require.NoError(t, err)

	a := asAgent(t, chatter)

	// Same-package mutation/readback — no bridge needed.
	bus2 := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	a.events = bus2
	assert.Equal(t, bus2, a.events)

	a.tracker = &agenttest.MockCostTracker{}
	assert.NotNil(t, a.tracker)

	// applyConfig is unexported and same-package callable.
	err = a.applyConfig(ctx)
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

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm),
		WithProviderName("test-provider"), WithPricing("test-model", "test-mode", nil))
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

	// Verify internal runtimeConfig update — direct field access since
	// this file is package agent.
	a := asAgent(t, chatter)
	rc := a.config.Load()
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
		tl.CloseFunc = func() error { return nil }

		chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithTurnsLogger(tl))
		require.NoError(t, err)

		err = chatter.Shutdown(ctx)
		assert.NoError(t, err)
		assert.Equal(t, 1, tl.Snapshot()["Close"], "Close should have been called once")
	})

	t.Run("Graceful handling of nil components", func(t *testing.T) {
		// Create an agent manually with some nil components — same-package
		// composite literal works because we live in package agent.
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
		tl.CloseFunc = func() error { return assert.AnError }

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

func TestAgent_BareConstruction_FieldAssignment(t *testing.T) {
	// Same-package construction of a bare agent. Replaces the previous
	// TestAgent_InternalAccessor_Remaining which exercised the now-removed
	// Set*/Get* accessors.
	a := &agent{}

	cw := domain_config.NewNoOpConfigWatcher(0, 0, 0)
	a.configWatcher = cw
	assert.Equal(t, cw, a.configWatcher)

	logger := slog.Default()
	a.logger = logger
	assert.Equal(t, logger, a.getLogger())

	rc := &runtimeConfig{Model: "test-model"}
	a.config.Store(rc)
	assert.Equal(t, rc, a.config.Load())
}

func TestAsInternal_Nil(t *testing.T) {
	assert.Nil(t, AsInternal(nil))
}

func TestAgent_GetLogger_Default(t *testing.T) {
	a := &agent{}
	assert.NotNil(t, a.getLogger())
}

func TestAgent_InitComponents_ConfigWatcher(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	cw := domain_config.NewNoOpConfigWatcher(100, 10, 20)
	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm), WithConfigWatcher(cw))
	require.NoError(t, err)

	a := asAgent(t, chatter)
	assert.NotNil(t, a.configWatcher)
	// Verify the injected watcher is the one we passed
	assert.Equal(t, cw, a.configWatcher)
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
