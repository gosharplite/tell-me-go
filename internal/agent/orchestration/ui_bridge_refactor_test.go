// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

func TestUIBridge_HandleEvent_SafetyWrapper(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(b *uiBridge)
		ctx           func() context.Context
		event         events.Event
		expectEnqueue bool
	}{
		{
			name: "bridge is closed",
			setup: func(b *uiBridge) {
				b.isClosed.Store(true)
			},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: false,
		},
		{
			name:  "caller context is cancelled",
			setup: func(b *uiBridge) {},
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
			setup:         func(b *uiBridge) {},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Manual setup of uiBridge to avoid starting background loop
			b := &uiBridge{
				eventCh: make(chan events.Event, 10),
				logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			}

			tt.setup(b)

			_ = b.handleEvent(tt.ctx(), tt.event)

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

func TestUIBridge_EnqueueEvent_RoutingLogic(t *testing.T) {
	tests := []struct {
		name          string
		event         events.Event
		fillQueue     bool
		expectInQueue bool
	}{
		{
			name:          "critical event - queued",
			event:         events.ResponseEvent{},
			fillQueue:     false,
			expectInQueue: true,
		},
		{
			name:          "non-critical event - queued",
			event:         events.InferenceStartedEvent{},
			fillQueue:     false,
			expectInQueue: true,
		},
		{
			name:          "non-critical event - shed when full",
			event:         events.InferenceStartedEvent{},
			fillQueue:     true,
			expectInQueue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &uiBridge{
				eventCh: make(chan events.Event, 1),
				logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			}

			if tt.fillQueue {
				b.eventCh <- events.TurnStarted{}
			}

			_ = b.enqueueEvent(context.Background(), tt.event)

			if tt.expectInQueue {
				if tt.fillQueue {
					// This case would block if we didn't use a goroutine.
					// For critical events, they block.
					// For non-critical with fillQueue=true, they should be shed.
				} else {
					select {
					case e := <-b.eventCh:
						assert.Equal(t, tt.event, e)
					default:
						t.Error("Expected event to be in queue")
					}
				}
			} else {
				// Verify queue still has the filler and nothing else
				select {
				case e := <-b.eventCh:
					assert.IsType(t, events.TurnStarted{}, e)
				default:
					t.Error("Expected filler to be in queue")
				}
				select {
				case e := <-b.eventCh:
					t.Errorf("Expected only filler in queue, but got %v", e)
				default:
					// Success
				}
			}
		})
	}
}

func TestUIBridge_EnqueueEvent_CriticalBlocking(t *testing.T) {
	b := &uiBridge{
		eventCh: make(chan events.Event, 1),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Fill the queue
	b.eventCh <- events.TurnStarted{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = b.enqueueEvent(ctx, events.ResponseEvent{})
	}()

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
