// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

func TestRenderer_MakeSubscriber(t *testing.T) {
	r := &renderer{}

	t.Run("high priority events are sent to channel", func(t *testing.T) {
		ch := make(chan events.Event, 3)
		sub := r.makeSubscriber(ch)

		sub(context.Background(), events.TurnStarted{Turn: 1, SessionTurns: 0})
		sub(context.Background(), events.TurnStatusEvent{})
		sub(context.Background(), events.ResponseEvent{})

		assert.Equal(t, 3, len(ch), "all three high-priority events should be in the channel")
	})

	t.Run("other events are sent non-blocking", func(t *testing.T) {
		ch := make(chan events.Event, 1)
		sub := r.makeSubscriber(ch)

		sub(context.Background(), events.ToolCallEvent{})

		select {
		case e := <-ch:
			_, ok := e.(events.ToolCallEvent)
			assert.True(t, ok, "should receive ToolCallEvent from channel")
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for non-priority event")
		}
	})

	t.Run("non-priority events wait up to 50ms when channel full", func(t *testing.T) {
		ch := make(chan events.Event) // unbuffered — forces deadline path
		sub := r.makeSubscriber(ch)

		start := time.Now()

		// Send ToolCallEvent to an unbuffered channel with no reader.
		// The 50ms deadline should fire and the subscriber should return.
		sub(context.Background(), events.ToolCallEvent{})

		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond,
			"subscriber should wait at least 50ms before giving up")
		assert.Less(t, elapsed, 150*time.Millisecond,
			"subscriber should not wait excessively long")
	})

	t.Run("control-plane events block when channel full", func(t *testing.T) {
		ch := make(chan events.Event) // unbuffered — forces blocking path
		sub := r.makeSubscriber(ch)

		done := make(chan struct{})
		ready := make(chan struct{})
		go func() {
			close(ready)
			sub(context.Background(), events.TurnStarted{Turn: 1, SessionTurns: 0})
			close(done)
		}()

		// Wait until the goroutine is about to enter the blocking send.
		<-ready
		select {
		case <-done:
			t.Fatal("TurnStarted should block when channel is full, but it returned")
		default:
			// Correct: goroutine is still blocked on the channel send.
		}

		// Now drain the event, unblocking the goroutine.
		<-ch

		select {
		case <-done:
			// Goroutine unblocked and completed.
		case <-time.After(200 * time.Millisecond):
			t.Fatal("TurnStarted should unblock after channel is drained")
		}
	})

	t.Run("ResponseEvent drops after timeout when channel full", func(t *testing.T) {
		ch := make(chan events.Event) // unbuffered
		sub := r.makeSubscriber(ch)

		done := make(chan struct{})
		go func() {
			sub(context.Background(), events.ResponseEvent{})
			close(done)
		}()

		select {
		case <-done:
			// subscriber returned (timed out after 100ms since no reader)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("ResponseEvent should have timed out and returned within 200ms")
		}
	})
}
