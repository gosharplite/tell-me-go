// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

// TestEventQueue_EnqueueNonCritical_CtxDone covers the ctx.Done() branch
// inside enqueueNonCritical. Cancellation is checked with strict priority
// before the non-blocking send, so a full channel never shadows it.
func TestEventQueue_EnqueueNonCritical_CtxDone(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t, withBridgeQueueCapacity(1))

	// TurnStatusEvent sent via sendDirect will be consumed by Listen;
	// the function field must be set up before it arrives.
	f.renderer.LogTurnStatusFn = func(ctx context.Context, status events.TurnStatus) {}

	// Fill the single-slot channel so the send case would block
	f.bridge.queue.(*eventQueue).sendDirect(events.TurnStatusEvent{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Cancellation is checked first — no non-determinism
	err := f.bridge.queue.(*eventQueue).enqueueNonCritical(ctx, events.InferenceStartedEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestEventQueue_EnqueueNonCritical_ActorDead covers the loopCtx.Done() branch
// inside enqueueNonCritical. Actor death is checked after caller cancellation
// but before the non-blocking send, so it always takes priority.
func TestEventQueue_EnqueueNonCritical_ActorDead(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t, withBridgeQueueCapacity(1))

	// TurnStatusEvent sent via sendDirect will be consumed by Listen;
	// the function field must be set up before it arrives.
	f.renderer.LogTurnStatusFn = func(ctx context.Context, status events.TurnStatus) {}

	// Fill the single-slot channel so the send case would block
	f.bridge.queue.(*eventQueue).sendDirect(events.TurnStatusEvent{})

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

	err := f.bridge.queue.(*eventQueue).enqueueCritical(ctx, events.TurnStatusEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestEventQueue_EnqueueCritical_ActorDead covers the pre-select loopCtx.Err()
// guard in enqueueCritical. A dead actor is caught before the blocking send,
// deterministically returning the "uibridge actor is dead" error.
func TestEventQueue_EnqueueCritical_ActorDead(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)

	f.bridge.KillActor()

	err := f.bridge.queue.(*eventQueue).enqueueCritical(context.Background(), events.TurnStatusEvent{})
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

	inSelect := make(chan struct{})
	var once sync.Once
	f.bridge.SetBeforeBlockingSendHook(func() { once.Do(func() { close(inSelect) }) })

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		_ = f.bridge.HandleEvent(ctx, events.TurnStatusEvent{})
		close(done)
	}()

	<-inSelect // deterministic: goroutine has passed both pre-guards, now in select
	cancel()   // mid-flight caller cancellation — guaranteed to hit <-ctx.Done()

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

	inSelect := make(chan struct{})
	var once sync.Once
	f.bridge.SetBeforeBlockingSendHook(func() { once.Do(func() { close(inSelect) }) })

	done := make(chan struct{})
	go func() {
		_ = f.bridge.HandleEvent(context.Background(), events.TurnStatusEvent{})
		close(done)
	}()

	<-inSelect           // deterministic: goroutine has passed both pre-guards, now in select
	f.bridge.KillActor() // mid-flight actor death — guaranteed to hit <-eq.loopCtx.Done()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleEvent did not unblock after actor death")
	}

	f.UnblockLoop()
}

func TestEventQueue_EnqueueEvent_CriticalAccepted(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newEventQueue(slog.New(slog.NewTextHandler(io.Discard, nil)), loopCtx, 1)
	_ = q.enqueueEvent(context.Background(), events.ResponseEvent{})
	select {
	case e := <-q.recv():
		assert.IsType(t, events.ResponseEvent{}, e)
	default:
		t.Error("expected critical event to be enqueued")
	}
}

func TestEventQueue_EnqueueEvent_NonCriticalAccepted(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newEventQueue(slog.New(slog.NewTextHandler(io.Discard, nil)), loopCtx, 1)
	_ = q.enqueueEvent(context.Background(), events.InferenceStartedEvent{})
	select {
	case e := <-q.recv():
		assert.IsType(t, events.InferenceStartedEvent{}, e)
	default:
		t.Error("expected non-critical event to be enqueued")
	}
}

func TestEventQueue_EnqueueEvent_ShedWhenFull(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newEventQueue(slog.New(slog.NewTextHandler(io.Discard, nil)), loopCtx, 1)
	q.sendDirect(events.TurnStarted{})
	_ = q.enqueueEvent(context.Background(), events.InferenceStartedEvent{})
	// Queue should still have only the filler; non-critical event was shed
	e := <-q.recv()
	assert.IsType(t, events.TurnStarted{}, e)
	select {
	case <-q.recv():
		t.Error("queue should be empty after consuming the filler")
	default:
	}
}

func TestEventQueue_EnqueueEvent_CriticalBlocking(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newEventQueue(slog.New(slog.NewTextHandler(io.Discard, nil)), loopCtx, 1)

	// Fill the queue
	q.sendDirect(events.TurnStarted{})

	ctx, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	done := make(chan struct{})
	inSelect := make(chan struct{})
	var once sync.Once
	q.beforeBlockingSendHook = func() { once.Do(func() { close(inSelect) }) }

	go func() {
		defer close(done)
		_ = q.enqueueEvent(ctx, events.ResponseEvent{})
	}()

	<-inSelect // deterministic: goroutine is past pre-guards, now in blocking select

	// Prove that done does not receive a value prematurely
	select {
	case <-done:
		t.Fatal("enqueueEvent returned prematurely, expected it to block")
	default:
		// Expected behavior: it is blocked
	}

	// Explicitly cancel to unblock
	cancel2()

	// Wait for the goroutine to finish
	<-done

	// Verify queue still has only the filler
	e := <-q.recv()
	assert.IsType(t, events.TurnStarted{}, e)
	select {
	case <-q.recv():
		t.Error("Queue should be empty")
	default:
	}
}
