// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"sync"
	"testing"

	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	security_impl "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/require"
)

func TestAgent_InitConfigFailure_Warning(t *testing.T) {
	t.Parallel()
	client := &mockLLMClient{}
	h := history.NewManager(infrapersistence.NewOSFileSystem(), "", "")
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)

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
	a, err := NewAgent(client, bus, h, "test-provider", reg, sm, withInitContext(ctx))
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
