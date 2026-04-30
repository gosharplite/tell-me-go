// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/stretchr/testify/require"
)

func TestAgent_Shutdown_NilDeps_Edge(t *testing.T) {
	t.Parallel()
	a := &agent{}
	err := a.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Expected nil error for nil dependencies, got %v", err)
	}
}

func TestAgent_ApplyConfig_ContextCancellation_Edge(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	a := &agent{
		events:        bus,
		configWatcher: session.NewNoOpConfigWatcher(1000, 5, 10),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := a.ApplyConfig(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}

func TestAgent_ApplyConfig_Publish_Error_Edge(t *testing.T) {
	mockBus := &eventstest.TestEventBus{}
	mockBus.SetPublishErr(context.Canceled)

	a := &agent{
		events:        mockBus,
		configWatcher: session.NewNoOpConfigWatcher(1000, 5, 10),
	}
	a.config.Store(&runtimeConfig{})

	err := a.ApplyConfig(context.Background())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error from applyConfig, got: %v", err)
	}
}

func TestAgent_Shutdown_FlushError_Edge(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mockBus := &eventstest.TestEventBus{}
	flushErr := errors.New("flush failed")
	mockBus.SetFlushErr(flushErr)

	a := &agent{
		events: mockBus,
		logger: logger,
	}

	ctx := context.Background()
	err := a.Shutdown(ctx)

	require.Error(t, err)
	require.ErrorIs(t, err, flushErr)

	// Verify that the error was logged at Debug level
	output := buf.String()
	require.Contains(t, output, "event bus flush incomplete during shutdown")
	require.Contains(t, output, "flush failed")
}
