// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

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
		setup         func(b *UIBridge)
		ctx           func() context.Context
		event         events.Event
		expectEnqueue bool
	}{
		{
			name: "bridge is closed",
			setup: func(b *UIBridge) {
				b.isClosed.Store(true)
			},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: false,
		},
		{
			name:  "caller context is cancelled",
			setup: func(b *UIBridge) {},
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
			setup:         func(b *UIBridge) {},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Manual setup of UIBridge to avoid starting background loop
			loopCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := &UIBridge{
				loopCtx: loopCtx,
				eventCh: make(chan events.Event, 10),
				logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			}

			tt.setup(b)

			_ = b.HandleEvent(tt.ctx(), tt.event)

			if tt.expectEnqueue {
				select {
				case e := <-b.eventCh:
					assert.Equal(t, tt.event, e)
				default:
					t.Error("Expected event to be enqueued")
				}
			} else {
				select {
				case e := <-b.eventCh:
					t.Errorf("Expected NO event to be enqueued, but got %v", e)
				default:
					// Success
				}
			}
		})
	}
}

func TestUIBridge_EnqueueEvent_CriticalAccepted(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &UIBridge{
		loopCtx: loopCtx,
		eventCh: make(chan events.Event, 1),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_ = b.enqueueEvent(context.Background(), events.ResponseEvent{})
	select {
	case e := <-b.eventCh:
		assert.IsType(t, events.ResponseEvent{}, e)
	default:
		t.Error("expected critical event to be enqueued")
	}
}

func TestUIBridge_EnqueueEvent_NonCriticalAccepted(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &UIBridge{
		loopCtx: loopCtx,
		eventCh: make(chan events.Event, 1),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	_ = b.enqueueEvent(context.Background(), events.InferenceStartedEvent{})
	select {
	case e := <-b.eventCh:
		assert.IsType(t, events.InferenceStartedEvent{}, e)
	default:
		t.Error("expected non-critical event to be enqueued")
	}
}

func TestUIBridge_EnqueueEvent_ShedWhenFull(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &UIBridge{
		loopCtx: loopCtx,
		eventCh: make(chan events.Event, 1),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	b.eventCh <- events.TurnStarted{}
	_ = b.enqueueEvent(context.Background(), events.InferenceStartedEvent{})
	assert.Equal(t, 1, len(b.eventCh))
	e := <-b.eventCh
	assert.IsType(t, events.TurnStarted{}, e)
}

func TestUIBridge_EnqueueEvent_CriticalBlocking(t *testing.T) {
	t.Parallel()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &UIBridge{
		loopCtx: loopCtx,
		eventCh: make(chan events.Event, 1),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Fill the queue
	b.eventCh <- events.TurnStarted{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		close(started)
		defer close(done)
		_ = b.enqueueEvent(ctx, events.ResponseEvent{})
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
	cancel()

	// Wait for the goroutine to finish
	<-done

	// Verify queue still has only the filler
	e := <-b.eventCh
	assert.IsType(t, events.TurnStarted{}, e)
	select {
	case <-b.eventCh:
		t.Error("Queue should be empty")
	default:
	}
}
