// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Event represents a generic signal from the SessionManager.
type Event interface {
	Type() string
}

// Subscriber defines the interface for event handlers.
// CRITICAL: Implementations MUST monitor ctx.Done() and abort
// long-running operations immediately to prevent goroutine leaks.
type Subscriber interface {
	Handle(ctx context.Context, e Event) error
}

var (
	ErrBusClosed         = errors.New("event bus is closed")
	ErrBusNotInitialized = errors.New("event bus is nil or uninitialized")
)

const (
	defaultQueueSize         = 1024
	defaultAsyncDispatch     = true
	defaultMaxConcurrentSubs = 1024
)

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(sub func(context.Context, Event))
	Shutdown(ctx context.Context) error
	Flush(ctx context.Context) error
	// Listen starts the event bus's internal workers and blocks until the context is canceled.
	// [ARCHITECTURAL REFACTOR] This replaces the previous fire-and-forget goroutine pattern.
	Listen(ctx context.Context) error
	// WaitStarted blocks until the listener goroutine is fully initialized.
	WaitStarted()
}

type subscriberWrapper struct {
	sub Subscriber
	ch  chan Event
}

// SimpleEventBus is an implementation of EventBus.
type SimpleEventBus struct {
	mu                sync.RWMutex
	subscribers       map[string][]*subscriberWrapper
	globalSubscribers []*subscriberWrapper
	closed            bool
	closing           chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	log               *slog.Logger

	running   bool
	listenCtx context.Context
	listenG   *errgroup.Group
	started   chan struct{}
	startOnce sync.Once

	queueSize     int
	asyncDispatch bool           // If false, runs synchronously
	workerWG      sync.WaitGroup // Tracks active worker goroutines for subscribers
	pendingMu     sync.Mutex
	cond          *sync.Cond
	pendingCount  int
}

// busOption defines a functional option for configuring the SimpleEventBus.
type busOption func(*SimpleEventBus)

// WithLogger sets the logger for the SimpleEventBus.
func WithLogger(l *slog.Logger) busOption {
	return func(b *SimpleEventBus) {
		b.log = l
	}
}

// WithQueueSize sets the size of the per-subscriber event queue.
func WithQueueSize(size int) busOption {
	return func(b *SimpleEventBus) {
		b.queueSize = size
	}
}

// WithAsync sets whether the event bus runs asynchronously.
func WithAsync(async bool) busOption {
	return func(b *SimpleEventBus) {
		b.asyncDispatch = async
	}
}

// WithMaxConcurrentSubscribers is deprecated and kept for backward compatibility.
func WithMaxConcurrentSubscribers(n int) busOption {
	return func(b *SimpleEventBus) {}
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus.
func NewSimpleEventBus(ctx context.Context, opts ...busOption) *SimpleEventBus {
	ctx, cancel := context.WithCancel(ctx)
	b := &SimpleEventBus{
		subscribers:       make(map[string][]*subscriberWrapper),
		globalSubscribers: make([]*subscriberWrapper, 0),
		closing:           make(chan struct{}),
		ctx:               ctx,
		cancel:            cancel,
		log:               slog.Default(),
		asyncDispatch:     defaultAsyncDispatch,
		queueSize:         defaultQueueSize,
		started:           make(chan struct{}),
	}
	b.cond = sync.NewCond(&b.pendingMu)

	for _, opt := range opts {
		opt(b)
	}

	return b
}

func (b *SimpleEventBus) WaitStarted() {
	if b == nil {
		return
	}
	<-b.started
}

func (b *SimpleEventBus) signalStarted() {
	if b == nil {
		return
	}
	b.startOnce.Do(func() {
		close(b.started)
	})
}

func (b *SimpleEventBus) incPending() {
	b.pendingMu.Lock()
	b.pendingCount++
	b.pendingMu.Unlock()
}

func (b *SimpleEventBus) decPending() {
	b.pendingMu.Lock()
	b.pendingCount--
	if b.pendingCount == 0 {
		b.cond.Broadcast()
	}
	b.pendingMu.Unlock()
}

func (b *SimpleEventBus) getLogger() *slog.Logger {
	if b == nil || b.log == nil {
		return slog.Default()
	}
	return b.log
}

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
	defer func() {
		if r := recover(); r != nil {
			subType := fmt.Sprintf("%T", sub)
			eventType := event.Type()
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

type funcSubscriber struct {
	f func(context.Context, Event)
}

func (s *funcSubscriber) Handle(ctx context.Context, e Event) error {
	s.f(ctx, e)
	return nil
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
		b.startSubscriberLoop(w)
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
		b.startSubscriberLoop(w)
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
		b.startSubscriberLoop(w)
	}
}

// startSubscriberLoop starts the background worker loop for a given subscriber.
// It assumes b.mu is held by the caller.
func (b *SimpleEventBus) startSubscriberLoop(w *subscriberWrapper) {
	b.workerWG.Add(1)
	b.listenG.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				b.getLogger().Error("panic in dynamic event bus subscriber loop",
					slog.Any("error", r),
					slog.String("stack", string(debug.Stack())))
			}
		}()
		return b.subscriberLoop(b.listenCtx, w)
	})
}

// Shutdown gracefully stops the event bus.
func (b *SimpleEventBus) Shutdown(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	if b.cancel != nil {
		b.cancel()
	}

	// Wait for active workers to finish
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Don't swallow the panic. Log it.
				if b.log != nil {
					b.log.Error("panic in event bus shutdown wait", "error", r, "stack", string(debug.Stack()))
				}
				close(done)
			}
		}()
		b.workerWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Flush waits for all currently queued events to be processed or context timeout.
func (b *SimpleEventBus) Flush(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return ErrBusClosed
	}

	done := make(chan struct{})
	var cancelled bool

	go b.flushWaiter(done, &cancelled)

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return b.cancelFlushWaiter(&cancelled, ctx.Err())
	case <-b.ctx.Done():
		return b.cancelFlushWaiter(&cancelled, ErrBusClosed)
	}
}

// flushWaiter blocks on b.cond until pendingCount drops to 0 or the cancelled
// flag is set by cancelFlushWaiter. It always closes done when it returns.
// Recovers from any panic in cond.Wait to avoid leaving Flush blocked forever.
func (b *SimpleEventBus) flushWaiter(done chan<- struct{}, cancelled *bool) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			if b.log != nil {
				b.log.Error("panic in event bus flush wait", "error", r, "stack", string(debug.Stack()))
			}
		}
	}()

	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	for b.pendingCount > 0 && !*cancelled {
		b.cond.Wait()
	}
}

// cancelFlushWaiter signals the flushWaiter goroutine to stop waiting and
// returns the supplied error. Used by both ctx-cancellation and bus-shutdown
// branches of Flush.
func (b *SimpleEventBus) cancelFlushWaiter(cancelled *bool, err error) error {
	b.pendingMu.Lock()
	*cancelled = true
	b.cond.Broadcast()
	b.pendingMu.Unlock()
	return err
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

// Listen starts all per-subscriber background worker loops and blocks until the context is canceled.
// Implementation follows the coordinated concurrency pattern using errgroup.
func (b *SimpleEventBus) Listen(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}

	b.mu.RLock()
	async := b.asyncDispatch
	b.mu.RUnlock()

	if !async {
		b.signalStarted()
		return nil
	}

	// Create a derived context for the listener
	g, listenCtx := errgroup.WithContext(ctx)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		b.signalStarted()
		return ErrBusClosed
	}

	if b.running {
		b.mu.Unlock()
		return nil // Already running
	}

	b.running = true
	b.listenCtx = listenCtx
	b.listenG = g

	// Coordinated shutdown: even if there are no subscribers,
	// the bus should stay "running" until the context is cancelled.
	b.listenG.Go(func() error {
		<-listenCtx.Done()
		return nil
	})

	// Collect all current subscribers to start their workers
	var wrappers []*subscriberWrapper
	for _, ws := range b.subscribers {
		wrappers = append(wrappers, ws...)
	}
	wrappers = append(wrappers, b.globalSubscribers...)

	for _, w := range wrappers {
		w := w
		b.workerWG.Add(1)
		b.listenG.Go(func() error {
			defer func() {
				if r := recover(); r != nil {
					b.getLogger().Error("panic in event bus subscriber loop",
						slog.Any("error", r),
						slog.String("stack", string(debug.Stack())))
				}
			}()
			return b.subscriberLoop(b.listenCtx, w)
		})
	}

	// Signal that the listener is fully initialized
	b.signalStarted()
	b.mu.Unlock()

	err := b.listenG.Wait()

	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	return err
}
