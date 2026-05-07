// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

// TestUIBridge_HandleEvent_CallerContextCancelled covers the early guard in
// HandleEvent that catches cancelled contexts before they reach the queue.
func TestUIBridge_HandleEvent_CallerContextCancelled(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.bridge.HandleEvent(ctx, events.InferenceStartedEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestEventQueue_EnqueueNonCritical_CtxDone covers the ctx.Done() branch
// inside enqueueNonCritical. Cancellation is checked with strict priority
// before the non-blocking send, so a full channel never shadows it.
func TestEventQueue_EnqueueNonCritical_CtxDone(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t, withBridgeQueueCapacity(1))

	// Fill the single-slot channel so the send case would block
	f.bridge.queue.sendDirect(events.TurnStatusEvent{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Cancellation is checked first — no non-determinism
	err := f.bridge.queue.enqueueNonCritical(ctx, events.InferenceStartedEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestEventQueue_EnqueueNonCritical_ActorDead covers the loopCtx.Done() branch
// inside enqueueNonCritical. Actor death is checked after caller cancellation
// but before the non-blocking send, so it always takes priority.
func TestEventQueue_EnqueueNonCritical_ActorDead(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t, withBridgeQueueCapacity(1))

	// Fill the single-slot channel so the send case would block
	f.bridge.queue.sendDirect(events.TurnStatusEvent{})

	// Kill the actor — loopCtx.Err() will be non-nil
	f.bridge.KillActor()

	// Actor death is checked before send — deterministic
	err := f.bridge.HandleEvent(context.Background(), events.TokenLimitReachedEvent{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "uibridge actor is dead")
}
