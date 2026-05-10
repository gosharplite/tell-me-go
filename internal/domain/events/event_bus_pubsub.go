// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

func (b *SimpleEventBus) Publish(ctx context.Context, event Event) error {
	if b == nil {
		return ErrBusNotInitialized
	}

	// Always check context first
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	wrappers, err := b.acquireDispatchSnapshot(event.Type())
	if err != nil {
		return err
	}
	// nil wrappers signals synchronous-dispatch mode.
	if wrappers == nil {
		return b.dispatchSync(ctx, event)
	}

	return b.dispatchAsync(ctx, wrappers, event)
}

// dispatchAsync enqueues the event onto each subscriber's channel, aborting
// early if the context is cancelled. Caller MUST NOT hold b.mu.
func (b *SimpleEventBus) dispatchAsync(ctx context.Context, wrappers []*subscriberWrapper, event Event) error {
	for _, w := range wrappers {
		if err := b.enqueueEvent(ctx, w, event); err != nil {
			return err
		}
	}
	return nil
}

// acquireDispatchSnapshot performs the locked precondition check and snapshot
// for an async Publish.
//
// Returns:
//   - (snapshot, nil): async-mode dispatch; snapshot may be empty but is non-nil.
//   - (nil, nil):      synchronous-dispatch mode; caller must invoke dispatchSync.
//   - (nil, err):      bus is closed (ErrBusClosed).
//
// On return, b.mu is always released.
func (b *SimpleEventBus) acquireDispatchSnapshot(eventType string) ([]*subscriberWrapper, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return nil, ErrBusClosed
	}
	if !b.asyncDispatch {
		// nil signals "use sync dispatch" — distinguishable from an empty async snapshot
		// which is always non-nil (allocated below).
		return nil, nil
	}
	return b.snapshotSubscribers(eventType), nil
}

// snapshotSubscribers returns a unified slice of subscribers for the given event type
// plus all global subscribers. Always returns a non-nil slice (possibly empty).
// The caller MUST hold b.mu (read or write).
func (b *SimpleEventBus) snapshotSubscribers(eventType string) []*subscriberWrapper {
	specificSubs := b.subscribers[eventType]
	globalSubs := b.globalSubscribers

	wrappers := make([]*subscriberWrapper, 0, len(specificSubs)+len(globalSubs))
	wrappers = append(wrappers, specificSubs...)
	wrappers = append(wrappers, globalSubs...)
	return wrappers
}

// enqueueEvent attempts to push the event onto a single subscriber's channel.
// It performs an inc/dec of the pending counter such that the counter is left
// incremented if and only if the event was successfully enqueued.
// Returns ctx.Err() if the context is cancelled during enqueue; otherwise nil
// (including the backpressure-drop case, which is logged but not an error).
// Caller MUST NOT hold b.mu.
func (b *SimpleEventBus) enqueueEvent(ctx context.Context, w *subscriberWrapper, event Event) error {
	b.incPending()
	select {
	case w.ch <- event:
		// Successfully enqueued
		return nil
	case <-ctx.Done():
		b.decPending()
		return ctx.Err()
	default:
		// Backpressure: Subscriber channel is full.
		// Shed load and log to avoid blocking the hot path.
		b.decPending()
		b.getLogger().Warn("subscriber queue full, dropping event",
			slog.String("event_type", event.Type()),
			slog.String("subscriber", fmt.Sprintf("%T", w.sub)))
		return nil
	}
}

func (b *SimpleEventBus) dispatchSync(ctx context.Context, event Event) error {
	b.mu.RLock()
	specificSubs := b.subscribers[event.Type()]
	globalSubs := b.globalSubscribers
	subs := make([]Subscriber, 0, len(specificSubs)+len(globalSubs))
	for _, w := range specificSubs {
		subs = append(subs, w.sub)
	}
	for _, w := range globalSubs {
		subs = append(subs, w.sub)
	}
	b.mu.RUnlock()

	var errs []error
	for _, sub := range subs {
		if err := b.notifySubscriber(ctx, sub, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *SimpleEventBus) newWrapper(sub Subscriber) *subscriberWrapper {
	w := &subscriberWrapper{
		sub: sub,
	}
	if b.asyncDispatch {
		w.ch = make(chan Event, b.queueSize)
	}
	return w
}

func (b *SimpleEventBus) subscriberLoop(ctx context.Context, w *subscriberWrapper) error {
	defer b.workerWG.Done()
	for {
		select {
		// ctx = listenCtx (per-Listen scope); b.ctx = bus-wide scope.
		// Both are watched so Shutdown() can stop workers even when
		// Listen() was called with a long-lived parent context.
		case <-ctx.Done():
			b.drain(w)
			return nil
		case <-b.ctx.Done():
			b.drain(w)
			return nil
		case event, ok := <-w.ch:
			if !ok {
				return nil
			}

			// Hard timeout prevents a subscriber from hanging forever
			timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := b.notifySubscriber(timeoutCtx, w.sub, event); err != nil {
				b.getLogger().ErrorContext(timeoutCtx, "subscriber failed",
					slog.String("event_type", event.Type()),
					slog.String("subscriber_type", fmt.Sprintf("%T", w.sub)),
					slog.Any("error", err),
				)
			}
			cancel()
			b.decPending()
		}
	}
}

func (b *SimpleEventBus) drain(w *subscriberWrapper) {
	// Drain the queue to decrement pending for unhandled events.
	for {
		select {
		case <-w.ch:
			b.decPending()
		default:
			return
		}
	}
}

func (b *SimpleEventBus) notifySubscriber(ctx context.Context, sub Subscriber, event Event) (err error) {
	// Cache event type before calling sub.Handle so that if event.Type()
	// panics inside the recover block below, we don't lose the original
	// subscriber panic reason and stack trace.
	eventType := event.Type()

	defer func() {
		if r := recover(); r != nil {
			subType := fmt.Sprintf("%T", sub)
			stack := string(debug.Stack())

			// 1. Emit structured log with context
			b.getLogger().ErrorContext(ctx, "Subscriber panicked during event handling",
				slog.String("subscriber_type", subType),
				slog.String("event_type", eventType),
				slog.Any("panic_reason", r),
				slog.String("stack_trace", stack),
			)

			// 2. Format the error to be returned to the caller
			err = fmt.Errorf("subscriber panicked: %v", r)
		}
	}()

	// Execute the actual subscriber logic here
	return sub.Handle(ctx, event)
}

func (b *SimpleEventBus) Subscribe(sub func(context.Context, Event)) {
	if b == nil || sub == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	w := b.newWrapper(&funcSubscriber{f: sub})
	b.globalSubscribers = append(b.globalSubscribers, w)

	if b.running && b.asyncDispatch {
		b.startSubscriberLoopLocked(w)
	}
}

// SubscribeGlobal registers a Subscriber that receives all events.
func (b *SimpleEventBus) SubscribeGlobal(sub Subscriber) {
	if b == nil || sub == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	w := b.newWrapper(sub)
	b.globalSubscribers = append(b.globalSubscribers, w)

	if b.running && b.asyncDispatch {
		b.startSubscriberLoopLocked(w)
	}
}

// SubscribeSubscriber registers a Subscriber for a specific event type.
func (b *SimpleEventBus) SubscribeSubscriber(eventType string, sub Subscriber) {
	if b == nil || sub == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	w := b.newWrapper(sub)
	b.subscribers[eventType] = append(b.subscribers[eventType], w)

	if b.running && b.asyncDispatch {
		b.startSubscriberLoopLocked(w)
	}
}

// startSubscriberLoopLocked starts the background worker loop for a given subscriber.
// It assumes b.mu is held by the caller.
func (b *SimpleEventBus) startSubscriberLoopLocked(w *subscriberWrapper) {
	ctx := b.listenCtx
	b.workerWG.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.getLogger().ErrorContext(ctx, "panic in event bus subscriber loop",
					slog.Any("error", r),
					slog.String("stack", string(debug.Stack())))
			}
		}()
		_ = b.subscriberLoop(ctx, w)
	}()
}

// SafePublish attempts to publish an event with a forced timeout.
// It returns an error if the context is cancelled, the queue is full, or the publication fails.
func SafePublish(ctx context.Context, bus EventBus, event Event) error {
	if bus == nil {
		return ErrBusNotInitialized
	}

	// Now bus.Publish is asynchronous and non-blocking (up to queue size).
	// We still apply a strict 2s limit to prevent the publisher from hanging if the queue is full.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err := bus.Publish(ctx, event)
	if err == nil {
		return nil
	}

	// Handle publish failures (mostly context timeout/cancellation)
	wrappedErr := fmt.Errorf("publish failure for event %s: %w", event.Type(), err)

	logger := slog.Default()
	if l, ok := bus.(interface{ getLogger() *slog.Logger }); ok {
		logger = l.getLogger()
	}

	logger.WarnContext(ctx, "Event dropped due to publish failure",
		slog.String("event_type", event.Type()),
		slog.String("error", wrappedErr.Error()),
	)
	return wrappedErr
}
