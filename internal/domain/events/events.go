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

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}

	// Synchronous mode for testing/specific use cases
	if !b.asyncDispatch {
		b.mu.RUnlock()
		return b.dispatchSync(ctx, event)
	}

	specificSubs := b.subscribers[event.Type()]
	globalSubs := b.globalSubscribers

	// Create unified list of wrappers (snapshot)
	wrappers := make([]*subscriberWrapper, 0, len(specificSubs)+len(globalSubs))
	wrappers = append(wrappers, specificSubs...)
	wrappers = append(wrappers, globalSubs...)

	b.mu.RUnlock() // Release lock EARLY

	for _, w := range wrappers {
		b.incPending()
		select {
		case w.ch <- event:
			// Successfully enqueued
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
		}
	}

	return nil
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

	go func() {
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
		for b.pendingCount > 0 && !cancelled {
			b.cond.Wait()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		b.pendingMu.Lock()
		cancelled = true
		b.cond.Broadcast()
		b.pendingMu.Unlock()
		return ctx.Err()
	case <-b.ctx.Done():
		b.pendingMu.Lock()
		cancelled = true
		b.cond.Broadcast()
		b.pendingMu.Unlock()
		return ErrBusClosed
	}
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

// StatusUpdate signals a change in the agent's internal state or progress.
type StatusUpdate struct {
	Message string
	Level   string
}

func (e StatusUpdate) Type() string { return "StatusUpdate" }

// TurnStarted signals the beginning of a new Think-Act-Observe cycle.
type TurnStarted struct {
	Turn     int
	MaxTurns int
}

func (e TurnStarted) Type() string { return "TurnStarted" }

// InferenceStartedEvent signals that the agent is starting to generate a response.
type InferenceStartedEvent struct {
	Model string
}

func (e InferenceStartedEvent) Type() string { return "InferenceStartedEvent" }

// SummarizationStartedEvent signals that the history summarization process has begun.
type SummarizationStartedEvent struct{}

func (e SummarizationStartedEvent) Type() string { return "SummarizationStartedEvent" }

// ResponseEvent carries the final LLM output.
type ResponseEvent struct {
	Content *llm.Content
}

func (e ResponseEvent) Type() string { return "ResponseEvent" }

// ToolCallEvent signals that one or more tools are being invoked.
type ToolCallEvent struct {
	Calls    []*llm.FunctionCall
	Turn     int
	MaxTurns int
}

func (e ToolCallEvent) Type() string { return "ToolCallEvent" }

// ToolExecutionStartedEvent signals that the tool execution phase has started.
type ToolExecutionStartedEvent struct {
	ToolNames []string
}

func (e ToolExecutionStartedEvent) Type() string { return "ToolExecutionStartedEvent" }

// ToolResultEvent signals that a tool has finished execution.
type ToolResultEvent struct {
	Name   string
	Result tools.ToolResult
}

func (e ToolResultEvent) Type() string { return "ToolResultEvent" }

// UsageMetricsEvent signals that a turn is complete and usage should be recorded.
type UsageMetricsEvent struct {
	Context   context.Context
	Metrics   *llm.Metrics
	LogFile   string
	StartTime time.Time
}

func (e UsageMetricsEvent) Type() string { return "UsageMetricsEvent" }

// SystemMessageEvent signals a system-level message (error, warning, info).
type SystemMessageEvent struct {
	Message string
	Level   string
}

func (e SystemMessageEvent) Type() string { return "SystemMessageEvent" }

// TokenLimitReachedEvent signals that the conversation has reached its token limit.
type TokenLimitReachedEvent struct {
	Tokens   int
	MaxLimit int
}

func (e TokenLimitReachedEvent) Type() string { return "TokenLimitReachedEvent" }

// SummarizationRequired signals that the history is becoming too large and should be summarized.
type SummarizationRequired struct {
	Tokens   int
	MaxLimit int
	Reason   string
}

func (e SummarizationRequired) Type() string { return "SummarizationRequired" }

// TraceEvent carries the TurnTrace for a completed turn.
type TraceEvent struct {
	Trace *telemetry.TurnTrace
}

func (e TraceEvent) Type() string { return "TraceEvent" }

// RetryWaitingEvent signals that the agent is waiting before retrying a failed operation.
type RetryWaitingEvent struct {
	Duration time.Duration
}

func (e RetryWaitingEvent) Type() string { return "RetryWaitingEvent" }

// ConsentStartedEvent signals that the user is being prompted for tool consent.
type ConsentStartedEvent struct{}

func (e ConsentStartedEvent) Type() string { return "ConsentStartedEvent" }

// ConsentFinishedEvent signals that the user consent prompt has finished.
type ConsentFinishedEvent struct{}

func (e ConsentFinishedEvent) Type() string { return "ConsentFinishedEvent" }

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
