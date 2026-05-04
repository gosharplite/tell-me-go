// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgent_ApplyConfig_ExportedWrapper exercises the exported
// (*agent).ApplyConfig wrapper at agent.go:348 directly through the
// public InternalAccessor interface (returned by agent.AsInternal).
//
// The wrapper is a one-line passthrough to the private (*agent).applyConfig.
// Behavioural coverage of applyConfig is provided elsewhere
// (TestAgent_Chat_ApplyConfigError, TestAgent_Chat_ApplyConfigFailure,
// TestAgent_InternalState_MutationAndReadback). This test exists solely
// to bring the exported wrapper to 100% line coverage and provide a
// regression anchor for the agentinternal bridge entry point declared
// by ADR-022.
//
// Two cases are exercised:
//  1. Live context — wrapper returns nil from a successful applyConfig.
//  2. Canceled context — wrapper returns context.Canceled, exercising the
//     ctx.Err() early-return path through the wrapper boundary.
//
// See GitHub issue #140.
func TestAgent_ApplyConfig_ExportedWrapper(t *testing.T) {
	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg, WithSecurityManager(sm))
	require.NoError(t, err)

	// AsInternal returns the agent typed as InternalAccessor — the same
	// interface that agentinternal.AgentInternal.ApplyConfig delegates
	// through in production test paths.
	accessor := AsInternal(chatter)
	require.NotNil(t, accessor, "AsInternal must return a non-nil accessor for the production *agent type")

	t.Run("live context returns nil", func(t *testing.T) {
		err := accessor.ApplyConfig(ctx)
		assert.NoError(t, err)
	})

	t.Run("canceled context returns context.Canceled", func(t *testing.T) {
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()

		err := accessor.ApplyConfig(canceledCtx)
		assert.ErrorIs(t, err, context.Canceled,
			"wrapper must propagate the ctx.Err() early-return from applyConfig")
	})
}
