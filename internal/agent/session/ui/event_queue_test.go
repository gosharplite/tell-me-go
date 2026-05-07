// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"testing"
	"time"

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

// TestEventQueue_EnqueueCritical_CallerContextCancelled covers the pre-select
// ctx.Err() guard in enqueueCritical. An already-cancelled context is caught
// before the blocking send, deterministically returning the cancellation error.
func TestEventQueue_EnqueueCritical_CallerContextCancelled(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.bridge.queue.enqueueCritical(ctx, events.TurnStatusEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestEventQueue_EnqueueCritical_ActorDead covers the pre-select loopCtx.Err()
// guard in enqueueCritical. A dead actor is caught before the blocking send,
// deterministically returning the "uibridge actor is dead" error.
func TestEventQueue_EnqueueCritical_ActorDead(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)

	f.bridge.KillActor()

	err := f.bridge.queue.enqueueCritical(context.Background(), events.TurnStatusEvent{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "uibridge actor is dead")
}

// TestEventQueue_EnqueueCritical_MidFlightCallerCancel covers the select's
// ctx.Done() case inside enqueueCritical. The caller context is alive at
// entry (passing the pre-guard) but gets cancelled while the send is blocked.
func TestEventQueue_EnqueueCritical_MidFlightCallerCancel(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)
	f.BlockLoop(t)
	f.FillQueue(events.TurnStatusEvent{})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		close(started)
		_ = f.bridge.HandleEvent(ctx, events.TurnStatusEvent{})
		close(done)
	}()

	<-started
	// Give the goroutine time to pass HandleEvent's guard and the pre-guards
	time.Sleep(10 * time.Millisecond)

	cancel() // mid-flight caller cancellation

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleEvent did not unblock after caller context cancellation")
	}

	f.UnblockLoop()
}

// TestEventQueue_EnqueueCritical_MidFlightActorDeath covers the select's
// loopCtx.Done() case inside enqueueCritical. The actor is alive at entry
// (passing the pre-guard) but killed while the send is blocked.
func TestEventQueue_EnqueueCritical_MidFlightActorDeath(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)
	f.BlockLoop(t)
	f.FillQueue(events.TurnStatusEvent{})

	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		close(started)
		_ = f.bridge.HandleEvent(context.Background(), events.TurnStatusEvent{})
		close(done)
	}()

	<-started
	// Give the goroutine time to pass HandleEvent's guard and the pre-guards
	time.Sleep(10 * time.Millisecond)

	f.bridge.KillActor() // mid-flight actor death

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleEvent did not unblock after actor death")
	}

	f.UnblockLoop()
}
