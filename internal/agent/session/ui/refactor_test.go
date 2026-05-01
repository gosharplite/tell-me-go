// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

func TestUIBridge_HandleEvent_SafetyWrapper(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		setup         func(q *eventQueue)
		ctx           func() context.Context
		event         events.Event
		expectEnqueue bool
	}{
		{
			name: "bridge is closed",
			setup: func(q *eventQueue) {
				q.closeInput()
			},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: false,
		},
		{
			name:  "caller context is cancelled",
			setup: func(q *eventQueue) {},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			event:         events.ResponseEvent{},
			expectEnqueue: false,
		},
		{
			name:          "normal case - enqueued",
			setup:         func(q *eventQueue) {},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loopCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			q := newEventQueue(slog.New(slog.NewTextHandler(io.Discard, nil)), loopCtx, 10)

			tt.setup(q)
			ctx := tt.ctx()

			var enqueued bool
			if !q.isInputClosed() && ctx.Err() == nil {
				_ = q.enqueueEvent(ctx, tt.event)
				enqueued = true
			}
			assert.Equal(t, tt.expectEnqueue, enqueued)

			if tt.expectEnqueue {
				assert.Equal(t, tt.event, <-q.recv())
			} else {
				select {
				case e, ok := <-q.recv():
					if ok {
						t.Errorf("unexpected %v", e)
					}
				default:
				}
			}
		})
	}
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
	started := make(chan struct{})
	go func() {
		close(started)
		defer close(done)
		_ = q.enqueueEvent(ctx, events.ResponseEvent{})
	}()

	<-started

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
