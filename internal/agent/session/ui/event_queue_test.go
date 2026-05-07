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
// inside enqueueNonCritical. This branch is unreachable via HandleEvent
// (its guard intercepts cancelled contexts first), so we call the queue
// directly with a full channel and a cancelled context. Go's select with
// default picks randomly between ctx.Done() and default when both are
// ready; we loop to guarantee hitting ctx.Done() at least once.
func TestEventQueue_EnqueueNonCritical_CtxDone(t *testing.T) {
	// NOT parallel — relies on repeated attempts to hit a non-deterministic branch
	f := newUIBridgeFixture(t, withBridgeQueueCapacity(1))

	// Fill the single-slot channel
	f.bridge.queue.sendDirect(events.TurnStatusEvent{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// When channel is full and ctx is cancelled, Go's select picks between
	// ctx.Done() and default. Loop until we hit ctx.Done().
	var gotCtxErr bool
	for i := 0; i < 100; i++ {
		err := f.bridge.queue.enqueueNonCritical(ctx, events.InferenceStartedEvent{})
		if err != nil {
			assert.ErrorIs(t, err, context.Canceled)
			gotCtxErr = true
			break
		}
	}
	assert.True(t, gotCtxErr, "expected ctx.Done() branch to be reached within 100 attempts")
}

// TestEventQueue_EnqueueNonCritical_ActorDead covers the loopCtx.Done() branch
// inside enqueueNonCritical. When the actor's internal context is cancelled
// (loopCtx), non-critical events that reach the select statement return an
// "uibridge actor is dead" error.
//
// The channel is pre-filled and the actor killed so the send case blocks.
// Go's select picks non-deterministically between loopCtx.Done() and default
// when both are ready; we loop to guarantee hitting the error branch.
func TestEventQueue_EnqueueNonCritical_ActorDead(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t, withBridgeQueueCapacity(1))

	// Fill the single-slot channel so the send case in enqueueNonCritical blocks
	f.bridge.queue.sendDirect(events.TurnStatusEvent{})

	// Kill the actor — loopCtx.Done() is now ready
	f.bridge.KillActor()

	// Non-critical event hits enqueueNonCritical: send blocked, loopCtx.Done() ready.
	// Go's select picks randomly between loopCtx.Done() and default; loop to win.
	var gotActorDead bool
	for i := 0; i < 100; i++ {
		err := f.bridge.HandleEvent(context.Background(), events.TokenLimitReachedEvent{})
		if err != nil {
			assert.Contains(t, err.Error(), "uibridge actor is dead")
			gotActorDead = true
			break
		}
	}
	assert.True(t, gotActorDead, "expected loopCtx.Done() branch to be reached within 100 attempts")
}
