// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// eventQueue manages the event channel lifecycle: creation, backpressure-aware
// enqueue, drain, and close. It owns the channel and the isClosed flag.
type eventQueue struct {
	ch        chan events.Event
	loopCtx   context.Context
	logger    ports.Logger
	isClosed  atomic.Bool
	closeOnce sync.Once
}

// newEventQueue creates a new eventQueue with the given capacity.
// loopCtx is the actor's lifecycle context, used for liveness checks in
// backpressure paths to prevent deadlocks when the consumer dies.
func newEventQueue(logger ports.Logger, loopCtx context.Context, capacity int) *eventQueue {
	return &eventQueue{
		ch:      make(chan events.Event, capacity),
		loopCtx: loopCtx,
		logger:  logger,
	}
}

// enqueueEvent attempts to deliver an event to the actor loop. Critical events
// enforce backpressure (blocking until delivery or cancellation). Non-critical
// events are shed if the queue is full.
func (eq *eventQueue) enqueueEvent(ctx context.Context, e events.Event) error {
	if isCriticalEvent(e) {
		return eq.enqueueCritical(ctx, e)
	}
	return eq.enqueueNonCritical(ctx, e)
}

// enqueueCritical delivers a critical event with backpressure. It blocks until
// the event is sent, the caller context is cancelled, or the actor dies.
func (eq *eventQueue) enqueueCritical(ctx context.Context, e events.Event) error {
	select {
	case eq.ch <- e:
		return nil
	case <-ctx.Done():
		eq.logger.Debug("Caller context cancelled while waiting to queue critical event")
		return ctx.Err()
	case <-eq.loopCtx.Done():
		return fmt.Errorf("uibridge actor is dead: %w", eq.loopCtx.Err())
	}
}

// enqueueNonCritical delivers a non-critical event with load-shedding.
// Context cancellation and actor death are checked with strict priority
// before the send attempt, so a cancelled caller always gets an error
// rather than having their event silently shed.
func (eq *eventQueue) enqueueNonCritical(ctx context.Context, e events.Event) error {
	// 1. Strict priority: caller cancellation
	if err := ctx.Err(); err != nil {
		return err
	}
	// 2. Strict priority: actor death
	if err := eq.loopCtx.Err(); err != nil {
		return fmt.Errorf("uibridge actor is dead: %w", err)
	}
	// 3. Non-blocking send with load-shedding
	select {
	case eq.ch <- e:
		return nil
	default:
		eq.logger.Debug("UI Bridge queue full, shedding load/visual event")
		return nil
	}
}

// closeInput closes the event channel. It is safe to call multiple times.
func (eq *eventQueue) closeInput() {
	eq.closeOnce.Do(func() {
		eq.isClosed.Store(true)
		close(eq.ch)
	})
}

// drainRemainingEvents processes any events still buffered in the channel
// after the Listen loop's parent context has been cancelled. It uses
// context.Background() for each event to avoid immediate cancellation during
// final rendering. The drain terminates when the channel is closed or no
// events are immediately available.
func (eq *eventQueue) drainRemainingEvents(process func(context.Context, events.Event)) {
	for {
		select {
		case e, ok := <-eq.ch:
			if !ok {
				return
			}
			process(context.Background(), e)
		default:
			return
		}
	}
}

// isInputClosed reports whether closeInput has been called.
func (eq *eventQueue) isInputClosed() bool {
	return eq.isClosed.Load()
}

// recv returns a receive-only view of the event channel, for use in
// select statements within the Listen loop.
func (eq *eventQueue) recv() <-chan events.Event {
	return eq.ch
}

// sendDirect delivers an event to the channel without backpressure logic.
// Used only by test helpers (syncBridge, dead-consumer tests) to inject
// events that bypass the critical/non-critical classification.
func (eq *eventQueue) sendDirect(e events.Event) {
	eq.ch <- e
}
